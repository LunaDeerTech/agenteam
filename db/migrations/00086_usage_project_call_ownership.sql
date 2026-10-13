-- agenteam:transaction tx
-- +goose Up
-- Usage owns invocation history in the Model schema. These are whole-call
-- identities, not Model invocation permissions or evidence of a Store return.
CREATE TABLE agenteam_model.usage_project_calls (
 id agenteam_model.safe_id PRIMARY KEY,
 project_id agenteam_model.safe_id NOT NULL REFERENCES agenteam_project.projects(id) ON DELETE RESTRICT,
 process_id agenteam_model.safe_id NOT NULL REFERENCES agenteam_object.process_claims(process_id) ON DELETE RESTRICT,
 instance_id agenteam_model.safe_id NOT NULL,
 lane text NOT NULL CHECK(lane IN ('list','aggregate','summary_get','summary_rebuild','invocation')),
 call_id agenteam_model.safe_id,
 phase text NOT NULL CHECK(phase IN ('running','business_closed')),
 registered_at timestamptz(6) NOT NULL,
 closed_at timestamptz(6),
 CONSTRAINT usage_project_calls_invocation CHECK((lane='invocation')=(call_id IS NOT NULL)),
 CONSTRAINT usage_project_calls_phase_time CHECK(
  (phase='running' AND closed_at IS NULL)
  OR (phase='business_closed' AND closed_at IS NOT NULL AND closed_at>=registered_at)
 )
);
CREATE INDEX usage_project_calls_target ON agenteam_model.usage_project_calls(project_id,id);

-- This SQL predicate is only an additional persisted guard. The original
-- Usage pair, real returned calls, ProcessGuard proofs and current private
-- Project cleanup claim are checked by the Go owners on every transaction.
-- +goose StatementBegin
CREATE FUNCTION agenteam_model.usage_project_call_cleanup_allowed(target_project uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
SELECT EXISTS (
 SELECT 1 FROM agenteam_project.projects p
 JOIN agenteam_project.lifecycle_operations o ON o.id=p.current_lifecycle_operation_id AND o.project_id=p.id
 JOIN agenteam_project.work_claims w ON w.work_kind='lifecycle' AND w.work_id=o.id AND w.project_id=p.id
 JOIN agenteam_project.lifecycle_participants lp ON lp.operation_id=o.id AND lp.participant_name='usage'
 WHERE p.id=target_project AND p.lifecycle='deleting' AND p.initialized_at IS NOT NULL
 AND o.owner_user_id=p.owner_user_id AND o.project_version=p.version
 AND o.action='delete' AND o.state='cleaning' AND o.cleanup_stage='domains'
 AND w.phase='running' AND lp.stop_state='stopped' AND lp.cleanup_state IN ('required','pending') AND lp.attempt_count>0
 AND NOT EXISTS (SELECT 1 FROM agenteam_project.lifecycle_participants other WHERE other.operation_id=o.id AND other.stop_state<>'stopped')
 AND EXISTS (SELECT 1 FROM agenteam_project.lifecycle_participants prior WHERE prior.operation_id=o.id AND prior.participant_name='execution' AND prior.cleanup_state='completed')
)
AND NOT EXISTS (SELECT 1 FROM agenteam_model.invocation_observations WHERE project_id=target_project)
AND NOT EXISTS (SELECT 1 FROM agenteam_model.invocations WHERE project_id=target_project)
AND NOT EXISTS (SELECT 1 FROM agenteam_model.execution_usage_summaries WHERE project_id=target_project)
AND NOT EXISTS (SELECT 1 FROM agenteam_model.usage_project_calls WHERE project_id=target_project AND phase<>'business_closed');
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION agenteam_model.guard_usage_project_call() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN
  IF OLD.phase='business_closed' AND agenteam_model.usage_project_call_cleanup_allowed(OLD.project_id)
  THEN RETURN OLD; END IF;
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='usage_project_calls_retained', MESSAGE='usage call history is retained';
 END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.phase<>'running' OR NEW.closed_at IS NOT NULL THEN
   RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='usage_project_calls_initial', MESSAGE='usage call must start running';
  END IF;
  RETURN NEW;
 END IF;
 IF ROW(NEW.id,NEW.project_id,NEW.process_id,NEW.instance_id,NEW.lane,NEW.call_id,NEW.registered_at)
 IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.process_id,OLD.instance_id,OLD.lane,OLD.call_id,OLD.registered_at)
 OR OLD.phase<>'running' OR NEW.phase<>'business_closed' THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='usage_project_calls_monotonic', MESSAGE='immutable usage call ownership';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER usage_project_calls_guard BEFORE INSERT OR UPDATE OR DELETE ON agenteam_model.usage_project_calls FOR EACH ROW EXECUTE FUNCTION agenteam_model.guard_usage_project_call();
