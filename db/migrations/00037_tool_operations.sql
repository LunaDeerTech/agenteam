-- agenteam:transaction tx
-- +goose Up
-- Tool owns logical calls/attempts. Execution's real owner is a mandatory Go
-- boundary; migration 38 may add the composite Execution FK after creating its
-- canonical table. Never create a placeholder Execution or reference an absent
-- table here. Current runtime writes only verified `created` metadata.
CREATE TABLE agenteam_tool.operations (
 operation_id agenteam_tool.safe_id PRIMARY KEY,
 project_id agenteam_tool.safe_id NOT NULL,
 agent_id agenteam_tool.safe_id NOT NULL,
 execution_id agenteam_tool.safe_id NOT NULL,
 logical_call_id agenteam_tool.safe_id NOT NULL,
 invocation_id agenteam_tool.safe_id NOT NULL,
 call_id text NOT NULL CHECK(octet_length(call_id) BETWEEN 1 AND 256),
 tool_id agenteam_tool.safe_id NOT NULL,
 spec_revision bigint NOT NULL CHECK(spec_revision>0),
 -- Canonical identity, payload reference and digests only, never arguments or
-- text_files contents. Every read validates exact encoding and all columns.
 input_data bytea NOT NULL CHECK(octet_length(input_data) BETWEEN 2 AND 32768),
 input_digest text NOT NULL CHECK(input_digest ~ '^sha256:[0-9a-f]{64}$'),
 skill_id agenteam_tool.safe_id NOT NULL,
 backend_key text NOT NULL CHECK(backend_key='tool.skill.install:'||operation_id::text),
 fingerprint text NOT NULL CHECK(fingerprint ~ '^sha256:[0-9a-f]{64}$'),
 phase text NOT NULL CHECK(phase IN ('created','authorizing','waiting_for_approval','ready','running','succeeded','failed','cancelled')),
 version bigint NOT NULL CHECK(version>0),
 active_attempt_id agenteam_tool.safe_id,
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 completed_at timestamptz(6),
 outcome text CHECK(outcome IN ('success','error','cancelled','unknown')),
 terminal_data bytea CHECK(octet_length(terminal_data) BETWEEN 2 AND 32768),
 CHECK((phase IN ('created','authorizing','waiting_for_approval','ready','running') AND completed_at IS NULL AND outcome IS NULL AND terminal_data IS NULL)
    OR (phase IN ('succeeded','failed','cancelled') AND completed_at IS NOT NULL AND completed_at>=created_at AND outcome IS NOT NULL AND terminal_data IS NOT NULL)),
 CHECK(phase<>'running' OR active_attempt_id IS NOT NULL),
 CHECK(phase<>'succeeded' OR outcome='success'),
 CHECK(phase<>'cancelled' OR outcome='cancelled'),
 CHECK(phase<>'failed' OR outcome IN ('error','unknown')),
 UNIQUE(execution_id,logical_call_id,invocation_id,call_id),
 UNIQUE(operation_id,execution_id,project_id,agent_id),
 FOREIGN KEY(tool_id,spec_revision) REFERENCES agenteam_tool.spec_revisions(tool_id,spec_revision)
);
CREATE INDEX tool_operation_execution_parent ON agenteam_tool.operations(execution_id,project_id,agent_id,operation_id);
CREATE INDEX tool_operation_project_active ON agenteam_tool.operations(project_id,operation_id) WHERE completed_at IS NULL;
CREATE INDEX tool_operation_spec ON agenteam_tool.operations(tool_id,spec_revision,operation_id);

CREATE TABLE agenteam_tool.attempts (
 attempt_id agenteam_tool.safe_id PRIMARY KEY,
 operation_id agenteam_tool.safe_id NOT NULL,
 execution_id agenteam_tool.safe_id NOT NULL,
 project_id agenteam_tool.safe_id NOT NULL,
 agent_id agenteam_tool.safe_id NOT NULL,
 ordinal bigint NOT NULL CHECK(ordinal>0),
 process_id agenteam_tool.safe_id NOT NULL,
 fence bigint NOT NULL CHECK(fence>0),
 backend_request_id agenteam_tool.safe_id NOT NULL,
 phase text NOT NULL CHECK(phase IN ('prepared','running','succeeded','failed','cancelled','unknown')),
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 completed_at timestamptz(6),
 retired boolean NOT NULL DEFAULT false,
 outcome_data bytea CHECK(octet_length(outcome_data) BETWEEN 2 AND 32768),
 CHECK((phase IN ('prepared','running') AND completed_at IS NULL AND outcome_data IS NULL AND NOT retired)
    OR (phase IN ('succeeded','failed','cancelled','unknown') AND completed_at IS NOT NULL AND completed_at>=created_at AND outcome_data IS NOT NULL)),
 UNIQUE(operation_id,ordinal),
 UNIQUE(attempt_id,operation_id,execution_id,project_id,agent_id),
 FOREIGN KEY(operation_id,execution_id,project_id,agent_id) REFERENCES agenteam_tool.operations(operation_id,execution_id,project_id,agent_id)
);
CREATE UNIQUE INDEX tool_attempt_active ON agenteam_tool.attempts(operation_id) WHERE phase IN ('prepared','running');
CREATE INDEX tool_attempt_parent ON agenteam_tool.attempts(operation_id,execution_id,project_id,agent_id,ordinal);
CREATE INDEX tool_attempt_unretired ON agenteam_tool.attempts(process_id,attempt_id) WHERE NOT retired;
ALTER TABLE agenteam_tool.operations ADD CONSTRAINT tool_active_attempt_parent
 FOREIGN KEY(active_attempt_id,operation_id,execution_id,project_id,agent_id)
 REFERENCES agenteam_tool.attempts(attempt_id,operation_id,execution_id,project_id,agent_id)
 DEFERRABLE INITIALLY DEFERRED;

-- +goose StatementBegin
CREATE FUNCTION agenteam_tool.operation_identity_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ROW(NEW.operation_id,NEW.project_id,NEW.agent_id,NEW.execution_id,NEW.logical_call_id,NEW.invocation_id,NEW.call_id,NEW.tool_id,NEW.spec_revision,NEW.input_data,NEW.input_digest,NEW.skill_id,NEW.backend_key,NEW.fingerprint,NEW.created_at)
 IS DISTINCT FROM ROW(OLD.operation_id,OLD.project_id,OLD.agent_id,OLD.execution_id,OLD.logical_call_id,OLD.invocation_id,OLD.call_id,OLD.tool_id,OLD.spec_revision,OLD.input_data,OLD.input_digest,OLD.skill_id,OLD.backend_key,OLD.fingerprint,OLD.created_at)
 OR NEW.version<OLD.version THEN
  RAISE EXCEPTION 'TOOL_OPERATION_IDENTITY_IMMUTABLE' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER tool_operation_identity_immutable BEFORE UPDATE ON agenteam_tool.operations
 FOR EACH ROW EXECUTE FUNCTION agenteam_tool.operation_identity_immutable();
