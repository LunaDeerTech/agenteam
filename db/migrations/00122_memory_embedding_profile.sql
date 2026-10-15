-- agenteam:transaction tx
-- +goose Up
-- Schema 1 generations keep their original pinned embedding space. Their
-- configuration revisions cannot be inferred from a newly captured snapshot.
ALTER TABLE agenteam_memory.generations
 ADD COLUMN profile_data jsonb,
 ADD COLUMN profile_digest text,
 ADD CONSTRAINT memory_generation_profile CHECK (
  (schema_version=1 AND profile_data IS NULL AND profile_digest IS NULL)
  OR (schema_version=2 AND profile_data IS NOT NULL AND profile_digest IS NOT NULL
   AND profile_digest ~ '^sha256:[0-9a-f]{64}$'
   AND jsonb_typeof(profile_data)='object'
   AND profile_data ?& ARRAY['selection_version','model_id','model_version','provider_id','provider_version','adapter_revision']
   AND profile_data-ARRAY['selection_version','model_id','model_version','provider_id','provider_version','adapter_revision']='{}'::jsonb
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
   AND (profile_data->>'adapter_revision') ~ '^[A-Za-z0-9_.:-]{1,128}$')
 );

-- Identity, dimensions and profile stay immutable through publish and retire.
-- +goose StatementBegin
CREATE FUNCTION agenteam_memory.generation_profile_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.generation_id,NEW.project_id,NEW.agent_id,NEW.snapshot_id,NEW.dimension,NEW.schema_version,NEW.profile_data,NEW.profile_digest)
  IS DISTINCT FROM
  (OLD.generation_id,OLD.project_id,OLD.agent_id,OLD.snapshot_id,OLD.dimension,OLD.schema_version,OLD.profile_data,OLD.profile_digest) THEN
  RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='memory_generation_profile_immutable',MESSAGE='immutable memory generation profile';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER memory_generation_profile_immutable BEFORE UPDATE ON agenteam_memory.generations
 FOR EACH ROW EXECUTE FUNCTION agenteam_memory.generation_profile_immutable();
