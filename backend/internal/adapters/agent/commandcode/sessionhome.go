package commandcode

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// Command Code accepts no system-prompt launch flag, and neither the ACP path's
// `_meta` nor its mod loader is a usable channel: `cmd acp` accepts `--mod`,
// ignores it, and starts normally.
//
// The shell hooks do run under ACP, but they cannot carry standing instructions
// either. SessionStart is the one hook the ACP path never fires, and the other
// three reach the model as tool-result context: a PreToolUse `additionalContext`
// arrives wrapped in a `<hook_context>` tag, only once a tool runs, and Command
// Code's own model discounts it — asked to repeat a token delivered that way, it
// answered that the note was "injected content from tool output, not a note I
// was ever given". A role prompt needs to be authoritative, not advice attached
// to a tool result.
//
// Memory is the one surface Command Code reads in every mode, and its own
// documentation places it in the system prompt rather than the conversation:
//
//	Memory is part of the system prompt, not the conversation.
//	  - It's re-read every request.
//	  - It survives compaction.
//
// The project tier is first-existing-wins, so a repository's own root AGENTS.md
// would silently shadow ours. The user tier is additive and relocatable through
// the documented HOME / USERPROFILE override, which is what this file uses: each
// AO session gets its own home, so concurrent sessions never share a memory file
// and a repo's own memory still loads alongside AO's.

// mirroredProfileFiles are copied from the user's real ~/.commandcode into the
// session home so the session can authenticate and keep its preferences. They are
// read-only inputs for the session; Command Code owns the session copy and may
// write to it.
var mirroredProfileFiles = []string{"auth.json", "config.json", "providers.json"}

// SessionHomeDir returns the per-session home directory for a Command Code
// process, or "" when AO has no data directory to build it under.
func SessionHomeDir(dataDir string, sessionID domain.SessionID) string {
	dataDir = strings.TrimSpace(dataDir)
	sessionID = domain.SessionID(strings.TrimSpace(string(sessionID)))
	if dataDir == "" || sessionID == "" {
		return ""
	}
	return filepath.Join(dataDir, "prompts", string(sessionID), "home")
}

// realCommandCodeDir returns the user's own ~/.commandcode, the source for the
// profile files this session mirrors.
func realCommandCodeDir() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".commandcode")
}

// PrepareSessionHome builds the per-session home and writes AO's standing
// instructions into its user-tier AGENTS.md. It returns the home directory to
// point HOME / USERPROFILE at.
//
// Mirroring is best-effort per file: a user who has never logged in has no
// auth.json, and Command Code can still start for an explicitly configured
// provider. Only a failure to write the memory file itself is fatal, because a
// session whose memory is empty would run with no AO role at all.
func PrepareSessionHome(dataDir string, sessionID domain.SessionID, systemPrompt string) (string, error) {
	home := SessionHomeDir(dataDir, sessionID)
	if home == "" {
		return "", nil
	}
	if strings.TrimSpace(systemPrompt) == "" {
		return "", nil
	}
	profile := filepath.Join(home, ".commandcode")
	if err := os.MkdirAll(profile, 0o700); err != nil {
		return "", fmt.Errorf("command-code: create session profile: %w", err)
	}
	if source := realCommandCodeDir(); source != "" && source != profile {
		mirrorProfileFiles(source, profile)
	}
	memory := strings.TrimRight(systemPrompt, "\n") + "\n"
	if err := writePrivateFile(filepath.Join(profile, "AGENTS.md"), []byte(memory)); err != nil {
		return "", fmt.Errorf("command-code: write session memory: %w", err)
	}
	return home, nil
}

// SessionHomeEnv returns the environment overlay that points a Command Code
// process at its per-session home. HOME and USERPROFILE are both set because
// Command Code resolves ~/.commandcode through either one.
func SessionHomeEnv(home string) map[string]string {
	if strings.TrimSpace(home) == "" {
		return nil
	}
	return map[string]string{
		"HOME":        home,
		"USERPROFILE": home,
	}
}

// mirrorProfileFiles copies the user's Command Code profile into the session
// home. Each file is independent: one missing or unreadable file must not stop
// the session from launching.
func mirrorProfileFiles(source, destination string) {
	for _, name := range mirroredProfileFiles {
		contents, err := os.ReadFile(filepath.Join(source, name)) //nolint:gosec // name is a fixed allowlist
		if err != nil {
			continue
		}
		_ = writePrivateFile(filepath.Join(destination, name), contents)
	}
}

// writePrivateFile writes contents atomically with owner-only permissions, so a
// mirrored credential is never briefly world-readable.
func writePrivateFile(path string, contents []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".ao-command-code-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}
