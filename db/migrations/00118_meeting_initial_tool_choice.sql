-- agenteam:transaction tx
-- +goose Up
-- A Meeting contribution may answer directly on its first round. An initial
-- auto choice requires the real immutable Meeting launch, never a caller's
-- request hint. Existing required rounds and all successor/approval/result
-- provenance retain their original semantics and bytes.
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
