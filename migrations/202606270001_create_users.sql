-- +goose Up
CREATE EXTENSION IF NOT EXISTS citext;

CREATE TABLE users (
    id              uuid            PRIMARY KEY,
    email           citext          NOT NULL,
    password_hash   text            NOT NULL,
    username        citext          NOT NULL,
    first_name      varchar(100)    NOT NULL DEFAULT '',
    last_name       varchar(100)    NOT NULL DEFAULT '',
    birth_date      date,
    phone           varchar(16),
    created_at      timestamptz     NOT NULL,
    updated_at      timestamptz     NOT NULL,
    deleted_at      timestamptz,
    CONSTRAINT users_email_not_blank CHECK (email <> ''),
    CONSTRAINT users_username_format CHECK (username ~ '^[a-z0-9][a-z0-9_.]{2,31}$'),
    CONSTRAINT users_phone_e164 CHECK (phone IS NULL OR phone ~ '^\+[1-9][0-9]{7,14}$'),
    CONSTRAINT users_birth_date_not_future CHECK (birth_date IS NULL OR birth_date <= CURRENT_DATE)
);

CREATE UNIQUE INDEX users_email_active_key ON users (email) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX users_username_active_key ON users (username) WHERE deleted_at IS NULL;
CREATE INDEX users_created_at_active_idx ON users (created_at, id) WHERE deleted_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS users;
