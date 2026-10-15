-- agenteam:transaction tx
-- +goose Up
-- Dynamic Skills keep the original install ledger and immutable Execution
-- snapshot. Each newly committed successor records its own exact Skill input.
ALTER TABLE agenteam_tool.operations DROP CONSTRAINT tool_operation_kind;
ALTER TABLE agenteam_tool.operations ADD CONSTRAINT tool_operation_kind CHECK(operation_kind IN ('skill.install','runner.read-file','runner.write-file','runner.run-command','skill.list','skill.read','skill.assign','skill.prepare','task.read','task.transfer','artifact.list','artifact.create','artifact.read','knowledge.query','knowledge.read','memory.retain','memory.recall','memory.reflect','meeting.list','meeting.request','meeting.add-agent','meeting.remove-agent','meeting.update','meeting.comment','meeting.request-decision','mcp.list-resources','mcp.read-resource','mcp.call'));
ALTER TABLE agenteam_tool.operations DROP CONSTRAINT tool_operation_backend_shape;
ALTER TABLE agenteam_tool.operations ADD CONSTRAINT tool_operation_backend_shape CHECK((
 (operation_kind='skill.install' AND skill_id IS NOT NULL AND backend_key='tool.skill.install:'||operation_id::text)
 OR (operation_kind IN ('runner.read-file','runner.write-file','runner.run-command','skill.list','skill.read','skill.assign','skill.prepare','task.read','task.transfer','artifact.list','artifact.create','artifact.read','knowledge.query','knowledge.read','memory.retain','memory.recall','memory.reflect','meeting.list','meeting.request','meeting.add-agent','meeting.remove-agent','meeting.update','meeting.comment','meeting.request-decision','mcp.list-resources','mcp.read-resource','mcp.call') AND skill_id IS NULL AND backend_key IS NULL)
) IS TRUE);

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

ALTER TABLE agenteam_execution.rounds ADD COLUMN skill_input_required boolean NOT NULL DEFAULT false;
ALTER TABLE agenteam_execution.rounds ALTER COLUMN skill_input_required SET DEFAULT true;
-- Existing rounds retain their original bytes. A later INSERT cannot claim
-- that compatibility exception, nor can completion rewrite the marker.
-- +goose StatementBegin
CREATE FUNCTION agenteam_execution.require_new_skill_input_marker() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT NEW.skill_input_required THEN
  RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='new round requires skill input marker';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER execution_round_skill_input_marker BEFORE INSERT ON agenteam_execution.rounds FOR EACH ROW EXECUTE FUNCTION agenteam_execution.require_new_skill_input_marker();

CREATE TABLE agenteam_execution.loop_skill_inputs (
 execution_id agenteam_execution.safe_id NOT NULL,
 round_id agenteam_execution.safe_id PRIMARY KEY,
 source_round_id agenteam_execution.safe_id NOT NULL,
 snapshot_id agenteam_execution.safe_id NOT NULL,
 assignment_cutoff bigint NOT NULL CHECK(assignment_cutoff>0),
 content bytea NOT NULL CHECK(octet_length(content) BETWEEN 2 AND 4194304),
 digest text NOT NULL CHECK(digest='sha256:'||encode(sha256(content),'hex')),
 control_sequence bigint CHECK(control_sequence>=4),
 created_at timestamptz(6) NOT NULL,
 CHECK(round_id<>source_round_id),
 UNIQUE(execution_id,round_id),
 UNIQUE(execution_id,source_round_id),
 FOREIGN KEY(execution_id,round_id) REFERENCES agenteam_execution.rounds(execution_id,id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(execution_id,source_round_id) REFERENCES agenteam_execution.rounds(execution_id,id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(execution_id,snapshot_id) REFERENCES agenteam_execution.snapshots(execution_id,id),
 FOREIGN KEY(execution_id,control_sequence,round_id) REFERENCES agenteam_execution.transcript_entries(execution_id,sequence,round_id) DEFERRABLE INITIALLY DEFERRED
);
CREATE TRIGGER execution_loop_skill_immutable BEFORE UPDATE OR DELETE ON agenteam_execution.loop_skill_inputs FOR EACH ROW EXECUTE FUNCTION agenteam_execution.guard_direct_text_immutable();
ALTER TABLE agenteam_execution.transcript_entries DROP CONSTRAINT execution_transcript_shape;
ALTER TABLE agenteam_execution.transcript_entries ADD CONSTRAINT execution_transcript_shape CHECK((
 (NOT loop_entry AND ((round_id IS NOT NULL AND round_input_id IS NULL AND ((sequence=1 AND kind='input') OR (sequence=2 AND kind='assistant') OR (sequence=3 AND kind='tool_result'))) OR (round_id IS NULL AND round_input_id IS NOT NULL AND sequence=4 AND kind='control')))
 OR (loop_entry AND round_id IS NOT NULL AND round_input_id IS NULL AND kind IN('input','assistant','tool_result','control'))
) IS TRUE);

-- The Skill owner still authorizes current assignment validity in the original
-- locked transaction. This guard pairs only immutable Execution facts and the
-- already authorized ingress, without querying current mutable assignments.
-- +goose StatementBegin
CREATE FUNCTION agenteam_execution.require_loop_skill_pair(execution text, target_round text) RETURNS void LANGUAGE plpgsql AS $$
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
  AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(b->'dispositions') d WHERE d->>'change_id'=i.change_id))
  OR (SELECT count(*) FROM jsonb_array_elements(b->'dispositions') d WHERE d->>'state'='applied')<>jsonb_array_length(b->'additions')
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
-- +goose StatementBegin
CREATE FUNCTION agenteam_execution.guard_loop_skill_pair() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM agenteam_execution.require_loop_skill_pair(NEW.execution_id::text,NEW.round_id::text);
 RETURN NULL;
