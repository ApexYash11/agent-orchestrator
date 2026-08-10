# Review Run Reconciliation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Recover AO review runs when the GitHub review was posted but `ao review submit` did not execute.

**Architecture:** Put a strict versioned AO marker in provider review bodies, observe COMMENTED reviews with numeric GitHub IDs, and reconcile matching provider facts through the existing review-service Submit path. Independently cancel old running runs only after reviewer liveness is conclusively absent, and fail review launch if AO's executable cannot be pinned into PATH.

**Tech Stack:** Go, GitHub GraphQL, SQLite/sqlc, existing daemon services and test fakes.

## Global Constraints

- Keep the CLI thin and all recovery inside daemon service/observer boundaries.
- Do not store derived session status or treat a failed runtime probe as death.
- Stale means exactly 30 minutes; cancel only a missing reviewer handle or `Alive == false`.
- Do not edit merged migrations or generated sqlc code by hand; change the query and run `npm run sqlc`.
- Reuse `service/review.Submit` for completion, idempotency, telemetry, and delivery.
- Validate marker run ID, worker session, canonical PR URL, target SHA, verdict, and positive numeric GitHub review ID.
- No API/OpenAPI, frontend, listener, authentication, schema-migration, dependency, or manual-CDC changes.
- Known baseline exception: the untouched Windows `TestLauncherSpawnPrependsNodeRuntimeForNodeShimReviewer` fails because its expected Node directory is absent from PATH.

## File Map

- `backend/internal/review/prompt.go`, `prompt_test.go`: provider marker contract.
- `backend/internal/service/review/reconcile.go`, `reconcile_test.go`: marker parsing and reconciliation orchestration.
- `backend/internal/adapters/scm/github/observer_provider.go`, `_test.go`: COMMENTED observations and numeric IDs.
- `backend/internal/review/review.go`, `_test.go`: exact per-run stale cancellation.
- `backend/internal/storage/sqlite/queries/review.sql`, generated sqlc, store and tests: guarded cancellation persistence.
- `backend/internal/observe/scm/observer.go`, `_test.go`: durable-fact reconciliation and retries.
- `backend/internal/review/launcher.go`, `_test.go`: strict AO PATH pinning.
- `backend/internal/daemon/scm_wiring.go`, `daemon.go`, wiring tests: dependency injection.

---

### Task 1: Marker contract and parser

**Files:**
- Modify: `backend/internal/review/prompt.go`
- Modify: `backend/internal/review/prompt_test.go`
- Create: `backend/internal/service/review/reconcile.go`
- Create: `backend/internal/service/review/reconcile_test.go`

**Interfaces:**
- Produces `parseProviderReviewMarker(body string) (providerReviewMarker, string, bool)`.
- Wire form: `<!-- ao-review:v1 run=<run-id> sha=<target-sha> verdict=<approved|changes_requested> -->`.

- [ ] **Step 1: Write failing prompt tests**

Use a launch item with run `run-123` and SHA `abc123`, then assert:

```go
marker := "<!-- ao-review:v1 run=run-123 sha=abc123 verdict=<approved|changes_requested> -->"
if !strings.Contains(got, marker) { t.Fatalf("missing marker contract: %s", got) }
if !strings.Contains(got, "Do not include the ao-review marker in the JSON sent to `ao review submit`") {
	t.Fatalf("missing marker-free submit instruction: %s", got)
}
```

- [ ] **Step 2: Prove the prompt test fails**

Run `cd backend && go test ./internal/review -run 'Test.*Prompt' -count=1`.
Expected: FAIL because provider POST bodies currently contain only `<summary>`.

- [ ] **Step 3: Implement prompt instructions**

Render each task's exact run ID and target SHA in the GitHub POST body after the human summary. Require the reviewer to replace the verdict placeholder with `approved` or `changes_requested`. Keep the CLI JSON body marker-free and preserve GitHub POST before CLI submit.

- [ ] **Step 4: Write failing parser tests**

```go
func TestParseProviderReviewMarker(t *testing.T) {
	body := "Looks good.\n\n<!-- ao-review:v1 run=run-123 sha=abc123 verdict=approved -->"
	m, prose, ok := parseProviderReviewMarker(body)
	if !ok || string(m.RunID) != "run-123" || m.SHA != "abc123" || string(m.Verdict) != "approved" || prose != "Looks good." {
		t.Fatalf("marker=%#v prose=%q ok=%v", m, prose, ok)
	}
}
```

Add rejected cases for unknown version, missing/extra fields, duplicate marker, invalid verdict, empty prose, and non-whitespace suffix text.

- [ ] **Step 5: Prove parser tests fail**

Run `cd backend && go test ./internal/service/review -run TestParseProviderReviewMarker -count=1`.
Expected: compile failure because the parser is absent.

