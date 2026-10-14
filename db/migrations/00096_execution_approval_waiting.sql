-- agenteam:transaction tx
-- +goose Up
ALTER TABLE agenteam_execution.executions
 ADD COLUMN waiting_reason text,
 ADD COLUMN waiting_reference agenteam_execution.safe_id,
 ADD COLUMN waiting_at timestamptz(6),
 ADD CONSTRAINT execution_waiting_tuple CHECK (
  (status='waiting' AND waiting_reason IS NOT NULL AND waiting_reason='approval' AND waiting_reference IS NOT NULL
   AND waiting_at IS NOT NULL AND waiting_at>=started_at)
  OR (status<>'waiting' AND waiting_reason IS NULL AND waiting_reference IS NULL AND waiting_at IS NULL));

ALTER TABLE agenteam_execution.tool_calls DROP CONSTRAINT tool_calls_operation_name_check;
ALTER TABLE agenteam_execution.tool_calls ADD CONSTRAINT tool_calls_operation_name_check CHECK
 (operation_name IN ('read-file','write-file','run-command','task.read','task.transfer','artifact.list','artifact.create','artifact.read'));

-- Approval rejection is a real zero-Attempt result. Keep its authority separate
-- from loop_tool_results, whose Attempt/Runner terminal binding is mandatory.
CREATE TABLE agenteam_execution.loop_approval_results (
 execution_id agenteam_execution.safe_id NOT NULL REFERENCES agenteam_execution.executions(id),
 round_id agenteam_execution.safe_id NOT NULL REFERENCES agenteam_execution.rounds(id),
 approval_id agenteam_execution.safe_id NOT NULL,
 operation_id agenteam_execution.safe_id NOT NULL,
 fingerprint text NOT NULL CHECK(fingerprint ~ '^sha256:[0-9a-f]{64}$'),
 call_id text NOT NULL CHECK(octet_length(call_id) BETWEEN 1 AND 256),
 message_digest text NOT NULL CHECK(message_digest ~ '^sha256:[0-9a-f]{64}$'),
 transcript_sequence bigint NOT NULL CHECK(transcript_sequence>0),
 checkpoint_version bigint NOT NULL CHECK(checkpoint_version>0),
 created_at timestamptz(6) NOT NULL,
 PRIMARY KEY(execution_id,round_id),
 UNIQUE(approval_id), UNIQUE(operation_id)
);
-- +goose StatementBegin
CREATE FUNCTION agenteam_execution.guard_approval_result() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' AND agenteam_execution.project_retirement_allowed(OLD.execution_id::text) THEN RETURN OLD; END IF;
 RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='immutable execution approval result';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER execution_approval_result_immutable BEFORE UPDATE OR DELETE ON agenteam_execution.loop_approval_results
 FOR EACH ROW EXECUTE FUNCTION agenteam_execution.guard_approval_result();
