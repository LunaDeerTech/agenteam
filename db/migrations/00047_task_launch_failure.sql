-- agenteam:transaction tx
-- +goose Up
-- Only a new original synchronous rejection can establish these facts. Old
-- known-not-created rows retain NULL markers and acquire no finality.
ALTER TABLE agenteam_scheduler.dispatches
 ADD COLUMN final_attempt bigint,
 ADD COLUMN failure_reason text,
 ADD COLUMN failure_code text,
 ADD COLUMN failure_occurred_at timestamptz(6),
 ADD COLUMN failed_at timestamptz(6);
ALTER TABLE agenteam_scheduler.dispatches ADD CONSTRAINT dispatch_final_failure_shape CHECK((
 (final_attempt IS NULL AND failure_reason IS NULL AND failure_code IS NULL AND failure_occurred_at IS NULL AND failed_at IS NULL)
 OR (final_attempt IS NOT NULL AND final_attempt>0 AND final_attempt=attempt_count
 AND failure_reason='unsupported_resource_constraints_v1' AND failure_code='DEPENDENCY_UNBOUND' AND failure_occurred_at IS NOT NULL
 AND launch_outcome='known_not_created' AND execution_id IS NULL AND next_retry_at IS NULL AND claim_guard IS NOT NULL
 AND busy_attempt IS NULL AND skip_reason IS NULL AND skipped_at IS NULL
 AND ((status='pending' AND failed_at IS NULL) OR (status='failed' AND failed_at IS NOT NULL AND failed_at=updated_at AND failed_at>=failure_occurred_at)))
) IS TRUE);
-- +goose StatementBegin
CREATE FUNCTION agenteam_scheduler.guard_dispatch_final_failure() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.final_attempt IS NOT NULL OR NEW.failure_reason IS NOT NULL OR NEW.failure_code IS NOT NULL OR NEW.failure_occurred_at IS NOT NULL OR NEW.failed_at IS NOT NULL THEN
   RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='dispatch_final_failure_immutable',MESSAGE='invalid final failure fact';
  END IF;
  RETURN NEW;
 END IF;
 IF OLD.final_attempt IS NULL AND NEW.final_attempt IS NOT NULL THEN
  IF (OLD.status='pending' AND OLD.launch_outcome='unknown' AND OLD.attempt_count>0
   AND NEW.status='pending' AND NEW.launch_outcome='known_not_created'
   AND NEW.attempt_count=OLD.attempt_count AND NEW.final_attempt=OLD.attempt_count
   AND NEW.failure_occurred_at=NEW.updated_at AND NEW.failed_at IS NULL) IS NOT TRUE THEN
   RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='dispatch_final_failure_immutable',MESSAGE='invalid final failure fact';
  END IF;
 ELSIF OLD.final_attempt IS NOT NULL AND ROW(NEW.final_attempt,NEW.failure_reason,NEW.failure_code,NEW.failure_occurred_at)
  IS DISTINCT FROM ROW(OLD.final_attempt,OLD.failure_reason,OLD.failure_code,OLD.failure_occurred_at) THEN
  RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='dispatch_final_failure_immutable',MESSAGE='immutable final failure fact';
 END IF;
 IF (NEW.status='failed' AND OLD.status='pending') OR NEW.failed_at IS DISTINCT FROM OLD.failed_at THEN
  IF (OLD.status='pending' AND OLD.launch_outcome='known_not_created' AND OLD.final_attempt IS NOT NULL
   AND NEW.status='failed' AND NEW.launch_outcome='known_not_created' AND NEW.final_attempt=OLD.final_attempt
   AND NEW.failed_at IS NOT NULL AND NEW.failed_at=NEW.updated_at) IS NOT TRUE THEN
   RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='dispatch_final_failure_immutable',MESSAGE='invalid final failure settlement';
  END IF;
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER dispatch_final_failure_immutable BEFORE INSERT OR UPDATE ON agenteam_scheduler.dispatches
 FOR EACH ROW EXECUTE FUNCTION agenteam_scheduler.guard_dispatch_final_failure();

