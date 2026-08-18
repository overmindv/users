-- +goose Up
CREATE EXTENSION IF NOT EXISTS pg_trgm;

ALTER TABLE users ADD COLUMN avatar_file_id UUID;

CREATE INDEX users_username_trgm_idx ON users USING GIN ((username::text) gin_trgm_ops) WHERE deleted_at IS NULL;
CREATE INDEX users_first_name_trgm_idx ON users USING GIN (first_name gin_trgm_ops) WHERE deleted_at IS NULL;
CREATE INDEX users_last_name_trgm_idx ON users USING GIN (last_name gin_trgm_ops) WHERE deleted_at IS NULL;

CREATE TABLE user_outbox_events (
    id UUID PRIMARY KEY,
    aggregate_id UUID NOT NULL,
    event_type TEXT NOT NULL,
    payload JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'published', 'failed')),
    attempts INTEGER NOT NULL DEFAULT 0,
    available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    locked_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX user_outbox_claim_idx
    ON user_outbox_events (available_at, created_at)
    WHERE status = 'pending';

-- +goose Down
DROP TABLE IF EXISTS user_outbox_events;
DROP INDEX IF EXISTS users_last_name_trgm_idx;
DROP INDEX IF EXISTS users_first_name_trgm_idx;
DROP INDEX IF EXISTS users_username_trgm_idx;
ALTER TABLE users DROP COLUMN IF EXISTS avatar_file_id;
