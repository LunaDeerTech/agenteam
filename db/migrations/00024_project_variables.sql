-- agenteam:transaction tx
-- +goose Up
CREATE SCHEMA agenteam_projectvariable;
CREATE DOMAIN agenteam_projectvariable.safe_id AS uuid
 CONSTRAINT projectvariable_safe_id CHECK (VALUE::text ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$');

CREATE TABLE agenteam_projectvariable.variables (
 id agenteam_projectvariable.safe_id PRIMARY KEY,
 project_id agenteam_projectvariable.safe_id NOT NULL,
 type text NOT NULL CHECK(type='variable'),
 name text COLLATE "C" NOT NULL CHECK(octet_length(name) BETWEEN 1 AND 128 AND name ~ '^[A-Za-z_][A-Za-z0-9_]*$'
  AND upper(name COLLATE "C")<>'AGENTEAM' AND left(upper(name COLLATE "C"),9)<>'AGENTEAM_'),
 description text NOT NULL CHECK(octet_length(description)<=4096),
 value text NOT NULL CHECK(octet_length(value)<=32768),
 version bigint NOT NULL CHECK(version>=1),
 created_at timestamptz(6) NOT NULL,
 updated_at timestamptz(6) NOT NULL CHECK(updated_at>=created_at),
 deleted_at timestamptz(6),
 CONSTRAINT variables_project_id_key UNIQUE(project_id,id),
 CONSTRAINT variables_deleted_shape CHECK(deleted_at IS NULL OR (deleted_at=updated_at AND version>=2 AND value='' AND description=''))
);
CREATE UNIQUE INDEX variables_live_name ON agenteam_projectvariable.variables(project_id,name) WHERE deleted_at IS NULL;
CREATE INDEX variables_live_page ON agenteam_projectvariable.variables(project_id,name,id) WHERE deleted_at IS NULL;

-- Stable IDs never change identity or reappear after deletion.
-- +goose StatementBegin
CREATE FUNCTION agenteam_projectvariable.reject_variable_rewrite() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ROW(NEW.id,NEW.project_id,NEW.type,NEW.created_at) IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.type,OLD.created_at)
 OR OLD.deleted_at IS NOT NULL OR OLD.version=9223372036854775807 OR NEW.version<>OLD.version+1 OR NEW.updated_at<OLD.updated_at THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='variables_immutable', MESSAGE='immutable variable identity';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER variables_immutable BEFORE UPDATE ON agenteam_projectvariable.variables FOR EACH ROW EXECUTE FUNCTION agenteam_projectvariable.reject_variable_rewrite();

CREATE TABLE agenteam_projectvariable.project_generations (
 project_id agenteam_projectvariable.safe_id PRIMARY KEY,
 query_generation bigint NOT NULL CHECK(query_generation>=2)
);

