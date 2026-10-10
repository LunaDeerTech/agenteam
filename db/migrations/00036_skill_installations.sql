-- agenteam:transaction tx
-- +goose Up
-- Skills owns the complete ordinary-install command and original Object
-- attempt mapping. This does not create a Tool registration or assignment.
CREATE TABLE agenteam_skill.installations (
 id agenteam_skill.safe_id PRIMARY KEY,
 project_id agenteam_skill.safe_id NOT NULL,
 actor_user_id agenteam_skill.safe_id NOT NULL,
 command_key text NOT NULL CHECK(octet_length(command_key) BETWEEN 1 AND 128 AND command_key ~ '^[A-Za-z0-9._:/-]+$'),
 skill_id agenteam_skill.safe_id NOT NULL UNIQUE,
 revision_id agenteam_skill.safe_id NOT NULL UNIQUE,
 semantic_digest text NOT NULL CHECK(semantic_digest ~ '^sha256:[0-9a-f]{64}$'),
 package_sha256 text NOT NULL CHECK(package_sha256 ~ '^sha256:[0-9a-f]{64}$'),
 manifest_sha256 text NOT NULL CHECK(manifest_sha256 ~ '^sha256:[0-9a-f]{64}$'),
 byte_size bigint NOT NULL CHECK(byte_size>0 AND byte_size<=33832982),
 manifest bytea NOT NULL CHECK(octet_length(manifest) BETWEEN 1 AND 1016832),
 name text NOT NULL CHECK(octet_length(name) BETWEEN 1 AND 128),
 normalized_name text COLLATE "C" NOT NULL CHECK(octet_length(normalized_name) BETWEEN 1 AND 384 AND normalized_name<>'add-skills'),
 description text NOT NULL CHECK(octet_length(description) BETWEEN 1 AND 8192),
 phase text NOT NULL CHECK(phase IN ('planned','reserved','published','failed')),
 version bigint NOT NULL CHECK(version>0),
 object_id agenteam_skill.safe_id UNIQUE,
 upload_id agenteam_skill.safe_id UNIQUE,
 current_attempt_id agenteam_skill.safe_id,
 safe_reason text CHECK(safe_reason IN ('dependency_unavailable','work_pending','outcome_unknown','operation_failed')),
 created_at timestamptz(6) NOT NULL,
 updated_at timestamptz(6) NOT NULL CHECK(updated_at>=created_at),
 UNIQUE(project_id,command_key),
 UNIQUE(project_id,id,skill_id,revision_id),
 UNIQUE(project_id,id,skill_id,revision_id,object_id),
 UNIQUE(project_id,id,skill_id,revision_id,object_id,upload_id),
 UNIQUE(project_id,skill_id),
 UNIQUE(project_id,skill_id,revision_id,object_id),
 CHECK((object_id IS NULL)=(upload_id IS NULL)),
 CHECK((phase='planned' AND object_id IS NULL AND current_attempt_id IS NULL AND safe_reason IS NULL)
 OR (phase IN ('reserved','published') AND object_id IS NOT NULL AND current_attempt_id IS NOT NULL AND safe_reason IS NULL)
 OR (phase='failed' AND safe_reason IS NOT NULL AND ((object_id IS NULL AND current_attempt_id IS NULL) OR (object_id IS NOT NULL AND current_attempt_id IS NOT NULL))))
);
CREATE UNIQUE INDEX skill_installation_live_name ON agenteam_skill.installations(project_id,normalized_name) WHERE phase<>'failed';

CREATE TABLE agenteam_skill.installation_attempts (
 attempt_id agenteam_skill.safe_id PRIMARY KEY,
 project_id agenteam_skill.safe_id NOT NULL,
 installation_id agenteam_skill.safe_id NOT NULL,
 skill_id agenteam_skill.safe_id NOT NULL,
 revision_id agenteam_skill.safe_id NOT NULL,
 object_id agenteam_skill.safe_id NOT NULL,
 upload_id agenteam_skill.safe_id NOT NULL,
 process_id agenteam_skill.safe_id NOT NULL,
 created_at timestamptz(6) NOT NULL,
 UNIQUE(attempt_id,project_id,installation_id,skill_id,revision_id,object_id,upload_id),
 FOREIGN KEY(project_id,installation_id,skill_id,revision_id,object_id,upload_id)
 REFERENCES agenteam_skill.installations(project_id,id,skill_id,revision_id,object_id,upload_id) DEFERRABLE INITIALLY DEFERRED
);
ALTER TABLE agenteam_skill.installations ADD CONSTRAINT skill_installation_current_attempt
 FOREIGN KEY(current_attempt_id,project_id,id,skill_id,revision_id,object_id,upload_id)
 REFERENCES agenteam_skill.installation_attempts(attempt_id,project_id,installation_id,skill_id,revision_id,object_id,upload_id) DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX skill_installation_original_object ON agenteam_skill.installation_attempts(object_id,attempt_id);
