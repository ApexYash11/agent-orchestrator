package agentlaunch

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/process"
)

// ResolveWindowsShimArgv rewrites a launch argv whose program is a Windows npm
// command shim so the shim's own target is started directly.
//
// npm installs each CLI as a `<name>.cmd` batch file that re-launches the
// package's entry point. Windows cannot start a batch file itself: CreateProcess
// hands it to cmd.exe, whose `/c` input line is capped near 8,191 characters.
// AO's generated system prompt (commonly 9-13 KB) exceeds that on its own, so
// the agent was rejected before it read its first instruction (issue #6207).
//
// Starting the target directly uses CreateProcess, which allows 32,767
// characters, and drops cmd.exe out of the quoting path so prompts containing
// `<`, `>`, `&`, `|`, and newlines reach the agent verbatim.
//
// argv is returned unchanged when it is not a batch shim, when the shim cannot
// be parsed, or when its target or a Node runtime is missing. Native `.exe`
// harnesses therefore keep their existing launch path untouched.
func ResolveWindowsShimArgv(argv []string, lookPath func(string) (string, error)) []string {
	if runtime.GOOS != "windows" || len(argv) == 0 {
		return argv
	}
	index, ok := launchBinaryIndex(argv)
	if !ok || !process.IsWindowsBatchFile(argv[index]) {
		return argv
	}
	shim := argv[index]
	content, err := os.ReadFile(shim)
	if err != nil {
		return argv
	}
	program, ok := npmShimInvocation(filepath.Dir(shim), string(content))
	if !ok {
		return argv
	}

	executable, args := "", []string(nil)
	switch {
	case program.NodeScript != "":
		// Both halves must exist: a Node runtime that is missing would fall
		// back to PATH, but a shim whose entry point was removed or moved must
		// stay untouched so the original launch still reports its own failure.
		if !isRegularFile(program.NodeScript) {
			return argv
		}
		executable = resolveNodeRuntime(filepath.Dir(shim), lookPath)
		args = []string{program.NodeScript}
	case program.Executable != "":
		executable, args = program.Executable, program.Args
	}
	if executable == "" || !isRegularFile(executable) {
		return argv
	}

	rewritten := make([]string, 0, len(argv)+len(args))
	rewritten = append(rewritten, argv[:index]...)
	rewritten = append(rewritten, executable)
	rewritten = append(rewritten, args...)
	rewritten = append(rewritten, argv[index+1:]...)
	return rewritten
}

// launchBinaryIndex reports the argv slot holding the real executable, which is
// argv[0] unless an `env KEY=VALUE` prefix is present.
func launchBinaryIndex(argv []string) (int, bool) {
	if len(argv) == 0 {
		return 0, false
	}
	if filepath.Base(argv[0]) != "env" {
		return 0, true
	}
	for i, arg := range argv[1:] {
		if strings.Contains(arg, "=") {
			continue
		}
		return i + 1, true
	}
	return 0, false
}

// npmShimProgram is the real program an npm command shim ends up running.
type npmShimProgram struct {
	// NodeScript is the package entry point when the shim launches it through a
	// Node runtime. It is empty when the shim invokes a native program.
	NodeScript string
	// Executable is the native program the shim invokes. It is empty when
	// NodeScript is set.
	Executable string
	// Args are leading arguments the shim passes before the caller's own.
	Args []string
}

// npmShimInvocation parses an npm shim body and reports the program it runs.
// cmd-shim emits a single invocation line ending in `%*`; a package shipping a
// native payload invokes that binary instead, through a line of its own.
func npmShimInvocation(dir, content string) (npmShimProgram, bool) {
	var found npmShimProgram
	ok := false
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if !strings.Contains(line, "%*") {
			continue
		}
		tokens := quotedTokens(line)
		if len(tokens) == 0 {
			continue
		}
		program, parsed := npmShimProgramFromTokens(dir, tokens)
		if !parsed {
			continue
		}
		found, ok = program, true
	}
	return found, ok
}

func npmShimProgramFromTokens(dir string, tokens []string) (npmShimProgram, bool) {
	// `"%_prog%"` is cmd-shim's placeholder for the Node runtime it selected.
	if strings.Contains(tokens[0], "%_prog%") {
		if len(tokens) < 2 {
			return npmShimProgram{}, false
		}
		entry := expandShimPath(tokens[1], dir)
		if entry == "" {
			return npmShimProgram{}, false
		}
		return npmShimProgram{NodeScript: entry}, true
	}

	executable := expandShimPath(tokens[0], dir)
	if executable == "" {
		return npmShimProgram{}, false
	}
	args := make([]string, 0, len(tokens)-1)
	for _, token := range tokens[1:] {
		if expanded := expandShimPath(token, dir); expanded != "" {
			args = append(args, expanded)
		}
	}
	return npmShimProgram{Executable: executable, Args: args}, true
}

// quotedTokens splits a command line into its double-quoted tokens, ignoring
// unquoted words such as `%*`. Shim invocation lines do not escape embedded
// quotes, so a token ends at the next quote.
func quotedTokens(line string) []string {
	var tokens []string
	var current strings.Builder
	inQuotes := false
	started := false
	for i := 0; i < len(line); i++ {
		switch c := line[i]; {
		case c == '"':
			inQuotes = !inQuotes
			started = true
		case (c == ' ' || c == '\t') && !inQuotes:
			if started {
				tokens = append(tokens, current.String())
				current.Reset()
				started = false
			}
		default:
			current.WriteByte(c)
		}
	}
	if started {
		tokens = append(tokens, current.String())
	}
	return tokens
}

// expandShimPath substitutes the shim's own directory for `%dp0%` and returns a
// cleaned absolute path. A token still holding a shell variable yields "" so a
// caller never rewrites argv from a half-understood shim.
func expandShimPath(token, dir string) string {
	prefix := dir
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}
	expanded := strings.ReplaceAll(token, "%~dp0", prefix)
	expanded = strings.ReplaceAll(expanded, "%dp0%", prefix)
	if expanded == "" || strings.Contains(expanded, "%") {
		return ""
	}
	return filepath.Clean(expanded)
}

// resolveNodeRuntime prefers the Node runtime installed beside the shim, which
// is how nvm-style installs pin the interpreter, before falling back to PATH.
func resolveNodeRuntime(shimDir string, lookPath func(string) (string, error)) string {
	if local := filepath.Join(shimDir, "node.exe"); isRegularFile(local) {
		return local
	}
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	for _, name := range []string{"node.exe", "node"} {
		if path, err := lookPath(name); err == nil && isRegularFile(path) {
			return path
		}
	}
	return ""
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}