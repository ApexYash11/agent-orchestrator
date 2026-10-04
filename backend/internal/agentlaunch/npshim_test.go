//go:build windows

package agentlaunch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeShim installs a batch file at <dir>/<name>.cmd and returns its path.
func writeShim(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write shim: %v", err)
	}
	return path
}

// nodeShimBody is the shape npm's cmd-shim writes for a Node-backed package.
const nodeShimBody = "@ECHO off\n" +
	"GOTO start\n" +
	":find_dp0\n" +
	"SET dp0=%~dp0\n" +
	"EXIT /b\n" +
	":start\n" +
	"SETLOCAL\n" +
	"CALL :find_dp0\n" +
	"\n" +
	"IF EXIST \"%dp0%\\node.exe\" (\n" +
	"  SET \"_prog=%dp0%\\node.exe\"\n" +
	") ELSE (\n" +
	"  SET \"_prog=node\"\n" +
	"  SET PATHEXT=%PATHEXT:;.JS;=;%\n" +
	")\n" +
	"\n" +
	"endLocal & goto #_undefined_# 2>NUL || title %COMSPEC% & \"%_prog%\" \"%dp0%\\node_modules\\pkg\\bin\\entry\" %*\n"

func TestResolveWindowsShimArgvStartsNodeEntryPointDirectly(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "node_modules", "pkg", "bin")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(pkgDir, "entry")
	if err := os.WriteFile(entry, []byte("#!/usr/bin/env node\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	node := filepath.Join(dir, "node.exe")
	if err := os.WriteFile(node, []byte("MZ"), 0o755); err != nil {
		t.Fatal(err)
	}
	shim := writeShim(t, dir, "pkg.cmd", nodeShimBody)

	got := ResolveWindowsShimArgv([]string{shim, "-s", "prompt"}, nil)
	want := []string{node, entry, "-s", "prompt"}
	if !equalArgs(got, want) {
		t.Fatalf("ResolveWindowsShimArgv() = %q, want %q", got, want)
	}
}

// The reported failure is a 9-13 KB prompt, which is what pushed the launch past
// cmd.exe's ceiling. Prove the rewrite keeps that argv intact rather than
// truncating or dropping the prompt (issue #6207).
func TestResolveWindowsShimArgvPreservesOversizedPrompt(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "node_modules", "pkg", "bin")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(pkgDir, "entry")
	if err := os.WriteFile(entry, []byte("#!/usr/bin/env node\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	node := filepath.Join(dir, "node.exe")
	if err := os.WriteFile(node, []byte("MZ"), 0o755); err != nil {
		t.Fatal(err)
	}
	shim := writeShim(t, dir, "pkg.cmd", nodeShimBody)

	prompt := strings.Repeat("x", 13000)
	got := ResolveWindowsShimArgv([]string{shim, "-s", prompt, "--model", "m"}, nil)
	if len(got) != 6 {
		t.Fatalf("argv length = %d, want 6 (%q)", len(got), got)
	}
	if got[0] != node {
		t.Fatalf("argv[0] = %q, want %q", got[0], node)
	}
	if got[1] != entry {
		t.Fatalf("argv[1] = %q, want %q", got[1], entry)
	}
	if got[2] != "-s" || got[3] != prompt {
		t.Fatalf("prompt argument not preserved verbatim: len=%d", len(got[3]))
	}
	if got[4] != "--model" || got[5] != "m" {
		t.Fatalf("trailing arguments reordered: %q", got[4:])
	}
}

// npm packages that ship a native payload invoke it directly rather than
// through Node. opencode does this, and resolving it to Node would break it.
func TestResolveWindowsShimArgvKeepsNativePayload(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "node_modules", "pkg", "bin")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	native := filepath.Join(pkgDir, "opencode.exe")
	if err := os.WriteFile(native, []byte("MZ"), 0o755); err != nil {
		t.Fatal(err)
	}
	shim := writeShim(t, dir, "opencode.cmd",
		"@ECHO off\r\n\"%dp0%\\node_modules\\pkg\\bin\\opencode.exe\"   %*\r\n")

	got := ResolveWindowsShimArgv([]string{shim, "--model", "x"}, nil)
	want := []string{native, "--model", "x"}
	if !equalArgs(got, want) {
		t.Fatalf("ResolveWindowsShimArgv() = %q, want %q", got, want)
	}
}

func TestResolveWindowsShimArgvResolvesNodeFromPath(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "node_modules", "pkg", "bin")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(pkgDir, "entry")
	if err := os.WriteFile(entry, []byte("#!/usr/bin/env node\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	shim := writeShim(t, dir, "pkg.cmd", nodeShimBody)

	nodeDir := t.TempDir()
	node := filepath.Join(nodeDir, "node.exe")
	if err := os.WriteFile(node, []byte("MZ"), 0o755); err != nil {
		t.Fatal(err)
	}
	lookPath := func(string) (string, error) { return node, nil }

	got := ResolveWindowsShimArgv([]string{shim, "-p"}, lookPath)
	if !equalArgs(got, []string{node, entry, "-p"}) {
		t.Fatalf("ResolveWindowsShimArgv() = %q, want node entry argv", got)
	}
}

func TestResolveWindowsShimArgvLeavesUnrelatedArgvAlone(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "claude.exe")
	if err := os.WriteFile(exe, []byte("MZ"), 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		argv []string
	}{
		{"native executable", []string{exe, "-s", strings.Repeat("x", 13000)}},
		{"extensionless path", []string{filepath.Join(dir, "agent"), "-s", "x"}},
		{"empty", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveWindowsShimArgv(tc.argv, func(string) (string, error) {
				t.Fatal("lookPath must not be consulted for a non-shim argv")
				return "", nil
			})
			if !equalArgs(got, tc.argv) {
				t.Fatalf("argv changed: got %q, want %q", got, tc.argv)
			}
		})
	}
}

