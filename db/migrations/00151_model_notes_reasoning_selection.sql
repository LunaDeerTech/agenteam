-- agenteam:transaction tx
-- +goose Up
-- Model notes are optional display-only text. Provider names remain unchanged.
ALTER TABLE agenteam_model.models RENAME COLUMN name TO notes;
ALTER TABLE agenteam_model.models DROP CONSTRAINT models_name_check;
ALTER TABLE agenteam_model.models ADD CONSTRAINT models_notes_check
 CHECK(char_length(notes)<=2000 AND octet_length(notes)<=8000);
ALTER TABLE agenteam_model.platform_selection ADD COLUMN summary_reasoning jsonb NOT NULL DEFAULT '{"mode":"default"}';
ALTER TABLE agenteam_model.platform_selection ADD CONSTRAINT platform_reasoning_choice CHECK ((
 jsonb_typeof(summary_reasoning)='object' AND (
  (summary_reasoning->>'mode' IN ('default','on','off') AND summary_reasoning-ARRAY['mode']='{}'::jsonb)
  OR (summary_reasoning->>'mode'='effort' AND summary_reasoning-ARRAY['mode','value']='{}'::jsonb
   AND jsonb_typeof(summary_reasoning->'value')='string' AND summary_reasoning->>'value' ~ '^[A-Za-z0-9_.:-]{1,32}$'))
 AND (configured OR summary_reasoning='{"mode":"default"}'::jsonb)
) IS TRUE);
ALTER TABLE agenteam_model.references ADD COLUMN reasoning_mode text NOT NULL DEFAULT '';
UPDATE agenteam_model.references SET reasoning_mode='default' WHERE owner_kind='platform_selector';
ALTER TABLE agenteam_model.references DROP CONSTRAINT references_check;
ALTER TABLE agenteam_model.references ADD CONSTRAINT references_check CHECK ((
 (owner_kind='platform_selector' AND project_id IS NULL AND role IN ('embedding','summary','reranker','image')
  AND ((reasoning_mode='default' AND reasoning_effort='')
    OR (role='summary' AND reasoning_mode IN ('on','off') AND reasoning_effort='')
    OR (role='summary' AND reasoning_mode='effort' AND reasoning_effort ~ '^[A-Za-z0-9_.:-]{1,32}$')))
 OR (owner_kind='agent' AND project_id IS NOT NULL AND role IN ('agent_model','approval_model') AND reasoning_mode='')
 OR (owner_kind='project_summary' AND project_id IS NOT NULL AND owner_id=project_id AND role='meeting_summary' AND reasoning_mode='')
) IS TRUE);
-- ModelName is a legacy identity key; newly captured values are the canonical
-- provider model ID, never notes. All other historical identity checks remain.
-- +goose StatementBegin
DO $$
DECLARE c record;
BEGIN
 FOR c IN SELECT conname FROM pg_constraint WHERE conrelid='agenteam_model.invocations'::regclass AND contype='c' AND pg_get_constraintdef(oid) LIKE '%{value,model_name}%' LOOP
  EXECUTE format('ALTER TABLE agenteam_model.invocations DROP CONSTRAINT %I',c.conname);
 END LOOP;
END $$;
-- +goose StatementEnd
ALTER TABLE agenteam_model.invocations ADD CONSTRAINT model_invocation_identity_names CHECK ((
 identity_data#>>'{value,provider_id}'=historical_provider_id::text AND identity_data#>>'{value,model_id}'=historical_model_id::text
 AND length(identity_data#>>'{value,provider_name}') BETWEEN 1 AND 128
 AND octet_length(identity_data#>>'{value,model_name}') BETWEEN 1 AND 256
 AND octet_length(identity_data#>>'{value,provider_model_id}') BETWEEN 1 AND 256
 AND octet_length(identity_data#>>'{value,adapter_revision}') BETWEEN 1 AND 128
 AND identity_data#>>'{value,adapter_revision}' ~ '^[A-Za-z0-9_.:-]+$'
) IS TRUE);
ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT audit_records_model_contract;
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
     OR (action='model.update' AND metadata-ARRAY['provider_id','model_id','version','changed_fields']='{}'::jsonb AND agenteam_model.audit_fields(metadata->'changed_fields',ARRAY['notes','enabled','model_id','parameters','request_overwrite','header_overwrite','capabilities']))
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
