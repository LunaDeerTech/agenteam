-- agenteam:transaction tx
-- +goose Up
-- Retire only Execution's permission to use a dedicated environment lease.
-- Original mappings, historical references and physical deletion FKs remain.
ALTER TABLE agenteam_secret.project_variable_execution_leases
 ADD COLUMN released boolean NOT NULL DEFAULT false,
 ADD COLUMN released_at timestamptz(6),
 ADD CONSTRAINT project_variable_execution_lease_release_shape
 CHECK ((released AND released_at IS NOT NULL) OR (NOT released AND released_at IS NULL));

DROP TRIGGER project_variable_execution_lease_immutable
 ON agenteam_secret.project_variable_execution_leases;

-- +goose StatementBegin
CREATE FUNCTION agenteam_secret.guard_project_variable_execution_lease_retirement()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN
  RAISE EXCEPTION 'EXECUTION_ENVIRONMENT_HISTORY_IMMUTABLE' USING ERRCODE='23514';
 END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.released OR NEW.released_at IS NOT NULL THEN
   RAISE EXCEPTION 'EXECUTION_ENVIRONMENT_LEASE_ALREADY_RELEASED' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF (NEW.lease_id,NEW.execution_id,NEW.project_id,NEW.agent_id,NEW.variable_id,
     NEW.variable_version,NEW.credential_id,NEW.credential_version,NEW.agent_version,NEW.attempt_binding)
  IS DISTINCT FROM
    (OLD.lease_id,OLD.execution_id,OLD.project_id,OLD.agent_id,OLD.variable_id,
     OLD.variable_version,OLD.credential_id,OLD.credential_version,OLD.agent_version,OLD.attempt_binding)
  OR (OLD.released AND (NOT NEW.released OR NEW.released_at IS DISTINCT FROM OLD.released_at)) THEN
  RAISE EXCEPTION 'EXECUTION_ENVIRONMENT_LEASE_RETIREMENT_IMMUTABLE' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER project_variable_execution_lease_retirement
 BEFORE INSERT OR UPDATE OR DELETE ON agenteam_secret.project_variable_execution_leases
 FOR EACH ROW EXECUTE FUNCTION agenteam_secret.guard_project_variable_execution_lease_retirement();
