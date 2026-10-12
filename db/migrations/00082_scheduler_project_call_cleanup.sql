-- agenteam:transaction tx
-- +goose Up
-- Persisted state is an additional guard, not proof of the current caller,
-- original return, instance ownership, or foreign-process death. Those facts
-- remain mandatory in the Scheduler owner and the private Project cleanup round.
-- Scheduler business project_id is TEXT (00042/00058); call ownership and
-- canonical Project identifiers are UUID (00081/00013).
-- +goose StatementBegin
CREATE FUNCTION agenteam_scheduler.project_call_cleanup_allowed(target_project uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
SELECT EXISTS (
 SELECT 1 FROM agenteam_project.projects p
 JOIN agenteam_project.lifecycle_operations o ON o.id=p.current_lifecycle_operation_id AND o.project_id=p.id
 JOIN agenteam_project.work_claims w ON w.work_kind='lifecycle' AND w.work_id=o.id AND w.project_id=p.id
 JOIN agenteam_project.lifecycle_participants lp ON lp.operation_id=o.id AND lp.participant_name='scheduler'
 WHERE p.id=target_project AND p.lifecycle='deleting' AND p.initialized_at IS NOT NULL
 AND o.owner_user_id=p.owner_user_id AND o.project_version=p.version
 AND o.action='delete' AND o.state='cleaning' AND o.cleanup_stage='domains'
 AND w.phase='running' AND lp.stop_state='stopped' AND lp.cleanup_state IN ('required','pending') AND lp.attempt_count>0
 AND NOT EXISTS (SELECT 1 FROM agenteam_project.lifecycle_participants other WHERE other.operation_id=o.id AND other.stop_state<>'stopped')
)
AND NOT EXISTS (SELECT 1 FROM agenteam_scheduler.task_runtimes WHERE project_id=target_project::text)
AND NOT EXISTS (SELECT 1 FROM agenteam_scheduler.relaunch_visits WHERE project_id=target_project::text)
AND NOT EXISTS (SELECT 1 FROM agenteam_scheduler.dispatches WHERE project_id=target_project::text)
AND NOT EXISTS (SELECT 1 FROM agenteam_scheduler.project_calls WHERE project_id=target_project AND phase<>'business_closed');
$$;
-- +goose StatementEnd

-- Only the DELETE branch changes. The original INSERT/UPDATE identity and
-- running -> business_closed protections from 00081 remain unchanged.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_scheduler.guard_project_call() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN
  IF OLD.phase='business_closed' AND agenteam_scheduler.project_call_cleanup_allowed(OLD.project_id)
  THEN RETURN OLD; END IF;
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='project_calls_retained', MESSAGE='scheduler call history is retained';
 END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.phase<>'running' OR NEW.closed_at IS NOT NULL THEN
   RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='project_calls_initial', MESSAGE='scheduler call must start running';
  END IF;
  RETURN NEW;
 END IF;
 IF ROW(NEW.id,NEW.project_id,NEW.process_id,NEW.instance_id,NEW.lane,NEW.invocation_identity,NEW.invocation_digest,NEW.registered_at)
 IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.process_id,OLD.instance_id,OLD.lane,OLD.invocation_identity,OLD.invocation_digest,OLD.registered_at)
 OR OLD.phase<>'running' OR NEW.phase<>'business_closed' THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='project_calls_monotonic', MESSAGE='immutable scheduler call ownership';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
