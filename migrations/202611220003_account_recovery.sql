-- +goose Up
ALTER TABLE users ADD COLUMN email_verified boolean NOT NULL DEFAULT false;
CREATE TABLE user_recovery_tokens (
 token_hash bytea PRIMARY KEY CHECK(octet_length(token_hash)=32),
 user_id uuid NOT NULL REFERENCES users(id),
 email text NOT NULL,
 security_version bigint NOT NULL,
 purpose text NOT NULL CHECK(purpose IN ('reset','verify')),
 expires_at timestamptz NOT NULL,
 used_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX user_recovery_user ON user_recovery_tokens(user_id,purpose);
CREATE INDEX user_recovery_expiry ON user_recovery_tokens(expires_at);
CREATE TABLE recovery_mail_outbox (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 token_hash bytea NOT NULL REFERENCES user_recovery_tokens(token_hash) ON DELETE CASCADE,
 payload bytea NOT NULL,
 attempts integer NOT NULL DEFAULT 0,
 next_attempt_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 claim_token uuid,
 lease_until timestamptz,
 sent_at timestamptz,
 exhausted boolean NOT NULL DEFAULT false
);
CREATE INDEX recovery_mail_pending ON recovery_mail_outbox(next_attempt_at) WHERE sent_at IS NULL AND NOT exhausted;

-- +goose Down
DROP TABLE recovery_mail_outbox;
DROP TABLE user_recovery_tokens;
ALTER TABLE users DROP COLUMN email_verified;
