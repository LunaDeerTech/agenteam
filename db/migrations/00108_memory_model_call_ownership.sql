-- agenteam:transaction tx
-- +goose Up
ALTER TABLE agenteam_memory.model_calls
 ADD COLUMN terminal_version bigint CHECK(terminal_version IS NULL OR terminal_version=1);
