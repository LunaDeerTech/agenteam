-- agenteam:transaction tx
-- +goose Up
-- The profile's retained snapshot provenance and its originating Knowledge
-- operation form a cycle. Project cleanup releases the Model-owned reference
-- first, then removes this exact Knowledge pair in one transaction. The FK
-- remains immediate for every other caller and is checked again at COMMIT.
ALTER TABLE agenteam_knowledge.index_profiles
 DROP CONSTRAINT index_profiles_source_operation_id_fkey,
 ADD CONSTRAINT index_profiles_source_operation_id_fkey
 FOREIGN KEY (source_operation_id)
 REFERENCES agenteam_knowledge.model_operations(id)
 ON DELETE NO ACTION DEFERRABLE INITIALLY IMMEDIATE;
