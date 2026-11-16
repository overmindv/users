-- +goose Up
ALTER TABLE users ADD COLUMN security_version bigint NOT NULL DEFAULT 1 CHECK(security_version>0);
CREATE TABLE user_sessions (
 id uuid PRIMARY KEY,
 user_id uuid NOT NULL REFERENCES users(id),
 security_version bigint NOT NULL CHECK(security_version>0),
 expires_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 revoked_at timestamptz
);
CREATE INDEX user_sessions_expiry_idx ON user_sessions(expires_at);
CREATE INDEX user_sessions_user_idx ON user_sessions(user_id);
-- +goose StatementBegin
CREATE FUNCTION users_security_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.password_hash IS DISTINCT FROM OLD.password_hash
 OR NEW.roles IS DISTINCT FROM OLD.roles
 OR NEW.is_superuser IS DISTINCT FROM OLD.is_superuser
 OR NEW.deleted_at IS DISTINCT FROM OLD.deleted_at THEN
  NEW.security_version := OLD.security_version + 1;
 ELSE
  NEW.security_version := OLD.security_version;
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER users_security_version BEFORE UPDATE ON users FOR EACH ROW EXECUTE FUNCTION users_security_version();

-- +goose Down
DROP TRIGGER users_security_version ON users;
DROP FUNCTION users_security_version();
DROP TABLE user_sessions;
ALTER TABLE users DROP COLUMN security_version;
