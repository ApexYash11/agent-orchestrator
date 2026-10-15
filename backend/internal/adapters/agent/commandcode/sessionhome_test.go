package commandcode

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
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
	if want := filepath.Join(dataDir, "prompts", "mer-1", "home"); home != want {
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
		if _, err := os.Stat(filepath.Join(dataDir, "prompts", "mer-empty")); !os.IsNotExist(err) {
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
	want := filepath.Join(dataDir, "prompts", "mer-1", "home")
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
