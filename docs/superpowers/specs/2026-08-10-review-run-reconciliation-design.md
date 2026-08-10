# Review Run Reconciliation

## Problem

An AO reviewer completes two independent side effects in sequence. It first
posts a `COMMENT` review to GitHub, then invokes `ao review submit` to record the
machine-readable verdict. If the second command cannot resolve the AO binary,
the provider review exists but the durable `review_run` remains `running` with
no verdict. The planner consequently reports `running` forever and prevents a
normal retry for the same PR, SHA, and reviewer harness.

The reviewer launcher currently tries to pin the daemon executable directory
onto `PATH`, but silently retains the inherited environment when that pin
cannot be constructed. The SCM observer already fetches provider review IDs and
bodies, but it has no boundary through which it can reconcile those facts with
AO review runs. A later manual trigger only clears the row when the reviewer
terminal is definitively dead; it does not recover the already-posted verdict.

## Intended Behavior

Posting the provider review and recording the AO verdict remains a two-step
workflow, but the second step is no longer a single point of permanent failure.

1. A newly launched reviewer must have a verified AO CLI on its runtime `PATH`.
   If AO cannot construct that environment, the launch fails and its newly
   created runs transition to `failed`; the reviewer never posts provider work
   that it cannot submit back to AO.
2. Every AO-authored GitHub review carries a strict, versioned HTML marker with
   its run ID, target SHA, and verdict. The marker is invisible in rendered
   review prose.
3. When the SCM observer later fetches that review, a review reconciliation
   service validates the marker against the durable run and completes it through
   the same submission path used by `ao review submit`.
4. A running row older than 30 minutes is cancelled only when its reviewer
   terminal is definitively unavailable. A failed liveness probe is unknown,
   not proof of death, and must not change durable state.

Manual `ao review submit` remains the fast path. Provider reconciliation is
idempotent recovery, normally converging on the already-completed or delivered
row without duplicate worker delivery or telemetry.

## Approaches Considered

### Only strengthen `PATH`

Failing reviewer launch when the AO binary cannot be pinned is the smallest
preventive change. It does not recover failures after the GitHub request, such
as a deleted binary, a shell error, or an interrupted submission, so a completed
review can still be stranded.

### Only add a timeout

A timeout removes the permanent dashboard badge but discards a valid provider
review and its verdict. It also cannot safely infer failure while a reviewer
terminal remains alive.

### Provider reconciliation with prevention and a bounded safety net

This is the selected design. It preserves the provider review as the recovery
fact, prevents the known missing-binary cause, and makes abandoned rows
retryable without treating an unknown runtime probe as death.

## Design

### Versioned provider marker

The reviewer prompt requires the final GitHub review body to end with exactly
one marker:

```text
<!-- ao-review:v1 run=<run-id> sha=<target-sha> verdict=<approved|changes_requested> -->
```

Parsing is strict: one line, known version, non-empty run and SHA tokens, and a
valid AO verdict. The marker is stripped before AO stores the recovered review
body. Unmarked reviews and malformed markers remain ordinary provider facts and
cannot mutate a review run.

The reconciler validates all durable relationships before submitting:

- the run exists and is still `running`, `complete`, or `delivered`;
- the run belongs to the observed worker session;
- the normalized PR URL and target SHA match the observation;
- the observed provider review ID is non-empty.

The run ID is an unguessable capability until the legitimate AO review publishes
it. A copied marker observed later cannot change an already-completed run because
the existing submission path rejects conflicting terminal results.

Validation failures are logged and ignored as non-AO provider reviews. Store or
submission failures are returned so the observer retries on its next refresh.

### Reconciliation boundary

The SCM observer receives an optional narrow `ReviewReconciler` interface. It
passes durable provider review observations to that interface after the SCM
facts have been written. The production implementation lives in the review
service, where it can reuse `Submit`/`submitOne`, delivery idempotency, telemetry,
and existing store validation. The observer does not write `review_run` rows
directly and no parallel CDC emission is introduced.

