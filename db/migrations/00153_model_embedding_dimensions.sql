-- agenteam:transaction tx
-- +goose Up
-- Pre-release: old development model/snapshot data must be rebuilt. There is
-- no guessed dimension or compatibility default for an unconfigured model.
-- +goose StatementBegin
CREATE FUNCTION agenteam_model.valid_embedding_capabilities(model_type text, capabilities jsonb)
 RETURNS boolean LANGUAGE sql IMMUTABLE AS $$
 SELECT CASE WHEN model_type='embedding' THEN
  (jsonb_typeof(capabilities->'embedding')='object'
   AND (capabilities->'embedding')-ARRAY['dimensions','dimensions_mode']='{}'::jsonb
   AND jsonb_typeof(capabilities->'embedding'->'dimensions')='string'
   AND CASE WHEN capabilities->'embedding'->>'dimensions' ~ '^[1-9][0-9]{0,3}$'
       THEN (capabilities->'embedding'->>'dimensions')::integer BETWEEN 1 AND 4096 ELSE false END
   AND capabilities->'embedding'->>'dimensions_mode' IN ('fixed','request')) IS TRUE
 ELSE (capabilities ? 'embedding' AND capabilities->'embedding'='null'::jsonb) IS TRUE END
$$;
-- +goose StatementEnd
ALTER TABLE agenteam_model.models ADD CONSTRAINT models_embedding_capabilities_check
 CHECK(agenteam_model.valid_embedding_capabilities(type,capabilities) IS TRUE);
ALTER TABLE agenteam_model.snapshots ADD CONSTRAINT model_snapshot_embedding_capabilities_check
 CHECK(agenteam_model.valid_embedding_capabilities(snapshot_data->'Identity'->>'model_type',snapshot_data->'Capabilities') IS TRUE);

-- Shared profile data is immutable and names the exact vector space. Preserve
-- the existing identity/version checks and require both new fields explicitly.
-- +goose StatementBegin
CREATE FUNCTION agenteam_model.valid_embedding_profile(profile_data jsonb, expected_dimensions integer)
 RETURNS boolean LANGUAGE sql IMMUTABLE AS $$
 SELECT (jsonb_typeof(profile_data)='object'
  AND profile_data ?& ARRAY['selection_version','model_id','model_version','provider_id','provider_version','adapter_revision','dimensions','dimensions_mode']
  AND profile_data-ARRAY['selection_version','model_id','model_version','provider_id','provider_version','adapter_revision','dimensions','dimensions_mode']='{}'::jsonb
  AND jsonb_typeof(profile_data->'selection_version')='string'
  AND jsonb_typeof(profile_data->'model_version')='string'
  AND jsonb_typeof(profile_data->'provider_version')='string'
  AND (profile_data->>'selection_version') ~ '^[1-9][0-9]{0,18}$'
  AND (profile_data->>'model_version') ~ '^[1-9][0-9]{0,18}$'
  AND (profile_data->>'provider_version') ~ '^[1-9][0-9]{0,18}$'
  AND jsonb_typeof(profile_data->'model_id')='string'
  AND jsonb_typeof(profile_data->'provider_id')='string'
  AND (profile_data->>'model_id') ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
  AND (profile_data->>'provider_id') ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
  AND jsonb_typeof(profile_data->'adapter_revision')='string'
  AND (profile_data->>'adapter_revision') ~ '^[A-Za-z0-9_.:-]{1,128}$'
  AND expected_dimensions BETWEEN 1 AND 4096
  AND jsonb_typeof(profile_data->'dimensions')='number'
  AND (profile_data->>'dimensions') ~ '^[1-9][0-9]{0,3}$'
  AND profile_data->'dimensions'=to_jsonb(expected_dimensions)
  AND jsonb_typeof(profile_data->'dimensions_mode')='string'
  AND profile_data->>'dimensions_mode' IN ('fixed','request')) IS TRUE
$$;
-- +goose StatementEnd
ALTER TABLE agenteam_knowledge.index_profiles DROP CONSTRAINT knowledge_embedding_profile;
ALTER TABLE agenteam_knowledge.index_profiles ADD CONSTRAINT knowledge_embedding_profile CHECK (
 ((profile_data IS NULL AND profile_digest IS NULL AND source_digest IS NULL)
  OR (profile_data IS NOT NULL AND profile_digest IS NOT NULL AND source_digest IS NOT NULL
   AND source_digest ~ '^sha256:[0-9a-f]{64}$'
   AND profile_digest ~ '^sha256:[0-9a-f]{64}$'
   AND agenteam_model.valid_embedding_profile(profile_data,dimension))) IS TRUE
);
ALTER TABLE agenteam_memory.generations DROP CONSTRAINT memory_generation_profile;
ALTER TABLE agenteam_memory.generations ADD CONSTRAINT memory_generation_profile CHECK (
 ((schema_version=1 AND profile_data IS NULL AND profile_digest IS NULL)
  OR (schema_version=2 AND profile_data IS NOT NULL AND profile_digest IS NOT NULL
   AND profile_digest ~ '^sha256:[0-9a-f]{64}$'
   AND agenteam_model.valid_embedding_profile(profile_data,dimension))) IS TRUE
);
