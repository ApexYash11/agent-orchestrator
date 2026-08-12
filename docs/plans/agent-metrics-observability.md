# Agent Metrics & Observability — Implementation Plan (v3)

> **TL;DR:** Surface per-agent token usage, cost, context%, and retries directly
> on session cards (Kanban board) and inside the session detail view (new "Usage"
> tab). No standalone dashboard/route — metrics live where you're already looking.

## Goal

When multiple agents (opencode, claude-code, codex, etc.) are running in
parallel, an operator should be able to:
- glance at the board and see cost/tokens per session on the card itself
- click into any session to get the full usage breakdown

No new page, no new nav item. Everything is additive to existing surfaces.

## Design Decisions

- **Append-only metrics table** — one row per poll interval. Enables
  historical/timeline data inside the per-session Usage tab (mini timeline)
  without a schema rewrite later.
- **Latest-value materialized row** (`session_metrics_current`) — cheap lookup
  for "what's the current cost/tokens for session X" when rendering the card
  badge, kept in sync via CDC on both insert and update.
- **Per-model pricing table with fallback** — cost is computed from token
  counts when the agent CLI doesn't self-report a dollar figure. Logs a
  warning (not silent `$0.00`) when a model has no pricing entry, since
  pricing tables go stale faster than dependencies get updated.
- **Poll-first, push later** — background collector polls every 60s.
  Documented v1 limitation: sessions that finish inside one poll window
  aren't sampled. Push-based (Phase 7.5) closes this later.
- **OpenCode ships in Phase 2, not deferred** — this is the CLI the whole
  proposal is based on (screenshot evidence of tokens/cost/context% already
  rendered in its terminal footer), so it can't be an afterthought.
- **No aggregate dashboard page.** Explicitly cut. If a fleet-wide total is
  ever wanted, it can be derived client-side by summing visible card badges —
  not worth a new route/table/API surface for v1.

---

## Phase 1 — Core Data Model & Storage

### Domain types (`backend/internal/domain/metrics.go`)

```go
type AgentUsage struct {
    SessionID    string
    InputTokens  int64
    OutputTokens int64
    // Cost: -1 is the sentinel for "not reported by CLI, compute via pricing
    // table." A real value, including 0.00 (free tier), must never be -1 —
    // float64 can't otherwise distinguish "unset" from "genuinely zero."
    Cost         float64
    Model        string
    ContextPct   float64
    RetryCount   int64   // see note below — source must be explicit per adapter
    RecordedAt   time.Time
}

type SessionMetrics struct {
    SessionID          string
    TotalInputTokens    int64
    TotalOutputTokens   int64
    EstimatedCost       float64
    Model               string
    ContextUtilization  float64
    RetryCount          int64
    LastActivityAt      time.Time
    UpdatedAt           time.Time
}

// NewAgentUsage is the only correct way to construct an AgentUsage —
// zero-value construction would leave Cost at 0.0, indistinguishable from
// a genuine free-tier $0.00. Adapters MUST use this constructor rather
// than a struct literal, so the -1 sentinel can't be silently forgotten.
func NewAgentUsage(sessionID string) AgentUsage {
    return AgentUsage{SessionID: sessionID, Cost: -1}
}
```

> **Retry count source:** must be pinned to one of two meanings before coding
> starts — (a) the agent CLI's own internal retry signal (parsed from its
> output), or (b) AO's reaction-engine retries (`ci-failed.retries`,
> `changes-requested.escalateAfter`). Do not conflate these. Recommend (a) for
> this field, and if (b) is wanted later, add a distinct `ReactionRetryCount`
> rather than overloading this one.

### Pricing table (`domain/pricing.go`)

```go
type ModelPricing struct {
    Input  float64 // per 1M tokens (USD)
    Output float64
}

var DefaultPricing = map[string]ModelPricing{
    "claude-sonnet-4-20250514": {Input: 3.00, Output: 15.00},
    "claude-opus-4-5":          {Input: 15.00, Output: 75.00},
    "claude-sonnet-4-5":        {Input: 3.00, Output: 15.00},
    "claude-haiku-4-5":         {Input: 0.80, Output: 4.00},
    "gpt-4o":                   {Input: 2.50, Output: 10.00},
    "gpt-4o-mini":              {Input: 0.15, Output: 0.60},
}

// ComputeCost returns (cost, true) if the model is known, else (0, false).
// Callers must log when false — never silently show $0.00.
func ComputeCost(model string, inputTokens, outputTokens int64) (float64, bool)
```