func TestResolveWindowsShimArgvSkipsEnvAssignmentPrefix(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "node_modules", "pkg", "bin")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(pkgDir, "entry")
	if err := os.WriteFile(entry, []byte("#!/usr/bin/env node\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	node := filepath.Join(dir, "node.exe")
	if err := os.WriteFile(node, []byte("MZ"), 0o755); err != nil {
		t.Fatal(err)
	}
	shim := writeShim(t, dir, "pkg.cmd", nodeShimBody)

	// An adapter that prefixes `env KEY=value` must keep the assignment.
	got := ResolveWindowsShimArgv([]string{"env", "OPENCODE_CONFIG=/tmp/x", shim, "-s", "p"}, nil)
	want := []string{"env", "OPENCODE_CONFIG=/tmp/x", node, entry, "-s", "p"}
	if !equalArgs(got, want) {
		t.Fatalf("ResolveWindowsShimArgv() = %q, want %q", got, want)
	}
}

func TestResolveWindowsShimArgvIgnoresUnparsableShim(t *testing.T) {
	dir := t.TempDir()
	shim := writeShim(t, dir, "pkg.cmd", "@ECHO off\r\nREM nothing here\r\n")
	argv := []string{shim, "-s", "x"}

	got := ResolveWindowsShimArgv(argv, nil)
	if !equalArgs(got, argv) {
		t.Fatalf("argv changed for unparsable shim: %q", got)
	}
}

func TestResolveWindowsShimArgvIgnoresMissingTarget(t *testing.T) {
	dir := t.TempDir()
	shim := writeShim(t, dir, "pkg.cmd", nodeShimBody)
	argv := []string{shim, "-s", "x"}

	got := ResolveWindowsShimArgv(argv, nil)
	if !equalArgs(got, argv) {
		t.Fatalf("argv changed when the entry point is absent: %q", got)
	}
}

func equalArgs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}