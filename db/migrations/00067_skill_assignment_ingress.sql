-- agenteam:transaction tx
-- +goose Up
-- Creation receipts remain immutable seq-1 facts. Current assignment state is
-- separately anchored to a complete immutable mutation and never overwrites
-- the initialization receipt or an Execution's captured revision.
CREATE TABLE agenteam_skill.assignment_commands (
 id agenteam_skill.safe_id PRIMARY KEY,
 project_id agenteam_skill.safe_id NOT NULL,
 agent_id agenteam_skill.safe_id NOT NULL,
 actor_user_id agenteam_skill.safe_id NOT NULL,
 command_name text NOT NULL CHECK(command_name IN ('skill.assign','skill.remove')),
 idempotency_key text NOT NULL CHECK(octet_length(idempotency_key) BETWEEN 1 AND 256),
 semantic_digest text NOT NULL CHECK(semantic_digest ~ '^sha256:[0-9a-f]{64}$'),
 input bytea NOT NULL CHECK(octet_length(input) BETWEEN 2 AND 16384),
 candidate bytea NOT NULL CHECK(octet_length(candidate) BETWEEN 2 AND 1048576),
 state text NOT NULL CHECK(state IN ('planned','completed')),
 receipt bytea CHECK(octet_length(receipt) BETWEEN 2 AND 65536),
 created_at timestamptz(6) NOT NULL,
 completed_at timestamptz(6),
 UNIQUE(project_id,agent_id,command_name,idempotency_key),
 UNIQUE(project_id,agent_id,id),
 CHECK((state='completed')=(receipt IS NOT NULL AND completed_at IS NOT NULL)),
 FOREIGN KEY(project_id,agent_id) REFERENCES agenteam_skill.agent_assignment_heads(project_id,agent_id)
);
CREATE TABLE agenteam_skill.assignment_changes (
 id agenteam_skill.safe_id PRIMARY KEY,
 project_id agenteam_skill.safe_id NOT NULL,
 agent_id agenteam_skill.safe_id NOT NULL,
 assignment_sequence bigint NOT NULL CHECK(assignment_sequence>1),
 agent_version bigint NOT NULL CHECK(agent_version>1),
 previous_digest text NOT NULL CHECK(previous_digest ~ '^sha256:[0-9a-f]{64}$'),
 state_data bytea NOT NULL CHECK(octet_length(state_data) BETWEEN 2 AND 1048576),
 state_digest text NOT NULL CHECK(state_digest ~ '^sha256:[0-9a-f]{64}$' AND state_digest='sha256:'||encode(sha256(state_data),'hex')),
 runtime_data bytea CHECK(octet_length(runtime_data) BETWEEN 2 AND 65536),
 created_at timestamptz(6) NOT NULL,
 UNIQUE(project_id,agent_id,assignment_sequence),
 UNIQUE(project_id,agent_id,id),
 UNIQUE(project_id,agent_id,id,assignment_sequence,state_digest),
 FOREIGN KEY(project_id,agent_id,id) REFERENCES agenteam_skill.assignment_commands(project_id,agent_id,id) DEFERRABLE INITIALLY DEFERRED
);
CREATE TABLE agenteam_skill.assignment_current (
 project_id agenteam_skill.safe_id NOT NULL,
 agent_id agenteam_skill.safe_id NOT NULL,
 assignment_sequence bigint NOT NULL CHECK(assignment_sequence>1),
 change_id agenteam_skill.safe_id NOT NULL,
 state_digest text NOT NULL CHECK(state_digest ~ '^sha256:[0-9a-f]{64}$'),
 PRIMARY KEY(project_id,agent_id),
 FOREIGN KEY(project_id,agent_id,change_id,assignment_sequence,state_digest)
  REFERENCES agenteam_skill.assignment_changes(project_id,agent_id,id,assignment_sequence,state_digest)
  DEFERRABLE INITIALLY DEFERRED
);
ALTER TABLE agenteam_skill.agent_assignments ADD COLUMN change_id agenteam_skill.safe_id;
ALTER TABLE agenteam_skill.agent_assignments ADD CONSTRAINT skill_grant_change_origin CHECK((assignment_sequence=1)=(change_id IS NULL));
ALTER TABLE agenteam_skill.agent_assignments ADD CONSTRAINT skill_grant_change_identity FOREIGN KEY(project_id,agent_id,change_id) REFERENCES agenteam_skill.assignment_changes(project_id,agent_id,id) DEFERRABLE INITIALLY DEFERRED;
CREATE TABLE agenteam_skill.assignment_pending_refs (
 change_id agenteam_skill.safe_id PRIMARY KEY,
 project_id agenteam_skill.safe_id NOT NULL,
 agent_id agenteam_skill.safe_id NOT NULL,
 execution_id agenteam_skill.safe_id NOT NULL,
 assignment_id agenteam_skill.safe_id NOT NULL,
 assignment_sequence bigint NOT NULL CHECK(assignment_sequence>1),
 skill_id agenteam_skill.safe_id NOT NULL,
 revision_id agenteam_skill.safe_id NOT NULL,
 revision bigint NOT NULL CHECK(revision>0),
 object_id agenteam_skill.safe_id NOT NULL,
 UNIQUE(execution_id,change_id),
 FOREIGN KEY(project_id,agent_id,change_id) REFERENCES agenteam_skill.assignment_changes(project_id,agent_id,id),
 FOREIGN KEY(project_id,agent_id,assignment_id,skill_id,assignment_sequence)
  REFERENCES agenteam_skill.agent_assignments(project_id,agent_id,id,skill_id,assignment_sequence),
 FOREIGN KEY(project_id,skill_id,revision_id,revision,object_id)
  REFERENCES agenteam_skill.revisions(project_id,skill_id,id,revision,object_id)
);
CREATE INDEX skill_pending_revision_protection ON agenteam_skill.assignment_pending_refs(project_id,skill_id,revision_id);