Optional: allow override via `~/.ao/pricing.yaml` merged on top of
`DefaultPricing` at startup, so new models don't require a rebuild.

### SQL migration (`0022_session_metrics.sql`)

```sql
-- Append-only: one row per poll interval per session.
CREATE TABLE session_metrics (
    id            TEXT PRIMARY KEY,
    session_id    TEXT NOT NULL REFERENCES sessions(id),
    input_tokens  INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    cost          REAL,
    model         TEXT,
    context_pct   REAL,
    retry_count   INTEGER NOT NULL DEFAULT 0,
    recorded_at   TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_session_metrics_session ON session_metrics(session_id);
CREATE INDEX idx_session_metrics_recorded ON session_metrics(recorded_at);

-- Materialized latest value — cheap source for the card badge lookup.
CREATE TABLE session_metrics_current (
    session_id          TEXT PRIMARY KEY REFERENCES sessions(id),
    total_input_tokens   INTEGER NOT NULL DEFAULT 0,
    total_output_tokens  INTEGER NOT NULL DEFAULT 0,
    estimated_cost       REAL,
    model                TEXT,
    context_utilization  REAL,
    retry_count          INTEGER NOT NULL DEFAULT 0,
    -- last_activity_at: NOT sourced from the agent adapter — AgentUsage has
    -- no activity timestamp. The collector must read this from the
    -- session's own record (updated_at, touched by lifecycle on activity
    -- signals), not derive it from recorded_at (which is just "last time
    -- metrics were polled," not true agent activity).
    last_activity_at     TEXT,
    updated_at            TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Append-only table: AFTER INSERT always fires, no upsert to dodge it.
CREATE TRIGGER session_metrics_cdc AFTER INSERT ON session_metrics
BEGIN
    INSERT INTO change_log (event_type, entity_type, entity_id, payload)
    VALUES ('metrics.updated', 'session_metrics', NEW.session_id,
            json_object('session_id', NEW.session_id, 'recorded_at', NEW.recorded_at));
END;

-- Current-value cache: needs BOTH insert and update triggers, since an
-- upsert executes one or the other depending on whether the row exists.
CREATE TRIGGER session_metrics_current_insert_cdc AFTER INSERT ON session_metrics_current
BEGIN
    INSERT INTO change_log (event_type, entity_type, entity_id, payload)
    VALUES ('metrics.current.updated', 'session_metrics_current', NEW.session_id,
            json_object('session_id', NEW.session_id));
END;

CREATE TRIGGER session_metrics_current_update_cdc AFTER UPDATE ON session_metrics_current
BEGIN
    INSERT INTO change_log (event_type, entity_type, entity_id, payload)
    VALUES ('metrics.current.updated', 'session_metrics_current', NEW.session_id,
            json_object('session_id', NEW.session_id));
END;
```

### sqlc queries (`storage/sqlite/queries/metrics.sql`)

