-- agenteam:transaction tx
-- +goose Up
-- Completion owns an original durable command and a planned two-domain postimage.
-- Existing Sprint lifecycle columns and sprints_one_started remain authoritative.
CREATE TABLE agenteam_work.sprint_complete_commands (
 id agenteam_work.safe_id PRIMARY KEY,
 project_id agenteam_work.safe_id NOT NULL,
 sprint_id agenteam_work.safe_id NOT NULL,
 actor_user_id agenteam_work.safe_id NOT NULL,
 idempotency_key text NOT NULL CHECK (octet_length(idempotency_key) BETWEEN 1 AND 128 AND idempotency_key ~ '^[A-Za-z0-9._:/-]+$'),
 semantic_digest text NOT NULL CHECK (semantic_digest ~ '^sha256:[0-9a-f]{64}$'),
 expected_version bigint NOT NULL CHECK (expected_version > 0),
 plan_revision bigint NOT NULL CHECK (plan_revision > 0),
 state text NOT NULL CHECK (state IN ('planned','completed')),
 plan jsonb NOT NULL CHECK (jsonb_typeof(plan)='object' AND octet_length(plan::text)<=1048576),
 event_id agenteam_work.safe_id NOT NULL UNIQUE,
 receipt jsonb CHECK (receipt IS NULL OR (jsonb_typeof(receipt)='object' AND octet_length(receipt::text)<=262144)),
 created_at timestamptz(6) NOT NULL,
 committed_at timestamptz(6) CHECK (committed_at IS NULL OR committed_at>=created_at),
 CONSTRAINT sprint_complete_commands_identity UNIQUE (project_id,idempotency_key),
 CHECK (((state='planned' AND receipt IS NULL AND committed_at IS NULL)
     OR (state='completed' AND receipt=plan->'after' AND receipt->>'event_id'=event_id::text AND committed_at IS NOT NULL)) IS TRUE),
 CHECK ((plan->'before'->>'id'=sprint_id::text
     AND plan->'before'->>'project_id'=project_id::text
     AND plan->'project'->>'id'=project_id::text
     AND plan->'project'->>'current_sprint_id'=sprint_id::text
     AND plan->'after'->'sprint'->>'id'=sprint_id::text
     AND plan->'after'->'sprint'->>'project_id'=project_id::text
     AND plan->'after'->'project'->>'id'=project_id::text
     AND plan->'after'->'project'->'current_sprint_id'='null'::jsonb
     AND plan->'before'->>'state'='current'
     AND plan->'after'->'sprint'->>'state'='completed'
     AND plan->'after'->>'event_id'=event_id::text) IS TRUE)
);
CREATE INDEX sprint_complete_commands_project_history ON agenteam_work.sprint_complete_commands(project_id,created_at,id);