CREATE TABLE agenteam_work.task_launch_failures (
 id agenteam_work.safe_id PRIMARY KEY,
 project_id agenteam_work.safe_id NOT NULL,
 task_id agenteam_work.safe_id NOT NULL,
 agent_id agenteam_work.safe_id NOT NULL,
 request_id agenteam_work.safe_id NOT NULL,
 dispatch_version bigint NOT NULL CHECK(dispatch_version>0),
 launch_attempt bigint NOT NULL CHECK(launch_attempt>0),
 reason text NOT NULL CHECK(reason='unsupported_resource_constraints_v1'),
 failure_occurred_at timestamptz(6) NOT NULL,
 changed boolean NOT NULL,
 before_version bigint NOT NULL CHECK(before_version>0),
 after_version bigint NOT NULL CHECK(after_version>0),
 blocker_id agenteam_work.safe_id UNIQUE,
 event_id agenteam_work.safe_id UNIQUE,
 record jsonb NOT NULL,
 created_at timestamptz(6) NOT NULL CHECK(created_at>=failure_occurred_at),
 UNIQUE(project_id,id),
 UNIQUE(project_id,id,blocker_id),
 FOREIGN KEY(project_id,id) REFERENCES agenteam_work.task_scheduler_claims(project_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(project_id,task_id) REFERENCES agenteam_work.tasks(project_id,id) ON DELETE RESTRICT,
 CHECK((jsonb_typeof(record)='object' AND octet_length(record::text)<=4194304
 AND record ?& ARRAY['request','facts','claim','before','after','sprint','current_sprint_id','changed','groups','query_generation','blocker','history','event','header','created_at']
 AND record-ARRAY['request','facts','claim','before','after','sprint','current_sprint_id','changed','groups','query_generation','blocker','history','event','header','created_at']='{}'::jsonb
 AND record->'request'->'Claim'->>'DispatchID'=id::text AND record->'request'->'Claim'->>'ProjectID'=project_id::text
 AND record->'request'->'Claim'->>'TaskID'=task_id::text AND record->'request'->'Claim'->>'AgentID'=agent_id::text
 AND record->'request'->'Claim'->>'RequestID'=request_id::text AND record->'request'->'Claim'->>'Purpose'='task/work'
 AND record->'request'->>'DispatchVersion'=dispatch_version::text AND record->'request'->>'LaunchAttempt'=launch_attempt::text
 AND record->'facts'->>'Reason'=reason AND record->>'changed'=changed::text
 AND record->'before'->>'id'=task_id::text AND record->'after'->>'id'=task_id::text
 AND record->'before'->>'project_id'=project_id::text AND record->'after'->>'project_id'=project_id::text
 AND record->'before'->>'version'=before_version::text AND record->'after'->>'version'=after_version::text
 AND ((changed AND before_version<9223372036854775807 AND after_version=before_version+1
  AND blocker_id IS NOT NULL AND event_id IS NOT NULL AND record->'blocker'->>'id'=blocker_id::text AND record->'header'->>'event_id'=event_id::text
  AND record->'after'->>'state'='blocked' AND jsonb_typeof(record->'history')='array'
  AND ((record->'before'->>'state'='blocked' AND jsonb_array_length(record->'history')=1 AND record->'groups'='[]'::jsonb)
   OR (record->'before'->>'state'='in_progress' AND jsonb_array_length(record->'history')=2 AND jsonb_array_length(record->'groups')=2)))
 OR (NOT changed AND before_version=after_version AND blocker_id IS NULL AND event_id IS NULL
  AND record->'before'=record->'after' AND record->'blocker'='null'::jsonb AND record->'history'='[]'::jsonb
  AND record->'event'='null'::jsonb AND record->'header'='null'::jsonb AND record->'groups'='[]'::jsonb AND record->>'query_generation'='0'))
 ) IS TRUE)
);
-- +goose StatementBegin
CREATE FUNCTION agenteam_work.reject_task_launch_failure_rewrite() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='task_launch_failures_immutable',MESSAGE='immutable task launch failure';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER task_launch_failures_immutable BEFORE UPDATE ON agenteam_work.task_launch_failures FOR EACH ROW EXECUTE FUNCTION agenteam_work.reject_task_launch_failure_rewrite();

ALTER TABLE agenteam_work.task_blockers ALTER COLUMN created_operation_id DROP NOT NULL;
ALTER TABLE agenteam_work.task_blockers ADD COLUMN failure_operation_id agenteam_work.safe_id;
ALTER TABLE agenteam_work.task_blockers ADD CONSTRAINT task_blockers_failure_operation_fk
 FOREIGN KEY(project_id,failure_operation_id,id) REFERENCES agenteam_work.task_launch_failures(project_id,id,blocker_id) ON DELETE RESTRICT;
ALTER TABLE agenteam_work.task_blockers DROP CONSTRAINT task_blockers_type_check;
ALTER TABLE agenteam_work.task_blockers ADD CONSTRAINT task_blockers_type_check CHECK(type IN ('rely_on','waiting_for_human','technical'));
ALTER TABLE agenteam_work.task_blockers ADD CONSTRAINT task_blockers_creation_source CHECK((
 (type IN ('rely_on','waiting_for_human') AND created_operation_id IS NOT NULL AND failure_operation_id IS NULL)
 OR (type='technical' AND created_operation_id IS NULL AND failure_operation_id IS NOT NULL
 AND resolved_at IS NULL AND resolved_by IS NULL AND resolution_comment IS NULL AND resolved_operation_id IS NULL)) IS TRUE);
-- Preserve the exact old metadata/actor predicates. The new read branch does
-- not turn the Human create command into a technical writer.
-- +goose StatementBegin
DO $$
DECLARE old_expr text;
BEGIN
 SELECT pg_get_expr(conbin,conrelid) INTO STRICT old_expr FROM pg_constraint WHERE conrelid='agenteam_work.task_blockers'::regclass AND conname='task_blockers_metadata_check' AND contype='c';
 ALTER TABLE agenteam_work.task_blockers DROP CONSTRAINT task_blockers_metadata_check;
 EXECUTE format($check$ALTER TABLE agenteam_work.task_blockers ADD CONSTRAINT task_blockers_metadata_check CHECK(((type<>'technical' AND (%s)) OR
 (type='technical' AND metadata=jsonb_build_object('code','scheduler_launch_failed','source','scheduler_dispatch','reference_id',failure_operation_id::text))) IS TRUE)$check$,old_expr);
 SELECT pg_get_expr(conbin,conrelid) INTO STRICT old_expr FROM pg_constraint WHERE conrelid='agenteam_work.task_blockers'::regclass AND conname='task_blockers_created_actor_check' AND contype='c';
 ALTER TABLE agenteam_work.task_blockers DROP CONSTRAINT task_blockers_created_actor_check;
 EXECUTE format($check$ALTER TABLE agenteam_work.task_blockers ADD CONSTRAINT task_blockers_created_actor_check CHECK(((type<>'technical' AND (%s)) OR
 (type='technical' AND created_by=jsonb_build_object('type','system','service_name','scheduler','source','scheduler','cause_id',failure_operation_id::text))) IS TRUE)$check$,old_expr);
END;
$$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_work.reject_blocker_rewrite() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ROW(NEW.id,NEW.project_id,NEW.task_id,NEW.type,NEW.description,NEW.metadata,NEW.created_at,NEW.created_by,NEW.created_operation_id,NEW.failure_operation_id)
 IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.task_id,OLD.type,OLD.description,OLD.metadata,OLD.created_at,OLD.created_by,OLD.created_operation_id,OLD.failure_operation_id)
 OR OLD.resolved_at IS NOT NULL OR NEW.resolved_at IS NULL THEN
  RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='task_blockers_immutable',MESSAGE='immutable blocker';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
