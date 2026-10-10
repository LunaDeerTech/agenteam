-- agenteam:transaction tx
-- +goose Up
-- Agent owns canonical configuration and command history. Resource providers
-- own reverse references; no cross-domain FK or cascade substitutes authority.
CREATE SCHEMA agenteam_agent;
CREATE DOMAIN agenteam_agent.safe_id AS uuid CHECK
 (VALUE::text ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$');

CREATE TABLE agenteam_agent.agents (
 id agenteam_agent.safe_id PRIMARY KEY,
 project_id agenteam_agent.safe_id NOT NULL,
 name text COLLATE "C" NOT NULL CHECK(octet_length(name) BETWEEN 3 AND 32 AND name ~ '^[A-Za-z0-9][A-Za-z0-9-]{1,30}[A-Za-z0-9]$'),
 normalized_name text COLLATE "C" NOT NULL CHECK(normalized_name=lower(name COLLATE "C")),
 display_name text CHECK(display_name IS NULL OR (octet_length(display_name) BETWEEN 1 AND 1024 AND char_length(display_name)<=256)),
 tag_color text CHECK(tag_color IS NULL OR tag_color ~ '^#[0-9a-f]{6}$'),
 description text NOT NULL CHECK(octet_length(description)<=8192),
 instructions text NOT NULL CHECK(octet_length(instructions)<=32768),
 inject_agents_md boolean NOT NULL,
 model_id agenteam_agent.safe_id NOT NULL,
 reasoning_effort text CHECK(reasoning_effort IS NULL OR (octet_length(reasoning_effort) BETWEEN 1 AND 32 AND reasoning_effort ~ '^[A-Za-z0-9_.:-]+$')),
 approval_policy text NOT NULL CHECK(approval_policy IN ('default','auto','allow')),
 approval_model_id agenteam_agent.safe_id,
 lifecycle text NOT NULL CHECK(lifecycle IN ('active','deleting')),
 version bigint NOT NULL CHECK(version>=1),
 created_at timestamptz(6) NOT NULL,
 updated_at timestamptz(6) NOT NULL CHECK(updated_at>=created_at),
 CONSTRAINT agents_project_identity UNIQUE(project_id,id),
 CONSTRAINT agents_project_name UNIQUE(project_id,normalized_name),
 CONSTRAINT agents_approval_model CHECK((approval_policy='auto')=(approval_model_id IS NOT NULL))
);

-- +goose StatementBegin
CREATE FUNCTION agenteam_agent.reject_command_rewrite() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.state='completed'
 OR ROW(NEW.id,NEW.project_id,NEW.actor_user_id,NEW.target_id,NEW.command_name,NEW.idempotency_key,NEW.semantic_digest,NEW.input,NEW.expected_version,NEW.created_at)
 IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.actor_user_id,OLD.target_id,OLD.command_name,OLD.idempotency_key,OLD.semantic_digest,OLD.input,OLD.expected_version,OLD.created_at) THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='commands_immutable', MESSAGE='immutable agent command';
 END IF;
 IF NEW.state='completed' THEN
  IF ROW(NEW.plan_revision,NEW.before_config,NEW.after_config,NEW.add_skills_enabled,NEW.install_skill_enabled,NEW.changed_fields)
  IS DISTINCT FROM ROW(OLD.plan_revision,OLD.before_config,OLD.after_config,OLD.add_skills_enabled,OLD.install_skill_enabled,OLD.changed_fields) THEN
   RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='commands_plan', MESSAGE='agent command plan changed';
  END IF;
 ELSIF OLD.plan_revision=9223372036854775807 OR NEW.plan_revision<>OLD.plan_revision+1 THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='commands_plan', MESSAGE='agent command plan revision';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TABLE agenteam_agent.tool_allowlist (
 project_id agenteam_agent.safe_id NOT NULL,
 agent_id agenteam_agent.safe_id NOT NULL,
 tool_id agenteam_agent.safe_id NOT NULL,
 PRIMARY KEY(project_id,agent_id,tool_id),
 FOREIGN KEY(project_id,agent_id) REFERENCES agenteam_agent.agents(project_id,id) ON DELETE RESTRICT
);
CREATE TABLE agenteam_agent.mount_allowlist (
 project_id agenteam_agent.safe_id NOT NULL,
 agent_id agenteam_agent.safe_id NOT NULL,
 mount_id agenteam_agent.safe_id NOT NULL,
 PRIMARY KEY(project_id,agent_id,mount_id),
 FOREIGN KEY(project_id,agent_id) REFERENCES agenteam_agent.agents(project_id,id) ON DELETE RESTRICT
);
CREATE TABLE agenteam_agent.secret_allowlist (
 project_id agenteam_agent.safe_id NOT NULL,
 agent_id agenteam_agent.safe_id NOT NULL,
 variable_id agenteam_agent.safe_id NOT NULL,
 PRIMARY KEY(project_id,agent_id,variable_id),
 FOREIGN KEY(project_id,agent_id) REFERENCES agenteam_agent.agents(project_id,id) ON DELETE RESTRICT
);

