-- agenteam:transaction tx
-- +goose Up
-- D10 Owner storage. The WIP 26..29 prefix is a test dependency, not a main delivery.
ALTER TABLE agenteam_projectvariable.variables
  DROP CONSTRAINT variables_type_check,
  DROP CONSTRAINT variables_deleted_shape,
  ALTER COLUMN value DROP NOT NULL,
  ADD COLUMN credential_id agenteam_projectvariable.safe_id,
  ADD COLUMN credential_version bigint,
  ADD CONSTRAINT variables_type_check CHECK (type IN ('variable','secret')),
  ADD CONSTRAINT variables_payload_shape CHECK (
    (type='variable' AND value IS NOT NULL
      AND credential_id IS NULL AND credential_version IS NULL)
    OR (type='secret' AND value IS NULL AND (
      (deleted_at IS NULL AND credential_id IS NOT NULL AND credential_version IS NOT NULL AND credential_version>0)
      OR (deleted_at IS NOT NULL AND credential_id IS NULL AND credential_version IS NULL)
    ))
  ),
  ADD CONSTRAINT variables_deleted_shape CHECK (
    deleted_at IS NULL OR (deleted_at=updated_at AND version>=2 AND description=''
      AND ((type='variable' AND value='') OR (type='secret' AND value IS NULL)))
  );
-- Existing cross-type live name uniqueness and immutable ID/type trigger stay.
CREATE UNIQUE INDEX variables_secret_credential
  ON agenteam_projectvariable.variables(credential_id)
  WHERE type='secret' AND deleted_at IS NULL;
CREATE INDEX variables_secret_page
  ON agenteam_projectvariable.variables(project_id,name,id)
  WHERE type='secret' AND deleted_at IS NULL;

CREATE TABLE agenteam_projectvariable.secret_project_generations (
  project_id agenteam_projectvariable.safe_id PRIMARY KEY,
  query_generation bigint NOT NULL CHECK(query_generation>=2)
);

CREATE TABLE agenteam_projectvariable.secret_commands (
  id agenteam_projectvariable.safe_id PRIMARY KEY,
  project_id agenteam_projectvariable.safe_id NOT NULL,
  actor_user_id agenteam_projectvariable.safe_id NOT NULL,
  command_name text NOT NULL CHECK(command_name IN (
    'project.secret_variable.create','project.secret_variable.update','project.secret_variable.delete')),
  idempotency_key text NOT NULL CHECK(octet_length(idempotency_key) BETWEEN 1 AND 128
    AND idempotency_key ~ '^[A-Za-z0-9._:/-]+$'),
  target_id agenteam_projectvariable.safe_id NOT NULL,
  expected_version bigint CHECK(expected_version>0),
  name_present boolean NOT NULL,
  description_present boolean NOT NULL,
  value_present boolean NOT NULL,
  d04_receipt_id agenteam_projectvariable.safe_id NOT NULL UNIQUE,
  credential_id agenteam_projectvariable.safe_id NOT NULL,
  credential_version bigint NOT NULL CHECK(credential_version>0),
  secret_effect text NOT NULL CHECK(secret_effect IN ('create','replace','delete','none')),
  secret_deleted boolean NOT NULL,
  event_id agenteam_projectvariable.safe_id UNIQUE,
  audit_id agenteam_projectvariable.safe_id UNIQUE,
  receipt jsonb NOT NULL CHECK(jsonb_typeof(receipt)='object' AND octet_length(receipt::text)<=1048576),
  committed_at timestamptz(6) NOT NULL,
  CONSTRAINT secret_commands_identity UNIQUE(project_id,command_name,idempotency_key),
  CONSTRAINT secret_commands_history_identity UNIQUE(project_id,id,target_id,event_id),
  CONSTRAINT secret_commands_intent_shape CHECK (
    (command_name='project.secret_variable.create' AND expected_version IS NULL
      AND name_present AND description_present AND value_present
      AND secret_effect='create' AND credential_version=1 AND NOT secret_deleted)
    OR (command_name='project.secret_variable.update' AND expected_version IS NOT NULL
      AND (name_present OR description_present OR value_present) AND NOT secret_deleted
      AND ((value_present AND secret_effect='replace' AND credential_version>=2)
        OR (NOT value_present AND secret_effect='none')))
    OR (command_name='project.secret_variable.delete' AND expected_version IS NOT NULL
      AND NOT name_present AND NOT description_present AND NOT value_present
      AND secret_effect='delete' AND credential_version>=2 AND secret_deleted)
  ),
  CONSTRAINT secret_commands_result_shape CHECK ((
    receipt->>'command'=command_name AND (
      (receipt->'changed'='true'::jsonb AND event_id IS NOT NULL AND audit_id IS NOT NULL
        AND receipt->>'event_id'=event_id::text AND receipt->>'audit_id'=audit_id::text)
      OR (command_name='project.secret_variable.update' AND NOT value_present
        AND receipt->'changed'='false'::jsonb AND event_id IS NULL AND audit_id IS NULL
        AND receipt->'event_id'='null'::jsonb AND receipt->'audit_id'='null'::jsonb)
    )
  ) IS TRUE)
);
-- No material, plaintext value digest, request JSON or durable planned state.
-- D04 is an independent owner: no FK to its current Credential or receipts.

