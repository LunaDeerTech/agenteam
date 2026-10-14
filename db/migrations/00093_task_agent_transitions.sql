-- agenteam:transaction tx
-- +goose Up
-- Human command rows keep their original identity; AgentRun rows retain the
-- captured execution even after Task reassignment or execution retirement.
ALTER TABLE agenteam_work.task_transition_commands
 ALTER COLUMN actor_user_id DROP NOT NULL,
 ADD COLUMN actor_agent_id agenteam_work.safe_id,
 ADD COLUMN actor_execution_id agenteam_work.safe_id,
 ADD CONSTRAINT task_transition_actor_identity CHECK (
  (actor_user_id IS NOT NULL AND actor_agent_id IS NULL AND actor_execution_id IS NULL)
  OR (actor_user_id IS NULL AND actor_agent_id IS NOT NULL AND actor_execution_id IS NOT NULL));

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_work.reject_transition_command_rewrite() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.state='completed' OR ROW(NEW.id,NEW.project_id,NEW.actor_user_id,NEW.actor_agent_id,NEW.actor_execution_id,NEW.task_id,NEW.command_name,NEW.idempotency_key,NEW.semantic_digest,NEW.expected_version,NEW.request,NEW.created_at)
 IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.actor_user_id,OLD.actor_agent_id,OLD.actor_execution_id,OLD.task_id,OLD.command_name,OLD.idempotency_key,OLD.semantic_digest,OLD.expected_version,OLD.request,OLD.created_at) THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='task_transition_commands_immutable', MESSAGE='immutable task transition command';
 END IF;
 IF NEW.state='completed' THEN
  IF ROW(NEW.plan_revision,NEW.plan,NEW.task_event_ids,NEW.event_id)
  IS DISTINCT FROM ROW(OLD.plan_revision,OLD.plan,OLD.task_event_ids,OLD.event_id) THEN
   RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='task_transition_commands_plan', MESSAGE='changed task transition plan';
  END IF;
 ELSIF OLD.plan_revision=9223372036854775807 OR NEW.plan_revision<>OLD.plan_revision+1 THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='task_transition_commands_plan', MESSAGE='task transition revision';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd

ALTER TABLE agenteam_work.task_blockers ADD COLUMN created_transition_operation_id agenteam_work.safe_id;
ALTER TABLE agenteam_work.task_blockers ADD CONSTRAINT task_blockers_created_transition_fk
 FOREIGN KEY(project_id,created_transition_operation_id) REFERENCES agenteam_work.task_transition_commands(project_id,id) ON DELETE RESTRICT;
ALTER TABLE agenteam_work.task_blockers DROP CONSTRAINT task_blockers_type_check;
ALTER TABLE agenteam_work.task_blockers ADD CONSTRAINT task_blockers_type_check
 CHECK(type IN('rely_on','waiting_for_human','technical','user_cancelled_execution'));
ALTER TABLE agenteam_work.task_blockers DROP CONSTRAINT task_blockers_creation_source;
ALTER TABLE agenteam_work.task_blockers ADD CONSTRAINT task_blockers_creation_source CHECK((
 (type IN('rely_on','waiting_for_human') AND created_operation_id IS NOT NULL AND failure_operation_id IS NULL AND created_transition_operation_id IS NULL)
 OR (type='technical' AND created_operation_id IS NULL AND failure_operation_id IS NOT NULL AND created_transition_operation_id IS NULL)
 OR (type='user_cancelled_execution' AND created_operation_id IS NULL AND failure_operation_id IS NULL AND created_transition_operation_id IS NOT NULL)) IS TRUE);

-- +goose StatementBegin
DO $$
DECLARE old_expr text;
BEGIN
 SELECT pg_get_expr(conbin,conrelid) INTO STRICT old_expr FROM pg_constraint
 WHERE conrelid='agenteam_work.task_blockers'::regclass AND conname='task_blockers_metadata_check' AND contype='c';
 ALTER TABLE agenteam_work.task_blockers DROP CONSTRAINT task_blockers_metadata_check;
 EXECUTE format($check$ALTER TABLE agenteam_work.task_blockers ADD CONSTRAINT task_blockers_metadata_check CHECK((
 (type<>'user_cancelled_execution' AND (%s)) OR (type='user_cancelled_execution'
 AND jsonb_typeof(metadata)='object' AND octet_length(metadata::text)<=8192
 AND metadata=jsonb_build_object('execution_id',metadata->'execution_id','cancelled_by',metadata->'cancelled_by','reason',metadata->'reason')
 AND metadata->>'execution_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
 AND metadata->>'cancelled_by'=created_by->>'user_id'
 AND (metadata->'reason'='null'::jsonb OR (jsonb_typeof(metadata->'reason')='string' AND octet_length(metadata->>'reason') BETWEEN 1 AND 1024 AND metadata->>'reason' ~ '[^[:space:]]')))) IS TRUE)$check$,old_expr);
 SELECT pg_get_expr(conbin,conrelid) INTO STRICT old_expr FROM pg_constraint
 WHERE conrelid='agenteam_work.task_blockers'::regclass AND conname='task_blockers_resolution_check' AND contype='c';
 ALTER TABLE agenteam_work.task_blockers DROP CONSTRAINT task_blockers_resolution_check;
 EXECUTE format($check$ALTER TABLE agenteam_work.task_blockers ADD CONSTRAINT task_blockers_resolution_check CHECK((
 (%s) OR (resolved_transition_operation_id IS NOT NULL AND resolved_operation_id IS NULL
 AND resolved_at IS NOT NULL AND resolved_at>=created_at AND resolution_comment IS NULL
 AND resolved_by=jsonb_build_object('type','agent_run','source','task_domain','agent_id',resolved_by->>'agent_id','execution_id',resolved_by->>'execution_id')
 AND resolved_by->>'agent_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
 AND resolved_by->>'execution_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$')) IS TRUE)$check$,old_expr);
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_work.reject_blocker_rewrite() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ROW(NEW.id,NEW.project_id,NEW.task_id,NEW.type,NEW.description,NEW.metadata,NEW.created_at,NEW.created_by,NEW.created_operation_id,NEW.failure_operation_id,NEW.created_transition_operation_id)
 IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.task_id,OLD.type,OLD.description,OLD.metadata,OLD.created_at,OLD.created_by,OLD.created_operation_id,OLD.failure_operation_id,OLD.created_transition_operation_id)
 OR OLD.resolved_at IS NOT NULL OR NEW.resolved_at IS NULL THEN
  RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='task_blockers_immutable',MESSAGE='immutable blocker';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$
DECLARE old_expr text;
BEGIN
 SELECT pg_get_expr(conbin,conrelid) INTO STRICT old_expr FROM pg_constraint
 WHERE conrelid='agenteam_work.task_events'::regclass AND conname='task_events_actor_source' AND contype='c';
 ALTER TABLE agenteam_work.task_events DROP CONSTRAINT task_events_actor_source;
 EXECUTE format($check$ALTER TABLE agenteam_work.task_events ADD CONSTRAINT task_events_actor_source CHECK((
 (%s) OR (transition_operation_id IS NOT NULL
 AND actor=jsonb_build_object('type','agent_run','source','task_domain','agent_id',actor->>'agent_id','execution_id',actor->>'execution_id')
 AND actor->>'agent_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
 AND actor->>'execution_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$')) IS TRUE)$check$,old_expr);
END;
$$;
-- +goose StatementEnd