- `InsertSessionMetric` — append-only insert
- `UpsertSessionMetricsCurrent` — upsert into the current-value cache
- `GetSessionMetrics` — single row from `session_metrics_current` (used by the Usage tab detail view)
- `GetSessionMetricsHistory` — rows from `session_metrics` for one session, time-bounded (used by Usage tab's mini timeline)
- `ListSessionMetricsCurrentByIDs` — `SELECT * FROM session_metrics_current WHERE session_id IN (?)`, one batch call for the whole session-list page (powers the card badge via `SessionView.Metrics` — see Phase 5). Must be a single `IN` query, not one call per session.

> Deliberately **no** `GetAggregateMetrics` / `ListSessionMetrics` — those only
> existed to feed the now-cut dashboard page.

### Port (`ports/metrics.go`)

```go
type MetricsReporter interface {
    RecordUsage(ctx context.Context, sessionID string, usage domain.AgentUsage) error
    GetSessionMetrics(ctx context.Context, sessionID string) (*domain.SessionMetrics, error)
    GetSessionMetricsHistory(ctx context.Context, sessionID string, since time.Time) ([]domain.SessionMetrics, error)
}
```

---

## Phase 2 — Agent Adapter Integration

### Extend `ports/agent.go`

```go
type AgentProcess interface {
    Wait(ctx context.Context) (*AgentResult, error)
    Signal(ctx context.Context, signal os.Signal) error
    Usage() *domain.AgentUsage // nil if adapter doesn't track usage yet
}
```

### OpenCode adapter (`adapters/agent/opencode/usage.go`) — ships first

OpenCode's terminal footer already renders `Model | Tokens | Cost | Context`
(confirmed from live use — this is the direct motivating case). Parse this
from the process's output stream via a small, well-tested regex; store on the
process struct; expose via `Usage()`.

Add `testdata/` fixtures — capture real sample lines, including edge cases
observed in practice (e.g. lines that get corrupted/truncated mid-render on
interrupt — the parser must fail closed, i.e. return nil rather than a
garbage partial value, when a line doesn't cleanly match).

**Regex must be anchored** (`^...$` or equivalent full-match, not a bare
`.Find`) — an unanchored pattern can partially match a truncated/corrupted
line and silently return a plausible-looking but wrong value instead of
failing closed. Add a one-line comment in `usage.go` calling this out so a
future edit doesn't loosen the anchor without realizing why it's there.

### Claude Code adapter (`adapters/agent/claudecode/`)

Same pattern, parsing its own stderr/stdout usage lines. Fixtures in
`testdata/`.

### Pricing fallback

```go
func (u *AgentUsage) FillCost() {
    // Cost == -1 is the only "unset" signal. A CLI-reported $0.00 (free
    // tier) is a real value and must be preserved, not overwritten.
    if u.Cost != -1 || u.Model == "" {
        return
    }
    if cost, ok := domain.ComputeCost(u.Model, u.InputTokens, u.OutputTokens); ok {
        u.Cost = cost
    } else {
        log.Warn("no pricing entry for model", "model", u.Model)
        u.Cost = 0 // fall back to 0 rather than storing a -1 sentinel downstream
    }
}
```

Called once by the collector before storing, so every stored row has a
consistent cost value (or an explicit logged gap) regardless of which adapter
produced it. Adapters get the `-1` sentinel for free by constructing usage
via `domain.NewAgentUsage(sessionID)` rather than a bare struct literal —
see Phase 1.

### Other adapters (codex, aider, cursor, ...)

Return `nil` from `Usage()` until implemented. Card badge and Usage tab both
render "N/A" gracefully rather than a blank/broken row.

---

## Phase 3 — Metrics Collector Service

### `service/metrics/service.go`

```go
type Service struct{ store MetricsStore }

func (s *Service) RecordUsage(ctx, sessionID, usage) error
func (s *Service) GetSessionMetrics(ctx, sessionID) (*SessionMetrics, error)
func (s *Service) GetSessionMetricsHistory(ctx, sessionID, since) ([]SessionMetrics, error)
```

### Background collector (`observe/metrics/collector.go`)

Ticker every 60s:
1. List active sessions from the session manager
2. Call each session's adapter `Usage()` if available
3. `FillCost()` before storing
4. Read `last_activity_at` from the session record itself (`updated_at`,
   touched by lifecycle on activity signals) — **not** from the adapter,
   since `AgentUsage` carries no activity timestamp
5. Append to `session_metrics`, upsert `session_metrics_current`
6. Per-session errors are logged, never block the tick

Wire into the daemon composition root next to the SCM observer and reaper.

> **Known v1 limitation:** sessions that spawn, run, and finish inside a
> single 60s window aren't sampled. Call this out explicitly in the PR
> description — Phase 7.5 (push-based) removes it.

---

## Phase 4 — HTTP API (trimmed)

Only what the card badge and Usage tab actually need:

| Method | Path | Response |
|---|---|---|
| `GET` | `/api/v1/sessions/{id}/metrics` | `SessionMetricsDetail` |
| `GET` | `/api/v1/sessions/{id}/metrics/history` | `[]SessionMetricsPoint` (Usage tab timeline) |

```go
type SessionMetricsDetail struct {
    SessionID          string  `json:"sessionId"`
    TotalInputTokens   int64   `json:"totalInputTokens"`
    TotalOutputTokens  int64   `json:"totalOutputTokens"`
    EstimatedCost      float64 `json:"estimatedCost"`
    Model              string  `json:"model"`
    ContextUtilization float64 `json:"contextUtilization"`
    RetryCount         int64   `json:"retryCount"`
    LastActivityAt     string  `json:"lastActivityAt"`
}

// SessionMetricsPoint — one row of the history response, powers the
// Usage tab's mini timeline. Was missing from earlier drafts.
type SessionMetricsPoint struct {
    RecordedAt   string  `json:"recordedAt"`
    InputTokens  int64   `json:"inputTokens"`
    OutputTokens int64   `json:"outputTokens"`
    Cost         float64 `json:"cost"`
    ContextPct   float64 `json:"contextPct"`
}

// SessionMetricsSummary — the lightweight embed on SessionView (Phase 5's
// session-list enrichment), not a standalone endpoint response. Belongs in
// controllers/dto.go alongside the two structs above, not just in prose.
type SessionMetricsSummary struct {
    EstimatedCost float64 `json:"estimatedCost"`
    TotalTokens   int64   `json:"totalTokens"` // input+output combined, badge doesn't need the split
}
```

`GET /api/v1/sessions/{id}/metrics/history` returns `[]SessionMetricsPoint`.

No `/api/v1/metrics` or `/api/v1/metrics/sessions` aggregate endpoints —
cut along with the dashboard page. Also no per-card metrics endpoint calls —
see Phase 5, badge data rides on the existing session-list response instead.

Register in `apispec/specgen/build.go`, regen via `npm run api`.

---

## Phase 5 — Frontend: Card Badge + Session Usage Tab

### SessionCard badge (Kanban board — every column)

Compact line at the bottom of each card:

```
$0.42 | 57K tokens
```

**Not** fetched per-card via the `/metrics` endpoint — that would mean N
extra requests per poll cycle on a board with N live sessions, competing
with the existing session-list and PR-status polling. Instead, embed the
`SessionMetricsSummary` DTO (defined in Phase 4) directly on the existing
`SessionView` that the session list endpoint already returns:

```go
type SessionView struct {
    // ... existing fields (id, name, status, branch, pr, ...)
    Metrics *SessionMetricsSummary `json:"metrics,omitempty"` // nil if unavailable
}
```

The session-list service does one extra batch lookup — a single
`SELECT * FROM session_metrics_current WHERE session_id IN (?)` covering
the whole page of results — when assembling the response, **not** a
per-session query in a loop. Session lists commonly run 20+ rows; N
separate indexed lookups is still N round-trips, the IN-query collapses it
to one. This rides on a request the frontend is already making — zero new
client requests either way. The per-id
`GET /api/v1/sessions/{id}/metrics` endpoint still exists, but only for the
Usage tab's fuller detail view.

If metrics are unavailable (adapter returns nil), `Metrics` is omitted and
the card renders nothing — no "N/A" clutter on the board.

### SessionInspector — new "Usage" tab

Alongside Summary / Git / Browser, add a Usage tab:
- Token usage (input vs output — horizontal stacked bar)
- Cost to date
- Context utilization %
- Model name
- Retry count
- Mini timeline (tokens per poll interval) sourced from `.../metrics/history`

This is the "click into the agent, everything's there" surface.

---

## Phase 6 — Testing

| Layer | Coverage |
|---|---|
| Domain | Types, pricing table, `ComputeCost`, `FillCost` fallback + log-on-miss |
| Store | Append insert, current upsert, history query — in-memory SQLite |
| Service | Record / retrieve / history — with fakes |
| Controller | HTTP round-trip — `httptest` |
| Collector | Tick / poll / store, per-session error isolation — fake agent + fake store |
| Adapter — OpenCode | Parsing fixtures, including malformed/truncated line → nil, not garbage |
| Adapter — Claude Code | Parsing fixtures |
| Frontend | Card badge render (present/absent), Usage tab render — vitest + MSW |
| E2E | `npm run api` regen + `go test ./...` + `npm run typecheck` |

---

## Phase 7 — Future Enhancements (explicitly out of scope for v1)

- **7.1** History charting inside the Usage tab (recharts/visx) — table already supports it
- **7.2** Cost budgets/alerts per project or session
- **7.3** Codex/Aider/Cursor adapters, same pattern as Phase 2
- **7.4** CSV export of one session's history
- **7.5** Push-based metrics (adapter pushes deltas on activity signal, replacing polling)
- **7.6** Per-step token log for expensive-turn analysis
- **7.7** TTL/pruning of old `session_metrics` rows via existing reaper

---

## File Inventory

| File | Action |
|---|---|
| `backend/internal/domain/metrics.go` | create |
| `backend/internal/domain/pricing.go` | create |
| `backend/internal/ports/metrics.go` | create |
| `backend/internal/ports/agent.go` | modify — add `Usage()` |
| `backend/internal/storage/sqlite/migrations/0022_session_metrics.sql` | create |
| `backend/internal/storage/sqlite/queries/metrics.sql` | create |
| `backend/internal/storage/sqlite/store/metrics_store.go` | create |
| `backend/internal/storage/sqlite/store/metrics_store_test.go` | create |
| `backend/internal/service/metrics/service.go` | create |
| `backend/internal/service/metrics/service_test.go` | create |
| `backend/internal/observe/metrics/collector.go` | create |
| `backend/internal/observe/metrics/collector_test.go` | create |
| `backend/internal/httpd/controllers/metrics.go` | create |
| `backend/internal/httpd/controllers/metrics_test.go` | create |
| `backend/internal/httpd/controllers/dto.go` | modify — add DTOs |
| `backend/internal/httpd/apispec/specgen/build.go` | modify |
| `backend/internal/adapters/agent/opencode/usage.go` | create |
| `backend/internal/adapters/agent/opencode/usage_test.go` | create |
| `backend/internal/adapters/agent/opencode/testdata/` | create |
| `backend/internal/adapters/agent/claudecode/claudecode.go` | modify |
| `backend/internal/adapters/agent/claudecode/claudecode_test.go` | modify |
| `backend/internal/adapters/agent/claudecode/testdata/` | create |
| `backend/internal/service/session/service.go` | modify — embed `Metrics` on `SessionView` when listing |
| `frontend/src/api/schema.ts` | regen |
| `frontend/src/renderer/components/SessionCard.tsx` | modify — render badge from existing `session.metrics` field (no new fetch) |
| `frontend/src/renderer/components/SessionInspector.tsx` | modify — add Usage tab |
| `frontend/src/renderer/hooks/useSessionMetricsHistoryQuery.ts` | create — Usage tab only |

---

# Opencode Session Kickoff Prompt

Paste this into a fresh opencode session against a local clone of
`AgentWrapper/agent-orchestrator`:

```
You're working in the agent-orchestrator repo (Go backend + React frontend,
plugin-based architecture). Before writing any code, explore the actual
codebase and verify or correct every assumption below — the paths and
patterns are my best guess from reading docs/architecture files, not
confirmed against current source.

Step 1 — Explore and confirm:
- Read backend/internal/ports/agent.go — confirm the AgentProcess interface
  shape and how adapters currently implement it.
- List backend/internal/adapters/agent/ — confirm opencode and claudecode
  adapters exist at those paths, and how they read the agent's stdout/stderr
  (raw pipe? PTY? existing parsing helpers I should reuse?).
- Read backend/internal/storage/sqlite/migrations/ — confirm the latest
  migration is 0021_pr_reviews.sql, making 0022 correct for this feature;
  flag if a newer one has landed since. Also confirm the existing
  change_log table schema and CDC trigger pattern used elsewhere.
- Read backend/internal/httpd/controllers/ — confirm existing controller
  and DTO conventions, and how routes get registered + how the OpenAPI spec
  gets regenerated.
- Read frontend/src/renderer/components/SessionCard.tsx and
  SessionInspector.tsx — confirm their current props/data-fetching pattern
  (TanStack Query?) so the new badge and Usage tab match existing style
  exactly rather than introducing a second pattern.
- Check whether opencode/claude-code expose any structured (JSON) output
  mode or internal event stream, as an alternative to regex-parsing their
  human-formatted terminal output — prefer that if it exists, since terminal
  output is lossy under interrupts.

Step 2 — Report back before implementing:
Summarize what you found vs. what I assumed, and flag any place my plan
below doesn't match the real codebase.

Step 3 — Implement, in this order:
1. Domain types + pricing table (backend/internal/domain/). Cost field uses
   -1 as the "unset" sentinel — a real $0.00 from a free-tier CLI must never
   be conflated with "nothing reported." Provide `NewAgentUsage(sessionID)`
   as the only constructor (sets Cost: -1) so adapters can't accidentally
   zero-init it via a bare struct literal.
2. Migration: append-only session_metrics + materialized session_metrics_current,
   with CDC triggers on session_metrics (AFTER INSERT) and
   session_metrics_current (AFTER INSERT and AFTER UPDATE — both needed
   because an upsert takes one or the other branch)
3. Store + service layer wrapping the migration, including a batch query
   (`ListSessionMetricsCurrentByIDs`, `WHERE session_id IN (?)`) for
   session-list enrichment — see step 9, must not be a per-session loop
4. OpenCode adapter usage parsing FIRST, with test fixtures — this is the
   priority case. Regex must be fully anchored (^...$ / equivalent full
   match) — an unanchored pattern can partial-match a truncated/corrupted
   line and return a plausible-but-wrong value instead of failing closed.
   Malformed/partial lines must return nil. Construct usage via
   NewAgentUsage(), never a bare AgentUsage{...} literal.
5. Claude Code adapter usage parsing, same pattern, same anchoring rule

**STOP HERE.** Before touching the collector, API, or frontend: show me the
actual parsed `AgentUsage` output against a real captured line from each
adapter's terminal/stdout (not a hypothetical fixture you wrote from
memory — an actual line you observed or captured from running the CLI).
This is the highest-risk phase — if the regex doesn't match the real output
format, everything downstream (collector, API, badge, Usage tab) will be
built on a wrong assumption and cost far more to unwind after 8 more files
are written on top of it. Wait for my go-ahead before continuing to step 6.

6. Pricing fallback (FillCost) — only fires when Cost == -1, preserves a
   real reported 0.00, logs a warning (falls back to 0, not -1) when a
   model has no pricing entry
7. Background collector (60s poll), wired into the daemon composition root.
   last_activity_at is read from the session's own record (updated_at) —
   AgentUsage has no activity timestamp, don't try to derive one from it or
   from recorded_at (that's just poll time, not real activity)
8. HTTP endpoints: GET /api/v1/sessions/{id}/metrics (SessionMetricsDetail)
   and GET /api/v1/sessions/{id}/metrics/history (returns
   []SessionMetricsPoint — define this DTO, it's new) — nothing else, no
   aggregate endpoints
9. Embed a `Metrics *SessionMetricsSummary` field (also defined as a new
   DTO) on the existing SessionView that the session-list endpoint already
   returns. The enrichment MUST use the batch `IN (?)` query from step 3 —
   one query for the whole page, not one call per session. Do NOT have
   SessionCard fetch /metrics per-card — with N live sessions on the board
   that's N extra requests every poll cycle, competing with existing
   polling. The per-id /metrics endpoint is for the Usage tab detail only.
10. Frontend: SessionCard reads session.metrics from the data it already
    has (no new fetch) and renders the badge, or nothing if metrics is
    omitted — don't show "N/A" clutter on the board. New "Usage" tab in
    SessionInspector alongside Summary/Git/Browser, using the per-id detail
    + history endpoints.
11. Tests at every layer per the table in the plan doc

Explicit constraints:
- No standalone metrics dashboard page or new top-level route — everything
  surfaces on the existing card and inside the existing session detail view.
- No per-card /metrics fetch — badge data must ride on the existing
  session-list poll, not add new requests.
- RetryCount must be sourced from the agent adapter's own retry signal, not
  conflated with AO's reaction-engine retry/escalation config — call this
  out clearly in a code comment if there's any ambiguity in what the
  adapter's output actually reports.
- Don't touch or fork the reaction-engine, lifecycle, or PR-flow logic —
  this should be a purely additive vertical slice.

Ask me before making any change outside backend/internal/{domain,ports,
storage,service,observe,httpd/controllers}/metrics-related files and
frontend/src/renderer/components/{SessionCard,SessionInspector}.tsx.
```