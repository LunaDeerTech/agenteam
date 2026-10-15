-- agenteam:transaction tx
-- +goose Up
-- 00115's cross-column released_version CHECK was named *_check; the
-- Knowledge/Memory owner CHECK was named *_check1. 00127 removed the former
-- instead of the latter. Its explicitly named, expanded owner CHECK is already
-- correct and remains in force. Preserve credential/state checks and all rows.
-- Validation of the restored bound is intentional: invalid stored versions
-- abort this migration atomically rather than being silently rewritten.
ALTER TABLE agenteam_model.serving_snapshot_retentions
 DROP CONSTRAINT serving_snapshot_retentions_check1,
 ADD CONSTRAINT model_serving_retention_released_version
  CHECK(released_version>=owner_version);
