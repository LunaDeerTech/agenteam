-- agenteam:transaction tx
-- +goose Up
-- The intent belongs to the current Contribution generation/attempt. Runtime
-- applies it only to that association through the original Execution owner.
ALTER TABLE agenteam_meeting.contributions
 ADD COLUMN cancel_requested boolean NOT NULL DEFAULT false,
 ADD CONSTRAINT meeting_agent_cancellation CHECK(NOT cancel_requested OR
  (kind='agent' AND current_execution_id IS NOT NULL));
