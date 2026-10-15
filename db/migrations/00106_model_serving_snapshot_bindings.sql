-- agenteam:transaction tx
-- +goose Up
-- A serving embedding generation retains one immutable Model snapshot across
-- calls. Each call still owns a distinct preparation, consumer binding and
-- Secret lease. The composite preparation and snapshot foreign keys remain.
ALTER TABLE agenteam_model.resolution_preparations
 DROP CONSTRAINT resolution_preparations_snapshot_id_key;
CREATE INDEX model_resolution_preparations_snapshot
 ON agenteam_model.resolution_preparations(snapshot_id);
