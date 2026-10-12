-- agenteam:transaction tx
-- +goose Up
-- Add only the write-file@1 metadata/call/Operation profile. Existing immutable
-- Builtin/read definitions, captures, input encodings, digests and bounds stay
-- unchanged. No registration, default binding or execution grant is inserted.
ALTER TABLE agenteam_tool.identities DROP CONSTRAINT tool_identity_stable_key;
ALTER TABLE agenteam_tool.identities ADD CONSTRAINT tool_identity_stable_key
 CHECK(stable_key ~ '^builtin:[a-z0-9_.-]{1,128}$'
  OR stable_key IN ('runner:read-file','runner:write-file'));

ALTER TABLE agenteam_tool.registrations DROP CONSTRAINT tool_registration_binding;
ALTER TABLE agenteam_tool.registrations ADD CONSTRAINT tool_registration_binding CHECK((
 (binding_kind='builtin' AND handler_id IS NOT NULL AND contract_revision IS NOT NULL
  AND operation_name IS NULL AND operation_revision IS NULL AND protocol_major IS NULL)
 OR (binding_kind='runner' AND handler_id IS NULL AND contract_revision IS NULL
  AND operation_revision='1' AND protocol_major=1 AND class='ordinary'
  AND ((operation_name='read-file' AND scope_resolver_id='runner.read-file' AND risk_classifier_id='runner.read-file')
   OR (operation_name='write-file' AND scope_resolver_id='runner.write-file' AND risk_classifier_id='runner.write-file')))
) IS TRUE);
ALTER TABLE agenteam_tool.execution_references DROP CONSTRAINT tool_execution_reference_binding;
ALTER TABLE agenteam_tool.execution_references ADD CONSTRAINT tool_execution_reference_binding CHECK((
 (binding_kind='builtin' AND handler_id IS NOT NULL AND contract_revision IS NOT NULL
  AND operation_name IS NULL AND operation_revision IS NULL AND protocol_major IS NULL)
 OR (binding_kind='runner' AND handler_id IS NULL AND contract_revision IS NULL
  AND operation_revision='1' AND protocol_major=1 AND class='ordinary'
  AND ((operation_name='read-file' AND scope_resolver_id='runner.read-file' AND risk_classifier_id='runner.read-file')
   OR (operation_name='write-file' AND scope_resolver_id='runner.write-file' AND risk_classifier_id='runner.write-file')))
) IS TRUE);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_tool.check_runner_binding_identity() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE stable text;
BEGIN
 SELECT stable_key INTO STRICT stable FROM agenteam_tool.identities WHERE tool_id=NEW.tool_id;
 IF (NEW.binding_kind='runner' AND (
    (stable='runner:read-file' AND NEW.operation_name='read-file')
    OR (stable='runner:write-file' AND NEW.operation_name='write-file')) IS NOT TRUE)
 OR (NEW.binding_kind='builtin' AND stable!~'^builtin:[a-z0-9_.-]{1,128}$') THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='tool_binding_identity', MESSAGE='invalid Tool binding identity';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- The original first-round shape, immutable payloads and 1MiB payload bounds
-- are not changed. The actual Execution writer still proves the exact captured
-- definition, binding, current permissions and original protected arguments.
ALTER TABLE agenteam_execution.tool_calls DROP CONSTRAINT tool_calls_operation_name_check;
ALTER TABLE agenteam_execution.tool_calls ADD CONSTRAINT tool_calls_operation_name_check
 CHECK(operation_name IN ('read-file','write-file'));

ALTER TABLE agenteam_tool.operations DROP CONSTRAINT tool_operation_kind;
ALTER TABLE agenteam_tool.operations ADD CONSTRAINT tool_operation_kind
 CHECK(operation_kind IN ('skill.install','runner.read-file','runner.write-file'));
ALTER TABLE agenteam_tool.operations DROP CONSTRAINT tool_operation_backend_shape;
ALTER TABLE agenteam_tool.operations ADD CONSTRAINT tool_operation_backend_shape CHECK((
 (operation_kind='skill.install' AND skill_id IS NOT NULL
  AND backend_key='tool.skill.install:'||operation_id::text)
 OR (operation_kind IN ('runner.read-file','runner.write-file') AND skill_id IS NULL AND backend_key IS NULL)
) IS TRUE);

