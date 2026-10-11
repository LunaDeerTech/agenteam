-- agenteam:transaction tx
-- +goose Up
-- This appends only one result after the immutable version-2 tool-pending
-- Round. It neither changes that Round's checkpoint nor enables another turn.
-- Select the four original sequence/kind checks by their referenced columns,
-- avoiding generated-name assumptions; all content/digest limits remain.
-- +goose StatementBegin
DO $$
DECLARE old_check record; removed integer := 0;
BEGIN
 FOR old_check IN
  SELECT c.conname FROM pg_constraint c
  WHERE c.conrelid='agenteam_execution.transcript_entries'::regclass AND c.contype='c'
   AND EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid=c.conrelid
     AND a.attnum=ANY(c.conkey) AND a.attname IN ('sequence','kind'))
 LOOP
  EXECUTE format('ALTER TABLE agenteam_execution.transcript_entries DROP CONSTRAINT %I',old_check.conname);
  removed := removed+1;
 END LOOP;
 IF removed<>4 THEN
  RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='unexpected transcript profile';
 END IF;
END;
$$;
-- +goose StatementEnd
ALTER TABLE agenteam_execution.transcript_entries
 ADD CONSTRAINT execution_transcript_sequence CHECK(sequence IN (1,2,3)),
 ADD CONSTRAINT execution_transcript_kind CHECK(kind IN ('input','assistant','tool_result')),
 ADD CONSTRAINT execution_transcript_shape CHECK((sequence=1 AND kind='input') OR (sequence=2 AND kind='assistant') OR (sequence=3 AND kind='tool_result')),
 ADD CONSTRAINT execution_transcript_role CHECK((jsonb_typeof(convert_from(message,'UTF8')::jsonb)='object'
  AND convert_from(message,'UTF8')::jsonb->>'role'=CASE kind WHEN 'input' THEN 'user' WHEN 'assistant' THEN 'assistant' ELSE 'tool' END) IS TRUE),
 ADD CONSTRAINT execution_transcript_result_identity UNIQUE(execution_id,sequence,round_id);

CREATE TABLE agenteam_execution.tool_result_consumptions (
 execution_id agenteam_execution.safe_id PRIMARY KEY,
 round_id agenteam_execution.safe_id NOT NULL,
 invocation_id agenteam_execution.safe_id NOT NULL,
 call_id text NOT NULL CHECK(octet_length(call_id) BETWEEN 1 AND 256),
 operation_id agenteam_execution.safe_id NOT NULL UNIQUE,
 attempt_id agenteam_execution.safe_id NOT NULL UNIQUE,
 request_id agenteam_execution.safe_id NOT NULL UNIQUE,
 input_digest text NOT NULL CHECK(input_digest ~ '^sha256:[0-9a-f]{64}$'),
 fingerprint text NOT NULL CHECK(fingerprint ~ '^sha256:[0-9a-f]{64}$'),
 terminal_digest text NOT NULL CHECK(terminal_digest ~ '^sha256:[0-9a-f]{64}$'),
 result_digest text NOT NULL CHECK(result_digest ~ '^sha256:[0-9a-f]{64}$'),
 result_bytes bigint NOT NULL CHECK(result_bytes BETWEEN 2 AND 524288),
 binding bytea NOT NULL CHECK(octet_length(binding) BETWEEN 2 AND 32768),
 binding_digest text NOT NULL CHECK(binding_digest='sha256:'||encode(sha256(binding),'hex')),
 message_digest text NOT NULL CHECK(message_digest ~ '^sha256:[0-9a-f]{64}$'),
 transcript_sequence bigint NOT NULL CHECK(transcript_sequence=3),
 checkpoint_version bigint NOT NULL CHECK(checkpoint_version>=4),
 created_at timestamptz(6) NOT NULL,
 FOREIGN KEY(execution_id,round_id,invocation_id,call_id) REFERENCES agenteam_execution.tool_calls(execution_id,round_id,invocation_id,call_id) ON DELETE RESTRICT,
 FOREIGN KEY(execution_id,transcript_sequence,round_id) REFERENCES agenteam_execution.transcript_entries(execution_id,sequence,round_id) DEFERRABLE INITIALLY DEFERRED,
 CHECK((jsonb_typeof(convert_from(binding,'UTF8')::jsonb)='object') IS TRUE)
);
CREATE TRIGGER execution_tool_result_consumption_immutable BEFORE UPDATE OR DELETE ON agenteam_execution.tool_result_consumptions FOR EACH ROW EXECUTE FUNCTION agenteam_execution.guard_direct_text_immutable();

