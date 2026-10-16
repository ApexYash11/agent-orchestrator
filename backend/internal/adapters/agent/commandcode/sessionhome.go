package commandcode

import (
	"encoding/json"
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
// Code's own model discounts it -- asked to repeat a token delivered that way, it
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
// session home so the session can authenticate and keep its preferences,
// including the user's own permission rules, hooks and MCP servers. settings.json
// and mcp.json are separate supported inputs from config.json, not fields of it,
// so omitting them would silently drop user policy once HOME is redirected. Each
// file is independent: one missing or unreadable file must not stop the session
// from launching.
var mirroredProfileFiles = []string{"auth.json", "config.json", "providers.json", "settings.json", "mcp.json"}

// linkedHomeEntries are real-home dotfiles exposed inside the session home as
// links rather than copies. They cover developer tooling that must resolve
// under HOME and offers no environment redirect: SSH reads keys, config and
// known_hosts from ~/.ssh, so git-over-SSH and ssh-based tooling would
// otherwise break behind the redirect. Links keep a single source of truth --
// a copied key would silently go stale on rotation -- and cross no privilege
// boundary since the session already runs as the same user.
//
// Linking is best-effort per entry: platforms without symlink privilege (stock
// Windows) skip the link and the session proceeds without it, exactly as
// before. Nothing is ever overwritten: an existing session-side entry wins.
var linkedHomeEntries = []string{".ssh"}

// SessionHomeDir returns the per-session home directory for a Command Code
// process, or "" when AO has no data directory to build it under.
//
// The home lives under dataDir/harnesses/command-code, alongside the other
// harness-owned state, and deliberately outside the disposable prompts/<id>
// directory: relaunchSessionWithPolicyAndGeneration wipes that whole directory
// through cleanupSystemPromptDir on ordinary restore failures, and a failed
// restore of a session created by this change must not permanently delete its
// previous conversation.
func SessionHomeDir(dataDir string, sessionID domain.SessionID) string {
	dataDir = strings.TrimSpace(dataDir)
	sessionID = domain.SessionID(strings.TrimSpace(string(sessionID)))
	if dataDir == "" || sessionID == "" {
		return ""
	}
	return filepath.Join(dataDir, "harnesses", "command-code", string(sessionID))
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
//
// Re-preparing an existing home rewrites only AO's own files (AGENTS.md and the
// mirrored profile inputs). Native transcripts the session has already written
// there are left untouched.
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
	if realHome, err := os.UserHomeDir(); err == nil && strings.TrimSpace(realHome) != "" && realHome != home {
		linkRealDotfiles(realHome, home)
	}
	memory := strings.TrimRight(systemPrompt, "\n") + "\n"
	if err := writePrivateFile(filepath.Join(profile, "AGENTS.md"), []byte(memory)); err != nil {
		return "", fmt.Errorf("command-code: write session memory: %w", err)
	}
	return home, nil
}

// MigrateNativeHistory copies a captured native conversation into the session
// home so `--resume` and `session/load` keep working after AO switches the
// session's HOME.
//
// Sessions created before this change wrote their transcripts under the real
// user's ~/.commandcode/projects/<slug>/, and Command Code resolves resume
// targets inside the home-scoped project catalog. Without migration, restoring
// such a session from its new isolated home searches an empty catalog and the
// conversation is unreachable. Only files belonging to this exact conversation
// id are copied, only when the destination does not already have them, and a
// missing or unreadable source is not an error: there may simply be nothing to
// migrate, for example on a fresh launch.
func MigrateNativeHistory(dataDir string, sessionID domain.SessionID, agentSessionID string) {
	agentSessionID = strings.TrimSpace(agentSessionID)
	home := SessionHomeDir(dataDir, sessionID)
	source := realCommandCodeDir()
	if agentSessionID == "" || home == "" || source == "" {
		return
	}
	matches, err := filepath.Glob(filepath.Join(source, "projects", "*", agentSessionID+".*"))
	if err != nil || len(matches) == 0 {
		return
	}
	for _, match := range matches {
		rel, err := filepath.Rel(filepath.Join(source, "projects"), match)
		if err != nil {
			continue
		}
		destination := filepath.Join(home, ".commandcode", "projects", rel)
		if _, err := os.Stat(destination); err == nil {
			continue
		}
		contents, err := os.ReadFile(match) //nolint:gosec // match comes from our own glob of a fixed filename
		if err != nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			continue
		}
		_ = writePrivateFile(destination, contents)
	}
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

// SessionProcessEnv returns the full environment overlay for a Command Code
// process: the per-session home plus passthroughs that keep the worker's own
// developer tooling working under the redirected HOME.
//
// The overlay is inherited by Command Code's shell commands and hooks, not just
// its memory loader, and the generated home contains only .commandcode. Without
// passthroughs, a user whose author identity or credential helper lives in
// ~/.gitconfig would fail `git commit` inside every AO session, and gh would
// lose its login. GIT_CONFIG_GLOBAL repoints git's global config lookup at the
// real file without copying it; GH_CONFIG_DIR does the same for gh. Each is set
// only when its target exists, so users without either are unaffected.
//
// Values the caller already supplied always win: an explicit GH_CONFIG_DIR or
// GIT_CONFIG_GLOBAL in base (daemon or project environment) is never
// overwritten, and neither is a native lookup the tool would otherwise perform.
// In particular git prefers $XDG_CONFIG_HOME/git/config over ~/.gitconfig and
// gh prefers $XDG_CONFIG_HOME/gh, so when XDG is in play there is nothing to
// fill in. Comparison is case-insensitive because Windows folds environment
// keys.
func SessionProcessEnv(dataDir string, sessionID domain.SessionID, base map[string]string) map[string]string {
	env := SessionHomeEnv(SessionHomeDir(dataDir, sessionID))
	if env == nil {
		return nil
	}
	realHome, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(realHome) == "" {
		return env
	}
	xdgSet := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")) != ""
	// Without an XDG layer, git falls back to ~/.gitconfig.
	if !hasEnvKey(base, "GIT_CONFIG_GLOBAL") && !xdgSet {
		if gitconfig := filepath.Join(realHome, ".gitconfig"); isRegularFile(gitconfig) {
			env["GIT_CONFIG_GLOBAL"] = gitconfig
		}
	}
	if !hasEnvKey(base, "GH_CONFIG_DIR") && !xdgSet {
		if dir := ghConfigDir(realHome); dir != "" {
			env["GH_CONFIG_DIR"] = dir
		}
	}
	return env
}

// hasEnvKey reports whether an environment map already defines a key,
// case-insensitively so Windows key folding cannot duplicate it.
func hasEnvKey(env map[string]string, key string) bool {
	for existing := range env {
		if strings.EqualFold(existing, key) {
			return true
		}
	}
	return false
}

// ghConfigDir locates the user's gh configuration without moving it: the
// platform default (%APPDATA%\GitHub CLI on Windows, ~/.config/gh elsewhere),
// used only when no XDG layer exists. Callers check XDG first.
func ghConfigDir(realHome string) string {
	if appData := strings.TrimSpace(os.Getenv("APPDATA")); appData != "" {
		if dir := filepath.Join(appData, "GitHub CLI"); isDirectory(dir) {
			return dir
		}
	}
	if dir := filepath.Join(realHome, ".config", "gh"); isDirectory(dir) {
		return dir
	}
	return ""
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func isDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// linkRealDotfiles exposes entries from the real home inside the session home
// without copying them. Copying would fork the source of truth -- a rotated
// key would leave a stale copy behind -- and private keys must never be
// duplicated onto disk by an agent launcher.
func linkRealDotfiles(realHome, sessionHome string) {
	for _, name := range linkedHomeEntries {
		source := filepath.Join(realHome, name)
		if !isDirectory(source) && !isRegularFile(source) {
			continue
		}
		link := filepath.Join(sessionHome, name)
		if _, err := os.Lstat(link); err == nil {
			continue
		}
		_ = os.Symlink(source, link)
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
		if name == "settings.json" {
			contents = rebaseSettingsHomeRefs(contents, filepath.Dir(source))
		}
		_ = writePrivateFile(filepath.Join(destination, name), contents)
	}
}

// rebaseSettingsHomeRefs rewrites home-relative asset paths in a mirrored
// settings.json to absolute real-home paths.
//
// Copying the JSON alone leaves home-relative references broken: a skill entry
// like "~/.commandcode/custom-skills" or a hook running
// `bash "$HOME/.commandcode/hooks/guard.sh"` would resolve inside the isolated
// home, where neither exists, so configured skills disappear and user hooks
// cannot execute. Command Code explicitly expands `~/` in configured skill
// paths, so these references unambiguously mean the real home. Rewriting
// happens on AO's copy only; the user's file is never touched.
//
// Only `~/` and `$HOME` prefixes are rewritten, and only inside the `skills`
// list and hook `command` strings -- permission rule patterns and other free
// text keep their literal form. Anything that fails to parse as JSON is
// returned unchanged rather than dropping the user's settings.
func rebaseSettingsHomeRefs(contents []byte, realHome string) []byte {
	realHome = strings.TrimSpace(realHome)
	if realHome == "" {
		return contents
	}
	var settings map[string]any
	if err := json.Unmarshal(contents, &settings); err != nil {
		return contents
	}
	changed := false
	if skills, ok := settings["skills"].([]any); ok {
		for i, skill := range skills {
			if path, ok := skill.(string); ok {
				if rebased, ok := rebaseHomeRef(path, realHome); ok {
					skills[i] = rebased
					changed = true
				}
			}
		}
	}
	if hooks, ok := settings["hooks"].(map[string]any); ok {
		for _, groups := range hooks {
			entries, ok := groups.([]any)
			if !ok {
				continue
			}
			for _, entry := range entries {
				entryMap, ok := entry.(map[string]any)
				if !ok {
					continue
				}
				handlers, ok := entryMap["hooks"].([]any)
				if !ok {
					continue
				}
				for _, handler := range handlers {
					handlerMap, ok := handler.(map[string]any)
					if !ok {
						continue
					}
					command, ok := handlerMap["command"].(string)
					if !ok || command == "" {
						continue
					}
					rebased := strings.ReplaceAll(command, "$HOME", realHome)
					for strings.Contains(rebased, "~/") {
						rebased = strings.Replace(rebased, "~/", realHome+"/", 1)
					}
					if rebased != command {
						handlerMap["command"] = rebased
						changed = true
					}
				}
			}
		}
	}
	if !changed {
		return contents
	}
	encoded, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return contents
	}
	return encoded
}

// rebaseHomeRef resolves a single configured path against the real home,
// reporting whether it changed. Absolute paths and non-home references pass
// through untouched.
func rebaseHomeRef(path, realHome string) (string, bool) {
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(realHome, strings.TrimPrefix(path, "~/")), true
	}
	if path == "~" {
		return realHome, true
	}
	if strings.HasPrefix(path, "$HOME/") {
		return filepath.Join(realHome, strings.TrimPrefix(path, "$HOME/")), true
	}
	if path == "$HOME" {
		return realHome, true
	}
	return path, false
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
