-- agenteam:transaction tx
-- +goose Up
-- New transitions retain only immutable Decision identity/state facts. No
-- historical events are fabricated for existing DecisionRequest rows. These
-- facts survive Meeting purge so a late event cannot reopen a deleted source.
CREATE TABLE agenteam_meeting.decision_events (
 event_id agenteam_meeting.safe_id PRIMARY KEY,
 project_id agenteam_meeting.safe_id NOT NULL,
 meeting_id agenteam_meeting.safe_id NOT NULL,
 turn_id agenteam_meeting.safe_id NOT NULL,
 contribution_id agenteam_meeting.safe_id NOT NULL,
 participant_id agenteam_meeting.safe_id NOT NULL,
 execution_id agenteam_meeting.safe_id NOT NULL,
 decision_id agenteam_meeting.safe_id NOT NULL,
 version bigint NOT NULL CHECK(version>0),
 event_type text NOT NULL,
 status text NOT NULL,
 occurred_at timestamptz(6) NOT NULL,
 UNIQUE(decision_id,version,event_type),
 CONSTRAINT meeting_decision_event_state CHECK(
  (event_type='meeting.decision.requested' AND status='pending') OR
  (event_type='meeting.decision.answered' AND status='answered') OR
  (event_type='meeting.decision.skipped' AND status='skipped') OR
  (event_type='meeting.decision.cancelled' AND status='cancelled')
 )
);
CREATE INDEX meeting_decision_events_source ON agenteam_meeting.decision_events(project_id,decision_id,version DESC);
CREATE TRIGGER meeting_decision_event_immutable BEFORE UPDATE OR DELETE ON agenteam_meeting.decision_events FOR EACH ROW EXECUTE FUNCTION agenteam_meeting.immutable_body();

-- Existing proposal items keep their fields and empty routing context. Only
-- the new Decision pair may carry its one typed parent identity; this is
-- navigation data, never permission or a replacement Decision state machine.
ALTER TABLE agenteam_humaninbox.items
 ADD COLUMN source_context jsonb NOT NULL DEFAULT '{}'::jsonb,
 DROP CONSTRAINT items_source_type_check,
 DROP CONSTRAINT items_action_type_check,
 ADD CONSTRAINT humaninbox_source_action_context CHECK((
  (source_type='meeting' AND action_type='meeting.review_proposal' AND source_context='{}'::jsonb)
  OR
  (source_type='decision_request' AND action_type='decision.answer'
   AND jsonb_typeof(source_context)='object'
   AND source_context ? 'meeting_id'
   AND source_context-'meeting_id'='{}'::jsonb
   AND jsonb_typeof(source_context->'meeting_id')='string'
   AND source_context->>'meeting_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$')
 ) IS TRUE);
