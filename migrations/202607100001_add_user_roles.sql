-- +goose Up
ALTER TABLE users
    ADD COLUMN roles text[] NOT NULL DEFAULT ARRAY[]::text[],
    ADD COLUMN is_superuser boolean NOT NULL DEFAULT false;

CREATE INDEX users_roles_gin_idx ON users USING gin (roles) WHERE deleted_at IS NULL;
CREATE INDEX users_username_search_idx ON users (username) WHERE deleted_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS users_username_search_idx;
DROP INDEX IF EXISTS users_roles_gin_idx;

ALTER TABLE users
    DROP COLUMN IF EXISTS is_superuser,
    DROP COLUMN IF EXISTS roles;
