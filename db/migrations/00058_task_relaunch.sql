-- agenteam:transaction tx
-- +goose Up
-- A relaunch records current Work eligibility without mutating the Task or
-- fabricating a todo claim. Scheduler pending and this immutable parent are
-- written in the same caller transaction, with no circular foreign key.
CREATE TABLE agenteam_work.task_scheduler_relaunches (
 id agenteam_work.safe_id PRIMARY KEY,
 project_id agenteam_work.safe_id NOT NULL,
 task_id agenteam_work.safe_id NOT NULL,
 agent_id agenteam_work.safe_id NOT NULL,
 sprint_id agenteam_work.safe_id NOT NULL,
 milestone_id agenteam_work.safe_id NOT NULL,
 request_id agenteam_work.safe_id NOT NULL,
 task_version bigint NOT NULL CHECK(task_version>0),
 purpose text NOT NULL CHECK(purpose='task/work'),
 source_digest text NOT NULL CHECK(source_digest ~ '^sha256:[0-9a-f]{64}$'),
 record jsonb NOT NULL,
 created_at timestamptz(6) NOT NULL,
 UNIQUE(project_id,id),
 FOREIGN KEY(project_id,task_id) REFERENCES agenteam_work.tasks(project_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(project_id,milestone_id,sprint_id) REFERENCES agenteam_work.sprints(project_id,milestone_id,id) ON DELETE RESTRICT,
 CHECK((jsonb_typeof(record)='object' AND octet_length(record::text)<=4194304
  AND record ?& ARRAY['request','task','sprint','milestone','created_at']
  AND record-ARRAY['request','task','sprint','milestone','created_at']='{}'::jsonb
  AND record->'request'=jsonb_build_object('ProjectID',project_id::text,'TaskID',task_id::text,'AgentID',agent_id::text,
   'CurrentSprintID',sprint_id::text,'ExpectedTaskVersion',task_version,'DispatchID',id::text,'RequestID',request_id::text,'Purpose',purpose)
  AND record->'task'->>'id'=task_id::text AND record->'task'->>'project_id'=project_id::text
  AND record->'task'->>'version'=task_version::text AND record->'task'->>'state'='in_progress'
  AND record->'task'->>'assignee_agent_id'=agent_id::text AND record->'task'->>'sprint_id'=sprint_id::text
  AND record->'task'->>'milestone_id'=milestone_id::text
  AND record->'sprint'->>'id'=sprint_id::text AND record->'sprint'->>'project_id'=project_id::text
  AND record->'sprint'->>'milestone_id'=milestone_id::text AND record->'sprint'->>'state'='current'
  AND record->'milestone'->>'id'=milestone_id::text AND record->'milestone'->>'project_id'=project_id::text
  AND (record->>'created_at')::timestamptz=created_at
  AND created_at>=(record->'task'->>'updated_at')::timestamptz) IS TRUE)
);
-- +goose StatementBegin
CREATE FUNCTION agenteam_work.reject_task_relaunch_rewrite() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='task_scheduler_relaunches_immutable',MESSAGE='immutable task relaunch';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER task_scheduler_relaunches_immutable BEFORE UPDATE ON agenteam_work.task_scheduler_relaunches
 FOR EACH ROW EXECUTE FUNCTION agenteam_work.reject_task_relaunch_rewrite();

-- Keep every historical claim parent and byte shape. New rows select the
-- relaunch parent explicitly; no old result is updated or reclassified.
ALTER TABLE agenteam_work.task_launch_failures ADD COLUMN relaunch_operation_id agenteam_work.safe_id;
ALTER TABLE agenteam_work.task_launch_failures ADD COLUMN claim_operation_id agenteam_work.safe_id
 GENERATED ALWAYS AS (CASE WHEN relaunch_operation_id IS NULL THEN id ELSE NULL END) STORED;
-- +goose StatementBegin
DO $$
DECLARE parent_name text; shape_name text; old_expr text;
BEGIN
 SELECT conname INTO STRICT parent_name FROM pg_constraint
  WHERE conrelid='agenteam_work.task_launch_failures'::regclass AND contype='f'
   AND confrelid='agenteam_work.task_scheduler_claims'::regclass;
 EXECUTE format('ALTER TABLE agenteam_work.task_launch_failures DROP CONSTRAINT %I',parent_name);
 ALTER TABLE agenteam_work.task_launch_failures ADD CONSTRAINT task_launch_failures_claim_parent
  FOREIGN KEY(project_id,claim_operation_id) REFERENCES agenteam_work.task_scheduler_claims(project_id,id) ON DELETE RESTRICT;
 ALTER TABLE agenteam_work.task_launch_failures ADD CONSTRAINT task_launch_failures_relaunch_parent
  FOREIGN KEY(project_id,relaunch_operation_id) REFERENCES agenteam_work.task_scheduler_relaunches(project_id,id) ON DELETE RESTRICT;
 ALTER TABLE agenteam_work.task_launch_failures ADD CONSTRAINT task_launch_failures_origin_shape CHECK((
  (relaunch_operation_id IS NULL AND claim_operation_id=id)
  OR (relaunch_operation_id=id AND claim_operation_id IS NULL)) IS TRUE);
 SELECT conname,pg_get_expr(conbin,conrelid) INTO STRICT shape_name,old_expr FROM pg_constraint
  WHERE conrelid='agenteam_work.task_launch_failures'::regclass AND contype='c'
   AND pg_get_expr(conbin,conrelid) LIKE '%jsonb_typeof(record)%';
 EXECUTE format('ALTER TABLE agenteam_work.task_launch_failures DROP CONSTRAINT %I',shape_name);
 EXECUTE format($check$ALTER TABLE agenteam_work.task_launch_failures ADD CONSTRAINT task_launch_failures_record_shape CHECK((
  (relaunch_operation_id IS NULL AND (%s)) OR
  (relaunch_operation_id=id AND jsonb_typeof(record)='object' AND octet_length(record::text)<=4194304
   AND record ?& ARRAY['request','facts','relaunch','before','after','sprint','current_sprint_id','changed','groups','query_generation','blocker','history','event','header','created_at']
   AND record-ARRAY['request','facts','relaunch','before','after','sprint','current_sprint_id','changed','groups','query_generation','blocker','history','event','header','created_at','relaunch_event']='{}'::jsonb
   AND record->'request' ?& ARRAY['Relaunch','DispatchVersion','LaunchAttempt']
   AND record->'request'-ARRAY['Relaunch','DispatchVersion','LaunchAttempt']='{}'::jsonb
   AND record->'request'->'Relaunch'=record->'relaunch'->'request'
   AND record->'request'->'Relaunch'->>'DispatchID'=id::text AND record->'request'->'Relaunch'->>'ProjectID'=project_id::text
   AND record->'request'->'Relaunch'->>'TaskID'=task_id::text AND record->'request'->'Relaunch'->>'AgentID'=agent_id::text
   AND record->'request'->'Relaunch'->>'RequestID'=request_id::text AND record->'request'->'Relaunch'->>'Purpose'='task/work'
   AND record->'request'->>'DispatchVersion'=dispatch_version::text AND record->'request'->>'LaunchAttempt'=launch_attempt::text
   AND record->'facts' ?& ARRAY['Relaunch','Reason','OccurredAt'] AND record->'facts'-ARRAY['Relaunch','Reason','OccurredAt']='{}'::jsonb
   AND record->'facts'->'Relaunch'->'Request'=record->'request'->'Relaunch'
   AND record->'facts'->'Relaunch'->>'MilestoneID'=record->'relaunch'->'task'->>'milestone_id'
   AND record->'facts'->'Relaunch'->>'ReferenceDigest' ~ '^sha256:[0-9a-f]{64}$'
   AND record->'facts'->>'Reason'=reason AND (record->'facts'->>'OccurredAt')::timestamptz=failure_occurred_at
   AND record->>'changed'=changed::text AND (record->>'created_at')::timestamptz=created_at
   AND record->'before'->>'id'=task_id::text AND record->'after'->>'id'=task_id::text
   AND record->'before'->>'project_id'=project_id::text AND record->'after'->>'project_id'=project_id::text
   AND record->'before'->>'version'=before_version::text AND record->'after'->>'version'=after_version::text
   AND record->'event'='null'::jsonb
   AND ((changed AND before_version<9223372036854775807 AND after_version=before_version+1
    AND blocker_id IS NOT NULL AND event_id IS NOT NULL AND record->'blocker'->>'id'=blocker_id::text
    AND record->'header'->>'event_id'=event_id::text AND record->'header'->>'schema_version'='5'
    AND record->'relaunch_event'->>'dispatch_id'=id::text AND record->'relaunch_event'->>'origin'='relaunch'
    AND record->'relaunch_event'->'source'=record->'facts'->'Relaunch'
    AND record->'relaunch_event'->>'blocker_id'=blocker_id::text AND record->'relaunch_event'->>'reason'=reason
    AND record->'after'->>'state'='blocked' AND jsonb_typeof(record->'history')='array'
    AND ((record->'before'->>'state'='blocked' AND jsonb_array_length(record->'history')=1 AND record->'groups'='[]'::jsonb)
     OR (record->'before'->>'state'='in_progress' AND jsonb_array_length(record->'history')=2 AND jsonb_array_length(record->'groups')=2)))
    OR (NOT changed AND before_version=after_version AND blocker_id IS NULL AND event_id IS NULL
     AND record->'before'=record->'after' AND record->'blocker'='null'::jsonb AND record->'history'='[]'::jsonb
     AND NOT record ? 'relaunch_event' AND record->'header'='null'::jsonb AND record->'groups'='[]'::jsonb AND record->>'query_generation'='0')))
 ) IS TRUE)$check$,old_expr);
END;
$$;
-- +goose StatementEnd

ALTER TABLE agenteam_scheduler.dispatches ADD COLUMN relaunch_source bytea;
ALTER TABLE agenteam_scheduler.dispatches ADD CONSTRAINT dispatch_relaunch_shape CHECK((
 relaunch_source IS NULL OR (octet_length(relaunch_source) BETWEEN 1 AND 262144
  AND claim_guard IS NULL AND claim_source_sprint_id IS NULL AND claim_source_state IS NULL AND claim_source_priority IS NULL
  AND convert_from(launch_request,'UTF8')::jsonb->>'purpose'='task/work'
  AND jsonb_typeof(convert_from(relaunch_source,'UTF8')::jsonb)='object'
  AND convert_from(relaunch_source,'UTF8')::jsonb ?& ARRAY['Request','MilestoneID','ReferenceDigest']
  AND convert_from(relaunch_source,'UTF8')::jsonb-ARRAY['Request','MilestoneID','ReferenceDigest']='{}'::jsonb
  AND convert_from(relaunch_source,'UTF8')::jsonb->>'MilestoneID' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
  AND convert_from(relaunch_source,'UTF8')::jsonb->>'ReferenceDigest' ~ '^sha256:[0-9a-f]{64}$'
  AND (convert_from(relaunch_source,'UTF8')::jsonb->'Request'->>'ExpectedTaskVersion')::bigint>0
  AND convert_from(relaunch_source,'UTF8')::jsonb->'Request'=jsonb_build_object(
   'ProjectID',project_id::text,'TaskID',task_id::text,'AgentID',agent_id::text,'CurrentSprintID',sprint_id::text,
   'DispatchID',id::text,'RequestID',request_id::text,'Purpose','task/work',
   'ExpectedTaskVersion',(convert_from(relaunch_source,'UTF8')::jsonb->'Request'->>'ExpectedTaskVersion')::bigint))
) IS TRUE);
-- The old permanent/temporary policy proofs are retained verbatim. Only their
-- required Work parent admits the new explicit relaunch arm.
-- +goose StatementBegin
DO $$
DECLARE old_expr text; new_expr text;
BEGIN
 SELECT pg_get_expr(conbin,conrelid) INTO STRICT old_expr FROM pg_constraint
  WHERE conrelid='agenteam_scheduler.dispatches'::regclass AND conname='dispatch_final_failure_shape' AND contype='c';
 new_expr:=replace(old_expr,'(claim_guard IS NOT NULL)','((claim_guard IS NOT NULL) OR (relaunch_source IS NOT NULL))');
 IF new_expr=old_expr THEN RAISE EXCEPTION 'missing dispatch failure source constraint'; END IF;
 ALTER TABLE agenteam_scheduler.dispatches DROP CONSTRAINT dispatch_final_failure_shape;
 EXECUTE format('ALTER TABLE agenteam_scheduler.dispatches ADD CONSTRAINT dispatch_final_failure_shape CHECK (%s)',new_expr);
END;
$$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION agenteam_scheduler.guard_dispatch_relaunch_source() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.relaunch_source IS DISTINCT FROM OLD.relaunch_source THEN
  RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='dispatch_relaunch_source_immutable',MESSAGE='immutable dispatch relaunch source';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER dispatch_relaunch_source_immutable BEFORE UPDATE ON agenteam_scheduler.dispatches
 FOR EACH ROW EXECUTE FUNCTION agenteam_scheduler.guard_dispatch_relaunch_source();

CREATE TABLE agenteam_scheduler.task_runtimes (
 project_id agenteam_scheduler.safe_id NOT NULL,
 task_id agenteam_scheduler.safe_id NOT NULL,
 latest_dispatch_id agenteam_scheduler.safe_id NOT NULL,
 latest_execution_id agenteam_scheduler.safe_id NOT NULL,
 cooldown_execution_id agenteam_scheduler.safe_id,
 cooldown_purpose text,
 relaunch_skip_remaining bigint NOT NULL CHECK(relaunch_skip_remaining>=0),
 version bigint NOT NULL CHECK(version>=1),
 updated_at timestamptz(6) NOT NULL,
 PRIMARY KEY(project_id,task_id),
 FOREIGN KEY(project_id,latest_dispatch_id) REFERENCES agenteam_scheduler.dispatches(project_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(latest_execution_id) REFERENCES agenteam_execution.executions(id) ON DELETE RESTRICT,
 CHECK(((cooldown_execution_id IS NULL AND cooldown_purpose IS NULL AND relaunch_skip_remaining=0)
  OR (cooldown_execution_id IS NOT NULL AND cooldown_execution_id=latest_execution_id AND cooldown_purpose='task/work')) IS TRUE)
);
-- +goose StatementBegin
CREATE FUNCTION agenteam_scheduler.guard_task_runtime() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' THEN
  IF ROW(NEW.project_id,NEW.task_id) IS DISTINCT FROM ROW(OLD.project_id,OLD.task_id)
   OR OLD.version=9223372036854775807 OR NEW.version<>OLD.version+1 OR NEW.updated_at<=OLD.updated_at THEN
   RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='task_runtime_immutable',MESSAGE='invalid task runtime update';
  END IF;
 END IF;
 IF NOT EXISTS(SELECT 1 FROM agenteam_scheduler.dispatches d WHERE d.project_id=NEW.project_id AND d.id=NEW.latest_dispatch_id
  AND d.task_id=NEW.task_id AND d.status='launched' AND d.execution_id=NEW.latest_execution_id) THEN
  RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='task_runtime_association',MESSAGE='invalid task runtime association';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER task_runtime_immutable BEFORE INSERT OR UPDATE ON agenteam_scheduler.task_runtimes
 FOR EACH ROW EXECUTE FUNCTION agenteam_scheduler.guard_task_runtime();

CREATE TABLE agenteam_scheduler.relaunch_visits (
 id agenteam_scheduler.safe_id PRIMARY KEY,
 project_id agenteam_scheduler.safe_id NOT NULL,
 task_id agenteam_scheduler.safe_id NOT NULL,
 request bytea NOT NULL CHECK(octet_length(request) BETWEEN 1 AND 262144),
 request_digest text NOT NULL CHECK(request_digest ~ '^sha256:[0-9a-f]{64}$'),
 outcome text NOT NULL CHECK(outcome IN ('cooldown_skipped','dispatch_created')),
 remaining bigint NOT NULL CHECK(remaining>=0),
 created_at timestamptz(6) NOT NULL,
 CHECK(outcome<>'dispatch_created' OR remaining=0),
 CHECK((jsonb_typeof(convert_from(request,'UTF8')::jsonb)='object'
  AND convert_from(request,'UTF8')::jsonb ?& ARRAY['request','policy','retry_policy_digest']
  AND convert_from(request,'UTF8')::jsonb-ARRAY['request','policy','retry_policy_digest']='{}'::jsonb
  AND jsonb_typeof(convert_from(request,'UTF8')::jsonb->'policy')='object'
  AND (convert_from(request,'UTF8')::jsonb->>'retry_policy_digest'='' OR convert_from(request,'UTF8')::jsonb->>'retry_policy_digest' ~ '^sha256:[0-9a-f]{64}$')
  AND convert_from(request,'UTF8')::jsonb->'request' ?& ARRAY['ProjectID','TaskID','AgentID','CurrentSprintID','ExpectedTaskVersion','DispatchID','RequestID','Purpose']
  AND convert_from(request,'UTF8')::jsonb->'request'-ARRAY['ProjectID','TaskID','AgentID','CurrentSprintID','ExpectedTaskVersion','DispatchID','RequestID','Purpose']='{}'::jsonb
  AND convert_from(request,'UTF8')::jsonb->'request'->>'ProjectID'=project_id::text
  AND convert_from(request,'UTF8')::jsonb->'request'->>'TaskID'=task_id::text
  AND convert_from(request,'UTF8')::jsonb->'request'->>'DispatchID'=id::text
  AND convert_from(request,'UTF8')::jsonb->'request'->>'Purpose'='task/work'
  AND (convert_from(request,'UTF8')::jsonb->'request'->>'ExpectedTaskVersion')::bigint>0
  AND convert_from(request,'UTF8')::jsonb->'request'->>'AgentID' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
  AND convert_from(request,'UTF8')::jsonb->'request'->>'CurrentSprintID' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
  AND convert_from(request,'UTF8')::jsonb->'request'->>'RequestID' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$') IS TRUE)
);
-- +goose StatementBegin
CREATE FUNCTION agenteam_scheduler.reject_relaunch_visit_rewrite() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='relaunch_visits_immutable',MESSAGE='immutable relaunch visit';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER relaunch_visits_immutable BEFORE UPDATE ON agenteam_scheduler.relaunch_visits
 FOR EACH ROW EXECUTE FUNCTION agenteam_scheduler.reject_relaunch_visit_rewrite();
