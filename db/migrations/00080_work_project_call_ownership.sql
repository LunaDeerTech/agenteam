-- agenteam:transaction tx
-- +goose Up
-- Only the explicitly owned five Work services register these whole calls.
-- A business_closed row is a database fact, not proof of its Store return.
CREATE TABLE agenteam_work.project_calls (
 id agenteam_work.safe_id PRIMARY KEY,
 project_id agenteam_work.safe_id NOT NULL REFERENCES agenteam_project.projects(id) ON DELETE RESTRICT,
 process_id agenteam_work.safe_id NOT NULL REFERENCES agenteam_object.process_claims(process_id) ON DELETE RESTRICT,
 instance_id agenteam_work.safe_id NOT NULL,
 lane text NOT NULL CHECK(lane IN ('structure','task','blocker','transition','sprint')),
 class text NOT NULL CHECK(class IN ('mutation','read')),
 phase text NOT NULL CHECK(phase IN ('running','business_closed')),
 registered_at timestamptz(6) NOT NULL,
 closed_at timestamptz(6),
 CONSTRAINT project_calls_phase_time CHECK(
  (phase='running' AND closed_at IS NULL)
  OR (phase='business_closed' AND closed_at IS NOT NULL AND closed_at>=registered_at)
 )
);
-- Stop observes closed as well as running rows: an unknown instance cannot
-- infer an original return from phase alone.
CREATE INDEX project_calls_target ON agenteam_work.project_calls(project_id,class,id);

-- +goose StatementBegin
CREATE FUNCTION agenteam_work.guard_project_call() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='project_calls_retained', MESSAGE='work call history is retained';
 END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.phase<>'running' OR NEW.closed_at IS NOT NULL THEN
   RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='project_calls_initial', MESSAGE='work call must start running';
  END IF;
  RETURN NEW;
 END IF;
 IF ROW(NEW.id,NEW.project_id,NEW.process_id,NEW.instance_id,NEW.lane,NEW.class,NEW.registered_at)
 IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.process_id,OLD.instance_id,OLD.lane,OLD.class,OLD.registered_at)
 OR OLD.phase<>'running' OR NEW.phase<>'business_closed' THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='project_calls_monotonic', MESSAGE='immutable work call ownership';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER project_calls_guard BEFORE INSERT OR UPDATE OR DELETE ON agenteam_work.project_calls FOR EACH ROW EXECUTE FUNCTION agenteam_work.guard_project_call();
