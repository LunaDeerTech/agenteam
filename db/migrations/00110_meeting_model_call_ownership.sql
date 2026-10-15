-- agenteam:transaction tx
-- +goose Up
-- Meeting owns the durable reason for each summary call. Model retains its
-- invocation/attempt/lease facts; this table never stores credential values.
ALTER TABLE agenteam_meeting.summary_generations ADD CONSTRAINT meeting_summary_generation_scope UNIQUE(meeting_id,id);
CREATE TABLE agenteam_meeting.summary_model_calls (
 call_id agenteam_meeting.safe_id PRIMARY KEY,
 generation_id agenteam_meeting.safe_id NOT NULL,
 project_id agenteam_meeting.safe_id NOT NULL,
 meeting_id agenteam_meeting.safe_id NOT NULL,
 attempt integer NOT NULL CHECK(attempt BETWEEN 1 AND 3),
 purpose text NOT NULL CHECK(purpose IN('meeting_summary_initial','meeting_summary_update')),
 input_digest text NOT NULL CHECK(input_digest ~ '^sha256:[0-9a-f]{64}$'),
 messages_data bytea NOT NULL CHECK(octet_length(messages_data) BETWEEN 2 AND 1048576),
 response_format_data bytea NOT NULL CHECK(octet_length(response_format_data) BETWEEN 2 AND 65536),
 status text NOT NULL CHECK(status IN('prepared','resolving','resolved','running','succeeded','failed','cancelled','unknown')),
 snapshot_id agenteam_meeting.safe_id,
 resolution_unknown boolean NOT NULL DEFAULT false,
 deadline_at timestamptz(6) NOT NULL,
 created_at timestamptz(6) NOT NULL,
 returned_at timestamptz(6),
 terminal_version bigint CHECK(terminal_version=1),
 retired_at timestamptz(6),
 error_code text NOT NULL DEFAULT '',
 UNIQUE(generation_id,attempt),
 FOREIGN KEY(meeting_id,generation_id) REFERENCES agenteam_meeting.summary_generations(meeting_id,id),
 FOREIGN KEY(project_id,meeting_id) REFERENCES agenteam_meeting.sessions(project_id,id),
 CHECK(deadline_at>created_at),
 CHECK((returned_at IS NULL AND terminal_version IS NULL AND retired_at IS NULL) OR
       (returned_at>=created_at AND terminal_version=1 AND status IN('succeeded','failed','cancelled'))),
 CHECK(retired_at IS NULL OR retired_at>=returned_at),
 CHECK(status NOT IN('resolved','running','succeeded') OR snapshot_id IS NOT NULL)
);
CREATE INDEX meeting_summary_unretired ON agenteam_meeting.summary_model_calls(meeting_id,call_id) WHERE retired_at IS NULL;
-- +goose StatementBegin
CREATE FUNCTION agenteam_meeting.guard_summary_call_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ROW(NEW.call_id,NEW.generation_id,NEW.project_id,NEW.meeting_id,NEW.attempt,NEW.purpose,NEW.input_digest,NEW.messages_data,NEW.response_format_data,NEW.deadline_at,NEW.created_at)
 IS DISTINCT FROM ROW(OLD.call_id,OLD.generation_id,OLD.project_id,OLD.meeting_id,OLD.attempt,OLD.purpose,OLD.input_digest,OLD.messages_data,OLD.response_format_data,OLD.deadline_at,OLD.created_at)
 OR (OLD.snapshot_id IS NOT NULL AND NEW.snapshot_id IS DISTINCT FROM OLD.snapshot_id)
 OR (OLD.returned_at IS NOT NULL AND ROW(NEW.returned_at,NEW.terminal_version,NEW.status) IS DISTINCT FROM ROW(OLD.returned_at,OLD.terminal_version,OLD.status))
 OR (OLD.retired_at IS NOT NULL AND NEW.retired_at IS DISTINCT FROM OLD.retired_at) THEN
  RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='immutable meeting summary call identity';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER meeting_summary_call_identity BEFORE UPDATE ON agenteam_meeting.summary_model_calls FOR EACH ROW EXECUTE FUNCTION agenteam_meeting.guard_summary_call_identity();
