-- agenteam:transaction tx
-- +goose Up
-- Add only the run-command@1 metadata/call/Operation profile. Existing immutable
-- Builtin/read definitions, captures, input encodings, digests and bounds stay
-- unchanged. No registration, default binding or execution grant is inserted.
ALTER TABLE agenteam_tool.identities DROP CONSTRAINT tool_identity_stable_key;
ALTER TABLE agenteam_tool.identities ADD CONSTRAINT tool_identity_stable_key
 CHECK(stable_key ~ '^builtin:[a-z0-9_.-]{1,128}$'
  OR stable_key IN ('runner:read-file','runner:write-file','runner:run-command'));

ALTER TABLE agenteam_tool.registrations DROP CONSTRAINT tool_registration_binding;
ALTER TABLE agenteam_tool.registrations ADD CONSTRAINT tool_registration_binding CHECK((
 (binding_kind='builtin' AND handler_id IS NOT NULL AND contract_revision IS NOT NULL
  AND operation_name IS NULL AND operation_revision IS NULL AND protocol_major IS NULL)
 OR (binding_kind='runner' AND handler_id IS NULL AND contract_revision IS NULL
  AND operation_revision='1' AND protocol_major=1 AND class='ordinary'
  AND ((operation_name='read-file' AND scope_resolver_id='runner.read-file' AND risk_classifier_id='runner.read-file')
   OR (operation_name='write-file' AND scope_resolver_id='runner.write-file' AND risk_classifier_id='runner.write-file')
   OR (operation_name='run-command' AND scope_resolver_id='runner.run-command' AND risk_classifier_id='runner.run-command')))
) IS TRUE);
ALTER TABLE agenteam_tool.execution_references DROP CONSTRAINT tool_execution_reference_binding;
ALTER TABLE agenteam_tool.execution_references ADD CONSTRAINT tool_execution_reference_binding CHECK((
 (binding_kind='builtin' AND handler_id IS NOT NULL AND contract_revision IS NOT NULL
  AND operation_name IS NULL AND operation_revision IS NULL AND protocol_major IS NULL)
 OR (binding_kind='runner' AND handler_id IS NULL AND contract_revision IS NULL
  AND operation_revision='1' AND protocol_major=1 AND class='ordinary'
  AND ((operation_name='read-file' AND scope_resolver_id='runner.read-file' AND risk_classifier_id='runner.read-file')
   OR (operation_name='write-file' AND scope_resolver_id='runner.write-file' AND risk_classifier_id='runner.write-file')
   OR (operation_name='run-command' AND scope_resolver_id='runner.run-command' AND risk_classifier_id='runner.run-command')))
) IS TRUE);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_tool.check_runner_binding_identity() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE stable text;
BEGIN
 SELECT stable_key INTO STRICT stable FROM agenteam_tool.identities WHERE tool_id=NEW.tool_id;
 IF (NEW.binding_kind='runner' AND (
    (stable='runner:read-file' AND NEW.operation_name='read-file')
    OR (stable='runner:write-file' AND NEW.operation_name='write-file')
    OR (stable='runner:run-command' AND NEW.operation_name='run-command')) IS NOT TRUE)
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
 CHECK(operation_name IN ('read-file','write-file','run-command'));

ALTER TABLE agenteam_tool.operations DROP CONSTRAINT tool_operation_kind;
ALTER TABLE agenteam_tool.operations ADD CONSTRAINT tool_operation_kind
 CHECK(operation_kind IN ('skill.install','runner.read-file','runner.write-file','runner.run-command'));
ALTER TABLE agenteam_tool.operations DROP CONSTRAINT tool_operation_backend_shape;
ALTER TABLE agenteam_tool.operations ADD CONSTRAINT tool_operation_backend_shape CHECK((
 (operation_kind='skill.install' AND skill_id IS NOT NULL
  AND backend_key='tool.skill.install:'||operation_id::text)
 OR (operation_kind IN ('runner.read-file','runner.write-file','runner.run-command') AND skill_id IS NULL AND backend_key IS NULL)
) IS TRUE);

-- The original terminal/Attempt metadata budget remains 32KiB. Each output
-- stream has its own 64KiB budget. No argv or environment value is stored here.
-- The composite RID key prevents attaching another request's complete report.
ALTER TABLE agenteam_tool.attempts ADD CONSTRAINT tool_attempt_request_identity
 UNIQUE(attempt_id,operation_id,execution_id,project_id,agent_id,backend_request_id);

