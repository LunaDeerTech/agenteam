-- agenteam:transaction tx
-- +goose Up
CREATE TABLE agenteam_work.task_blocker_commands (
 id agenteam_work.safe_id PRIMARY KEY,
 project_id agenteam_work.safe_id NOT NULL,
 actor_user_id agenteam_work.safe_id NOT NULL,
 command_name text NOT NULL CHECK(command_name IN ('work.task.blocker.add','work.task.blocker.resolve')),
 idempotency_key text NOT NULL CHECK(octet_length(idempotency_key) BETWEEN 1 AND 128 AND idempotency_key ~ '^[A-Za-z0-9._:/-]+$'),
 semantic_digest text NOT NULL CHECK(semantic_digest ~ '^sha256:[0-9a-f]{64}$'),
 request jsonb NOT NULL CHECK(jsonb_typeof(request)='object' AND octet_length(request::text)<=524288),
 plan_revision bigint NOT NULL CHECK(plan_revision>=1),
 state text NOT NULL CHECK(state IN ('planned','completed')),
 plan jsonb NOT NULL CHECK(jsonb_typeof(plan)='object' AND octet_length(plan::text)<=4194304),
 task_event_id agenteam_work.safe_id NOT NULL UNIQUE,
 event_id agenteam_work.safe_id NOT NULL UNIQUE,
 receipt jsonb CHECK(receipt IS NULL OR (jsonb_typeof(receipt)='object' AND octet_length(receipt::text)<=524288)),
 created_at timestamptz(6) NOT NULL,
 committed_at timestamptz(6) CHECK(committed_at IS NULL OR committed_at>=created_at),
 CONSTRAINT task_blocker_commands_project_id_key UNIQUE(project_id,id),
 CONSTRAINT task_blocker_commands_history_key UNIQUE(project_id,id,task_event_id),
 CONSTRAINT task_blocker_commands_identity_key UNIQUE(project_id,command_name,idempotency_key),
 CONSTRAINT task_blocker_commands_result_check CHECK(((state='planned' AND receipt IS NULL AND committed_at IS NULL)
 OR (state='completed' AND receipt IS NOT NULL AND committed_at IS NOT NULL
 AND receipt->>'task_event_id'=task_event_id::text
 AND jsonb_typeof(receipt->'event_ids')='array' AND jsonb_array_length(receipt->'event_ids')=1
 AND receipt->'event_ids'->>0=event_id::text)) IS TRUE)
);

CREATE TABLE agenteam_work.task_blockers (
 id agenteam_work.safe_id PRIMARY KEY,
 project_id agenteam_work.safe_id NOT NULL,
 task_id agenteam_work.safe_id NOT NULL,
 type text NOT NULL CHECK(type IN ('rely_on','waiting_for_human')),
 description text NOT NULL CHECK(octet_length(description)<=1024),
 metadata jsonb NOT NULL,
 created_at timestamptz(6) NOT NULL,
 created_by jsonb NOT NULL,
 created_operation_id agenteam_work.safe_id NOT NULL,
 resolved_at timestamptz(6),
 resolved_by jsonb,
 resolution_comment text CHECK(resolution_comment IS NULL OR (octet_length(resolution_comment) BETWEEN 1 AND 1024 AND resolution_comment ~ '[^[:space:]]')),
 resolved_operation_id agenteam_work.safe_id,
 CONSTRAINT task_blockers_project_task_id_key UNIQUE(project_id,task_id,id),
 CONSTRAINT task_blockers_task_fk FOREIGN KEY(project_id,task_id) REFERENCES agenteam_work.tasks(project_id,id) ON DELETE RESTRICT,
 CONSTRAINT task_blockers_created_operation_fk FOREIGN KEY(project_id,created_operation_id) REFERENCES agenteam_work.task_blocker_commands(project_id,id) ON DELETE RESTRICT,
 CONSTRAINT task_blockers_resolved_operation_fk FOREIGN KEY(project_id,resolved_operation_id) REFERENCES agenteam_work.task_blocker_commands(project_id,id) ON DELETE RESTRICT,
 CONSTRAINT task_blockers_metadata_check CHECK((jsonb_typeof(metadata)='object' AND octet_length(metadata::text)<=1024 AND
 ((type='waiting_for_human' AND metadata='{}'::jsonb) OR (type='rely_on'
 AND metadata=jsonb_build_object('related_task_id',metadata->'related_task_id')
 AND jsonb_typeof(metadata->'related_task_id')='string'
 AND metadata->>'related_task_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
 AND metadata->>'related_task_id'<>task_id::text))) IS TRUE),
 CONSTRAINT task_blockers_created_actor_check CHECK((octet_length(created_by::text)<=1024
 AND created_by=jsonb_build_object('type','human','source','task_domain','user_id',created_by->>'user_id')
 AND created_by->>'user_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$') IS TRUE),
 CONSTRAINT task_blockers_resolution_check CHECK(((resolved_at IS NULL AND resolved_by IS NULL AND resolution_comment IS NULL AND resolved_operation_id IS NULL)
 OR (resolved_at IS NOT NULL AND resolved_at>=created_at AND resolved_by IS NOT NULL AND resolved_operation_id IS NOT NULL
 AND octet_length(resolved_by::text)<=1024
 AND resolved_by=jsonb_build_object('type','human','source','task_domain','user_id',resolved_by->>'user_id')
 AND resolved_by->>'user_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$')) IS TRUE)
);
CREATE INDEX task_blockers_history ON agenteam_work.task_blockers(project_id,task_id,created_at,id);
CREATE INDEX task_blockers_unresolved ON agenteam_work.task_blockers(project_id,task_id) WHERE resolved_at IS NULL;

