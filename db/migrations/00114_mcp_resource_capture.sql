-- agenteam:transaction tx
-- +goose Up
-- Resource capability capture is distinct from remote Tool capture. A complete
-- empty set has a head, and old executions never read today's live connections.
CREATE TABLE agenteam_tool.execution_mcp_resource_captures (
 execution_id agenteam_tool.safe_id PRIMARY KEY,
 project_id agenteam_tool.safe_id NOT NULL,
 agent_id agenteam_tool.safe_id NOT NULL,
 connection_count integer NOT NULL CHECK(connection_count BETWEEN 0 AND 64),
 bindings_digest text NOT NULL CHECK(bindings_digest ~ '^sha256:[0-9a-f]{64}$'),
 UNIQUE(execution_id,project_id),
 FOREIGN KEY(execution_id,project_id) REFERENCES agenteam_tool.execution_configurations(execution_id,project_id) ON DELETE RESTRICT,
 FOREIGN KEY(execution_id,project_id,agent_id) REFERENCES agenteam_execution.executions(id,project_id,agent_id) ON DELETE RESTRICT
);
CREATE TABLE agenteam_tool.execution_mcp_resources (
 execution_id agenteam_tool.safe_id NOT NULL,
 project_id agenteam_tool.safe_id NOT NULL,
 config_id agenteam_tool.safe_id NOT NULL,
 connection_id agenteam_tool.safe_id NOT NULL,
 runtime_binding_id agenteam_tool.safe_id NOT NULL,
 config_revision bigint NOT NULL CHECK(config_revision>0),
 PRIMARY KEY(execution_id,connection_id),
 UNIQUE(execution_id,runtime_binding_id),
 FOREIGN KEY(execution_id,project_id) REFERENCES agenteam_tool.execution_mcp_resource_captures(execution_id,project_id) ON DELETE RESTRICT,
 FOREIGN KEY(runtime_binding_id,project_id,connection_id) REFERENCES agenteam_mcp.runtime_bindings(id,project_id,connection_id) ON DELETE RESTRICT
);
CREATE TRIGGER tool_execution_mcp_resource_head_immutable BEFORE UPDATE OR DELETE ON agenteam_tool.execution_mcp_resource_captures
 FOR EACH ROW EXECUTE FUNCTION agenteam_tool.immutable_execution_capture();
CREATE TRIGGER tool_execution_mcp_resource_immutable BEFORE UPDATE OR DELETE ON agenteam_tool.execution_mcp_resources
 FOR EACH ROW EXECUTE FUNCTION agenteam_tool.immutable_execution_capture();
-- +goose StatementBegin
CREATE FUNCTION agenteam_tool.check_mcp_resource_capture() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE expected integer; actual bigint;
BEGIN
 SELECT connection_count INTO STRICT expected FROM agenteam_tool.execution_mcp_resource_captures WHERE execution_id=NEW.execution_id;
 SELECT count(*) INTO actual FROM agenteam_tool.execution_mcp_resources WHERE execution_id=NEW.execution_id;
 IF actual<>expected OR NOT EXISTS (
  SELECT 1 FROM agenteam_tool.execution_references WHERE execution_id=NEW.execution_id
   AND binding_kind='builtin' AND handler_id IN ('mcp.list-resources','mcp.read-resource') AND contract_revision=1 AND class='core'
 ) OR EXISTS (
  SELECT 1 FROM agenteam_tool.execution_mcp_resources r JOIN agenteam_mcp.runtime_bindings b ON b.id=r.runtime_binding_id
   WHERE r.execution_id=NEW.execution_id AND (b.project_id<>r.project_id OR b.connection_id<>r.connection_id OR b.config_id<>r.config_id OR b.config_revision<>r.config_revision)
 ) THEN
  RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='tool_mcp_resource_capture',MESSAGE='invalid MCP resource capture';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER tool_mcp_resource_capture_head_complete AFTER INSERT ON agenteam_tool.execution_mcp_resource_captures
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION agenteam_tool.check_mcp_resource_capture();
CREATE CONSTRAINT TRIGGER tool_mcp_resource_capture_complete AFTER INSERT ON agenteam_tool.execution_mcp_resources
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION agenteam_tool.check_mcp_resource_capture();
