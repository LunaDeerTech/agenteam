-- agenteam:transaction tx
-- +goose Up
-- Project-only retirement of completed assignment history without any actual
-- Execution. These are extra database invariants, never a caller/Tx witness.
-- The original Agent Applied owner, exact before-image and caller transaction
-- still authorize and atomically commit both owners and the Project checkpoint.
-- No UPDATE protection, queued binding, pending reference or shared Execution
-- immutability guard is relaxed.
-- +goose StatementBegin
CREATE FUNCTION agenteam_skill.project_agent_history_retirement_allowed(target_project uuid,target_agent uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
SELECT EXISTS (
 SELECT 1 FROM agenteam_project.projects p
 JOIN agenteam_project.lifecycle_operations o ON o.id=p.current_lifecycle_operation_id AND o.project_id=p.id
 JOIN agenteam_project.work_claims w ON w.work_kind='lifecycle' AND w.work_id=o.id AND w.project_id=p.id
 JOIN agenteam_project.lifecycle_participants lp ON lp.operation_id=o.id AND lp.participant_name='agent-skills-variables'
 WHERE p.id=target_project AND p.lifecycle='deleting' AND p.initialized_at IS NOT NULL
 AND o.owner_user_id=p.owner_user_id AND o.project_version=p.version
 AND o.action='delete' AND o.state='cleaning' AND o.cleanup_stage='domains'
 AND w.phase='running' AND lp.stop_state='stopped' AND lp.cleanup_state IN ('required','pending')
 AND NOT EXISTS (SELECT 1 FROM agenteam_project.lifecycle_participants other WHERE other.operation_id=o.id AND other.stop_state<>'stopped')
)
AND NOT EXISTS (SELECT 1 FROM agenteam_agent.agents WHERE id=target_agent)
AND NOT EXISTS (SELECT 1 FROM agenteam_execution.executions WHERE agent_id=target_agent::text)
AND EXISTS (SELECT 1 FROM agenteam_skill.agent_assignment_heads WHERE agent_id=target_agent AND project_id=target_project)
AND NOT EXISTS (SELECT 1 FROM agenteam_execution.skill_assignment_ingress WHERE agent_id=target_agent::text)
AND NOT EXISTS (SELECT 1 FROM agenteam_skill.execution_binding_heads WHERE agent_id=target_agent)
AND NOT EXISTS (SELECT 1 FROM agenteam_skill.execution_bindings WHERE agent_id=target_agent)
AND NOT EXISTS (SELECT 1 FROM agenteam_skill.assignment_pending_refs WHERE agent_id=target_agent)
AND NOT EXISTS (SELECT 1 FROM agenteam_skill.assignment_commands WHERE agent_id=target_agent AND (project_id<>target_project OR state<>'completed'))
AND NOT EXISTS (SELECT 1 FROM agenteam_skill.agent_assignments WHERE agent_id=target_agent AND project_id<>target_project)
AND NOT EXISTS (SELECT 1 FROM agenteam_skill.assignment_changes WHERE agent_id=target_agent AND project_id<>target_project)
AND NOT EXISTS (SELECT 1 FROM agenteam_skill.assignment_current WHERE agent_id=target_agent AND project_id<>target_project);
$$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_skill.guard_assignment_command() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' AND OLD.state='completed'
 AND agenteam_skill.project_agent_history_retirement_allowed(OLD.project_id,OLD.agent_id)
 THEN RETURN OLD; END IF;
 -- A completed row is wholly immutable, including its independently frozen
 -- runtime_receipt. Planned rows carry no routing proof by the table guard.
 IF TG_OP='DELETE' OR OLD.state='completed' OR NEW.state<>'completed'
 OR ROW(NEW.id,NEW.project_id,NEW.agent_id,NEW.actor_user_id,NEW.command_name,NEW.idempotency_key,NEW.semantic_digest,NEW.input,NEW.candidate,NEW.created_at)
 IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.agent_id,OLD.actor_user_id,OLD.command_name,OLD.idempotency_key,OLD.semantic_digest,OLD.input,OLD.candidate,OLD.created_at)
 THEN RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='immutable Skill assignment command'; END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_skill.guard_assignment_grant() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' AND agenteam_skill.project_agent_history_retirement_allowed(OLD.project_id,OLD.agent_id)
 THEN RETURN OLD; END IF;
 IF TG_OP='DELETE' OR OLD.removed_at IS NOT NULL OR NEW.removed_at IS NULL
 OR ROW(NEW.id,NEW.project_id,NEW.agent_id,NEW.skill_id,NEW.enabled,NEW.assignment_sequence,NEW.created_at,NEW.change_id)
 IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.agent_id,OLD.skill_id,OLD.enabled,OLD.assignment_sequence,OLD.created_at,OLD.change_id)
 THEN RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='immutable Skill assignment grant'; END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
