-- agenteam:transaction tx
-- +goose Up
-- Historical profiles remain pinned until a complete authorized replacement.
-- Configuration versions cannot be reconstructed from their snapshot IDs.
ALTER TABLE agenteam_knowledge.index_profiles
 ADD COLUMN profile_data jsonb,
 ADD COLUMN profile_digest text,
 ADD COLUMN source_digest text,
 ADD CONSTRAINT knowledge_embedding_profile CHECK (
  (profile_data IS NULL AND profile_digest IS NULL AND source_digest IS NULL)
  OR (profile_data IS NOT NULL AND profile_digest IS NOT NULL AND source_digest IS NOT NULL
   AND source_digest ~ '^sha256:[0-9a-f]{64}$'
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
-- +goose StatementBegin
CREATE FUNCTION agenteam_knowledge.embedding_profile_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.profile_data,NEW.profile_digest,NEW.source_digest) IS DISTINCT FROM
    (OLD.profile_data,OLD.profile_digest,OLD.source_digest) THEN
  RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='knowledge_embedding_profile_immutable',MESSAGE='immutable knowledge embedding profile';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER knowledge_embedding_profile_immutable BEFORE UPDATE ON agenteam_knowledge.index_profiles
 FOR EACH ROW EXECUTE FUNCTION agenteam_knowledge.embedding_profile_immutable();
