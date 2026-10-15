-- agenteam:transaction tx
-- +goose Up
-- Original connection I/O owns its Secret lease independently of the mutable
-- live connection. Hard Config deletion must not erase release/recovery proof.
CREATE TABLE agenteam_mcp.connection_requests (
 id agenteam_mcp.safe_id PRIMARY KEY,
 command_digest text NOT NULL REFERENCES agenteam_mcp.commands(command_digest),
 connection_id agenteam_mcp.safe_id NOT NULL,
 project_id agenteam_mcp.safe_id NOT NULL,
 config_id agenteam_mcp.safe_id NOT NULL,
 config_revision bigint NOT NULL CHECK(config_revision>0),
 connection_version bigint NOT NULL CHECK(connection_version>0),
 user_id agenteam_mcp.safe_id NOT NULL,
 session_id agenteam_mcp.safe_id NOT NULL,
 credential_id agenteam_mcp.safe_id,
 credential_lease_id agenteam_mcp.safe_id,
 phase text NOT NULL DEFAULT 'pending' CHECK(phase IN ('pending','complete','abandoned')),
 io_retired boolean NOT NULL DEFAULT false,
 lease_released boolean NOT NULL DEFAULT false,
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 retired_at timestamptz(6),
 CHECK((credential_id IS NULL)=(credential_lease_id IS NULL)),
 CHECK(io_retired=(retired_at IS NOT NULL)),
 CHECK(NOT lease_released OR io_retired)
);
CREATE INDEX mcp_connection_request_retirement ON agenteam_mcp.connection_requests(created_at,id) WHERE NOT io_retired OR NOT lease_released;

-- File references identify one canonical content row, never an upload receipt.
-- Before this release all materialized rows, if any, have completed objects.
ALTER TABLE agenteam_mcp.materializations
 ADD COLUMN state text NOT NULL DEFAULT 'ready' CHECK(state IN ('pending','ready','failed')),
 ADD COLUMN version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 ADD COLUMN attempt_id agenteam_mcp.safe_id,
 ADD COLUMN request_id agenteam_mcp.safe_id;
ALTER TABLE agenteam_mcp.materialized_contents ADD COLUMN content_id agenteam_mcp.safe_id;
UPDATE agenteam_mcp.materialized_contents SET content_id=object_id;
ALTER TABLE agenteam_mcp.materialized_contents ALTER COLUMN content_id SET NOT NULL;
ALTER TABLE agenteam_mcp.materialized_contents ADD CONSTRAINT mcp_content_id_unique UNIQUE(content_id);
