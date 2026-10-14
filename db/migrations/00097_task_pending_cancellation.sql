-- agenteam:transaction tx
-- +goose Up
-- The canonical Work transition proves its same-Tx cancelled postimage before
-- Scheduler writes this original-Dispatch request. Unknown keeps its outcome.
CREATE TABLE agenteam_scheduler.task_cancellation_requests (
 project_id agenteam_scheduler.safe_id NOT NULL,
 dispatch_id agenteam_scheduler.safe_id NOT NULL,
 task_id agenteam_scheduler.safe_id NOT NULL,
 task_transition_id agenteam_scheduler.safe_id NOT NULL,
 requested_at timestamptz(6) NOT NULL,
 PRIMARY KEY(project_id,dispatch_id),
 FOREIGN KEY(project_id,dispatch_id) REFERENCES agenteam_scheduler.dispatches(project_id,id) ON DELETE CASCADE
);
CREATE INDEX task_cancellation_task ON agenteam_scheduler.task_cancellation_requests(project_id,task_id,dispatch_id);
-- +goose StatementBegin
CREATE FUNCTION agenteam_scheduler.guard_task_cancellation_request() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP<>'INSERT' OR NOT EXISTS(SELECT 1 FROM agenteam_scheduler.dispatches d
  WHERE d.project_id=NEW.project_id AND d.id=NEW.dispatch_id AND d.task_id=NEW.task_id AND d.status='pending') THEN
  RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='task_cancellation_request_immutable',MESSAGE='invalid task cancellation request';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER task_cancellation_request_immutable BEFORE INSERT OR UPDATE ON agenteam_scheduler.task_cancellation_requests
 FOR EACH ROW EXECUTE FUNCTION agenteam_scheduler.guard_task_cancellation_request();

ALTER TABLE agenteam_scheduler.dispatches DROP CONSTRAINT dispatches_status_check;
ALTER TABLE agenteam_scheduler.dispatches ADD CONSTRAINT dispatches_status_check
 CHECK(status IN ('pending','launched','failed','skipped','cancelled'));
-- A cancelled row is byte-for-byte the already-validated pending row, except
-- status/version/update time. Its retry timestamp is preserved as a diagnostic;
-- the terminal status prevents another send. Keep all original arms unchanged.
-- +goose StatementBegin
DO $$
DECLARE constraint_name text; old_expr text;
BEGIN
 FOREACH constraint_name IN ARRAY ARRAY['dispatch_result_shape','dispatch_busy_shape','dispatch_final_failure_shape','dispatch_temporary_shape'] LOOP
  SELECT pg_get_expr(conbin,conrelid) INTO STRICT old_expr FROM pg_constraint
   WHERE conrelid='agenteam_scheduler.dispatches'::regclass AND conname=constraint_name AND contype='c';
  EXECUTE format('ALTER TABLE agenteam_scheduler.dispatches DROP CONSTRAINT %I',constraint_name);
  EXECUTE format('ALTER TABLE agenteam_scheduler.dispatches ADD CONSTRAINT %I CHECK((status=''cancelled'' OR (%s)) IS TRUE)',constraint_name,old_expr);
 END LOOP;
END;
$$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION agenteam_scheduler.guard_cancelled_dispatch() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status='cancelled' THEN
  IF TG_OP='INSERT' THEN
   RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='dispatch_cancellation_proven',MESSAGE='invalid cancelled dispatch insert';
  END IF;
  IF OLD.status<>'pending' OR OLD.launch_outcome NOT IN ('not_sent','known_not_created')
   OR NEW.version<>OLD.version+1 OR NEW.updated_at<=OLD.updated_at
   OR (to_jsonb(NEW)-ARRAY['status','version','updated_at']) IS DISTINCT FROM (to_jsonb(OLD)-ARRAY['status','version','updated_at'])
   OR NOT EXISTS(SELECT 1 FROM agenteam_scheduler.task_cancellation_requests c
      WHERE c.project_id=NEW.project_id AND c.dispatch_id=NEW.id AND c.task_id=NEW.task_id) THEN
   RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='dispatch_cancellation_proven',MESSAGE='invalid cancelled dispatch transition';
  END IF;
 END IF;
 -- A request never permits another send, including after process replacement.
 -- Canonical Lookup may still record or associate the original Unknown attempt.
 IF TG_OP='UPDATE' AND NEW.attempt_count>OLD.attempt_count AND EXISTS(
  SELECT 1 FROM agenteam_scheduler.task_cancellation_requests c WHERE c.project_id=NEW.project_id AND c.dispatch_id=NEW.id) THEN
  RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='dispatch_cancellation_no_resend',MESSAGE='cancelled task cannot send';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER dispatch_cancellation_proven BEFORE INSERT OR UPDATE ON agenteam_scheduler.dispatches
 FOR EACH ROW EXECUTE FUNCTION agenteam_scheduler.guard_cancelled_dispatch();
