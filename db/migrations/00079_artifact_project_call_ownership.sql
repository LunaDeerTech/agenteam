-- agenteam:transaction tx
-- +goose Up

-- The existing technical work ledger now also records complete Artifact read
-- and convergence calls. They are not creation/upload work and their original
-- SQL/Audit returns must remain visible to Project Delete stopping.
ALTER TABLE agenteam_artifact.project_work
  DROP CONSTRAINT project_work_kind_check;
ALTER TABLE agenteam_artifact.project_work
  ADD CONSTRAINT project_work_kind_check
  CHECK (kind IN ('upload','creation','read','converge'));

-- No historical instance is guessed or backfilled. A legacy NULL is unknown
-- ownership; only the original live owner or exact foreign process death may
-- establish return. This column is an identity, never a Project authorization.
ALTER TABLE agenteam_artifact.project_work
  ADD COLUMN instance_id agenteam_object.safe_id;
