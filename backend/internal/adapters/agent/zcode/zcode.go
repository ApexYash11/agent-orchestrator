// Package zcode implements the Z.ai ZCode CLI agent adapter: launching the
// interactive ZCode terminal session AO supervises.
//
// ZCode resolves its instructions file from the process HOME: it reads
// `<HOME>/.zcode/AGENTS.md`. AO redirects HOME to a session-scoped directory
// (see AugmentRuntimeEnv) and writes its standing instructions there — a
// private, per-session instruction channel the user's own files never see.
// Nothing is dropped and nothing is rejected.
//
// ZCode (binary "zcode", https://github.com/zai-org/ZCode) is Z.ai's Apache-2.0
// coding harness, tuned for GLM models. Its CLI distribution installs a single
// `zcode` command: no arguments start the terminal UI, `--web` starts the
// browser client, and every other argument goes to the agent CLI.
//
// AO drives ZCode in its interactive TUI and delivers the task after startup.
// `--prompt` is deliberately not used: ZCode's headless prompt mode exits once
// the turn is printed, which would make a supervised worker disappear mid-task.
// Consequently this adapter also has no in-command prompt delivery strategy.
// AO standing instructions use the private HOME-scoped channel (see
// AugmentRuntimeEnv); BuildAfterStartPrompt types only the task itself.
//
// ZCode exposes a hook surface (SessionStart, UserPromptSubmit, PreToolUse,
// PermissionRequest, PostToolUse, PostToolUseFailure, Stop), but project-level
// hooks stay untrusted until a human runs `zcode hooks trust grant` with the
// reviewed digest. User-level hooks exist but have no session-specific AO
// integration here. This adapter therefore installs no hooks, reports no native
// session id, and cannot restore a previous conversation; a session relaunches
// fresh in its worktree. Chat mode is likewise not registered.
package zcode

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters"
	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/agent/agentbase"
	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/agent/binaryutil"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

const adapterID = "zcode"

// envSessionID is the session AO stamps on every launch environment (the
// manager's EnvSessionID). The adapter cannot import the manager package, so
// the key is mirrored here like other adapters do.
const envSessionID = "AO_SESSION_ID"

// Plugin is the ZCode agent adapter. It is safe for concurrent use; the binary
// path is resolved once and cached under binaryMu.
type Plugin struct {
	agentbase.Base
	binaryMu       sync.Mutex
	resolvedBinary string
}

// New returns a ready-to-register ZCode adapter.
func New() *Plugin {
	return &Plugin{}
}

var _ adapters.Adapter = (*Plugin)(nil)
var _ ports.Agent = (*Plugin)(nil)
var _ ports.AgentAuthChecker = (*Plugin)(nil)
var _ ports.AgentBinaryResolver = (*Plugin)(nil)
var _ ports.AgentBinaryResolutionInvalidator = (*Plugin)(nil)
var _ ports.AgentAfterStartPromptBuilder = (*Plugin)(nil)
var _ ports.AgentPromptReadinessProvider = (*Plugin)(nil)
var _ interface{ AugmentRuntimeEnv(map[string]string, string) } = (*Plugin)(nil)

// Manifest returns the adapter's static self-description.
func (p *Plugin) Manifest() adapters.Manifest {
	return adapters.Manifest{
		ID:          adapterID,
		Name:        "ZCode",
		Description: "Run ZCode worker sessions.",
		Version:     "0.0.1",
		Capabilities: []adapters.Capability{
			adapters.CapabilityAgent,
		},
	}
}

