-- agenteam:transaction tx
-- +goose Up
-- Explicit Agent ownership only. Business commands and Project work claims
-- do not represent individual Agent reads or original mutation/confirmation.
CREATE TABLE agenteam_agent.project_calls (
 id agenteam_agent.safe_id PRIMARY KEY,
 project_id agenteam_agent.safe_id NOT NULL REFERENCES agenteam_project.projects(id) ON DELETE RESTRICT,
 process_id agenteam_agent.safe_id NOT NULL,
 instance_id agenteam_agent.safe_id NOT NULL,
 lane text NOT NULL CHECK(lane IN ('service','directory')),
 class text NOT NULL CHECK(class IN ('mutation','read')),
 phase text NOT NULL CHECK(phase IN ('running','joined')),
 registered_at timestamptz(6) NOT NULL,
 joined_at timestamptz(6),
 CONSTRAINT project_calls_directory_read CHECK(lane<>'directory' OR class='read'),
 CONSTRAINT project_calls_phase_time CHECK(
  (phase='running' AND joined_at IS NULL)
  OR (phase='joined' AND joined_at IS NOT NULL AND joined_at>=registered_at)
 )
);
CREATE INDEX project_calls_running ON agenteam_agent.project_calls(project_id,class,id) WHERE phase='running';

-- +goose StatementBegin
CREATE FUNCTION agenteam_agent.guard_project_call() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='project_calls_retained', MESSAGE='agent call history is retained';
 END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.phase<>'running' OR NEW.joined_at IS NOT NULL THEN
   RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='project_calls_initial', MESSAGE='agent call must start running';
  END IF;
  RETURN NEW;
 END IF;
 IF ROW(NEW.id,NEW.project_id,NEW.process_id,NEW.instance_id,NEW.lane,NEW.class,NEW.registered_at)
 IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.process_id,OLD.instance_id,OLD.lane,OLD.class,OLD.registered_at)
 OR OLD.phase<>'running' OR NEW.phase<>'joined' THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='project_calls_monotonic', MESSAGE='immutable agent call ownership';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER project_calls_guard BEFORE INSERT OR UPDATE OR DELETE ON agenteam_agent.project_calls FOR EACH ROW EXECUTE FUNCTION agenteam_agent.guard_project_call();
