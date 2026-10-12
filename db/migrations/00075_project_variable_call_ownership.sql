-- agenteam:transaction tx
-- +goose Up
-- Explicit ordinary/Secret Variable project-business call ownership.
-- Closed history is retained; this does not retire Secret technical work or references.
CREATE TABLE agenteam_projectvariable.project_calls (
 id agenteam_projectvariable.safe_id PRIMARY KEY,
 project_id agenteam_projectvariable.safe_id NOT NULL REFERENCES agenteam_project.projects(id) ON DELETE RESTRICT,
 process_id agenteam_projectvariable.safe_id NOT NULL REFERENCES agenteam_object.process_claims(process_id) ON DELETE RESTRICT,
 instance_id agenteam_projectvariable.safe_id NOT NULL,
 lane text NOT NULL CHECK(lane IN ('ordinary','secret')),
 class text NOT NULL CHECK(class IN ('mutation','read')),
 phase text NOT NULL CHECK(phase IN ('running','business_closed')),
 registered_at timestamptz(6) NOT NULL,
 closed_at timestamptz(6),
 CONSTRAINT project_calls_phase_time CHECK(
  (phase='running' AND closed_at IS NULL)
  OR (phase='business_closed' AND closed_at IS NOT NULL AND closed_at>=registered_at)
 )
);
CREATE INDEX project_calls_running ON agenteam_projectvariable.project_calls(project_id,class,id) WHERE phase='running';

-- +goose StatementBegin
CREATE FUNCTION agenteam_projectvariable.guard_project_call() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='project_calls_retained', MESSAGE='project variable call history is retained';
 END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.phase<>'running' OR NEW.closed_at IS NOT NULL THEN
   RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='project_calls_initial', MESSAGE='project variable call must start running';
  END IF;
  RETURN NEW;
 END IF;
 IF ROW(NEW.id,NEW.project_id,NEW.process_id,NEW.instance_id,NEW.lane,NEW.class,NEW.registered_at)
 IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.process_id,OLD.instance_id,OLD.lane,OLD.class,OLD.registered_at)
 OR OLD.phase<>'running' OR NEW.phase<>'business_closed' THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='project_calls_monotonic', MESSAGE='immutable project variable call ownership';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER project_calls_guard BEFORE INSERT OR UPDATE OR DELETE ON agenteam_projectvariable.project_calls FOR EACH ROW EXECUTE FUNCTION agenteam_projectvariable.guard_project_call();
