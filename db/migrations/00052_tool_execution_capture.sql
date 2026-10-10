-- agenteam:transaction tx
-- +goose Up
-- Tool owns immutable metadata/name/binding references, not Execution status,
-- Snapshot completion or runtime permission. Even an empty set has an actual
-- execution parent and a head written only after the same-Tx preparing proof.
CREATE TABLE agenteam_tool.execution_configurations (
 execution_id agenteam_tool.safe_id PRIMARY KEY,
 project_id agenteam_tool.safe_id NOT NULL,
 agent_id agenteam_tool.safe_id NOT NULL,
 agent_version bigint NOT NULL CHECK(agent_version>0),
 attempt_binding text NOT NULL CHECK(attempt_binding ~ '^sha256:[0-9a-f]{64}$'),
 capture_digest text NOT NULL CHECK(capture_digest ~ '^sha256:[0-9a-f]{64}$'),
 tool_count integer NOT NULL CHECK(tool_count BETWEEN 0 AND 128),
 FOREIGN KEY(execution_id,project_id,agent_id)
  REFERENCES agenteam_execution.executions(id,project_id,agent_id) ON DELETE RESTRICT
);

CREATE TABLE agenteam_tool.execution_references (
 execution_id agenteam_tool.safe_id NOT NULL
  REFERENCES agenteam_tool.execution_configurations(execution_id) ON DELETE RESTRICT,
 tool_id agenteam_tool.safe_id NOT NULL,
 spec_revision bigint NOT NULL CHECK(spec_revision>0),
 model_visible_name text COLLATE "C" NOT NULL
  CHECK(model_visible_name='tool_' || replace(tool_id::text,'-','')),
 handler_id text NOT NULL CHECK(handler_id ~ '^[a-z0-9_.-]{1,128}$'),
 contract_revision bigint NOT NULL CHECK(contract_revision>0),
 scope_resolver_id text NOT NULL CHECK(scope_resolver_id ~ '^[a-z0-9_.-]{1,128}$'),
 risk_classifier_id text NOT NULL CHECK(risk_classifier_id ~ '^[a-z0-9_.-]{1,128}$'),
 class text NOT NULL CHECK(class IN ('ordinary','core')),
 PRIMARY KEY(execution_id,tool_id),
 UNIQUE(execution_id,model_visible_name),
 FOREIGN KEY(tool_id,spec_revision)
  REFERENCES agenteam_tool.spec_revisions(tool_id,spec_revision) ON DELETE RESTRICT
);
CREATE INDEX tool_execution_spec_references
 ON agenteam_tool.execution_references(tool_id,spec_revision,execution_id);

-- A future retirement path needs an explicit owner/retention contract. This
-- slice supplies none: deleting a registration cannot change or release the
-- original execution's fixed metadata. Outer rollback needs no release API.
-- +goose StatementBegin
CREATE FUNCTION agenteam_tool.immutable_execution_capture() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'TOOL_EXECUTION_CAPTURE_IMMUTABLE' USING ERRCODE='23514';
END $$;
-- +goose StatementEnd
CREATE TRIGGER tool_execution_configuration_immutable
 BEFORE UPDATE OR DELETE ON agenteam_tool.execution_configurations
 FOR EACH ROW EXECUTE FUNCTION agenteam_tool.immutable_execution_capture();
CREATE TRIGGER tool_execution_reference_immutable
 BEFORE UPDATE OR DELETE ON agenteam_tool.execution_references
 FOR EACH ROW EXECUTE FUNCTION agenteam_tool.immutable_execution_capture();

-- Both insert directions check the complete final set, including zero. This is
-- deferred because the head precedes its references inside the one caller Tx.
-- +goose StatementBegin
CREATE FUNCTION agenteam_tool.check_execution_capture_count() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE expected integer; actual bigint;
BEGIN
 SELECT tool_count INTO STRICT expected FROM agenteam_tool.execution_configurations
  WHERE execution_id=NEW.execution_id;
 SELECT count(*) INTO actual FROM agenteam_tool.execution_references
  WHERE execution_id=NEW.execution_id;
 IF actual<>expected THEN
  RAISE EXCEPTION 'TOOL_EXECUTION_CAPTURE_COUNT' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER tool_execution_configuration_count
 AFTER INSERT ON agenteam_tool.execution_configurations DEFERRABLE INITIALLY DEFERRED
 FOR EACH ROW EXECUTE FUNCTION agenteam_tool.check_execution_capture_count();
CREATE CONSTRAINT TRIGGER tool_execution_reference_count
 AFTER INSERT ON agenteam_tool.execution_references DEFERRABLE INITIALLY DEFERRED
 FOR EACH ROW EXECUTE FUNCTION agenteam_tool.check_execution_capture_count();
