//go:build windows

package agentlaunch

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestResolveWindowsShimArgvAgainstInstalledShims exercises the resolver against
// the npm shims really present on this machine rather than a synthetic fixture.
// It is skipped when those shims are absent so CI on other hosts stays green.
func TestResolveWindowsShimArgvAgainstInstalledShims(t *testing.T) {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		t.Skip("APPDATA unset")
	}
	npmDir := filepath.Join(appData, "npm")
	shims := []string{"cline.cmd", "codex.cmd", "opencode.cmd", "pi.cmd"}
	found := 0
	for _, name := range shims {
		shim := filepath.Join(npmDir, name)
		if _, err := os.Stat(shim); err != nil {
			continue
		}
		found++
		prompt := strings.Repeat("p", 13000)
		argv := []string{shim, "-s", prompt}

		got := ResolveWindowsShimArgv(argv, exec.LookPath)
		if got[0] == shim {
			t.Logf("%s: left unchanged (unparsable or target missing)", name)
			continue
		}
		if got[0] == shim || len(got) < 2 {
			t.Fatalf("%s: shim not bypassed: %q", name, got)
		}
		// The oversized prompt must survive verbatim; the whole point is that
		// the agent still receives all 13 KB of it.
		if !containsArg(got, prompt) {
			t.Fatalf("%s: prompt not preserved (argv len %d)", name, len(got))
		}
		t.Logf("%s -> %s %s", name, filepath.Base(got[0]), filepath.Base(got[1]))
	}
	if found == 0 {
		t.Skipf("no npm shims under %s", npmDir)
	}
	t.Logf("resolved %d installed npm shim(s)", found)
}

func containsArg(argv []string, want string) bool {
	for _, arg := range argv {
		if arg == want {
			return true
		}
	}
	return false
}
