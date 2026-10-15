-- agenteam:transaction tx
-- +goose Up
-- Capture the original request before Resolve begins. Historical NULL values
-- deliberately remain unknown; the current selector is not historical proof.
ALTER TABLE agenteam_meeting.summary_model_calls
 ADD COLUMN resolve_source_data bytea,
 ADD COLUMN resolve_binding text,
 ADD CONSTRAINT meeting_summary_original_resolve_pair CHECK(
  (resolve_source_data IS NULL AND resolve_binding IS NULL)
  OR (resolve_source_data IS NOT NULL AND octet_length(resolve_source_data) BETWEEN 2 AND 8192
      AND resolve_binding IS NOT NULL AND resolve_binding ~ '^sha256:[0-9a-f]{64}$')
 );

-- +goose StatementBegin
CREATE FUNCTION agenteam_meeting.guard_summary_original_resolve() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (TG_OP='INSERT' AND (NEW.resolve_source_data IS NULL OR NEW.resolve_binding IS NULL))
 OR (TG_OP='UPDATE' AND ROW(NEW.resolve_source_data,NEW.resolve_binding)
     IS DISTINCT FROM ROW(OLD.resolve_source_data,OLD.resolve_binding)) THEN
  RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='immutable meeting summary original resolve';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER meeting_summary_original_resolve BEFORE INSERT OR UPDATE ON agenteam_meeting.summary_model_calls FOR EACH ROW EXECUTE FUNCTION agenteam_meeting.guard_summary_original_resolve();
