-- agenteam:transaction tx
-- +goose Up
-- Preserve all original install encodings, digests, keys and identities. A
-- read-file operation has no Skill target or backend idempotency promise.
ALTER TABLE agenteam_tool.operations
 ADD COLUMN operation_kind text NOT NULL DEFAULT 'skill.install',
 ALTER COLUMN skill_id DROP NOT NULL,
 ALTER COLUMN backend_key DROP NOT NULL,
 ADD CONSTRAINT tool_operation_kind CHECK(operation_kind IN ('skill.install','runner.read-file')),
 ADD CONSTRAINT tool_operation_backend_shape CHECK((
  (operation_kind='skill.install' AND skill_id IS NOT NULL
   AND backend_key='tool.skill.install:'||operation_id::text)
  OR (operation_kind='runner.read-file' AND skill_id IS NULL AND backend_key IS NULL)
 ) IS TRUE);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_tool.operation_identity_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ROW(NEW.operation_id,NEW.project_id,NEW.agent_id,NEW.execution_id,NEW.logical_call_id,NEW.invocation_id,NEW.call_id,NEW.tool_id,NEW.spec_revision,NEW.input_data,NEW.input_digest,NEW.skill_id,NEW.backend_key,NEW.fingerprint,NEW.created_at,NEW.operation_kind)
 IS DISTINCT FROM ROW(OLD.operation_id,OLD.project_id,OLD.agent_id,OLD.execution_id,OLD.logical_call_id,OLD.invocation_id,OLD.call_id,OLD.tool_id,OLD.spec_revision,OLD.input_data,OLD.input_digest,OLD.skill_id,OLD.backend_key,OLD.fingerprint,OLD.created_at,OLD.operation_kind)
 OR NEW.version<OLD.version THEN
  RAISE EXCEPTION 'TOOL_OPERATION_IDENTITY_IMMUTABLE' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd

-- Original terminal/attempt metadata keep their 32KiB bounds. The explicit
-- read-file payload uses the protocol's 512KiB encoded cap, sufficient for a
-- full 65536-byte result including JSON escaping or base64. No public reader or
-- general-purpose payload store is introduced by this table.
CREATE TABLE agenteam_tool.read_file_results (
 operation_id agenteam_tool.safe_id NOT NULL,
 attempt_id agenteam_tool.safe_id NOT NULL,
 execution_id agenteam_tool.safe_id NOT NULL,
 project_id agenteam_tool.safe_id NOT NULL,
 agent_id agenteam_tool.safe_id NOT NULL,
 body_sha256 text NOT NULL CHECK(body_sha256 ~ '^sha256:[0-9a-f]{64}$'),
 body_bytes integer NOT NULL CHECK(body_bytes BETWEEN 2 AND 524288),
 body_data bytea NOT NULL CHECK(octet_length(body_data) BETWEEN 2 AND 524288),
 CHECK(body_bytes=octet_length(body_data)),
 PRIMARY KEY(operation_id,attempt_id),
 FOREIGN KEY(attempt_id,operation_id,execution_id,project_id,agent_id)
  REFERENCES agenteam_tool.attempts(attempt_id,operation_id,execution_id,project_id,agent_id)
  ON DELETE RESTRICT
);

-- +goose StatementBegin
CREATE FUNCTION agenteam_tool.read_file_result_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='tool_read_file_result_immutable', MESSAGE='immutable read-file result';
END $$;
-- +goose StatementEnd
CREATE TRIGGER tool_read_file_result_immutable BEFORE UPDATE OR DELETE
 ON agenteam_tool.read_file_results FOR EACH ROW
 EXECUTE FUNCTION agenteam_tool.read_file_result_immutable();

-- Final success, its original retired Attempt and the complete result are one
-- transaction. These deferred checks do not grant authority or prove a join;
-- the Tool owner must actually join the original call before writing them.
-- +goose StatementBegin
CREATE FUNCTION agenteam_tool.check_read_file_result() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE target uuid; k text; ph text; active uuid; matching bigint; total bigint;
BEGIN
 target:=NEW.operation_id;
 SELECT operation_kind,phase,active_attempt_id INTO STRICT k,ph,active
  FROM agenteam_tool.operations WHERE operation_id=target;
 SELECT count(*) INTO total FROM agenteam_tool.read_file_results WHERE operation_id=target;
 IF k='runner.read-file' AND ph='succeeded' THEN
  SELECT count(*) INTO matching FROM agenteam_tool.read_file_results r
   JOIN agenteam_tool.attempts a ON a.attempt_id=r.attempt_id AND a.operation_id=r.operation_id
    AND a.execution_id=r.execution_id AND a.project_id=r.project_id AND a.agent_id=r.agent_id
   WHERE r.operation_id=target AND r.attempt_id=active AND a.phase='succeeded' AND a.retired;
  IF total<>1 OR matching<>1 THEN
   RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='tool_read_file_result_complete', MESSAGE='incomplete read-file result';
  END IF;
 ELSIF total<>0 THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='tool_read_file_result_complete', MESSAGE='unexpected read-file result';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER tool_read_file_result_complete AFTER INSERT
 ON agenteam_tool.read_file_results DEFERRABLE INITIALLY DEFERRED
 FOR EACH ROW EXECUTE FUNCTION agenteam_tool.check_read_file_result();
CREATE CONSTRAINT TRIGGER tool_read_file_operation_complete AFTER INSERT OR UPDATE
 ON agenteam_tool.operations DEFERRABLE INITIALLY DEFERRED
 FOR EACH ROW EXECUTE FUNCTION agenteam_tool.check_read_file_result();
CREATE CONSTRAINT TRIGGER tool_read_file_attempt_complete AFTER INSERT OR UPDATE
 ON agenteam_tool.attempts DEFERRABLE INITIALLY DEFERRED
 FOR EACH ROW EXECUTE FUNCTION agenteam_tool.check_read_file_result();
