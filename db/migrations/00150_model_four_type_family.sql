-- agenteam:transaction tx
-- +goose Up
-- A compatible gateway shares one endpoint and credential across all four
-- model types. Reranking uses the Jina-compatible format, not an OpenAI API.
-- Preserve the existing parent SHARE lock, foreign key and immutable protocol
-- triggers; only extend their closed protocol/type relation.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_model.protocol_supports_type(protocol text, model_type text)
 RETURNS boolean LANGUAGE sql IMMUTABLE STRICT AS $$
 SELECT CASE protocol
  WHEN 'openai-compatible' THEN model_type IN ('chat','embedding','reranker','image_generation')
  WHEN 'openai-chat-completions' THEN model_type='chat'
  WHEN 'anthropic-messages' THEN model_type='chat'
  WHEN 'openai-embeddings' THEN model_type='embedding'
  WHEN 'jina-rerank' THEN model_type='reranker'
  WHEN 'openai-images-generations' THEN model_type='image_generation'
  ELSE false END
$$;
-- +goose StatementEnd
