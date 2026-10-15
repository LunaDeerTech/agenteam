-- agenteam:transaction tx
-- +goose Up
-- A readable file projection belongs to the exact generation/document version.
-- NULL preserves older generations; readers never fabricate extracted text.
ALTER TABLE agenteam_knowledge.index_generations
 ADD COLUMN parsed_text text,
 ADD COLUMN parsed_profile text,
 ADD CONSTRAINT knowledge_parsed_text_bound CHECK(
  (parsed_text IS NULL AND parsed_profile IS NULL)
  OR (parsed_text IS NOT NULL AND parsed_profile IS NOT NULL
   AND octet_length(parsed_text)<=1048576
   AND octet_length(parsed_profile) BETWEEN 1 AND 256));
