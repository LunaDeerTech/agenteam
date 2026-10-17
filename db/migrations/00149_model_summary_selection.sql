-- agenteam:transaction tx
-- +goose Up
-- Pre-release reset: old independent selector values are deliberately not
-- translated. Operators must configure the new single platform resource.
DELETE FROM agenteam_model.references WHERE owner_kind='platform_selector';
DELETE FROM agenteam_model.commands WHERE scope='system' AND
 (command_name='model.selection.update'
  OR mutation_plan->'BeforeSelection' IS DISTINCT FROM 'null'::jsonb
     AND mutation_plan ? 'BeforeSelection'
  OR mutation_plan->'BeforeMeetingSummary' IS DISTINCT FROM 'null'::jsonb
     AND mutation_plan ? 'BeforeMeetingSummary');
DROP TABLE agenteam_model.meeting_summary_selection;
DROP TABLE agenteam_model.platform_selection;
CREATE TABLE agenteam_model.platform_selection (
 id agenteam_model.safe_id PRIMARY KEY,
 singleton boolean NOT NULL DEFAULT true UNIQUE CHECK(singleton),
 version bigint NOT NULL CHECK(version>0),
 configured boolean NOT NULL,
 embedding_id agenteam_model.safe_id REFERENCES agenteam_model.models(id) ON DELETE RESTRICT,
 summary_id agenteam_model.safe_id REFERENCES agenteam_model.models(id) ON DELETE RESTRICT,
 reranker_id agenteam_model.safe_id REFERENCES agenteam_model.models(id) ON DELETE RESTRICT,
 image_id agenteam_model.safe_id REFERENCES agenteam_model.models(id) ON DELETE RESTRICT,
 updated_at timestamptz(6) NOT NULL,
 CHECK((NOT configured AND embedding_id IS NULL AND summary_id IS NULL AND reranker_id IS NULL AND image_id IS NULL)
   OR (configured AND embedding_id IS NOT NULL AND summary_id IS NOT NULL))
);
ALTER TABLE agenteam_model.references DROP CONSTRAINT references_check;
ALTER TABLE agenteam_model.references ADD CONSTRAINT references_check CHECK (
 (owner_kind='platform_selector' AND project_id IS NULL AND role IN ('embedding','summary','reranker','image') AND reasoning_effort='')
 OR (owner_kind='agent' AND project_id IS NOT NULL AND role IN ('agent_model','approval_model'))
 OR (owner_kind='project_summary' AND project_id IS NOT NULL AND owner_id=project_id AND role='meeting_summary')
);

-- Current Memory calls persist only the shared summary selector. Old
-- resolve bindings are not migrated; deployments with old records rebuild.
ALTER TABLE agenteam_memory.model_calls DROP CONSTRAINT memory_model_resolution_origin;
ALTER TABLE agenteam_memory.model_calls
 ADD CONSTRAINT memory_model_resolution_origin CHECK((
  (resolve_source IS NULL AND resolve_binding IS NULL)
  OR (resolve_source IS NOT NULL AND resolve_binding IS NOT NULL AND process_id IS NOT NULL
   AND resolve_binding ~ '^sha256:[0-9a-f]{64}$' AND octet_length(resolve_source::text)<=4096
   AND jsonb_typeof(resolve_source)='object' AND resolve_source->'schema'='"1"'::jsonb
   AND (resolve_source-ARRAY['schema','source','selection','serving_snapshot_id','serving_generation_id'])='{}'::jsonb
   AND (
    (resolve_source->>'source'='current_selection' AND resolve_source ? 'selection'
     AND NOT(resolve_source ?| ARRAY['serving_snapshot_id','serving_generation_id'])
     AND jsonb_typeof(resolve_source->'selection')='object'
     AND ((resolve_source->'selection')-ARRAY['kind','selector','version'])='{}'::jsonb
     AND resolve_source->'selection'->>'kind'='platform'
     AND ((purpose='memory_embedding' AND resolve_source->'selection'->>'selector'='embedding')
       OR (purpose IN ('memory_extraction','memory_consolidation','memory_reflection') AND resolve_source->'selection'->>'selector'='summary'))
     AND (NOT((resolve_source->'selection') ? 'version') OR (jsonb_typeof(resolve_source->'selection'->'version')='string' AND (resolve_source->'selection'->>'version') ~ '^[1-9][0-9]{0,18}$')))
    OR (resolve_source->>'source'='serving_snapshot' AND purpose='memory_embedding' AND NOT(resolve_source ? 'selection')
     AND (resolve_source->>'serving_snapshot_id') ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
     AND (resolve_source->>'serving_generation_id') ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')
   ))
 ) IS TRUE);
