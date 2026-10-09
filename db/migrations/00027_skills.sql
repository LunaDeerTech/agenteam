-- agenteam:transaction tx
-- +goose Up
-- Skills owns these facts. Project/Object current authorization is obtained
-- through formal ports in the caller's transaction, never cross-schema FK/SQL.
CREATE SCHEMA agenteam_skill;
CREATE DOMAIN agenteam_skill.safe_id AS uuid CHECK (VALUE::text ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$');
CREATE TABLE agenteam_skill.initializations (
 project_id agenteam_skill.safe_id PRIMARY KEY,
 creation_id agenteam_skill.safe_id NOT NULL UNIQUE,
 initialization_key text NOT NULL CHECK (octet_length(initialization_key) BETWEEN 1 AND 128 AND initialization_key ~ '^[!-~]+$'),
 skill_id agenteam_skill.safe_id NOT NULL UNIQUE,
 revision_id agenteam_skill.safe_id NOT NULL UNIQUE,
 semantic_digest text NOT NULL CHECK (semantic_digest ~ '^sha256:[0-9a-f]{64}$'),
 bundle_id text NOT NULL CHECK (bundle_id='builtin.add-skills.v1'),
 revision bigint NOT NULL CHECK (revision=1),
 package_sha256 text NOT NULL CHECK (package_sha256 ~ '^sha256:[0-9a-f]{64}$'),
 manifest_sha256 text NOT NULL CHECK (manifest_sha256 ~ '^sha256:[0-9a-f]{64}$'),
 byte_size bigint NOT NULL CHECK (byte_size>0 AND byte_size<=262144),
 manifest jsonb NOT NULL CHECK (jsonb_typeof(manifest)='object' AND octet_length(manifest::text)<=1048576),
 name text NOT NULL CHECK (name='Add Skills'),
 description text NOT NULL CHECK (octet_length(description) BETWEEN 1 AND 8192),
 phase text NOT NULL CHECK (phase IN ('planned','reserved','published','failed')),
 version bigint NOT NULL CHECK (version>0),
 object_id agenteam_skill.safe_id UNIQUE,
 upload_id agenteam_skill.safe_id UNIQUE,
 current_attempt_id agenteam_skill.safe_id,
 safe_reason text CHECK (safe_reason IN ('dependency_unbound','dependency_unavailable','work_pending','outcome_unknown','operation_failed')),
 created_at timestamptz(6) NOT NULL,
 updated_at timestamptz(6) NOT NULL CHECK (updated_at>=created_at),
 UNIQUE(project_id,initialization_key),
 UNIQUE(project_id,creation_id,skill_id,revision_id),
 UNIQUE(project_id,skill_id),
 UNIQUE(project_id,skill_id,revision_id,object_id),
 UNIQUE(project_id,skill_id,revision_id,object_id,upload_id),
 UNIQUE(project_id,creation_id,skill_id,revision_id,object_id,upload_id),
 CHECK ((object_id IS NULL)=(upload_id IS NULL)),
 CHECK ((phase='planned' AND object_id IS NULL AND current_attempt_id IS NULL AND safe_reason IS NULL)
 OR (phase IN ('reserved','published') AND object_id IS NOT NULL AND current_attempt_id IS NOT NULL AND safe_reason IS NULL)
 OR (phase='failed' AND safe_reason IS NOT NULL AND ((object_id IS NULL AND current_attempt_id IS NULL) OR (object_id IS NOT NULL AND current_attempt_id IS NOT NULL))))
);
CREATE TABLE agenteam_skill.object_attempts (
 attempt_id agenteam_skill.safe_id PRIMARY KEY,
 project_id agenteam_skill.safe_id NOT NULL,
 creation_id agenteam_skill.safe_id NOT NULL,
 skill_id agenteam_skill.safe_id NOT NULL,
 revision_id agenteam_skill.safe_id NOT NULL,
 object_id agenteam_skill.safe_id NOT NULL,
 upload_id agenteam_skill.safe_id NOT NULL,
 process_id agenteam_skill.safe_id NOT NULL,
 created_at timestamptz(6) NOT NULL,
 UNIQUE(attempt_id,project_id,creation_id,skill_id,revision_id,object_id,upload_id),
 FOREIGN KEY(project_id,creation_id,skill_id,revision_id,object_id,upload_id)
 REFERENCES agenteam_skill.initializations(project_id,creation_id,skill_id,revision_id,object_id,upload_id) DEFERRABLE INITIALLY DEFERRED
);
ALTER TABLE agenteam_skill.initializations ADD CONSTRAINT skill_initialization_current_attempt
 FOREIGN KEY(current_attempt_id,project_id,creation_id,skill_id,revision_id,object_id,upload_id)
 REFERENCES agenteam_skill.object_attempts(attempt_id,project_id,creation_id,skill_id,revision_id,object_id,upload_id) DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX skill_attempts_original_object ON agenteam_skill.object_attempts(object_id,attempt_id);
CREATE TABLE agenteam_skill.skills (
 id agenteam_skill.safe_id PRIMARY KEY,
 project_id agenteam_skill.safe_id NOT NULL,
 creation_id agenteam_skill.safe_id NOT NULL,
 revision_id agenteam_skill.safe_id NOT NULL UNIQUE,
 name text NOT NULL CHECK(name='Add Skills'),
 normalized_name text NOT NULL CHECK(normalized_name='add-skills'),
 description text NOT NULL CHECK(octet_length(description) BETWEEN 1 AND 8192),
 protected boolean NOT NULL CHECK(protected),
 current_revision bigint NOT NULL CHECK(current_revision=1),
 version bigint NOT NULL CHECK(version>0),
 serving boolean NOT NULL,
 UNIQUE(project_id,normalized_name),
 UNIQUE(project_id,id,revision_id),
 FOREIGN KEY(project_id,creation_id,id,revision_id)
 REFERENCES agenteam_skill.initializations(project_id,creation_id,skill_id,revision_id) DEFERRABLE INITIALLY DEFERRED
);
CREATE TABLE agenteam_skill.revisions (
 id agenteam_skill.safe_id PRIMARY KEY,
 project_id agenteam_skill.safe_id NOT NULL,
 skill_id agenteam_skill.safe_id NOT NULL,
 revision bigint NOT NULL CHECK(revision=1),
 object_id agenteam_skill.safe_id NOT NULL UNIQUE,
 object_version bigint NOT NULL CHECK(object_version>0),
 object_created_at timestamptz(6) NOT NULL,
 published_at timestamptz(6) NOT NULL CHECK(published_at>=object_created_at),
 UNIQUE(skill_id,revision),
 FOREIGN KEY(project_id,skill_id,id) REFERENCES agenteam_skill.skills(project_id,id,revision_id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(project_id,skill_id,id,object_id) REFERENCES agenteam_skill.initializations(project_id,skill_id,revision_id,object_id) DEFERRABLE INITIALLY DEFERRED
);
-- Manifest, description, size and digests remain in the immutable original
-- initialization row; revision reads join that exact same-domain mapping.
CREATE TABLE agenteam_skill.work (
 id agenteam_skill.safe_id PRIMARY KEY,
 project_id agenteam_skill.safe_id NOT NULL,
 skill_id agenteam_skill.safe_id NOT NULL,
 process_id agenteam_skill.safe_id NOT NULL,
 kind text NOT NULL CHECK(kind IN ('initialization','package_reader')),
 phase text NOT NULL CHECK(phase IN ('running','joined','unknown')),
 fence bigint NOT NULL CHECK(fence>0),
 recovery_pass bigint NOT NULL DEFAULT 0 CHECK(recovery_pass>=0),
 created_at timestamptz(6) NOT NULL,
 joined_at timestamptz(6),
 CHECK((phase='joined' AND joined_at IS NOT NULL AND joined_at>=created_at) OR (phase<>'joined' AND joined_at IS NULL)),
 FOREIGN KEY(project_id,skill_id) REFERENCES agenteam_skill.initializations(project_id,skill_id) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX skill_work_live_project ON agenteam_skill.work(project_id,id) WHERE phase<>'joined';
CREATE INDEX skill_work_recovery ON agenteam_skill.work(recovery_pass,id) WHERE phase<>'joined';
CREATE TABLE agenteam_skill.cleanup (
 id agenteam_skill.safe_id PRIMARY KEY,
 project_id agenteam_skill.safe_id NOT NULL,
 lifecycle_operation_id agenteam_skill.safe_id NOT NULL,
 project_version bigint NOT NULL CHECK(project_version>0),
 action text NOT NULL CHECK(action='delete'),
 skill_id agenteam_skill.safe_id NOT NULL,
 revision_id agenteam_skill.safe_id NOT NULL,
 object_id agenteam_skill.safe_id NOT NULL,
 upload_id agenteam_skill.safe_id NOT NULL,
 phase text NOT NULL CHECK(phase IN ('gated','pending','completed')),
 version bigint NOT NULL CHECK(version>0),
 created_at timestamptz(6) NOT NULL,
 updated_at timestamptz(6) NOT NULL CHECK(updated_at>=created_at),
 UNIQUE(project_id,lifecycle_operation_id,revision_id),
 FOREIGN KEY(project_id,skill_id,revision_id) REFERENCES agenteam_skill.skills(project_id,id,revision_id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(project_id,skill_id,revision_id,object_id,upload_id) REFERENCES agenteam_skill.initializations(project_id,skill_id,revision_id,object_id,upload_id) DEFERRABLE INITIALLY DEFERRED
);