// GetLaunchCommand builds the argv that starts an interactive ZCode session:
//
//	zcode [--mode plan|edit|yolo]
//
// The task text is not part of the argv: AO injects it into the running TUI
// (see GetPromptDeliveryStrategy), so ZCode stays in its interactive session
// instead of running headless `--prompt`, which prints one turn and exits.
//
// `--cwd` is not passed even though ZCode accepts it: AO already starts the
// process in the session's worktree, and ZCode resolves project config, skills,
// and session ownership from that working directory.
//
// ZCode's standing instructions never ride the argv. GetLaunchCommand writes
// them into the session-scoped instructions home (AugmentRuntimeEnv redirected
// HOME to it), and the launch itself carries only the binary and the permission
// mode.
func (p *Plugin) GetLaunchCommand(ctx context.Context, cfg ports.LaunchConfig) (cmd []string, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.Config.Model) != "" {
		// ZCode selects models through its provider configuration, not the
		// launch. Silently launching the native default while recording a
		// requested model would misreport what ran (review finding): reject
		// instead, before resolving the binary.
		return nil, errors.New("zcode: per-session model selection is unsupported; configure the native default model before launching")
	}
	if err := writeInstructionFile(ctx, cfg); err != nil {
		return nil, err
	}
	binary, err := p.zcodeBinary(ctx)
	if err != nil {
		return nil, err
	}

	cmd = []string{binary}
	if err := appendModeFlag(&cmd, cfg.Permissions, cfg.Config.Mode); err != nil {
		return nil, err
	}
	return cmd, nil
}

// zcodeInstructionHome returns the session-scoped directory that becomes the
// launched CLI's HOME (HOME=<dir>/.zcode/AGENTS.md is ZCode's user-default
// instructions file). Nothing else in the CLI resolves through that HOME,
// aside from os.homedir() users covered by the ZCODE_STORAGE_DIR override set
// alongside it, so the redirect moves the instruction file and nothing else.
// A missing session id yields an empty path: the caller leaves HOME alone
// (development/probe launches without a session carry no AO instructions).
func zcodeInstructionHome(dataDir, sessionID string) string {
	dataDir = strings.TrimSpace(dataDir)
	sessionID = strings.TrimSpace(sessionID)
	if dataDir == "" || sessionID == "" {
		return ""
	}
	return filepath.Join(dataDir, "zcode", "instruction-homes", sessionID)
}

// writeInstructionFile materializes the launch's standing instructions into
// the session's instructions home, matching the HOME AugmentRuntimeEnv set.
// Inline system prompts and instruction files both land there: ZCode reads a
// single user-level AGENTS.md, so the session gets one merged origin instead
// of a typed echo. Blank instructions write nothing; the redirected home then
// simply has no user-level instruction file, and project discovery inside the
// worktree still applies.
func writeInstructionFile(ctx context.Context, cfg ports.LaunchConfig) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	mergeInstructions := ""
	if strings.TrimSpace(cfg.SystemPrompt) != "" {
		mergeInstructions = cfg.SystemPrompt
	}
	if cfg.SystemPromptFile != "" {
		fileInstructions, err := os.ReadFile(cfg.SystemPromptFile)
		if err != nil {
			return fmt.Errorf("zcode: read AO standing instructions: %w", err)
		}
		if strings.TrimSpace(string(fileInstructions)) != "" {
			if mergeInstructions != "" {
				mergeInstructions += "\n\n"
			}
			mergeInstructions += string(fileInstructions)
		}
	}
	if strings.TrimSpace(mergeInstructions) == "" {
		return nil
	}
	home := zcodeInstructionHome(cfg.DataDir, cfg.SessionID)
	if home == "" {
		return errors.New("zcode: cannot deliver AO standing instructions without a session data directory")
	}
	instructionsDir := filepath.Join(home, ".zcode")
	if err := os.MkdirAll(instructionsDir, 0o700); err != nil {
		return fmt.Errorf("zcode: prepare session instructions home: %w", err)
	}
	if err := os.WriteFile(filepath.Join(instructionsDir, "AGENTS.md"), []byte(mergeInstructions), 0o600); err != nil {
		return fmt.Errorf("zcode: write AO standing instructions: %w", err)
	}
	return nil
}