CREATE TABLE agenteam_projectvariable.commands (
 id agenteam_projectvariable.safe_id PRIMARY KEY,
 project_id agenteam_projectvariable.safe_id NOT NULL,
 actor_user_id agenteam_projectvariable.safe_id NOT NULL,
 command_name text NOT NULL CHECK(command_name IN ('project.variable.create','project.variable.update','project.variable.delete')),
 idempotency_key text NOT NULL CHECK(octet_length(idempotency_key) BETWEEN 1 AND 128 AND idempotency_key ~ '^[A-Za-z0-9._:/-]+$'),
 semantic_digest text NOT NULL CHECK(semantic_digest ~ '^sha256:[0-9a-f]{64}$'),
 target_id agenteam_projectvariable.safe_id NOT NULL,
 request jsonb NOT NULL CHECK(jsonb_typeof(request)='object' AND octet_length(request::text)<=524288),
 state text NOT NULL CHECK(state IN ('planned','completed')),
 plan_revision bigint NOT NULL CHECK(plan_revision>=1),
 plan jsonb CHECK(plan IS NULL OR (jsonb_typeof(plan)='object' AND octet_length(plan::text)<=1048576)),
 event_id agenteam_projectvariable.safe_id UNIQUE,
 receipt jsonb CHECK(receipt IS NULL OR (jsonb_typeof(receipt)='object' AND octet_length(receipt::text)<=524288)),
 created_at timestamptz(6) NOT NULL,
 committed_at timestamptz(6) CHECK(committed_at IS NULL OR committed_at>=created_at),
 CONSTRAINT commands_project_id_key UNIQUE(project_id,id),
 CONSTRAINT commands_history_key UNIQUE(project_id,id,target_id,event_id),
 CONSTRAINT commands_identity_key UNIQUE(project_id,command_name,idempotency_key),
 CONSTRAINT commands_result_check CHECK((
  (state='planned' AND plan IS NOT NULL AND event_id IS NOT NULL AND receipt IS NULL AND committed_at IS NULL)
  OR (state='completed' AND receipt IS NOT NULL AND committed_at IS NOT NULL AND receipt->>'command'=command_name
   AND ((receipt->'changed'='true'::jsonb AND plan IS NOT NULL AND event_id IS NOT NULL
     AND receipt->>'event_id'=event_id::text AND jsonb_typeof(receipt->'audit_id')='string'
     AND receipt->>'audit_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$')
    OR (command_name='project.variable.update' AND receipt->'changed'='false'::jsonb AND plan IS NULL AND event_id IS NULL
     AND receipt->'event_id'='null'::jsonb AND receipt->'audit_id'='null'::jsonb)))) IS TRUE)
);