CREATE INDEX skill_installation_attempt_parent ON agenteam_skill.installation_attempts(project_id,installation_id,attempt_id);

-- +goose StatementBegin
CREATE FUNCTION agenteam_skill.reject_installation_rewrite() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.phase IN ('published','failed')
 OR ROW(NEW.id,NEW.project_id,NEW.actor_user_id,NEW.command_key,NEW.skill_id,NEW.revision_id,NEW.semantic_digest,NEW.package_sha256,NEW.manifest_sha256,NEW.byte_size,NEW.manifest,NEW.name,NEW.normalized_name,NEW.description,NEW.created_at)
 IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.actor_user_id,OLD.command_key,OLD.skill_id,OLD.revision_id,OLD.semantic_digest,OLD.package_sha256,OLD.manifest_sha256,OLD.byte_size,OLD.manifest,OLD.name,OLD.normalized_name,OLD.description,OLD.created_at)
 OR OLD.version=9223372036854775807 OR NEW.version<>OLD.version+1
 OR NEW.updated_at<OLD.updated_at
 OR (OLD.phase='reserved' AND NEW.phase='planned')
 OR (OLD.object_id IS NOT NULL AND ROW(NEW.object_id,NEW.upload_id) IS DISTINCT FROM ROW(OLD.object_id,OLD.upload_id)) THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='installations_immutable', MESSAGE='immutable skill installation';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER installations_immutable BEFORE UPDATE ON agenteam_skill.installations
 FOR EACH ROW EXECUTE FUNCTION agenteam_skill.reject_installation_rewrite();

