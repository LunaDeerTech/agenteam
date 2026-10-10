-- agenteam:transaction tx
-- +goose Up
-- Canonical Scheduler intent; Work and Project facts are checked by their
-- original caller-transaction ports. No SQL seed or service name is a grant.
CREATE SCHEMA agenteam_scheduler;
CREATE DOMAIN agenteam_scheduler.safe_id AS text CHECK
 (VALUE ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$');

CREATE TABLE agenteam_scheduler.dispatches (
 id agenteam_scheduler.safe_id PRIMARY KEY,
 project_id agenteam_scheduler.safe_id NOT NULL,
 sprint_id agenteam_scheduler.safe_id NOT NULL,
 task_id agenteam_scheduler.safe_id NOT NULL,
 agent_id agenteam_scheduler.safe_id NOT NULL,
 launch_request bytea NOT NULL CHECK(octet_length(launch_request) BETWEEN 1 AND 262144),
 launch_digest text NOT NULL CHECK(launch_digest ~ '^sha256:[0-9a-f]{64}$'),
 idempotency_key text COLLATE "C" NOT NULL CHECK(idempotency_key='scheduler_dispatch:'||id::text),
 request_id agenteam_scheduler.safe_id NOT NULL,
 status text NOT NULL CHECK(status IN ('pending','launched','failed','skipped')),
 launch_outcome text NOT NULL CHECK(launch_outcome IN ('not_sent','known_not_created','unknown','created')),
 version bigint NOT NULL CHECK(version>=1),
 claim_guard bytea CHECK(octet_length(claim_guard) BETWEEN 1 AND 4096),
 claim_source_sprint_id agenteam_scheduler.safe_id,
 claim_source_state text CHECK(claim_source_state='todo'),
 claim_source_priority text CHECK(claim_source_priority IN ('low','medium','high','critical')),
 execution_id agenteam_scheduler.safe_id,
 attempt_count bigint NOT NULL CHECK(attempt_count>=0),
 next_retry_at timestamptz(6),
 created_at timestamptz(6) NOT NULL,
 updated_at timestamptz(6) NOT NULL CHECK(updated_at>=created_at),
 CONSTRAINT dispatch_identity UNIQUE(project_id,id),
 CONSTRAINT dispatch_launch_key UNIQUE(project_id,agent_id,idempotency_key),
 CONSTRAINT dispatch_execution_unique UNIQUE(execution_id),
 CONSTRAINT dispatch_execution_scope FOREIGN KEY(execution_id,project_id,agent_id)
  REFERENCES agenteam_execution.executions(id,project_id,agent_id) ON DELETE RESTRICT,
 CONSTRAINT dispatch_result_shape CHECK(
  ((status='launched')=(execution_id IS NOT NULL)) AND ((status='launched')=(launch_outcome='created'))
  AND (status NOT IN ('failed','skipped') OR launch_outcome='known_not_created')
  AND ((launch_outcome='not_sent')=(attempt_count=0))
  AND (next_retry_at IS NULL OR (status='pending' AND launch_outcome='known_not_created' AND next_retry_at>=updated_at))),
 CONSTRAINT dispatch_launch_shape CHECK((
  jsonb_typeof(convert_from(launch_request,'UTF8')::jsonb)='object'
  AND convert_from(launch_request,'UTF8')::jsonb ?& ARRAY['project_id','agent_id','trigger','purpose','execution_policy','lineage']
  AND convert_from(launch_request,'UTF8')::jsonb-ARRAY['project_id','agent_id','trigger','purpose','execution_policy','lineage']='{}'::jsonb
  AND convert_from(launch_request,'UTF8')::jsonb->>'project_id'=project_id::text
  AND convert_from(launch_request,'UTF8')::jsonb->>'agent_id'=agent_id::text
  AND convert_from(launch_request,'UTF8')::jsonb->'trigger'->>'kind'='task'
  AND convert_from(launch_request,'UTF8')::jsonb->'trigger'->>'task_id'=task_id::text
  AND convert_from(launch_request,'UTF8')::jsonb->>'purpose' IN ('task/work','task/review')
  AND convert_from(launch_request,'UTF8')::jsonb->'lineage'->>'dispatch_id'=id::text
 ) IS TRUE),
 CONSTRAINT dispatch_claim_shape CHECK(
  (claim_guard IS NULL AND claim_source_sprint_id IS NULL AND claim_source_state IS NULL AND claim_source_priority IS NULL)
  OR (claim_guard IS NOT NULL AND claim_source_sprint_id IS NOT NULL AND claim_source_state IS NOT NULL AND claim_source_priority IS NOT NULL AND (
   jsonb_typeof(convert_from(claim_guard,'UTF8')::jsonb)='object'
   AND convert_from(claim_guard,'UTF8')::jsonb ?& ARRAY['task_id','claimed_version','source_state','source_assignee_id','source_priority','source_sprint_id','source_order_generation']
   AND convert_from(claim_guard,'UTF8')::jsonb-ARRAY['task_id','claimed_version','source_state','source_assignee_id','source_priority','source_sprint_id','source_order_generation','predecessor_id','successor_id']='{}'::jsonb
   AND convert_from(claim_guard,'UTF8')::jsonb->>'task_id'=task_id::text
   AND convert_from(claim_guard,'UTF8')::jsonb->>'source_assignee_id'=agent_id::text
   AND convert_from(claim_guard,'UTF8')::jsonb->>'source_sprint_id'=sprint_id::text
   AND claim_source_sprint_id=sprint_id
   AND convert_from(claim_guard,'UTF8')::jsonb->>'source_state'=claim_source_state
   AND convert_from(claim_guard,'UTF8')::jsonb->>'source_priority'=claim_source_priority
   AND (convert_from(claim_guard,'UTF8')::jsonb->>'claimed_version')::bigint>1
   AND (convert_from(claim_guard,'UTF8')::jsonb->>'source_order_generation')::bigint>0
   AND convert_from(launch_request,'UTF8')::jsonb->>'purpose'='task/work'
  ) IS TRUE))
);
CREATE UNIQUE INDEX dispatch_one_pending_task ON agenteam_scheduler.dispatches(project_id,task_id) WHERE status='pending';
CREATE INDEX dispatch_task_history ON agenteam_scheduler.dispatches(project_id,task_id,status,id);
CREATE INDEX dispatch_project_state ON agenteam_scheduler.dispatches(project_id,status,id);
CREATE INDEX dispatch_pending_source_group ON agenteam_scheduler.dispatches(project_id,claim_source_sprint_id,claim_source_state,claim_source_priority)
 WHERE status='pending' AND claim_guard IS NOT NULL;
CREATE INDEX dispatch_execution_parent ON agenteam_scheduler.dispatches(execution_id,project_id,agent_id) WHERE execution_id IS NOT NULL;

-- +goose StatementBegin
CREATE FUNCTION agenteam_scheduler.guard_dispatch_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ROW(NEW.id,NEW.project_id,NEW.sprint_id,NEW.task_id,NEW.agent_id,NEW.launch_request,NEW.launch_digest,NEW.idempotency_key,NEW.request_id,NEW.claim_guard,NEW.claim_source_sprint_id,NEW.claim_source_state,NEW.claim_source_priority,NEW.created_at)
 IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.sprint_id,OLD.task_id,OLD.agent_id,OLD.launch_request,OLD.launch_digest,OLD.idempotency_key,OLD.request_id,OLD.claim_guard,OLD.claim_source_sprint_id,OLD.claim_source_state,OLD.claim_source_priority,OLD.created_at)
 OR OLD.status<>'pending' OR OLD.version=9223372036854775807 OR NEW.version<>OLD.version+1 OR NEW.updated_at<=OLD.updated_at
 OR NEW.attempt_count<OLD.attempt_count OR NEW.attempt_count>OLD.attempt_count+1
 OR (NEW.attempt_count>OLD.attempt_count AND (NEW.status<>'pending' OR NEW.launch_outcome<>'unknown' OR OLD.launch_outcome NOT IN ('not_sent','known_not_created')))
 OR (NEW.launch_outcome='not_sent' AND OLD.launch_outcome<>'not_sent') THEN
  RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='dispatch_immutable',MESSAGE='invalid dispatch update';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER dispatch_immutable BEFORE UPDATE ON agenteam_scheduler.dispatches
 FOR EACH ROW EXECUTE FUNCTION agenteam_scheduler.guard_dispatch_update();