-- Deferred pairing forbids an orphan third Transcript entry or a consumption
-- without its exact original ToolCall and full result message. Cross-domain
-- Tool/Attempt facts remain verified by their real provider in this same Tx;
-- this trigger reads only Execution-owned tables.
-- +goose StatementBegin
CREATE FUNCTION agenteam_execution.guard_tool_result_consumption() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE v record; b jsonb; m jsonb; content text;
BEGIN
 IF TG_TABLE_NAME='transcript_entries' THEN
  IF NEW.sequence<>3 THEN RETURN NULL; END IF;
 END IF;
 SELECT q.*,e.project_id,e.agent_id,e.version AS current_version,
  r.schema_version,r.tool_pending,r.transcript_through,r.terminal_status,
  r.snapshot_id,r.input_binding_id,r.call_id AS logical_call_id,
  t.model_visible_name,t.tool_id,t.spec_revision,t.operation_name,t.operation_revision,t.protocol_major,
  t.payload_id,t.checkpoint_version AS call_version,t.created_at AS call_created,
  a.original_digest,a.canonical_digest,octet_length(a.original) AS original_bytes,
  x.message,x.digest AS actual_message_digest,x.created_at AS message_created
 INTO v FROM agenteam_execution.tool_result_consumptions q
 JOIN agenteam_execution.executions e ON e.id=q.execution_id
 JOIN agenteam_execution.rounds r ON r.execution_id=q.execution_id AND r.id=q.round_id
 JOIN agenteam_execution.tool_calls t ON t.execution_id=q.execution_id AND t.round_id=q.round_id AND t.invocation_id=q.invocation_id AND t.call_id=q.call_id
 JOIN agenteam_execution.tool_call_arguments a ON a.execution_id=t.execution_id AND a.id=t.payload_id
 JOIN agenteam_execution.transcript_entries x ON x.execution_id=q.execution_id AND x.round_id=q.round_id AND x.sequence=3 AND x.kind='tool_result'
 WHERE q.execution_id=NEW.execution_id;
 IF NOT FOUND THEN
  RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='execution_tool_result_pair',MESSAGE='missing result consumption pair';
 END IF;
 b := convert_from(v.binding,'UTF8')::jsonb;
 m := convert_from(v.message,'UTF8')::jsonb;
 content := m#>>'{parts,0,tool_result,parts,0,text,text}';
 IF NOT (v.schema_version=2 AND v.tool_pending AND v.transcript_through=2 AND v.terminal_status IS NULL
  AND v.checkpoint_version>v.call_version AND v.current_version>=v.checkpoint_version
  AND v.created_at>=v.call_created AND v.message_created=v.created_at AND v.message_digest=v.actual_message_digest
  AND b->>'project_id'=v.project_id AND b->>'agent_id'=v.agent_id AND b->>'execution_id'=v.execution_id
  AND b->>'round_id'=v.round_id AND b->>'snapshot_id'=v.snapshot_id AND b->>'input_binding_id'=v.input_binding_id
  AND b->>'logical_call_id'=v.logical_call_id AND b->>'invocation_id'=v.invocation_id AND b->>'call_id'=v.call_id
  AND b->>'model_visible_name'=v.model_visible_name AND b->>'tool_id'=v.tool_id AND b->>'spec_revision'=v.spec_revision::text
  AND b->>'operation_name'=v.operation_name AND b->>'operation_revision'=v.operation_revision AND b->>'protocol_major'=v.protocol_major::text
  AND b->>'payload_id'=v.payload_id AND b->>'payload_digest'=v.original_digest AND b->>'payload_bytes'=v.original_bytes::text AND b->>'canonical_arguments'=v.canonical_digest
  AND b->>'operation_id'=v.operation_id AND b->>'attempt_id'=v.attempt_id AND b->>'request_id'=v.request_id
  AND b->>'input_digest'=v.input_digest AND b->>'fingerprint'=v.fingerprint AND b->>'terminal_digest'=v.terminal_digest
  AND b->>'outcome'='success' AND b->>'code'='' AND b->'attempted'='true'::jsonb
  AND b->>'result_digest'=v.result_digest AND b->>'result_bytes'=v.result_bytes::text
  AND m->>'role'='tool' AND jsonb_array_length(m->'parts')=1
  AND m#>>'{parts,0,tool_result,call_id}'=v.call_id AND m#>'{parts,0,tool_result,is_error}'='false'::jsonb
  AND jsonb_array_length(m#>'{parts,0,tool_result,parts}')=1
  AND octet_length(convert_to(content,'UTF8'))=v.result_bytes
  AND 'sha256:'||encode(sha256(convert_to(content,'UTF8')),'hex')=v.result_digest) IS TRUE THEN
  RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='execution_tool_result_pair',MESSAGE='invalid result consumption pair';
 END IF;
 RETURN NULL;
END;
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER execution_tool_result_pair AFTER INSERT ON agenteam_execution.tool_result_consumptions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION agenteam_execution.guard_tool_result_consumption();
CREATE CONSTRAINT TRIGGER execution_transcript_tool_result_pair AFTER INSERT ON agenteam_execution.transcript_entries DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION agenteam_execution.guard_tool_result_consumption();
