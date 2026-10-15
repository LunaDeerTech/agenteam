-- agenteam:transaction tx
-- +goose Up
-- Parenthesize JSONB extraction before subtraction. PostgreSQL otherwise
-- evaluates the literal ref as JSON during planning, even for text results.
-- Preserve the existing typed result and original ledger constraints.
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
 IF k IN ('skill.list','skill.read','skill.assign','skill.prepare','task.read','task.transfer','artifact.list','artifact.create','artifact.read','knowledge.query','knowledge.read','memory.retain','memory.recall','memory.reflect','meeting.list','meeting.request','meeting.add-agent','meeting.remove-agent','meeting.update','meeting.comment','meeting.request-decision','mcp.list-resources','mcp.read-resource','mcp.call') AND ph IN ('succeeded','failed','cancelled') THEN
  SELECT count(*) INTO matching FROM agenteam_tool.builtin_results r JOIN agenteam_tool.attempts a
   ON(a.attempt_id,a.operation_id,a.execution_id,a.project_id,a.agent_id,a.backend_request_id)=(r.attempt_id,r.operation_id,r.execution_id,r.project_id,r.agent_id,r.request_id)
   WHERE r.operation_id=target AND r.attempt_id=active AND a.phase=CASE WHEN k IN ('mcp.call','mcp.list-resources','mcp.read-resource') AND ph='failed' AND op.outcome='unknown' THEN 'unknown' ELSE ph END AND a.retired;
  IF total<>1 OR matching<>1 THEN RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='tool_builtin_result_complete',MESSAGE='incomplete builtin result'; END IF;
  IF k IN ('mcp.call','mcp.list-resources','mcp.read-resource') THEN
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
      AND ((part->'ref')-ARRAY['kind','project_id','file_id'])='{}'::jsonb
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

