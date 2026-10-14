-- agenteam:transaction tx
-- +goose Up
ALTER TABLE agenteam_tool.operations DROP CONSTRAINT tool_operation_kind;
ALTER TABLE agenteam_tool.operations ADD CONSTRAINT tool_operation_kind CHECK(operation_kind IN
 ('skill.install','runner.read-file','runner.write-file','runner.run-command','task.read','task.transfer','artifact.list','artifact.create','artifact.read'));
ALTER TABLE agenteam_tool.operations DROP CONSTRAINT tool_operation_backend_shape;
ALTER TABLE agenteam_tool.operations ADD CONSTRAINT tool_operation_backend_shape CHECK((
 (operation_kind='skill.install' AND skill_id IS NOT NULL AND backend_key='tool.skill.install:'||operation_id::text)
 OR (operation_kind IN ('runner.read-file','runner.write-file','runner.run-command','task.read','task.transfer','artifact.list','artifact.create','artifact.read') AND skill_id IS NULL AND backend_key IS NULL)
) IS TRUE);

CREATE TABLE agenteam_tool.builtin_results (
 operation_id agenteam_tool.safe_id NOT NULL,
 attempt_id agenteam_tool.safe_id NOT NULL,
 execution_id agenteam_tool.safe_id NOT NULL,
 project_id agenteam_tool.safe_id NOT NULL,
 agent_id agenteam_tool.safe_id NOT NULL,
 request_id agenteam_tool.safe_id NOT NULL,
 body_sha256 text NOT NULL CHECK(body_sha256 ~ '^sha256:[0-9a-f]{64}$'),
 body_bytes integer NOT NULL CHECK(body_bytes BETWEEN 2 AND 1048576),
 body_data bytea NOT NULL CHECK(octet_length(body_data) BETWEEN 2 AND 1048576),
 PRIMARY KEY(operation_id,attempt_id),
 CHECK(body_bytes=octet_length(body_data)),
 CHECK(body_sha256='sha256:'||encode(sha256(body_data),'hex')),
 FOREIGN KEY(attempt_id,operation_id,execution_id,project_id,agent_id,request_id)
 REFERENCES agenteam_tool.attempts(attempt_id,operation_id,execution_id,project_id,agent_id,backend_request_id)
);

-- +goose StatementBegin
CREATE FUNCTION agenteam_tool.builtin_result_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='tool_builtin_result_immutable',MESSAGE='immutable builtin result';
END $$;
-- +goose StatementEnd
CREATE TRIGGER tool_builtin_result_immutable BEFORE UPDATE OR DELETE ON agenteam_tool.builtin_results FOR EACH ROW EXECUTE FUNCTION agenteam_tool.builtin_result_immutable();

-- A domain call must have returned before the Tool owner writes this complete
-- result and retires its actual Attempt. SQL binds their atomic persistence;
-- it does not manufacture the in-process return/join proof.
-- +goose StatementBegin
CREATE FUNCTION agenteam_tool.check_builtin_result() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE target agenteam_tool.operations.operation_id%TYPE; k text; ph text; active agenteam_tool.operations.active_attempt_id%TYPE; total bigint; matching bigint;
BEGIN
 target:=NEW.operation_id;
 SELECT operation_kind,phase,active_attempt_id INTO STRICT k,ph,active FROM agenteam_tool.operations WHERE operation_id=target;
 SELECT count(*) INTO total FROM agenteam_tool.builtin_results WHERE operation_id=target;
 IF k IN ('task.read','task.transfer','artifact.list','artifact.create','artifact.read') AND ph IN ('succeeded','failed','cancelled') THEN
  SELECT count(*) INTO matching FROM agenteam_tool.builtin_results r JOIN agenteam_tool.attempts a
   ON(a.attempt_id,a.operation_id,a.execution_id,a.project_id,a.agent_id,a.backend_request_id)=(r.attempt_id,r.operation_id,r.execution_id,r.project_id,r.agent_id,r.request_id)
   WHERE r.operation_id=target AND r.attempt_id=active AND a.phase=ph AND a.retired;
  IF total<>1 OR matching<>1 THEN RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='tool_builtin_result_complete',MESSAGE='incomplete builtin result'; END IF;
 ELSIF total<>0 THEN
  RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='tool_builtin_result_complete',MESSAGE='unexpected builtin result';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER tool_builtin_result_complete AFTER INSERT ON agenteam_tool.builtin_results DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION agenteam_tool.check_builtin_result();
CREATE CONSTRAINT TRIGGER tool_builtin_operation_complete AFTER INSERT OR UPDATE ON agenteam_tool.operations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION agenteam_tool.check_builtin_result();
CREATE CONSTRAINT TRIGGER tool_builtin_attempt_complete AFTER INSERT OR UPDATE ON agenteam_tool.attempts DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION agenteam_tool.check_builtin_result();
