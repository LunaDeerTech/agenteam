-- agenteam:transaction tx
-- +goose Up
CREATE SCHEMA agenteam_secret;
CREATE TABLE agenteam_secret.secret_master_registry (
  version bigint PRIMARY KEY CHECK(version>0),
  fingerprint bytea NOT NULL UNIQUE CHECK(octet_length(fingerprint)=32),
  registration_id uuid NOT NULL UNIQUE,
  nonce_high_water bigint NOT NULL DEFAULT 0 CHECK(nonce_high_water BETWEEN 0 AND 4294967295),
  canary_nonce bytea,
  canary_ciphertext bytea,
  retireable boolean NOT NULL DEFAULT false,
  CHECK((canary_nonce IS NULL AND canary_ciphertext IS NULL) OR (canary_nonce IS NOT NULL AND octet_length(canary_nonce)=12 AND canary_ciphertext IS NOT NULL AND octet_length(canary_ciphertext)=51))
);
CREATE TABLE agenteam_secret.secret_control (
  singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
  current_write_version bigint NOT NULL REFERENCES agenteam_secret.secret_master_registry(version),
  write_epoch bigint NOT NULL CHECK(write_epoch>0)
);
CREATE TABLE agenteam_secret.secret_payloads (
  payload_id uuid PRIMARY KEY,
  scope text NOT NULL CHECK(scope IN ('system','project')),
  project_id uuid,
  owner_kind smallint NOT NULL CHECK(owner_kind IN (1,2)),
  owner_id uuid NOT NULL,
  format integer NOT NULL CHECK(format=1),
  algorithm text NOT NULL CHECK(algorithm='AES-256-GCM'),
  data_nonce bytea NOT NULL CHECK(octet_length(data_nonce)=12),
  ciphertext bytea NOT NULL CHECK(octet_length(ciphertext) BETWEEN 17 AND 65552),
  wrap_nonce bytea NOT NULL CHECK(octet_length(wrap_nonce)=12),
  wrapped_dek bytea NOT NULL CHECK(octet_length(wrapped_dek)=48),
  master_version bigint NOT NULL REFERENCES agenteam_secret.secret_master_registry(version),
  wrap_revision bigint NOT NULL CHECK(wrap_revision>0),
  CHECK((scope='system' AND project_id IS NULL) OR (scope='project' AND project_id IS NOT NULL)),
  CHECK(owner_kind<>2 OR octet_length(ciphertext)=48)
);
CREATE INDEX secret_payload_rotation ON agenteam_secret.secret_payloads(master_version,payload_id);
CREATE INDEX secret_payload_project ON agenteam_secret.secret_payloads(project_id,payload_id) WHERE scope='project';
CREATE TABLE agenteam_secret.secrets (
  id uuid PRIMARY KEY,
  scope text NOT NULL CHECK(scope IN ('system','project')),
  project_id uuid,
  scope_key text GENERATED ALWAYS AS(coalesce(project_id::text,'system')) STORED,
  purpose text NOT NULL CHECK(purpose IN ('model','mcp','runner','smtp','object_storage','system')),
  version bigint NOT NULL CHECK(version>0),
  current_payload_id uuid NOT NULL UNIQUE REFERENCES agenteam_secret.secret_payloads(payload_id) DEFERRABLE INITIALLY DEFERRED,
  CHECK((scope='system' AND project_id IS NULL) OR (scope='project' AND project_id IS NOT NULL)),
  UNIQUE(id,scope,scope_key)
);
CREATE INDEX secret_project ON agenteam_secret.secrets(project_id,id) WHERE scope='project';
CREATE TABLE agenteam_secret.secret_references (
  credential_id uuid NOT NULL,
  scope text NOT NULL CHECK(scope IN ('system','project')),
  project_id uuid,
  scope_key text GENERATED ALWAYS AS(coalesce(project_id::text,'system')) STORED,
  consumer text NOT NULL CHECK(consumer IN ('model','mcp','runner','smtp','object_storage','system')),
  owner_id uuid NOT NULL,
  PRIMARY KEY(credential_id,consumer,owner_id),
  FOREIGN KEY(credential_id,scope,scope_key) REFERENCES agenteam_secret.secrets(id,scope,scope_key),
  CHECK((scope='system' AND project_id IS NULL) OR (scope='project' AND project_id IS NOT NULL))
);
CREATE TABLE agenteam_secret.secret_leases (
  id uuid PRIMARY KEY,
  credential_id uuid NOT NULL,
  scope text NOT NULL CHECK(scope IN ('system','project')),
  project_id uuid,
  scope_key text GENERATED ALWAYS AS(coalesce(project_id::text,'system')) STORED,
  owner_kind text NOT NULL CHECK(owner_kind IN ('execution','model_call')),
  owner_id uuid NOT NULL,
  consumer text NOT NULL CHECK(consumer IN ('model','mcp','runner','smtp','object_storage','system')),
  released boolean NOT NULL DEFAULT false,
  UNIQUE(credential_id,owner_kind,owner_id),
  FOREIGN KEY(credential_id,scope,scope_key) REFERENCES agenteam_secret.secrets(id,scope,scope_key),
  CHECK((scope='system' AND project_id IS NULL) OR (scope='project' AND project_id IS NOT NULL))
);
CREATE INDEX secret_lease_project ON agenteam_secret.secret_leases(project_id,id) WHERE scope='project';
CREATE TABLE agenteam_secret.secret_command_receipts (
  id uuid PRIMARY KEY,
  scope text NOT NULL CHECK(scope IN ('system','project')),
  project_id uuid,
  scope_key text GENERATED ALWAYS AS(coalesce(project_id::text,'system')) STORED,
  command_digest text NOT NULL CHECK(command_digest ~ '^sha256:[0-9a-f]{64}$'),
  mutation_kind text NOT NULL CHECK(mutation_kind IN ('create','update','delete')),
  credential_id uuid NOT NULL,
  purpose text NOT NULL CHECK(purpose IN ('model','mcp','runner','smtp','object_storage','system')),
  result_version bigint NOT NULL CHECK(result_version>0),
  deleted boolean NOT NULL,
  digest_payload_id uuid NOT NULL UNIQUE REFERENCES agenteam_secret.secret_payloads(payload_id) DEFERRABLE INITIALLY DEFERRED,
  CHECK((scope='system' AND project_id IS NULL) OR (scope='project' AND project_id IS NOT NULL)),
  UNIQUE(scope,scope_key,command_digest)
);
CREATE INDEX secret_receipt_project ON agenteam_secret.secret_command_receipts(project_id,id) WHERE scope='project';
CREATE TABLE agenteam_secret.secret_rotation_runs (
  run_id uuid PRIMARY KEY,
  target_version bigint NOT NULL UNIQUE REFERENCES agenteam_secret.secret_master_registry(version),
  state text NOT NULL CHECK(state IN ('running','failed','completed')),
  last_payload_id uuid,
  processed bigint NOT NULL DEFAULT 0 CHECK(processed>=0),
  failure_count bigint NOT NULL DEFAULT 0 CHECK(failure_count>=0),
  safe_error text CHECK(safe_error IN ('SECRET_DECRYPT_FAILED','SECRET_KEY_UNAVAILABLE','SECRET_UNAVAILABLE','SECRET_ROTATION_FAILED','SECRET_WRITE_EPOCH_CHANGED')),
  updated_at timestamptz(6) NOT NULL DEFAULT clock_timestamp()
);
