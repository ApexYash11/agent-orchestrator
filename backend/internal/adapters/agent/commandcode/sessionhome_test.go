package commandcode

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// The user tier is used instead of the project tier precisely because it is
// additive: a repository that already carries its own AGENTS.md must keep it,
// and AO's instructions must load alongside it rather than being shadowed.
func TestPrepareSessionHomeWritesStandingInstructionsToTheUserTier(t *testing.T) {
	dataDir := t.TempDir()
	home, err := PrepareSessionHome(dataDir, domain.SessionID("mer-1"), "You are an AO worker.")
	if err != nil {
		t.Fatalf("PrepareSessionHome: %v", err)
	}
	if want := filepath.Join(dataDir, "harnesses", "command-code", "mer-1"); home != want {
		t.Fatalf("home = %q, want %q", home, want)
	}
	memory := filepath.Join(home, ".commandcode", "AGENTS.md")
	contents, err := os.ReadFile(memory)
	if err != nil {
		t.Fatalf("read memory: %v", err)
	}
	if string(contents) != "You are an AO worker.\n" {
		t.Fatalf("memory = %q", contents)
	}
}

// Two AO sessions on the same machine must not overwrite each other's role
// prompt. Each owns a distinct home under its own session id.
func TestPrepareSessionHomeIsolatesConcurrentSessions(t *testing.T) {
	dataDir := t.TempDir()
	first, err := PrepareSessionHome(dataDir, domain.SessionID("mer-1"), "orchestrator role")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := PrepareSessionHome(dataDir, domain.SessionID("mer-2"), "worker role")
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first == second {
		t.Fatalf("both sessions share home %q", first)
	}
	for home, want := range map[string]string{first: "orchestrator role", second: "worker role"} {
		contents, err := os.ReadFile(filepath.Join(home, ".commandcode", "AGENTS.md"))
		if err != nil {
			t.Fatalf("read %q: %v", home, err)
		}
		if !strings.Contains(string(contents), want) {
			t.Fatalf("%q memory = %q, want %q", home, contents, want)
		}
	}
}

// Command Code reloads memory on every request, so re-preparing a session must
// update the file in place rather than fail on the existing one.
func TestPrepareSessionHomeRewritesOnRestore(t *testing.T) {
	dataDir := t.TempDir()
	if _, err := PrepareSessionHome(dataDir, domain.SessionID("mer-1"), "first role"); err != nil {
		t.Fatalf("first: %v", err)
	}
	home, err := PrepareSessionHome(dataDir, domain.SessionID("mer-1"), "recomputed role")
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	contents, err := os.ReadFile(filepath.Join(home, ".commandcode", "AGENTS.md"))
	if err != nil {
		t.Fatalf("read memory: %v", err)
	}
	if string(contents) != "recomputed role\n" {
		t.Fatalf("memory = %q, want the recomputed instructions", contents)
	}
}

// A session with no instructions must not get an empty memory file, which
// Command Code would load as an empty block ahead of its own system prompt.
func TestPrepareSessionHomeSkipsEmptyInstructions(t *testing.T) {
	dataDir := t.TempDir()
	for _, prompt := range []string{"", "   ", "\n\t"} {
		home, err := PrepareSessionHome(dataDir, domain.SessionID("mer-empty"), prompt)
		if err != nil {
			t.Fatalf("PrepareSessionHome(%q): %v", prompt, err)
		}
		if home != "" {
			t.Fatalf("PrepareSessionHome(%q) = %q, want no home", prompt, home)
		}
		if _, err := os.Stat(filepath.Join(dataDir, "harnesses", "command-code", "mer-empty")); !os.IsNotExist(err) {
			t.Fatalf("PrepareSessionHome(%q) created storage", prompt)
		}
	}
}

