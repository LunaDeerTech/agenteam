-- agenteam:transaction tx
-- +goose Up
-- MCP owns connection configuration, capability snapshots and retained runtime
-- material. Shared Tool identities, registration and Operations remain Tool-owned.
CREATE SCHEMA agenteam_mcp;
CREATE DOMAIN agenteam_mcp.safe_id AS text
 CHECK (VALUE ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$');

CREATE TABLE agenteam_mcp.configs (
 id agenteam_mcp.safe_id PRIMARY KEY,
 scope_kind text NOT NULL CHECK(scope_kind IN ('system','project')),
 project_id agenteam_mcp.safe_id,
 name text NOT NULL CHECK(octet_length(name) BETWEEN 1 AND 256),
 description text NOT NULL DEFAULT '' CHECK(octet_length(description)<=8192),
 endpoint text NOT NULL CHECK(octet_length(endpoint) BETWEEN 1 AND 16384),
 auth_scheme text NOT NULL CHECK(auth_scheme IN ('none','bearer','basic','custom_header','oauth')),
 header_name text NOT NULL DEFAULT '' CHECK(octet_length(header_name)<=128),
 version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 runtime_revision bigint NOT NULL DEFAULT 1 CHECK(runtime_revision>0),
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 CHECK((scope_kind='system' AND project_id IS NULL) OR (scope_kind='project' AND project_id IS NOT NULL)),
 CHECK((auth_scheme='custom_header' AND header_name<>'') OR (auth_scheme<>'custom_header' AND header_name=''))
);
CREATE INDEX mcp_project_configs ON agenteam_mcp.configs(project_id,id);

CREATE TABLE agenteam_mcp.connections (
 id agenteam_mcp.safe_id PRIMARY KEY,
 project_id agenteam_mcp.safe_id NOT NULL,
 config_id agenteam_mcp.safe_id NOT NULL REFERENCES agenteam_mcp.configs(id) ON DELETE CASCADE,
 version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 config_revision bigint NOT NULL CHECK(config_revision>0),
 state text NOT NULL CHECK(state IN ('unconnected','connecting','connected','auth_required','error','needs_reconnect')),
 enabled boolean NOT NULL DEFAULT false,
 credential_id agenteam_mcp.safe_id,
 runtime_binding_id agenteam_mcp.safe_id,
 refresh_id agenteam_mcp.safe_id,
 protocol_revision text CHECK(protocol_revision IN ('2026-07-28','2025-11-25')),
 tools_state text NOT NULL DEFAULT 'never' CHECK(tools_state IN ('never','ready','error')),
 resources_state text NOT NULL DEFAULT 'never' CHECK(resources_state IN ('never','ready','error')),
 tools_revision bigint NOT NULL DEFAULT 0 CHECK(tools_revision>=0),
 resources_revision bigint NOT NULL DEFAULT 0 CHECK(resources_revision>=0),
 last_error_code text NOT NULL DEFAULT '' CHECK(last_error_code ~ '^[a-z0-9_]{0,80}$'),
 last_connected_at timestamptz(6),
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(project_id,config_id),
 UNIQUE(id,project_id)
);

-- No FK to live Config/Connection: a hard delete must not break an already
-- captured Execution. Only references are retained here, never plaintext.
CREATE TABLE agenteam_mcp.runtime_bindings (
 id agenteam_mcp.safe_id PRIMARY KEY,
 project_id agenteam_mcp.safe_id NOT NULL,
 config_id agenteam_mcp.safe_id NOT NULL,
 connection_id agenteam_mcp.safe_id NOT NULL,
 config_revision bigint NOT NULL CHECK(config_revision>0),
 endpoint text NOT NULL CHECK(octet_length(endpoint) BETWEEN 1 AND 16384),
 auth_scheme text NOT NULL CHECK(auth_scheme IN ('none','bearer','basic','custom_header','oauth')),
 header_name text NOT NULL DEFAULT '',
 credential_id agenteam_mcp.safe_id,
 protocol_revision text NOT NULL CHECK(protocol_revision IN ('2026-07-28','2025-11-25')),
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(id,project_id,connection_id),
 CHECK((auth_scheme='none' AND credential_id IS NULL) OR (auth_scheme<>'none' AND credential_id IS NOT NULL))
);
ALTER TABLE agenteam_mcp.connections ADD CONSTRAINT mcp_current_runtime_binding
 FOREIGN KEY(runtime_binding_id,project_id,id) REFERENCES agenteam_mcp.runtime_bindings(id,project_id,connection_id)
 DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE agenteam_mcp.runtime_leases (
 execution_id agenteam_mcp.safe_id NOT NULL,
 runtime_binding_id agenteam_mcp.safe_id NOT NULL REFERENCES agenteam_mcp.runtime_bindings(id),
 project_id agenteam_mcp.safe_id NOT NULL,
 connection_id agenteam_mcp.safe_id NOT NULL,
 credential_lease_id agenteam_mcp.safe_id,
 retired boolean NOT NULL DEFAULT false,
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 retired_at timestamptz(6),
 PRIMARY KEY(execution_id,runtime_binding_id),
 FOREIGN KEY(runtime_binding_id,project_id,connection_id) REFERENCES agenteam_mcp.runtime_bindings(id,project_id,connection_id),
 CHECK(retired=(retired_at IS NOT NULL))
);
CREATE INDEX mcp_active_runtime_leases ON agenteam_mcp.runtime_leases(runtime_binding_id) WHERE NOT retired;

CREATE TABLE agenteam_mcp.tool_snapshots (
 connection_id agenteam_mcp.safe_id NOT NULL REFERENCES agenteam_mcp.connections(id) ON DELETE CASCADE,
 revision bigint NOT NULL CHECK(revision>0),
 protocol_revision text NOT NULL CHECK(protocol_revision IN ('2026-07-28','2025-11-25')),
 tool_count integer NOT NULL CHECK(tool_count BETWEEN 0 AND 512),
 invalid_count integer NOT NULL CHECK(invalid_count BETWEEN 0 AND 512),
 content_sha256 text NOT NULL CHECK(content_sha256 ~ '^sha256:[0-9a-f]{64}$'),
 discovered_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(connection_id,revision)
);
CREATE TABLE agenteam_mcp.resource_catalog (
 id agenteam_mcp.safe_id PRIMARY KEY,
 connection_id agenteam_mcp.safe_id NOT NULL REFERENCES agenteam_mcp.connections(id) ON DELETE CASCADE,
 project_id agenteam_mcp.safe_id NOT NULL,
 kind text NOT NULL CHECK(kind IN ('resource','template')),
 uri text NOT NULL CHECK(octet_length(uri) BETWEEN 1 AND 8192),
 name text NOT NULL DEFAULT '' CHECK(octet_length(name)<=256),
 title text NOT NULL DEFAULT '' CHECK(octet_length(title)<=512),
 description text NOT NULL DEFAULT '' CHECK(octet_length(description)<=8192),
 mime_type text NOT NULL DEFAULT '' CHECK(octet_length(mime_type)<=256),
 byte_size bigint CHECK(byte_size>=0),
 catalog_revision bigint NOT NULL CHECK(catalog_revision>0),
 active boolean NOT NULL DEFAULT true,
 UNIQUE(connection_id,kind,uri),
 FOREIGN KEY(connection_id,project_id) REFERENCES agenteam_mcp.connections(id,project_id) ON DELETE CASCADE
);

CREATE TABLE agenteam_mcp.materializations (
 id agenteam_mcp.safe_id PRIMARY KEY,
 project_id agenteam_mcp.safe_id NOT NULL,
 connection_id agenteam_mcp.safe_id NOT NULL,
 runtime_binding_id agenteam_mcp.safe_id NOT NULL REFERENCES agenteam_mcp.runtime_bindings(id),
 execution_id agenteam_mcp.safe_id NOT NULL,
 operation_id agenteam_mcp.safe_id NOT NULL,
 uri text NOT NULL CHECK(octet_length(uri) BETWEEN 1 AND 8192),
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 FOREIGN KEY(runtime_binding_id,project_id,connection_id) REFERENCES agenteam_mcp.runtime_bindings(id,project_id,connection_id)
);
CREATE TABLE agenteam_mcp.materialized_contents (
 materialization_id agenteam_mcp.safe_id NOT NULL REFERENCES agenteam_mcp.materializations(id),
 ordinal integer NOT NULL CHECK(ordinal BETWEEN 0 AND 255),
 object_id agenteam_mcp.safe_id NOT NULL,
 uri text NOT NULL CHECK(octet_length(uri) BETWEEN 1 AND 8192),
 mime_type text NOT NULL CHECK(octet_length(mime_type) BETWEEN 1 AND 256),
 byte_size bigint NOT NULL CHECK(byte_size BETWEEN 0 AND 8388608),
 PRIMARY KEY(materialization_id,ordinal)
);

CREATE TABLE agenteam_mcp.commands (
 command_digest text PRIMARY KEY CHECK(command_digest ~ '^sha256:[0-9a-f]{64}$'),
 semantic_digest text NOT NULL CHECK(semantic_digest ~ '^sha256:[0-9a-f]{64}$'),
 actor_user_id agenteam_mcp.safe_id NOT NULL,
 scope_kind text NOT NULL CHECK(scope_kind IN ('system','project')),
 project_id agenteam_mcp.safe_id,
 command_name text NOT NULL CHECK(command_name IN ('config.create','config.update','config.delete','connection.connect','connection.refresh','connection.enable','connection.disable','connection.disconnect','oauth.begin','oauth.complete')),
 phase text NOT NULL CHECK(phase IN ('pending','complete')),
 response bytea CHECK(octet_length(response) BETWEEN 2 AND 131072),
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 CHECK((phase='pending' AND response IS NULL) OR (phase='complete' AND response IS NOT NULL)),
 CHECK((scope_kind='system' AND project_id IS NULL) OR (scope_kind='project' AND project_id IS NOT NULL))
);

-- OAuth stores state/PKCE/token material exclusively through Secret. Database
-- rows contain opaque references and a one-use, session-bound callback digest.
CREATE TABLE agenteam_mcp.oauth_flows (
 id agenteam_mcp.safe_id PRIMARY KEY,
 connection_id agenteam_mcp.safe_id NOT NULL REFERENCES agenteam_mcp.connections(id) ON DELETE CASCADE,
 project_id agenteam_mcp.safe_id NOT NULL,
 user_id agenteam_mcp.safe_id NOT NULL,
 session_id agenteam_mcp.safe_id NOT NULL,
 state_digest text NOT NULL UNIQUE CHECK(state_digest ~ '^sha256:[0-9a-f]{64}$'),
 credential_id agenteam_mcp.safe_id NOT NULL,
 issuer text NOT NULL CHECK(octet_length(issuer) BETWEEN 1 AND 16384),
 expires_at timestamptz(6) NOT NULL,
 consumed_at timestamptz(6),
 FOREIGN KEY(connection_id,project_id) REFERENCES agenteam_mcp.connections(id,project_id) ON DELETE CASCADE
);
