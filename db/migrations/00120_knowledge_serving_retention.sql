-- agenteam:transaction tx
-- +goose Up
-- A stored embedding snapshot is not a live Secret reference. Existing derived
-- profiles did not own one, so they require a fresh authorized rebuild. The
-- canonical documents and immutable source versions are preserved.
ALTER TABLE agenteam_knowledge.index_profiles
 ADD COLUMN retention_state text NOT NULL DEFAULT 'unretained'
  CHECK(retention_state IN ('unretained','retained','released')),
 ADD COLUMN source_operation_id agenteam_knowledge.safe_id
  REFERENCES agenteam_knowledge.model_operations(id) ON DELETE RESTRICT;

UPDATE agenteam_knowledge.index_state
 SET serving_profile_id=NULL,version=version+1
 WHERE serving_profile_id IS NOT NULL;
UPDATE agenteam_knowledge.index_profiles
 SET state='retired'
 WHERE state IN ('building','ready','active');
UPDATE agenteam_knowledge.documents
 SET indexing_status='pending'
 WHERE status='active' AND indexing_status<>'pending';

-- Profile version already identifies its immutable embedding space. Retain
-- and release use that exact version; lifecycle state is checked separately.
ALTER TABLE agenteam_knowledge.index_profiles
 ADD CONSTRAINT knowledge_profile_retention_source CHECK(
  (retention_state='unretained' AND source_operation_id IS NULL)
  OR (retention_state IN ('retained','released') AND source_operation_id IS NOT NULL AND snapshot_id IS NOT NULL)
 ),
 ADD CONSTRAINT knowledge_profile_retention_lifecycle CHECK(
  (state='building' AND ((snapshot_id IS NULL AND retention_state='unretained') OR retention_state='retained'))
  OR (state IN ('ready','active') AND retention_state='retained')
  OR (state IN ('retired','failed'))
 ),
 ADD CONSTRAINT knowledge_profile_released_terminal CHECK(
  retention_state<>'released' OR state IN ('retired','failed')
 );
CREATE INDEX knowledge_profile_retention_cleanup
 ON agenteam_knowledge.index_profiles(project_id,created_at,id)
 WHERE state='building' OR (state IN ('retired','failed') AND retention_state='retained');