END $$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER execution_loop_skill_pair AFTER INSERT ON agenteam_execution.loop_skill_inputs DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION agenteam_execution.guard_loop_skill_pair();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_execution.guard_round_completion() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN
  IF agenteam_execution.project_retirement_allowed(OLD.execution_id::text) THEN RETURN OLD; END IF;
  RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='immutable execution round';
 END IF;
 IF OLD.terminal_status IS NOT NULL OR (OLD.tool_pending AND NOT (OLD.schema_version=4 AND NEW.tool_pending AND NEW.terminal_status IN('failed','cancelled') AND ROW(NEW.response,NEW.response_digest,NEW.checkpoint_at,NEW.transcript_through) IS NOT DISTINCT FROM ROW(OLD.response,OLD.response_digest,OLD.checkpoint_at,OLD.transcript_through))) OR
 ROW(NEW.id,NEW.execution_id,NEW.snapshot_id,NEW.start_id,NEW.input_binding_id,NEW.call_id,NEW.input_digest,NEW.context_digest,NEW.schema_version,NEW.content,NEW.digest,NEW.created_at,NEW.round_ordinal,NEW.predecessor_round_id,NEW.transcript_start,NEW.skill_input_required)
 IS DISTINCT FROM ROW(OLD.id,OLD.execution_id,OLD.snapshot_id,OLD.start_id,OLD.input_binding_id,OLD.call_id,OLD.input_digest,OLD.context_digest,OLD.schema_version,OLD.content,OLD.digest,OLD.created_at,OLD.round_ordinal,OLD.predecessor_round_id,OLD.transcript_start,OLD.skill_input_required)
 OR NOT ((NEW.terminal_status IS NOT NULL AND NOT NEW.tool_pending) OR (OLD.schema_version IN(2,4) AND NOT OLD.tool_pending AND NEW.tool_pending AND NEW.terminal_status IS NULL) OR (OLD.schema_version=4 AND OLD.tool_pending AND NEW.tool_pending AND NEW.terminal_status IN('failed','cancelled'))) THEN
  RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='immutable execution round';
 END IF;
 RETURN NEW;
END;
$$;
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
   AND n.round_ordinal=r.round_ordinal+1 AND n.transcript_start=q.transcript_sequence+CASE WHEN EXISTS(SELECT 1 FROM agenteam_execution.loop_skill_inputs sk WHERE sk.execution_id=n.execution_id AND sk.round_id=n.id AND sk.control_sequence=q.transcript_sequence+1) THEN 2 ELSE 1 END AND n.created_at=q.created_at
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
  IF r.skill_input_required AND r.round_ordinal>1 THEN PERFORM agenteam_execution.require_loop_skill_pair(r.execution_id::text,r.id::text); END IF;
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
    AND q.transcript_sequence+CASE WHEN EXISTS(SELECT 1 FROM agenteam_execution.loop_skill_inputs sk WHERE sk.execution_id=r.execution_id AND sk.round_id=r.id AND sk.control_sequence=q.transcript_sequence+1) THEN 2 ELSE 1 END=r.transcript_start AND r.created_at=q.created_at AND b#>>'{fields,predecessor}'=p.id
    AND (b#>>'{fields,round,input,schema_version}')::bigint=3 AND b#>>'{fields,tool_choice,kind}'='auto') IS TRUE THEN
    RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='invalid loop predecessor';
   END IF;
  ELSE
   IF NOT (b#>>'{fields,round,input,schema_version}'='4' AND b#>'{fields,predecessor}'='null'::jsonb
    AND (b#>>'{fields,tool_choice,kind}'='required'
     OR (b#>>'{fields,tool_choice,kind}'='auto' AND EXISTS(
      SELECT 1 FROM agenteam_execution.executions e WHERE e.id=r.execution_id
       AND e.launch_request#>>'{trigger,kind}'='meeting' AND e.launch_request->>'purpose'='meeting/response')))) IS TRUE THEN
    RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='invalid first loop round';
   END IF;
  END IF;
  RETURN NULL;
 END IF;
 IF TG_TABLE_NAME='transcript_entries' THEN
  IF NOT NEW.loop_entry THEN RETURN NULL; END IF;
  SELECT * INTO r FROM agenteam_execution.rounds WHERE execution_id=NEW.execution_id AND id=NEW.round_id;
  IF NEW.kind='control' THEN
   PERFORM agenteam_execution.require_loop_skill_pair(NEW.execution_id::text,NEW.round_id::text);
   RETURN NULL;
  END IF;
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
