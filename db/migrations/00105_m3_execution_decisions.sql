-- agenteam:transaction tx
-- +goose Up
ALTER TABLE agenteam_execution.tool_calls DROP CONSTRAINT tool_calls_operation_name_check;
ALTER TABLE agenteam_execution.tool_calls ADD CONSTRAINT tool_calls_operation_name_check
 CHECK(operation_name IN ('read-file','write-file','run-command','task.read','task.transfer','artifact.list','artifact.create','artifact.read','knowledge.query','knowledge.read','memory.retain','memory.recall','memory.reflect','meeting.list','meeting.request','meeting.add-agent','meeting.remove-agent','meeting.update','meeting.comment','meeting.request-decision','mcp.list-resources','mcp.read-resource','mcp.call'));

ALTER TABLE agenteam_execution.executions DROP CONSTRAINT execution_waiting_tuple;
ALTER TABLE agenteam_execution.executions ADD CONSTRAINT execution_waiting_tuple CHECK((
 (status='waiting' AND waiting_reason IN ('approval','decision') AND waiting_reference IS NOT NULL
  AND waiting_at IS NOT NULL AND waiting_at>=started_at
  AND (waiting_reason<>'decision' OR launch_request#>>'{trigger,kind}'='meeting'))
 OR (status<>'waiting' AND waiting_reason IS NULL AND waiting_reference IS NULL AND waiting_at IS NULL)
) IS TRUE);

ALTER TABLE agenteam_meeting.decision_requests ADD CONSTRAINT meeting_decision_execution
 FOREIGN KEY(execution_id) REFERENCES agenteam_execution.executions(id);
CREATE TABLE agenteam_execution.loop_decision_results (
 execution_id agenteam_execution.safe_id NOT NULL REFERENCES agenteam_execution.executions(id),
 round_id agenteam_execution.safe_id NOT NULL REFERENCES agenteam_execution.rounds(id),
 decision_id agenteam_execution.safe_id NOT NULL REFERENCES agenteam_meeting.decision_requests(id),
 operation_id agenteam_execution.safe_id NOT NULL,
 decision_version bigint NOT NULL CHECK(decision_version>0),
 call_id text NOT NULL CHECK(octet_length(call_id) BETWEEN 1 AND 256),
 message_digest text NOT NULL CHECK(message_digest ~ '^sha256:[0-9a-f]{64}$'),
 transcript_sequence bigint NOT NULL CHECK(transcript_sequence>0),
 checkpoint_version bigint NOT NULL CHECK(checkpoint_version>0),
 created_at timestamptz(6) NOT NULL,
 PRIMARY KEY(execution_id,round_id), UNIQUE(decision_id), UNIQUE(operation_id)
);
-- +goose StatementBegin
CREATE FUNCTION agenteam_execution.guard_decision_result() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' AND agenteam_execution.project_retirement_allowed(OLD.execution_id::text) THEN RETURN OLD; END IF;
 RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='immutable execution decision result';
END $$;
-- +goose StatementEnd
CREATE TRIGGER execution_decision_result_immutable BEFORE UPDATE OR DELETE ON agenteam_execution.loop_decision_results
 FOR EACH ROW EXECUTE FUNCTION agenteam_execution.guard_decision_result();
