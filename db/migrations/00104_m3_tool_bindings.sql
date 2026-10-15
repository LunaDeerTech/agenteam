-- agenteam:transaction tx
-- +goose Up
-- Extend only the actual M3 backend vocabulary; publication still requires
-- the current trusted adapter and immutable original Execution input.
ALTER TABLE agenteam_tool.operations DROP CONSTRAINT tool_operation_kind;
ALTER TABLE agenteam_tool.operations ADD CONSTRAINT tool_operation_kind CHECK(operation_kind IN ('skill.install','runner.read-file','runner.write-file','runner.run-command','task.read','task.transfer','artifact.list','artifact.create','artifact.read','knowledge.query','knowledge.read','memory.retain','memory.recall','memory.reflect','meeting.list','meeting.request','meeting.add-agent','meeting.remove-agent','meeting.update','meeting.comment','meeting.request-decision','mcp.list-resources','mcp.read-resource','mcp.call'));
ALTER TABLE agenteam_tool.operations DROP CONSTRAINT tool_operation_backend_shape;
ALTER TABLE agenteam_tool.operations ADD CONSTRAINT tool_operation_backend_shape CHECK((
 (operation_kind='skill.install' AND skill_id IS NOT NULL AND backend_key='tool.skill.install:'||operation_id::text)
 OR (operation_kind IN ('runner.read-file','runner.write-file','runner.run-command','task.read','task.transfer','artifact.list','artifact.create','artifact.read','knowledge.query','knowledge.read','memory.retain','memory.recall','memory.reflect','meeting.list','meeting.request','meeting.add-agent','meeting.remove-agent','meeting.update','meeting.comment','meeting.request-decision','mcp.list-resources','mcp.read-resource','mcp.call') AND skill_id IS NULL AND backend_key IS NULL)
) IS TRUE);

ALTER TABLE agenteam_tool.identities DROP CONSTRAINT tool_identity_stable_key;
ALTER TABLE agenteam_tool.identities ADD CONSTRAINT tool_identity_stable_key CHECK(
 stable_key ~ '^builtin:[a-z0-9_.-]{1,128}$'
 OR stable_key IN ('runner:read-file','runner:write-file','runner:run-command')
 OR stable_key ~ '^mcp:[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}:[A-Za-z0-9_.-]{1,128}$');

-- One logical Tool identity may have different discovered definitions in two
-- Projects. The immutable definition history is global; the serving head is not.
CREATE TABLE agenteam_tool.mcp_registrations (
 project_id agenteam_tool.safe_id NOT NULL,
 tool_id agenteam_tool.safe_id NOT NULL,
 spec_revision bigint NOT NULL,
 config_id agenteam_tool.safe_id NOT NULL,
 connection_id agenteam_tool.safe_id NOT NULL,
 runtime_binding_id agenteam_tool.safe_id NOT NULL,
 config_revision bigint NOT NULL CHECK(config_revision>0),
 remote_tool_name text NOT NULL CHECK(remote_tool_name ~ '^[A-Za-z0-9_.-]{1,128}$'),
 default_enabled boolean NOT NULL,
 active boolean NOT NULL,
 PRIMARY KEY(project_id,tool_id),
 UNIQUE(project_id,config_id,remote_tool_name),
 FOREIGN KEY(tool_id,spec_revision) REFERENCES agenteam_tool.spec_revisions(tool_id,spec_revision),
 FOREIGN KEY(connection_id,project_id) REFERENCES agenteam_mcp.connections(id,project_id) ON DELETE CASCADE,
 FOREIGN KEY(runtime_binding_id,project_id,connection_id) REFERENCES agenteam_mcp.runtime_bindings(id,project_id,connection_id)
);
CREATE INDEX mcp_tool_connection ON agenteam_tool.mcp_registrations(project_id,connection_id,tool_id);

ALTER TABLE agenteam_tool.execution_references
 ADD COLUMN mcp_project_id agenteam_tool.safe_id,
 ADD COLUMN mcp_config_id agenteam_tool.safe_id,
 ADD COLUMN mcp_connection_id agenteam_tool.safe_id,
 ADD COLUMN mcp_runtime_binding_id agenteam_tool.safe_id,
 ADD COLUMN mcp_config_revision bigint,
 ADD COLUMN mcp_remote_tool_name text;
