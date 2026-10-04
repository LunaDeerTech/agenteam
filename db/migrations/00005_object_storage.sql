-- agenteam:transaction tx
-- +goose Up

-- Extend the original D04 CHECKs without changing migration 00002 or dropping
-- any old valid actor, producer, action or resource. The unnamed table CHECKs
-- below are PostgreSQL's actual names from that frozen CREATE TABLE sequence.
ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT audit_records_action_check;
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_action_check CHECK (action IN (
  'secret.create','secret.update','secret.delete','secret.resolve','secret.master.register','secret.master.rotation.start','secret.master.rotation.complete','secret.master.rotation.failed','outbound.policy.update','outbound.access.deny',
  'object.upload.complete','object.upload.failed','object.delete','object.transfer.issue','object.transfer.complete','object.transfer.revoke','artifact.create','artifact.list','artifact.read','artifact.download'));
ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT audit_records_resource_kind_check;
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_resource_kind_check CHECK (resource_kind IN ('secret','secret_master','secret_rotation','outbound_policy','agent','stored_object','object_transfer','artifact','artifact_collection'));
ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT audit_records_producer_check;
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_producer_check CHECK (producer IN ('secret','secret.master','outbound.policy','outbound.access','object','artifact'));
ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT audit_records_check1;
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_check1 CHECK (
  (actor_kind='human' AND user_id IS NOT NULL AND session_id IS NOT NULL AND actor_project_id IS NULL AND agent_id IS NULL AND actor_execution_id IS NULL AND service_name IS NULL AND service_cause IS NULL)
  OR (actor_kind='agent_run' AND user_id IS NULL AND session_id IS NULL AND actor_project_id IS NOT NULL AND agent_id IS NOT NULL AND actor_execution_id IS NOT NULL AND service_name IS NULL AND service_cause IS NULL AND scope='project' AND actor_project_id=project_id AND execution_id=actor_execution_id)
  OR (actor_kind='service' AND user_id IS NULL AND session_id IS NULL AND agent_id IS NULL AND actor_execution_id IS NULL AND service_name IN ('secret','secret-maintenance','outbound','project-lifecycle','object','object-maintenance') AND service_cause IS NOT NULL AND actor_project_id IS NOT DISTINCT FROM project_id));
ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT audit_records_check2;
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_check2 CHECK (
  (resource_kind IN ('secret_master','outbound_policy') AND resource_id IS NULL)
  OR (resource_kind IN ('secret','secret_rotation','agent','stored_object','object_transfer','artifact','artifact_collection') AND resource_id IS NOT NULL));
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_content_contract CHECK (
  (producer NOT IN ('object','artifact') AND action NOT LIKE 'object.%' AND action NOT LIKE 'artifact.%')
  OR (producer='object' AND actor_kind='service' AND service_name IN ('object','object-maintenance') AND service_cause=cause_ref
    AND ((action IN ('object.upload.complete','object.upload.failed','object.delete') AND resource_kind='stored_object')
      OR (action IN ('object.transfer.issue','object.transfer.complete','object.transfer.revoke') AND resource_kind='object_transfer' AND scope='project')))
  OR (producer='artifact' AND actor_kind IN ('human','agent_run') AND scope='project'
    AND ((action IN ('artifact.create','artifact.read','artifact.download') AND resource_kind='artifact')
      OR (action='artifact.list' AND resource_kind='artifact_collection' AND resource_id=project_id))));

