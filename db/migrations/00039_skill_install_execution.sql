-- agenteam:transaction tx
-- +goose Up
-- Existing Human commands keep their exact identity and semantic bytes.
-- Agent provenance is an exclusive immutable branch, never a fake User or a
-- stored authorization grant. Current Runtime authority is checked per call.
ALTER TABLE agenteam_skill.installations
 ALTER COLUMN actor_user_id DROP NOT NULL,
 ADD COLUMN actor_kind text NOT NULL DEFAULT 'human',
 ADD COLUMN actor_agent_id agenteam_skill.safe_id,
 ADD COLUMN actor_execution_id agenteam_skill.safe_id,
 ADD COLUMN tool_operation_id agenteam_skill.safe_id,
 ADD COLUMN tool_id agenteam_skill.safe_id,
 ADD COLUMN tool_spec_revision bigint,
 ADD COLUMN operation_fingerprint text,
 ADD COLUMN first_tool_attempt_id agenteam_skill.safe_id,
 ADD COLUMN first_backend_request_id agenteam_skill.safe_id,
 ADD CONSTRAINT installations_actor_origin CHECK (
  (actor_kind='human' AND actor_user_id IS NOT NULL AND
   actor_agent_id IS NULL AND actor_execution_id IS NULL AND tool_operation_id IS NULL AND
   tool_id IS NULL AND tool_spec_revision IS NULL AND operation_fingerprint IS NULL AND
   first_tool_attempt_id IS NULL AND first_backend_request_id IS NULL)
  OR
  (actor_kind='agent_run' AND actor_user_id IS NULL AND
   actor_agent_id IS NOT NULL AND actor_execution_id IS NOT NULL AND tool_operation_id IS NOT NULL AND
   tool_id IS NOT NULL AND tool_spec_revision IS NOT NULL AND tool_spec_revision>0 AND
   operation_fingerprint IS NOT NULL AND operation_fingerprint ~ '^sha256:[0-9a-f]{64}$' AND
   first_tool_attempt_id IS NOT NULL AND first_backend_request_id IS NOT NULL AND
   command_key='tool.skill.install:'||tool_operation_id::text)),
 ADD CONSTRAINT installations_actor_tuple UNIQUE(project_id,id,actor_kind);
CREATE UNIQUE INDEX installations_tool_operation ON agenteam_skill.installations(tool_operation_id)
 WHERE actor_kind='agent_run';

ALTER TABLE agenteam_skill.installation_attempts
 ADD COLUMN actor_kind text NOT NULL DEFAULT 'human',
 ADD COLUMN tool_attempt_id agenteam_skill.safe_id,
 ADD COLUMN backend_request_id agenteam_skill.safe_id,
 ADD CONSTRAINT installation_attempt_actor_origin CHECK (
  (actor_kind='human' AND tool_attempt_id IS NULL AND backend_request_id IS NULL)
  OR (actor_kind='agent_run' AND tool_attempt_id IS NOT NULL AND backend_request_id IS NOT NULL)),
 ADD CONSTRAINT installation_attempt_actor_parent FOREIGN KEY(project_id,installation_id,actor_kind)
 REFERENCES agenteam_skill.installations(project_id,id,actor_kind) DEFERRABLE INITIALLY DEFERRED;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_skill.reject_installation_rewrite() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.phase IN ('published','failed')
 OR ROW(NEW.id,NEW.project_id,NEW.actor_user_id,NEW.command_key,NEW.skill_id,NEW.revision_id,NEW.semantic_digest,NEW.package_sha256,NEW.manifest_sha256,NEW.byte_size,NEW.manifest,NEW.name,NEW.normalized_name,NEW.description,NEW.created_at,
        NEW.actor_kind,NEW.actor_agent_id,NEW.actor_execution_id,NEW.tool_operation_id,NEW.tool_id,NEW.tool_spec_revision,NEW.operation_fingerprint,NEW.first_tool_attempt_id,NEW.first_backend_request_id)
 IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.actor_user_id,OLD.command_key,OLD.skill_id,OLD.revision_id,OLD.semantic_digest,OLD.package_sha256,OLD.manifest_sha256,OLD.byte_size,OLD.manifest,OLD.name,OLD.normalized_name,OLD.description,OLD.created_at,
        OLD.actor_kind,OLD.actor_agent_id,OLD.actor_execution_id,OLD.tool_operation_id,OLD.tool_id,OLD.tool_spec_revision,OLD.operation_fingerprint,OLD.first_tool_attempt_id,OLD.first_backend_request_id)
 OR OLD.version=9223372036854775807 OR NEW.version<>OLD.version+1
 OR NEW.updated_at<OLD.updated_at
 OR (OLD.phase='reserved' AND NEW.phase='planned')
 OR (OLD.object_id IS NOT NULL AND ROW(NEW.object_id,NEW.upload_id) IS DISTINCT FROM ROW(OLD.object_id,OLD.upload_id)) THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='installations_immutable', MESSAGE='immutable skill installation';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
