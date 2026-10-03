-- agenteam:transaction tx
-- +goose Up
CREATE SCHEMA agenteam_audit;
CREATE TABLE agenteam_audit.audit_records (
  id uuid PRIMARY KEY,
  created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
  scope text NOT NULL CHECK (scope IN ('system','project')),
  project_id uuid,
  scope_key text GENERATED ALWAYS AS (coalesce(project_id::text,'system')) STORED,
  actor_kind text NOT NULL CHECK (actor_kind IN ('human','agent_run','service')),
  user_id uuid, session_id uuid, actor_project_id uuid, agent_id uuid, actor_execution_id uuid,
  actor_id uuid GENERATED ALWAYS AS (coalesce(user_id,agent_id)) STORED,
  service_name text, service_cause text,
  action text NOT NULL CHECK (action IN ('secret.create','secret.update','secret.delete','secret.resolve','secret.master.register','secret.master.rotation.start','secret.master.rotation.complete','secret.master.rotation.failed','outbound.policy.update','outbound.access.deny')),
  outcome text NOT NULL CHECK (outcome IN ('success','denied','failed','unknown')),
  resource_kind text NOT NULL CHECK (resource_kind IN ('secret','secret_master','secret_rotation','outbound_policy','agent')),
  resource_id uuid,
  metadata jsonb NOT NULL CHECK (jsonb_typeof(metadata)='object' AND octet_length(metadata::text)<=4096),
  semantic_digest text NOT NULL CHECK (semantic_digest ~ '^sha256:[0-9a-f]{64}$'),
  producer text NOT NULL CHECK (producer IN ('secret','secret.master','outbound.policy','outbound.access')),
  cause_ref text NOT NULL CHECK (cause_ref ~ '^(sha256:[0-9a-f]{64}|[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12})$'),
  ordinal bigint NOT NULL CHECK (ordinal>=0),
  tool_id uuid, execution_id uuid, tool_call_id uuid, operation_id uuid, request_id uuid,
  approval_id uuid, runner_id uuid, correlation_id uuid, http_trace_id uuid,
  UNIQUE (scope,scope_key,producer,cause_ref,ordinal),
  CHECK ((scope='system' AND project_id IS NULL) OR (scope='project' AND project_id IS NOT NULL)),
  CHECK ((actor_kind='human' AND user_id IS NOT NULL AND session_id IS NOT NULL AND actor_project_id IS NULL AND agent_id IS NULL AND actor_execution_id IS NULL AND service_name IS NULL AND service_cause IS NULL)
    OR (actor_kind='agent_run' AND user_id IS NULL AND session_id IS NULL AND actor_project_id IS NOT NULL AND agent_id IS NOT NULL AND actor_execution_id IS NOT NULL AND service_name IS NULL AND service_cause IS NULL AND scope='project' AND actor_project_id=project_id AND execution_id=actor_execution_id)
    OR (actor_kind='service' AND user_id IS NULL AND session_id IS NULL AND agent_id IS NULL AND actor_execution_id IS NULL AND service_name IN ('secret','secret-maintenance','outbound','project-lifecycle') AND service_cause IS NOT NULL AND actor_project_id IS NOT DISTINCT FROM project_id)),
  CHECK ((resource_kind IN ('secret_master','outbound_policy') AND resource_id IS NULL) OR (resource_kind IN ('secret','secret_rotation','agent') AND resource_id IS NOT NULL)),
  CHECK (scope='project' OR (execution_id IS NULL AND tool_call_id IS NULL AND operation_id IS NULL AND approval_id IS NULL AND resource_kind<>'agent'))
);

CREATE INDEX audit_project_time ON agenteam_audit.audit_records(project_id,created_at DESC,id DESC) WHERE scope='project';
CREATE INDEX audit_system_time ON agenteam_audit.audit_records(created_at DESC,id DESC) WHERE scope='system';
CREATE INDEX audit_project_actor ON agenteam_audit.audit_records(project_id,actor_kind,actor_id,created_at DESC,id DESC) WHERE scope='project';
CREATE INDEX audit_system_actor ON agenteam_audit.audit_records(actor_kind,actor_id,created_at DESC,id DESC) WHERE scope='system';
CREATE INDEX audit_project_action ON agenteam_audit.audit_records(project_id,action,created_at DESC,id DESC) WHERE scope='project';
CREATE INDEX audit_system_action ON agenteam_audit.audit_records(action,created_at DESC,id DESC) WHERE scope='system';
CREATE INDEX audit_project_resource ON agenteam_audit.audit_records(project_id,resource_kind,resource_id,created_at DESC,id DESC) WHERE scope='project';
CREATE INDEX audit_system_resource ON agenteam_audit.audit_records(resource_kind,resource_id,created_at DESC,id DESC) WHERE scope='system';
CREATE INDEX audit_project_agent ON agenteam_audit.audit_records(project_id,agent_id,created_at DESC,id DESC) WHERE scope='project' AND actor_kind='agent_run';
CREATE INDEX audit_project_tool ON agenteam_audit.audit_records(project_id,tool_id,created_at DESC,id DESC) WHERE scope='project' AND tool_id IS NOT NULL;
CREATE INDEX audit_system_tool ON agenteam_audit.audit_records(tool_id,created_at DESC,id DESC) WHERE scope='system' AND tool_id IS NOT NULL;
CREATE INDEX audit_project_execution ON agenteam_audit.audit_records(project_id,execution_id,created_at DESC,id DESC) WHERE scope='project' AND execution_id IS NOT NULL;
CREATE INDEX audit_project_operation ON agenteam_audit.audit_records(project_id,operation_id,created_at DESC,id DESC) WHERE scope='project' AND operation_id IS NOT NULL;
CREATE INDEX audit_project_approval ON agenteam_audit.audit_records(project_id,approval_id,created_at DESC,id DESC) WHERE scope='project' AND approval_id IS NOT NULL;
CREATE INDEX audit_project_runner ON agenteam_audit.audit_records(project_id,runner_id,created_at DESC,id DESC) WHERE scope='project' AND runner_id IS NOT NULL;
CREATE INDEX audit_system_runner ON agenteam_audit.audit_records(runner_id,created_at DESC,id DESC) WHERE scope='system' AND runner_id IS NOT NULL;
