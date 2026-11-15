-- +goose Up
CREATE TABLE login_throttle_buckets (
    scope text NOT NULL CHECK (scope IN ('email', 'ip')),
    key_hash text NOT NULL CHECK (length(key_hash) = 64),
    attempts integer NOT NULL CHECK (attempts > 0),
    expires_at timestamptz NOT NULL,
    PRIMARY KEY (scope, key_hash)
);
CREATE INDEX login_throttle_expiry_idx ON login_throttle_buckets (expires_at);

-- +goose Down
DROP TABLE login_throttle_buckets;
