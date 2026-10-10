-- agenteam:transaction tx
-- +goose Up
-- Public Human transfers have their own command identity and ordered history.
-- Planning and standalone Blocker commands retain their released storage arms.
CREATE TABLE agenteam_work.task_transition_commands (
 id agenteam_work.safe_id PRIMARY KEY,
 project_id agenteam_work.safe_id NOT NULL,
 actor_user_id agenteam_work.safe_id NOT NULL,
 task_id agenteam_work.safe_id NOT NULL,
 command_name text NOT NULL CHECK(command_name='work.task.transfer'),
 idempotency_key text NOT NULL CHECK(octet_length(idempotency_key) BETWEEN 1 AND 128 AND idempotency_key ~ '^[A-Za-z0-9._:/-]+$'),
 semantic_digest text NOT NULL CHECK(semantic_digest ~ '^sha256:[0-9a-f]{64}$'),
 expected_version bigint NOT NULL CHECK(expected_version>0),
 request jsonb NOT NULL CHECK(jsonb_typeof(request)='object' AND octet_length(request::text)<=524288),
 plan_revision bigint NOT NULL CHECK(plan_revision>0),
 state text NOT NULL CHECK(state IN ('planned','completed')),
 plan jsonb NOT NULL CHECK(jsonb_typeof(plan)='object' AND octet_length(plan::text)<=4194304),
 task_event_ids jsonb NOT NULL,
 event_id agenteam_work.safe_id NOT NULL UNIQUE,
 receipt jsonb CHECK(receipt IS NULL OR (jsonb_typeof(receipt)='object' AND octet_length(receipt::text)<=524288)),
 created_at timestamptz(6) NOT NULL,
 committed_at timestamptz(6) CHECK(committed_at IS NULL OR committed_at>=created_at),
 UNIQUE(project_id,id),
 UNIQUE(project_id,command_name,idempotency_key),
 CHECK((jsonb_typeof(task_event_ids)='array' AND jsonb_array_length(task_event_ids) BETWEEN 1 AND 35) IS TRUE),
 CHECK(((state='planned' AND receipt IS NULL AND committed_at IS NULL)
 OR (state='completed' AND receipt IS NOT NULL AND committed_at IS NOT NULL
 AND receipt->'task_event_ids'=task_event_ids
 AND jsonb_typeof(receipt->'event_ids')='array' AND jsonb_array_length(receipt->'event_ids')=1
 AND receipt->'event_ids'->>0=event_id::text
 AND receipt->'task'->>'id'=task_id::text AND receipt->'task'->>'project_id'=project_id::text)) IS TRUE)
);

-- +goose StatementBegin
CREATE FUNCTION agenteam_work.reject_transition_command_rewrite() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.state='completed' OR ROW(NEW.id,NEW.project_id,NEW.actor_user_id,NEW.task_id,NEW.command_name,NEW.idempotency_key,NEW.semantic_digest,NEW.expected_version,NEW.request,NEW.created_at)
 IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.actor_user_id,OLD.task_id,OLD.command_name,OLD.idempotency_key,OLD.semantic_digest,OLD.expected_version,OLD.request,OLD.created_at) THEN
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
CREATE TRIGGER task_transition_commands_immutable BEFORE UPDATE ON agenteam_work.task_transition_commands
 FOR EACH ROW EXECUTE FUNCTION agenteam_work.reject_transition_command_rewrite();

ALTER TABLE agenteam_work.task_events ADD COLUMN transition_operation_id agenteam_work.safe_id;
ALTER TABLE agenteam_work.task_events ADD CONSTRAINT task_events_transition_operation_fk
 FOREIGN KEY(project_id,transition_operation_id) REFERENCES agenteam_work.task_transition_commands(project_id,id) ON DELETE RESTRICT;
ALTER TABLE agenteam_work.task_events DROP CONSTRAINT task_events_operation_kind_check;
ALTER TABLE agenteam_work.task_events ADD CONSTRAINT task_events_operation_kind_check CHECK((
 (type IN ('task_created','fields_updated') AND operation_id IS NOT NULL AND blocker_operation_id IS NULL AND transition_operation_id IS NULL AND correlation_id=operation_id)
 OR (type IN ('blocker_added','blocker_resolved') AND operation_id IS NULL AND blocker_operation_id IS NOT NULL AND transition_operation_id IS NULL AND correlation_id=blocker_operation_id)
 OR (type IN ('state_changed','assignee_changed','blocker_added','blocker_resolved','comment') AND operation_id IS NULL AND blocker_operation_id IS NULL AND transition_operation_id IS NOT NULL AND correlation_id=transition_operation_id)
) IS TRUE);

-- Only comment history gains the larger cap; all released payloads retain 8KiB.
-- +goose StatementBegin
DO $$
DECLARE old_name text; matches integer;
BEGIN
 SELECT count(*),min(conname) INTO matches,old_name FROM pg_constraint
 WHERE conrelid='agenteam_work.task_events'::regclass AND contype='c'
 AND pg_get_constraintdef(oid) LIKE '%jsonb_typeof(payload)%'
 AND pg_get_constraintdef(oid) LIKE '%8192%';
 IF matches<>1 THEN RAISE EXCEPTION 'unexpected task history payload constraint'; END IF;
 EXECUTE format('ALTER TABLE agenteam_work.task_events DROP CONSTRAINT %I',old_name);
END;
$$;
-- +goose StatementEnd
ALTER TABLE agenteam_work.task_events ADD CONSTRAINT task_events_payload_cap CHECK(
 jsonb_typeof(payload)='object' AND octet_length(payload::text)<=CASE WHEN type='comment' THEN 262144 ELSE 8192 END);
CREATE INDEX task_events_transition_operation ON agenteam_work.task_events(project_id,transition_operation_id,id) WHERE transition_operation_id IS NOT NULL;