-- Canonical identity is immutable. No-op commands leave the row untouched;
-- real changes use exactly the next version and a strictly later DB instant.
-- +goose StatementBegin
CREATE FUNCTION agenteam_agent.reject_agent_rewrite() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ROW(NEW.id,NEW.project_id,NEW.created_at) IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.created_at)
 OR OLD.version=9223372036854775807 OR NEW.version<>OLD.version+1 OR NEW.updated_at<=OLD.updated_at THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='agents_immutable', MESSAGE='immutable agent identity';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER agents_immutable BEFORE UPDATE ON agenteam_agent.agents
 FOR EACH ROW EXECUTE FUNCTION agenteam_agent.reject_agent_rewrite();

CREATE TABLE agenteam_agent.commands (
 id agenteam_agent.safe_id PRIMARY KEY,
 project_id agenteam_agent.safe_id NOT NULL,
 actor_user_id agenteam_agent.safe_id NOT NULL,
 target_id agenteam_agent.safe_id NOT NULL,
 command_name text NOT NULL CHECK(command_name IN ('agent.create','agent.update')),
 idempotency_key text NOT NULL CHECK(octet_length(idempotency_key) BETWEEN 1 AND 128 AND idempotency_key ~ '^[A-Za-z0-9._:/-]+$'),
 semantic_digest text NOT NULL CHECK(semantic_digest ~ '^sha256:[0-9a-f]{64}$'),
 input jsonb NOT NULL CHECK(jsonb_typeof(input)='object' AND octet_length(input::text)<=524288),
 expected_version bigint CHECK(expected_version IS NULL OR expected_version>=1),
 plan_revision bigint NOT NULL CHECK(plan_revision>=1),
 before_config jsonb CHECK(before_config IS NULL OR (jsonb_typeof(before_config)='object' AND octet_length(before_config::text)<=524288)),
 after_config jsonb NOT NULL CHECK(jsonb_typeof(after_config)='object' AND octet_length(after_config::text)<=524288),
 add_skills_enabled boolean,
 install_skill_enabled boolean,
 changed_fields jsonb NOT NULL CHECK(jsonb_typeof(changed_fields)='array' AND jsonb_array_length(changed_fields)<=17),
 state text NOT NULL CHECK(state IN ('planned','completed')),
 receipt jsonb CHECK(receipt IS NULL OR (jsonb_typeof(receipt)='object' AND octet_length(receipt::text)<=524288)),
 created_at timestamptz(6) NOT NULL,
 committed_at timestamptz(6) CHECK(committed_at IS NULL OR committed_at>=created_at),
 CONSTRAINT commands_identity UNIQUE(project_id,command_name,idempotency_key),
 CONSTRAINT commands_project_identity UNIQUE(project_id,id),
 CONSTRAINT commands_create_update CHECK(
  (command_name='agent.create' AND expected_version IS NULL AND before_config IS NULL AND add_skills_enabled IS NOT NULL AND install_skill_enabled IS NOT NULL)
  OR (command_name='agent.update' AND expected_version IS NOT NULL AND before_config IS NOT NULL AND add_skills_enabled IS NULL AND install_skill_enabled IS NULL)),
 CONSTRAINT commands_outcome CHECK((state='planned' AND receipt IS NULL AND committed_at IS NULL)
  OR (state='completed' AND receipt IS NOT NULL AND committed_at IS NOT NULL))
);
CREATE TRIGGER commands_immutable BEFORE UPDATE ON agenteam_agent.commands
 FOR EACH ROW EXECUTE FUNCTION agenteam_agent.reject_command_rewrite();

