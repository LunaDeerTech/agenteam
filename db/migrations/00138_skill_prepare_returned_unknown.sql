-- agenteam:transaction tx
-- +goose Up
-- Preserve the original failed Operation / unknown retired Attempt for an
-- actually returned skill.prepare result. No new phase or ledger is introduced;
-- MCP and other Builtins retain their exact existing constraints.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_tool.check_builtin_result() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE target agenteam_tool.operations.operation_id%TYPE; k text; ph text; active agenteam_tool.operations.active_attempt_id%TYPE; total bigint; matching bigint; op agenteam_tool.operations%ROWTYPE; payload jsonb; terminal jsonb; part jsonb; prepare_request text;
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
   WHERE r.operation_id=target AND r.attempt_id=active AND a.phase=CASE WHEN k IN ('mcp.call','mcp.list-resources','mcp.read-resource','skill.prepare') AND ph='failed' AND op.outcome='unknown' THEN 'unknown' ELSE ph END AND a.retired;
  IF total<>1 OR matching<>1 THEN RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='tool_builtin_result_complete',MESSAGE='incomplete builtin result'; END IF;
  IF k='skill.prepare' THEN
   SELECT convert_from(body_data,'UTF8')::jsonb,request_id INTO STRICT payload,prepare_request
    FROM agenteam_tool.builtin_results WHERE operation_id=target;
   terminal:=convert_from(op.terminal_data,'UTF8')::jsonb;
   -- Any unknown marker must agree across the original Operation, Attempt,
   -- terminal and closed returned-result envelope. It never supplies a path.
   IF op.outcome='unknown' OR terminal->>'Outcome'='unknown' OR payload->>'outcome'='unknown' THEN
    IF NOT (ph='failed' AND op.version=3 AND op.outcome='unknown'
     AND terminal->>'Format'='1' AND terminal->>'Operation'=op.operation_id
     AND terminal->>'Attempt'=active AND terminal->>'Request'=prepare_request
     AND terminal->>'Outcome'='unknown' AND terminal->>'Code'='DEPENDENCY_UNAVAILABLE'
     AND jsonb_typeof(payload)='object'
     AND (payload-ARRAY['skill_id','revision_id','revision','mount_id','outcome','may_have_prepared','error'])='{}'::jsonb
     AND payload->>'outcome'='unknown' AND payload->'may_have_prepared'='true'::jsonb
     AND jsonb_typeof(payload->'skill_id')='string'
     AND payload->>'skill_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
     AND jsonb_typeof(payload->'revision_id')='string'
     AND payload->>'revision_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
     AND jsonb_typeof(payload->'mount_id')='string'
     AND payload->>'mount_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
     AND jsonb_typeof(payload->'revision')='string'
     AND CASE WHEN payload->>'revision' ~ '^[1-9][0-9]{0,18}$'
         THEN (payload->>'revision')::numeric<=9223372036854775807 ELSE false END
     AND jsonb_typeof(payload->'error')='object'
     AND (payload->'error')='{"code":"DEPENDENCY_UNAVAILABLE"}'::jsonb
    ) IS TRUE THEN
     RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='tool_skill_prepare_unknown_result',MESSAGE='invalid returned preparation result';
    END IF;
   END IF;
  END IF;
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
