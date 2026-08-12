-- Append-only: one row per poll interval per session.
-- +goose Up
-- +goose StatementBegin
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

-- Current-value cache: needs BOTH insert and update triggers.
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
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS session_metrics_current_update_cdc;
DROP TRIGGER IF EXISTS session_metrics_current_insert_cdc;
DROP TRIGGER IF EXISTS session_metrics_cdc;
DROP TABLE IF EXISTS session_metrics_current;
DROP TABLE IF EXISTS session_metrics;
-- +goose StatementEnd