-- The original successor guard used d for both its current disposition and
-- a SQL set alias. Qualify the set values without changing the accepted facts.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_execution.require_loop_skill_pair(execution text, target_round text) RETURNS void LANGUAGE plpgsql AS $$
DECLARE k record; r record; p record; q record; t record; b jsonb; before_data jsonb; expected_control jsonb; expected_bindings jsonb; d jsonb; addition jsonb; change_data jsonb; prior_sequence bigint;
BEGIN
 SELECT * INTO k FROM agenteam_execution.loop_skill_inputs WHERE execution_id=execution AND round_id=target_round;
 SELECT * INTO r FROM agenteam_execution.rounds WHERE execution_id=execution AND id=target_round;
 IF k.round_id IS NULL OR r.id IS NULL THEN
  RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='missing loop skill input';
 END IF;
 SELECT * INTO p FROM agenteam_execution.rounds WHERE execution_id=execution AND id=k.source_round_id;
 IF EXISTS(SELECT 1 FROM agenteam_execution.loop_approval_results WHERE execution_id=execution AND round_id=k.source_round_id) THEN
  SELECT * INTO q FROM agenteam_execution.loop_approval_results WHERE execution_id=execution AND round_id=k.source_round_id;
 ELSE
  SELECT * INTO q FROM agenteam_execution.loop_tool_results WHERE execution_id=execution AND round_id=k.source_round_id;
 END IF;
 b:=convert_from(k.content,'UTF8')::jsonb;
 IF NOT (r.skill_input_required AND r.schema_version=4 AND r.round_ordinal>1 AND p.id=r.predecessor_round_id AND p.snapshot_id=r.snapshot_id AND k.snapshot_id=r.snapshot_id
  AND r.round_ordinal=p.round_ordinal+1 AND q.round_id=p.id AND k.created_at=r.created_at AND q.created_at=r.created_at
  AND jsonb_typeof(b)='object' AND b-'schema_version'-'execution_id'-'snapshot_id'-'round_id'-'source_round_id'-'previous_cutoff'-'assignment_cutoff'-'previous_bindings'-'bindings'-'dispositions'-'additions'-'control_sequence'-'created_at'='{}'::jsonb
  AND b->>'schema_version'='1' AND b->>'execution_id'=k.execution_id AND b->>'snapshot_id'=k.snapshot_id AND b->>'round_id'=k.round_id AND b->>'source_round_id'=k.source_round_id
  AND b->>'assignment_cutoff'=k.assignment_cutoff::text AND (b->>'previous_cutoff')::bigint BETWEEN 1 AND k.assignment_cutoff AND (b->>'created_at')::timestamptz=k.created_at
  AND jsonb_typeof(b->'previous_bindings')='array' AND jsonb_typeof(b->'bindings')='array' AND jsonb_typeof(b->'dispositions')='array' AND jsonb_typeof(b->'additions')='array'
  AND ((k.control_sequence IS NULL AND b->'control_sequence'='null'::jsonb AND jsonb_array_length(b->'additions')=0 AND r.transcript_start=q.transcript_sequence+1)
   OR (k.control_sequence=q.transcript_sequence+1 AND b->>'control_sequence'=k.control_sequence::text AND jsonb_array_length(b->'additions')>0 AND r.transcript_start=k.control_sequence+1))) IS TRUE THEN
  RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='invalid loop skill input pair';
 END IF;
 IF p.skill_input_required AND p.round_ordinal>1 THEN
  SELECT convert_from(content,'UTF8')::jsonb INTO before_data FROM agenteam_execution.loop_skill_inputs WHERE execution_id=execution AND round_id=p.id;
  IF before_data IS NULL OR NOT (before_data->>'assignment_cutoff'=b->>'previous_cutoff' AND before_data->'bindings'=b->'previous_bindings') IS TRUE THEN
   RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='invalid previous loop skills';
  END IF;
 ELSE
  SELECT convert_from(content,'UTF8')::jsonb#>'{context,preparation_input,skills}' INTO before_data FROM agenteam_execution.snapshots WHERE execution_id=execution AND id=k.snapshot_id;
  IF before_data IS NULL OR NOT (before_data->>'assignment_sequence'=b->>'previous_cutoff' AND before_data->'bindings'=b->'previous_bindings') IS TRUE THEN
   RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='invalid initial loop skills';
  END IF;
 END IF;
 prior_sequence:=(b->>'previous_cutoff')::bigint;
 FOR d IN SELECT value FROM jsonb_array_elements(b->'dispositions') LOOP
  SELECT convert_from(i.change_data,'UTF8')::jsonb INTO change_data FROM agenteam_execution.skill_assignment_ingress i
   WHERE i.execution_id=execution AND i.change_id=d->>'change_id' AND i.outcome='queued';
  IF change_data IS NULL OR NOT ((d->>'assignment_sequence')::bigint>prior_sequence AND (d->>'assignment_sequence')::bigint<=k.assignment_cutoff
   AND d->>'assignment_sequence'=change_data->>'assignment_sequence'
   AND ((d->>'state'='applied' AND d->>'reason'='') OR (d->>'state'='skipped' AND d->>'reason' IN('removed','disabled','deleted','superseded')))) IS TRUE THEN
   RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='invalid loop skill disposition';
  END IF;
  prior_sequence:=(d->>'assignment_sequence')::bigint;
  IF d->>'state'='applied' THEN
   SELECT value INTO addition FROM jsonb_array_elements(b->'additions') WHERE value->>'change_id'=d->>'change_id';
   IF addition IS NULL OR addition->'binding' IS DISTINCT FROM change_data->'binding' THEN
    RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='invalid loop skill addition';
   END IF;
  END IF;
 END LOOP;
 IF EXISTS(SELECT 1 FROM agenteam_execution.skill_assignment_ingress i WHERE i.execution_id=execution AND i.outcome='queued'
  AND (convert_from(i.change_data,'UTF8')::jsonb->>'assignment_sequence')::bigint>(b->>'previous_cutoff')::bigint
  AND (convert_from(i.change_data,'UTF8')::jsonb->>'assignment_sequence')::bigint<=k.assignment_cutoff
  AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(b->'dispositions') AS dispositions_row(value) WHERE dispositions_row.value->>'change_id'=i.change_id))
  OR (SELECT count(*) FROM jsonb_array_elements(b->'dispositions') AS dispositions_row(value) WHERE dispositions_row.value->>'state'='applied')<>jsonb_array_length(b->'additions')
  OR (SELECT count(DISTINCT a->>'change_id') FROM jsonb_array_elements(b->'additions') a)<>jsonb_array_length(b->'additions') THEN
  RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='incomplete loop skill dispositions';
 END IF;
 SELECT COALESCE(jsonb_agg(binding ORDER BY binding->>'skill_id'),'[]'::jsonb) INTO expected_bindings FROM (
  SELECT value AS binding FROM jsonb_array_elements(b->'previous_bindings') old WHERE NOT EXISTS(SELECT 1 FROM jsonb_array_elements(b->'additions') a WHERE a#>>'{binding,skill_id}'=old->>'skill_id')
  UNION ALL SELECT value->'binding' FROM jsonb_array_elements(b->'additions')) bindings;
 IF expected_bindings IS DISTINCT FROM b->'bindings' THEN
  RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='invalid effective loop skills';
 END IF;
 IF k.control_sequence IS NOT NULL THEN
  SELECT * INTO t FROM agenteam_execution.transcript_entries WHERE execution_id=execution AND sequence=k.control_sequence;
  expected_control:=jsonb_build_object('kind','skills_added','schema_version','1','execution_id',k.execution_id,'round_id',k.round_id,'source_round_id',k.source_round_id,'assignment_cutoff',k.assignment_cutoff::text,'payload',b->'additions','transcript_position',k.control_sequence::text);
  IF t.round_id IS NULL OR NOT (t.round_id=k.round_id AND t.loop_entry AND t.kind='control' AND t.round_input_id IS NULL AND t.created_at=k.created_at AND convert_from(t.message,'UTF8')::jsonb=expected_control) IS TRUE THEN
   RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='invalid loop skill control';
  END IF;
 END IF;
END $$;
-- +goose StatementEnd
