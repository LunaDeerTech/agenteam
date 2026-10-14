-- agenteam:transaction tx
-- +goose Up
-- Executable loop rounds retain the original snapshot/lease and reserve one
-- distinct Model CallID per durable successor. Historical profiles stay intact.
ALTER TABLE agenteam_execution.rounds
 DROP CONSTRAINT rounds_execution_id_key,
 DROP CONSTRAINT rounds_schema_version_check,
 DROP CONSTRAINT rounds_transcript_through_check,
 ADD COLUMN round_ordinal bigint NOT NULL DEFAULT 1 CHECK(round_ordinal>0),
 ADD COLUMN predecessor_round_id agenteam_execution.safe_id,
 ADD COLUMN transcript_start bigint NOT NULL DEFAULT 1 CHECK(transcript_start>0),
 ADD CONSTRAINT rounds_schema_version_check CHECK(schema_version IN(1,2,4)),
 ADD CONSTRAINT execution_round_position CHECK((schema_version IN(1,2) AND round_ordinal=1 AND predecessor_round_id IS NULL AND transcript_start=1 AND transcript_through IN(1,2)) OR
 (schema_version=4 AND transcript_through>0 AND ((round_ordinal=1 AND predecessor_round_id IS NULL AND transcript_start=1) OR (round_ordinal>1 AND predecessor_round_id IS NOT NULL AND transcript_start>2)))),
 ADD UNIQUE(execution_id,round_ordinal),
 ADD UNIQUE(execution_id,predecessor_round_id),
 ADD FOREIGN KEY(execution_id,predecessor_round_id) REFERENCES agenteam_execution.rounds(execution_id,id);
ALTER TABLE agenteam_execution.tool_calls DROP CONSTRAINT tool_calls_pkey, ADD PRIMARY KEY(execution_id,round_id);

ALTER TABLE agenteam_execution.rounds DROP CONSTRAINT execution_round_phase;
ALTER TABLE agenteam_execution.rounds ADD CONSTRAINT execution_round_phase CHECK((
 (NOT tool_pending AND checkpoint_at IS NULL AND (
  (terminal_status IS NULL AND terminal_version IS NULL AND terminal_reason IS NULL AND terminal_event_id IS NULL AND response IS NULL AND response_digest IS NULL AND finished_at IS NULL AND transcript_through=CASE WHEN round_ordinal=1 THEN 1 ELSE transcript_start-1 END)
  OR (terminal_status IS NOT NULL AND terminal_version IS NOT NULL AND terminal_event_id IS NOT NULL AND finished_at>=created_at AND
   ((schema_version IN(1,4) AND terminal_status='succeeded' AND terminal_reason='completed' AND transcript_through=CASE WHEN round_ordinal=1 THEN 2 ELSE transcript_start END AND response IS NOT NULL AND response_digest='sha256:'||encode(sha256(response),'hex'))
    OR (terminal_status IN('failed','cancelled') AND transcript_through=CASE WHEN round_ordinal=1 THEN 1 ELSE transcript_start-1 END AND response IS NULL AND response_digest IS NULL AND
     ((terminal_status='failed' AND terminal_reason IN('model_failed','incomplete_response','runtime_failed')) OR (terminal_status='cancelled' AND terminal_reason='cancelled')))))
 )) OR (schema_version IN(2,4) AND tool_pending AND checkpoint_at>=created_at AND transcript_through=CASE WHEN round_ordinal=1 THEN 2 ELSE transcript_start END AND response IS NOT NULL AND response_digest='sha256:'||encode(sha256(response),'hex') AND (
  (terminal_status IS NULL AND terminal_version IS NULL AND terminal_reason IS NULL AND terminal_event_id IS NULL AND finished_at IS NULL)
  OR (schema_version=4 AND terminal_version IS NOT NULL AND terminal_event_id IS NOT NULL AND finished_at>=checkpoint_at AND ((terminal_status='cancelled' AND terminal_reason='cancelled') OR (terminal_status='failed' AND terminal_reason IN('model_failed','runtime_failed','incomplete_response'))))))
) IS TRUE);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_execution.guard_round_completion() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN
  IF agenteam_execution.project_retirement_allowed(OLD.execution_id::text) THEN RETURN OLD; END IF;
  RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='immutable execution round';
 END IF;
 IF OLD.terminal_status IS NOT NULL OR (OLD.tool_pending AND NOT (OLD.schema_version=4 AND NEW.tool_pending AND NEW.terminal_status IN('failed','cancelled') AND ROW(NEW.response,NEW.response_digest,NEW.checkpoint_at,NEW.transcript_through) IS NOT DISTINCT FROM ROW(OLD.response,OLD.response_digest,OLD.checkpoint_at,OLD.transcript_through))) OR
 ROW(NEW.id,NEW.execution_id,NEW.snapshot_id,NEW.start_id,NEW.input_binding_id,NEW.call_id,NEW.input_digest,NEW.context_digest,NEW.schema_version,NEW.content,NEW.digest,NEW.created_at,NEW.round_ordinal,NEW.predecessor_round_id,NEW.transcript_start)
 IS DISTINCT FROM ROW(OLD.id,OLD.execution_id,OLD.snapshot_id,OLD.start_id,OLD.input_binding_id,OLD.call_id,OLD.input_digest,OLD.context_digest,OLD.schema_version,OLD.content,OLD.digest,OLD.created_at,OLD.round_ordinal,OLD.predecessor_round_id,OLD.transcript_start)
 OR NOT ((NEW.terminal_status IS NOT NULL AND NOT NEW.tool_pending) OR (OLD.schema_version IN(2,4) AND NOT OLD.tool_pending AND NEW.tool_pending AND NEW.terminal_status IS NULL) OR (OLD.schema_version=4 AND OLD.tool_pending AND NEW.tool_pending AND NEW.terminal_status IN('failed','cancelled'))) THEN
  RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='immutable execution round';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd

