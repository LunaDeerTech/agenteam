-- agenteam:transaction tx
-- +goose Up
-- MCP shares the original approval and single-attempt Tool ledger. Unknown
-- effects remain a failed operation with an unknown retired Attempt; no retry.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_tool.check_builtin_result() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE target agenteam_tool.operations.operation_id%TYPE; k text; ph text; active agenteam_tool.operations.active_attempt_id%TYPE; total bigint; matching bigint; op agenteam_tool.operations%ROWTYPE; payload jsonb; terminal jsonb; part jsonb;
BEGIN
 target:=NEW.operation_id;
 SELECT operation_kind,phase,active_attempt_id INTO STRICT k,ph,active FROM agenteam_tool.operations WHERE operation_id=target;
 SELECT count(*) INTO total FROM agenteam_tool.builtin_results WHERE operation_id=target;
 SELECT * INTO STRICT op FROM agenteam_tool.operations WHERE operation_id=target;
 -- A denied MCP request has the original approval receipt and no Attempt.
 IF k='mcp.call' AND ph='failed' AND op.version=2 AND active IS NULL THEN
  IF total<>0 OR op.outcome<>'error' OR EXISTS(SELECT 1 FROM agenteam_tool.attempts WHERE operation_id=target)
   OR NOT EXISTS(SELECT 1 FROM agenteam_tool.approval_denials d WHERE d.operation_id=target AND d.fingerprint=op.fingerprint
     AND convert_from(op.terminal_data,'UTF8')::jsonb=jsonb_build_object('kind','approval_rejected','request_id',d.request_id,'report_available',false)) THEN
   RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='tool_builtin_result_complete',MESSAGE='invalid MCP denial';
  END IF;
  RETURN NEW;
 END IF;
 IF k IN ('task.read','task.transfer','artifact.list','artifact.create','artifact.read','knowledge.query','knowledge.read','memory.retain','memory.recall','memory.reflect','meeting.list','meeting.request','meeting.add-agent','meeting.remove-agent','meeting.update','meeting.comment','meeting.request-decision','mcp.list-resources','mcp.read-resource','mcp.call') AND ph IN ('succeeded','failed','cancelled') THEN
  SELECT count(*) INTO matching FROM agenteam_tool.builtin_results r JOIN agenteam_tool.attempts a
   ON(a.attempt_id,a.operation_id,a.execution_id,a.project_id,a.agent_id,a.backend_request_id)=(r.attempt_id,r.operation_id,r.execution_id,r.project_id,r.agent_id,r.request_id)
   WHERE r.operation_id=target AND r.attempt_id=active AND a.phase=CASE WHEN k='mcp.call' AND ph='failed' AND op.outcome='unknown' THEN 'unknown' ELSE ph END AND a.retired;
  IF total<>1 OR matching<>1 THEN RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='tool_builtin_result_complete',MESSAGE='incomplete builtin result'; END IF;
  IF k='mcp.call' THEN
   SELECT convert_from(body_data,'UTF8')::jsonb INTO STRICT payload FROM agenteam_tool.builtin_results WHERE operation_id=target;
   terminal:=convert_from(op.terminal_data,'UTF8')::jsonb;
   IF NOT (op.version=3 AND payload->>'format'='1' AND payload->>'call_id'=op.call_id
    AND jsonb_typeof(payload->'is_error')='boolean' AND jsonb_typeof(payload->'outcome_known')='boolean'
    AND jsonb_typeof(payload->'may_have_executed')='boolean' AND jsonb_typeof(payload->'failure_code')='string'
    AND jsonb_typeof(payload->'parts')='array' AND jsonb_array_length(payload->'parts') BETWEEN 1 AND 4096
    AND (payload - ARRAY['format','call_id','is_error','parts','outcome_known','may_have_executed','failure_code'])='{}'::jsonb
    AND ((op.outcome='success' AND terminal->>'Outcome'='success' AND payload->>'outcome_known'='true' AND payload->>'is_error'='false')
      OR (op.outcome='error' AND terminal->>'Outcome'='failure' AND payload->>'outcome_known'='true' AND payload->>'is_error'='true')
      OR (op.outcome='unknown' AND ph='failed' AND terminal->>'Outcome'='unknown' AND payload->>'outcome_known'='false' AND payload->>'may_have_executed'='true' AND payload->>'is_error'='true'))
   ) IS TRUE THEN
    RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='tool_mcp_result_envelope',MESSAGE='invalid MCP result envelope';
   END IF;
   FOR part IN SELECT value FROM jsonb_array_elements(payload->'parts') LOOP
    IF NOT ((part->>'kind'='text' AND (part-ARRAY['kind','text'])='{}'::jsonb AND (NOT part?'text' OR jsonb_typeof(part->'text')='string'))
     OR (part->>'kind' IN ('image','file') AND (part-ARRAY['kind','media_type','ref'])='{}'::jsonb
      AND jsonb_typeof(part->'media_type')='string' AND length(part->>'media_type') BETWEEN 1 AND 255
      AND (part->'ref'-ARRAY['kind','project_id','file_id'])='{}'::jsonb
      AND part#>>'{ref,kind}'='mcp_file' AND part#>>'{ref,project_id}'=op.project_id
      AND part#>>'{ref,file_id}' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$')) IS TRUE THEN
     RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='tool_mcp_result_envelope',MESSAGE='invalid MCP result part';
    END IF;
   END LOOP;
  END IF;
 ELSIF total<>0 THEN
  RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='tool_builtin_result_complete',MESSAGE='unexpected builtin result';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_execution.require_loop_approval_pair(execution text, round text) RETURNS void LANGUAGE plpgsql AS $$
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
   AND ((tc.operation_name='run-command' AND o.operation_kind='runner.run-command') OR (tc.operation_name='mcp.call' AND o.operation_kind='mcp.call'))
   AND tc.operation_revision='1' AND tc.protocol_major=1
   AND a.status='rejected' AND a.version=2 AND a.project_id=e.project_id AND a.agent_id=e.agent_id
   AND a.execution_id=r.execution_id AND a.round_id=r.id AND a.tool_call_id=tc.call_id AND a.tool_id=tc.tool_id
   AND o.project_id=e.project_id AND o.agent_id=e.agent_id AND o.execution_id=r.execution_id
   AND o.logical_call_id=r.call_id AND o.invocation_id=tc.invocation_id AND o.call_id=tc.call_id
   AND o.tool_id=tc.tool_id AND o.spec_revision=tc.spec_revision
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