CREATE TABLE agenteam_tool.command_results (
 operation_id agenteam_tool.safe_id NOT NULL,
 attempt_id agenteam_tool.safe_id NOT NULL,
 execution_id agenteam_tool.safe_id NOT NULL,
 project_id agenteam_tool.safe_id NOT NULL,
 agent_id agenteam_tool.safe_id NOT NULL,
 request_id agenteam_tool.safe_id NOT NULL,
 body_sha256 text NOT NULL CHECK(body_sha256 ~ '^sha256:[0-9a-f]{64}$'),
 body_bytes integer NOT NULL CHECK(body_bytes BETWEEN 2 AND 32768),
 body_data bytea NOT NULL CHECK(octet_length(body_data) BETWEEN 2 AND 32768),
 stdout_sha256 text NOT NULL CHECK(stdout_sha256 ~ '^sha256:[0-9a-f]{64}$'),
 stdout_bytes integer NOT NULL CHECK(stdout_bytes BETWEEN 0 AND 65536),
 stdout_data bytea NOT NULL CHECK(octet_length(stdout_data) BETWEEN 0 AND 65536),
 stderr_sha256 text NOT NULL CHECK(stderr_sha256 ~ '^sha256:[0-9a-f]{64}$'),
 stderr_bytes integer NOT NULL CHECK(stderr_bytes BETWEEN 0 AND 65536),
 stderr_data bytea NOT NULL CHECK(octet_length(stderr_data) BETWEEN 0 AND 65536),
 CHECK(body_bytes=octet_length(body_data)),
 CHECK(body_sha256='sha256:'||encode(sha256(body_data),'hex')),
 CHECK(stdout_bytes=octet_length(stdout_data)),
 CHECK(stdout_sha256='sha256:'||encode(sha256(stdout_data),'hex')),
 CHECK(stderr_bytes=octet_length(stderr_data)),
 CHECK(stderr_sha256='sha256:'||encode(sha256(stderr_data),'hex')),
 PRIMARY KEY(operation_id,attempt_id),
 FOREIGN KEY(attempt_id,operation_id,execution_id,project_id,agent_id,request_id)
  REFERENCES agenteam_tool.attempts(attempt_id,operation_id,execution_id,project_id,agent_id,backend_request_id)
  ON DELETE RESTRICT
);

-- +goose StatementBegin
CREATE FUNCTION agenteam_tool.command_result_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='tool_command_result_immutable', MESSAGE='immutable command result';
END $$;
-- +goose StatementEnd
CREATE TRIGGER tool_command_result_immutable BEFORE UPDATE OR DELETE
 ON agenteam_tool.command_results FOR EACH ROW
 EXECUTE FUNCTION agenteam_tool.command_result_immutable();

-- A complete known invocation report may accompany success, a nonzero exit,
-- signal, timeout, cancellation or local I/O error. It is not business success.
-- report_available is the command-only explicit original terminal assertion;
-- Unknown and pre-start failures cannot fabricate a report. These structural
-- constraints do not prove actual Wait, healthy wire, current rights or UTF-8;
-- the real producer/consumer must establish those facts before the final Tx.
-- +goose StatementBegin
CREATE FUNCTION agenteam_tool.check_command_result() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE target agenteam_tool.operations.operation_id%TYPE;
 k text; ph text; result_outcome text; active agenteam_tool.operations.active_attempt_id%TYPE;
 terminal bytea; terminal_json jsonb; available boolean;
 matching bigint; total bigint;
BEGIN
 target:=NEW.operation_id;
 SELECT operation_kind,phase,outcome,active_attempt_id,terminal_data
  INTO STRICT k,ph,result_outcome,active,terminal
  FROM agenteam_tool.operations WHERE operation_id=target;
 SELECT count(*) INTO total FROM agenteam_tool.command_results WHERE operation_id=target;
 IF k='runner.run-command' AND ph IN ('succeeded','failed','cancelled') THEN
  terminal_json:=convert_from(terminal,'UTF8')::jsonb;
  IF jsonb_typeof(terminal_json->'report_available') IS DISTINCT FROM 'boolean' THEN
   RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='tool_command_result_complete', MESSAGE='missing command report status';
  END IF;
  available:=(terminal_json->>'report_available')::boolean;
  IF (ph='succeeded' AND NOT available) OR (result_outcome='unknown' AND available) THEN
   RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='tool_command_result_complete', MESSAGE='invalid command report status';
  END IF;
  IF available THEN
   SELECT count(*) INTO matching FROM agenteam_tool.command_results r
    JOIN agenteam_tool.attempts a ON a.attempt_id=r.attempt_id AND a.operation_id=r.operation_id
     AND a.execution_id=r.execution_id AND a.project_id=r.project_id AND a.agent_id=r.agent_id
     AND a.backend_request_id=r.request_id
    WHERE r.operation_id=target AND r.attempt_id=active AND a.phase=ph AND a.retired
     AND a.outcome_data=terminal;
   IF total<>1 OR matching<>1 THEN
    RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='tool_command_result_complete', MESSAGE='incomplete command result';
   END IF;
  ELSIF total<>0 THEN
   RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='tool_command_result_complete', MESSAGE='unexpected command result';
  END IF;
 ELSIF total<>0 THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='tool_command_result_complete', MESSAGE='unexpected command result';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER tool_command_result_complete AFTER INSERT
 ON agenteam_tool.command_results DEFERRABLE INITIALLY DEFERRED
 FOR EACH ROW EXECUTE FUNCTION agenteam_tool.check_command_result();
CREATE CONSTRAINT TRIGGER tool_command_operation_complete AFTER INSERT OR UPDATE
 ON agenteam_tool.operations DEFERRABLE INITIALLY DEFERRED
 FOR EACH ROW EXECUTE FUNCTION agenteam_tool.check_command_result();
CREATE CONSTRAINT TRIGGER tool_command_attempt_complete AFTER INSERT OR UPDATE
 ON agenteam_tool.attempts DEFERRABLE INITIALLY DEFERRED
 FOR EACH ROW EXECUTE FUNCTION agenteam_tool.check_command_result();