-- This stores only the complete known-publication result, never data_base64 or
-- file contents. All Operation input/terminal and Attempt metadata remain 32KiB.
-- Path/state/bytes_written/durable/optional completion_code are encoded and
-- checked by the write-file codec against the original protected arguments.
CREATE TABLE agenteam_tool.write_file_results (
 operation_id agenteam_tool.safe_id NOT NULL,
 attempt_id agenteam_tool.safe_id NOT NULL,
 execution_id agenteam_tool.safe_id NOT NULL,
 project_id agenteam_tool.safe_id NOT NULL,
 agent_id agenteam_tool.safe_id NOT NULL,
 body_sha256 text NOT NULL CHECK(body_sha256 ~ '^sha256:[0-9a-f]{64}$'),
 body_bytes integer NOT NULL CHECK(body_bytes BETWEEN 2 AND 32768),
 body_data bytea NOT NULL CHECK(octet_length(body_data) BETWEEN 2 AND 32768),
 CHECK(body_bytes=octet_length(body_data)),
 CHECK(body_sha256='sha256:'||encode(sha256(body_data),'hex')),
 PRIMARY KEY(operation_id,attempt_id),
 FOREIGN KEY(attempt_id,operation_id,execution_id,project_id,agent_id)
  REFERENCES agenteam_tool.attempts(attempt_id,operation_id,execution_id,project_id,agent_id)
  ON DELETE RESTRICT
);

-- +goose StatementBegin
CREATE FUNCTION agenteam_tool.write_file_result_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='tool_write_file_result_immutable', MESSAGE='immutable write-file result';
END $$;
-- +goose StatementEnd
CREATE TRIGGER tool_write_file_result_immutable BEFORE UPDATE OR DELETE
 ON agenteam_tool.write_file_results FOR EACH ROW
 EXECUTE FUNCTION agenteam_tool.write_file_result_immutable();

-- Successful means known atomic publication, not guaranteed durability.
-- Non-success/Unknown/cancelled and other profiles cannot carry this payload.
-- These checks prove row linkage, not the actual call join or permissions.
-- +goose StatementBegin
CREATE FUNCTION agenteam_tool.check_write_file_result() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE target agenteam_tool.operations.operation_id%TYPE;
 k text; ph text; active agenteam_tool.operations.active_attempt_id%TYPE;
 matching bigint; total bigint;
BEGIN
 target:=NEW.operation_id;
 SELECT operation_kind,phase,active_attempt_id INTO STRICT k,ph,active
  FROM agenteam_tool.operations WHERE operation_id=target;
 SELECT count(*) INTO total FROM agenteam_tool.write_file_results WHERE operation_id=target;
 IF k='runner.write-file' AND ph='succeeded' THEN
  SELECT count(*) INTO matching FROM agenteam_tool.write_file_results r
   JOIN agenteam_tool.attempts a ON a.attempt_id=r.attempt_id AND a.operation_id=r.operation_id
    AND a.execution_id=r.execution_id AND a.project_id=r.project_id AND a.agent_id=r.agent_id
   WHERE r.operation_id=target AND r.attempt_id=active AND a.phase='succeeded' AND a.retired;
  IF total<>1 OR matching<>1 THEN
   RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='tool_write_file_result_complete', MESSAGE='incomplete write-file result';
  END IF;
 ELSIF total<>0 THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='tool_write_file_result_complete', MESSAGE='unexpected write-file result';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER tool_write_file_result_complete AFTER INSERT
 ON agenteam_tool.write_file_results DEFERRABLE INITIALLY DEFERRED
 FOR EACH ROW EXECUTE FUNCTION agenteam_tool.check_write_file_result();
CREATE CONSTRAINT TRIGGER tool_write_file_operation_complete AFTER INSERT OR UPDATE
 ON agenteam_tool.operations DEFERRABLE INITIALLY DEFERRED
 FOR EACH ROW EXECUTE FUNCTION agenteam_tool.check_write_file_result();
CREATE CONSTRAINT TRIGGER tool_write_file_attempt_complete AFTER INSERT OR UPDATE
 ON agenteam_tool.attempts DEFERRABLE INITIALLY DEFERRED
 FOR EACH ROW EXECUTE FUNCTION agenteam_tool.check_write_file_result();
