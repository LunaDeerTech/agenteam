-- agenteam:transaction tx
-- +goose Up
-- A complete final projection, never a durable delta log. The same original
-- transaction seals the canonical terminal row, response and this snapshot.
CREATE TABLE agenteam_execution.runtime_snapshots (
 execution_id agenteam_execution.safe_id PRIMARY KEY REFERENCES agenteam_execution.executions(id) ON DELETE RESTRICT,
 project_id agenteam_execution.safe_id NOT NULL,
 snapshot_id agenteam_execution.safe_id NOT NULL,
 round_id agenteam_execution.safe_id NOT NULL,
 terminal_status text NOT NULL CHECK(terminal_status IN ('succeeded','failed','cancelled')),
 terminal_version bigint NOT NULL CHECK(terminal_version>=3),
 completed_at timestamptz(6) NOT NULL,
 content bytea NOT NULL CHECK(octet_length(content) BETWEEN 2 AND 16842752),
 digest text NOT NULL CHECK(digest='sha256:'||encode(sha256(content),'hex')),
 FOREIGN KEY(execution_id,snapshot_id) REFERENCES agenteam_execution.snapshots(execution_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(execution_id,round_id) REFERENCES agenteam_execution.rounds(execution_id,id) ON DELETE RESTRICT,
 CHECK(jsonb_typeof(convert_from(content,'UTF8')::jsonb)='object')
);
CREATE INDEX execution_runtime_project ON agenteam_execution.runtime_snapshots(project_id,execution_id);

-- +goose StatementBegin
CREATE FUNCTION agenteam_execution.guard_runtime_snapshot() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE source record; response jsonb; item jsonb; expected jsonb; text_content text;
BEGIN
 IF TG_OP='DELETE' AND agenteam_execution.project_retirement_allowed(OLD.execution_id::text) THEN RETURN OLD; END IF;
 IF TG_OP<>'INSERT' THEN
  RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='immutable execution runtime snapshot';
 END IF;
 SELECT e.id,e.project_id,e.snapshot_id,e.status,e.version,e.started_at,e.completed_at,
        r.id AS round_id,r.created_at,r.response,r.terminal_reason
 INTO source
 FROM agenteam_execution.executions e JOIN agenteam_execution.rounds r ON r.execution_id=e.id
 WHERE e.id=NEW.execution_id AND e.project_id=NEW.project_id
 AND e.snapshot_id=NEW.snapshot_id AND r.snapshot_id=e.snapshot_id AND r.id=NEW.round_id
 AND e.status=NEW.terminal_status AND r.terminal_status=e.status
 AND e.version=NEW.terminal_version AND r.terminal_version=e.version
 AND e.completed_at=NEW.completed_at AND r.finished_at=e.completed_at
 AND e.started_at=r.created_at AND r.schema_version=1 AND NOT r.tool_pending
 AND e.xmin::text::numeric=pg_current_xact_id()::text::numeric % 4294967296
 AND r.xmin::text::numeric=pg_current_xact_id()::text::numeric % 4294967296;
 IF NOT FOUND THEN
  RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='runtime snapshot requires original terminal transaction';
 END IF;
 expected:=jsonb_build_object('schema','durable_final_text_v1','generation',NEW.snapshot_id,
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
CREATE TRIGGER execution_runtime_snapshot_guard BEFORE INSERT OR UPDATE OR DELETE ON agenteam_execution.runtime_snapshots
 FOR EACH ROW EXECUTE FUNCTION agenteam_execution.guard_runtime_snapshot();