CREATE TABLE agenteam_projectvariable.secret_history (
  id agenteam_projectvariable.safe_id PRIMARY KEY,
  project_id agenteam_projectvariable.safe_id NOT NULL,
  variable_id agenteam_projectvariable.safe_id NOT NULL,
  operation_id agenteam_projectvariable.safe_id NOT NULL UNIQUE,
  version bigint NOT NULL CHECK(version>=1),
  kind text NOT NULL CHECK(kind IN ('created','updated','deleted')),
  changed_fields jsonb NOT NULL CHECK(agenteam_projectvariable.valid_changed_fields(kind,changed_fields) IS TRUE),
  actor_user_id agenteam_projectvariable.safe_id NOT NULL,
  occurred_at timestamptz(6) NOT NULL,
  event_id agenteam_projectvariable.safe_id NOT NULL UNIQUE,
  CONSTRAINT secret_history_version UNIQUE(project_id,variable_id,version),
  CONSTRAINT secret_history_variable FOREIGN KEY(project_id,variable_id)
    REFERENCES agenteam_projectvariable.variables(project_id,id) ON DELETE RESTRICT,
  CONSTRAINT secret_history_command FOREIGN KEY(project_id,operation_id,variable_id,event_id)
    REFERENCES agenteam_projectvariable.secret_commands(project_id,id,target_id,event_id)
    DEFERRABLE INITIALLY DEFERRED,
  CONSTRAINT secret_history_version_kind CHECK(
    (kind='created' AND version=1) OR (kind<>'created' AND version>=2))
);
-- The real Audit checker reads history before Audit allocates its ID. The
-- completed-only command is inserted afterward in the SAME transaction.

CREATE TABLE agenteam_projectvariable.secret_references (
  project_id agenteam_projectvariable.safe_id NOT NULL,
  variable_id agenteam_projectvariable.safe_id NOT NULL,
  owner_kind text NOT NULL CHECK(owner_kind='agent'),
  owner_id agenteam_projectvariable.safe_id NOT NULL,
  owner_version bigint NOT NULL CHECK(owner_version>0),
  PRIMARY KEY(project_id,variable_id,owner_kind,owner_id),
  FOREIGN KEY(project_id,variable_id)
    REFERENCES agenteam_projectvariable.variables(project_id,id) ON DELETE RESTRICT
);
CREATE INDEX secret_references_owner
  ON agenteam_projectvariable.secret_references(project_id,owner_kind,owner_id,variable_id);
-- Owner deletion checks these true owned facts. No F1 success provider here.

-- Retain every existing action/Project/ordinary Variable predicate, admitting
-- only the separate Secret tuple before applying its precise metadata guard.
-- +goose StatementBegin
DO $secret_variable_audit_guards$
DECLARE guard_name text; original_guard text;
BEGIN
  FOREACH guard_name IN ARRAY ARRAY['audit_records_action_check',
    'audit_records_project_contract','audit_records_projectvariable_contract'] LOOP
    SELECT pg_get_expr(conbin,conrelid) INTO STRICT original_guard
      FROM pg_constraint WHERE conrelid='agenteam_audit.audit_records'::regclass
        AND conname=guard_name AND contype='c';
    EXECUTE format('ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT %I',guard_name);
    EXECUTE format('ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT %I CHECK ((%s) OR '
      || '(producer=''projectvariable'' AND scope=''project'' AND resource_kind=''project_variable'' '
      || 'AND action IN (''project.secret_variable.create'',''project.secret_variable.update'',''project.secret_variable.delete'')))',
      guard_name,original_guard);
  END LOOP;
END;
$secret_variable_audit_guards$;
-- +goose StatementEnd
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_projectsecretvariable_contract CHECK ((
  action NOT IN ('project.secret_variable.create','project.secret_variable.update','project.secret_variable.delete')
  OR (producer='projectvariable' AND scope='project' AND actor_kind='human' AND outcome='success' AND ordinal=0
    AND resource_kind='project_variable' AND resource_id IS NOT NULL AND cause_ref ~ '^sha256:[0-9a-f]{64}$'
    AND tool_id IS NULL AND execution_id IS NULL AND tool_call_id IS NULL AND operation_id IS NULL AND request_id IS NULL
    AND approval_id IS NULL AND runner_id IS NULL AND correlation_id IS NULL AND http_trace_id IS NULL
    AND metadata ?& ARRAY['variable_id','version','changed_fields']
    AND metadata-ARRAY['variable_id','version','changed_fields']='{}'::jsonb
    AND jsonb_typeof(metadata->'variable_id')='string' AND metadata->>'variable_id'=resource_id::text
    AND agenteam_projectvariable.valid_version(metadata->'version')
    AND ((action='project.secret_variable.create' AND metadata->>'version'='1'
        AND agenteam_projectvariable.valid_changed_fields('created',metadata->'changed_fields'))
      OR (action='project.secret_variable.update' AND metadata->>'version'<>'1'
        AND agenteam_projectvariable.valid_changed_fields('updated',metadata->'changed_fields'))
      OR (action='project.secret_variable.delete' AND metadata->>'version'<>'1'
        AND agenteam_projectvariable.valid_changed_fields('deleted',metadata->'changed_fields'))))
) IS TRUE);