-- +goose StatementBegin
CREATE FUNCTION agenteam_skill.immutable_assignment_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='immutable Skill assignment fact';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER skill_assignment_change_immutable BEFORE UPDATE OR DELETE ON agenteam_skill.assignment_changes FOR EACH ROW EXECUTE FUNCTION agenteam_skill.immutable_assignment_change();
CREATE TRIGGER skill_assignment_pending_immutable BEFORE UPDATE OR DELETE ON agenteam_skill.assignment_pending_refs FOR EACH ROW EXECUTE FUNCTION agenteam_skill.immutable_assignment_change();

-- Counts are complete sets, no longer the initialization-only 0/1 shortcut.
-- The existing deferred count and sequence checks remain in force.
ALTER TABLE agenteam_skill.execution_binding_heads DROP CONSTRAINT execution_binding_heads_binding_count_check;
ALTER TABLE agenteam_skill.execution_binding_heads ADD CONSTRAINT execution_binding_heads_binding_count_check CHECK(binding_count>=0);

CREATE TABLE agenteam_execution.skill_assignment_ingress (
 change_id agenteam_execution.safe_id PRIMARY KEY,
 project_id agenteam_execution.safe_id NOT NULL,
 agent_id agenteam_execution.safe_id NOT NULL,
 execution_id agenteam_execution.safe_id,
 outcome text NOT NULL CHECK(outcome IN ('queued','no_active_execution')),
 change_data bytea NOT NULL CHECK(octet_length(change_data) BETWEEN 2 AND 65536),
 change_digest text NOT NULL CHECK(change_digest ~ '^sha256:[0-9a-f]{64}$' AND change_digest='sha256:'||encode(sha256(change_data),'hex')),
 created_at timestamptz(6) NOT NULL,
 CHECK((outcome='queued')=(execution_id IS NOT NULL)),
 UNIQUE(execution_id,change_id),
 FOREIGN KEY(execution_id,project_id,agent_id) REFERENCES agenteam_execution.executions(id,project_id,agent_id) ON DELETE RESTRICT
);
CREATE TRIGGER execution_skill_assignment_ingress_immutable BEFORE UPDATE OR DELETE ON agenteam_execution.skill_assignment_ingress FOR EACH ROW EXECUTE FUNCTION agenteam_execution.guard_direct_text_immutable();

-- Preserve every released Audit guard while admitting only this new typed
-- Owner assignment tuple. No default Project producer registration is added.
-- +goose StatementBegin
DO $skill_assignment_audit_guards$
DECLARE guard_name text; original_guard text;
BEGIN
 FOREACH guard_name IN ARRAY ARRAY['audit_records_action_check','audit_records_producer_check'] LOOP
  SELECT pg_get_expr(conbin,conrelid) INTO STRICT original_guard FROM pg_constraint
   WHERE conrelid='agenteam_audit.audit_records'::regclass AND conname=guard_name AND contype='c';
  EXECUTE format('ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT %I',guard_name);
  EXECUTE format('ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT %I CHECK ((%s) OR '
   || '(producer=''skill'' AND scope=''project'' AND resource_kind=''agent'' '
   || 'AND action IN (''skill.assign'',''skill.remove'')))',guard_name,original_guard);
 END LOOP;
