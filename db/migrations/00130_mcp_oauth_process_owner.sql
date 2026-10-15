-- agenteam:transaction tx
-- +goose Up
-- Existing requests carry no invented process identity. They remain historical
-- facts and cannot acquire the new stopped-process recovery authority.
ALTER TABLE agenteam_mcp.oauth_requests
 ADD COLUMN process_id agenteam_object.safe_id REFERENCES agenteam_object.process_claims(process_id) ON DELETE RESTRICT,
 ADD COLUMN process_required boolean NOT NULL DEFAULT false,
 ADD CONSTRAINT mcp_oauth_process_shape CHECK(process_required=(process_id IS NOT NULL));
ALTER TABLE agenteam_mcp.oauth_requests ALTER COLUMN process_required SET DEFAULT true;

-- +goose StatementBegin
CREATE FUNCTION agenteam_mcp.guard_oauth_request_origin() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NOT NEW.process_required OR NEW.process_id IS NULL THEN
   RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='OAuth request process required';
  END IF;
 ELSIF ROW(NEW.id,NEW.operation,NEW.project_id,NEW.config_id,NEW.config_revision,
           NEW.connection_id,NEW.connection_version,NEW.flow_id,NEW.command_digest,
           NEW.user_id,NEW.session_id,NEW.execution_id,NEW.operation_id,NEW.attempt_id,
           NEW.backend_request_id,NEW.runtime_binding_id,NEW.credential_id,
           NEW.credential_version,NEW.credential_lease_id,NEW.created_at,
           NEW.process_id,NEW.process_required)
       IS DISTINCT FROM
       ROW(OLD.id,OLD.operation,OLD.project_id,OLD.config_id,OLD.config_revision,
           OLD.connection_id,OLD.connection_version,OLD.flow_id,OLD.command_digest,
           OLD.user_id,OLD.session_id,OLD.execution_id,OLD.operation_id,OLD.attempt_id,
           OLD.backend_request_id,OLD.runtime_binding_id,OLD.credential_id,
           OLD.credential_version,OLD.credential_lease_id,OLD.created_at,
           OLD.process_id,OLD.process_required) THEN
  RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='OAuth request origin immutable';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER oauth_request_origin BEFORE INSERT OR UPDATE ON agenteam_mcp.oauth_requests
 FOR EACH ROW EXECUTE FUNCTION agenteam_mcp.guard_oauth_request_origin();
CREATE INDEX mcp_oauth_process_retirement ON agenteam_mcp.oauth_requests(process_id,id)
 WHERE process_required AND (NOT io_retired OR NOT lease_released OR phase IN ('pending','claimed','unknown'));
