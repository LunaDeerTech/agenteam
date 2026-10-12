-- agenteam:transaction tx
-- +goose Up
-- The database checks only persisted state. It cannot identify the original
-- caller, prove a local call returned, or authorize a foreign process. Those
-- checks belong to the concrete pair and the private Project terminal scope.
-- The command anchor, both owners' closed ledgers and schema 6 replace schema 5
-- in one caller transaction. No ordinary UPDATE protection is relaxed.
-- +goose StatementBegin
CREATE FUNCTION agenteam_project.agent_variables_terminal_cleanup_allowed(target_project uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
SELECT EXISTS (
 SELECT 1 FROM agenteam_project.projects p
 JOIN agenteam_project.lifecycle_operations o ON o.id=p.current_lifecycle_operation_id AND o.project_id=p.id
 JOIN agenteam_project.work_claims w ON w.work_kind='lifecycle' AND w.work_id=o.id AND w.project_id=p.id
 JOIN agenteam_project.lifecycle_participants lp ON lp.operation_id=o.id AND lp.participant_name='agent-skills-variables'
 WHERE p.id=target_project AND p.lifecycle='deleting' AND p.initialized_at IS NOT NULL
 AND o.owner_user_id=p.owner_user_id AND o.project_version=p.version
 AND o.action='delete' AND o.state='cleaning' AND o.cleanup_stage='domains'
 AND w.phase='running' AND lp.stop_state='stopped' AND lp.cleanup_state='pending'
 AND lp.checkpoint_schema=5 AND lp.checkpoint IS NOT NULL
 AND (convert_from(lp.checkpoint,'UTF8')::jsonb)->>'project_id'=p.id::text
 AND (convert_from(lp.checkpoint,'UTF8')::jsonb)->>'operation_id'=o.id::text
 AND (convert_from(lp.checkpoint,'UTF8')::jsonb)->>'project_version'=o.project_version::text
 AND (convert_from(lp.checkpoint,'UTF8')::jsonb)->>'variables_state'='completed'
 AND NOT EXISTS (SELECT 1 FROM agenteam_project.lifecycle_participants other WHERE other.operation_id=o.id AND other.stop_state<>'stopped')
)
AND NOT EXISTS (SELECT 1 FROM agenteam_agent.agents WHERE project_id=target_project)
AND NOT EXISTS (SELECT 1 FROM agenteam_agent.commands WHERE project_id=target_project AND state<>'completed')
AND NOT EXISTS (SELECT 1 FROM agenteam_agent.project_calls WHERE project_id=target_project AND phase<>'joined')
AND NOT EXISTS (SELECT 1 FROM agenteam_projectvariable.project_calls WHERE project_id=target_project AND phase<>'business_closed')
AND NOT EXISTS (SELECT 1 FROM agenteam_projectvariable.history WHERE project_id=target_project)
AND NOT EXISTS (SELECT 1 FROM agenteam_projectvariable.secret_history WHERE project_id=target_project)
AND NOT EXISTS (SELECT 1 FROM agenteam_projectvariable.commands WHERE project_id=target_project)
AND NOT EXISTS (SELECT 1 FROM agenteam_projectvariable.secret_commands WHERE project_id=target_project)
AND NOT EXISTS (SELECT 1 FROM agenteam_projectvariable.variables WHERE project_id=target_project)
AND NOT EXISTS (SELECT 1 FROM agenteam_projectvariable.project_generations WHERE project_id=target_project)
AND NOT EXISTS (SELECT 1 FROM agenteam_projectvariable.secret_project_generations WHERE project_id=target_project);
$$;
-- +goose StatementEnd

-- Agent commands previously guarded UPDATE only. DELETE now has its own
-- narrow guard; the original completed-command/plan UPDATE trigger is intact.
-- +goose StatementBegin
CREATE FUNCTION agenteam_agent.guard_terminal_command_delete() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.state='completed' AND agenteam_project.agent_variables_terminal_cleanup_allowed(OLD.project_id)
 THEN RETURN OLD; END IF;
 RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='commands_retained', MESSAGE='agent command history is retained';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER commands_terminal_delete BEFORE DELETE ON agenteam_agent.commands
FOR EACH ROW EXECUTE FUNCTION agenteam_agent.guard_terminal_command_delete();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_agent.guard_project_call() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN
  IF OLD.phase='joined' AND agenteam_project.agent_variables_terminal_cleanup_allowed(OLD.project_id)
  THEN RETURN OLD; END IF;
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='project_calls_retained', MESSAGE='agent call history is retained';
 END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.phase<>'running' OR NEW.joined_at IS NOT NULL THEN
   RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='project_calls_initial', MESSAGE='agent call must start running';
  END IF;
  RETURN NEW;
 END IF;
 IF ROW(NEW.id,NEW.project_id,NEW.process_id,NEW.instance_id,NEW.lane,NEW.class,NEW.registered_at)
 IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.process_id,OLD.instance_id,OLD.lane,OLD.class,OLD.registered_at)
 OR OLD.phase<>'running' OR NEW.phase<>'joined' THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='project_calls_monotonic', MESSAGE='immutable agent call ownership';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_projectvariable.guard_project_call() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN
  IF OLD.phase='business_closed' AND agenteam_project.agent_variables_terminal_cleanup_allowed(OLD.project_id)
  THEN RETURN OLD; END IF;
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='project_calls_retained', MESSAGE='project variable call history is retained';
 END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.phase<>'running' OR NEW.closed_at IS NOT NULL THEN
   RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='project_calls_initial', MESSAGE='project variable call must start running';
  END IF;
  RETURN NEW;
 END IF;
 IF ROW(NEW.id,NEW.project_id,NEW.process_id,NEW.instance_id,NEW.lane,NEW.class,NEW.registered_at)
 IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.process_id,OLD.instance_id,OLD.lane,OLD.class,OLD.registered_at)
 OR OLD.phase<>'running' OR NEW.phase<>'business_closed' THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='project_calls_monotonic', MESSAGE='immutable project variable call ownership';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
