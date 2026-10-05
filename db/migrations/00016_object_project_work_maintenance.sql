-- agenteam:transaction tx
-- +goose Up

-- A recovery verifier is new I/O, independent of the original uploader.
-- Each cleanup claim keeps its original worker UUID as work.id, its cleanup
-- operation as resource_id, and the actual executing process as process_id.
-- Reclaiming the native operation must not replace the preceding work record.
ALTER TABLE agenteam_object.project_work ADD COLUMN cleanup_claim_fence bigint;
ALTER TABLE agenteam_object.project_work DROP CONSTRAINT project_work_kind_check;
ALTER TABLE agenteam_object.project_work ADD CONSTRAINT project_work_kind_check CHECK (
  kind IN ('preparation','reader','source','download','transfer_get','transfer_put','verification','cleanup'));
ALTER TABLE agenteam_object.project_work ADD CONSTRAINT project_work_cleanup_claim_check CHECK (
  (kind='cleanup' AND cleanup_claim_fence IS NOT NULL AND cleanup_claim_fence>0)
  OR (kind<>'cleanup' AND cleanup_claim_fence IS NULL));
ALTER TABLE agenteam_object.project_work ADD CONSTRAINT project_work_maintenance_object_check CHECK (
  kind NOT IN ('verification','cleanup') OR object_id IS NOT NULL);

-- Joined claims also retain their identity. Runtime registration must still
-- compare every immutable field; this index is not an authorization or a join.
CREATE UNIQUE INDEX object_project_work_cleanup_claim
  ON agenteam_object.project_work(resource_id,cleanup_claim_fence) WHERE kind='cleanup';