END;
$skill_assignment_audit_guards$;
-- +goose StatementEnd
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_skill_assignment_contract CHECK ((
 (producer<>'skill' AND action NOT IN ('skill.assign','skill.remove'))
 OR (producer='skill' AND action IN ('skill.assign','skill.remove')
  AND scope='project' AND project_id IS NOT NULL AND actor_kind='human'
  AND user_id IS NOT NULL AND session_id IS NOT NULL AND outcome='success' AND ordinal=0
  AND resource_kind='agent' AND resource_id IS NOT NULL AND cause_ref ~ '^sha256:[0-9a-f]{64}$'
  AND tool_id IS NULL AND execution_id IS NULL AND tool_call_id IS NULL AND operation_id IS NULL
  AND request_id IS NULL AND approval_id IS NULL AND runner_id IS NULL AND correlation_id IS NULL AND http_trace_id IS NULL
  AND jsonb_typeof(metadata)='object' AND octet_length(metadata::text)<=4096
  AND metadata ?& ARRAY['agent_id','assignment_id','skill_id','change_id','version','assignment_sequence','enabled']
  AND metadata-ARRAY['agent_id','assignment_id','skill_id','change_id','version','assignment_sequence','enabled']='{}'::jsonb
  AND jsonb_typeof(metadata->'agent_id')='string' AND metadata->>'agent_id'=resource_id::text
  AND jsonb_typeof(metadata->'assignment_id')='string' AND metadata->>'assignment_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
  AND jsonb_typeof(metadata->'skill_id')='string' AND metadata->>'skill_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
  AND jsonb_typeof(metadata->'change_id')='string' AND metadata->>'change_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
  AND jsonb_typeof(metadata->'version')='string'
  AND CASE WHEN metadata->>'version' ~ '^[1-9][0-9]{0,18}$' THEN (metadata->>'version')::numeric BETWEEN 2 AND 9223372036854775807 ELSE false END
  AND jsonb_typeof(metadata->'assignment_sequence')='string'
  AND CASE WHEN metadata->>'assignment_sequence' ~ '^[1-9][0-9]{0,18}$' THEN (metadata->>'assignment_sequence')::numeric BETWEEN 2 AND 9223372036854775807 ELSE false END
  AND jsonb_typeof(metadata->'enabled')='boolean' AND (action<>'skill.remove' OR metadata->'enabled'='false'::jsonb))
) IS TRUE);

-- +goose StatementBegin
CREATE FUNCTION agenteam_skill.guard_assignment_command() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' OR OLD.state='completed' OR NEW.state<>'completed'
 OR ROW(NEW.id,NEW.project_id,NEW.agent_id,NEW.actor_user_id,NEW.command_name,NEW.idempotency_key,NEW.semantic_digest,NEW.input,NEW.candidate,NEW.created_at)
 IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.agent_id,OLD.actor_user_id,OLD.command_name,OLD.idempotency_key,OLD.semantic_digest,OLD.input,OLD.candidate,OLD.created_at)
 THEN RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='immutable Skill assignment command'; END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER skill_assignment_command_immutable BEFORE UPDATE OR DELETE ON agenteam_skill.assignment_commands FOR EACH ROW EXECUTE FUNCTION agenteam_skill.guard_assignment_command();
-- +goose StatementBegin
CREATE FUNCTION agenteam_skill.guard_assignment_grant() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' OR OLD.removed_at IS NOT NULL OR NEW.removed_at IS NULL
 OR ROW(NEW.id,NEW.project_id,NEW.agent_id,NEW.skill_id,NEW.enabled,NEW.assignment_sequence,NEW.created_at,NEW.change_id)
 IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.agent_id,OLD.skill_id,OLD.enabled,OLD.assignment_sequence,OLD.created_at,OLD.change_id)
 THEN RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='immutable Skill assignment grant'; END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER skill_assignment_grant_immutable BEFORE UPDATE OR DELETE ON agenteam_skill.agent_assignments FOR EACH ROW EXECUTE FUNCTION agenteam_skill.guard_assignment_grant();
