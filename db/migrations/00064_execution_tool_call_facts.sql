-- agenteam:transaction tx
-- +goose Up
-- A forward extension of the one canonical first-round/transcript writer.
-- Version 1 keeps precisely its old completion profile. Version 2 may stop at
-- a nonterminal tool-pending checkpoint; it grants no ToolOperation authority.
ALTER TABLE agenteam_execution.rounds DROP CONSTRAINT rounds_schema_version_check;
ALTER TABLE agenteam_execution.rounds ADD CONSTRAINT rounds_schema_version_check CHECK(schema_version IN (1,2));
ALTER TABLE agenteam_execution.rounds ADD COLUMN tool_pending boolean NOT NULL DEFAULT false;
ALTER TABLE agenteam_execution.rounds ADD COLUMN checkpoint_at timestamptz(6);
ALTER TABLE agenteam_execution.rounds ADD UNIQUE(execution_id,id,input_binding_id,call_id,snapshot_id);
ALTER TABLE agenteam_execution.rounds DROP CONSTRAINT execution_round_phase;
ALTER TABLE agenteam_execution.rounds ADD CONSTRAINT execution_round_phase CHECK((
 (NOT tool_pending AND checkpoint_at IS NULL AND (
   (terminal_status IS NULL AND terminal_version IS NULL AND terminal_reason IS NULL AND terminal_event_id IS NULL AND response IS NULL AND response_digest IS NULL AND finished_at IS NULL AND transcript_through=1)
   OR (terminal_status IS NOT NULL AND terminal_version IS NOT NULL AND terminal_event_id IS NOT NULL AND finished_at>=created_at
     AND ((schema_version=1 AND terminal_status='succeeded' AND terminal_reason='completed' AND transcript_through=2 AND response IS NOT NULL AND response_digest='sha256:'||encode(sha256(response),'hex'))
       OR (terminal_status='failed' AND terminal_reason IN ('model_failed','incomplete_response','runtime_failed') AND transcript_through=1 AND response IS NULL AND response_digest IS NULL)
       OR (terminal_status='cancelled' AND terminal_reason='cancelled' AND transcript_through=1 AND response IS NULL AND response_digest IS NULL)))
 )) OR (schema_version=2 AND tool_pending AND checkpoint_at>=created_at AND transcript_through=2
   AND terminal_status IS NULL AND terminal_version IS NULL AND terminal_reason IS NULL AND terminal_event_id IS NULL AND finished_at IS NULL
   AND response IS NOT NULL AND response_digest='sha256:'||encode(sha256(response),'hex'))
) IS TRUE);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_execution.guard_round_completion() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='execution_round_immutable', MESSAGE='immutable execution round';
 END IF;
 IF OLD.terminal_status IS NOT NULL OR OLD.tool_pending OR
 ROW(NEW.id,NEW.execution_id,NEW.snapshot_id,NEW.start_id,NEW.input_binding_id,NEW.call_id,NEW.input_digest,NEW.context_digest,NEW.schema_version,NEW.content,NEW.digest,NEW.created_at)
 IS DISTINCT FROM ROW(OLD.id,OLD.execution_id,OLD.snapshot_id,OLD.start_id,OLD.input_binding_id,OLD.call_id,OLD.input_digest,OLD.context_digest,OLD.schema_version,OLD.content,OLD.digest,OLD.created_at)
 OR NOT ((NEW.terminal_status IS NOT NULL AND NOT NEW.tool_pending)
   OR (OLD.schema_version=2 AND NEW.tool_pending AND NEW.terminal_status IS NULL)) THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='execution_round_immutable', MESSAGE='immutable execution round';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TABLE agenteam_execution.tool_call_arguments (
 id agenteam_execution.safe_id PRIMARY KEY,
 execution_id agenteam_execution.safe_id NOT NULL,
 round_id agenteam_execution.safe_id NOT NULL,
 input_binding_id agenteam_execution.safe_id NOT NULL,
 logical_call_id agenteam_execution.safe_id NOT NULL,
 snapshot_id agenteam_execution.safe_id NOT NULL,
 project_id agenteam_execution.safe_id NOT NULL,
 agent_id agenteam_execution.safe_id NOT NULL,
 original bytea NOT NULL CHECK(octet_length(original) BETWEEN 1 AND 1048576),
 original_digest text NOT NULL CHECK(original_digest='sha256:'||encode(sha256(original),'hex')),
 canonical bytea NOT NULL CHECK(octet_length(canonical) BETWEEN 2 AND 1048576),
 canonical_digest text NOT NULL CHECK(canonical_digest='sha256:'||encode(sha256(canonical),'hex')),
 created_at timestamptz(6) NOT NULL,
 UNIQUE(execution_id,round_id,id),
 FOREIGN KEY(execution_id,project_id,agent_id) REFERENCES agenteam_execution.executions(id,project_id,agent_id) ON DELETE RESTRICT,
 FOREIGN KEY(execution_id,round_id,input_binding_id,logical_call_id,snapshot_id) REFERENCES agenteam_execution.rounds(execution_id,id,input_binding_id,call_id,snapshot_id) ON DELETE RESTRICT,
 CHECK(jsonb_typeof(convert_from(original,'UTF8')::jsonb)='object'),
 CHECK(convert_from(original,'UTF8')::jsonb=convert_from(canonical,'UTF8')::jsonb)
);
CREATE TABLE agenteam_execution.tool_calls (
 execution_id agenteam_execution.safe_id PRIMARY KEY,
 round_id agenteam_execution.safe_id NOT NULL,
 invocation_id agenteam_execution.safe_id NOT NULL,
 call_id text NOT NULL CHECK(octet_length(call_id) BETWEEN 1 AND 256),
 model_visible_name text NOT NULL CHECK(model_visible_name ~ '^tool_[0-9a-f]{32}$'),
 tool_id agenteam_execution.safe_id NOT NULL,
 spec_revision bigint NOT NULL CHECK(spec_revision>=1),
 operation_name text NOT NULL CHECK(operation_name='read-file'),
 operation_revision text NOT NULL CHECK(operation_revision='1'),
 protocol_major smallint NOT NULL CHECK(protocol_major=1),
 payload_id agenteam_execution.safe_id NOT NULL UNIQUE,
 checkpoint_version bigint NOT NULL CHECK(checkpoint_version>=3),
 created_at timestamptz(6) NOT NULL,
 UNIQUE(execution_id,round_id,invocation_id,call_id),
 FOREIGN KEY(execution_id,round_id,payload_id) REFERENCES agenteam_execution.tool_call_arguments(execution_id,round_id,id) ON DELETE RESTRICT
);
CREATE TRIGGER execution_tool_arguments_immutable BEFORE UPDATE OR DELETE ON agenteam_execution.tool_call_arguments FOR EACH ROW EXECUTE FUNCTION agenteam_execution.guard_direct_text_immutable();
CREATE TRIGGER execution_tool_calls_immutable BEFORE UPDATE OR DELETE ON agenteam_execution.tool_calls FOR EACH ROW EXECUTE FUNCTION agenteam_execution.guard_direct_text_immutable();
