-- agenteam:transaction tx
-- +goose Up
-- A rejected approval has no Attempt. Its successor must consume the exact
-- durable denial in the same transaction, while ordinary Tool results retain
-- the original required Attempt/result provenance.
-- +goose StatementBegin
CREATE FUNCTION agenteam_execution.require_loop_approval_pair(execution text, round text) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS (
  SELECT 1
  FROM agenteam_execution.loop_approval_results q
  JOIN agenteam_execution.rounds r ON r.execution_id=q.execution_id AND r.id=q.round_id
  JOIN agenteam_execution.tool_calls tc ON tc.execution_id=r.execution_id AND tc.round_id=r.id AND tc.call_id=q.call_id
  JOIN agenteam_execution.executions e ON e.id=q.execution_id
  JOIN agenteam_governance.approval_requests a ON a.id=q.approval_id AND a.operation_id=q.operation_id AND a.fingerprint=q.fingerprint
  JOIN agenteam_tool.approval_denials d ON d.operation_id=q.operation_id AND d.request_id=q.approval_id AND d.fingerprint=q.fingerprint
  JOIN agenteam_tool.operations o ON o.operation_id=q.operation_id AND o.fingerprint=q.fingerprint
  JOIN agenteam_execution.transcript_entries t ON t.execution_id=q.execution_id AND t.round_id=q.round_id AND t.sequence=q.transcript_sequence
  JOIN agenteam_execution.rounds n ON n.execution_id=r.execution_id AND n.predecessor_round_id=r.id
  WHERE q.execution_id=execution AND q.round_id=round
   AND r.schema_version=4 AND r.tool_pending AND r.terminal_status IS NULL
   AND tc.operation_name='run-command' AND tc.operation_revision='1' AND tc.protocol_major=1
   AND a.status='rejected' AND a.version=2 AND a.project_id=e.project_id AND a.agent_id=e.agent_id
   AND a.execution_id=r.execution_id AND a.round_id=r.id AND a.tool_call_id=tc.call_id AND a.tool_id=tc.tool_id
   AND o.project_id=e.project_id AND o.agent_id=e.agent_id AND o.execution_id=r.execution_id
   AND o.logical_call_id=r.call_id AND o.invocation_id=tc.invocation_id AND o.call_id=tc.call_id
   AND o.tool_id=tc.tool_id AND o.spec_revision=tc.spec_revision AND o.operation_kind='runner.run-command'
   AND o.phase='failed' AND o.version=2 AND o.active_attempt_id IS NULL AND o.outcome='error'
   AND convert_from(o.terminal_data,'UTF8')::jsonb=jsonb_build_object('kind','approval_rejected','request_id',q.approval_id,'report_available',false)
   AND NOT EXISTS(SELECT 1 FROM agenteam_tool.attempts attempt WHERE attempt.operation_id=q.operation_id)
   AND NOT EXISTS(SELECT 1 FROM agenteam_execution.loop_tool_results normal WHERE normal.execution_id=q.execution_id AND normal.round_id=q.round_id)
   AND q.transcript_sequence=r.transcript_through+1 AND q.checkpoint_version>tc.checkpoint_version
   AND t.loop_entry AND t.kind='tool_result' AND t.digest=q.message_digest AND t.created_at=q.created_at
   AND convert_from(t.message,'UTF8')::jsonb=jsonb_build_object('role','tool','parts',jsonb_build_array(jsonb_build_object('tool_result',
    jsonb_build_object('call_id',q.call_id,'is_error',true,'parts',jsonb_build_array(jsonb_build_object('text',jsonb_build_object('text','{"code":"approval_rejected","message":"The user rejected this tool operation."}')))))))
   AND n.schema_version=4 AND n.snapshot_id=r.snapshot_id AND n.start_id=r.start_id AND n.context_digest=r.context_digest
   AND n.round_ordinal=r.round_ordinal+1 AND n.transcript_start=q.transcript_sequence+1 AND n.created_at=q.created_at
   AND e.snapshot_id=r.snapshot_id AND e.version=q.checkpoint_version AND e.status='running' AND e.cancel_requested_at IS NULL
   AND e.waiting_reason IS NULL AND e.waiting_reference IS NULL AND e.waiting_at IS NULL
   AND q.xmin::text::numeric=pg_current_xact_id()::text::numeric % 4294967296
   AND t.xmin::text::numeric=pg_current_xact_id()::text::numeric % 4294967296
   AND n.xmin::text::numeric=pg_current_xact_id()::text::numeric % 4294967296
   AND e.xmin::text::numeric=pg_current_xact_id()::text::numeric % 4294967296
 ) THEN
  RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='invalid loop approval result pair';
 END IF;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_execution.guard_loop_provenance() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE r record; p record; t record; q record; b jsonb; m jsonb; expected_sequence bigint; approval_result boolean;
