-- agenteam:transaction tx
-- +goose Up
-- Artifact owns its business metadata/commands. Object payload state is changed
-- only through the object service's InTx ports, never by this schema's triggers.
CREATE SCHEMA agenteam_artifact;

CREATE TABLE agenteam_artifact.upload_intents (
  id agenteam_object.safe_id PRIMARY KEY,
  file_id agenteam_object.safe_id NOT NULL UNIQUE,
  project_id agenteam_object.safe_id NOT NULL,
  command_hash bytea NOT NULL UNIQUE CHECK (octet_length(command_hash)=32),
  command_key text NOT NULL,
  request_digest bytea NOT NULL CHECK (octet_length(request_digest)=32),
  stable_actor text NOT NULL,
  creation_cause text NOT NULL,
  state text NOT NULL CHECK (state IN ('pending','uploaded','consumed','cancel_requested','cancelled','deleted')),
  expected_version bigint CHECK (expected_version>0),
  name text NOT NULL CHECK (octet_length(name) BETWEEN 1 AND 255),
  description text NOT NULL CHECK (octet_length(description)<=4096),
  media_type text NOT NULL,
  byte_size bigint NOT NULL CHECK (byte_size BETWEEN 0 AND 1073741824),
  expected_sha256 bytea CHECK (octet_length(expected_sha256)=32),
  sha256 bytea CHECK (octet_length(sha256)=32),
  target_object_id agenteam_object.safe_id,
  target_upload_id agenteam_object.safe_id,
  target_attempt_id agenteam_object.safe_id,
  receipt_id agenteam_object.safe_id,
  object_version bigint CHECK (object_version>0),
  object_created_at timestamptz(6),
  execution_id agenteam_object.safe_id,
  operation_id agenteam_object.safe_id,
  tool_id agenteam_object.safe_id,
  tool_call_id agenteam_object.safe_id,
  created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
  CHECK (operation_id IS NULL OR execution_id IS NOT NULL),
  CHECK (tool_call_id IS NULL OR tool_id IS NOT NULL),
  CHECK (state NOT IN ('uploaded','consumed') OR sha256 IS NOT NULL AND target_object_id IS NOT NULL AND target_upload_id IS NOT NULL AND receipt_id IS NOT NULL AND object_version IS NOT NULL AND object_created_at IS NOT NULL)
);

CREATE TABLE agenteam_artifact.commands (
  command_hash bytea PRIMARY KEY CHECK (octet_length(command_hash)=32),
  command_key text NOT NULL,
  project_id agenteam_object.safe_id NOT NULL,
  artifact_id agenteam_object.safe_id NOT NULL UNIQUE,
  file_id agenteam_object.safe_id NOT NULL UNIQUE,
  stable_actor text NOT NULL,
  request_digest bytea NOT NULL CHECK (octet_length(request_digest)=32),
  creation_cause text NOT NULL,
  path text NOT NULL CHECK (path IN ('content','source','upload')),
  kind text NOT NULL CHECK (kind IN ('generated','user_upload')),
  state text NOT NULL CHECK (state IN ('pending','completed','cancel_requested','cancelled','deleted')),
  name text NOT NULL CHECK (octet_length(name) BETWEEN 1 AND 255),
  description text NOT NULL CHECK (octet_length(description)<=4096),
  media_type text NOT NULL,
  byte_size bigint NOT NULL CHECK (byte_size BETWEEN 0 AND 1073741824),
  sha256 bytea NOT NULL CHECK (octet_length(sha256)=32),
  source jsonb,
  source_lease_id agenteam_object.safe_id,
  cleanup_pass bigint NOT NULL DEFAULT 0 CHECK (cleanup_pass>=0),
  target_object_id agenteam_object.safe_id,
  target_upload_id agenteam_object.safe_id,
  target_attempt_id agenteam_object.safe_id,
  creator_kind text NOT NULL CHECK (creator_kind IN ('human','agent_run')),
  creator_id agenteam_object.safe_id NOT NULL,
  execution_id agenteam_object.safe_id,
  operation_id agenteam_object.safe_id,
  tool_id agenteam_object.safe_id,
  tool_call_id agenteam_object.safe_id,
  created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
  completed_at timestamptz(6),
  CHECK ((path='upload')=(kind='user_upload')),
  CHECK ((path='source')=(source IS NOT NULL)),
  CHECK (source_lease_id IS NULL OR source IS NOT NULL),
  CHECK (target_attempt_id IS NULL OR target_upload_id IS NOT NULL AND target_object_id IS NOT NULL),
  CHECK (state<>'completed' OR completed_at IS NOT NULL AND target_object_id IS NOT NULL),
  CHECK (creator_kind<>'agent_run' OR execution_id IS NOT NULL),
  CHECK (operation_id IS NULL OR execution_id IS NOT NULL),
  CHECK (tool_call_id IS NULL OR tool_id IS NOT NULL)
);