-- +goose StatementBegin
CREATE FUNCTION agenteam_work.reject_blocker_rewrite() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ROW(NEW.id,NEW.project_id,NEW.task_id,NEW.type,NEW.description,NEW.metadata,NEW.created_at,NEW.created_by,NEW.created_operation_id)
 IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.task_id,OLD.type,OLD.description,OLD.metadata,OLD.created_at,OLD.created_by,OLD.created_operation_id)
 OR OLD.resolved_at IS NOT NULL OR NEW.resolved_at IS NULL THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='task_blockers_immutable', MESSAGE='immutable blocker';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER task_blockers_immutable BEFORE UPDATE ON agenteam_work.task_blockers FOR EACH ROW EXECUTE FUNCTION agenteam_work.reject_blocker_rewrite();

-- Identify the exact released correlation CHECK, without assuming PostgreSQL's
-- generated name for a CHECK mentioning two columns. No other CHECK is removed.
ALTER TABLE agenteam_work.task_events DROP CONSTRAINT task_events_type_check;
-- +goose StatementBegin
DO $$
DECLARE old_name text; matches integer;
BEGIN
 SELECT count(*),min(conname) INTO matches,old_name FROM pg_constraint
 WHERE conrelid='agenteam_work.task_events'::regclass AND contype='c'
 AND pg_get_constraintdef(oid) IN ('CHECK ((correlation_id = operation_id))','CHECK (((correlation_id)::uuid = (operation_id)::uuid))');
 IF matches<>1 THEN RAISE EXCEPTION 'unexpected task history correlation constraint'; END IF;
 EXECUTE format('ALTER TABLE agenteam_work.task_events DROP CONSTRAINT %I',old_name);
END;
$$;
-- +goose StatementEnd
ALTER TABLE agenteam_work.task_events ALTER COLUMN operation_id DROP NOT NULL;
ALTER TABLE agenteam_work.task_events ADD COLUMN blocker_operation_id agenteam_work.safe_id;
ALTER TABLE agenteam_work.task_events ADD CONSTRAINT task_events_blocker_operation_key UNIQUE(blocker_operation_id);
ALTER TABLE agenteam_work.task_events ADD CONSTRAINT task_events_blocker_operation_fk FOREIGN KEY(project_id,blocker_operation_id,id) REFERENCES agenteam_work.task_blocker_commands(project_id,id,task_event_id) ON DELETE RESTRICT;
ALTER TABLE agenteam_work.task_events ADD CONSTRAINT task_events_operation_kind_check CHECK(
 (type IN ('task_created','fields_updated') AND operation_id IS NOT NULL AND blocker_operation_id IS NULL AND correlation_id=operation_id)
 OR (type IN ('blocker_added','blocker_resolved') AND operation_id IS NULL AND blocker_operation_id IS NOT NULL AND correlation_id=blocker_operation_id));
