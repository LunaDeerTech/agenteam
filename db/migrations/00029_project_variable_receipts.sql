-- agenteam:transaction tx
-- +goose Up
-- Dedicated D04 storage only; D10 Owner tables belong to a later migration.
ALTER TABLE agenteam_secret.secret_payloads
  DROP CONSTRAINT secret_payloads_owner_kind_check;
ALTER TABLE agenteam_secret.secret_payloads
  ADD CONSTRAINT secret_payloads_owner_kind_check CHECK (owner_kind IN (1, 2, 3)),
  ADD CONSTRAINT secret_payloads_project_variable_receipt_scope
    CHECK (owner_kind <> 3 OR scope = 'project'),
  ADD CONSTRAINT secret_payloads_project_variable_receipt_digest
    CHECK (owner_kind <> 3 OR octet_length(ciphertext) = 48);
-- The existing kind2 48-byte CHECK and kind1 bounds remain unchanged.

ALTER TABLE agenteam_secret.secrets DROP CONSTRAINT secrets_purpose_check;
ALTER TABLE agenteam_secret.secrets
  ADD CONSTRAINT secrets_purpose_check
    CHECK (purpose IN ('model', 'mcp', 'runner', 'smtp', 'object_storage', 'system', 'project_variable')),
  ADD CONSTRAINT secrets_project_variable_scope
    CHECK (purpose <> 'project_variable' OR scope = 'project');
-- Do not extend legacy receipt purposes, reference consumers or lease consumers.

CREATE DOMAIN agenteam_secret.safe_id AS uuid
  CONSTRAINT secret_safe_id_check
  CHECK (VALUE::text ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$');

CREATE TABLE agenteam_secret.project_variable_receipts (
  id agenteam_secret.safe_id PRIMARY KEY,
  project_id agenteam_secret.safe_id NOT NULL,
  variable_id agenteam_secret.safe_id NOT NULL,
  user_id agenteam_secret.safe_id NOT NULL,
  command_digest text NOT NULL CHECK (command_digest ~ '^sha256:[0-9a-f]{64}$'),
  command_kind text NOT NULL CHECK (command_kind IN (
    'project.secret_variable.create',
    'project.secret_variable.update',
    'project.secret_variable.delete'
  )),
  external_expected_version bigint CHECK (external_expected_version > 0),
  credential_id agenteam_secret.safe_id NOT NULL,
  effect text NOT NULL CHECK (effect IN ('create', 'replace', 'delete', 'none')),
  result_version bigint NOT NULL CHECK (result_version > 0),
  deleted boolean NOT NULL,
  digest_payload_id agenteam_secret.safe_id NOT NULL UNIQUE
    REFERENCES agenteam_secret.secret_payloads(payload_id)
    DEFERRABLE INITIALLY DEFERRED,
  CONSTRAINT secret_project_variable_receipt_result CHECK (
    (command_kind = 'project.secret_variable.create'
      AND external_expected_version IS NULL
      AND effect = 'create' AND result_version = 1 AND NOT deleted)
    OR (command_kind = 'project.secret_variable.update'
      AND external_expected_version IS NOT NULL AND NOT deleted
      AND (effect = 'none' OR (effect = 'replace' AND result_version >= 2)))
    OR (command_kind = 'project.secret_variable.delete'
      AND external_expected_version IS NOT NULL
      AND effect = 'delete' AND result_version >= 2 AND deleted)
  ),
  UNIQUE (project_id, command_digest)
);
CREATE INDEX secret_project_variable_receipt_project
  ON agenteam_secret.project_variable_receipts(project_id, id);

-- No FK to current Credential or D10 canonical/receipt rows: historical
-- delete/replay, rewrap and Project cleanup retain this domain's ownership.
-- The native writer must check owner kind3 + exact receipt/payload/Project;
-- reverse rotation lookup uses the PK and compares digest_payload_id/Project.
-- external_expected_version is D10 Variable version, never Credential version.
-- No semantic digest, material, name or description is stored in this table.