-- Preserve the previous Builtin/Runner constraint byte for byte while adding
-- exactly one MCP branch, rather than weakening existing backend checks.
-- +goose StatementBegin
DO $$
DECLARE old_expr text;
BEGIN
 SELECT pg_get_expr(conbin,conrelid) INTO STRICT old_expr FROM pg_constraint
 WHERE conrelid='agenteam_tool.execution_references'::regclass AND conname='tool_execution_reference_binding' AND contype='c';
 ALTER TABLE agenteam_tool.execution_references DROP CONSTRAINT tool_execution_reference_binding;
 EXECUTE format('ALTER TABLE agenteam_tool.execution_references ADD CONSTRAINT tool_execution_reference_binding CHECK((((%s) AND mcp_project_id IS NULL AND mcp_config_id IS NULL AND mcp_connection_id IS NULL AND mcp_runtime_binding_id IS NULL AND mcp_config_revision IS NULL AND mcp_remote_tool_name IS NULL) OR (binding_kind=''mcp'' AND handler_id IS NULL AND contract_revision IS NULL AND operation_name IS NULL AND operation_revision IS NULL AND protocol_major IS NULL AND scope_resolver_id=''mcp.project'' AND risk_classifier_id=''mcp.external'' AND class=''ordinary'' AND mcp_project_id IS NOT NULL AND mcp_config_id IS NOT NULL AND mcp_connection_id IS NOT NULL AND mcp_runtime_binding_id IS NOT NULL AND mcp_config_revision>0 AND mcp_remote_tool_name ~ ''^[A-Za-z0-9_.-]{1,128}$'')) IS TRUE)',old_expr);
END $$;
-- +goose StatementEnd
ALTER TABLE agenteam_tool.execution_references ADD CONSTRAINT tool_execution_mcp_runtime
 FOREIGN KEY(mcp_runtime_binding_id,mcp_project_id,mcp_connection_id) REFERENCES agenteam_mcp.runtime_bindings(id,project_id,connection_id);

ALTER TABLE agenteam_tool.execution_configurations ADD CONSTRAINT tool_capture_execution_project UNIQUE(execution_id,project_id);
ALTER TABLE agenteam_tool.execution_references ADD CONSTRAINT tool_execution_mcp_project
 FOREIGN KEY(execution_id,mcp_project_id) REFERENCES agenteam_tool.execution_configurations(execution_id,project_id);
-- The original identity trigger still protects Builtin and Runner. This second
-- trigger binds MCP names and immutable runtime metadata to the same Project.
-- +goose StatementBegin
CREATE FUNCTION agenteam_tool.check_mcp_execution_binding() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE stable text;
BEGIN
 IF NEW.binding_kind<>'mcp' THEN RETURN NEW; END IF;
 SELECT stable_key INTO STRICT stable FROM agenteam_tool.identities WHERE tool_id=NEW.tool_id;
 IF stable <> 'mcp:'||NEW.mcp_config_id::text||':'||NEW.mcp_remote_tool_name OR NOT EXISTS (
  SELECT 1 FROM agenteam_mcp.runtime_bindings b WHERE b.id=NEW.mcp_runtime_binding_id
   AND b.project_id=NEW.mcp_project_id AND b.connection_id=NEW.mcp_connection_id
   AND b.config_id=NEW.mcp_config_id AND b.config_revision=NEW.mcp_config_revision
 ) THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='tool_mcp_binding_identity', MESSAGE='invalid MCP binding identity';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER tool_execution_mcp_binding BEFORE INSERT ON agenteam_tool.execution_references
 FOR EACH ROW EXECUTE FUNCTION agenteam_tool.check_mcp_execution_binding();

ALTER TABLE agenteam_secret.secret_leases DROP CONSTRAINT secret_leases_owner_kind_check;
ALTER TABLE agenteam_secret.secret_leases ADD CONSTRAINT secret_leases_owner_kind_check
 CHECK(owner_kind IN ('execution','model_call','account_delivery_attempt','account_response','mcp_connection_request'));
ALTER TABLE agenteam_secret.secret_leases ADD CONSTRAINT mcp_connection_lease_purpose
 CHECK(owner_kind<>'mcp_connection_request' OR consumer='mcp');

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_tool.check_builtin_result() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE target agenteam_tool.operations.operation_id%TYPE; k text; ph text; active agenteam_tool.operations.active_attempt_id%TYPE; total bigint; matching bigint;
BEGIN
 target:=NEW.operation_id;
 SELECT operation_kind,phase,active_attempt_id INTO STRICT k,ph,active FROM agenteam_tool.operations WHERE operation_id=target;
 SELECT count(*) INTO total FROM agenteam_tool.builtin_results WHERE operation_id=target;
 IF k IN ('task.read','task.transfer','artifact.list','artifact.create','artifact.read','knowledge.query','knowledge.read','memory.retain','memory.recall','memory.reflect','meeting.list','meeting.request','meeting.add-agent','meeting.remove-agent','meeting.update','meeting.comment','meeting.request-decision','mcp.list-resources','mcp.read-resource','mcp.call') AND ph IN ('succeeded','failed','cancelled') THEN
  SELECT count(*) INTO matching FROM agenteam_tool.builtin_results r JOIN agenteam_tool.attempts a
   ON(a.attempt_id,a.operation_id,a.execution_id,a.project_id,a.agent_id,a.backend_request_id)=(r.attempt_id,r.operation_id,r.execution_id,r.project_id,r.agent_id,r.request_id)
   WHERE r.operation_id=target AND r.attempt_id=active AND a.phase=ph AND a.retired;
  IF total<>1 OR matching<>1 THEN RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='tool_builtin_result_complete',MESSAGE='incomplete builtin result'; END IF;
 ELSIF total<>0 THEN
  RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='tool_builtin_result_complete',MESSAGE='unexpected builtin result';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
