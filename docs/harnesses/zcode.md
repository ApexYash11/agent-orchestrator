# ZCode Adapter

The [ZCode](https://github.com/zai-org/ZCode) adapter is development-only.
**ZCode is not registered or selectable in AO.** Its public harness, installer,
authentication-plan, and CLI-help registration are deferred until private AO
standing-instruction delivery is supported and qualified. The adapter and
migration history are retained for future integration work and persisted rows.

## Blocking limitation: AO instructions are unsupported

**Instruction-bearing AO launches are unsupported.** Normal AO worker and
orchestrator sessions carry standing instructions. The adapter rejects them
with an actionable launch error instead of silently dropping their role and
coordination instructions. Restore falls back through the same guarded launch
path. Launches with no standing instructions, including empty or whitespace-only
instruction files, retain the existing TUI command and permission mapping.
An unreadable instruction file fails rather than being treated as empty.

Static inspection of upstream revision
[`3c5b227649`](https://github.com/zai-org/ZCode/tree/3c5b227649371507ddc4eb46ac5133afe6db1cab)
found no private instruction flag in the
[CLI parser](https://github.com/zai-org/ZCode/blob/3c5b227649371507ddc4eb46ac5133afe6db1cab/apps/zcode-cli/packages/cli/src/arguments.ts)
or instruction override in the
[environment config loader](https://github.com/zai-org/ZCode/blob/3c5b227649371507ddc4eb46ac5133afe6db1cab/apps/zcode-cli/packages/adapters/src/config/env-config.adapter.ts).
The native instruction loader merges `~/.zcode/AGENTS.md` and workspace
`AGENTS.md`; AO does not overwrite these files or redirect the CLI's home.
Upstream or integration support for private instruction delivery is required
before supervised worker/orchestrator launches can work.

**Live harness qualification has not passed.** An isolated macOS audit with
source-built ZCode 0.17.0 recorded inconclusive authentication, an empty model
catalog, no actionable setup control, and no ZCode entry in New Task. Both
worker and orchestrator launch requests reached the instruction guard and
failed. [Report and API evidence](https://github.com/OrchestratorInc/agent-orchestrator/pull/7389#issuecomment-6478145575)
and [all 15 real screenshots](https://github.com/OrchestratorInc/agent-orchestrator/pull/7389#issuecomment-6480704889)
document the prior registered head `f02b360c5f`; they do not qualify the deferred
adapter or any future instruction channel. User-level configuration was left
untouched. Before re-enabling registration, manager-level worker/orchestrator
spawns must reach launch with their AO instructions preserved, then pass the
live prompt, role-compliance, activity, and restore audit.

## Native setup reference (not an AO launch path)

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

## Retained adapter behavior (not publicly enabled)

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
  `zcode hooks trust grant`. User-level hooks are executable upstream, but AO
  has not integrated a session-specific configuration channel for them.
- No native session id is captured, so sessions relaunch fresh in their
  worktree instead of restoring a previous ZCode conversation, subject to the
  standing-instruction launch guard above.

The retained adapter targets TUI only. AO does not register ZCode for Chat, reviewer,
interface handoff, or first-class nested-agent sessions. ZCode ships an
`app-server` protocol entry, but AO has not qualified it as a chat driver yet.