// AugmentRuntimeEnv redirects the launch environment's HOME to the session's
// instructions home and pins ZCODE_STORAGE_DIR to the real profile, so ZCode
// reads AO's standing instructions from the session-scoped AGENTS.md while
// keeping its login and storage on the user's machine. The manager calls this
// on every session launch path for adapters that opt in. A launch without a
// session id keeps its native environment (development and probe paths carry
// no AO instructions).
func (p *Plugin) AugmentRuntimeEnv(env map[string]string, dataDir string) {
	if env == nil {
		return
	}
	home := zcodeInstructionHome(dataDir, env[envSessionID])
	if home == "" {
		return
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return
	}
	// Keep auth and storage on the real profile. On POSIX os.homedir() would
	// otherwise follow the redirected HOME into the empty session home and
	// lose the user's `.zcode`.
	if _, redirected := env["ZCODE_STORAGE_DIR"]; !redirected {
		if userHome, err := os.UserHomeDir(); err == nil && strings.TrimSpace(userHome) != "" {
			env["ZCODE_STORAGE_DIR"] = filepath.Join(userHome, ".zcode")
		}
	}
	env["HOME"] = home
}

// BuildAfterStartPrompt composes the first message AO types into the running
// TUI: the task itself. Standing instructions no longer ride this message —
// the session gets them through the private AGENTS.md channel — so AO never
// echoes its own role text back into a conversation a user may watch.
func (p *Plugin) BuildAfterStartPrompt(ctx context.Context, cfg ports.LaunchConfig) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if strings.TrimSpace(cfg.Prompt) == "" {
		return "", nil
	}
	return cfg.Prompt, nil
}

// PromptReadinessHints waits briefly for ZCode's interactive prompt before AO
// injects the first message. The opentui prompt glyph varies by build, so the
// patterns are broad and the timeout short: a miss falls back to timed
// delivery rather than blocking the session.
func (p *Plugin) PromptReadinessHints(ctx context.Context, _ ports.LaunchConfig) (ports.PromptReadinessHints, error) {
	if err := ctx.Err(); err != nil {
		return ports.PromptReadinessHints{}, err
	}
	return ports.PromptReadinessHints{
		InitialDelay: 1500 * time.Millisecond,
		Patterns:     []string{"❯", ">"},
		PollInterval: 250 * time.Millisecond,
		Timeout:      5 * time.Second,
	}, nil
}

// zcodeModes is ZCode's documented `--mode` ladder (see
// apps/zcode-cli/docs/usage/05-permissions.md in zai-org/ZCode). `auto` is a
// reserved internal value and is deliberately absent.
var zcodeModes = map[string]bool{
	"plan": true, "build": true, "edit": true, "yolo": true, "guarded": true,
}

// appendModeFlag pins ZCode's own permission mode. An explicit session mode
// (AgentConfig.Mode, exposed by GetConfigSpec) wins; otherwise AO's permission
// mapping applies. PermissionModeDefault with no pin emits no flag so ZCode
// keeps its configured mode (`build`, which asks before writes and
// side-effecting tools). Auto and bypass both collapse onto `yolo`: it skips
// approval prompts for ordinary tools, and ZCode has no separate switch that
// also drops its remaining guard rails (`guarded` still asks for dangerous
// commands).
func appendModeFlag(cmd *[]string, permissions ports.PermissionMode, mode string) error {
	mode = strings.TrimSpace(mode)
	if mode != "" {
		if !zcodeModes[mode] {
			return fmt.Errorf("zcode: unknown mode %q (want plan, build, edit, yolo, or guarded)", mode)
		}
		*cmd = append(*cmd, "--mode", mode)
		return nil
	}
	switch ports.NormalizePermissionMode(permissions) {
	case ports.PermissionModeAcceptEdits:
		*cmd = append(*cmd, "--mode", "edit")
	case ports.PermissionModeAuto, ports.PermissionModeBypassPermissions:
		*cmd = append(*cmd, "--mode", "yolo")
	}
	return nil
}

