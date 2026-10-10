-- agenteam:transaction tx
-- +goose Up
-- No historical known-not-created row is inferred to mean AgentBusy. Only
-- the real original Launch result may record this exact attempted outcome.
ALTER TABLE agenteam_scheduler.dispatches
 ADD COLUMN busy_attempt bigint,
 ADD COLUMN skip_reason text,
 ADD COLUMN skipped_at timestamptz(6);
ALTER TABLE agenteam_scheduler.dispatches ADD CONSTRAINT dispatch_busy_shape CHECK((
 (busy_attempt IS NULL AND skip_reason IS NULL AND skipped_at IS NULL)
 OR (busy_attempt IS NOT NULL AND busy_attempt>0 AND busy_attempt=attempt_count
  AND launch_outcome='known_not_created' AND execution_id IS NULL AND next_retry_at IS NULL
  AND ((status='pending' AND skip_reason IS NULL AND skipped_at IS NULL)
   OR (status='skipped' AND skip_reason='agent_busy' AND skipped_at IS NOT NULL AND skipped_at=updated_at)))
) IS TRUE);
-- +goose StatementBegin
CREATE FUNCTION agenteam_scheduler.guard_dispatch_busy() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.busy_attempt IS NOT NULL OR NEW.skip_reason IS NOT NULL OR NEW.skipped_at IS NOT NULL THEN
   RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='dispatch_busy_immutable',MESSAGE='invalid busy fact';
  END IF;
  RETURN NEW;
 END IF;
 IF OLD.busy_attempt IS NULL AND NEW.busy_attempt IS NOT NULL THEN
  IF ((OLD.status='pending' AND OLD.launch_outcome='unknown' AND OLD.attempt_count>0
   AND NEW.status='pending' AND NEW.launch_outcome='known_not_created'
   AND NEW.attempt_count=OLD.attempt_count AND NEW.busy_attempt=OLD.attempt_count
   AND NEW.skip_reason IS NULL AND NEW.skipped_at IS NULL) IS TRUE)=false THEN
   RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='dispatch_busy_immutable',MESSAGE='invalid busy fact';
  END IF;
 ELSIF OLD.busy_attempt IS NOT NULL AND NEW.busy_attempt IS DISTINCT FROM OLD.busy_attempt THEN
  RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='dispatch_busy_immutable',MESSAGE='immutable busy attempt';
 END IF;
 IF (NEW.status='skipped' AND OLD.status='pending') OR NEW.skip_reason IS DISTINCT FROM OLD.skip_reason OR NEW.skipped_at IS DISTINCT FROM OLD.skipped_at THEN
  IF ((OLD.status='pending' AND OLD.launch_outcome='known_not_created' AND OLD.busy_attempt IS NOT NULL
   AND NEW.status='skipped' AND NEW.launch_outcome='known_not_created'
   AND NEW.busy_attempt=OLD.busy_attempt AND NEW.skip_reason='agent_busy'
   AND NEW.skipped_at IS NOT NULL AND NEW.skipped_at=NEW.updated_at) IS TRUE)=false THEN
   RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='dispatch_busy_immutable',MESSAGE='invalid busy settlement';
  END IF;
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER dispatch_busy_immutable BEFORE INSERT OR UPDATE ON agenteam_scheduler.dispatches
 FOR EACH ROW EXECUTE FUNCTION agenteam_scheduler.guard_dispatch_busy();

