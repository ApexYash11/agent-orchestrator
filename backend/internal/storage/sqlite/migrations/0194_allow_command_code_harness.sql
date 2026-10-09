-- Widen the sessions.harness CHECK to allow the Command Code adapter after the
-- shipped OpenHands and Codewhale harness migrations. SQLite cannot ALTER a CHECK
-- constraint, so this applies the same surgical sqlite_master rewrite those
-- migrations use, anchored on the retained 'fake' fixture harness.

-- +goose NO TRANSACTION
-- +goose Up
-- +goose StatementBegin
PRAGMA writable_schema = ON;
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE sqlite_master
SET sql = replace(sql, '''fake''))', '''command-code'', ''fake''))')
WHERE type = 'table' AND name = 'sessions'
  AND sql LIKE '%CHECK (harness IN (%'
  AND sql LIKE '%''muse''%'
  AND sql LIKE '%''omp''%'
  AND sql LIKE '%''gemini''%'
  AND sql LIKE '%''unreal-agent''%'
  AND sql LIKE '%''mimo-code''%'
  AND sql LIKE '%''deepseek-harness''%'
  AND sql LIKE '%''openhands''%'
  AND sql LIKE '%''codewhale''%'
  AND sql NOT LIKE '%''command-code''%';
-- +goose StatementEnd
-- +goose StatementBegin
PRAGMA writable_schema = RESET;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
PRAGMA writable_schema = ON;
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE sqlite_master
SET sql = replace(sql, '''command-code'', ''fake''))', '''fake''))')
WHERE type = 'table' AND name = 'sessions';
-- +goose StatementEnd
-- +goose StatementBegin
PRAGMA writable_schema = RESET;
-- +goose StatementEnd