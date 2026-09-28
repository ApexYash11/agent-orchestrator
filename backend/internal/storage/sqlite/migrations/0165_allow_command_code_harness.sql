-- Widen the sessions.harness CHECK to allow the Command Code adapter.
-- Migrations 0163 (fx) and 0164 (gemini) already ran ahead of this version,
-- so the current schema carries both; the statements below match the current
-- post-gemini variants first and the legacy pre-fx variants as fallback.
-- SQLite cannot ALTER an existing CHECK constraint, so this applies the same
-- surgical sqlite_master rewrite used by the earlier harness migrations.
-- writable_schema changes run outside a transaction; RESET forces SQLite to
-- reparse the schema. replace() is a no-op when a variant is absent.

-- +goose NO TRANSACTION
-- +goose Up
-- +goose StatementBegin
PRAGMA writable_schema = ON;
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE sqlite_master
SET sql = replace(sql, '''unreal-agent'', ''fx'', ''fake''', '''unreal-agent'', ''fx'', ''command-code'', ''fake''')
WHERE type = 'table' AND name = 'sessions';
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE sqlite_master
SET sql = replace(sql, '''unreal-agent'', ''fx'', ''qm'', ''fake''', '''unreal-agent'', ''fx'', ''command-code'', ''qm'', ''fake''')
WHERE type = 'table' AND name = 'sessions';
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE sqlite_master
SET sql = replace(sql, '''omp'', ''unreal-agent'', ''fake''', '''omp'', ''unreal-agent'', ''command-code'', ''fake''')
WHERE type = 'table' AND name = 'sessions';
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE sqlite_master
SET sql = replace(sql, '''omp'', ''unreal-agent'', ''qm'', ''fake''', '''omp'', ''unreal-agent'', ''command-code'', ''qm'', ''fake''')
WHERE type = 'table' AND name = 'sessions';
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
SET sql = replace(sql, '''unreal-agent'', ''fx'', ''command-code'', ''fake''', '''unreal-agent'', ''fx'', ''fake''')
WHERE type = 'table' AND name = 'sessions';
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE sqlite_master
SET sql = replace(sql, '''unreal-agent'', ''fx'', ''command-code'', ''qm'', ''fake''', '''unreal-agent'', ''fx'', ''qm'', ''fake''')
WHERE type = 'table' AND name = 'sessions';
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE sqlite_master
SET sql = replace(sql, '''omp'', ''unreal-agent'', ''command-code'', ''fake''', '''omp'', ''unreal-agent'', ''fake''')
WHERE type = 'table' AND name = 'sessions';
-- +goose StatementEnd
-- +goose StatementBegin
UPDATE sqlite_master
SET sql = replace(sql, '''omp'', ''unreal-agent'', ''command-code'', ''qm'', ''fake''', '''omp'', ''unreal-agent'', ''qm'', ''fake''')
WHERE type = 'table' AND name = 'sessions';
-- +goose StatementEnd
-- +goose StatementBegin
PRAGMA writable_schema = RESET;
-- +goose StatementEnd
