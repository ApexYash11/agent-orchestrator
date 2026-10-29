# ZCode Adapter

AO registers [ZCode](https://github.com/zai-org/ZCode) as a terminal-only
harness. ZCode is Z.ai's coding agent for GLM models; its CLI distribution
installs a single `zcode` command. AO does not bundle ZCode.

## Install and authenticate

Install the official ZCode distribution from
[zcode.z.ai](https://zcode.z.ai/en/docs/install). It installs the CLI under
`~/.zcode/runtime` and links `zcode` into `~/.local/bin`; AO resolves the
binary from `PATH` first and then those locations.

**The TUI needs a CLI with its terminal runtime.** The desktop app's bundled
`zcode.cjs` is headless-only (no `@zcode/tui`, and current releases ship no
single-executable CLI for Windows), so a stock desktop install cannot run the
terminal sessions AO supervises. Build the CLI from source until upstream
publishes a TUI-capable standalone CLI:

```bash
git clone https://github.com/zai-org/ZCode && cd ZCode
pnpm install --filter @zcode/cli... --filter @zcode/tui...
pnpm --filter @zcode/cli... run build
# runs from apps/zcode-cli/packages/cli/dist/zcode.cjs with Node 24
```

Sign in with `zcode login` (choose Z.AI or BigModel; browser authorization) —
AO's Harness settings offers this as the harness's Log in action — or connect
a provider key in the personal provider configuration
(`~/.zcode/v2/provider_config.json`; `ZCODE_PERSONAL_PROVIDER_CONFIG_FILE` and
`ZCODE_DATA_BASE_DIR` relocate it). `ZAI_API_KEY`, `ZCODE_API_KEY`, and
`GLM_API_KEY` in AO's environment also count as a key.

AO's readiness check verifies with one tiny plan-mode request in a throwaway
directory: a completed turn reports **authorized**, the provider's
plan-exhaustion or rejection codes report **unauthorized**, and only a
genuinely inconclusive check reports **unknown**. Because `zcode login`
stores credentials in a store AO cannot read, absence of file evidence is
never treated as signed out.

## AO integration

- ZCode starts in its interactive TUI; AO types the session's first message
  after startup. Headless `--prompt` runs are not used because they print one
  turn and exit, which would end the supervised worker mid-task.
- **Standing instructions ride the first typed message.** ZCode has no
  system-prompt flag (confirmed against the upstream CLI parser), so the
  adapter composes AO's role instructions ahead of the task with an explicit
  framing and a task marker. Nothing is dropped silently and the argv carries
  neither the instructions nor the task.
- Permission modes map onto ZCode's documented `--mode` ladder: accept-edits
  uses `edit`, auto and bypass use `yolo`, and the default keeps ZCode's own
  configured mode (`build`). A session can also pin the ladder directly with
  the `mode` config value (`plan`, `build`, `edit`, `yolo`, `guarded`), which
  overrides the permission mapping.
- The Models tab reads ZCode's own surfaces: the provider runtime catalog the
  distribution downloads under `~/.zcode/v2/runtime/provider/`
  (`builtinModelIds` per provider rule) plus `personalModelIds` from the
  personal provider configuration. IDs are never invented; with neither file
  present the catalog is empty and model selection stays inside ZCode.
- AO installs no hooks. ZCode supports lifecycle hooks, but project hooks stay
  untrusted until a human reviews and grants the digest with
  `zcode hooks trust grant`, and user-level hooks have no session-specific AO
  channel.
- No native session id is captured, so sessions relaunch fresh in their
  worktree instead of restoring a previous ZCode conversation.

This integration is TUI-only. It does not register ZCode for Chat, reviewer,
interface handoff, or first-class nested-agent sessions. ZCode ships an
`app-server` protocol entry, but AO has not qualified it as a chat driver yet.