- [ ] **Step 6: Implement the parser**

```go
var providerReviewMarkerRE = regexp.MustCompile(`(?s)^(.*?)\s*<!-- ao-review:v1 run=([A-Za-z0-9._:-]+) sha=([A-Fa-f0-9]+) verdict=(approved|changes_requested) -->\s*$`)

type providerReviewMarker struct {
	RunID domain.ReviewRunID
	SHA string
	Verdict domain.ReviewVerdict
}
```

Match the anchored expression, require `strings.Count(body, "<!-- ao-review:") == 1`, trim prose, reject empty prose, and return typed captures. Use the repository's actual existing type names without introducing aliases.

- [ ] **Step 7: Verify and commit**

Run `cd backend && go test ./internal/review ./internal/service/review -run 'Test.*Prompt|TestParseProviderReviewMarker' -count=1`.
Then commit:

```bash
git add backend/internal/review/prompt.go backend/internal/review/prompt_test.go backend/internal/service/review/reconcile.go backend/internal/service/review/reconcile_test.go
git commit -m "feat: mark provider reviews for reconciliation"
```

### Task 2: Observe COMMENTED reviews and numeric IDs

**Files:**
- Modify: `backend/internal/adapters/scm/github/observer_provider.go`
- Modify: `backend/internal/adapters/scm/github/observer_provider_test.go`

**Interfaces:**
- Produces GraphQL selection `reviews(... states:[APPROVED,CHANGES_REQUESTED,COMMENTED])`.
- Maps `databaseId` to decimal `domain.PullRequestReview.ID`, falling back to node `id` only if absent.

- [ ] **Step 1: Write failing adapter tests**

Add a fixture node with opaque `id`, `databaseId: 98765`, `state: COMMENTED`, review URL/body, and assert the query requests both `COMMENTED` and `databaseId`; assert mapped ID is `98765`.

- [ ] **Step 2: Prove the test fails**

Run `cd backend && go test ./internal/adapters/scm/github -run 'Test.*Review|Test.*Observe' -count=1`.
Expected: FAIL because COMMENTED and databaseId are absent.

- [ ] **Step 3: Implement query and mapping**

Use:

```graphql
reviews(last:%d, states:[APPROVED,CHANGES_REQUESTED,COMMENTED]) {
  nodes { id databaseId state url submittedAt body author { login } }
}
```

Before building the domain review:

```go
reviewID := stringValue(review["id"])
if databaseID := int64(numberValue(review["databaseId"])); databaseID > 0 {
	reviewID = strconv.FormatInt(databaseID, 10)
}
```

Use the file's existing JSON helper names rather than duplicating them.

- [ ] **Step 4: Verify and commit**

Run `cd backend && go test ./internal/adapters/scm/github -count=1`.
Then commit:

```bash
git add backend/internal/adapters/scm/github/observer_provider.go backend/internal/adapters/scm/github/observer_provider_test.go
git commit -m "fix: observe AO comment reviews"
```

### Task 3: Provider-fact reconciliation through Submit

**Files:**
- Modify: `backend/internal/service/review/reconcile.go`
- Modify: `backend/internal/service/review/reconcile_test.go`
- Modify: `backend/internal/service/review/review.go`

**Interfaces:**
- Produces `ReconcileProviderReviews(context.Context, domain.SessionID, domain.PullRequest, []domain.PullRequestReview) error`.
- Consumes existing `GetReviewRun` and `Submit` paths.

- [ ] **Step 1: Write failing service tests**

Add tests named `TestReconcileProviderReviewsCompletesMatchingRunningRun`, `...RetriesDeliveryForCompletedRun`, `...IgnoresDeliveredRun`, `...IgnoresMismatches`, and `...ReturnsOperationalError`. Assert success stores marker-free prose, marker verdict, and ID `98765`; mismatched session/PR/SHA/verdict, malformed markers, and nonnumeric IDs do nothing.

- [ ] **Step 2: Prove tests fail**

Run `cd backend && go test ./internal/service/review -run TestReconcileProviderReviews -count=1`.
Expected: compile failure because the method is absent.

- [ ] **Step 3: Implement validation and Submit delegation**

For each marked provider review: require a positive decimal ID, load the run, ignore not-found/invalid facts, require worker session, exact canonical PR URL, marker SHA equal to run target SHA and observed PR head SHA, and skip delivered runs. For running runs call existing `Submit` with marker-free prose; for complete runs call it with the already-stored body/verdict/review ID so delivery retries idempotently. Return operational store/lifecycle errors to the observer.

```go
func isDecimalID(value string) bool {
	n, err := strconv.ParseUint(value, 10, 64)
	return err == nil && n > 0
}
```

- [ ] **Step 4: Verify and commit**

Run `cd backend && go test ./internal/service/review -count=1`.
Then commit:

