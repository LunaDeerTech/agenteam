-- agenteam:transaction tx
-- +goose Up
-- One logical Summary generation keeps its first selected Model snapshot
-- through all finite validation attempts. Credential material stays in Secret.
ALTER TABLE agenteam_meeting.summary_generations
 ADD COLUMN snapshot_id agenteam_meeting.safe_id,
 ADD COLUMN retention_version bigint NOT NULL DEFAULT 1 CHECK(retention_version IN(1,2)),
 ADD COLUMN retained_at timestamptz(6),
 ADD COLUMN released_at timestamptz(6),
 ADD CONSTRAINT meeting_summary_snapshot_lifecycle CHECK(
  (snapshot_id IS NULL AND retained_at IS NULL AND released_at IS NULL AND retention_version=1)
  OR
  (snapshot_id IS NOT NULL AND retained_at IS NOT NULL AND retained_at>=created_at AND
   ((released_at IS NULL AND retention_version=1) OR (released_at>=retained_at AND retention_version=2)))
 ),
 ADD CONSTRAINT meeting_summary_release_terminal CHECK(released_at IS NULL OR status IN('published','cancelled') OR (status='failed' AND attempt=3));

-- Existing calls have no historical preparation-process fact. Leave them
-- unknown; neither this migration nor recovery may invent one retrospectively.
ALTER TABLE agenteam_meeting.summary_model_calls
 ADD COLUMN prepared_process_id agenteam_meeting.safe_id;

-- +goose StatementBegin
CREATE FUNCTION agenteam_meeting.guard_summary_generation_snapshot() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ROW(NEW.id,NEW.meeting_id,NEW.turn_id,NEW.expected_summary_version,NEW.input_data,NEW.input_digest,NEW.initial,NEW.created_at)
 IS DISTINCT FROM ROW(OLD.id,OLD.meeting_id,OLD.turn_id,OLD.expected_summary_version,OLD.input_data,OLD.input_digest,OLD.initial,OLD.created_at)
 OR (OLD.snapshot_id IS NOT NULL AND ROW(NEW.snapshot_id,NEW.retained_at) IS DISTINCT FROM ROW(OLD.snapshot_id,OLD.retained_at))
 OR (OLD.released_at IS NOT NULL AND ROW(NEW.released_at,NEW.retention_version) IS DISTINCT FROM ROW(OLD.released_at,OLD.retention_version)) THEN
  RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='immutable meeting summary generation snapshot';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER meeting_summary_generation_snapshot BEFORE UPDATE ON agenteam_meeting.summary_generations FOR EACH ROW EXECUTE FUNCTION agenteam_meeting.guard_summary_generation_snapshot();

-- +goose StatementBegin
CREATE FUNCTION agenteam_meeting.guard_summary_prepared_process() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (TG_OP='INSERT' AND NEW.prepared_process_id IS NULL)
 OR (TG_OP='UPDATE' AND NEW.prepared_process_id IS DISTINCT FROM OLD.prepared_process_id) THEN
  RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='immutable meeting summary preparation process';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER meeting_summary_prepared_process BEFORE INSERT OR UPDATE ON agenteam_meeting.summary_model_calls FOR EACH ROW EXECUTE FUNCTION agenteam_meeting.guard_summary_prepared_process();
