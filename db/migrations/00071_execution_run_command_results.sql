-- agenteam:transaction tx
-- +goose Up
-- Consume complete historical run-command reports without granting dispatch.
-- Only the exact original command ToolCall gets the 1MiB canonical bundle
-- budget and known failure/cancellation classification. Old read/write result
-- bytes, success/IsError rules, all identity/FK/digest/immutable/sequence gates
-- stay in place. No new result table or historical row rewrite is necessary.
-- +goose StatementBegin
DO $$
DECLARE old_check record; removed integer := 0;
BEGIN
 FOR old_check IN
  SELECT c.conname FROM pg_constraint c
  WHERE c.conrelid='agenteam_execution.tool_result_consumptions'::regclass AND c.contype='c'
   AND EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid=c.conrelid
    AND a.attnum=ANY(c.conkey) AND a.attname='result_bytes')
 LOOP
  EXECUTE format('ALTER TABLE agenteam_execution.tool_result_consumptions DROP CONSTRAINT %I',old_check.conname);
  removed:=removed+1;
 END LOOP;
 IF removed<>1 THEN
  RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='unexpected result byte profile';
 END IF;
END;
$$;
-- +goose StatementEnd
ALTER TABLE agenteam_execution.tool_result_consumptions ADD CONSTRAINT execution_tool_result_bytes
 CHECK((result_bytes BETWEEN 2 AND CASE WHEN
  convert_from(binding,'UTF8')::jsonb->>'operation_name'='run-command'
  AND convert_from(binding,'UTF8')::jsonb->>'operation_revision'='1'
  AND convert_from(binding,'UTF8')::jsonb->>'protocol_major'='1'
 THEN 1048576 ELSE 524288 END) IS TRUE);

-- This deferred original pairing verifies the claimed binding against the
-- actual persisted Execution ToolCall and still verifies every identity/digest.
-- Only the OUTER message is parsed as jsonb. Its inner Text is canonical bundle
-- JSON and may contain a legal escaped NUL in stdout/stderr; parsing that inner
-- text as jsonb would wrongly reject it. Compare its full UTF8 bytes and SHA.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_execution.guard_tool_result_consumption() RETURNS trigger LANGUAGE plpgsql AS $$
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
  AND b->'attempted'='true'::jsonb
  AND (
   (v.operation_name='run-command' AND v.operation_revision='1' AND v.protocol_major=1
    AND v.result_bytes BETWEEN 2 AND 1048576
    AND (
     (b->>'outcome'='success' AND b->>'code'='' AND m#>'{parts,0,tool_result,is_error}'='false'::jsonb)
     OR (b->>'outcome'='failure' AND b->>'code' IN ('INTERNAL_ERROR','output_incomplete','command_signaled','command_exit_nonzero')
      AND m#>'{parts,0,tool_result,is_error}'='true'::jsonb)
     OR (b->>'outcome'='cancelled' AND b->>'code' IN ('CANCELLED','TIMEOUT')
      AND m#>'{parts,0,tool_result,is_error}'='true'::jsonb)))
   OR (v.operation_name IN ('read-file','write-file') AND v.operation_revision='1' AND v.protocol_major=1
    AND v.result_bytes BETWEEN 2 AND 524288 AND b->>'outcome'='success' AND b->>'code'=''
    AND m#>'{parts,0,tool_result,is_error}'='false'::jsonb))
  AND b->>'result_digest'=v.result_digest AND b->>'result_bytes'=v.result_bytes::text
  AND m->>'role'='tool' AND jsonb_array_length(m->'parts')=1
  AND m#>>'{parts,0,tool_result,call_id}'=v.call_id
  AND jsonb_array_length(m#>'{parts,0,tool_result,parts}')=1
  AND octet_length(convert_to(content,'UTF8'))=v.result_bytes
  AND 'sha256:'||encode(sha256(convert_to(content,'UTF8')),'hex')=v.result_digest) IS TRUE THEN
  RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='execution_tool_result_pair',MESSAGE='invalid result consumption pair';
 END IF;
 RETURN NULL;
END;
$$;
-- +goose StatementEnd