CREATE TABLE agenteam_artifact.artifacts (
  id agenteam_object.safe_id PRIMARY KEY,
  file_id agenteam_object.safe_id NOT NULL UNIQUE,
  project_id agenteam_object.safe_id NOT NULL,
  object_id agenteam_object.safe_id NOT NULL UNIQUE REFERENCES agenteam_object.objects(id),
  kind text NOT NULL CHECK (kind IN ('generated','user_upload')),
  name text NOT NULL CHECK (octet_length(name) BETWEEN 1 AND 255),
  description text NOT NULL CHECK (octet_length(description)<=4096),
  media_type text NOT NULL,
  byte_size bigint NOT NULL CHECK (byte_size BETWEEN 0 AND 1073741824),
  sha256 bytea NOT NULL CHECK (octet_length(sha256)=32),
  object_version bigint NOT NULL CHECK (object_version>0),
  object_created_at timestamptz(6) NOT NULL,
  version bigint NOT NULL DEFAULT 1 CHECK (version>0),
  creator_kind text NOT NULL CHECK (creator_kind IN ('human','agent_run')),
  creator_id agenteam_object.safe_id NOT NULL,
  creation_cause text NOT NULL,
  execution_id agenteam_object.safe_id,
  operation_id agenteam_object.safe_id,
  tool_id agenteam_object.safe_id,
  tool_call_id agenteam_object.safe_id,
  created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
  CHECK (creator_kind<>'agent_run' OR execution_id IS NOT NULL),
  CHECK (operation_id IS NULL OR execution_id IS NOT NULL),
  CHECK (tool_call_id IS NULL OR tool_id IS NOT NULL)
);
CREATE INDEX artifacts_project_page ON agenteam_artifact.artifacts(project_id,created_at DESC,id DESC);
CREATE INDEX artifacts_project_execution ON agenteam_artifact.artifacts(project_id,execution_id,created_at DESC,id DESC);

CREATE TABLE agenteam_artifact.cleanup (
  project_id agenteam_object.safe_id PRIMARY KEY,
  operation_id agenteam_object.safe_id NOT NULL,
  project_version bigint NOT NULL CHECK (project_version>0),
  state text NOT NULL CHECK (state IN ('pending','completed')),
  updated_at timestamptz(6) NOT NULL DEFAULT clock_timestamp()
);
-- This identity/operation/version/state row is the only Artifact-domain receipt
-- retained after permanent Project cleanup. Command/intent recovery rows are
-- physically removed in the same final Tx; their display/source/content facts
-- must not be replaced with placeholder data or copied to a history table.

CREATE SCHEMA agenteam_download;
CREATE TABLE agenteam_download.grants (
  id agenteam_object.safe_id PRIMARY KEY,
  project_id agenteam_object.safe_id,
  user_id agenteam_object.safe_id NOT NULL,
  object_id agenteam_object.safe_id NOT NULL,
  binding jsonb NOT NULL,
  binding_sha256 bytea NOT NULL CHECK (octet_length(binding_sha256)=32),
  expires_at timestamptz(6) NOT NULL,
  revoked boolean NOT NULL DEFAULT false,
  created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX download_grants_project ON agenteam_download.grants(project_id,id);
CREATE INDEX download_grants_expiry ON agenteam_download.grants(expires_at,id);

-- Pending terminal facts are an I/O checkpoint, not a confirmed Audit outcome.
-- A crash with only started leaves an unresolved attempt; it never implies 0
-- bytes or permits automatic replay of the payload.
CREATE TABLE agenteam_download.attempts (
  id agenteam_object.safe_id PRIMARY KEY,
  grant_id agenteam_object.safe_id NOT NULL REFERENCES agenteam_download.grants(id) ON DELETE CASCADE,
  session_id agenteam_object.safe_id NOT NULL,
  offset_bytes bigint NOT NULL CHECK (offset_bytes>=0),
  length_bytes bigint NOT NULL CHECK (length_bytes BETWEEN 0 AND 1073741824),
  phase text NOT NULL CHECK (phase IN ('started','sent','failed')),
  sent_bytes bigint CHECK (sent_bytes>=0 AND sent_bytes<=length_bytes),
  failure text CHECK (failure IN ('read_failed','write_failed','cancelled','integrity_failed')),
  pending_phase text CHECK (pending_phase IN ('sent','failed')),
  pending_bytes bigint CHECK (pending_bytes>=0 AND pending_bytes<=length_bytes),
  pending_failure text CHECK (pending_failure IN ('read_failed','write_failed','cancelled','integrity_failed')),
  started_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
  finished_at timestamptz(6),
  CHECK (phase='started' AND sent_bytes IS NULL AND failure IS NULL AND finished_at IS NULL OR phase='sent' AND sent_bytes=length_bytes AND failure IS NULL AND finished_at IS NOT NULL OR phase='failed' AND sent_bytes IS NOT NULL AND failure IS NOT NULL AND finished_at IS NOT NULL),
  CHECK (pending_phase IS NULL AND pending_bytes IS NULL AND pending_failure IS NULL OR pending_phase='sent' AND pending_bytes=length_bytes AND pending_failure IS NULL OR pending_phase='failed' AND pending_bytes IS NOT NULL AND pending_failure IS NOT NULL)
);
CREATE INDEX download_attempts_pending ON agenteam_download.attempts(grant_id,id) WHERE phase='started';
