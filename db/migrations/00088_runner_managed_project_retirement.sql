-- agenteam:transaction tx
-- +goose Up
-- This proof preserves the original Agent configuration and logical mapping.
-- It neither edits that configuration nor retires a System Runner, credential,
-- physical directory or unowned Runner operation. Agent Applied consumes it
-- after canonical deletion, so there is deliberately no Agent foreign key.
CREATE TABLE agenteam_mount.project_agent_retirements (
 project_id agenteam_mount.safe_id NOT NULL REFERENCES agenteam_project.projects(id) ON DELETE RESTRICT,
 operation_id agenteam_mount.safe_id NOT NULL REFERENCES agenteam_project.lifecycle_operations(id) ON DELETE RESTRICT,
 project_version bigint NOT NULL CHECK(project_version>0),
 agent_id agenteam_mount.safe_id NOT NULL,
 owner_version bigint NOT NULL CHECK(owner_version>0),
 before_config jsonb NOT NULL CHECK(jsonb_typeof(before_config)='object' AND octet_length(before_config::text)<=8388608),
 before_digest text NOT NULL CHECK(before_digest ~ '^sha256:[0-9a-f]{64}$'),
 mount_snapshot jsonb NOT NULL CHECK(jsonb_typeof(mount_snapshot)='object' AND octet_length(mount_snapshot::text)<=8388608),
 snapshot_digest text NOT NULL CHECK(snapshot_digest ~ '^sha256:[0-9a-f]{64}$'),
 process_id agenteam_mount.safe_id NOT NULL REFERENCES agenteam_object.process_claims(process_id) ON DELETE RESTRICT,
 PRIMARY KEY(project_id,agent_id)
);