-- One immutable Work result per original claim, including exact preservation.
-- The Dispatch skipped write shares this transaction; no cross-owner FK cycle.
CREATE TABLE agenteam_work.task_busy_compensations (
 id agenteam_work.safe_id PRIMARY KEY,
 project_id agenteam_work.safe_id NOT NULL,
 task_id agenteam_work.safe_id NOT NULL,
 agent_id agenteam_work.safe_id NOT NULL,
 request_id agenteam_work.safe_id NOT NULL,
 dispatch_version bigint NOT NULL CHECK(dispatch_version>0),
 launch_attempt bigint NOT NULL CHECK(launch_attempt>0),
 restored boolean NOT NULL,
 before_version bigint NOT NULL CHECK(before_version>0),
 after_version bigint NOT NULL CHECK(after_version>0),
 task_event_id agenteam_work.safe_id UNIQUE,
 event_id agenteam_work.safe_id UNIQUE,
 record jsonb NOT NULL,
 created_at timestamptz(6) NOT NULL,
 UNIQUE(project_id,id),
 FOREIGN KEY(project_id,id) REFERENCES agenteam_work.task_scheduler_claims(project_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(project_id,task_id) REFERENCES agenteam_work.tasks(project_id,id) ON DELETE RESTRICT,
 CHECK((jsonb_typeof(record)='object' AND octet_length(record::text)<=4194304
  AND record ?& ARRAY['request','claim','before','after','sprint','current_sprint_id','restored','groups','query_generation','history','event','header','created_at']
  AND record-ARRAY['request','claim','before','after','sprint','current_sprint_id','restored','groups','query_generation','history','event','header','created_at']='{}'::jsonb
  AND record->'request'->'Claim'->>'DispatchID'=id::text
  AND record->'request'->'Claim'->>'ProjectID'=project_id::text
  AND record->'request'->'Claim'->>'TaskID'=task_id::text
  AND record->'request'->'Claim'->>'AgentID'=agent_id::text
  AND record->'request'->'Claim'->>'RequestID'=request_id::text
  AND record->'request'->'Claim'->>'Purpose'='task/work'
  AND record->'request'->>'DispatchVersion'=dispatch_version::text
  AND record->'request'->>'LaunchAttempt'=launch_attempt::text
  AND record->>'restored'=restored::text
  AND record->'before'->>'id'=task_id::text AND record->'after'->>'id'=task_id::text
  AND record->'before'->>'project_id'=project_id::text AND record->'after'->>'project_id'=project_id::text
  AND record->'before'->>'version'=before_version::text AND record->'after'->>'version'=after_version::text
  AND ((restored AND before_version<9223372036854775807 AND after_version=before_version+1
   AND task_event_id IS NOT NULL AND event_id IS NOT NULL
   AND record->'before'->>'state'='in_progress' AND record->'after'->>'state'='todo'
   AND record->'history'->>'id'=task_event_id::text AND record->'header'->>'event_id'=event_id::text)
  OR (NOT restored AND after_version=before_version AND task_event_id IS NULL AND event_id IS NULL
   AND record->'before'=record->'after' AND record->'history'='null'::jsonb
   AND record->'event'='null'::jsonb AND record->'header'='null'::jsonb
   AND record->'groups'='[]'::jsonb AND record->>'query_generation'='0'))
 ) IS TRUE)
);
-- +goose StatementBegin
CREATE FUNCTION agenteam_work.reject_busy_compensation_rewrite() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='task_busy_compensations_immutable',MESSAGE='immutable task busy compensation';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER task_busy_compensations_immutable BEFORE UPDATE ON agenteam_work.task_busy_compensations
 FOR EACH ROW EXECUTE FUNCTION agenteam_work.reject_busy_compensation_rewrite();
ALTER TABLE agenteam_work.task_events ADD COLUMN compensation_operation_id agenteam_work.safe_id;
ALTER TABLE agenteam_work.task_events ADD CONSTRAINT task_events_compensation_operation_fk
 FOREIGN KEY(project_id,compensation_operation_id) REFERENCES agenteam_work.task_busy_compensations(project_id,id) ON DELETE RESTRICT;
ALTER TABLE agenteam_work.task_events DROP CONSTRAINT task_events_operation_kind_check;
ALTER TABLE agenteam_work.task_events ADD CONSTRAINT task_events_operation_kind_check CHECK((
 (type IN ('task_created','fields_updated') AND operation_id IS NOT NULL AND blocker_operation_id IS NULL AND transition_operation_id IS NULL AND claim_operation_id IS NULL AND compensation_operation_id IS NULL AND correlation_id=operation_id)
 OR (type IN ('blocker_added','blocker_resolved') AND operation_id IS NULL AND blocker_operation_id IS NOT NULL AND transition_operation_id IS NULL AND claim_operation_id IS NULL AND compensation_operation_id IS NULL AND correlation_id=blocker_operation_id)
 OR (type IN ('state_changed','assignee_changed','blocker_added','blocker_resolved','comment') AND operation_id IS NULL AND blocker_operation_id IS NULL AND transition_operation_id IS NOT NULL AND claim_operation_id IS NULL AND compensation_operation_id IS NULL AND correlation_id=transition_operation_id)
 OR (type='state_changed' AND operation_id IS NULL AND blocker_operation_id IS NULL AND transition_operation_id IS NULL AND claim_operation_id IS NOT NULL AND compensation_operation_id IS NULL AND correlation_id=claim_operation_id)
 OR (type='state_changed' AND operation_id IS NULL AND blocker_operation_id IS NULL AND transition_operation_id IS NULL AND claim_operation_id IS NULL AND compensation_operation_id IS NOT NULL AND correlation_id=compensation_operation_id)
) IS TRUE);
-- Keep all existing Human and Claim arms byte-for-byte inside the old predicate.
-- +goose StatementBegin
DO $$
DECLARE old_expr text;
BEGIN
 SELECT pg_get_expr(conbin,conrelid) INTO STRICT old_expr FROM pg_constraint
 WHERE conrelid='agenteam_work.task_events'::regclass AND conname='task_events_actor_source' AND contype='c';
 ALTER TABLE agenteam_work.task_events DROP CONSTRAINT task_events_actor_source;
 EXECUTE format($check$ALTER TABLE agenteam_work.task_events ADD CONSTRAINT task_events_actor_source CHECK((
 (compensation_operation_id IS NULL AND (%s)) OR
 (compensation_operation_id IS NOT NULL AND jsonb_typeof(actor)='object' AND octet_length(actor::text)<=1024
 AND actor ?& ARRAY['type','service_name','cause_id','source']
 AND actor-ARRAY['type','service_name','cause_id','source']='{}'::jsonb
 AND actor->>'type'='system' AND actor->>'service_name'='scheduler' AND actor->>'source'='scheduler'
 AND actor->>'cause_id'=compensation_operation_id::text
 AND payload=jsonb_build_object('from_state','in_progress','to_state','todo','reason_code','scheduler_agent_busy_compensation'))
 ) IS TRUE)$check$,old_expr);
END;
$$;
-- +goose StatementEnd
CREATE INDEX task_events_compensation_operation ON agenteam_work.task_events(project_id,compensation_operation_id,id) WHERE compensation_operation_id IS NOT NULL;