-- Changes get their own exception. The original shared change/pending guard
-- continues to reject all UPDATE/DELETE of assignment_pending_refs.
-- +goose StatementBegin
CREATE FUNCTION agenteam_skill.guard_assignment_change_retirement() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' AND agenteam_skill.project_agent_history_retirement_allowed(OLD.project_id,OLD.agent_id)
 THEN RETURN OLD; END IF;
 RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='immutable Skill assignment fact';
END;
$$;
-- +goose StatementEnd
DROP TRIGGER skill_assignment_change_immutable ON agenteam_skill.assignment_changes;
CREATE TRIGGER skill_assignment_change_immutable BEFORE UPDATE OR DELETE ON agenteam_skill.assignment_changes
FOR EACH ROW EXECUTE FUNCTION agenteam_skill.guard_assignment_change_retirement();
-- Only this one ingress table changes guard; Snapshot/Transcript/RoundInput
-- and every other user of guard_direct_text_immutable remain untouched.
-- +goose StatementBegin
CREATE FUNCTION agenteam_execution.guard_skill_assignment_ingress_retirement() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE target_project uuid; target_agent uuid;
BEGIN
 target_project:=OLD.project_id::uuid; target_agent:=OLD.agent_id::uuid;
 IF TG_OP='DELETE' AND OLD.outcome='no_active_execution' AND OLD.execution_id IS NULL
 AND EXISTS (
 SELECT 1 FROM agenteam_project.projects p
 JOIN agenteam_project.lifecycle_operations o ON o.id=p.current_lifecycle_operation_id AND o.project_id=p.id
 JOIN agenteam_project.work_claims w ON w.work_kind='lifecycle' AND w.work_id=o.id AND w.project_id=p.id
 JOIN agenteam_project.lifecycle_participants lp ON lp.operation_id=o.id AND lp.participant_name='agent-skills-variables'
 WHERE p.id=target_project AND p.lifecycle='deleting' AND p.initialized_at IS NOT NULL
 AND o.owner_user_id=p.owner_user_id AND o.project_version=p.version
 AND o.action='delete' AND o.state='cleaning' AND o.cleanup_stage='domains'
 AND w.phase='running' AND lp.stop_state='stopped' AND lp.cleanup_state IN ('required','pending')
 AND NOT EXISTS (SELECT 1 FROM agenteam_project.lifecycle_participants other WHERE other.operation_id=o.id AND other.stop_state<>'stopped')
)
AND NOT EXISTS (SELECT 1 FROM agenteam_agent.agents WHERE id=target_agent)
AND NOT EXISTS (SELECT 1 FROM agenteam_execution.executions WHERE agent_id=target_agent::text)
 AND NOT EXISTS (SELECT 1 FROM agenteam_execution.skill_assignment_ingress WHERE agent_id=OLD.agent_id AND (project_id<>OLD.project_id OR outcome<>'no_active_execution' OR execution_id IS NOT NULL))
 THEN RETURN OLD; END IF;
 RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='execution_direct_text_immutable', MESSAGE='immutable execution input';
END;
$$;
-- +goose StatementEnd
DROP TRIGGER execution_skill_assignment_ingress_immutable ON agenteam_execution.skill_assignment_ingress;
CREATE TRIGGER execution_skill_assignment_ingress_immutable BEFORE UPDATE OR DELETE ON agenteam_execution.skill_assignment_ingress
FOR EACH ROW EXECUTE FUNCTION agenteam_execution.guard_skill_assignment_ingress_retirement();