The interface also exposes stale-run reconciliation for the worker sessions
already discovered by the observer. This reuses the observer's existing 30
second daemon loop rather than creating another scheduler.

### AO CLI launch validation

The reviewer launcher receives an injectable executable resolver, defaulting to
`os.Executable`. Its runtime-environment builder returns an error when
`sessionmanager.HookPATH` cannot pin an executable named `ao` (or `ao.exe` on
Windows). Spawn and restore propagate the error before runtime creation. The
review engine's existing `failRuns` path records a launch failure, so no new
terminal status is needed.

Tests inject a real temporary `ao`/`ao.exe` path. Production continues invoking
the bare `ao` command from the prompt, now backed by a verified environment.
Existing agent-binary PATH augmentation remains in place after the AO pin.

### Stale-run finalization

The review engine exposes a reconciliation operation for running runs older than
30 minutes. Runs younger than the threshold are untouched. For each stale run,
the engine resolves the reviewer row for the run's harness:

- missing row or empty terminal handle: cancel the run as unavailable;
- `Alive` returns `false`: cancel the run as unavailable;
- `Alive` returns `true`: leave it running;
- `Alive` returns an error: leave it running and return the error for logging
  and retry.

Cancellation uses the existing conditional store operation and existing
`cancelled` status, which makes the planner report `needs_review` and permits a
new run under the current uniqueness rules. This safety net does not overwrite
provider-reconciled `complete` or `delivered` rows.

## Data Flow

```text
review trigger
  -> create running review_run
  -> verify reviewer binary and AO CLI environment
  -> launch reviewer
  -> GitHub COMMENT review containing ao-review:v1 marker
  -> ao review submit (normal fast path)

SCM observer refresh
  -> persist provider review facts
  -> review service parses and validates AO marker
  -> existing Submit path transitions running -> complete -> delivered
  -> planner reports up_to_date or changes_requested

SCM observer session pass
  -> stale-run reconciliation
  -> only confirmed unavailable reviewer cancels an old running row
  -> planner reports needs_review
```

## Error Handling and Idempotency

- Re-observing the same provider review must not redeliver worker feedback or
  emit duplicate telemetry.
- A marker for another session, PR, SHA, or author must not mutate state.
- A conflicting verdict on an already-completed run remains an invalid
  submission and is logged for investigation.
- Observer errors are isolated per session/PR and retried; one malformed review
  does not stop the poll.
- Liveness errors never cause cancellation.
- No existing migration is edited. This design does not require a schema or API
  contract change.

## Testing

TDD coverage will include:

1. Prompt tests for the exact versioned marker and both allowed verdicts.
2. Parser tests for valid markers, stripped prose, malformed input, duplicate
   markers, unknown versions, and invalid verdicts.
3. Review-service tests showing a matching observed provider review completes
   and delivers a running run, repeats idempotently, and rejects mismatched
   session, PR, SHA, or provider ID.
4. Observer tests proving persisted reviews are offered to the reconciler and
   reconciliation failures remain retryable without aborting unrelated PRs.
5. Launcher tests proving an unpinnable AO executable prevents runtime creation
   and a valid AO path is first on the runtime `PATH`.
6. Stale-run tests for younger runs, live terminals, dead terminals, missing
   handles, completed runs, and liveness probe errors.
7. Planner/service integration coverage confirming reconciliation yields
   `up_to_date` or `changes_requested`, while confirmed abandonment yields
   `needs_review`.

Focused review, review-service, observer, daemon wiring, and storage tests must
pass. The repository-wide backend lint command should also be attempted; the
untouched upstream baseline currently has a Windows-only failure in
`TestLauncherSpawnPrependsNodeRuntimeForNodeShimReviewer`, which will be reported
separately and not changed as part of issue #3755.

## Non-Goals

- Changing GitHub reviews from `COMMENT` to `APPROVE` or `REQUEST_CHANGES`.
- Parsing free-form review prose to guess a verdict.
- Moving review state transitions into the SCM observer or SQLite store.
- Adding a new public daemon endpoint or changing generated API artifacts.
- Fixing unrelated reviewer PATH discovery tests or general runtime reaping.
