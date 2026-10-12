-- agenteam:transaction tx
-- +goose Up
-- An immutable reservation identifies the original claimant. It is not an
-- executable owner, Invocation, credential lease or process-death proof.
CREATE TABLE agenteam_execution.round_model_claims (
 id agenteam_execution.safe_id PRIMARY KEY,
 project_id agenteam_execution.safe_id NOT NULL,
 agent_id agenteam_execution.safe_id NOT NULL,
 execution_id agenteam_execution.safe_id NOT NULL,
 round_input_id agenteam_execution.safe_id NOT NULL,
 snapshot_id agenteam_execution.safe_id NOT NULL,
 call_id agenteam_execution.safe_id NOT NULL UNIQUE,
 process_id agenteam_execution.safe_id NOT NULL,
 schema_version bigint NOT NULL CHECK(schema_version=1),
 round_input_digest text NOT NULL CHECK(round_input_digest ~ '^sha256:[0-9a-f]{64}$'),
 snapshot_digest text NOT NULL CHECK(snapshot_digest ~ '^sha256:[0-9a-f]{64}$'),
 projection_digest text NOT NULL CHECK(projection_digest ~ '^sha256:[0-9a-f]{64}$'),
 input_digest text NOT NULL CHECK(input_digest ~ '^sha256:[0-9a-f]{64}$'),
 request_digest text NOT NULL CHECK(request_digest ~ '^sha256:[0-9a-f]{64}$'),
 created_at timestamptz(6) NOT NULL,
 UNIQUE(execution_id,round_input_id),
 FOREIGN KEY(execution_id,round_input_id) REFERENCES agenteam_execution.round_model_requests(execution_id,round_input_id) ON DELETE RESTRICT,
 FOREIGN KEY(execution_id,project_id,agent_id) REFERENCES agenteam_execution.executions(id,project_id,agent_id) ON DELETE RESTRICT,
 FOREIGN KEY(execution_id,snapshot_id) REFERENCES agenteam_execution.snapshots(execution_id,id) ON DELETE RESTRICT
);
CREATE TRIGGER execution_round_model_claim_immutable BEFORE UPDATE OR DELETE ON agenteam_execution.round_model_claims FOR EACH ROW EXECUTE FUNCTION agenteam_execution.guard_direct_text_immutable();

-- Match the complete safe projection against original 72 and its 68/snapshot
-- parents. The request bytea remains solely in 72; no inner JSON is decoded.
-- +goose StatementBegin
CREATE FUNCTION agenteam_execution.guard_round_model_claim_pair() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE v record;
BEGIN
 SELECT q.*, r.project_id AS source_project, r.agent_id AS source_agent,
  r.snapshot_id AS source_snapshot, r.call_id AS source_call,
  r.round_input_digest AS source_round_digest, r.snapshot_digest AS source_snapshot_digest,
  r.projection_digest AS source_projection_digest, r.input_digest AS source_input_digest,
  r.request_digest AS source_request_digest, r.created_at AS source_at,
  n.project_id AS input_project, n.agent_id AS input_agent,
  n.snapshot_id AS input_snapshot, n.digest AS actual_round_digest, n.created_at AS input_at,
  s.digest AS actual_snapshot_digest
 INTO v FROM agenteam_execution.round_model_claims q
 JOIN agenteam_execution.round_model_requests r ON (r.execution_id,r.round_input_id)=(q.execution_id,q.round_input_id)
 JOIN agenteam_execution.round_inputs n ON (n.execution_id,n.id)=(q.execution_id,q.round_input_id)
 JOIN agenteam_execution.snapshots s ON (s.execution_id,s.id)=(q.execution_id,q.snapshot_id)
 WHERE q.id=NEW.id;
 IF NOT FOUND THEN
  RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='execution_round_model_claim_pair',MESSAGE='missing reservation source';
 END IF;
 IF NOT (v.project_id=v.source_project AND v.project_id=v.input_project
  AND v.agent_id=v.source_agent AND v.agent_id=v.input_agent
  AND v.snapshot_id=v.source_snapshot AND v.snapshot_id=v.input_snapshot
  AND v.call_id=v.source_call
  AND v.round_input_digest=v.source_round_digest AND v.round_input_digest=v.actual_round_digest
  AND v.snapshot_digest=v.source_snapshot_digest AND v.snapshot_digest=v.actual_snapshot_digest
  AND v.projection_digest=v.source_projection_digest
  AND v.input_digest=v.source_input_digest AND v.request_digest=v.source_request_digest
  AND v.created_at>=v.source_at AND v.source_at>=v.input_at) IS TRUE THEN
  RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='execution_round_model_claim_pair',MESSAGE='invalid reservation source';
 END IF;
 RETURN NULL;
END;
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER execution_round_model_claim_pair AFTER INSERT ON agenteam_execution.round_model_claims DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION agenteam_execution.guard_round_model_claim_pair();
