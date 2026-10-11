-- agenteam:transaction tx
-- +goose Up
-- Review uses the same immutable relaunch origin, with an exact purpose/phase
-- pair. Historical todo claims, work origins and schema 4 remain unchanged.
ALTER TABLE agenteam_work.task_scheduler_relaunches DROP CONSTRAINT task_scheduler_relaunches_purpose_check;
ALTER TABLE agenteam_work.task_scheduler_relaunches ADD CONSTRAINT task_scheduler_relaunches_purpose_check
 CHECK(purpose IN ('task/work','task/review'));
-- +goose StatementBegin
DO $$
DECLARE shape_name text; old_expr text; review_expr text;
BEGIN
 SELECT conname,pg_get_expr(conbin,conrelid) INTO STRICT shape_name,old_expr FROM pg_constraint
  WHERE conrelid='agenteam_work.task_scheduler_relaunches'::regclass AND contype='c'
   AND pg_get_expr(conbin,conrelid) LIKE '%jsonb_typeof(record)%';
 review_expr:=replace(old_expr,'''in_progress''::text','''in_review''::text');
 IF review_expr=old_expr THEN RAISE EXCEPTION 'missing relaunch source phase constraint'; END IF;
 EXECUTE format('ALTER TABLE agenteam_work.task_scheduler_relaunches DROP CONSTRAINT %I',shape_name);
 EXECUTE format($check$ALTER TABLE agenteam_work.task_scheduler_relaunches ADD CONSTRAINT task_scheduler_relaunches_record_shape
  CHECK(((purpose='task/work' AND (%s)) OR (purpose='task/review' AND (%s))) IS TRUE)$check$,old_expr,review_expr);

 -- The old complete predicate remains a separate arm. Only a real relaunch
 -- parent can enter the new review arm; the generated claim parent stays NULL.
 SELECT pg_get_expr(conbin,conrelid) INTO STRICT old_expr FROM pg_constraint
  WHERE conrelid='agenteam_work.task_launch_failures'::regclass AND conname='task_launch_failures_record_shape' AND contype='c';
 review_expr:=replace(old_expr,'''task/work''::text','''task/review''::text');
 IF review_expr=old_expr THEN RAISE EXCEPTION 'missing relaunch failure purpose constraint'; END IF;
 IF replace(review_expr,'''in_progress''::text','''in_review''::text')=review_expr THEN
  RAISE EXCEPTION 'missing relaunch failure phase constraint';
 END IF;
 review_expr:=replace(review_expr,'''in_progress''::text','''in_review''::text');
 ALTER TABLE agenteam_work.task_launch_failures DROP CONSTRAINT task_launch_failures_record_shape;
 EXECUTE format($check$ALTER TABLE agenteam_work.task_launch_failures ADD CONSTRAINT task_launch_failures_record_shape CHECK((
  (%s) OR (relaunch_operation_id=id AND claim_operation_id IS NULL
   AND record->'request'->'Relaunch'->>'Purpose'='task/review'
   AND record->'relaunch'->'task'->>'state'='in_review' AND (%s))) IS TRUE)$check$,old_expr,review_expr);

 -- History has its own typed shape, independent of the Outbox schema version.
 -- Preserve every prior actor/operation arm and add only the review failure edge.
 SELECT pg_get_expr(conbin,conrelid) INTO STRICT old_expr FROM pg_constraint
  WHERE conrelid='agenteam_work.task_events'::regclass AND conname='task_events_actor_source' AND contype='c';
 ALTER TABLE agenteam_work.task_events DROP CONSTRAINT task_events_actor_source;
 EXECUTE format($check$ALTER TABLE agenteam_work.task_events ADD CONSTRAINT task_events_actor_source CHECK((
  (%s) OR (failure_operation_id IS NOT NULL AND type='state_changed'
   AND actor=jsonb_build_object('type','system','service_name','scheduler','source','scheduler','cause_id',failure_operation_id::text)
   AND payload=jsonb_build_object('from_state','in_review','to_state','blocked','reason_code','scheduler_launch_failed'))) IS TRUE)$check$,old_expr);
END;
$$;
-- +goose StatementEnd

-- A relaunch Dispatch binds its purpose to the original Launch request. The
-- original canonical version is a JSON string, not a numeric reconstruction.
ALTER TABLE agenteam_scheduler.dispatches DROP CONSTRAINT dispatch_relaunch_shape;
ALTER TABLE agenteam_scheduler.dispatches ADD CONSTRAINT dispatch_relaunch_shape CHECK((
 relaunch_source IS NULL OR (octet_length(relaunch_source) BETWEEN 1 AND 262144
  AND claim_guard IS NULL AND claim_source_sprint_id IS NULL AND claim_source_state IS NULL AND claim_source_priority IS NULL
  AND convert_from(launch_request,'UTF8')::jsonb->>'purpose' IN ('task/work','task/review')
  AND jsonb_typeof(convert_from(relaunch_source,'UTF8')::jsonb)='object'
  AND convert_from(relaunch_source,'UTF8')::jsonb ?& ARRAY['Request','MilestoneID','ReferenceDigest']
  AND convert_from(relaunch_source,'UTF8')::jsonb-ARRAY['Request','MilestoneID','ReferenceDigest']='{}'::jsonb
  AND convert_from(relaunch_source,'UTF8')::jsonb->>'MilestoneID' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
  AND convert_from(relaunch_source,'UTF8')::jsonb->>'ReferenceDigest' ~ '^sha256:[0-9a-f]{64}$'
  AND (convert_from(relaunch_source,'UTF8')::jsonb->'Request'->>'ExpectedTaskVersion')::bigint>0
  AND convert_from(relaunch_source,'UTF8')::jsonb->'Request'=jsonb_build_object(
   'ProjectID',project_id::text,'TaskID',task_id::text,'AgentID',agent_id::text,'CurrentSprintID',sprint_id::text,
   'DispatchID',id::text,'RequestID',request_id::text,'Purpose',convert_from(launch_request,'UTF8')::jsonb->>'purpose',
   'ExpectedTaskVersion',(convert_from(relaunch_source,'UTF8')::jsonb->'Request'->>'ExpectedTaskVersion')::bigint::text))
) IS TRUE);

-- The original latest pair continues to mean work. A review-only Task may
-- legitimately have no work history; neither pair is fabricated or backfilled.
ALTER TABLE agenteam_scheduler.task_runtimes
 ALTER COLUMN latest_dispatch_id DROP NOT NULL,
 ALTER COLUMN latest_execution_id DROP NOT NULL,
 ADD COLUMN latest_review_dispatch_id agenteam_scheduler.safe_id,
 ADD COLUMN latest_review_execution_id agenteam_scheduler.safe_id;
ALTER TABLE agenteam_scheduler.task_runtimes ADD CONSTRAINT task_runtime_review_dispatch_fk
 FOREIGN KEY(project_id,latest_review_dispatch_id) REFERENCES agenteam_scheduler.dispatches(project_id,id) ON DELETE RESTRICT;
ALTER TABLE agenteam_scheduler.task_runtimes ADD CONSTRAINT task_runtime_review_execution_fk
 FOREIGN KEY(latest_review_execution_id) REFERENCES agenteam_execution.executions(id) ON DELETE RESTRICT;
ALTER TABLE agenteam_scheduler.task_runtimes ADD CONSTRAINT task_runtime_phase_pairs CHECK((
 ((latest_dispatch_id IS NULL)=(latest_execution_id IS NULL))
 AND ((latest_review_dispatch_id IS NULL)=(latest_review_execution_id IS NULL))
 AND (latest_dispatch_id IS NOT NULL OR latest_review_dispatch_id IS NOT NULL)) IS TRUE);
-- +goose StatementBegin
DO $$
DECLARE shape_name text;
BEGIN
 SELECT conname INTO STRICT shape_name FROM pg_constraint
  WHERE conrelid='agenteam_scheduler.task_runtimes'::regclass AND contype='c'
   AND pg_get_expr(conbin,conrelid) LIKE '%cooldown_execution_id%';
 EXECUTE format('ALTER TABLE agenteam_scheduler.task_runtimes DROP CONSTRAINT %I',shape_name);
 ALTER TABLE agenteam_scheduler.task_runtimes ADD CONSTRAINT task_runtime_cooldown_shape CHECK((
  (cooldown_execution_id IS NULL AND cooldown_purpose IS NULL AND relaunch_skip_remaining=0)
  OR (cooldown_execution_id IS NOT NULL AND
   ((cooldown_purpose='task/work' AND latest_execution_id IS NOT NULL AND cooldown_execution_id=latest_execution_id)
    OR (cooldown_purpose='task/review' AND latest_review_execution_id IS NOT NULL AND cooldown_execution_id=latest_review_execution_id)))) IS TRUE);
END;
$$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_scheduler.guard_task_runtime() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' THEN
  IF ROW(NEW.project_id,NEW.task_id) IS DISTINCT FROM ROW(OLD.project_id,OLD.task_id)
   OR OLD.version=9223372036854775807 OR NEW.version<>OLD.version+1 OR NEW.updated_at<=OLD.updated_at THEN
   RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='task_runtime_immutable',MESSAGE='invalid task runtime update';
  END IF;
 END IF;
 IF NEW.latest_dispatch_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM agenteam_scheduler.dispatches d WHERE d.project_id=NEW.project_id AND d.id=NEW.latest_dispatch_id
   AND d.task_id=NEW.task_id AND d.status='launched' AND d.execution_id=NEW.latest_execution_id
   AND convert_from(d.launch_request,'UTF8')::jsonb->>'purpose'='task/work') THEN
  RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='task_runtime_association',MESSAGE='invalid work runtime association';
 END IF;
 IF NEW.latest_review_dispatch_id IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM agenteam_scheduler.dispatches d WHERE d.project_id=NEW.project_id AND d.id=NEW.latest_review_dispatch_id
   AND d.task_id=NEW.task_id AND d.status='launched' AND d.execution_id=NEW.latest_review_execution_id
   AND convert_from(d.launch_request,'UTF8')::jsonb->>'purpose'='task/review') THEN
  RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='task_runtime_association',MESSAGE='invalid review runtime association';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- Keep the complete immutable visit identity/receipt and retry-policy binding.
-- Only the request's closed purpose set expands; an old visit is never replayed
-- under a new phase or charged to another phase's cooldown.
-- +goose StatementBegin
DO $$
DECLARE shape_name text; old_expr text; review_expr text;
BEGIN
 SELECT conname,pg_get_expr(conbin,conrelid) INTO STRICT shape_name,old_expr FROM pg_constraint
  WHERE conrelid='agenteam_scheduler.relaunch_visits'::regclass AND contype='c'
   AND pg_get_expr(conbin,conrelid) LIKE '%jsonb_typeof%';
 review_expr:=replace(old_expr,'''task/work''::text','''task/review''::text');
 IF review_expr=old_expr THEN RAISE EXCEPTION 'missing relaunch visit purpose constraint'; END IF;
 EXECUTE format('ALTER TABLE agenteam_scheduler.relaunch_visits DROP CONSTRAINT %I',shape_name);
 EXECUTE format('ALTER TABLE agenteam_scheduler.relaunch_visits ADD CONSTRAINT relaunch_visits_request_shape CHECK (((%s) OR (%s)) IS TRUE)',old_expr,review_expr);
END;
$$;
-- +goose StatementEnd