-- Keep one canonical Skill catalogue. A protected row retains its exact
-- initialization parent; an ordinary row must name its exact install command.
-- Nullable origin columns are never authority: the exclusive origin check and
-- deferred full-tuple FKs retain the parent throughout publication's same Tx.
ALTER TABLE agenteam_skill.skills
 ALTER COLUMN creation_id DROP NOT NULL,
 ADD COLUMN installation_id agenteam_skill.safe_id,
 DROP CONSTRAINT skills_name_check,
 DROP CONSTRAINT skills_normalized_name_check,
 DROP CONSTRAINT skills_protected_check,
 ADD CONSTRAINT skills_name_bounded CHECK(octet_length(name) BETWEEN 1 AND 128),
 ADD CONSTRAINT skills_normalized_name_bounded CHECK(octet_length(normalized_name) BETWEEN 1 AND 384),
 ADD CONSTRAINT skills_exact_origin CHECK(
  (protected AND creation_id IS NOT NULL AND installation_id IS NULL AND name='Add Skills' AND normalized_name='add-skills')
  OR (NOT protected AND creation_id IS NULL AND installation_id IS NOT NULL AND normalized_name<>'add-skills')),
 ADD CONSTRAINT skills_installation_identity UNIQUE(project_id,id,revision_id,installation_id),
 ADD CONSTRAINT skills_installation_parent FOREIGN KEY(project_id,installation_id,id,revision_id)
 REFERENCES agenteam_skill.installations(project_id,id,skill_id,revision_id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE agenteam_skill.revisions
 ADD COLUMN installation_id agenteam_skill.safe_id,
 ADD COLUMN initialization_project_id agenteam_skill.safe_id GENERATED ALWAYS AS (CASE WHEN installation_id IS NULL THEN project_id END) STORED,
 ADD COLUMN installation_project_id agenteam_skill.safe_id GENERATED ALWAYS AS (CASE WHEN installation_id IS NOT NULL THEN project_id END) STORED,
 DROP CONSTRAINT revisions_project_id_skill_id_id_object_id_fkey,
 ADD CONSTRAINT revisions_initialization_parent FOREIGN KEY(initialization_project_id,skill_id,id,object_id)
 REFERENCES agenteam_skill.initializations(project_id,skill_id,revision_id,object_id) DEFERRABLE INITIALLY DEFERRED,
 ADD CONSTRAINT revisions_installation_parent FOREIGN KEY(installation_project_id,installation_id,skill_id,id,object_id)
 REFERENCES agenteam_skill.installations(project_id,id,skill_id,revision_id,object_id) DEFERRABLE INITIALLY DEFERRED,
 ADD CONSTRAINT revisions_installed_core FOREIGN KEY(project_id,skill_id,id,installation_id)
 REFERENCES agenteam_skill.skills(project_id,id,revision_id,installation_id) DEFERRABLE INITIALLY DEFERRED;

-- The original nine-column work projection remains valid. Source is a closed
-- kind, with a real same-domain FK on each side, not an arbitrary parent flag.
ALTER TABLE agenteam_skill.work
 DROP CONSTRAINT work_kind_check,
 ADD CONSTRAINT work_kind_check CHECK(kind IN ('initialization','package_reader','installation','installed_package_reader')),
 ADD COLUMN initialization_project_id agenteam_skill.safe_id GENERATED ALWAYS AS (CASE WHEN kind IN ('initialization','package_reader') THEN project_id END) STORED,
 ADD COLUMN installation_project_id agenteam_skill.safe_id GENERATED ALWAYS AS (CASE WHEN kind IN ('installation','installed_package_reader') THEN project_id END) STORED,
 DROP CONSTRAINT work_project_id_skill_id_fkey,
 ADD CONSTRAINT work_initialization_parent FOREIGN KEY(initialization_project_id,skill_id)
 REFERENCES agenteam_skill.initializations(project_id,skill_id) DEFERRABLE INITIALLY DEFERRED,
 ADD CONSTRAINT work_installation_parent FOREIGN KEY(installation_project_id,skill_id)
 REFERENCES agenteam_skill.installations(project_id,skill_id) DEFERRABLE INITIALLY DEFERRED;

-- Cleanup retains the exact source even if reservation never published a
-- canonical Skill. Initialization still requires its original canonical row;
-- ordinary cleanup is parented by the original complete installation tuple.
ALTER TABLE agenteam_skill.cleanup
 ADD COLUMN installation_id agenteam_skill.safe_id,
 ADD COLUMN initialization_project_id agenteam_skill.safe_id GENERATED ALWAYS AS (CASE WHEN installation_id IS NULL THEN project_id END) STORED,
 ADD COLUMN installation_project_id agenteam_skill.safe_id GENERATED ALWAYS AS (CASE WHEN installation_id IS NOT NULL THEN project_id END) STORED,
 DROP CONSTRAINT cleanup_project_id_skill_id_revision_id_fkey,
 DROP CONSTRAINT cleanup_project_id_skill_id_revision_id_object_id_upload_i_fkey,
 ADD CONSTRAINT cleanup_initialized_core FOREIGN KEY(initialization_project_id,skill_id,revision_id)
 REFERENCES agenteam_skill.skills(project_id,id,revision_id) DEFERRABLE INITIALLY DEFERRED,
 ADD CONSTRAINT cleanup_initialization_parent FOREIGN KEY(initialization_project_id,skill_id,revision_id,object_id,upload_id)
 REFERENCES agenteam_skill.initializations(project_id,skill_id,revision_id,object_id,upload_id) DEFERRABLE INITIALLY DEFERRED,
 ADD CONSTRAINT cleanup_installation_parent FOREIGN KEY(installation_project_id,installation_id,skill_id,revision_id,object_id,upload_id)
 REFERENCES agenteam_skill.installations(project_id,id,skill_id,revision_id,object_id,upload_id) DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX skill_cleanup_installation_parent ON agenteam_skill.cleanup(project_id,installation_id,id) WHERE installation_id IS NOT NULL;
CREATE INDEX skill_work_parent_history ON agenteam_skill.work(project_id,skill_id,phase,id);
CREATE INDEX skill_cleanup_initialization_fk ON agenteam_skill.cleanup(initialization_project_id,skill_id,revision_id,object_id,upload_id) WHERE initialization_project_id IS NOT NULL;
CREATE INDEX skill_cleanup_installation_fk ON agenteam_skill.cleanup(installation_project_id,installation_id,skill_id,revision_id,object_id,upload_id) WHERE installation_project_id IS NOT NULL;
CREATE INDEX skill_work_initialization_fk ON agenteam_skill.work(initialization_project_id,skill_id) WHERE initialization_project_id IS NOT NULL;
CREATE INDEX skill_work_installation_fk ON agenteam_skill.work(installation_project_id,skill_id) WHERE installation_project_id IS NOT NULL;
