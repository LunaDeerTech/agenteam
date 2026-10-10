-- agenteam:transaction tx
-- +goose Up
-- Tool owns stable identity, immutable definitions, current registrations and
-- its side of Agent configuration references. No seed pretends to bind a tool.
CREATE SCHEMA agenteam_tool;
CREATE DOMAIN agenteam_tool.safe_id AS text
 CHECK (VALUE ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$');

CREATE TABLE agenteam_tool.identities (
 tool_id agenteam_tool.safe_id PRIMARY KEY,
 stable_key text NOT NULL UNIQUE CHECK (stable_key ~ '^builtin:[a-z0-9_.-]{1,128}$'),
 latest_revision bigint NOT NULL CHECK (latest_revision > 0),
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE agenteam_tool.spec_revisions (
 tool_id agenteam_tool.safe_id NOT NULL REFERENCES agenteam_tool.identities(tool_id),
 spec_revision bigint NOT NULL CHECK (spec_revision > 0),
 definition jsonb NOT NULL CHECK ((jsonb_typeof(definition)='object' AND octet_length(definition::text)<=131072) IS TRUE),
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tool_id,spec_revision)
);
ALTER TABLE agenteam_tool.identities ADD CONSTRAINT tool_latest_spec
 FOREIGN KEY(tool_id,latest_revision) REFERENCES agenteam_tool.spec_revisions(tool_id,spec_revision)
 DEFERRABLE INITIALLY DEFERRED;

-- +goose StatementBegin
CREATE FUNCTION agenteam_tool.immutable_spec() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'TOOL_SPEC_IMMUTABLE' USING ERRCODE='23514';
END $$;
-- +goose StatementEnd
CREATE TRIGGER tool_spec_immutable BEFORE UPDATE OR DELETE ON agenteam_tool.spec_revisions
 FOR EACH ROW EXECUTE FUNCTION agenteam_tool.immutable_spec();
-- +goose StatementBegin
CREATE FUNCTION agenteam_tool.stable_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'TOOL_IDENTITY_IMMUTABLE' USING ERRCODE='23514'; END IF;
 IF NEW.tool_id IS DISTINCT FROM OLD.tool_id OR NEW.stable_key IS DISTINCT FROM OLD.stable_key OR NEW.created_at IS DISTINCT FROM OLD.created_at OR NEW.latest_revision < OLD.latest_revision THEN
  RAISE EXCEPTION 'TOOL_IDENTITY_IMMUTABLE' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER tool_identity_stable BEFORE UPDATE OR DELETE ON agenteam_tool.identities
 FOR EACH ROW EXECUTE FUNCTION agenteam_tool.stable_identity();

CREATE TABLE agenteam_tool.registrations (
 tool_id agenteam_tool.safe_id PRIMARY KEY,
 spec_revision bigint NOT NULL,
 handler_id text NOT NULL CHECK(handler_id ~ '^[a-z0-9_.-]{1,128}$'),
 contract_revision bigint NOT NULL CHECK(contract_revision>0),
 scope_resolver_id text NOT NULL CHECK(scope_resolver_id ~ '^[a-z0-9_.-]{1,128}$'),
 risk_classifier_id text NOT NULL CHECK(risk_classifier_id ~ '^[a-z0-9_.-]{1,128}$'),
 class text NOT NULL CHECK(class IN ('ordinary','core')),
 FOREIGN KEY(tool_id,spec_revision) REFERENCES agenteam_tool.spec_revisions(tool_id,spec_revision)
);

-- Agent remains the canonical writer. These rows are written only after its
-- private same-Tx applied witness; even an explicit empty set has an owner row.
CREATE TABLE agenteam_tool.agent_configurations (
 agent_id agenteam_tool.safe_id PRIMARY KEY,
 project_id agenteam_tool.safe_id NOT NULL,
 config_version bigint NOT NULL CHECK(config_version>0),
 UNIQUE(agent_id,project_id)
);
CREATE INDEX tool_configuration_projects ON agenteam_tool.agent_configurations(project_id,agent_id);
CREATE TABLE agenteam_tool.agent_references (
 agent_id agenteam_tool.safe_id NOT NULL REFERENCES agenteam_tool.agent_configurations(agent_id),
 tool_id agenteam_tool.safe_id NOT NULL REFERENCES agenteam_tool.identities(tool_id),
 PRIMARY KEY(agent_id,tool_id)
);
CREATE INDEX tool_references_tool ON agenteam_tool.agent_references(tool_id,agent_id);
