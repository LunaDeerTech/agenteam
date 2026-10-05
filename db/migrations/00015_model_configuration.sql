-- agenteam:transaction tx
-- +goose Up
-- D09 System configuration follows the accepted continuous prefix through 00014.
-- Existing Audit branches are retained from 00013; 00014 does not alter Audit.
CREATE SCHEMA agenteam_model;
CREATE DOMAIN agenteam_model.safe_id AS uuid
 CHECK (VALUE::text ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$');

CREATE TABLE agenteam_model.providers (
 id agenteam_model.safe_id PRIMARY KEY,
 scope text NOT NULL CHECK(scope IN ('system','project')),
 project_id agenteam_model.safe_id,
 name text NOT NULL CHECK(char_length(name) BETWEEN 1 AND 128),
 protocol text NOT NULL CHECK(protocol IN ('openai-chat-completions','anthropic-messages','openai-embeddings','jina-rerank','openai-images-generations')),
 capability_type text GENERATED ALWAYS AS (CASE protocol
  WHEN 'openai-chat-completions' THEN 'chat' WHEN 'anthropic-messages' THEN 'chat'
  WHEN 'openai-embeddings' THEN 'embedding' WHEN 'jina-rerank' THEN 'reranker'
  WHEN 'openai-images-generations' THEN 'image_generation' END) STORED,
 base_url text NOT NULL CHECK(octet_length(base_url) BETWEEN 1 AND 8192),
 credential_id agenteam_model.safe_id,
 provider_options jsonb NOT NULL CHECK(jsonb_typeof(provider_options)='object' AND octet_length(provider_options::text)<=65536),
 enabled boolean NOT NULL,
 version bigint NOT NULL CHECK(version>0),
 created_at timestamptz(6) NOT NULL,updated_at timestamptz(6) NOT NULL CHECK(updated_at>=created_at),
 UNIQUE(id,capability_type),
 CHECK((scope='system' AND project_id IS NULL) OR (scope='project' AND project_id IS NOT NULL)),
 CHECK(scope<>'project' OR protocol IN ('openai-chat-completions','anthropic-messages'))
);
-- Secret metadata/ref has no cross-domain FK; only the formal same-Tx reference API protects it.
-- Name is display data, not globally unique identity; protocol/scope are immutable in commands.
CREATE INDEX model_providers_page ON agenteam_model.providers(scope,created_at DESC,id DESC);

CREATE TABLE agenteam_model.models (
 id agenteam_model.safe_id PRIMARY KEY,
 provider_id agenteam_model.safe_id NOT NULL,
 type text NOT NULL CHECK(type IN ('chat','embedding','reranker','image_generation')),
 name text NOT NULL CHECK(char_length(name) BETWEEN 1 AND 128),
 provider_model_id text NOT NULL CHECK(octet_length(provider_model_id) BETWEEN 1 AND 256),
 parameters jsonb NOT NULL CHECK(jsonb_typeof(parameters)='object' AND octet_length(parameters::text)<=65536),
 request_overwrite jsonb NOT NULL CHECK(jsonb_typeof(request_overwrite)='object'),
 header_overwrite jsonb NOT NULL CHECK(jsonb_typeof(header_overwrite)='object' AND octet_length(header_overwrite::text)<=16384),
 capabilities jsonb NOT NULL CHECK(jsonb_typeof(capabilities)='object'),
 enabled boolean NOT NULL,version bigint NOT NULL CHECK(version>0),
 created_at timestamptz(6) NOT NULL,updated_at timestamptz(6) NOT NULL CHECK(updated_at>=created_at),
 FOREIGN KEY(provider_id,type) REFERENCES agenteam_model.providers(id,capability_type) ON DELETE RESTRICT,
 CHECK(octet_length(request_overwrite::text)+octet_length(header_overwrite::text)<=65536)
);
-- Scope derives from immutable provider_id; model type/provider_id are immutable in commands.
-- JSONB shape/size defenses do not replace strict duplicate-key/profile validation before SQL.
CREATE INDEX model_models_provider_page ON agenteam_model.models(provider_id,created_at DESC,id DESC);

CREATE TABLE agenteam_model.platform_selection (
 id agenteam_model.safe_id PRIMARY KEY,
 singleton boolean NOT NULL DEFAULT true UNIQUE CHECK(singleton),
 version bigint NOT NULL CHECK(version>0),
 configured boolean NOT NULL,
 embedding_id agenteam_model.safe_id REFERENCES agenteam_model.models(id) ON DELETE RESTRICT,
 memory_id agenteam_model.safe_id REFERENCES agenteam_model.models(id) ON DELETE RESTRICT,
 reranker_id agenteam_model.safe_id REFERENCES agenteam_model.models(id) ON DELETE RESTRICT,
 image_id agenteam_model.safe_id REFERENCES agenteam_model.models(id) ON DELETE RESTRICT,
 updated_at timestamptz(6) NOT NULL,
 CHECK((NOT configured AND embedding_id IS NULL AND memory_id IS NULL AND reranker_id IS NULL AND image_id IS NULL)
   OR (configured AND embedding_id IS NOT NULL AND memory_id IS NOT NULL))
);
-- Initialize creates only this technical ID/version=1/configured=false row, never model defaults.
-- Enabled/System/type/structured-output requirements are rechecked under the same writer locks.

CREATE TABLE agenteam_model.commands (
 id agenteam_model.safe_id PRIMARY KEY,
 scope text NOT NULL CHECK(scope IN ('system','project')),project_id agenteam_model.safe_id,
 scope_key text GENERATED ALWAYS AS(coalesce(project_id::text,'system')) STORED,
 user_id agenteam_model.safe_id NOT NULL,
 command_name text NOT NULL CHECK(command_name IN ('provider.create','provider.update','provider.delete','model.create','model.update','model.delete','model.selection.update')),
 command_identity text NOT NULL UNIQUE,
 key_digest text NOT NULL CHECK(key_digest ~ '^sha256:[0-9a-f]{64}$'),
 semantic_digest text NOT NULL CHECK(semantic_digest ~ '^sha256:[0-9a-f]{64}$'),
 resource_id agenteam_model.safe_id NOT NULL,
 phase text NOT NULL CHECK(phase IN ('prepared','committed')),
 mutation_plan jsonb NOT NULL CHECK(jsonb_typeof(mutation_plan)='object' AND octet_length(mutation_plan::text)<=262144),
 safe_receipt jsonb CHECK(safe_receipt IS NULL OR (jsonb_typeof(safe_receipt)='object' AND octet_length(safe_receipt::text)<=4096)),
 event_id agenteam_model.safe_id NOT NULL UNIQUE,
 event_header jsonb NOT NULL CHECK(jsonb_typeof(event_header)='object'),
 event_payload jsonb NOT NULL CHECK(jsonb_typeof(event_payload)='object'),
 -- A selector change may also append embedding-selection-changed: both event IDs/bodies
 -- must be preassigned and persisted in the same typed plan, never regenerated on replay.
 created_at timestamptz(6) NOT NULL,
 CHECK((scope='system' AND project_id IS NULL) OR (scope='project' AND project_id IS NOT NULL)),
 CHECK((phase='prepared' AND safe_receipt IS NULL) OR (phase='committed' AND safe_receipt IS NOT NULL)),
 UNIQUE(scope,scope_key,user_id,command_name,key_digest)
);
-- prepared exists inside the business Tx only. No API is allowed to commit that phase.
-- mutation_plan stores exact before/after reference intent and current safe config facts, no credential material.

CREATE TABLE agenteam_model.references (
 owner_kind text NOT NULL CHECK(owner_kind IN ('platform_selector','agent','project_summary')),
 owner_id agenteam_model.safe_id NOT NULL,role text NOT NULL,
 project_id agenteam_model.safe_id,
 model_id agenteam_model.safe_id NOT NULL REFERENCES agenteam_model.models(id) ON DELETE RESTRICT,
 owner_version bigint NOT NULL CHECK(owner_version>0),reasoning_effort text NOT NULL DEFAULT '',
 PRIMARY KEY(owner_kind,owner_id,role),
 CHECK((owner_kind='platform_selector' AND project_id IS NULL AND role IN ('embedding','memory','reranker','image'))
  OR (owner_kind='agent' AND project_id IS NOT NULL AND role IN ('agent_model','approval_model'))
  OR (owner_kind='project_summary' AND project_id IS NOT NULL AND owner_id=project_id AND role='meeting_summary'))
);
CREATE INDEX model_reference_reverse ON agenteam_model.references(model_id,owner_kind,owner_id,role);
-- Only platform_selector is bound in B01-K. Actual foreign owner references block deletion when their adapter is unbound.


ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT audit_records_action_check;
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_action_check CHECK (action IN (
  'secret.create','secret.update','secret.delete','secret.resolve','secret.master.register','secret.master.rotation.start','secret.master.rotation.complete','secret.master.rotation.failed','outbound.policy.update','outbound.access.deny',
  'object.upload.complete','object.upload.failed','object.delete','object.transfer.issue','object.transfer.complete','object.transfer.revoke','artifact.create','artifact.list','artifact.read','artifact.download','outbox.delivery.requeue','account.bootstrap','account.login','account.logout','account.invite.create','account.invite.revoke','account.invite.redeem','account.password.change','account.password.reset.request','account.password.reset.complete','account.profile.update','account.avatar.update','account.settings.update','smtp.settings.update','smtp.test.request','smtp.delivery','smtp.delivery.retry','project.create.accepted','project.create.completed','project.update','project.archive.accepted','project.archive.completed','project.restore','project.delete.accepted','project.lifecycle.retry','provider.create','provider.update','provider.delete','model.create','model.update','model.delete','model.selection.update'));
ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT audit_records_resource_kind_check;
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_resource_kind_check CHECK (resource_kind IN ('secret','secret_master','secret_rotation','outbound_policy','agent','stored_object','object_transfer','artifact','artifact_collection','outbox_delivery','user','session','account_attempt','invitation','password_reset','account_settings','smtp_settings','mail_job','project','project_operation','project_creation','model_provider','model_config','model_selection'));
ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT audit_records_producer_check;
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_producer_check CHECK (producer IN ('secret','secret.master','outbound.policy','outbound.access','object','artifact','outbox','account','account.mail','project','model'));
ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT audit_records_check2;
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_check2 CHECK (
 (resource_kind IN ('secret_master','outbound_policy') AND resource_id IS NULL)
 OR (resource_kind IN ('secret','secret_rotation','agent','stored_object','object_transfer','artifact','artifact_collection','outbox_delivery','user','session','account_attempt','invitation','password_reset','account_settings','smtp_settings','mail_job') AND resource_id IS NOT NULL)
 OR (resource_kind IN ('project','project_operation','project_creation') AND resource_id IS NOT NULL)
 OR (resource_kind IN ('model_provider','model_config','model_selection') AND resource_id IS NOT NULL));

-- New Model metadata cannot re-label an old producer/action/resource.
-- The explicit IS TRUE prevents SQL NULL from escaping a closed branch.
-- +goose StatementBegin
CREATE FUNCTION agenteam_model.audit_count(v jsonb, positive boolean) RETURNS boolean
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $fn$
 SELECT CASE WHEN jsonb_typeof(v)='string' AND (v#>>'{}') ~ '^(0|[1-9][0-9]{0,18})$'
 THEN (v#>>'{}')::numeric <= 9223372036854775807 AND (NOT positive OR (v#>>'{}')::numeric>0)
 ELSE false END
$fn$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION agenteam_model.audit_fields(v jsonb, allowed text[]) RETURNS boolean
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $fn$
 SELECT CASE WHEN jsonb_typeof(v)='array' THEN
 jsonb_array_length(v) BETWEEN 1 AND 14 AND
 NOT EXISTS(SELECT 1 FROM jsonb_array_elements(v) x WHERE jsonb_typeof(x)<>'string' OR NOT ((x#>>'{}')=ANY(allowed))) AND
 (SELECT count(DISTINCT x) FROM jsonb_array_elements(v) x)=jsonb_array_length(v)
 ELSE false END
$fn$;
-- +goose StatementEnd
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_model_contract CHECK ((
 (producer<>'model' AND action NOT IN ('provider.create','provider.update','provider.delete','model.create','model.update','model.delete','model.selection.update') AND resource_kind NOT IN ('model_provider','model_config','model_selection'))
 OR (
  producer='model' AND actor_kind='human' AND outcome='success' AND ordinal=0
  AND resource_id::text ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
  AND agenteam_model.audit_count(metadata->'version',true)
  AND tool_id IS NULL AND execution_id IS NULL AND tool_call_id IS NULL AND operation_id IS NULL AND request_id IS NULL AND approval_id IS NULL AND runner_id IS NULL
  AND (
   (action IN ('provider.create','provider.update','provider.delete') AND resource_kind='model_provider'
    AND metadata ?& ARRAY['provider_id','version','changed_fields'] AND metadata-ARRAY['provider_id','version','changed_fields']='{}'::jsonb
    AND jsonb_typeof(metadata->'provider_id')='string' AND metadata->>'provider_id'=resource_id::text
    AND ((action='provider.create' AND metadata->'changed_fields'='["created"]'::jsonb)
      OR (action='provider.delete' AND metadata->'changed_fields'='["deleted"]'::jsonb)
      OR (action='provider.update' AND agenteam_model.audit_fields(metadata->'changed_fields',ARRAY['name','enabled','base_url','credential_ref','provider_options']))))
   OR (action IN ('model.create','model.update','model.delete') AND resource_kind='model_config'
    AND metadata ?& ARRAY['provider_id','model_id','version','changed_fields']
    AND jsonb_typeof(metadata->'provider_id')='string'
    AND metadata->>'provider_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
    AND jsonb_typeof(metadata->'model_id')='string' AND metadata->>'model_id'=resource_id::text
    AND ((action='model.create' AND metadata-ARRAY['provider_id','model_id','version','changed_fields']='{}'::jsonb AND metadata->'changed_fields'='["created"]'::jsonb)
     OR (action='model.update' AND metadata-ARRAY['provider_id','model_id','version','changed_fields']='{}'::jsonb AND agenteam_model.audit_fields(metadata->'changed_fields',ARRAY['name','enabled','model_id','parameters','request_overwrite','header_overwrite','capabilities']))
     OR (action='model.delete' AND metadata ? 'affected_count' AND agenteam_model.audit_count(metadata->'affected_count',false)
      AND metadata-ARRAY['provider_id','model_id','version','changed_fields','replacement_id','affected_count']='{}'::jsonb
      AND ((NOT (metadata ? 'replacement_id') AND metadata->'changed_fields'='["deleted"]'::jsonb)
       OR (jsonb_typeof(metadata->'replacement_id')='string' AND metadata->>'replacement_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' AND metadata->>'replacement_id'<>resource_id::text AND metadata->'changed_fields'='["deleted","replacement"]'::jsonb)))))
   OR (action='model.selection.update' AND resource_kind='model_selection' AND scope='system'
    AND metadata ?& ARRAY['selection_id','version','changed_fields','selector_kind']
    AND metadata-ARRAY['selection_id','version','changed_fields','selector_kind']='{}'::jsonb
    AND jsonb_typeof(metadata->'selection_id')='string' AND metadata->>'selection_id'=resource_id::text
    AND metadata->>'selector_kind'='platform' AND metadata->'changed_fields'='["selection"]'::jsonb)
  )
 )
) IS TRUE);
