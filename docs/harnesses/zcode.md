# ZCode Adapter

AO supports [ZCode](https://github.com/zai-org/ZCode) as a terminal worker,
orchestrator, and reviewer harness. ZCode is Z.ai's coding agent for GLM
models; its CLI distribution installs a single `zcode` command and AO drives it
in the interactive terminal UI. AO does not bundle ZCode.

## Install and authenticate

Install the official ZCode distribution from
[zcode.z.ai](https://zcode.z.ai/en/docs/install). It installs the CLI under
`~/.zcode/runtime` and links `zcode` into `~/.local/bin`; AO resolves the
binary from `PATH` first and then those locations.

Sign in with a Z.ai / GLM Coding Plan account in ZCode's own onboarding, or add
a provider key in its personal provider configuration
(`~/.zcode/v2/provider_config.json`; `ZCODE_PERSONAL_PROVIDER_CONFIG_FILE` and
`ZCODE_DATA_BASE_DIR` relocate it). `ZAI_API_KEY`, `ZCODE_API_KEY`, and
`GLM_API_KEY` in AO's environment also count as a key. AO's readiness check
reports **configured** when it finds one of those keys, because it only checks
presence, not provider acceptance; it reports **unknown** when ZCode is signed
in through its opaque credential store.

## AO integration

- ZCode starts in its interactive TUI; AO types the task into the session
  after startup. Headless `--prompt` runs are not used because they print one
  turn and exit, which would end the supervised worker mid-task.
- Permission modes map onto ZCode's documented `--mode` ladder: accept-edits
  uses `edit`, auto and bypass use `yolo`, and the default keeps ZCode's own
  configured mode (`build`). A session can also pin the ladder directly with
  the `mode` config value (`plan`, `build`, `edit`, `yolo`, `guarded`), which
  overrides the permission mapping.
- Model selection stays in ZCode's provider configuration; AO exposes no model
  override because the CLI has no model flag.
- AO installs no hooks. ZCode supports lifecycle hooks, but project hooks stay
  untrusted until a human reviews and grants the digest with
  `zcode hooks trust grant`, so an unattended install would silently never run.
- No native session id is captured, so sessions relaunch fresh in their
  worktree instead of restoring a previous ZCode conversation.

This integration is TUI-only. It does not register ZCode for Chat, reviewer,
interface handoff, or first-class nested-agent sessions. ZCode ships an
`app-server` protocol entry, but AO has not qualified it as a chat driver yet.
