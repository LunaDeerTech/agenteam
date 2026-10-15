-- agenteam:transaction tx
-- +goose Up
-- Summary generations retain the same immutable snapshot and credential
-- reference across their bounded logical attempts. Existing embedding owner
-- identities and references are unchanged; the consumer authority still
-- proves generation ownership and lifecycle in the original transaction.
ALTER TABLE agenteam_model.serving_snapshot_retentions
 DROP CONSTRAINT serving_snapshot_retentions_consumer_kind_check,
 DROP CONSTRAINT serving_snapshot_retentions_check,
 ADD CONSTRAINT model_serving_retention_kind CHECK(consumer_kind IN ('knowledge','memory','meeting')),
 ADD CONSTRAINT model_serving_retention_owner CHECK(
  (consumer_kind IN ('knowledge','meeting') AND agent_id IS NULL)
  OR (consumer_kind='memory' AND agent_id IS NOT NULL)
 );
