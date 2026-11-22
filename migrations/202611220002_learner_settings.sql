-- +goose Up
ALTER TABLE users
    ADD COLUMN timezone varchar(64) NOT NULL DEFAULT 'UTC',
    ADD COLUMN locale varchar(64) NOT NULL DEFAULT 'ru-RU',
    ADD COLUMN public_name boolean NOT NULL DEFAULT false,
    ADD COLUMN leaderboard_visible boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE users DROP COLUMN leaderboard_visible, DROP COLUMN public_name,
    DROP COLUMN locale, DROP COLUMN timezone;
