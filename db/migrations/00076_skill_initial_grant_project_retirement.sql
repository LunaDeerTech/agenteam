-- agenteam:transaction tx
-- +goose Up
-- Only the unchanged initial grant may be removed during Project deletion.
-- These database predicates are additional state invariants, not an issuer:
-- they cannot identify the caller or prove this transaction deleted the Agent.
-- The Skills owner still requires the original private Applied witness and
-- complete caller-held locks; grant, head, Agent and checkpoint share its Tx.
-- No command/change/binding history or ordinary mutation is made deletable.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_skill.guard_assignment_grant() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' AND OLD.assignment_sequence=1 AND OLD.change_id IS NULL
 AND OLD.enabled AND OLD.removed_at IS NULL
 AND EXISTS (
  SELECT 1 FROM agenteam_skill.agent_assignment_heads h
  WHERE h.project_id=OLD.project_id AND h.agent_id=OLD.agent_id
   AND h.add_skills_enabled AND h.assignment_sequence=1
   AND h.initial_assignment_id=OLD.id AND h.initial_skill_id=OLD.skill_id
   AND h.created_at=OLD.created_at
 )
 AND EXISTS (
  SELECT 1 FROM agenteam_project.projects p
  JOIN agenteam_project.lifecycle_operations o ON o.id=p.current_lifecycle_operation_id AND o.project_id=p.id
  JOIN agenteam_project.work_claims w ON w.work_kind='lifecycle' AND w.work_id=o.id AND w.project_id=p.id
  JOIN agenteam_project.lifecycle_participants lp ON lp.operation_id=o.id AND lp.participant_name='agent-skills-variables'
  WHERE p.id=OLD.project_id AND p.lifecycle='deleting' AND p.initialized_at IS NOT NULL
   AND o.owner_user_id=p.owner_user_id AND o.project_version=p.version
   AND o.action='delete' AND o.state='cleaning' AND o.cleanup_stage='domains'
   AND w.phase='running' AND lp.stop_state='stopped' AND lp.cleanup_state IN ('required','pending')
   AND NOT EXISTS (SELECT 1 FROM agenteam_project.lifecycle_participants pending WHERE pending.operation_id=o.id AND pending.stop_state<>'stopped')
 )
 AND NOT EXISTS (SELECT 1 FROM agenteam_agent.agents WHERE id=OLD.agent_id)
 AND NOT EXISTS (SELECT 1 FROM agenteam_execution.executions WHERE agent_id=OLD.agent_id::text)
 AND NOT EXISTS (SELECT 1 FROM agenteam_execution.skill_assignment_ingress WHERE agent_id=OLD.agent_id::text)
 AND NOT EXISTS (SELECT 1 FROM agenteam_skill.agent_assignments WHERE agent_id=OLD.agent_id AND id<>OLD.id)
 AND NOT EXISTS (SELECT 1 FROM agenteam_skill.execution_binding_heads WHERE agent_id=OLD.agent_id)
 AND NOT EXISTS (SELECT 1 FROM agenteam_skill.execution_bindings WHERE agent_id=OLD.agent_id)
 AND NOT EXISTS (SELECT 1 FROM agenteam_skill.assignment_commands WHERE agent_id=OLD.agent_id)
 AND NOT EXISTS (SELECT 1 FROM agenteam_skill.assignment_changes WHERE agent_id=OLD.agent_id)
 AND NOT EXISTS (SELECT 1 FROM agenteam_skill.assignment_current WHERE agent_id=OLD.agent_id)
 AND NOT EXISTS (SELECT 1 FROM agenteam_skill.assignment_pending_refs WHERE agent_id=OLD.agent_id)
 THEN RETURN OLD; END IF;
 IF TG_OP='DELETE' OR OLD.removed_at IS NOT NULL OR NEW.removed_at IS NULL
 OR ROW(NEW.id,NEW.project_id,NEW.agent_id,NEW.skill_id,NEW.enabled,NEW.assignment_sequence,NEW.created_at,NEW.change_id)
 IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.agent_id,OLD.skill_id,OLD.enabled,OLD.assignment_sequence,OLD.created_at,OLD.change_id)
 THEN RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='immutable Skill assignment grant'; END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
