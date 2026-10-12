-- agenteam:transaction tx
-- +goose Up
-- A prepared successor request is data, not another invoked Round. Preserve
-- the 56/68 cardinality, transcript profiles, Model runtime and current grants.
CREATE TABLE agenteam_execution.round_model_requests (
 execution_id agenteam_execution.safe_id NOT NULL,
 round_input_id agenteam_execution.safe_id NOT NULL,
 project_id agenteam_execution.safe_id NOT NULL,
 agent_id agenteam_execution.safe_id NOT NULL,
 snapshot_id agenteam_execution.safe_id NOT NULL,
 round_input_digest text NOT NULL CHECK(round_input_digest ~ '^sha256:[0-9a-f]{64}$'),
 snapshot_digest text NOT NULL CHECK(snapshot_digest ~ '^sha256:[0-9a-f]{64}$'),
 projection_digest text NOT NULL CHECK(projection_digest ~ '^sha256:[0-9a-f]{64}$'),
 call_id agenteam_execution.safe_id NOT NULL UNIQUE,
 schema_version bigint NOT NULL CHECK(schema_version=1),
 input_digest text NOT NULL CHECK(input_digest ~ '^sha256:[0-9a-f]{64}$'),
 request bytea NOT NULL CHECK(octet_length(request) BETWEEN 2 AND 25182208),
 request_digest text NOT NULL CHECK(request_digest='sha256:'||encode(sha256(request),'hex')),
 request_bytes bigint NOT NULL CHECK(request_bytes=octet_length(request)),
 created_at timestamptz(6) NOT NULL,
 PRIMARY KEY(execution_id,round_input_id),
 FOREIGN KEY(execution_id,round_input_id) REFERENCES agenteam_execution.round_inputs(execution_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(execution_id,project_id,agent_id) REFERENCES agenteam_execution.executions(id,project_id,agent_id) ON DELETE RESTRICT,
 FOREIGN KEY(execution_id,snapshot_id) REFERENCES agenteam_execution.snapshots(execution_id,id) ON DELETE RESTRICT,
 CHECK((jsonb_typeof(convert_from(request,'UTF8')::jsonb)='object') IS TRUE)
);
CREATE TRIGGER execution_round_model_request_immutable BEFORE UPDATE OR DELETE ON agenteam_execution.round_model_requests FOR EACH ROW EXECUTE FUNCTION agenteam_execution.guard_direct_text_immutable();

-- Bind all duplicated identities to the immutable input/snapshot. Full message
-- and tool/schema pairing is revalidated by the Execution/Loop reader in the
-- original transaction; no SQL access to another domain is introduced here.
-- +goose StatementBegin
CREATE FUNCTION agenteam_execution.guard_round_model_request_pair() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE v record; b jsonb;
BEGIN
 SELECT q.*, r.project_id AS source_project, r.agent_id AS source_agent,
  r.snapshot_id AS source_snapshot, r.digest AS source_digest,
  r.created_at AS source_at, s.digest AS actual_snapshot_digest
 INTO v FROM agenteam_execution.round_model_requests q
 JOIN agenteam_execution.round_inputs r ON (r.execution_id,r.id)=(q.execution_id,q.round_input_id)
 JOIN agenteam_execution.snapshots s ON (s.execution_id,s.id)=(q.execution_id,q.snapshot_id)
 WHERE q.execution_id=NEW.execution_id AND q.round_input_id=NEW.round_input_id;
 IF NOT FOUND THEN
  RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='execution_round_model_request_pair',MESSAGE='missing request input pair';
 END IF;
 b := convert_from(v.request,'UTF8')::jsonb;
 IF NOT (v.project_id=v.source_project AND v.agent_id=v.source_agent
  AND v.snapshot_id=v.source_snapshot AND v.round_input_digest=v.source_digest
  AND v.snapshot_digest=v.actual_snapshot_digest AND v.created_at>=v.source_at
  AND b->>'schema_version'='1'
  AND b->>'project_id'=v.project_id AND b->>'agent_id'=v.agent_id AND b->>'execution_id'=v.execution_id
  AND b->>'round_input_id'=v.round_input_id AND b->>'round_input_digest'=v.round_input_digest
  AND b->>'snapshot_id'=v.snapshot_id AND b->>'snapshot_digest'=v.snapshot_digest
  AND b->>'projection_digest'=v.projection_digest AND b->>'call_id'=v.call_id
  AND b#>>'{input,execution_id}'=v.execution_id AND b#>>'{input,round_id}'=v.round_input_id
  AND b#>>'{input,digest}'=v.input_digest AND b#>>'{input,schema_version}'='3'
  AND b#>>'{semantic,schema_version}'='3'
  AND b#>>'{semantic,actor,kind}'='agent_run'
  AND b#>>'{semantic,actor,project_id}'=v.project_id AND b#>>'{semantic,actor,agent_id}'=v.agent_id
  AND b#>>'{semantic,actor,execution_id}'=v.execution_id
  AND b#>>'{semantic,tool_choice,kind}'='auto' AND b#>>'{semantic,response_format,kind}'='text'
  AND b#>>'{semantic,retry_class}'='agent') IS TRUE THEN
  RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='execution_round_model_request_pair',MESSAGE='invalid request input pair';
 END IF;
 RETURN NULL;
END;
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER execution_round_model_request_pair AFTER INSERT ON agenteam_execution.round_model_requests DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION agenteam_execution.guard_round_model_request_pair();

-- +goose Down
DO $$ BEGIN RAISE EXCEPTION 'forward-only migration'; END $$;