```bash
git add backend/internal/service/review/reconcile.go backend/internal/service/review/reconcile_test.go backend/internal/service/review/review.go
git commit -m "fix: reconcile completed provider reviews"
```

### Task 4: Guarded stale-run cancellation

**Files:**
- Modify: `backend/internal/storage/sqlite/queries/review.sql`
- Regenerate: `backend/internal/storage/sqlite/gen/review.sql.go`
- Modify: `backend/internal/storage/sqlite/store/review_store.go`, `_test.go`
- Modify: `backend/internal/review/review.go`, `_test.go`
- Modify: `backend/internal/service/review/reconcile.go`, `_test.go`

**Interfaces:**
- Produces `CancelReviewRun(context.Context, domain.ReviewRunID, string) (bool, error)`.
- Produces engine `ReconcileStaleRunningRuns(context.Context, domain.SessionID, time.Time) error`.
- Produces service `ReconcileStaleReviewRuns(context.Context, domain.SessionID, time.Time) error`.

- [ ] **Step 1: Write a failing store test**

Create running and complete rows. Cancel both by exact ID and assert only the running row changes to cancelled with body `cancelled because reviewer terminal is unavailable`; the second call returns `false`.

- [ ] **Step 2: Prove the store test fails**

Run `cd backend && go test ./internal/storage/sqlite/store -run TestReviewStoreCancelReviewRun -count=1`.
Expected: compile failure because the method is absent.

- [ ] **Step 3: Add the query and regenerate sqlc**

```sql
-- name: CancelReviewRun :execrows
UPDATE review_run
SET status = 'cancelled', body = ?
WHERE id = ? AND status = 'running' AND verdict = '';
```

Run `npm run sqlc` from the repository root. Implement the store wrapper using affected rows; do not hand-edit generated output.

- [ ] **Step 4: Write failing liveness tests**

Table-test: a 29m59s run stays running; a 30m run with missing row/handle is cancelled; `Alive == false` cancels; `Alive == true` stays; an `Alive` error stays and is returned; completed runs are untouched. Assert cancellation is by exact run ID, never the existing session-wide method.

- [ ] **Step 5: Implement core stale reconciliation**

List running runs and review registrations, map registrations by harness, inspect only `CreatedAt <= staleBefore`, and cache probes by terminal handle. Missing handle or confirmed dead calls exact `CancelReviewRun`; probe errors are collected with `errors.Join` and never cause cancellation.

- [ ] **Step 6: Add the service threshold**

```go
const staleReviewRunAge = 30 * time.Minute

func (s *Service) ReconcileStaleReviewRuns(ctx context.Context, workerID domain.SessionID, now time.Time) error {
	return s.engine.ReconcileStaleRunningRuns(ctx, workerID, now.Add(-staleReviewRunAge))
}
```

Test with a fixed clock and assert the engine receives exactly `now.Add(-30*time.Minute)`.

- [ ] **Step 7: Verify and commit**

Run `cd backend && go test ./internal/review ./internal/service/review ./internal/storage/sqlite/store -run 'ReconcileStale|CancelReviewRun' -count=1`.
Then commit all query/generated/store/core/service files with `git commit -m "fix: cancel confirmed stale review runs"`.

### Task 5: SCM observer integration

**Files:**
- Modify: `backend/internal/observe/scm/observer.go`
- Modify: `backend/internal/observe/scm/observer_test.go`

**Interfaces:**
- Produces narrow `ReviewReconciler` with the two service methods from Tasks 3–4.
- Produces `Config.ReviewReconciler ReviewReconciler`.

- [ ] **Step 1: Write failing observer tests**

Add fake-reconciler tests proving: provider facts are persisted before reconciliation; a reconciliation error retains the pending review hash and retries next poll; stale reconciliation runs without SCM credentials; one session's stale error does not block another.

- [ ] **Step 2: Prove tests fail**

Run `cd backend && go test ./internal/observe/scm -run 'TestObserver.*Reconcil' -count=1`.
Expected: compile failure because the interface/config are absent.

- [ ] **Step 3: Implement stale and provider hooks**

```go
type ReviewReconciler interface {
	ReconcileProviderReviews(context.Context, domain.SessionID, domain.PullRequest, []domain.PullRequestReview) error
	ReconcileStaleReviewRuns(context.Context, domain.SessionID, time.Time) error
}
```

After subject discovery but before zero-repository/missing-credential returns, reconcile stale runs once per unique worker ID and log/continue per-session errors. After the first durable observation write, reconcile changed/fetched reviews; on error mark refresh failed and keep pending hashes. Acknowledge final hashes only after reconciliation and lifecycle work both succeed.

- [ ] **Step 4: Verify and commit**