// GetConfigSpec reports ZCode's own permission ladder. ZCode selects its GLM
// models through its provider configuration file rather than a launch flag, so
// AO exposes mode rather than a misleading raw-model field.
func (p *Plugin) GetConfigSpec(ctx context.Context) (ports.ConfigSpec, error) {
	if err := ctx.Err(); err != nil {
		return ports.ConfigSpec{}, err
	}
	return ports.ConfigSpec{Fields: []ports.ConfigField{{
		Key:         "mode",
		Type:        ports.ConfigFieldEnum,
		Description: "ZCode permission mode pinned for the session; overrides the session permission mapping.",
		Enum:        []string{"plan", "build", "edit", "yolo", "guarded"},
	}}}, nil
}

// GetPromptDeliveryStrategy reports that AO should type prompted tasks into the
// running ZCode TUI after startup.
func (p *Plugin) GetPromptDeliveryStrategy(ctx context.Context, _ ports.LaunchConfig) (ports.PromptDeliveryStrategy, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return ports.PromptDeliveryAfterStart, nil
}

var zcodeBinarySpec = binaryutil.BinarySpec{
	Label:         "zcode",
	Names:         []string{"zcode"},
	WinNames:      []string{"zcode.cmd", "zcode.exe", "zcode"},
	UnixPaths:     []string{"/usr/local/bin/zcode", "/opt/homebrew/bin/zcode"},
	UnixHomePaths: [][]string{{".local", "bin", "zcode"}, {".zcode", "runtime", "bin", "zcode"}},
	WinPaths: []binaryutil.WinPath{
		{Base: binaryutil.WinHome, Parts: []string{".local", "bin", "zcode.cmd"}},
		{Base: binaryutil.WinLocalAppData, Parts: []string{"Programs", "ZCode", "zcode.cmd"}},
		{Base: binaryutil.WinAppData, Parts: []string{"npm", "zcode.cmd"}},
	},
}

// ResolveZCodeBinary locates the user-installed ZCode executable. The CLI
// distribution installs to ~/.zcode/runtime and links `zcode` into
// ~/.local/bin; AO never bundles ZCode.
func ResolveZCodeBinary(ctx context.Context) (string, error) {
	return binaryutil.ResolveBinary(ctx, zcodeBinarySpec)
}

// ResolveBinary implements ports.AgentBinaryResolver for harness readiness.
func (p *Plugin) ResolveBinary(ctx context.Context) (string, error) { return p.zcodeBinary(ctx) }

// InvalidateBinaryResolution forgets the cached executable path so the next
// launch resolves it again: after a ZCode update, for example.
func (p *Plugin) InvalidateBinaryResolution() {
	p.binaryMu.Lock()
	p.resolvedBinary = ""
	p.binaryMu.Unlock()
}

func (p *Plugin) zcodeBinary(ctx context.Context) (string, error) {
	p.binaryMu.Lock()
	defer p.binaryMu.Unlock()
	if p.resolvedBinary != "" {
		return p.resolvedBinary, nil
	}
	binary, err := ResolveZCodeBinary(ctx)
	if err != nil {
		return "", err
	}
	p.resolvedBinary = binary
	return binary, nil
}

// nativeSessionIDPrefix is the prefix ZCode uses for its own session ids
// (`sess_<uuid>`), as printed in `--resume` usage and stored in its CLI
// database. It is recorded here so a future hook-based capture can validate ids
// without re-deriving the format.
const nativeSessionIDPrefix = "sess_"

// validNativeSessionID reports whether id looks like a ZCode session id.
func validNativeSessionID(id string) bool {
	trimmed := strings.TrimSpace(id)
	return strings.HasPrefix(trimmed, nativeSessionIDPrefix) && len(trimmed) > len(nativeSessionIDPrefix)
}
