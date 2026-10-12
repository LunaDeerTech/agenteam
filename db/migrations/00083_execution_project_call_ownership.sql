-- agenteam:transaction tx
-- +goose Up
-- Execution business identifiers use an older text domain. Whole-call
-- ownership uses UUIDs so its Project and Process foreign keys retain the
-- canonical owners' actual types.
CREATE DOMAIN agenteam_execution.ownership_id AS uuid CHECK
 (VALUE::text ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$');

CREATE TABLE agenteam_execution.project_calls (
 id agenteam_execution.ownership_id PRIMARY KEY,
 project_id agenteam_execution.ownership_id NOT NULL REFERENCES agenteam_project.projects(id) ON DELETE RESTRICT,
 process_id agenteam_execution.ownership_id NOT NULL REFERENCES agenteam_object.process_claims(process_id) ON DELETE RESTRICT,
 instance_id agenteam_execution.ownership_id NOT NULL,
 lane text NOT NULL CHECK(lane IN ('service','preparation','direct','associated')),
 invocation_identity text COLLATE "C" NOT NULL CHECK(octet_length(invocation_identity) BETWEEN 1 AND 8192),
 invocation_digest text NOT NULL CHECK(invocation_digest ~ '^sha256:[0-9a-f]{64}$'),
 phase text NOT NULL CHECK(phase IN ('running','business_closed')),
 registered_at timestamptz(6) NOT NULL,
 closed_at timestamptz(6),
 CONSTRAINT project_calls_phase_time CHECK(
  (phase='running' AND closed_at IS NULL)
  OR (phase='business_closed' AND closed_at IS NOT NULL AND closed_at>=registered_at)
 )
);
-- Closed rows remain relevant: their phase cannot prove that the original
-- Store call, outer owner or retained confirmation has actually returned.
CREATE INDEX project_calls_target ON agenteam_execution.project_calls(project_id,id);

-- +goose StatementBegin
CREATE FUNCTION agenteam_execution.guard_project_call() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='project_calls_retained', MESSAGE='execution call history is retained';
 END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.phase<>'running' OR NEW.closed_at IS NOT NULL THEN
   RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='project_calls_initial', MESSAGE='execution call must start running';
  END IF;
  RETURN NEW;
 END IF;
 IF ROW(NEW.id,NEW.project_id,NEW.process_id,NEW.instance_id,NEW.lane,NEW.invocation_identity,NEW.invocation_digest,NEW.registered_at)
 IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.process_id,OLD.instance_id,OLD.lane,OLD.invocation_identity,OLD.invocation_digest,OLD.registered_at)
 OR OLD.phase<>'running' OR NEW.phase<>'business_closed' THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='project_calls_monotonic', MESSAGE='immutable execution call ownership';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER project_calls_guard BEFORE INSERT OR UPDATE OR DELETE ON agenteam_execution.project_calls FOR EACH ROW EXECUTE FUNCTION agenteam_execution.guard_project_call();
