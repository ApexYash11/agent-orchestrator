# Command Code Adapter

Command Code is integrated into Agent Orchestrator as an interactive Terminal UI
harness. AO launches the user's own `cmd` binary inside the session worktree and
leaves the session open for further work.

## Install

Install Command Code with its upstream npm package, then make sure the `cmd`
binary is available on `PATH`:

```bash
npm install -g command-code
```

Verify the install:

```bash
cmd --version
```

AO's one-click installer uses the same `command-code` npm package. AO resolves
the binary from:

- `PATH`: `cmd`, then `command-code`
- `/usr/local/bin/cmd`, `/opt/homebrew/bin/cmd`
- `/usr/local/bin/command-code`, `/opt/homebrew/bin/command-code`
- Node-managed global bin directories
- `~/.commandcode/bin/cmd`
- `cmdc.cmd`, `cmdc.exe`, and `command-code.cmd` on Windows

On Windows the binary is `cmdc` or `command-code`: `cmd` is reserved for the
built-in command shell.

## Supported AO Mode

Command Code is exposed through AO's Terminal UI mode. A fresh session launches
as:

```bash
cmd --skip-onboarding --no-auto-update --trust [--model <model>] [--effort <level>]
```

AO then injects the worker's initial task through the terminal once the
interactive UI is ready. Command Code accepts a positional initial message
(`cmd "fix login redirect"`), but injecting after startup means a prompt that
begins with `-` is never parsed as a flag.

- `--skip-onboarding` skips taste onboarding for automated runs.
- `--no-auto-update` disables background self-updates for the AO-managed session.
- `--trust` skips the initial project-trust prompt, which would otherwise block
  every fresh AO worktree.
