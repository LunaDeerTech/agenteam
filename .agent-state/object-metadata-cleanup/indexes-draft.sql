-- CANDIDATE FOR ROOT-ASSIGNED 00028. NOT A MIGRATION. NOT EXECUTED.
-- Root transferred Skills' former reservation to this shared cleanup task.
-- Wait for root to assemble stable 00025/26/27 before db/migrations placement.
-- These candidates follow d05-bounded-metadata-cleanup.md section 7.1.
-- Real EXPLAIN (ANALYZE, BUFFERS), FK-trigger cost, and budget evidence remain
-- required. No FK, lifecycle, receipt, or authority semantics are changed.

CREATE INDEX attempts_object_history ON agenteam_object.upload_attempts(object_id,id);
CREATE INDEX attempts_object_pending ON agenteam_object.upload_attempts(object_id,id)
  WHERE phase<>'cleaned' OR NOT cleanup_gate OR (kind='private_candidate' AND NOT io_closed);
CREATE INDEX attempts_transfer_fk ON agenteam_object.upload_attempts(transfer_id) WHERE transfer_id IS NOT NULL;
CREATE INDEX uploads_current_attempt_fk ON agenteam_object.uploads(current_attempt_id) WHERE current_attempt_id IS NOT NULL;
CREATE INDEX uploads_project_reserved ON agenteam_object.uploads(project_id,object_id) WHERE disposition='reserved';
CREATE INDEX cleanup_object_cause ON agenteam_object.cleanup_operations(object_id,created_at,id);
CREATE INDEX cleanup_object_pending ON agenteam_object.cleanup_operations(object_id,attempt_id) WHERE phase<>'completed';
CREATE INDEX references_upload_fk ON agenteam_object.object_references(upload_id) WHERE upload_id IS NOT NULL;
CREATE INDEX leases_object_history ON agenteam_object.object_leases(object_id,id);
CREATE INDEX leases_attempt_fk ON agenteam_object.object_leases(attempt_id) WHERE attempt_id IS NOT NULL;
CREATE INDEX object_work_history ON agenteam_object.project_work(object_id,id);
CREATE INDEX object_work_pending ON agenteam_object.project_work(object_id,id) WHERE joined_at IS NULL;
CREATE INDEX object_work_project_pending ON agenteam_object.project_work(project_id,id) WHERE joined_at IS NULL;
CREATE INDEX transfer_object_history ON agenteam_object.object_transfers(object_id,id);
CREATE INDEX transfer_object_pending ON agenteam_object.object_transfers(object_id,id)
  WHERE revoked_at IS NULL OR retirement_evidence IS NULL;
CREATE INDEX transfer_project_pending ON agenteam_object.object_transfers(project_id,id)
  WHERE revoked_at IS NULL OR retirement_evidence IS NULL;
CREATE INDEX transfer_upload_fk ON agenteam_object.object_transfers(upload_id) WHERE upload_id IS NOT NULL;
CREATE INDEX transfer_candidate_fk ON agenteam_object.object_transfers(candidate_id) WHERE candidate_id IS NOT NULL;
CREATE INDEX transfer_source_lease_fk ON agenteam_object.object_transfers(source_lease_id) WHERE source_lease_id IS NOT NULL;
CREATE INDEX objects_project_id ON agenteam_object.objects(project_id,id);
CREATE INDEX download_grants_project_active ON agenteam_download.grants(project_id,id) WHERE NOT revoked;

-- Skills consumer query is specified, not implemented here. One full Project
-- prefix is the first candidate for joined history ORDER BY id and the
-- (project_id,skill_id) FK probe; retain existing live/recovery partials.
-- A joined-only partial alone would not cover the complete FK probe.
CREATE INDEX skill_work_project_history ON agenteam_skill.work(project_id,id);