// A mirrored credential must never be briefly world-readable. Go's os.Chmod
// only toggles the read-only bit on Windows, where access is governed by ACLs
// rather than the POSIX mode bits, so the mode assertion runs where it is
// meaningful and the copy itself is checked everywhere.
func TestPrepareSessionHomeKeepsMirroredFilesPrivate(t *testing.T) {
	dataDir := t.TempDir()
	source := t.TempDir()
	if err := os.MkdirAll(filepath.Join(source, ".commandcode"), 0o700); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(source, ".commandcode", "auth.json")
	if err := os.WriteFile(secret, []byte(`{"token":"secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", source)
	t.Setenv("USERPROFILE", source)

	home, err := PrepareSessionHome(dataDir, domain.SessionID("mer-1"), "worker role")
	if err != nil {
		t.Fatalf("PrepareSessionHome: %v", err)
	}
	mirrored, err := os.ReadFile(filepath.Join(home, ".commandcode", "auth.json"))
	if err != nil {
		t.Fatalf("read mirrored auth.json: %v", err)
	}
	if !strings.Contains(string(mirrored), "secret") {
		t.Fatalf("mirrored auth.json = %q, want the user's credential", mirrored)
	}
	if runtime.GOOS == "windows" {
		t.Skip("Windows expresses access through ACLs, not the POSIX mode bits Go reports")
	}
	info, err := os.Stat(filepath.Join(home, ".commandcode", "AGENTS.md"))
	if err != nil {
		t.Fatalf("stat memory: %v", err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("memory mode = %v, want no group or world access", perm)
	}
}

// The terminal side points HOME / USERPROFILE at the same directory the Chat
// binding does, so both surfaces resolve the same memory.
func TestAugmentRuntimeLaunchEnvPointsAtTheSessionHome(t *testing.T) {
	dataDir := t.TempDir()
	env := map[string]string{}
	(&Plugin{}).AugmentRuntimeLaunchEnv(env, dataDir, domain.SessionID("mer-1"), "launch-1")
	want := filepath.Join(dataDir, "harnesses", "command-code", "mer-1")
	if env["HOME"] != want || env["USERPROFILE"] != want {
		t.Fatalf("env = %#v, want HOME and USERPROFILE = %q", env, want)
	}
}

// A session with no data directory has nowhere to put a home, so AO must leave
// the environment alone rather than point Command Code at an empty path.
func TestSessionHomeDirNeedsBothInputs(t *testing.T) {
	if got := SessionHomeDir("", domain.SessionID("mer-1")); got != "" {
		t.Fatalf("SessionHomeDir with no dataDir = %q", got)
	}
	if got := SessionHomeDir(t.TempDir(), ""); got != "" {
		t.Fatalf("SessionHomeDir with no session = %q", got)
	}
	if got := SessionHomeEnv(""); got != nil {
		t.Fatalf("SessionHomeEnv(\"\") = %v, want nil", got)
	}
}

// The home must live outside the disposable prompts/<id> directory: a failed
// restore wipes that whole tree through cleanupSystemPromptDir, and the wipe
// must never take the native conversation with it.
func TestSessionHomeSurvivesOutsideThePromptsDirectory(t *testing.T) {
	home := SessionHomeDir(filepath.Join("data"), domain.SessionID("mer-1"))
	if strings.Contains(filepath.ToSlash(home), "prompts/") {
		t.Fatalf("home = %q, want a location cleanupSystemPromptDir never touches", home)
	}
}

// settings.json and mcp.json are separate supported inputs from config.json —
// user deny rules, hooks and MCP servers — so they must ride along into the
// session home rather than silently disappearing behind the redirect.
func TestPrepareSessionHomeMirrorsUserSettingsAndMCP(t *testing.T) {
	dataDir := t.TempDir()
	source := t.TempDir()
	profile := filepath.Join(source, ".commandcode")
	if err := os.MkdirAll(profile, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"settings.json": `{"permissions":{"deny":["Bash(rm -rf:*)"]}}`,
		"mcp.json":      `{"mcpServers":{"tools":{"command":"tools"}}}`,
	} {
		if err := os.WriteFile(filepath.Join(profile, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", source)
	t.Setenv("USERPROFILE", source)

	home, err := PrepareSessionHome(dataDir, domain.SessionID("mer-1"), "worker role")
	if err != nil {
		t.Fatalf("PrepareSessionHome: %v", err)
	}
	for name, want := range map[string]string{
		"settings.json": "rm -rf",
		"mcp.json":      "mcpServers",
	} {
		contents, err := os.ReadFile(filepath.Join(home, ".commandcode", name))
		if err != nil {
			t.Fatalf("read mirrored %s: %v", name, err)
		}
		if !strings.Contains(string(contents), want) {
			t.Fatalf("mirrored %s = %q, want %q", name, contents, want)
		}
	}
}

// Sessions created before per-session homes existed keep their native transcript
// under the real home's project catalog. Restoring one must bring that
// conversation into the isolated home first, or --resume searches an empty
// catalog. Only this conversation's own files move; the rest of the catalog is
// untouched, and an existing destination is never overwritten.
func TestMigrateNativeHistoryBringsTheCapturedConversationHome(t *testing.T) {
	dataDir := t.TempDir()
	source := t.TempDir()
	slug := filepath.Join(source, ".commandcode", "projects", "c-users-work")
	if err := os.MkdirAll(slug, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"native-1.jsonl", "native-1.meta.json", "other.jsonl"} {
		if err := os.WriteFile(filepath.Join(slug, name), []byte("contents of "+name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", source)
	t.Setenv("USERPROFILE", source)

	home, err := PrepareSessionHome(dataDir, domain.SessionID("mer-1"), "worker role")
	if err != nil {
		t.Fatalf("PrepareSessionHome: %v", err)
	}
	MigrateNativeHistory(dataDir, domain.SessionID("mer-1"), "native-1")
	for _, name := range []string{"native-1.jsonl", "native-1.meta.json"} {
		contents, err := os.ReadFile(filepath.Join(home, ".commandcode", "projects", "c-users-work", name))
		if err != nil {
			t.Fatalf("migrated %s missing: %v", name, err)
		}
		if string(contents) != "contents of "+name {
			t.Fatalf("migrated %s = %q", name, contents)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".commandcode", "projects", "c-users-work", "other.jsonl")); !os.IsNotExist(err) {
		t.Fatal("migration carried an unrelated conversation")
	}

	// A second restore must not clobber the session's own newer copy.
	migrated := filepath.Join(home, ".commandcode", "projects", "c-users-work", "native-1.jsonl")
	if err := os.WriteFile(migrated, []byte("session's own newer copy"), 0o600); err != nil {
		t.Fatal(err)
	}
	MigrateNativeHistory(dataDir, domain.SessionID("mer-1"), "native-1")
	contents, err := os.ReadFile(migrated)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "session's own newer copy" {
		t.Fatalf("restore overwrote the session copy: %q", contents)
	}
}

// With no captured native id there is nothing to migrate, and the restore must
// proceed as a fresh start rather than fail.
func TestMigrateNativeHistoryWithoutAnIdentityIsANoOp(t *testing.T) {
	dataDir := t.TempDir()
	MigrateNativeHistory(dataDir, domain.SessionID("mer-1"), "")
	MigrateNativeHistory(dataDir, domain.SessionID("mer-1"), "   ")
	if _, err := os.Stat(filepath.Join(dataDir, "harnesses")); !os.IsNotExist(err) {
		t.Fatal("migration created storage with nothing to migrate")
	}
}

// The redirected HOME is inherited by the agent's shell children, so the real
// developer tooling has to be reachable without copying secrets: git through
// its documented global-config pointer, gh through its config directory. Each
// is set only when its target exists.
func TestSessionProcessEnvPreservesRealDeveloperTooling(t *testing.T) {
	dataDir := t.TempDir()
	real := t.TempDir()
	if err := os.WriteFile(filepath.Join(real, ".gitconfig"), []byte("[user]\n\tname = dev\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(real, ".config", "gh"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", real)
	t.Setenv("USERPROFILE", real)
	t.Setenv("APPDATA", t.TempDir())

	env := SessionProcessEnv(dataDir, domain.SessionID("mer-1"))
	if env["GIT_CONFIG_GLOBAL"] != filepath.Join(real, ".gitconfig") {
		t.Fatalf("GIT_CONFIG_GLOBAL = %q, want the real .gitconfig", env["GIT_CONFIG_GLOBAL"])
	}
	if env["GH_CONFIG_DIR"] != filepath.Join(real, ".config", "gh") {
		t.Fatalf("GH_CONFIG_DIR = %q, want the real gh config", env["GH_CONFIG_DIR"])
	}
	want := filepath.Join(dataDir, "harnesses", "command-code", "mer-1")
	if env["HOME"] != want || env["USERPROFILE"] != want {
		t.Fatalf("env HOME = %q / USERPROFILE = %q, want %q", env["HOME"], env["USERPROFILE"], want)
	}
}

// A machine without either tool configured gets neither variable, rather than
// pointers at paths that do not exist.
func TestSessionProcessEnvOmitsMissingTooling(t *testing.T) {
	dataDir := t.TempDir()
	real := t.TempDir()
	t.Setenv("HOME", real)
	t.Setenv("USERPROFILE", real)
	t.Setenv("APPDATA", t.TempDir())

	env := SessionProcessEnv(dataDir, domain.SessionID("mer-1"))
	if _, ok := env["GIT_CONFIG_GLOBAL"]; ok {
		t.Fatalf("GIT_CONFIG_GLOBAL = %q with no real .gitconfig", env["GIT_CONFIG_GLOBAL"])
	}
	if _, ok := env["GH_CONFIG_DIR"]; ok {
		t.Fatalf("GH_CONFIG_DIR = %q with no real gh config", env["GH_CONFIG_DIR"])
	}
}

// Final workspace teardown removes the session home. It runs only there —
// never on spawn or restore failures, where the home still holds the native
// history a retry needs.
func TestCleanupWorkspaceRemovesTheSessionHome(t *testing.T) {
	dataDir := t.TempDir()
	home, err := PrepareSessionHome(dataDir, domain.SessionID("mer-1"), "worker role")
	if err != nil {
		t.Fatalf("PrepareSessionHome: %v", err)
	}
	if err := (&Plugin{}).CleanupWorkspace(context.Background(), ports.WorkspaceHookConfig{
		DataDir:   dataDir,
		SessionID: "mer-1",
	}); err != nil {
		t.Fatalf("CleanupWorkspace: %v", err)
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatalf("session home %q survived teardown", home)
	}
}
