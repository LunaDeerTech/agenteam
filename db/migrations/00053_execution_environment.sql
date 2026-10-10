-- agenteam:transaction tx
-- +goose Up
-- Environment captures ordinary values in the caller's immutable input, not in
-- these reference tables. Secret captures retain only stable IDs and metadata
-- versions. Neither ciphertext nor plaintext is duplicated here. Legacy Secret
-- purposes, secret_leases and their readers remain unchanged.
CREATE TABLE agenteam_projectvariable.execution_environments (
 execution_id agenteam_execution.safe_id PRIMARY KEY,
 project_id agenteam_execution.safe_id NOT NULL,
 agent_id agenteam_execution.safe_id NOT NULL,
 agent_version bigint NOT NULL CHECK(agent_version>0),
 attempt_binding text NOT NULL CHECK(attempt_binding ~ '^sha256:[0-9a-f]{64}$'),
 capture_digest text NOT NULL CHECK(capture_digest ~ '^sha256:[0-9a-f]{64}$'),
 ordinary_count integer NOT NULL CHECK(ordinary_count BETWEEN 0 AND 4096),
 secret_count integer NOT NULL CHECK(secret_count BETWEEN 0 AND 256),
 UNIQUE(execution_id,project_id,agent_id,agent_version,attempt_binding),
 FOREIGN KEY(execution_id,project_id,agent_id)
  REFERENCES agenteam_execution.executions(id,project_id,agent_id) ON DELETE RESTRICT
);

