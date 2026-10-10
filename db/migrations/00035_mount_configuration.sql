-- agenteam:transaction tx
-- +goose Up
-- Logical Mount facts only. No host path, Runner connectivity or filesystem
-- ensure is represented here. All writers serialize on the owning Agent EX.
-- MountCreate/Runner-selection authorization is deliberately not implemented
-- by this schema or by the Agent reference provider.
CREATE SCHEMA agenteam_mount;
CREATE DOMAIN agenteam_mount.safe_id AS uuid CHECK
 (VALUE::text ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$');

CREATE TABLE agenteam_mount.mounts (
 id agenteam_mount.safe_id PRIMARY KEY,
 project_id agenteam_mount.safe_id NOT NULL,
 agent_id agenteam_mount.safe_id NOT NULL,
 runner_id agenteam_mount.safe_id NOT NULL,
 name text COLLATE "C" NOT NULL CHECK(octet_length(name) BETWEEN 1 AND 128 AND name !~ '[[:cntrl:]]'),
 description text CHECK(description IS NULL OR octet_length(description)<=4096),
 workspace text COLLATE "C" NOT NULL CHECK(
  octet_length(workspace) BETWEEN 1 AND 255
  AND workspace ~ '^[A-Za-z0-9_-][A-Za-z0-9_.-]{0,254}$'
  AND right(workspace,1)<>'.'
  AND upper(split_part(workspace,'.',1)) NOT IN ('CON','PRN','AUX','NUL')
  AND upper(split_part(workspace,'.',1)) !~ '^(COM|LPT)[1-9]$'),
 lifecycle text NOT NULL CHECK(lifecycle IN ('active','disabled','removed')),
 version bigint NOT NULL CHECK(version>=1),
 CONSTRAINT mounts_scope UNIQUE(project_id,agent_id,id)
);
CREATE INDEX mounts_agent ON agenteam_mount.mounts(project_id,agent_id,id);

-- Definitions have stable scope/identity; removal retains their logical
-- identity and must never imply recursive removal of physical workspace data.
-- +goose StatementBegin
CREATE FUNCTION agenteam_mount.reject_mount_rewrite() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ROW(NEW.id,NEW.project_id,NEW.agent_id) IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.agent_id)
 OR OLD.lifecycle='removed' OR OLD.version=9223372036854775807 OR NEW.version<>OLD.version+1 THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='mounts_immutable', MESSAGE='immutable mount identity';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER mounts_immutable BEFORE UPDATE ON agenteam_mount.mounts
 FOR EACH ROW EXECUTE FUNCTION agenteam_mount.reject_mount_rewrite();

-- An initialized empty configuration has a real head. Absence is not an
-- implicit success and cannot be repaired without the Agent creation witness.
CREATE TABLE agenteam_mount.agent_mount_heads (
 agent_id agenteam_mount.safe_id PRIMARY KEY,
 project_id agenteam_mount.safe_id NOT NULL,
 owner_version bigint NOT NULL CHECK(owner_version>=1),
 CONSTRAINT agent_mount_heads_scope UNIQUE(project_id,agent_id)
);
CREATE TABLE agenteam_mount.agent_mount_refs (
 project_id agenteam_mount.safe_id NOT NULL,
 agent_id agenteam_mount.safe_id NOT NULL,
 mount_id agenteam_mount.safe_id NOT NULL,
 PRIMARY KEY(agent_id,mount_id),
 FOREIGN KEY(project_id,agent_id) REFERENCES agenteam_mount.agent_mount_heads(project_id,agent_id) ON DELETE RESTRICT,
 FOREIGN KEY(project_id,agent_id,mount_id) REFERENCES agenteam_mount.mounts(project_id,agent_id,id) ON DELETE RESTRICT
);
-- +goose StatementBegin
CREATE FUNCTION agenteam_mount.reject_head_rewrite() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ROW(NEW.agent_id,NEW.project_id) IS DISTINCT FROM ROW(OLD.agent_id,OLD.project_id)
 OR OLD.owner_version=9223372036854775807 OR NEW.owner_version<>OLD.owner_version+1 THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='agent_mount_heads_immutable', MESSAGE='immutable mount configuration identity';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER agent_mount_heads_immutable BEFORE UPDATE ON agenteam_mount.agent_mount_heads
 FOR EACH ROW EXECUTE FUNCTION agenteam_mount.reject_head_rewrite();