- `--model <model>` is forwarded from the agent config model field when set.
- `--effort <level>` is forwarded from the agent config effort field when set.
  `cmd --list-models` advertises no per-model effort levels, so AO forwards the
  raw level without catalog validation (empty keeps Command Code's default);
  unknown values are rejected by `cmd --effort` at launch.

Permission modes map as follows:

| AO permission mode | Command Code flags |
| --- | --- |
| default | none (uses Command Code's configured default) |
| accept-edits | `--permission-mode accept-edits` |
| auto | `--permission-mode accept-edits` |
| bypass-permissions | `--yolo` |

Command Code has no separate "auto" tier, so accept-edits and auto both use
`--permission-mode accept-edits`.

## Activity Tracking

AO merges workspace-local hooks into `.commandcode/settings.json` for
`SessionStart`, `PreToolUse`, `PostToolUse`, and `Stop`. These callbacks report
active/idle activity and persist Command Code's native `session_id`. AO marks
this coverage partial because Command Code does not expose a permission-request
hook, so it cannot reliably distinguish an approval prompt from other in-turn
work.

On `SessionStart`, the hook does not carry AO's standing instructions; see
[Standing Instructions](#standing-instructions) below.

## Standing Instructions

Command Code accepts no system-prompt launch flag, and the alternatives AO tried
are unavailable to Chat: `cmd acp` builds no mod host, so a mod's
`appendSystemPrompt` never runs; `session/new` ignores `_meta`; and `SessionStart`
is the one hook the ACP path never fires. The other three hooks do run under ACP,
but their `additionalContext` arrives as tool-result material that only appears
once a tool runs, and Command Code's own model discounts it rather than treating
it as a standing instruction.

Memory is the one surface Command Code reads in both modes, and its own
documentation places memory in the **system prompt** rather than the
conversation, so AO's instructions survive compaction.

Each session therefore gets its own Command Code home under
`~/.ao/harnesses/command-code/<session-id>/.commandcode/`, containing `AGENTS.md`
with the session's standing instructions. The home lives outside the disposable
`prompts/<session-id>` directory on purpose: a failed restore wipes that whole
tree, and the wipe must never take the native conversation with it. AO points
`HOME` and `USERPROFILE` at that directory for the session's process.

Two consequences follow from Command Code's tier rules:

- The **user** tier is used because it is additive. A repository's own
  `AGENTS.md` still loads alongside AO's; the project tier would silently shadow
  it, because only the first existing project-tier file is read.
- Each session has its own home, so concurrent sessions never share a memory file
  and each carries its own role (orchestrator or worker).

The session home mirrors the user's `auth.json`, `config.json`, `providers.json`,
`settings.json` and `mcp.json` so the session keeps its login, preferences,
permission rules, hooks and MCP servers; Command Code has no environment variable
for its OAuth token, so the token is copied at `0600` rather than referenced.
The user's own `~/.commandcode/AGENTS.md`, when present, is composed into the
session's memory ahead of AO's instructions instead of being replaced: the
redirect would otherwise hide the global instructions the user already relies
on. The original is only read, and the session file is rebuilt from it on every
launch, so a restore neither loses the user's memory nor duplicates it.
The user's mods directory (`~/.commandcode/mods`, including npm-installed
packages under `.store`) is mirrored recursively and refreshed on every launch,
because Command Code resolves mods from `HOME` -- without the copy every mod
the user has installed would vanish behind the redirect. `~/`-relative skill
and mod paths and `$HOME` references in hook commands are rebased to the real
home in the mirrored copy, since they would otherwise resolve inside the
isolated home where those assets do not exist; absolute paths, single-quoting
and permission rule text are left untouched. When the real home contains
whitespace, only the substituted home prefix is quoted and the rest of the path
stays outside the quotes, so a wildcard such as
`~/.commandcode/hooks/*.json` still expands. All AO-owned state stays under
`~/.ao`, and nothing outside it is written: the repository's `AGENTS.md` and the
user's real `.commandcode/AGENTS.md` are only read.

The redirected `HOME` is inherited by the agent's shell children, so the real
developer's tooling is kept reachable rather than moved. Git stays on its native
discovery: the session home's `~/.gitconfig` includes the real `~/.gitconfig`, so
a user who splits identity or credentials between `~/.gitconfig` and
`$XDG_CONFIG_HOME/git/config` keeps both, in the same order git resolves them
outside a session. When `$XDG_CONFIG_HOME` is unset, so that path would resolve
inside the session home, the real `~/.config/git` is mirrored there instead.
`GIT_CONFIG_GLOBAL` is deliberately not set in the ordinary case: it would
replace both native global files and hide the XDG half.

Git expands a `~/` include path and a `gitdir:~/` condition against `HOME`, not
against the file containing the directive, and resolves `./` bases against that
file's directory. A layered config (`~/.gitconfig` chaining to
`~/.gitconfig-work`, optionally further) would therefore lose everything it
references, and a relocated conditional include would silently stop matching.
Those files are rewritten into a session-owned copy under `~/.ao-gitconfig/`
with every include and `gitdir` condition anchored to its real absolute path,
and the copies are regenerated on each launch; the originals are only read. The
same treatment covers the XDG global file, whether git reads it natively or from
the mirrored `~/.config/git`. A config whose bases already resolve correctly
from where it lives is used in place. Backslash-continued values are parsed as
the single logical line git sees, so a rewritten include never leaves half a
path behind.

Each source is read exactly once. With an absolute `$XDG_CONFIG_HOME`, native
discovery would still read the real file while the session included its anchored
copy, applying the whole file twice; most settings tolerate that, but multivalued
ones do not (`http.extraHeader` would send two `Authorization` headers). For that
session only, the session home's `.gitconfig` -- which already includes both
sources in native order -- becomes the child's global file, and the mark is
dropped again on the first launch whose XDG chain needs no anchoring. That launch
also removes AO's include block, keeping whatever the session wrote itself, so
the session is back on native discovery rather than left reading a stale copy. A
user who sets `GIT_CONFIG_GLOBAL` themselves keeps their choice, and git then
reads only their file, so nothing is duplicated either.

`GH_CONFIG_DIR` points at the real gh config whenever no
`$XDG_CONFIG_HOME` redirect covers it, and `~/.ssh` is linked through so
git-over-SSH keeps working -- each only when its target exists, and the link is
skipped where the platform denies symlink privilege. The decision about what is
already configured is made against the environment the child actually starts
from, so a pointer configured per project or simply inherited by the daemon is
never overwritten by our defaults. No secrets move: keys stay live-linked so
rotation is never stale.

The home is retained like the prompts directory: it is never removed on spawn,
restore, or teardown, because every one of those paths can precede a retry that
needs the native history. Mirrored credentials refresh on every launch, so a
stale copy self-heals the next time the session starts.

Sessions created before per-session homes existed keep their native transcript
under the real home. Restoring one copies that conversation's own files into the
isolated home first, so `--resume` and `session/load` keep working after the
upgrade; files the session already has are never overwritten.

Because a session whose memory is empty would run with no AO role at all, a
Command Code session fails to start rather than launching without its standing
instructions.

## Chat Mode

**Supported** since Command Code v1.74.0, which ships a first-party ACP agent.

AO launches `cmd acp` and speaks the Agent Client Protocol over stdio. Verified
by a live handshake against v1.74.0 on 2026-10-02:

```text
protocolVersion 1
loadSession      true                  -> resume works
promptCapabilities{image: true, embeddedContext: true}
mcpCapabilities  {http: true}
sessionCapabilities{list: {}, resume: {}, close: {}}
```

`session/new` advertises five modes and two live config options:

| AO permission mode | Command Code mode id        |
| ------------------ | --------------------------- |
| default            | `default`                   |
| accept-edits       | `auto-accept`               |
| auto               | `auto-accept`               |
| bypass-permissions | `bypass`                    |

The model picker and effort control come from the provider's own catalog
(`model`, with 85 entries at the time of writing, and `effort` in category
`thought_level`), so AO keeps no hardcoded model list. Command Code also
advertises the `plan` and `dont-ask` modes, which have no AO equivalent and are
reachable only through Command Code's own UI.

The Chat binding lives in
`backend/internal/adapters/chatdriver/commandcodeacp/` and is registered in
`chatdriver/registry`, which is what makes the TUI ⇄ Chat toggle appear for this
harness.

Standing instructions reach Chat sessions through the first prompt (an embedded
`ao://agent-orchestrator/standing-instructions` resource), not through hooks:
the workspace `SessionStart` hook fires only in the TUI lifecycle and `cmd acp`
never calls it, so installing `.commandcode/settings.json` changes nothing for
Chat delivery. For the same reason AO never passes `--mod`/`--mod-option` on
the ACP path: `cmd acp` accepts the flag but silently never loads the mod
(verified on v1.82.4/v1.83.0 against issue #6784 — the same sentinel mod logs
under `cmd -p --mod` and logs nothing under `cmd acp`). Passing it would look
configured while staying inert.

## Restore

AO restores a Command Code session with `cmd --resume <session-id>` after the
workspace hook has captured the native session id. Before the first hook is
received, restore falls back to a fresh launch with AO's saved task.

## Auth

AO probes authentication with:

```bash
cmd status
```

Command Code prints `Authentication verified ...` when signed in; AO treats an
explicit "verified" result as authorized and otherwise classifies the output
with the shared authprobe needles. The result is advisory and a later model call
can still fail because of quota, plan, or provider configuration.

Log in with `cmd login`, which opens a browser; an API key can also be pasted in
the terminal.

## Not Supported

- Headless `-p`/`--print` sessions. AO runs supervised, multi-turn Terminal UI
  sessions and does not use Command Code's single-turn print mode.
- Mods on the Chat (`cmd acp`) path: `--mod`/`--mod-option`, `settings.mods.paths`,
  and `<project>/.commandcode/mods/` are silently ignored there (upstream vendor
  gap, issue #6784). AO does not pass them for Chat; standing instructions ride
  the first prompt instead. The `SessionStart` hook likewise never fires under
  ACP, while `PreToolUse`/`PostToolUse`/`Stop` continue to work.
