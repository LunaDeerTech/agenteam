-- agenteam:transaction tx
-- +goose Up
-- Owners freeze the exact published Artifact metadata in their mutation Tx.
-- Execution trigger canonical input carries the same immutable ordered values.
ALTER TABLE agenteam_work.tasks ADD COLUMN attachment_data jsonb NOT NULL DEFAULT '[]';
ALTER TABLE agenteam_work.tasks ADD CONSTRAINT tasks_attachment_data_check CHECK (
 jsonb_typeof(attachment_data)='array' AND jsonb_array_length(attachment_data)<=4);
ALTER TABLE agenteam_meeting.messages ADD COLUMN attachment_data jsonb NOT NULL DEFAULT '[]';
ALTER TABLE agenteam_meeting.messages ADD CONSTRAINT meeting_attachment_data_check CHECK (
 jsonb_typeof(attachment_data)='array' AND jsonb_array_length(attachment_data)<=4);