BEGIN
 IF TG_TABLE_NAME='rounds' THEN
  IF NEW.schema_version<>4 THEN RETURN NULL; END IF;
  SELECT * INTO r FROM agenteam_execution.rounds WHERE id=NEW.id;
  b:=convert_from(r.content,'UTF8')::jsonb;
  IF NOT (b->>'schema_version'='4' AND b#>>'{fields,ordinal}'=r.round_ordinal::text AND b#>>'{fields,transcript_start}'=r.transcript_start::text
   AND b#>>'{fields,round,round_id}'=r.id AND b#>>'{fields,round,execution_id}'=r.execution_id AND b#>>'{fields,round,snapshot_id}'=r.snapshot_id
   AND b#>>'{fields,round,call_id}'=r.call_id AND b#>>'{fields,round,input_binding_id}'=r.input_binding_id
   AND b#>>'{fields,round,input,digest}'=r.input_digest AND b#>>'{fields,round,context_digest}'=r.context_digest
   AND (b#>>'{fields,round,created_at}')::timestamptz=r.created_at) IS TRUE THEN
   RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='invalid loop round bytes';
  END IF;
  IF r.round_ordinal>1 THEN
   SELECT * INTO p FROM agenteam_execution.rounds WHERE execution_id=r.execution_id AND id=r.predecessor_round_id;
   SELECT EXISTS(SELECT 1 FROM agenteam_execution.loop_approval_results WHERE execution_id=r.execution_id AND round_id=r.predecessor_round_id) INTO approval_result;
   IF approval_result THEN
    PERFORM agenteam_execution.require_loop_approval_pair(r.execution_id::text,r.predecessor_round_id::text);
    SELECT * INTO q FROM agenteam_execution.loop_approval_results WHERE execution_id=r.execution_id AND round_id=r.predecessor_round_id;
   ELSE
    SELECT * INTO q FROM agenteam_execution.loop_tool_results WHERE execution_id=r.execution_id AND round_id=r.predecessor_round_id;
   END IF;
   IF p.id IS NULL OR q.round_id IS NULL OR NOT (p.schema_version=4 AND p.tool_pending AND p.terminal_status IS NULL AND p.round_ordinal+1=r.round_ordinal AND p.snapshot_id=r.snapshot_id AND p.start_id=r.start_id AND p.context_digest=r.context_digest
    AND q.transcript_sequence+1=r.transcript_start AND r.created_at=q.created_at AND b#>>'{fields,predecessor}'=p.id
    AND (b#>>'{fields,round,input,schema_version}')::bigint=3 AND b#>>'{fields,tool_choice,kind}'='auto') IS TRUE THEN
    RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='invalid loop predecessor';
   END IF;
  ELSE
   IF b#>>'{fields,round,input,schema_version}'<>'4' OR b#>>'{fields,tool_choice,kind}'<>'required' OR b#>'{fields,predecessor}'<>'null'::jsonb THEN
    RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='invalid first loop round';
   END IF;
  END IF;
  RETURN NULL;
 END IF;
 IF TG_TABLE_NAME='transcript_entries' THEN
  IF NOT NEW.loop_entry THEN RETURN NULL; END IF;
  SELECT * INTO r FROM agenteam_execution.rounds WHERE execution_id=NEW.execution_id AND id=NEW.round_id;
  expected_sequence:=CASE WHEN r.round_ordinal=1 THEN 2 ELSE r.transcript_start END;
  IF r.schema_version<>4 OR NOT ((NEW.kind='input' AND r.round_ordinal=1 AND NEW.sequence=1) OR (NEW.kind='assistant' AND NEW.sequence=expected_sequence) OR (NEW.kind='tool_result' AND NEW.sequence=expected_sequence+1)) THEN
   RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='invalid loop transcript position';
  END IF;
  IF NEW.kind<>'tool_result' THEN RETURN NULL; END IF;
 END IF;
 SELECT EXISTS(SELECT 1 FROM agenteam_execution.loop_approval_results WHERE execution_id=NEW.execution_id AND round_id=NEW.round_id) INTO approval_result;
 IF approval_result THEN
  PERFORM agenteam_execution.require_loop_approval_pair(NEW.execution_id::text,NEW.round_id::text);
  SELECT * INTO q FROM agenteam_execution.loop_approval_results WHERE execution_id=NEW.execution_id AND round_id=NEW.round_id;
 ELSE
  SELECT * INTO q FROM agenteam_execution.loop_tool_results WHERE execution_id=NEW.execution_id AND round_id=NEW.round_id;
 END IF;
 SELECT * INTO r FROM agenteam_execution.rounds WHERE execution_id=NEW.execution_id AND id=NEW.round_id;
 SELECT * INTO t FROM agenteam_execution.transcript_entries WHERE execution_id=q.execution_id AND sequence=q.transcript_sequence;
 m:=convert_from(t.message,'UTF8')::jsonb;
 IF q.round_id IS NULL OR t.round_id IS NULL OR NOT (r.schema_version=4 AND r.tool_pending AND q.transcript_sequence=r.transcript_through+1 AND t.loop_entry AND t.kind='tool_result' AND t.round_id=q.round_id AND t.digest=q.message_digest AND t.created_at=q.created_at
  AND m->>'role'='tool' AND jsonb_array_length(m->'parts')=1 AND m#>>'{parts,0,tool_result,call_id}'=q.call_id
  AND EXISTS(SELECT 1 FROM agenteam_execution.executions e WHERE e.id=q.execution_id AND e.version>=q.checkpoint_version)) IS TRUE THEN
  RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='invalid loop result pair';
 END IF;
 RETURN NULL;
END;
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER execution_loop_approval_pair AFTER INSERT ON agenteam_execution.loop_approval_results
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION agenteam_execution.guard_loop_provenance();