Run `cd backend && go test ./internal/observe/scm -count=1`.
Commit with `git commit -m "fix: reconcile review runs from SCM observations"`.

### Task 6: Fail closed on AO PATH pinning

**Files:**
- Modify: `backend/internal/review/launcher.go`
- Modify: `backend/internal/review/launcher_test.go`

**Interfaces:**
- Produces `WithExecutableResolver(func() (string, error))` launcher option.
- Changes `runtimeEnv` to return `(map[string]string, error)`.

- [ ] **Step 1: Write a failing launcher test**

Inject a resolver returning `errors.New("executable unavailable")`; assert Spawn returns an error containing `pin ao executable in PATH` and runtime Create receives zero calls. Add success coverage that the resolved AO directory is first in PATH.

- [ ] **Step 2: Prove the test fails**

Run `cd backend && go test ./internal/review -run TestLauncherSpawnFailsWhenExecutablePATHCannotBePinned -count=1`.

- [ ] **Step 3: Implement strict PATH construction**

Default the resolver to `os.Executable`. Pass it to `sessionmanager.HookPATH`; return a wrapped error on failure, otherwise set PATH and retain `AugmentRuntimePATHForLaunchBinary`. Update Spawn and RestoreTerminal to stop before runtime creation on error; existing engine failure handling records failed runs.

- [ ] **Step 4: Verify and commit**

Run `cd backend && go test ./internal/review -run 'ExecutablePATH|Launcher.*PATH' -count=1`. Report, but do not alter, the known unrelated Windows Node-shim baseline if selected. Commit with `git commit -m "fix: fail review launch when ao path is unavailable"`.

### Task 7: Daemon dependency wiring

**Files:**
- Modify: `backend/internal/daemon/scm_wiring.go`
- Modify: `backend/internal/daemon/daemon.go`
- Modify: `backend/internal/daemon/scm_wiring_test.go`

**Interfaces:**
- `startSCMObserver` accepts `scmobserve.ReviewReconciler` and assigns `Config.ReviewReconciler`.

- [ ] **Step 1: Write a failing wiring test**

Pass a fake reconciler through SCM wiring and use one controlled poll to assert `ReconcileStaleReviewRuns` is called once.

- [ ] **Step 2: Prove it fails**

Run `cd backend && go test ./internal/daemon -run 'Test.*SCMObserver' -count=1`.

- [ ] **Step 3: Wire the existing service**

Pass the already-created `reviewSvc` from daemon session wiring into `startSCMObserver`, then into observer config. Do not create a second service or global singleton.

- [ ] **Step 4: Verify and commit**

Run `cd backend && go test ./internal/daemon -run 'Test.*SCMObserver|Test.*Start' -count=1`.
Commit with `git commit -m "fix: wire review reconciliation into SCM polling"`.

### Task 8: Regression and verification

**Files:**
- Modify only the files above if an issue-scoped defect is exposed.

**Interfaces:**
- Verifies provider POST → observation → Submit/delivery plus stale fallback.

- [ ] **Step 1: Add the cross-layer regression**

At the existing observer integration boundary, seed a running run and COMMENTED review containing `LGTM\n\n<!-- ao-review:v1 run=run-3755 sha=deadbeef verdict=approved -->`. Poll and assert marker-free body `LGTM`, approved verdict, numeric ID `3755`, and normal delivery. Poll again and assert no duplicate delivery or completion mutation.

- [ ] **Step 2: Run focused checks**

```bash
cd backend
go test ./internal/adapters/scm/github ./internal/observe/scm ./internal/service/review ./internal/storage/sqlite/store -count=1
go test ./internal/review -run 'Prompt|ReconcileStale|ExecutablePATH' -count=1
```

- [ ] **Step 3: Format and regenerate**

Run gofmt on every touched Go file, `npm run sqlc`, `git diff --check`, and `git status --short`. Expect no sqlc drift and only issue-scoped files.

- [ ] **Step 4: Run broad checks**

Run `go test ./... -count=1`, `go vet ./...`, `npm run lint`, and `npm run frontend:typecheck`. Bound broad commands in this Windows environment; if they hang, stop only their exact spawned processes and report the timeout. Separate the known pre-existing Node-shim PATH failure.

- [ ] **Step 5: Inspect the final diff**

Confirm: explicit CLI submit remains; markers are stripped before storage; COMMENTED/numeric IDs are observed; all provider identities validate; completed runs retry delivery; observer hashes remain retryable; stale cancellation is per-run and probe-safe; PATH errors fail launch; no migration/API/frontend/network/auth/manual-CDC changes exist.

- [ ] **Step 6: Prepare evidence**

Run `git status --short` and `git log --oneline upstream/main..HEAD`; invoke `superpowers:verification-before-completion` before claiming completion. Commit any final test-only adjustment as `test: cover review run reconciliation`.
