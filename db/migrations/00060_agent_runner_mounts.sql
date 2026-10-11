-- agenteam:transaction tx
-- +goose Up
-- IDs allocated while preparing an Agent command are part of that command's
-- immutable intent mapping. They are not a second Mount command ledger.
ALTER TABLE agenteam_agent.commands ADD COLUMN runner_mounts jsonb;
ALTER TABLE agenteam_agent.commands ADD CONSTRAINT commands_runner_mounts CHECK
 (runner_mounts IS NULL OR (jsonb_typeof(runner_mounts)='array'
  AND jsonb_array_length(runner_mounts)<=128
  AND octet_length(runner_mounts::text)<=131072));

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_agent.reject_command_rewrite() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.state='completed'
 OR ROW(NEW.id,NEW.project_id,NEW.actor_user_id,NEW.target_id,NEW.command_name,NEW.idempotency_key,NEW.semantic_digest,NEW.input,NEW.expected_version,NEW.created_at,NEW.runner_mounts)
 IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.actor_user_id,OLD.target_id,OLD.command_name,OLD.idempotency_key,OLD.semantic_digest,OLD.input,OLD.expected_version,OLD.created_at,OLD.runner_mounts) THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='commands_immutable', MESSAGE='immutable agent command';
 END IF;
 IF NEW.state='completed' THEN
  IF ROW(NEW.plan_revision,NEW.before_config,NEW.after_config,NEW.add_skills_enabled,NEW.install_skill_enabled,NEW.changed_fields)
  IS DISTINCT FROM ROW(OLD.plan_revision,OLD.before_config,OLD.after_config,OLD.add_skills_enabled,OLD.install_skill_enabled,OLD.changed_fields) THEN
   RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='commands_plan', MESSAGE='agent command plan changed';
  END IF;
 ELSIF OLD.plan_revision=9223372036854775807 OR NEW.plan_revision<>OLD.plan_revision+1 THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='commands_plan', MESSAGE='agent command plan revision';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- This is a logical physical-identity mapping only: no host path, ensured
-- directory, connection, heartbeat or execution authority is stored here.
-- Existing definitions remain valid legacy facts. A real explicit Agent
-- command can adopt an unambiguous legacy definition; migration does not.
CREATE TABLE agenteam_mount.runner_workspaces (
 workspace_id agenteam_mount.safe_id PRIMARY KEY,
 mount_id agenteam_mount.safe_id NOT NULL UNIQUE,
 project_id agenteam_mount.safe_id NOT NULL,
 agent_id agenteam_mount.safe_id NOT NULL,
 runner_id agenteam_mount.safe_id NOT NULL,
 workspace text COLLATE "C" NOT NULL CHECK(
  octet_length(workspace) BETWEEN 1 AND 255
  AND workspace ~ '^[A-Za-z0-9_-][A-Za-z0-9_.-]{0,254}$'
  AND right(workspace,1)<>'.'
  AND upper(split_part(workspace,'.',1)) NOT IN ('CON','PRN','AUX','NUL')
  AND upper(split_part(workspace,'.',1)) !~ '^(COM|LPT)[1-9]$'),
 UNIQUE(runner_id,project_id,agent_id,workspace),
 FOREIGN KEY(project_id,agent_id,mount_id) REFERENCES agenteam_mount.mounts(project_id,agent_id,id) ON DELETE RESTRICT
);
CREATE INDEX runner_workspaces_agent ON agenteam_mount.runner_workspaces(project_id,agent_id,mount_id);

-- +goose StatementBegin
CREATE FUNCTION agenteam_mount.check_workspace_definition() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
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
CREATE TRIGGER runner_workspaces_definition BEFORE INSERT OR UPDATE OR DELETE ON agenteam_mount.runner_workspaces
 FOR EACH ROW EXECUTE FUNCTION agenteam_mount.check_workspace_definition();

-- Preserve old definition lifecycle/version rules, adding stable mapping
-- identity for adopted/new definitions. Removing a reference does not delete
-- or retarget either identity, and never implies physical directory removal.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_mount.reject_mount_rewrite() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ROW(NEW.id,NEW.project_id,NEW.agent_id) IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.agent_id)
 OR OLD.lifecycle='removed' OR OLD.version=9223372036854775807 OR NEW.version<>OLD.version+1
 OR (ROW(NEW.runner_id,NEW.workspace) IS DISTINCT FROM ROW(OLD.runner_id,OLD.workspace)
  AND EXISTS(SELECT 1 FROM agenteam_mount.runner_workspaces w WHERE w.mount_id=OLD.id)) THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='mounts_immutable', MESSAGE='immutable mount identity';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
