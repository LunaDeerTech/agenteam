-- agenteam:transaction tx
-- +goose Up
-- Complete atomic capture input. This is neither a sealed Snapshot nor proof
-- that Execution entered running. Provider references/leases and this row are
-- committed by the same original preparation transaction.
CREATE TABLE agenteam_execution.preparation_inputs (
 execution_id agenteam_execution.safe_id PRIMARY KEY,
 project_id agenteam_execution.safe_id NOT NULL,
 agent_id agenteam_execution.safe_id NOT NULL,
 attempt_id agenteam_execution.safe_id NOT NULL,
 process_id agenteam_execution.safe_id NOT NULL,
 fence bigint NOT NULL CHECK(fence>=1),
 launch_digest text NOT NULL CHECK(launch_digest ~ '^sha256:[0-9a-f]{64}$'),
 request_id agenteam_execution.safe_id NOT NULL,
 command_identity text NOT NULL CHECK(octet_length(command_identity) BETWEEN 1 AND 4096),
 attempt_binding text NOT NULL CHECK(attempt_binding ~ '^sha256:[0-9a-f]{64}$'),
 schema_version bigint NOT NULL CHECK(schema_version=1),
 input bytea NOT NULL CHECK(octet_length(input) BETWEEN 2 AND 8388608),
 input_digest text NOT NULL CHECK(input_digest='sha256:'||encode(sha256(input),'hex')),
 captured_at timestamptz(6) NOT NULL,
 FOREIGN KEY(execution_id,project_id,agent_id)
  REFERENCES agenteam_execution.executions(id,project_id,agent_id) ON DELETE RESTRICT,
 FOREIGN KEY(execution_id,attempt_id,process_id,fence)
  REFERENCES agenteam_execution.preparation_attempts(execution_id,attempt_id,process_id,fence) ON DELETE RESTRICT,
 CONSTRAINT preparation_input_object CHECK(jsonb_typeof(convert_from(input,'UTF8')::jsonb)='object')
);
CREATE INDEX preparation_inputs_project ON agenteam_execution.preparation_inputs(project_id,execution_id);
CREATE INDEX preparation_inputs_attempt ON agenteam_execution.preparation_inputs(execution_id,attempt_id,process_id,fence);

-- +goose StatementBegin
CREATE FUNCTION agenteam_execution.guard_preparation_input() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='preparation_input_immutable', MESSAGE='immutable preparation input';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER preparation_input_immutable BEFORE UPDATE ON agenteam_execution.preparation_inputs
 FOR EACH ROW EXECUTE FUNCTION agenteam_execution.guard_preparation_input();
