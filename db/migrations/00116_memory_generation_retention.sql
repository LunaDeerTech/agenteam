-- agenteam:transaction tx
-- +goose Up
-- Legacy projections did not own a serving credential reference. Retire only
-- that derived state; canonical memories remain available for a fresh build.
ALTER TABLE agenteam_memory.generations
 ADD COLUMN owner_version bigint NOT NULL DEFAULT 1 CHECK(owner_version>0),
 ADD COLUMN retention_state text NOT NULL DEFAULT 'unretained' CHECK(retention_state IN ('unretained','retained','released')),
 ADD COLUMN source_call_id agenteam_memory.safe_id REFERENCES agenteam_memory.model_calls(call_id),
 ADD COLUMN process_id agenteam_memory.safe_id;
UPDATE agenteam_memory.namespaces SET serving_generation_id=NULL WHERE serving_generation_id IS NOT NULL;
UPDATE agenteam_memory.generations SET status='retired',owner_version=2;
ALTER TABLE agenteam_memory.generations ADD CONSTRAINT memory_generation_retention_lifecycle CHECK (
 (status IN ('building','ready') AND owner_version=1 AND retention_state='retained' AND source_call_id IS NOT NULL AND process_id IS NOT NULL)
 OR (status='retired' AND owner_version=2 AND retention_state IN ('unretained','retained','released'))
);
ALTER TABLE agenteam_memory.model_calls
 ADD COLUMN process_id agenteam_memory.safe_id,
 ADD COLUMN lease_id agenteam_memory.safe_id,
 ADD COLUMN attempt_index bigint CHECK(attempt_index>0),
 ADD COLUMN attempt_process_id agenteam_memory.safe_id,
 ADD COLUMN attempt_fence bigint CHECK(attempt_fence>0),
 ADD CONSTRAINT memory_model_attempt_binding CHECK (
 (attempt_index IS NULL AND attempt_process_id IS NULL AND attempt_fence IS NULL)
 OR (invocation_id IS NOT NULL AND attempt_index=1 AND attempt_process_id IS NOT NULL AND attempt_process_id=process_id AND attempt_fence IS NOT NULL)
 );
CREATE INDEX memory_generation_cleanup ON agenteam_memory.generations(project_id,agent_id,created_at)
 WHERE status='building' OR (status='retired' AND retention_state='retained');