-- +goose StatementBegin
CREATE FUNCTION agenteam_projectvariable.valid_changed_fields(kind text, fields jsonb) RETURNS boolean
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
 SELECT CASE WHEN kind='created' THEN fields='["created"]'::jsonb
 WHEN kind='deleted' THEN fields='["deleted"]'::jsonb
 WHEN kind='updated' AND jsonb_typeof(fields)='array' THEN
  jsonb_array_length(fields) BETWEEN 1 AND 3
  AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(fields) v WHERE jsonb_typeof(v)<>'string' OR (v#>>'{}') NOT IN ('description','name','value'))
  AND fields=(SELECT jsonb_agg(v ORDER BY v COLLATE "C") FROM (SELECT DISTINCT v#>>'{}' AS v FROM jsonb_array_elements(fields) v) q)
 ELSE false END
$$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION agenteam_projectvariable.valid_version(v jsonb) RETURNS boolean
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
 SELECT CASE WHEN jsonb_typeof(v)='string' AND (v#>>'{}') ~ '^[1-9][0-9]{0,18}$'
 THEN (v#>>'{}')::numeric<=9223372036854775807 ELSE false END
$$;
-- +goose StatementEnd
CREATE TABLE agenteam_projectvariable.history (
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
 CONSTRAINT history_variable_version_key UNIQUE(project_id,variable_id,version),
 CONSTRAINT history_variable_fk FOREIGN KEY(project_id,variable_id) REFERENCES agenteam_projectvariable.variables(project_id,id) ON DELETE RESTRICT,
 CONSTRAINT history_command_fk FOREIGN KEY(project_id,operation_id,variable_id,event_id) REFERENCES agenteam_projectvariable.commands(project_id,id,target_id,event_id) ON DELETE RESTRICT,
 CONSTRAINT history_version_kind CHECK((kind='created' AND version=1) OR (kind<>'created' AND version>=2))
);

ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT audit_records_action_check;
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_action_check CHECK (action IN (
  'secret.create','secret.update','secret.delete','secret.resolve','secret.master.register','secret.master.rotation.start','secret.master.rotation.complete','secret.master.rotation.failed','outbound.policy.update','outbound.access.deny',
  'object.upload.complete','object.upload.failed','object.delete','object.transfer.issue','object.transfer.complete','object.transfer.revoke','artifact.create','artifact.list','artifact.read','artifact.download','outbox.delivery.requeue','account.bootstrap','account.login','account.logout','account.invite.create','account.invite.revoke','account.invite.redeem','account.password.change','account.password.reset.request','account.password.reset.complete','account.profile.update','account.avatar.update','account.settings.update','smtp.settings.update','smtp.test.request','smtp.delivery','smtp.delivery.retry','project.create.accepted','project.create.completed','project.update','project.archive.accepted','project.archive.completed','project.restore','project.delete.accepted','project.lifecycle.retry','provider.create','provider.update','provider.delete','model.create','model.update','model.delete','model.selection.update','project.variable.create','project.variable.update','project.variable.delete'));
ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT audit_records_resource_kind_check;
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_resource_kind_check CHECK (resource_kind IN ('secret','secret_master','secret_rotation','outbound_policy','agent','stored_object','object_transfer','artifact','artifact_collection','outbox_delivery','user','session','account_attempt','invitation','password_reset','account_settings','smtp_settings','mail_job','project','project_operation','project_creation','model_provider','model_config','model_selection','project_variable'));
ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT audit_records_producer_check;
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_producer_check CHECK (producer IN ('secret','secret.master','outbound.policy','outbound.access','object','artifact','outbox','account','account.mail','project','model','projectvariable'));
ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT audit_records_check2;
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_check2 CHECK (
 (resource_kind IN ('secret_master','outbound_policy') AND resource_id IS NULL)
 OR (resource_kind IN ('secret','secret_rotation','agent','stored_object','object_transfer','artifact','artifact_collection','outbox_delivery','user','session','account_attempt','invitation','password_reset','account_settings','smtp_settings','mail_job') AND resource_id IS NOT NULL)
 OR (resource_kind IN ('project','project_operation','project_creation') AND resource_id IS NOT NULL)
 OR (resource_kind IN ('model_provider','model_config','model_selection','project_variable') AND resource_id IS NOT NULL));

-- The original Project guard reserves every project.* action. Preserve its
-- entire predicate and admit only the three separate Variable action tuples.
-- The independent projectvariable_contract below still validates their facts.
-- +goose StatementBegin
DO $projectvariable_guard$
DECLARE original_guard text;
BEGIN
 SELECT pg_get_expr(conbin,conrelid) INTO STRICT original_guard
 FROM pg_constraint
 WHERE conrelid='agenteam_audit.audit_records'::regclass
   AND conname='audit_records_project_contract' AND contype='c';
 ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT audit_records_project_contract;
 EXECUTE 'ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_project_contract CHECK (('
  || original_guard || ') OR (producer=''projectvariable'' AND resource_kind=''project_variable'' AND action IN '
  || '(''project.variable.create'',''project.variable.update'',''project.variable.delete'')))';
END;
$projectvariable_guard$;
-- +goose StatementEnd

ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_projectvariable_contract CHECK((
 (producer<>'projectvariable' AND action NOT IN ('project.variable.create','project.variable.update','project.variable.delete') AND resource_kind<>'project_variable')
 OR (producer='projectvariable' AND scope='project' AND actor_kind='human' AND outcome='success' AND ordinal=0
  AND resource_kind='project_variable' AND resource_id IS NOT NULL AND cause_ref ~ '^sha256:[0-9a-f]{64}$'
  AND tool_id IS NULL AND execution_id IS NULL AND tool_call_id IS NULL AND operation_id IS NULL AND request_id IS NULL
  AND approval_id IS NULL AND runner_id IS NULL AND correlation_id IS NULL AND http_trace_id IS NULL
  AND metadata ?& ARRAY['variable_id','version','changed_fields'] AND metadata-ARRAY['variable_id','version','changed_fields']='{}'::jsonb
  AND jsonb_typeof(metadata->'variable_id')='string' AND metadata->>'variable_id'=resource_id::text
  AND agenteam_projectvariable.valid_version(metadata->'version')
  AND ((action='project.variable.create' AND metadata->>'version'='1' AND agenteam_projectvariable.valid_changed_fields('created',metadata->'changed_fields'))
   OR (action='project.variable.update' AND metadata->>'version'<>'1' AND agenteam_projectvariable.valid_changed_fields('updated',metadata->'changed_fields'))
   OR (action='project.variable.delete' AND metadata->>'version'<>'1' AND agenteam_projectvariable.valid_changed_fields('deleted',metadata->'changed_fields'))))
) IS TRUE);