ALTER TABLE agenteam_execution.transcript_entries
 ADD COLUMN loop_entry boolean NOT NULL DEFAULT false,
 DROP CONSTRAINT execution_transcript_sequence,
 DROP CONSTRAINT execution_transcript_shape,
 ADD CONSTRAINT execution_transcript_sequence CHECK((NOT loop_entry AND sequence IN(1,2,3,4)) OR (loop_entry AND sequence>0)),
 ADD CONSTRAINT execution_transcript_shape CHECK((
  (NOT loop_entry AND ((round_id IS NOT NULL AND round_input_id IS NULL AND ((sequence=1 AND kind='input') OR (sequence=2 AND kind='assistant') OR (sequence=3 AND kind='tool_result'))) OR (round_id IS NULL AND round_input_id IS NOT NULL AND sequence=4 AND kind='control')))
  OR (loop_entry AND round_id IS NOT NULL AND round_input_id IS NULL AND kind IN('input','assistant','tool_result'))
 ) IS TRUE);
DROP TRIGGER execution_transcript_tool_result_pair ON agenteam_execution.transcript_entries;
CREATE CONSTRAINT TRIGGER execution_transcript_tool_result_pair AFTER INSERT ON agenteam_execution.transcript_entries DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NOT NEW.loop_entry) EXECUTE FUNCTION agenteam_execution.guard_tool_result_consumption();
DROP TRIGGER execution_transcript_round_pair ON agenteam_execution.transcript_entries;
CREATE CONSTRAINT TRIGGER execution_transcript_round_pair AFTER INSERT ON agenteam_execution.transcript_entries DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NOT NEW.loop_entry) EXECUTE FUNCTION agenteam_execution.guard_round_input_pair();

CREATE TABLE agenteam_execution.loop_tool_results (
 execution_id agenteam_execution.safe_id NOT NULL,
 round_id agenteam_execution.safe_id NOT NULL,
 invocation_id agenteam_execution.safe_id NOT NULL,
 call_id text NOT NULL,
 operation_id agenteam_execution.safe_id NOT NULL UNIQUE,
 attempt_id agenteam_execution.safe_id NOT NULL UNIQUE,
 request_id agenteam_execution.safe_id NOT NULL UNIQUE,
 result_digest text NOT NULL CHECK(result_digest ~ '^sha256:[0-9a-f]{64}$'),
 terminal_digest text NOT NULL CHECK(terminal_digest ~ '^sha256:[0-9a-f]{64}$'),
 message_digest text NOT NULL CHECK(message_digest ~ '^sha256:[0-9a-f]{64}$'),
 transcript_sequence bigint NOT NULL CHECK(transcript_sequence>=3),
 checkpoint_version bigint NOT NULL CHECK(checkpoint_version>=4),
 created_at timestamptz(6) NOT NULL,
 PRIMARY KEY(execution_id,round_id),
 FOREIGN KEY(execution_id,round_id,invocation_id,call_id) REFERENCES agenteam_execution.tool_calls(execution_id,round_id,invocation_id,call_id),
 FOREIGN KEY(execution_id,transcript_sequence,round_id) REFERENCES agenteam_execution.transcript_entries(execution_id,sequence,round_id) DEFERRABLE INITIALLY DEFERRED
);
CREATE TRIGGER execution_loop_result_immutable BEFORE UPDATE OR DELETE ON agenteam_execution.loop_tool_results FOR EACH ROW EXECUTE FUNCTION agenteam_execution.guard_direct_text_immutable();

