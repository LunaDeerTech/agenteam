-- agenteam:transaction tx
-- +goose Up
-- One System provider may share its endpoint/credential across two supported
-- model types. Runtime snapshots still use the existing concrete wire profiles.
-- Existing endpoint-specific records remain unchanged; Project provider
-- creation retains its existing constraint and is outside this UI work.
ALTER TABLE agenteam_model.providers DROP CONSTRAINT providers_protocol_check;
ALTER TABLE agenteam_model.providers ADD CONSTRAINT providers_protocol_check
 CHECK(protocol IN ('openai-chat-completions','anthropic-messages','openai-embeddings','jina-rerank','openai-images-generations','openai-compatible'));

-- A single generated capability_type cannot express a shared provider. Keep
-- referential integrity and enforce the closed protocol/type relation under a
-- parent row SHARE lock. This conflicts with protocol changes/deletion, so a
-- concurrent insert cannot pass against an obsolete provider configuration.
ALTER TABLE agenteam_model.models DROP CONSTRAINT models_provider_id_type_fkey;
ALTER TABLE agenteam_model.models ADD CONSTRAINT models_provider_id_fkey
 FOREIGN KEY(provider_id) REFERENCES agenteam_model.providers(id) ON DELETE RESTRICT;
ALTER TABLE agenteam_model.providers DROP CONSTRAINT providers_id_capability_type_key;
ALTER TABLE agenteam_model.providers DROP COLUMN capability_type;

-- +goose StatementBegin
CREATE FUNCTION agenteam_model.protocol_supports_type(protocol text, model_type text)
 RETURNS boolean LANGUAGE sql IMMUTABLE STRICT AS $$
 SELECT CASE protocol
  WHEN 'openai-compatible' THEN model_type IN ('chat','embedding')
  WHEN 'openai-chat-completions' THEN model_type='chat'
  WHEN 'anthropic-messages' THEN model_type='chat'
  WHEN 'openai-embeddings' THEN model_type='embedding'
  WHEN 'jina-rerank' THEN model_type='reranker'
  WHEN 'openai-images-generations' THEN model_type='image_generation'
  ELSE false END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION agenteam_model.check_model_provider_type() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE provider_protocol text;
BEGIN
 SELECT protocol INTO provider_protocol FROM agenteam_model.providers
  WHERE id=NEW.provider_id FOR SHARE;
 IF NOT FOUND THEN
  RAISE EXCEPTION USING ERRCODE='23503',CONSTRAINT='models_provider_id_fkey',MESSAGE='model provider missing';
 END IF;
 IF agenteam_model.protocol_supports_type(provider_protocol,NEW.type) IS NOT TRUE THEN
  RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='models_provider_type_check',MESSAGE='unsupported provider model type';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER models_provider_type_check BEFORE INSERT OR UPDATE OF provider_id,type ON agenteam_model.models
 FOR EACH ROW EXECUTE FUNCTION agenteam_model.check_model_provider_type();

-- Provider protocol is immutable in the command API. Enforce the same rule at
-- storage, avoiding a cross-table snapshot race at stricter isolation levels.
-- +goose StatementBegin
CREATE FUNCTION agenteam_model.immutable_provider_protocol() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.protocol IS DISTINCT FROM OLD.protocol THEN
  RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='providers_protocol_immutable',MESSAGE='immutable provider protocol';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER providers_protocol_immutable BEFORE UPDATE OF protocol ON agenteam_model.providers
 FOR EACH ROW EXECUTE FUNCTION agenteam_model.immutable_provider_protocol();