-- +goose StatementBegin
CREATE FUNCTION agenteam_work.guard_sprint_complete_command() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.id IS DISTINCT FROM OLD.id OR NEW.project_id IS DISTINCT FROM OLD.project_id
    OR NEW.sprint_id IS DISTINCT FROM OLD.sprint_id OR NEW.actor_user_id IS DISTINCT FROM OLD.actor_user_id
    OR NEW.idempotency_key IS DISTINCT FROM OLD.idempotency_key OR NEW.semantic_digest IS DISTINCT FROM OLD.semantic_digest
    OR NEW.expected_version IS DISTINCT FROM OLD.expected_version OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
  RAISE EXCEPTION 'SPRINT_COMPLETE_IDENTITY_IMMUTABLE';
 END IF;
 IF OLD.state='completed' AND NEW IS DISTINCT FROM OLD THEN
  RAISE EXCEPTION 'SPRINT_COMPLETE_RECEIPT_IMMUTABLE';
 END IF;
 IF OLD.state='planned' THEN
  IF NEW.state='planned' AND (NEW.plan_revision<>OLD.plan_revision+1 OR NEW.receipt IS NOT NULL OR NEW.committed_at IS NOT NULL) THEN
   RAISE EXCEPTION 'SPRINT_COMPLETE_REPLAN_INVALID';
  END IF;
  IF NEW.state='completed' AND (NEW.plan_revision<>OLD.plan_revision OR NEW.plan IS DISTINCT FROM OLD.plan OR NEW.event_id IS DISTINCT FROM OLD.event_id) THEN
   RAISE EXCEPTION 'SPRINT_COMPLETE_COMPLETION_INVALID';
  END IF;
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER sprint_complete_command_guard BEFORE UPDATE ON agenteam_work.sprint_complete_commands FOR EACH ROW EXECUTE FUNCTION agenteam_work.guard_sprint_complete_command();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_work.project_call_cleanup_allowed(target_project uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
SELECT EXISTS (
 SELECT 1 FROM agenteam_project.projects p
 JOIN agenteam_project.lifecycle_operations o ON o.id=p.current_lifecycle_operation_id AND o.project_id=p.id
 JOIN agenteam_project.work_claims w ON w.work_kind='lifecycle' AND w.work_id=o.id AND w.project_id=p.id
 JOIN agenteam_project.lifecycle_participants lp ON lp.operation_id=o.id AND lp.participant_name='work'
 WHERE p.id=target_project AND p.lifecycle='deleting' AND p.initialized_at IS NOT NULL
 AND o.owner_user_id=p.owner_user_id AND o.project_version=p.version
 AND o.action='delete' AND o.state='cleaning' AND o.cleanup_stage='domains'
 AND w.phase='running' AND lp.stop_state='stopped' AND lp.cleanup_state IN ('required','pending') AND lp.attempt_count>0
 AND NOT EXISTS (SELECT 1 FROM agenteam_project.lifecycle_participants other WHERE other.operation_id=o.id AND other.stop_state<>'stopped')
 AND EXISTS (SELECT 1 FROM agenteam_project.lifecycle_participants prior WHERE prior.operation_id=o.id AND prior.participant_name='scheduler' AND prior.cleanup_state='completed')
 AND EXISTS (SELECT 1 FROM agenteam_project.lifecycle_participants prior WHERE prior.operation_id=o.id AND prior.participant_name='execution' AND prior.cleanup_state='completed')
)
AND NOT EXISTS (SELECT 1 FROM agenteam_work.task_events WHERE project_id=target_project)
AND NOT EXISTS (SELECT 1 FROM agenteam_work.task_blockers WHERE project_id=target_project)
AND NOT EXISTS (SELECT 1 FROM agenteam_work.task_launch_failures WHERE project_id=target_project)
AND NOT EXISTS (SELECT 1 FROM agenteam_work.task_busy_compensations WHERE project_id=target_project)
AND NOT EXISTS (SELECT 1 FROM agenteam_work.task_scheduler_claims WHERE project_id=target_project)
AND NOT EXISTS (SELECT 1 FROM agenteam_work.task_scheduler_relaunches WHERE project_id=target_project)
AND NOT EXISTS (SELECT 1 FROM agenteam_work.task_blocker_commands WHERE project_id=target_project)
AND NOT EXISTS (SELECT 1 FROM agenteam_work.task_transition_commands WHERE project_id=target_project)
AND NOT EXISTS (SELECT 1 FROM agenteam_work.task_commands WHERE project_id=target_project)
AND NOT EXISTS (SELECT 1 FROM agenteam_work.sprint_start_commands WHERE project_id=target_project)
AND NOT EXISTS (SELECT 1 FROM agenteam_work.sprint_complete_commands WHERE project_id=target_project)
AND NOT EXISTS (SELECT 1 FROM agenteam_work.structure_commands WHERE project_id=target_project)
AND NOT EXISTS (SELECT 1 FROM agenteam_work.tasks WHERE project_id=target_project)
AND NOT EXISTS (SELECT 1 FROM agenteam_work.task_order_groups WHERE project_id=target_project)
AND NOT EXISTS (SELECT 1 FROM agenteam_work.task_query_generations WHERE project_id=target_project)
AND NOT EXISTS (SELECT 1 FROM agenteam_work.sprints WHERE project_id=target_project)
AND NOT EXISTS (SELECT 1 FROM agenteam_work.sprint_order_groups WHERE project_id=target_project)
AND NOT EXISTS (SELECT 1 FROM agenteam_work.milestones WHERE project_id=target_project)
AND NOT EXISTS (SELECT 1 FROM agenteam_work.milestone_order_groups WHERE project_id=target_project)
AND NOT EXISTS (SELECT 1 FROM agenteam_work.project_calls WHERE project_id=target_project AND phase<>'business_closed');
$$;
-- +goose StatementEnd
