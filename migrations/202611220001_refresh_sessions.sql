-- +goose Up
CREATE TABLE user_refresh_families (
 id uuid PRIMARY KEY,
 user_id uuid NOT NULL REFERENCES users(id),
 security_version bigint NOT NULL CHECK(security_version>0),
 expires_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 revoked_at timestamptz
);
CREATE INDEX user_refresh_families_user_idx ON user_refresh_families(user_id);
CREATE TABLE user_refresh_tokens (
 token_hash bytea PRIMARY KEY CHECK(octet_length(token_hash)=32),
 family_id uuid NOT NULL REFERENCES user_refresh_families(id),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 used_at timestamptz
);
CREATE INDEX user_refresh_tokens_family_idx ON user_refresh_tokens(family_id);
ALTER TABLE user_sessions ADD COLUMN refresh_family_id uuid REFERENCES user_refresh_families(id);
CREATE INDEX user_sessions_refresh_family_idx ON user_sessions(refresh_family_id) WHERE refresh_family_id IS NOT NULL;

-- +goose Down
ALTER TABLE user_sessions DROP COLUMN refresh_family_id;
DROP TABLE user_refresh_tokens;
DROP TABLE user_refresh_families;
