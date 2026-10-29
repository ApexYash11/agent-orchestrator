// Package zcode implements the Z.ai ZCode CLI agent adapter: launching the
// interactive ZCode terminal session AO supervises.
//
// ZCode has no system-prompt channel, so AO standing instructions ride the
// session's first typed message (see BuildAfterStartPrompt); nothing is
// dropped and nothing is rejected.
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
// AO standing instructions have no private channel (no system-prompt flag), so
// BuildAfterStartPrompt delivers them explicitly as the first typed message;
// nothing is silently dropped and instruction-bearing launches start normally.
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
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters"
	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/agent/agentbase"
	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/agent/binaryutil"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

const adapterID = "zcode"

// instructionPreambleHeader frames AO's standing instructions inside the first
// typed message. It is explicit by design: ZCode has no system-prompt channel,
// so the instructions travel with the task and the framing keeps the model from
// treating the instructions themselves as the task.
const instructionPreambleHeader = "The following are standing operating instructions for this AO session. They apply to the whole session; the actual task follows after the marker line."

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
// ZCode has no system-prompt CLI flag, so standing instructions are never part
// of the argv. BuildAfterStartPrompt delivers them with the first typed message;
// the launch itself carries only the binary and the permission mode.
func (p *Plugin) GetLaunchCommand(ctx context.Context, cfg ports.LaunchConfig) (cmd []string, err error) {
	if err := ctx.Err(); err != nil {
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

// BuildAfterStartPrompt composes the first message AO types into the running
// TUI: AO's standing instructions (when the session carries any) framed ahead
// of the task. This is ZCode's only instruction surface — there is no
// system-prompt flag and project hooks stay untrusted — so the composition is
// explicit rather than silent: the preamble names itself as instructions and
// marks where the task begins. A task-only launch composes to the task
// unchanged.
func (p *Plugin) BuildAfterStartPrompt(ctx context.Context, cfg ports.LaunchConfig) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	instructions := strings.TrimSpace(cfg.SystemPrompt)
	task := strings.TrimSpace(cfg.Prompt)
	switch {
	case instructions == "" && task == "":
		return "", nil
	case instructions == "":
		return cfg.Prompt, nil
	case task == "":
		return instructionPreambleHeader + "\n\n" + cfg.SystemPrompt, nil
	default:
		return instructionPreambleHeader + "\n\n" + cfg.SystemPrompt + "\n\n--- Task ---\n\n" + cfg.Prompt, nil
	}
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
