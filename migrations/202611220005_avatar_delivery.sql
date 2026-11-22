-- +goose Up
ALTER TABLE users ADD COLUMN avatar_version BIGINT NOT NULL DEFAULT 1 CHECK (avatar_version > 0);
CREATE INDEX user_outbox_avatar_version_idx ON user_outbox_events (aggregate_id, (payload->>'version')) WHERE event_type='user.avatar_changed';

-- +goose StatementBegin
CREATE FUNCTION users_replay_avatar(subject UUID) RETURNS UUID LANGUAGE plpgsql AS $$
DECLARE snapshot users%ROWTYPE; event_id UUID;
BEGIN
    SELECT * INTO snapshot FROM users WHERE id=subject FOR UPDATE;
    IF NOT FOUND THEN RAISE EXCEPTION 'user_not_found'; END IF;
    SELECT id INTO event_id FROM user_outbox_events
     WHERE aggregate_id=subject AND event_type='user.avatar_changed'
       AND payload->>'version'=snapshot.avatar_version::text AND status IN ('pending','running')
     ORDER BY created_at,id LIMIT 1;
    IF event_id IS NOT NULL THEN RETURN event_id; END IF;
    event_id := gen_random_uuid();
    INSERT INTO user_outbox_events (id,aggregate_id,event_type,payload)
    VALUES (event_id,subject,'user.avatar_changed',jsonb_build_object('user_id',subject,
      'version',snapshot.avatar_version,'file_id',CASE WHEN snapshot.deleted_at IS NULL THEN snapshot.avatar_file_id ELSE NULL END));
    RETURN event_id;
END $$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION users_replay_avatar(UUID) FROM PUBLIC;

-- +goose StatementBegin
CREATE FUNCTION users_avatar_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    NEW.avatar_version := OLD.avatar_version;
    IF NEW.avatar_file_id IS DISTINCT FROM OLD.avatar_file_id OR NEW.deleted_at IS DISTINCT FROM OLD.deleted_at THEN
        NEW.avatar_version := OLD.avatar_version + 1;
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER users_avatar_version BEFORE UPDATE ON users FOR EACH ROW EXECUTE FUNCTION users_avatar_version();

-- +goose StatementBegin
CREATE FUNCTION users_avatar_event() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.avatar_version <> OLD.avatar_version THEN PERFORM users_replay_avatar(NEW.id); END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER users_avatar_event AFTER UPDATE ON users FOR EACH ROW EXECUTE FUNCTION users_avatar_event();

-- +goose Down
DROP TRIGGER users_avatar_event ON users;
DROP FUNCTION users_avatar_event();
DROP TRIGGER users_avatar_version ON users;
DROP FUNCTION users_avatar_version();
DROP FUNCTION users_replay_avatar(UUID);
DROP INDEX user_outbox_avatar_version_idx;
ALTER TABLE users DROP COLUMN avatar_version;