ALTER TABLE agenteam_work.task_events ADD COLUMN failure_operation_id agenteam_work.safe_id;
ALTER TABLE agenteam_work.task_events ADD CONSTRAINT task_events_failure_operation_fk
 FOREIGN KEY(project_id,failure_operation_id) REFERENCES agenteam_work.task_launch_failures(project_id,id) ON DELETE RESTRICT;
-- +goose StatementBegin
DO $$
DECLARE old_expr text;
BEGIN
 SELECT pg_get_expr(conbin,conrelid) INTO STRICT old_expr FROM pg_constraint WHERE conrelid='agenteam_work.task_events'::regclass AND conname='task_events_operation_kind_check' AND contype='c';
 ALTER TABLE agenteam_work.task_events DROP CONSTRAINT task_events_operation_kind_check;
 EXECUTE format($check$ALTER TABLE agenteam_work.task_events ADD CONSTRAINT task_events_operation_kind_check CHECK(((failure_operation_id IS NULL AND (%s)) OR
 (failure_operation_id IS NOT NULL AND type IN ('blocker_added','state_changed') AND operation_id IS NULL AND blocker_operation_id IS NULL AND transition_operation_id IS NULL AND claim_operation_id IS NULL AND compensation_operation_id IS NULL AND correlation_id=failure_operation_id)) IS TRUE)$check$,old_expr);
 SELECT pg_get_expr(conbin,conrelid) INTO STRICT old_expr FROM pg_constraint WHERE conrelid='agenteam_work.task_events'::regclass AND conname='task_events_actor_source' AND contype='c';
 ALTER TABLE agenteam_work.task_events DROP CONSTRAINT task_events_actor_source;
 EXECUTE format($check$ALTER TABLE agenteam_work.task_events ADD CONSTRAINT task_events_actor_source CHECK(((failure_operation_id IS NULL AND (%s)) OR
 (failure_operation_id IS NOT NULL AND actor=jsonb_build_object('type','system','service_name','scheduler','source','scheduler','cause_id',failure_operation_id::text)
 AND ((type='state_changed' AND payload=jsonb_build_object('from_state','in_progress','to_state','blocked','reason_code','scheduler_launch_failed'))
 OR (type='blocker_added' AND payload=jsonb_build_object('blocker_id',payload->>'blocker_id','blocker_type','technical','reason_code','scheduler_launch_failed')
 AND payload->>'blocker_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$')))) IS TRUE)$check$,old_expr);
END;
$$;
-- +goose StatementEnd
CREATE INDEX task_events_failure_operation ON agenteam_work.task_events(project_id,failure_operation_id,id) WHERE failure_operation_id IS NOT NULL;
CREATE INDEX task_blockers_failure_operation ON agenteam_work.task_blockers(project_id,failure_operation_id) WHERE failure_operation_id IS NOT NULL;
