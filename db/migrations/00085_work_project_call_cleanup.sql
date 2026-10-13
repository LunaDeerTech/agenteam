-- agenteam:transaction tx
-- +goose Up
-- Work identifiers are UUID (00021). Persisted state is only an additional
-- guard; actual original returns, same instance/foreign proof and the current
-- private Work cleanup family remain mandatory in the Go owners.
-- +goose StatementBegin
CREATE FUNCTION agenteam_work.project_call_cleanup_allowed(target_project uuid)
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

-- Only DELETE changes; preserve 00080 identity and monotonic UPDATE rules.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_work.guard_project_call() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN
  IF OLD.phase='business_closed' AND agenteam_work.project_call_cleanup_allowed(OLD.project_id)
  THEN RETURN OLD; END IF;
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='project_calls_retained', MESSAGE='work call history is retained';
 END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.phase<>'running' OR NEW.closed_at IS NOT NULL THEN
   RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='project_calls_initial', MESSAGE='work call must start running';
  END IF;
  RETURN NEW;
 END IF;
 IF ROW(NEW.id,NEW.project_id,NEW.process_id,NEW.instance_id,NEW.lane,NEW.class,NEW.registered_at)
 IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.process_id,OLD.instance_id,OLD.lane,OLD.class,OLD.registered_at)
 OR OLD.phase<>'running' OR NEW.phase<>'business_closed' THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='project_calls_monotonic', MESSAGE='immutable work call ownership';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