-- Keep every released Audit predicate, including Knowledge, Runner and Secret
-- additions. Existing actions using resource_kind=agent retain their own rules.
-- Only the exact new tuple is admitted to the two shared closed lists.
-- +goose StatementBegin
DO $agent_audit_guards$
DECLARE guard_name text; original_guard text;
BEGIN
 FOREACH guard_name IN ARRAY ARRAY['audit_records_action_check','audit_records_producer_check'] LOOP
  SELECT pg_get_expr(conbin,conrelid) INTO STRICT original_guard FROM pg_constraint
   WHERE conrelid='agenteam_audit.audit_records'::regclass
    AND conname=guard_name AND contype='c';
  EXECUTE format('ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT %I',guard_name);
  EXECUTE format('ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT %I CHECK ((%s) OR '
   || '(producer=''agent'' AND scope=''project'' AND resource_kind=''agent'' '
   || 'AND action IN (''agent.create'',''agent.update'')))',guard_name,original_guard);
 END LOOP;
END;
$agent_audit_guards$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION agenteam_agent.valid_audit_fields(action text, fields jsonb) RETURNS boolean
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
 SELECT CASE WHEN jsonb_typeof(fields)='array' AND action IN ('agent.create','agent.update') THEN
  jsonb_array_length(fields) BETWEEN 1 AND 13
  AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(fields) item WHERE
   jsonb_typeof(item)<>'string' OR item#>>'{}' NOT IN
    ('allowed_mount_ids','allowed_secret_variable_ids','allowed_tool_ids','approval_model_ref',
     'approval_policy','description','display_name','inject_agents_md','instructions','model_ref',
     'name','reasoning_effort','tag_color'))
  AND fields=coalesce((SELECT jsonb_agg(item ORDER BY item COLLATE "C") FROM
   (SELECT DISTINCT item#>>'{}' AS item FROM jsonb_array_elements(fields) item) ordered),'[]'::jsonb)
  AND (action='agent.update' OR fields=
   '["allowed_mount_ids","allowed_secret_variable_ids","allowed_tool_ids","approval_model_ref","approval_policy","description","display_name","inject_agents_md","instructions","model_ref","name","reasoning_effort","tag_color"]'::jsonb)
 ELSE false END
$$;
-- +goose StatementEnd

ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_agent_contract CHECK ((
 (producer<>'agent' AND action NOT IN ('agent.create','agent.update'))
 OR (producer='agent' AND action IN ('agent.create','agent.update')
  AND scope='project' AND project_id IS NOT NULL AND actor_kind='human'
  AND user_id IS NOT NULL AND session_id IS NOT NULL AND outcome='success' AND ordinal=0
  AND resource_kind='agent' AND resource_id IS NOT NULL AND cause_ref ~ '^sha256:[0-9a-f]{64}$'
  AND tool_id IS NULL AND execution_id IS NULL AND tool_call_id IS NULL AND operation_id IS NULL
  AND request_id IS NULL AND approval_id IS NULL AND runner_id IS NULL
  AND correlation_id IS NULL AND http_trace_id IS NULL
  AND jsonb_typeof(metadata)='object' AND octet_length(metadata::text)<=4096
  AND metadata ?& ARRAY['agent_id','version','command_id','changed_fields']
  AND metadata-ARRAY['agent_id','version','command_id','changed_fields']='{}'::jsonb
  AND jsonb_typeof(metadata->'agent_id')='string' AND metadata->>'agent_id'=resource_id::text
  AND jsonb_typeof(metadata->'command_id')='string'
  AND metadata->>'command_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
  AND jsonb_typeof(metadata->'version')='string'
  AND CASE WHEN metadata->>'version' ~ '^[1-9][0-9]{0,18}$'
   THEN (metadata->>'version')::numeric<=9223372036854775807 ELSE false END
  AND ((action='agent.create' AND metadata->>'version'='1')
   OR (action='agent.update' AND metadata->>'version'<>'1'))
  AND agenteam_agent.valid_audit_fields(action,metadata->'changed_fields'))
) IS TRUE);