-- This is a separate project_variable / execution arm, inaccessible to the
-- generic Purpose.Valid / Model lease API. Captured versions prove the original
-- metadata match; they do not pin a Secret value for a later process launch.
CREATE TABLE agenteam_secret.project_variable_execution_leases (
 lease_id uuid PRIMARY KEY CHECK(lease_id::text ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'),
 execution_id agenteam_execution.safe_id NOT NULL,
 project_id agenteam_execution.safe_id NOT NULL,
 agent_id agenteam_execution.safe_id NOT NULL,
 variable_id agenteam_projectvariable.safe_id NOT NULL,
 variable_version bigint NOT NULL CHECK(variable_version>0),
 credential_id uuid NOT NULL REFERENCES agenteam_secret.secrets(id) ON DELETE RESTRICT,
 credential_version bigint NOT NULL CHECK(credential_version>0),
 agent_version bigint NOT NULL CHECK(agent_version>0),
 attempt_binding text NOT NULL CHECK(attempt_binding ~ '^sha256:[0-9a-f]{64}$'),
 UNIQUE(execution_id,credential_id),
 UNIQUE(execution_id,variable_id),
 UNIQUE(lease_id,execution_id,variable_id),
 FOREIGN KEY(execution_id,project_id,agent_id)
  REFERENCES agenteam_execution.executions(id,project_id,agent_id) ON DELETE RESTRICT,
 FOREIGN KEY(execution_id,project_id,agent_id,agent_version,attempt_binding)
  REFERENCES agenteam_projectvariable.execution_environments(execution_id,project_id,agent_id,agent_version,attempt_binding)
  DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX project_variable_execution_lease_credential
 ON agenteam_secret.project_variable_execution_leases(credential_id,execution_id);

CREATE TABLE agenteam_projectvariable.execution_secret_references (
 execution_id agenteam_execution.safe_id NOT NULL
  REFERENCES agenteam_projectvariable.execution_environments(execution_id) ON DELETE RESTRICT,
 variable_id agenteam_projectvariable.safe_id NOT NULL
  REFERENCES agenteam_projectvariable.variables(id) ON DELETE RESTRICT,
 lease_id uuid NOT NULL,
 PRIMARY KEY(execution_id,variable_id),
 UNIQUE(lease_id),
 FOREIGN KEY(lease_id,execution_id,variable_id)
  REFERENCES agenteam_secret.project_variable_execution_leases(lease_id,execution_id,variable_id) ON DELETE RESTRICT
);
CREATE INDEX execution_environment_variable_reference
 ON agenteam_projectvariable.execution_secret_references(variable_id,execution_id);

-- Check the dedicated credential's current scope/purpose/version at insertion.
-- Rotation after capture remains legal; no constraint pins future values.
-- +goose StatementBegin
CREATE FUNCTION agenteam_secret.check_project_variable_execution_lease() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE stored_scope text; stored_project uuid; stored_purpose text; stored_version bigint;
BEGIN
 SELECT scope,project_id,purpose,version INTO STRICT stored_scope,stored_project,stored_purpose,stored_version
 FROM agenteam_secret.secrets WHERE id=NEW.credential_id;
 IF stored_scope<>'project' OR stored_project::text<>NEW.project_id OR stored_purpose<>'project_variable' OR stored_version<>NEW.credential_version THEN
  RAISE EXCEPTION 'ENVIRONMENT_CREDENTIAL_MAPPING' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER project_variable_execution_lease_mapping BEFORE INSERT
 ON agenteam_secret.project_variable_execution_leases FOR EACH ROW
 EXECUTE FUNCTION agenteam_secret.check_project_variable_execution_lease();

-- A future retention owner must supply an explicit retirement contract. This
-- slice intentionally has no release method: existing Secret delete observes
-- these leases and refuses deletion. Transaction rollback needs no release.
-- +goose StatementBegin
CREATE FUNCTION agenteam_projectvariable.immutable_execution_environment() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'EXECUTION_ENVIRONMENT_IMMUTABLE' USING ERRCODE='23514';
END $$;
-- +goose StatementEnd
CREATE TRIGGER execution_environment_immutable BEFORE UPDATE OR DELETE
 ON agenteam_projectvariable.execution_environments FOR EACH ROW
 EXECUTE FUNCTION agenteam_projectvariable.immutable_execution_environment();
CREATE TRIGGER execution_secret_reference_immutable BEFORE UPDATE OR DELETE
 ON agenteam_projectvariable.execution_secret_references FOR EACH ROW
 EXECUTE FUNCTION agenteam_projectvariable.immutable_execution_environment();
CREATE TRIGGER project_variable_execution_lease_immutable BEFORE UPDATE OR DELETE
 ON agenteam_secret.project_variable_execution_leases FOR EACH ROW
 EXECUTE FUNCTION agenteam_projectvariable.immutable_execution_environment();

-- Both directions validate the complete same-Tx set, including an empty one.
-- Cross-domain UUID/text comparison is explicit; the real Execution FK uses
-- its native text tuple, while Variable and Secret IDs keep their UUID types.
-- +goose StatementBegin
CREATE FUNCTION agenteam_projectvariable.check_execution_environment_references() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE expected integer; actual bigint; lease_count bigint; bad boolean;
BEGIN
 SELECT secret_count INTO STRICT expected FROM agenteam_projectvariable.execution_environments WHERE execution_id=NEW.execution_id;
 SELECT count(*) INTO actual FROM agenteam_projectvariable.execution_secret_references WHERE execution_id=NEW.execution_id;
 SELECT count(*) INTO lease_count FROM agenteam_secret.project_variable_execution_leases WHERE execution_id=NEW.execution_id;
 SELECT EXISTS(
  SELECT 1 FROM agenteam_projectvariable.execution_secret_references r
  JOIN agenteam_secret.project_variable_execution_leases l ON l.lease_id=r.lease_id
  JOIN agenteam_projectvariable.variables v ON v.id=r.variable_id
  WHERE r.execution_id=NEW.execution_id AND
   (v.project_id::text<>l.project_id OR v.type<>'secret' OR v.deleted_at IS NOT NULL OR
    v.version<>l.variable_version OR v.credential_id IS DISTINCT FROM l.credential_id OR
    v.credential_version IS DISTINCT FROM l.credential_version)
 ) INTO bad;
 IF actual<>expected OR lease_count<>expected OR bad THEN
  RAISE EXCEPTION 'EXECUTION_ENVIRONMENT_REFERENCE_SET' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER execution_environment_reference_set AFTER INSERT
 ON agenteam_projectvariable.execution_environments DEFERRABLE INITIALLY DEFERRED
 FOR EACH ROW EXECUTE FUNCTION agenteam_projectvariable.check_execution_environment_references();
CREATE CONSTRAINT TRIGGER execution_secret_reference_set AFTER INSERT
 ON agenteam_projectvariable.execution_secret_references DEFERRABLE INITIALLY DEFERRED
 FOR EACH ROW EXECUTE FUNCTION agenteam_projectvariable.check_execution_environment_references();
CREATE CONSTRAINT TRIGGER execution_environment_lease_set AFTER INSERT
 ON agenteam_secret.project_variable_execution_leases DEFERRABLE INITIALLY DEFERRED
 FOR EACH ROW EXECUTE FUNCTION agenteam_projectvariable.check_execution_environment_references();