-- Additional persisted invariants only. The original private Project round,
-- same Store/live Tx, Guard and actual Agent/Execution returned calls remain
-- mandatory in the owners; no SQL predicate issues those capabilities.
-- +goose StatementBegin
CREATE FUNCTION agenteam_mount.project_retirement_allowed(target_project uuid, participant text)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT EXISTS (
  SELECT 1 FROM agenteam_project.projects p
  JOIN agenteam_project.lifecycle_operations o ON o.id=p.current_lifecycle_operation_id AND o.project_id=p.id
  JOIN agenteam_project.work_claims w ON w.work_kind='lifecycle' AND w.work_id=o.id AND w.project_id=p.id
  JOIN agenteam_project.lifecycle_participants lp ON lp.operation_id=o.id AND lp.participant_name=participant
  JOIN agenteam_project.lifecycle_participants e ON e.operation_id=o.id AND e.participant_name='execution'
  WHERE p.id=target_project AND p.lifecycle='deleting' AND p.initialized_at IS NOT NULL
   AND o.owner_user_id=p.owner_user_id AND o.project_version=p.version
   AND o.action='delete' AND o.state='cleaning' AND o.cleanup_stage='domains'
   AND w.phase='running' AND lp.stop_state='stopped' AND lp.cleanup_state IN ('required','pending') AND lp.attempt_count>0
   AND e.stop_state='stopped' AND e.cleanup_state='completed'
   AND NOT EXISTS(SELECT 1 FROM agenteam_project.lifecycle_participants other WHERE other.operation_id=o.id AND other.stop_state<>'stopped')
 );
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION agenteam_mount.guard_project_agent_retirement() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE head jsonb; refs jsonb; workspaces jsonb; definitions jsonb;
BEGIN
 IF TG_OP='INSERT' THEN
  IF NOT agenteam_mount.project_retirement_allowed(NEW.project_id,'runner-managed')
   OR NOT EXISTS(SELECT 1 FROM agenteam_project.projects p WHERE p.id=NEW.project_id AND p.current_lifecycle_operation_id=NEW.operation_id AND p.version=NEW.project_version)
   OR NOT EXISTS(SELECT 1 FROM agenteam_project.work_claims w WHERE w.work_kind='lifecycle' AND w.work_id=NEW.operation_id AND w.project_id=NEW.project_id AND w.phase='running' AND w.process_id=NEW.process_id)
   OR NOT EXISTS(SELECT 1 FROM agenteam_agent.agents a WHERE a.id=NEW.agent_id AND a.project_id=NEW.project_id AND a.version=NEW.owner_version)
   OR NEW.before_config->>'id' IS DISTINCT FROM NEW.agent_id::text
   OR NEW.before_config->>'project_id' IS DISTINCT FROM NEW.project_id::text
   OR NEW.before_config->'version' IS DISTINCT FROM to_jsonb(NEW.owner_version::text)
  THEN RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='runner_managed_original', MESSAGE='original runner mapping required'; END IF;
  SELECT to_jsonb(h) INTO head FROM agenteam_mount.agent_mount_heads h WHERE h.project_id=NEW.project_id AND h.agent_id=NEW.agent_id AND h.owner_version=NEW.owner_version;
  SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY r.mount_id),'[]'::jsonb) INTO refs FROM agenteam_mount.agent_mount_refs r WHERE r.project_id=NEW.project_id AND r.agent_id=NEW.agent_id;
  SELECT COALESCE(jsonb_agg(to_jsonb(w) ORDER BY w.workspace_id),'[]'::jsonb) INTO workspaces FROM agenteam_mount.runner_workspaces w WHERE w.project_id=NEW.project_id AND w.agent_id=NEW.agent_id;
  SELECT COALESCE(jsonb_agg(to_jsonb(m) ORDER BY m.id),'[]'::jsonb) INTO definitions FROM agenteam_mount.mounts m WHERE m.project_id=NEW.project_id AND m.agent_id=NEW.agent_id;
  IF head IS NULL OR NEW.mount_snapshot IS DISTINCT FROM jsonb_build_object('head',head,'references',refs,'workspaces',workspaces,'definitions',definitions)
   OR jsonb_array_length(refs)>1024 OR jsonb_array_length(workspaces)>1024 OR jsonb_array_length(definitions)>1024
   OR NEW.before_config->'allowed_mount_ids' IS DISTINCT FROM (SELECT COALESCE(jsonb_agg(x.mount_id::text ORDER BY x.mount_id),'[]'::jsonb) FROM agenteam_agent.mount_allowlist x WHERE x.project_id=NEW.project_id AND x.agent_id=NEW.agent_id)
  THEN RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='runner_managed_snapshot', MESSAGE='complete runner mapping required'; END IF;
  RETURN NEW;
 END IF;
 IF TG_OP='DELETE'
  AND agenteam_mount.project_retirement_allowed(OLD.project_id,'agent-skills-variables')
  AND EXISTS(SELECT 1 FROM agenteam_project.projects p JOIN agenteam_project.lifecycle_participants lp ON lp.operation_id=p.current_lifecycle_operation_id AND lp.participant_name='runner-managed' WHERE p.id=OLD.project_id AND p.current_lifecycle_operation_id=OLD.operation_id AND p.version=OLD.project_version AND lp.cleanup_state='completed')
  AND NOT EXISTS(SELECT 1 FROM agenteam_agent.agents WHERE id=OLD.agent_id)
  AND NOT EXISTS(SELECT 1 FROM agenteam_mount.agent_mount_heads WHERE agent_id=OLD.agent_id)
  AND NOT EXISTS(SELECT 1 FROM agenteam_mount.agent_mount_refs WHERE agent_id=OLD.agent_id)
  AND NOT EXISTS(SELECT 1 FROM agenteam_mount.mounts WHERE agent_id=OLD.agent_id)
  AND NOT EXISTS(SELECT 1 FROM agenteam_mount.runner_workspaces WHERE agent_id=OLD.agent_id)
 THEN RETURN OLD; END IF;
 RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='runner_managed_proof_immutable', MESSAGE='runner mapping proof retained';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER runner_managed_proof_guard BEFORE INSERT OR UPDATE OR DELETE ON agenteam_mount.project_agent_retirements
 FOR EACH ROW EXECUTE FUNCTION agenteam_mount.guard_project_agent_retirement();

-- Only exact original logical workspace rows in the immutable proof become
-- deletable. The INSERT validation and all UPDATE rejection remain unchanged.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_mount.check_workspace_definition() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' AND agenteam_mount.project_retirement_allowed(OLD.project_id,'runner-managed')
  AND EXISTS(SELECT 1 FROM agenteam_mount.project_agent_retirements r JOIN agenteam_project.projects p ON p.id=r.project_id WHERE r.project_id=OLD.project_id AND r.agent_id=OLD.agent_id AND r.operation_id=p.current_lifecycle_operation_id AND r.project_version=p.version AND r.mount_snapshot->'workspaces' @> jsonb_build_array(to_jsonb(OLD)))
  AND NOT EXISTS(SELECT 1 FROM agenteam_mount.execution_references WHERE workspace_id=OLD.workspace_id)
 THEN RETURN OLD; END IF;
 IF TG_OP<>'INSERT' THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='runner_workspaces_immutable', MESSAGE='immutable workspace identity';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM agenteam_mount.mounts m
  WHERE m.id=NEW.mount_id AND m.project_id=NEW.project_id AND m.agent_id=NEW.agent_id
   AND m.runner_id=NEW.runner_id AND m.workspace=NEW.workspace) THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='runner_workspaces_definition', MESSAGE='workspace definition mismatch';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