CREATE SCHEMA agenteam_object;
CREATE DOMAIN agenteam_object.safe_id AS uuid CHECK (VALUE::text ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$');

CREATE TABLE agenteam_object.store_identity (
  singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
  instance_id agenteam_object.safe_id NOT NULL UNIQUE,
  phase text NOT NULL CHECK (phase IN ('reserved','confirmed')),
  created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE agenteam_object.objects (
  id agenteam_object.safe_id PRIMARY KEY,
  scope text NOT NULL CHECK (scope IN ('system','project')),
  partition_id agenteam_object.safe_id NOT NULL,
  project_id agenteam_object.safe_id,
  media_type text NOT NULL CHECK (octet_length(media_type) BETWEEN 1 AND 256),
  byte_size bigint NOT NULL CHECK (byte_size BETWEEN 0 AND 1073741824),
  sha256 bytea NOT NULL CHECK (octet_length(sha256)=32),
  state text NOT NULL CHECK (state IN ('pending','available','failed','deleted')),
  version bigint NOT NULL DEFAULT 1 CHECK (version>0),
  candidate_key text UNIQUE CHECK (candidate_key ~ '^candidate/[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'),
  cleaning boolean NOT NULL DEFAULT false,
  project_cleanup_pass bigint NOT NULL DEFAULT 0 CHECK (project_cleanup_pass>=0),
  created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
  deleted_at timestamptz(6),
  CHECK ((scope='system' AND project_id IS NULL) OR (scope='project' AND project_id=partition_id)),
  CHECK (state<>'available' OR candidate_key IS NOT NULL),
  CHECK ((state='deleted')=(deleted_at IS NOT NULL))
);

CREATE TABLE agenteam_object.uploads (
  id agenteam_object.safe_id PRIMARY KEY,
  object_id agenteam_object.safe_id NOT NULL UNIQUE REFERENCES agenteam_object.objects(id),
  command_hash bytea NOT NULL UNIQUE CHECK (octet_length(command_hash)=32),
  command_key text NOT NULL CHECK (octet_length(command_key) BETWEEN 1 AND 128 AND command_key ~ '^[A-Za-z0-9._:/-]+$'),
  semantic_digest bytea NOT NULL CHECK (octet_length(semantic_digest)=32),
  expected_version bigint CHECK (expected_version>0),
  owner_kind text NOT NULL CHECK (owner_kind IN ('avatar','artifact','knowledge','skill_revision','mcp_content','execution_payload','meeting_file')),
  owner_id agenteam_object.safe_id NOT NULL,
  project_id agenteam_object.safe_id,
  stable_actor text NOT NULL CHECK (octet_length(stable_actor)<=1024),
  initiator_kind text NOT NULL CHECK (initiator_kind IN ('human','agent_run','service')),
  initiator_id agenteam_object.safe_id NOT NULL,
  initiator_execution_id agenteam_object.safe_id,
  existence text NOT NULL CHECK (existence IN ('existing','prospective')),
  creation_cause text CHECK (creation_cause ~ '^(sha256:[0-9a-f]{64}|[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12})$'),
  state text NOT NULL CHECK (state IN ('pending','unknown','failed','committed')),
  disposition text NOT NULL CHECK (disposition IN ('reserved','attached','revoked')),
  receipt_id agenteam_object.safe_id NOT NULL UNIQUE,
  current_attempt_id agenteam_object.safe_id,
  audit_id uuid REFERENCES agenteam_audit.audit_records(id),
  created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
  CHECK ((owner_kind='avatar' AND project_id IS NULL) OR (owner_kind<>'avatar' AND project_id IS NOT NULL)),
  CHECK (existence<>'prospective' OR creation_cause IS NOT NULL),
  CHECK ((initiator_kind='agent_run')=(initiator_execution_id IS NOT NULL)),
  CHECK (disposition<>'attached' OR state='committed'),
  CHECK (state<>'committed' OR audit_id IS NOT NULL)
);

CREATE TABLE agenteam_object.upload_attempts (
  id agenteam_object.safe_id PRIMARY KEY,
  upload_id agenteam_object.safe_id NOT NULL REFERENCES agenteam_object.uploads(id),
  object_id agenteam_object.safe_id NOT NULL REFERENCES agenteam_object.objects(id),
  ordinal bigint NOT NULL CHECK (ordinal>0),
  candidate_key text NOT NULL UNIQUE CHECK (candidate_key ~ '^candidate/[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'),
  phase text NOT NULL CHECK (phase IN ('reserved','sending','unknown','verified','published','failed','abandoned','cleaned')),
  process_id agenteam_object.safe_id NOT NULL,
  spool_payload_id agenteam_object.safe_id NOT NULL,
  maybe_late boolean NOT NULL DEFAULT false,
  io_closed boolean NOT NULL DEFAULT false,
  cleanup_gate boolean NOT NULL DEFAULT false,
  byte_size bigint NOT NULL CHECK (byte_size BETWEEN 0 AND 1073741824),
  sha256 bytea NOT NULL CHECK (octet_length(sha256)=32),
  fault_code text CHECK (fault_code IN ('DEPENDENCY_UNAVAILABLE','OBJECT_PAYLOAD_MISSING','OBJECT_INTEGRITY_MISMATCH','COMMIT_UNKNOWN','SHUTTING_DOWN')),
  created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
  UNIQUE(upload_id,ordinal)
);
ALTER TABLE agenteam_object.uploads ADD CONSTRAINT uploads_current_attempt_fkey FOREIGN KEY(current_attempt_id) REFERENCES agenteam_object.upload_attempts(id);

CREATE TABLE agenteam_object.object_references (
  object_id agenteam_object.safe_id NOT NULL REFERENCES agenteam_object.objects(id),
  owner_kind text NOT NULL CHECK (owner_kind IN ('avatar','artifact','knowledge','skill_revision','mcp_content','execution_payload','meeting_file')),
  owner_id agenteam_object.safe_id NOT NULL,
  partition_id agenteam_object.safe_id NOT NULL,
  kind text NOT NULL CHECK (kind IN ('reserved','canonical')),
  upload_id agenteam_object.safe_id REFERENCES agenteam_object.uploads(id),
  created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY(object_id,owner_kind,owner_id),
  CHECK (kind<>'reserved' OR upload_id IS NOT NULL)
);

CREATE TABLE agenteam_object.object_leases (
  id agenteam_object.safe_id PRIMARY KEY,
  object_id agenteam_object.safe_id NOT NULL REFERENCES agenteam_object.objects(id),
  attempt_id agenteam_object.safe_id REFERENCES agenteam_object.upload_attempts(id),
  owner_kind text NOT NULL CHECK (owner_kind IN ('reader','source','writer','execution','history','transfer')),
  owner_id agenteam_object.safe_id NOT NULL,
  process_id agenteam_object.safe_id,
  state text NOT NULL CHECK (state IN ('active','released')),
  created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
  released_at timestamptz(6),
  UNIQUE(object_id,owner_kind,owner_id),
  CHECK ((owner_kind IN ('reader','source','writer'))=(process_id IS NOT NULL)),
  CHECK (owner_kind<>'writer' OR attempt_id IS NOT NULL),
  CHECK ((state='released')=(released_at IS NOT NULL))
);

CREATE TABLE agenteam_object.cleanup_operations (
  id agenteam_object.safe_id PRIMARY KEY,
  operation_id agenteam_object.safe_id NOT NULL,
  object_id agenteam_object.safe_id NOT NULL REFERENCES agenteam_object.objects(id),
  attempt_id agenteam_object.safe_id NOT NULL UNIQUE REFERENCES agenteam_object.upload_attempts(id),
  reason text NOT NULL CHECK (reason IN ('cancelled_upload','abandoned_attempt','owner_deleted','project_deleted','replaced_object')),
  mode text NOT NULL CHECK (mode IN ('delete','zero_marker')),
  phase text NOT NULL CHECK (phase IN ('gated','applying','verified','completed')),
  worker_id agenteam_object.safe_id,
  fence bigint NOT NULL DEFAULT 1 CHECK (fence>0),
  fault_code text CHECK (fault_code IN ('DEPENDENCY_UNAVAILABLE','OBJECT_INTEGRITY_MISMATCH','COMMIT_UNKNOWN','SHUTTING_DOWN')),
  created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX objects_project_state ON agenteam_object.objects(project_id,state,created_at,id);
CREATE INDEX objects_project_cleanup ON agenteam_object.objects(project_id,project_cleanup_pass,id);
CREATE INDEX uploads_owner ON agenteam_object.uploads(owner_kind,owner_id,created_at,id);
CREATE INDEX attempts_recovery ON agenteam_object.upload_attempts(phase,created_at,id) WHERE phase NOT IN ('published','cleaned');
CREATE INDEX leases_object_active ON agenteam_object.object_leases(object_id,owner_kind,id) WHERE state='active';
CREATE INDEX leases_process_active ON agenteam_object.object_leases(process_id,id) WHERE state='active';
CREATE INDEX cleanup_recovery ON agenteam_object.cleanup_operations(phase,created_at,id) WHERE phase<>'completed';