-- Protect contiguous request/result provenance in addition to the actual
-- Tool owner checking its durable Operation/retired Attempt in the same Tx.
-- +goose StatementBegin
CREATE FUNCTION agenteam_execution.guard_loop_provenance() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE r record; p record; t record; q record; b jsonb; m jsonb; expected_sequence bigint;
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
   SELECT * INTO q FROM agenteam_execution.loop_tool_results WHERE execution_id=r.execution_id AND round_id=r.predecessor_round_id;
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
 SELECT * INTO q FROM agenteam_execution.loop_tool_results WHERE execution_id=NEW.execution_id AND round_id=NEW.round_id;
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
CREATE CONSTRAINT TRIGGER execution_loop_round_pair AFTER INSERT ON agenteam_execution.rounds DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION agenteam_execution.guard_loop_provenance();
CREATE CONSTRAINT TRIGGER execution_loop_transcript_pair AFTER INSERT ON agenteam_execution.transcript_entries DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION agenteam_execution.guard_loop_provenance();
CREATE CONSTRAINT TRIGGER execution_loop_result_pair AFTER INSERT ON agenteam_execution.loop_tool_results DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION agenteam_execution.guard_loop_provenance();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_execution.guard_runtime_snapshot() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE source record; response jsonb; item jsonb; expected jsonb; text_content text;
BEGIN
 IF TG_OP='DELETE' AND agenteam_execution.project_retirement_allowed(OLD.execution_id::text) THEN RETURN OLD; END IF;
 IF TG_OP<>'INSERT' THEN
  RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='immutable execution runtime snapshot';
 END IF;
 SELECT e.id,e.project_id,e.snapshot_id,e.status,e.version,e.started_at,e.completed_at,
        r.id AS round_id,r.created_at,r.response,r.terminal_reason,r.schema_version
 INTO source
 FROM agenteam_execution.executions e JOIN agenteam_execution.rounds r ON r.execution_id=e.id
 WHERE e.id=NEW.execution_id AND e.project_id=NEW.project_id
 AND e.snapshot_id=NEW.snapshot_id AND r.snapshot_id=e.snapshot_id AND r.id=NEW.round_id
 AND e.status=NEW.terminal_status AND r.terminal_status=e.status
 AND e.version=NEW.terminal_version AND r.terminal_version=e.version
 AND e.completed_at=NEW.completed_at AND r.finished_at=e.completed_at
 AND ((r.schema_version=1 AND e.started_at=r.created_at) OR (r.schema_version=4 AND e.started_at<=r.created_at)) AND (NOT r.tool_pending OR (r.schema_version=4 AND e.status IN('failed','cancelled')))
 AND e.xmin::text::numeric=pg_current_xact_id()::text::numeric % 4294967296
 AND r.xmin::text::numeric=pg_current_xact_id()::text::numeric % 4294967296;
 IF NOT FOUND THEN
  RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='runtime snapshot requires original terminal transaction';
 END IF;
 expected:=jsonb_build_object('schema',CASE WHEN source.schema_version=4 THEN 'durable_final_text_v2' ELSE 'durable_final_text_v1' END,'generation',NEW.snapshot_id,
  'included_update_progress','1','execution_version',NEW.terminal_version::text,'items','[]'::jsonb);
 IF NEW.terminal_status='succeeded' THEN
  response:=convert_from(source.response,'UTF8')::jsonb;
  IF (response->>'finish_reason'='stop' AND response->'message'->>'role'='assistant'
    AND jsonb_typeof(response->'message'->'parts')='array'
    AND jsonb_array_length(response->'message'->'parts') BETWEEN 1 AND 256) IS NOT TRUE THEN
   RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='runtime snapshot requires complete text response';
  END IF;
  IF EXISTS(SELECT 1 FROM jsonb_array_elements(response->'message'->'parts') p
    WHERE (jsonb_typeof(p)='object' AND p-'text'='{}'::jsonb
      AND jsonb_typeof(p->'text')='object' AND (p->'text')-'text'='{}'::jsonb
      AND jsonb_typeof(p->'text'->'text')='string') IS NOT TRUE) THEN
   RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='runtime snapshot contains unsupported response part';
  END IF;
  SELECT string_agg(p.value->'text'->>'text','' ORDER BY p.ordinality) INTO text_content
   FROM jsonb_array_elements(response->'message'->'parts') WITH ORDINALITY p;
  IF text_content IS NULL OR octet_length(text_content)>16777216 THEN
   RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='runtime text exceeds response bound';
  END IF;
  item:=jsonb_build_object('id',NEW.round_id,'execution_id',NEW.execution_id,'seq','1',
   'type','text','status','completed','revision','1',
   'started_at',to_char(source.created_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
   'updated_at',to_char(NEW.completed_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
   'completed_at',to_char(NEW.completed_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
   'source_reference',jsonb_build_object('kind','execution_round','id',NEW.round_id),
   'data',jsonb_build_object('content',text_content));
  expected:=jsonb_set(expected,'{items}',jsonb_build_array(item));
 END IF;
 IF convert_from(NEW.content,'UTF8')::jsonb IS DISTINCT FROM expected THEN
  RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='runtime snapshot does not match original complete projection';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
