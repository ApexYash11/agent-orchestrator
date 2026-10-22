package commandcode

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// isolatedHome points os.UserHomeDir at a fresh directory, so a test never
// composes the developer's own ~/.commandcode memory or git configuration into
// its expectations.
func isolatedHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

// The user tier is used instead of the project tier precisely because it is
// additive: a repository that already carries its own AGENTS.md must keep it,
// and AO's instructions must load alongside it rather than being shadowed.
func TestPrepareSessionHomeWritesStandingInstructionsToTheUserTier(t *testing.T) {
	isolatedHome(t)
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
	isolatedHome(t)
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
	isolatedHome(t)
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

// The user tier is where Command Code reads memory, so redirecting HOME must not
// hide the user's own global instructions: the session file carries both, with
// the user's memory first and AO's role last.
func TestPrepareSessionHomeKeepsTheUsersOwnMemory(t *testing.T) {
	userHome := isolatedHome(t)
	realProfile := filepath.Join(userHome, ".commandcode")
	if err := os.MkdirAll(realProfile, 0o700); err != nil {
		t.Fatal(err)
	}
	userMemory := "# My preferences\n\nAlways run gofmt.\n"
	if err := os.WriteFile(filepath.Join(realProfile, "AGENTS.md"), []byte(userMemory), 0o600); err != nil {
		t.Fatal(err)
	}

	dataDir := t.TempDir()
	home, err := PrepareSessionHome(dataDir, domain.SessionID("mer-1"), "You are an AO worker.")
	if err != nil {
		t.Fatalf("PrepareSessionHome: %v", err)
	}
	contents, err := os.ReadFile(filepath.Join(home, ".commandcode", "AGENTS.md"))
	if err != nil {
		t.Fatalf("read memory: %v", err)
	}
	want := userMemory + "\nYou are an AO worker.\n"
	if string(contents) != want {
		t.Fatalf("memory = %q, want the user's memory followed by AO's role", contents)
	}
	original, err := os.ReadFile(filepath.Join(realProfile, "AGENTS.md"))
	if err != nil || string(original) != userMemory {
		t.Fatalf("real user memory = %q (%v), want it untouched", original, err)
	}

	// Restoring re-prepares from the real file, so the user's text is composed
	// once rather than folded into the session copy again.
	if _, err := PrepareSessionHome(dataDir, domain.SessionID("mer-1"), "You are an AO worker."); err != nil {
		t.Fatalf("restore: %v", err)
	}
	restored, err := os.ReadFile(filepath.Join(home, ".commandcode", "AGENTS.md"))
	if err != nil {
		t.Fatalf("read restored memory: %v", err)
	}
	if string(restored) != want {
		t.Fatalf("restored memory = %q, want it composed once", restored)
	}
	if count := strings.Count(string(restored), "Always run gofmt."); count != 1 {
		t.Fatalf("user memory appears %d times after restore", count)
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

// The session home follows prompts/ retention: it is never removed on spawn or
// restore failures, and no workspace-cleanup hook deletes it, because every
// one of those paths can precede a retry that needs the native history. Only
// the disposable prompts/<id> tree is wiped there, and the home deliberately
// lives outside it.
func TestSessionHomeSurvivesOutsideThePromptsDirectory(t *testing.T) {
	home := SessionHomeDir(filepath.Join("data"), domain.SessionID("mer-1"))
	if strings.Contains(filepath.ToSlash(home), "prompts/") {
		t.Fatalf("home = %q, want a location cleanupSystemPromptDir never touches", home)
	}
}

// settings.json and mcp.json are separate supported inputs from config.json --
// user deny rules, hooks and MCP servers -- so they must ride along into the
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
// developer tooling has to be reachable without copying secrets: gh through its
// config directory, and git through native discovery (the session home includes
// the real ~/.gitconfig rather than repointing GIT_CONFIG_GLOBAL, which would
// hide git's XDG file). Each is set only when its target exists.
func TestSessionProcessEnvPreservesRealDeveloperTooling(t *testing.T) {
	dataDir := t.TempDir()
	userHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(userHome, ".gitconfig"), []byte("[user]\n\tname = dev\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(userHome, ".config", "gh"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", userHome)
	t.Setenv("USERPROFILE", userHome)
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")

	env := SessionProcessEnv(dataDir, domain.SessionID("mer-1"), nil)
	if value, ok := env["GIT_CONFIG_GLOBAL"]; ok {
		t.Fatalf("GIT_CONFIG_GLOBAL = %q, want git's native discovery left intact", value)
	}
	if env["GH_CONFIG_DIR"] != filepath.Join(userHome, ".config", "gh") {
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
	userHome := t.TempDir()
	t.Setenv("HOME", userHome)
	t.Setenv("USERPROFILE", userHome)
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")

	env := SessionProcessEnv(dataDir, domain.SessionID("mer-1"), nil)
	if _, ok := env["GIT_CONFIG_GLOBAL"]; ok {
		t.Fatalf("GIT_CONFIG_GLOBAL = %q with no real .gitconfig", env["GIT_CONFIG_GLOBAL"])
	}
	if _, ok := env["GH_CONFIG_DIR"]; ok {
		t.Fatalf("GH_CONFIG_DIR = %q with no real gh config", env["GH_CONFIG_DIR"])
	}
}

// SSH has no environment redirect, so the session home links the real ~/.ssh
// rather than copying it: a copied key would silently go stale on rotation,
// and private keys must never be duplicated by an agent launcher. Linking needs
// symlink privilege, which stock Windows lacks, so there the session proceeds
// without it -- exactly as before this change.
func TestPrepareSessionHomeLinksRealSSHDirectory(t *testing.T) {
	dataDir := t.TempDir()
	source := t.TempDir()
	sshDir := filepath.Join(source, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte("Host example\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", source)
	t.Setenv("USERPROFILE", source)

	home, err := PrepareSessionHome(dataDir, domain.SessionID("mer-1"), "worker role")
	if err != nil {
		t.Fatalf("PrepareSessionHome: %v", err)
	}
	link := filepath.Join(home, ".ssh")
	if runtime.GOOS == "windows" {
		if _, err := os.Lstat(link); os.IsNotExist(err) {
			t.Skip("no symlink privilege: session proceeds without the ssh link")
		}
	}
	contents, err := os.ReadFile(filepath.Join(link, "config"))
	if err != nil {
		t.Fatalf("read through ssh link: %v", err)
	}
	if string(contents) != "Host example\n" {
		t.Fatalf("ssh config through link = %q", contents)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link = %v, want a symlink so key rotation stays live", link)
	}
}

// Explicitly configured values always win over the passthrough: a caller that
// already set GH_CONFIG_DIR keeps its account, including through different
// key casing on Windows. Git is never repointed at all, so an explicit
// GIT_CONFIG_GLOBAL is likewise left exactly as the caller supplied it.
func TestSessionProcessEnvRespectsExplicitConfiguration(t *testing.T) {
	dataDir := t.TempDir()
	userHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(userHome, ".config", "gh"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", userHome)
	t.Setenv("USERPROFILE", userHome)
	t.Setenv("APPDATA", t.TempDir())

	base := map[string]string{"GH_CONFIG_DIR": "/work/gh-profile", "git_config_global": "/work/gitconfig"}
	env := SessionProcessEnv(dataDir, domain.SessionID("mer-1"), base)
	// The overlay omits keys the caller already defined, so merging it over the
	// base can never overwrite an explicit value -- in either key case.
	if _, ok := env["GH_CONFIG_DIR"]; ok {
		t.Fatalf("GH_CONFIG_DIR emitted despite explicit configuration")
	}
	if _, ok := env["GIT_CONFIG_GLOBAL"]; ok {
		t.Fatalf("GIT_CONFIG_GLOBAL emitted despite explicit configuration")
	}
}

// Native lookup order wins over our defaults: with $XDG_CONFIG_HOME set, gh
// already resolves its config to $XDG/gh, and git reads the real XDG file
// directly, so pointing either at a different path would actively downgrade
// those users. Git especially must not get GIT_CONFIG_GLOBAL here: that would
// suppress the very XDG file the user relies on.
func TestSessionProcessEnvDefersToNativeXDGLayout(t *testing.T) {
	dataDir := t.TempDir()
	userHome := t.TempDir()
	xdg := t.TempDir()
	if err := os.WriteFile(filepath.Join(userHome, ".gitconfig"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(userHome, ".config", "gh"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", userHome)
	t.Setenv("USERPROFILE", userHome)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("APPDATA", t.TempDir())
	defer t.Setenv("XDG_CONFIG_HOME", "")

	env := SessionProcessEnv(dataDir, domain.SessionID("mer-1"), nil)
	if value, ok := env["GIT_CONFIG_GLOBAL"]; ok {
		t.Fatalf("GIT_CONFIG_GLOBAL = %q, want the XDG file left discoverable", value)
	}
	if _, ok := env["GH_CONFIG_DIR"]; ok {
		t.Fatalf("GH_CONFIG_DIR = %q, want native XDG lookup left alone", env["GH_CONFIG_DIR"])
	}
	sessionHome := filepath.Join(dataDir, "harnesses", "command-code", "mer-1")
	if env["HOME"] != sessionHome || env["USERPROFILE"] != sessionHome {
		t.Fatalf("home keys must still point at the session home")
	}
}

// Home-relative asset paths in mirrored settings must keep working: a skill
// entry or hook command that says ~/... means the real home, and inside the
// isolated home that directory does not exist.
func TestPrepareSessionHomeRebasesSettingsHomeReferences(t *testing.T) {
	dataDir := t.TempDir()
	source := t.TempDir()
	profile := filepath.Join(source, ".commandcode")
	if err := os.MkdirAll(profile, 0o700); err != nil {
		t.Fatal(err)
	}
	settings := `{"skills": ["~/.commandcode/custom-skills", "/opt/shared"], "mods": {"paths": ["~/mods", "/opt/m"]}, "hooks": {"PreToolUse": [{"hooks": [{"type": "command", "command": "bash \"$HOME/.commandcode/hooks/guard.sh\""}]}]}, "permissions": {"deny": ["Bash(rm -rf ~/*)"]}}`
	if err := os.WriteFile(filepath.Join(profile, "settings.json"), []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", source)
	t.Setenv("USERPROFILE", source)

	home, err := PrepareSessionHome(dataDir, domain.SessionID("mer-1"), "worker role")
	if err != nil {
		t.Fatalf("PrepareSessionHome: %v", err)
	}
	contents, err := os.ReadFile(filepath.Join(home, ".commandcode", "settings.json"))
	if err != nil {
		t.Fatalf("read mirrored settings: %v", err)
	}
	// Compare parsed values: JSON encoding doubles Windows backslashes, so
	// raw text matching would never agree with filepath.Join.
	var mirrored struct {
		Skills []string `json:"skills"`
		Mods   struct {
			Paths []string `json:"paths"`
		} `json:"mods"`
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
		Permissions struct {
			Deny []string `json:"deny"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(contents, &mirrored); err != nil {
		t.Fatalf("parse mirrored settings: %v\n%s", err, contents)
	}
	if len(mirrored.Skills) != 2 ||
		mirrored.Skills[0] != filepath.Join(source, ".commandcode", "custom-skills") ||
		mirrored.Skills[1] != "/opt/shared" {
		t.Fatalf("skills = %q, want rebased home path plus untouched absolute path", mirrored.Skills)
	}
	if len(mirrored.Mods.Paths) != 2 ||
		mirrored.Mods.Paths[0] != filepath.Join(source, "mods") ||
		mirrored.Mods.Paths[1] != "/opt/m" {
		t.Fatalf("mods.paths = %q, want rebased home path plus untouched absolute path", mirrored.Mods.Paths)
	}
	var command string
	for _, groups := range mirrored.Hooks {
		for _, entry := range groups {
			for _, handler := range entry.Hooks {
				command = handler.Command
			}
		}
	}
	// Windows accepts mixed separators, and the rebased prefix keeps the OS-native
	// real-home spelling, so compare separator-blind.
	if filepath.ToSlash(command) != "bash \""+filepath.ToSlash(filepath.Join(source, ".commandcode", "hooks", "guard.sh"))+"\"" {
		t.Fatalf("hook command = %q, want rebased to the real home", command)
	}
	if len(mirrored.Permissions.Deny) != 1 || mirrored.Permissions.Deny[0] != "Bash(rm -rf ~/*)" {
		t.Fatalf("deny = %q, want the pattern untouched", mirrored.Permissions.Deny)
	}
}

// The overlay is not the whole environment: the daemon's ambient environment
// joins in on every launch path. An inherited git or gh pointer must survive
// our defaults, and a project-configured XDG must count for the gh decision.
func TestSessionProcessEnvRespectsInheritedTooling(t *testing.T) {
	dataDir := t.TempDir()
	// When the caller or the inherited environment already carries a pointer,
	// the overlay must omit the key entirely so that value flows through.
	env := SessionProcessEnv(dataDir, domain.SessionID("mer-1"), map[string]string{
		"GH_CONFIG_DIR":     "/work/gh-profile",
		"GIT_CONFIG_GLOBAL": "/work/git-profile",
	})
	if _, ok := env["GH_CONFIG_DIR"]; ok {
		t.Fatalf("GH_CONFIG_DIR emitted although the caller supplies it")
	}
	if _, ok := env["GIT_CONFIG_GLOBAL"]; ok {
		t.Fatalf("GIT_CONFIG_GLOBAL emitted although the caller supplies it")
	}
	sessionHome := filepath.Join(dataDir, "harnesses", "command-code", "mer-1")
	if env["HOME"] != sessionHome {
		t.Fatalf("HOME = %q, want the session home", env["HOME"])
	}

	// The same holds for values inherited by the daemon rather than passed per
	// call: both defaults exist in the fake real home, so without the ambient
	// values below both defaults would have been emitted.
	userHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(userHome, ".gitconfig"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(userHome, ".config", "gh"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", userHome)
	t.Setenv("USERPROFILE", userHome)
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("GIT_CONFIG_GLOBAL", "/work/git-profile")
	t.Setenv("GH_CONFIG_DIR", "/work/gh-profile")
	env = SessionProcessEnv(dataDir, domain.SessionID("mer-1"), nil)
	if _, ok := env["GIT_CONFIG_GLOBAL"]; ok {
		t.Fatalf("GIT_CONFIG_GLOBAL emitted despite the inherited pointer")
	}
	if _, ok := env["GH_CONFIG_DIR"]; ok {
		t.Fatalf("GH_CONFIG_DIR emitted despite the inherited pointer")
	}

	// A project-configured XDG counts for gh, which reads $XDG/gh directly,
	// while git gets no pointer at all: its session-home include keeps the real
	// ~/.gitconfig applying next to that XDG file.
	xdg := t.TempDir()
	defer t.Setenv("XDG_CONFIG_HOME", "")
	env = SessionProcessEnv(dataDir, domain.SessionID("mer-1"), map[string]string{"XDG_CONFIG_HOME": xdg})
	if _, ok := env["GIT_CONFIG_GLOBAL"]; ok {
		t.Fatalf("GIT_CONFIG_GLOBAL emitted although git needs no pointer")
	}
	if _, ok := env["GH_CONFIG_DIR"]; ok {
		t.Fatalf("GH_CONFIG_DIR emitted while the session sets XDG_CONFIG_HOME")
	}
}

// The user's mods live in $HOME/.commandcode/mods at runtime: drop-ins and
// npm-installed packages under .store alike. Mirroring the real directory
// keeps the user's mods loading once HOME is redirected.
func TestPrepareSessionHomeMirrorsUserMods(t *testing.T) {
	dataDir := t.TempDir()
	source := t.TempDir()
	mods := filepath.Join(source, ".commandcode", "mods", "weather-mod")
	if err := os.MkdirAll(mods, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mods, "index.js"), []byte("export const name = 'weather';\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(source, ".commandcode", "mods", ".store", "pkg"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, ".commandcode", "mods", ".store", "pkg", "index.js"), []byte("// installed package\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", source)
	t.Setenv("USERPROFILE", source)

	home, err := PrepareSessionHome(dataDir, domain.SessionID("mer-1"), "worker role")
	if err != nil {
		t.Fatalf("PrepareSessionHome: %v", err)
	}
	mirrored, err := os.ReadFile(filepath.Join(home, ".commandcode", "mods", "weather-mod", "index.js"))
	if err != nil || string(mirrored) != "export const name = 'weather';\n" {
		t.Fatalf("mirrored drop-in: v = %q, err = %v", mirrored, err)
	}
	installed, err := os.ReadFile(filepath.Join(home, ".commandcode", "mods", ".store", "pkg", "index.js"))
	if err != nil || string(installed) != "// installed package\n" {
		t.Fatalf("mirrored .store package: v = %q, err = %v", installed, err)
	}

	// A later change to the real mods tree, including `mods update` run
	// inside an earlier session, must land on the next launch refresh.
	if err := os.WriteFile(filepath.Join(mods, "index.js"), []byte("export const name = 'weather-2';\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareSessionHome(dataDir, domain.SessionID("mer-1"), "worker role"); err != nil {
		t.Fatalf("re-PrepareSessionHome: %v", err)
	}
	refreshed, err := os.ReadFile(filepath.Join(home, ".commandcode", "mods", "weather-mod", "index.js"))
	if err != nil || string(refreshed) != "export const name = 'weather-2';\n" {
		t.Fatalf("refreshed drop-in: v = %q, err = %v", refreshed, err)
	}

	// Nothing session-side is ever deleted: a file the session itself wrote
	// into its own mods tree survives the refresh.
	sessionSide := filepath.Join(home, ".commandcode", "mods", "session-out.log")
	if err := os.WriteFile(sessionSide, []byte("log"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareSessionHome(dataDir, domain.SessionID("mer-1"), "worker role"); err != nil {
		t.Fatalf("re-PrepareSessionHome: %v", err)
	}
	if _, err := os.Stat(sessionSide); err != nil {
		t.Fatalf("session-side mod file removed by the refresh: %v", err)
	}
}

// When no XDG layer is set, git computes its second user config path from
// HOME, so the real ~/.config/git disappears behind the redirect unless the
// real one is mirrored. With XDG set the real path stays directly readable
// and mirroring would be redundant.
func TestPrepareSessionHomeMirrorsUserGitConfig(t *testing.T) {
	dataDir := t.TempDir()
	source := t.TempDir()
	if err := os.MkdirAll(filepath.Join(source, ".config", "git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, ".config", "git", "config"), []byte("[user]\n\tname = dev\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", source)
	t.Setenv("USERPROFILE", source)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("APPDATA", t.TempDir())

	home, err := PrepareSessionHome(dataDir, domain.SessionID("mer-1"), "worker role")
	if err != nil {
		t.Fatalf("PrepareSessionHome: %v", err)
	}
	copied, err := os.ReadFile(filepath.Join(home, ".config", "git", "config"))
	if err != nil || string(copied) != "[user]\n\tname = dev\n" {
		t.Fatalf("mirrored git config: v = %q, err = %v", copied, err)
	}

	xdg := t.TempDir()
	if err := os.MkdirAll(filepath.Join(xdg, "git"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", xdg)

	if _, err := PrepareSessionHome(dataDir, domain.SessionID("edge-2"), "other role"); err != nil {
		t.Fatalf("PrepareSessionHome with XDG set: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "harnesses", "command-code", "edge-2", ".config", "git")); err == nil {
		t.Fatalf("git config mirrored although XDG already redirects the real path")
	}
}

// Both of git's global files must keep applying inside the session, in their
// native order, whichever way XDG is configured. The check runs real git
// against the generated home rather than asserting on the environment or the
// copy: GIT_CONFIG_GLOBAL used to make git skip the XDG file, and only the
// effective values catch that.
func TestPrepareSessionHomePreservesBothGlobalGitSources(t *testing.T) {
	for _, test := range []struct {
		name string
		xdg  bool
	}{
		{name: "xdg unset", xdg: false},
		{name: "xdg set", xdg: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			realHome := isolatedHome(t)
			if err := os.WriteFile(filepath.Join(realHome, ".gitconfig"), []byte("[user]\n\tname = Real Name\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			xdg := ""
			second := filepath.Join(realHome, ".config", "git", "config")
			if test.xdg {
				xdg = t.TempDir()
				second = filepath.Join(xdg, "git", "config")
			}
			if err := os.MkdirAll(filepath.Dir(second), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(second, []byte("[user]\n\temail = real@example.com\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("XDG_CONFIG_HOME", xdg)
			t.Setenv("APPDATA", t.TempDir())

			dataDir := t.TempDir()
			home, err := PrepareSessionHome(dataDir, domain.SessionID("mer-1"), "worker role")
			if err != nil {
				t.Fatalf("PrepareSessionHome: %v", err)
			}
			if value, ok := SessionProcessEnv(dataDir, domain.SessionID("mer-1"), nil)["GIT_CONFIG_GLOBAL"]; ok {
				t.Fatalf("GIT_CONFIG_GLOBAL = %q, want native discovery", value)
			}

			// A restore rewrites the include rather than stacking another one.
			if _, err := PrepareSessionHome(dataDir, domain.SessionID("mer-1"), "worker role"); err != nil {
				t.Fatalf("restore: %v", err)
			}
			sessionConfig, err := os.ReadFile(filepath.Join(home, ".gitconfig"))
			if err != nil {
				t.Fatalf("read session gitconfig: %v", err)
			}
			if count := strings.Count(string(sessionConfig), sessionGitConfigIncludeBegin); count != 1 {
				t.Fatalf("include block appears %d times: %q", count, sessionConfig)
			}

			values := effectiveGitConfig(t, home, xdg)
			if values["user.name"] != "Real Name" {
				t.Fatalf("user.name = %q, want the ~/.gitconfig value", values["user.name"])
			}
			if values["user.email"] != "real@example.com" {
				t.Fatalf("user.email = %q, want the second global file's value", values["user.email"])
			}

			original, err := os.ReadFile(filepath.Join(realHome, ".gitconfig"))
			if err != nil || string(original) != "[user]\n\tname = Real Name\n" {
				t.Fatalf("real ~/.gitconfig = %q (%v), want it untouched", original, err)
			}
		})
	}
}

// effectiveGitConfig reports what git resolves for the session home, using the
// same environment the child would receive plus a clean config environment so
// the developer's own machine cannot answer the question.
func effectiveGitConfig(t *testing.T, home, xdg string) map[string]string {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is unavailable")
	}
	work := t.TempDir()
	cmd := exec.Command(git, "config", "--list")
	cmd.Dir = work
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(key) {
		case "HOME", "USERPROFILE", "XDG_CONFIG_HOME", "APPDATA", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM", "GIT_CONFIG_NOSYSTEM", "GIT_CONFIG":
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env, "HOME="+home, "USERPROFILE="+home, "GIT_CONFIG_NOSYSTEM=1")
	if xdg != "" {
		cmd.Env = append(cmd.Env, "XDG_CONFIG_HOME="+xdg)
	}
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("git config --list: %v", err)
	}
	values := make(map[string]string)
	for _, line := range strings.Split(string(output), "\n") {
		if key, value, ok := strings.Cut(line, "="); ok {
			values[key] = value
		}
	}
	return values
}

// A layered config must survive the redirect too: git expands a `~/` include
// path against HOME rather than against the file holding the directive, so
// including the real ~/.gitconfig alone would silently drop everything it
// chains to -- including a split user.name / user.email identity.
func TestPrepareSessionHomeRebasesNestedGitIncludes(t *testing.T) {
	realHome := isolatedHome(t)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("APPDATA", t.TempDir())

	originals := map[string]string{
		".gitconfig": "[user]\n\tname = Real Name\n[include]\n\tpath = ~/.gitconfig-work\n" +
			"[includeIf \"gitdir:/nonexistent/\"]\n\tpath = ~/\"space dir\"/work.gitconfig\n",
		".gitconfig-work":          "[user]\n\temail = work@example.com\n[include]\n\tpath = dotfiles/extra.gitconfig\n",
		"dotfiles/extra.gitconfig": "[core]\n\tpager = cat\n",
		"space dir/work.gitconfig": "[commit]\n\tgpgsign = true\n",
	}
	for name, contents := range originals {
		path := filepath.Join(realHome, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	dataDir := t.TempDir()
	home, err := PrepareSessionHome(dataDir, domain.SessionID("mer-1"), "worker role")
	if err != nil {
		t.Fatalf("PrepareSessionHome: %v", err)
	}

	values := effectiveGitConfig(t, home, "")
	for key, want := range map[string]string{
		"user.name":  "Real Name",
		"user.email": "work@example.com",
		"core.pager": "cat",
	} {
		if values[key] != want {
			t.Fatalf("%s = %q, want %q (nested include lost)", key, values[key], want)
		}
	}

	for name, contents := range originals {
		got, err := os.ReadFile(filepath.Join(realHome, filepath.FromSlash(name)))
		if err != nil || string(got) != contents {
			t.Fatalf("original %s = %q (%v), want it untouched", name, got, err)
		}
	}

	// Every session-owned copy is anchored to the real filesystem, so nothing
	// still depends on HOME, and the session config points at the copies.
	copies, err := os.ReadDir(filepath.Join(home, sessionGitConfigCopiesDir))
	if err != nil || len(copies) == 0 {
		t.Fatalf("session git config copies = %v (%v), want the rewritten chain", copies, err)
	}
	for _, entry := range copies {
		contents, err := os.ReadFile(filepath.Join(home, sessionGitConfigCopiesDir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(contents), "~/") {
			t.Fatalf("copy %s still uses a home-relative include: %q", entry.Name(), contents)
		}
	}
	sessionConfig, err := os.ReadFile(filepath.Join(home, ".gitconfig"))
	if err != nil {
		t.Fatalf("read session gitconfig: %v", err)
	}
	if !strings.Contains(string(sessionConfig), sessionGitConfigCopiesDir) {
		t.Fatalf("session .gitconfig = %q, want it to include the rebased copy", sessionConfig)
	}
}

// Include values follow git's quoting and comment rules closely enough to
// rewrite real-world dotfiles: quoted paths stay whole, and an inline comment is
// not part of the path.
func TestGitIncludePathParsing(t *testing.T) {
	for _, test := range []struct {
		line string
		want string
		ok   bool
	}{
		{line: "\tpath = ~/.gitconfig-work", want: "~/.gitconfig-work", ok: true},
		{line: "path = ~/\"space dir\"/work.gitconfig", want: "~/space dir/work.gitconfig", ok: true},
		{line: "path = /abs/gitconfig # trailing comment", want: "/abs/gitconfig", ok: true},
		{line: "path = ~/.x ; comment", want: "~/.x", ok: true},
		{line: "  # path = ~/ignored"},
		{line: "value = ~/not-a-path"},
		{line: "path ="},
	} {
		got, ok := gitIncludePathValue(test.line)
		if got != test.want || ok != test.ok {
			t.Fatalf("%q -> (%q, %v), want (%q, %v)", test.line, got, ok, test.want, test.ok)
		}
	}
	for _, test := range []struct {
		line string
		want string
	}{
		{line: "[include]", want: "include"},
		{line: "[includeIf \"gitdir:~/work/\"]", want: "includeif"},
		{line: "  [core] # comment", want: "core"},
		{line: "# [user]", want: ""},
	} {
		if got := gitSectionName(test.line); got != test.want {
			t.Fatalf("gitSectionName(%q) = %q, want %q", test.line, got, test.want)
		}
	}
}

// The hook rewriter must preserve shell semantics rather than substitute
// blindly: $HOMEBREW-style identifiers sharing the $HOME prefix stay
// untouched, single-quoted references are deliberately literal, and a home
// containing spaces must come out as one word again.
func TestRebaseHookCommandPreservesShellSemantics(t *testing.T) {
	spaced := "Users/Jane Doe"
	unspaced := `Users/flat`

	cases := []struct {
		name    string
		command string
		home    string
		want    string
	}{
		{
			name:    "shared prefix identifiers are left alone",
			command: `"$HOMEBREW_PREFIX/bin/check"`,
			home:    spaced,
			want:    `"$HOMEBREW_PREFIX/bin/check"`,
		},
		{
			name:    "unquoted tilde with spaced home stays one word",
			command: "bash ~/.commandcode/hooks/guard.sh",
			home:    spaced,
			want:    `bash "Users/Jane Doe"/.commandcode/hooks/guard.sh`,
		},
		{
			name:    "unquoted tilde keeps a wildcard outside the inserted quotes",
			command: `cat ~/.commandcode/hooks/*.json`,
			home:    spaced,
			want:    `cat "Users/Jane Doe"/.commandcode/hooks/*.json`,
		},
		{
			name:    "dollar home keeps a wildcard outside the inserted quotes",
			command: `for f in $HOME/.commandcode/hooks/*.sh; do sh "$f"; done`,
			home:    spaced,
			want:    `for f in "Users/Jane Doe"/.commandcode/hooks/*.sh; do sh "$f"; done`,
		},
		{
			name:    "quoted dollar home inside double quotes",
			command: `bash "$HOME/g.sh"`,
			home:    spaced,
			want:    `bash "Users/Jane Doe/g.sh"`,
		},
		{
			name:    "single quotes are deliberately literal",
			command: `bash '$HOME/g.sh'`,
			home:    spaced,
			want:    `bash '$HOME/g.sh'`,
		},
		{
			name:    "plain dollar home needs no quotes when home is clean",
			command: "bash $HOME/x",
			home:    unspaced,
			want:    "bash Users/flat/x",
		},
		{
			name:    "dollar home becomes one quoted word when home has spaces",
			command: "cp $HOME/notes.txt .",
			home:    spaced,
			want:    `cp "Users/Jane Doe"/notes.txt .`,
		},
		{
			name:    "a quoted suffix keeps its own quoting",
			command: `bash ~/"a b"/x`,
			home:    spaced,
			want:    `bash "Users/Jane Doe"/"a b"/x`,
		},
		{
			name:    "bare tilde expands with quotes when needed",
			command: "cd ~",
			home:    spaced,
			want:    `cd "Users/Jane Doe"`,
		},
		{
			name:    "tilde inside double quotes is a literal",
			command: `echo "~/static"`,
			home:    spaced,
			want:    `echo "~/static"`,
		},
		{
			name:    "homework lowercase variables are untouched",
			command: "check $homework",
			home:    spaced,
			want:    "check $homework",
		},
	}
	for _, test := range cases {
		got, changed := rebaseHookCommand(test.command, test.home)
		if !changed {
			got = test.command
		}
		if got != test.want {
			t.Fatalf("%s: got %q, want %q", test.name, got, test.want)
		}
	}
}

// A rewritten hook keeps working in a real shell: a glob under a home whose
// path contains a space must still expand to the files it matched before.
func TestRebaseHookCommandKeepsGlobsExpandable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the shell glob check needs a POSIX sh with POSIX paths")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh is unavailable")
	}
	root := t.TempDir()
	home := filepath.Join(root, "Jane Doe")
	hooks := filepath.Join(home, ".commandcode", "hooks")
	if err := os.MkdirAll(hooks, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.json", "b.json"} {
		if err := os.WriteFile(filepath.Join(hooks, name), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	rewritten, changed := rebaseHookCommand(`printf '%s\n' ~/.commandcode/hooks/*.json`, home)
	if !changed {
		t.Fatal("the home-relative hook was not rewritten")
	}
	cmd := exec.Command(sh, "-c", rewritten)
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("run rewritten hook %q: %v", rewritten, err)
	}
	for _, name := range []string{"a.json", "b.json"} {
		want := filepath.Join(hooks, name)
		if !strings.Contains(string(output), want) {
			t.Fatalf("rewritten hook %q listed %q, want %q", rewritten, output, want)
		}
	}
}
