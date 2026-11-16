-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION users_last_admin() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE loses_admin boolean;
BEGIN
 loses_admin := TG_OP='DELETE';
 IF TG_OP='UPDATE' THEN
  loses_admin := NEW.deleted_at IS NOT NULL OR NOT (NEW.is_superuser OR 'admin'=ANY(NEW.roles) OR 'superuser'=ANY(NEW.roles));
 END IF;
 IF OLD.deleted_at IS NULL AND (OLD.is_superuser OR 'admin'=ANY(OLD.roles) OR 'superuser'=ANY(OLD.roles)) AND loses_admin THEN
  -- Запись guard сериализует проверку и вызывает serialization failure при stale RR snapshot.
  UPDATE user_security_guard SET version=version+1 WHERE id=true;
  IF NOT EXISTS(SELECT 1 FROM users WHERE id<>OLD.id AND deleted_at IS NULL AND id::text NOT LIKE '00000000-0000-0000-0000-0000000000%' AND password_hash LIKE '$argon2id$%' AND (is_superuser OR 'admin'=ANY(roles) OR 'superuser'=ANY(roles))) THEN
   RAISE EXCEPTION 'last active administrator' USING ERRCODE='23514',CONSTRAINT='users_last_admin';
  END IF;
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION users_last_admin() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE loses_admin boolean;
BEGIN
 loses_admin := TG_OP='DELETE';
 IF TG_OP='UPDATE' THEN
  loses_admin := NEW.deleted_at IS NOT NULL OR NOT (NEW.is_superuser OR 'admin'=ANY(NEW.roles) OR 'superuser'=ANY(NEW.roles));
 END IF;
 IF OLD.deleted_at IS NULL AND (OLD.is_superuser OR 'admin'=ANY(OLD.roles) OR 'superuser'=ANY(OLD.roles)) AND loses_admin THEN
  -- Запись guard сериализует проверку и вызывает serialization failure при stale RR snapshot.
  UPDATE user_security_guard SET version=version+1 WHERE id=true;
  IF NOT EXISTS(SELECT 1 FROM users WHERE id<>OLD.id AND deleted_at IS NULL AND (is_superuser OR 'admin'=ANY(roles) OR 'superuser'=ANY(roles))) THEN
   RAISE EXCEPTION 'last active administrator' USING ERRCODE='23514',CONSTRAINT='users_last_admin';
  END IF;
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
