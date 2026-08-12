-- name: InsertSessionMetric :exec
INSERT INTO session_metrics (id, session_id, input_tokens, output_tokens, cost, model, context_pct, retry_count, recorded_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpsertSessionMetricsCurrent :exec
INSERT INTO session_metrics_current (session_id, total_input_tokens, total_output_tokens, estimated_cost, model, context_utilization, retry_count, last_activity_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(session_id) DO UPDATE SET
    total_input_tokens = excluded.total_input_tokens,
    total_output_tokens = excluded.total_output_tokens,
    estimated_cost = excluded.estimated_cost,
    model = excluded.model,
    context_utilization = excluded.context_utilization,
    retry_count = excluded.retry_count,
    last_activity_at = excluded.last_activity_at,
    updated_at = excluded.updated_at;

-- name: GetSessionMetrics :one
SELECT session_id, total_input_tokens, total_output_tokens, estimated_cost, model, context_utilization, retry_count, last_activity_at, updated_at
FROM session_metrics_current
WHERE session_id = ?;

-- name: GetSessionMetricsHistory :many
SELECT id, session_id, input_tokens, output_tokens, cost, model, context_pct, retry_count, recorded_at
FROM session_metrics
WHERE session_id = ? AND recorded_at >= ?
ORDER BY recorded_at ASC;

-- name: ListSessionMetricsCurrentByIDs :many
SELECT session_id, total_input_tokens, total_output_tokens, estimated_cost, model, context_utilization, retry_count, last_activity_at, updated_at
FROM session_metrics_current
WHERE session_id IN (sqlc.slice('session_ids'));
