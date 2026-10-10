-- agenteam:transaction tx
-- +goose Up
-- Execution is the sole durable identity/slot owner. Source identities remain
-- validated through Task/Meeting ports; no cross-domain SQL replaces them.
CREATE SCHEMA agenteam_execution;
CREATE DOMAIN agenteam_execution.safe_id AS uuid CHECK
 (VALUE::text ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$');

CREATE TABLE agenteam_execution.executions (
 id agenteam_execution.safe_id PRIMARY KEY,
 project_id agenteam_execution.safe_id NOT NULL,
 agent_id agenteam_execution.safe_id NOT NULL,
 status text NOT NULL CHECK(status IN ('created','preparing','running','waiting','succeeded','failed','cancelled')),
 version bigint NOT NULL CHECK(version>=1),
 idempotency_key text COLLATE "C" NOT NULL CHECK(octet_length(idempotency_key) BETWEEN 1 AND 128 AND idempotency_key ~ '^[A-Za-z0-9._:/-]+$'),
 request_id agenteam_execution.safe_id NOT NULL,
 request_digest text NOT NULL CHECK(request_digest ~ '^sha256:[0-9a-f]{64}$'),
 trigger_reference_digest text NOT NULL CHECK(trigger_reference_digest ~ '^sha256:[0-9a-f]{64}$'),
 launch_request jsonb NOT NULL CHECK(jsonb_typeof(launch_request)='object' AND octet_length(launch_request::text)<=262144),
 initiator jsonb NOT NULL CHECK(jsonb_typeof(initiator)='object' AND octet_length(initiator::text)<=4096),
 cancel_requested_at timestamptz(6),
 snapshot_id agenteam_execution.safe_id,
 created_at timestamptz(6) NOT NULL,
 updated_at timestamptz(6) NOT NULL CHECK(updated_at>=created_at),
 started_at timestamptz(6),
 completed_at timestamptz(6),
 CONSTRAINT executions_scope_identity UNIQUE(id,project_id,agent_id),
 CONSTRAINT executions_launch_identity UNIQUE(project_id,agent_id,idempotency_key),
 CONSTRAINT executions_launch_shape CHECK((
  launch_request ?& ARRAY['project_id','agent_id','trigger','purpose','execution_policy','lineage']
  AND launch_request-ARRAY['project_id','agent_id','trigger','purpose','execution_policy','lineage']='{}'::jsonb
  AND launch_request->>'project_id'=project_id::text
  AND launch_request->>'agent_id'=agent_id::text
  AND jsonb_typeof(launch_request->'trigger')='object'
  AND jsonb_typeof(launch_request->'execution_policy')='object'
  AND jsonb_typeof(launch_request->'lineage')='object'
  AND ((launch_request->'trigger'->>'kind'='task' AND launch_request->>'purpose' IN ('task/work','task/review'))
   OR (launch_request->'trigger'->>'kind'='meeting' AND launch_request->>'purpose'='meeting/response'))
 ) IS TRUE),
 CONSTRAINT executions_times CHECK(
  (cancel_requested_at IS NULL OR cancel_requested_at>=created_at)
  AND (started_at IS NULL OR started_at>=created_at)
  AND (completed_at IS NULL OR completed_at>=created_at)
  AND (completed_at IS NULL OR started_at IS NULL OR completed_at>=started_at)),
 CONSTRAINT executions_phase CHECK(
  ((status IN ('succeeded','failed','cancelled'))=(completed_at IS NOT NULL))
  AND (status NOT IN ('created','preparing') OR (snapshot_id IS NULL AND started_at IS NULL))
  AND (status NOT IN ('running','waiting','succeeded') OR (snapshot_id IS NOT NULL AND started_at IS NOT NULL)))
);

-- A waiting Execution retains the same global Agent slot. This constraint and
-- the Agent EX gate serialize Task and Meeting creation across all Projects.
CREATE UNIQUE INDEX executions_active_agent ON agenteam_execution.executions(agent_id)
 WHERE status IN ('created','preparing','running','waiting');
CREATE INDEX executions_project_state ON agenteam_execution.executions(project_id,status,id);

-- +goose StatementBegin
CREATE FUNCTION agenteam_execution.guard_execution_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ROW(NEW.id,NEW.project_id,NEW.agent_id,NEW.idempotency_key,NEW.request_id,NEW.request_digest,NEW.trigger_reference_digest,NEW.launch_request,NEW.initiator,NEW.created_at)
 IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.agent_id,OLD.idempotency_key,OLD.request_id,OLD.request_digest,OLD.trigger_reference_digest,OLD.launch_request,OLD.initiator,OLD.created_at)
 OR OLD.version=9223372036854775807 OR NEW.version<>OLD.version+1 OR NEW.updated_at<=OLD.updated_at
 OR OLD.status IN ('succeeded','failed','cancelled')
 OR (OLD.cancel_requested_at IS NOT NULL AND NEW.cancel_requested_at IS DISTINCT FROM OLD.cancel_requested_at)
 OR (OLD.snapshot_id IS NOT NULL AND NEW.snapshot_id IS DISTINCT FROM OLD.snapshot_id)
 OR (OLD.started_at IS NOT NULL AND NEW.started_at IS DISTINCT FROM OLD.started_at) THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='execution_immutable', MESSAGE='immutable execution identity';
 END IF;
 IF NEW.status<>OLD.status AND NOT (
  (OLD.status='created' AND NEW.status IN ('preparing','failed','cancelled'))
  OR (OLD.status='preparing' AND NEW.status IN ('running','failed','cancelled'))
  OR (OLD.status='running' AND NEW.status IN ('waiting','succeeded','failed','cancelled'))
  OR (OLD.status='waiting' AND NEW.status IN ('running','failed','cancelled'))
 ) THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='execution_transition', MESSAGE='invalid execution transition';
 END IF;
 IF OLD.cancel_requested_at IS NOT NULL AND NEW.status IN ('running','waiting','succeeded') AND NEW.status<>OLD.status THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='execution_cancel_wins', MESSAGE='execution cancellation already requested';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER execution_immutable BEFORE UPDATE ON agenteam_execution.executions
 FOR EACH ROW EXECUTE FUNCTION agenteam_execution.guard_execution_update();

-- Migration 37 stores only the Runtime's scalar references. This actual parent
-- identity is supplied by 38; Runtime cannot create an Execution by itself.
ALTER TABLE agenteam_tool.operations ADD CONSTRAINT tool_operations_execution_scope
 FOREIGN KEY(execution_id,project_id,agent_id)
 REFERENCES agenteam_execution.executions(id,project_id,agent_id) ON DELETE RESTRICT;
