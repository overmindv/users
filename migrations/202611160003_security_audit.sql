-- +goose Up
CREATE TABLE user_security_guard(id boolean PRIMARY KEY DEFAULT true CHECK(id), version bigint NOT NULL DEFAULT 0);
INSERT INTO user_security_guard(id) VALUES(true);
CREATE TABLE user_security_audit(
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 actor_id uuid,
 subject_id uuid NOT NULL,
 action text NOT NULL,
 details jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX user_security_audit_subject_idx ON user_security_audit(subject_id,created_at);
-- +goose StatementBegin
CREATE FUNCTION users_last_admin() RETURNS trigger LANGUAGE plpgsql AS $$
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
CREATE FUNCTION users_security_audit() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE actor uuid; subject uuid; event text; detail jsonb;
BEGIN
 actor := NULLIF(current_setting('app.actor_id',true),'')::uuid;
 IF TG_OP='INSERT' THEN
  IF NOT(NEW.is_superuser OR 'admin'=ANY(NEW.roles) OR 'superuser'=ANY(NEW.roles)) THEN RETURN NEW; END IF;
  subject:=NEW.id;event:='privileged_created';
  detail:=jsonb_build_object('roles',NEW.roles,'superuser',NEW.is_superuser,'security_version',NEW.security_version);
 ELSIF TG_OP='DELETE' THEN
  subject:=OLD.id;event:='account_deleted';detail:=jsonb_build_object('security_version',OLD.security_version);
 ELSE
  IF NEW.security_version=OLD.security_version THEN RETURN NEW; END IF;
  subject:=NEW.id;event:='security_changed';
  detail:=jsonb_build_object('roles_before',OLD.roles,'roles_after',NEW.roles,'superuser_before',OLD.is_superuser,'superuser_after',NEW.is_superuser,'active_before',OLD.deleted_at IS NULL,'active_after',NEW.deleted_at IS NULL,'version_before',OLD.security_version,'version_after',NEW.security_version,'password_changed',NEW.password_hash IS DISTINCT FROM OLD.password_hash);
 END IF;
 INSERT INTO user_security_audit(actor_id,subject_id,action,details) VALUES(actor,subject,event,detail);
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
END;
$$;
CREATE FUNCTION reject_security_audit_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'security audit is append only' USING ERRCODE='42501';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER users_last_admin BEFORE UPDATE OR DELETE ON users FOR EACH ROW EXECUTE FUNCTION users_last_admin();
CREATE TRIGGER users_security_audit AFTER INSERT OR UPDATE OR DELETE ON users FOR EACH ROW EXECUTE FUNCTION users_security_audit();
CREATE TRIGGER security_audit_immutable BEFORE UPDATE OR DELETE ON user_security_audit FOR EACH ROW EXECUTE FUNCTION reject_security_audit_mutation();
CREATE TRIGGER security_audit_no_truncate BEFORE TRUNCATE ON user_security_audit FOR EACH STATEMENT EXECUTE FUNCTION reject_security_audit_mutation();

-- +goose Down
DROP TRIGGER users_security_audit ON users;
DROP TRIGGER users_last_admin ON users;
DROP TABLE user_security_audit;
DROP TABLE user_security_guard;
DROP FUNCTION users_security_audit();
DROP FUNCTION users_last_admin();
DROP FUNCTION reject_security_audit_mutation();
