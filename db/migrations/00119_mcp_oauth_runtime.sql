-- agenteam:transaction tx
-- +goose Up
-- OAuth requests survive mutable Config/Connection/flow deletion so the exact
-- original Secret mutation and I/O retirement can still be observed. No code,
-- verifier, client secret, access token or refresh token is stored here.
CREATE TABLE agenteam_mcp.oauth_requests (
 id agenteam_mcp.safe_id PRIMARY KEY,
 operation text NOT NULL CHECK(operation IN ('begin','exchange','refresh','retire')),
 phase text NOT NULL DEFAULT 'pending' CHECK(phase IN ('pending','claimed','complete','failed','unknown')),
 project_id agenteam_mcp.safe_id NOT NULL,
 config_id agenteam_mcp.safe_id NOT NULL,
 config_revision bigint NOT NULL CHECK(config_revision>0),
 connection_id agenteam_mcp.safe_id NOT NULL,
 connection_version bigint NOT NULL CHECK(connection_version>0),
 flow_id agenteam_mcp.safe_id,
 command_digest text REFERENCES agenteam_mcp.commands(command_digest),
 user_id agenteam_mcp.safe_id,
 session_id agenteam_mcp.safe_id,
 execution_id agenteam_mcp.safe_id,
 operation_id agenteam_mcp.safe_id,
 attempt_id agenteam_mcp.safe_id,
 backend_request_id agenteam_mcp.safe_id,
 runtime_binding_id agenteam_mcp.safe_id,
 credential_id agenteam_mcp.safe_id,
 credential_version bigint CHECK(credential_version>0),
 credential_lease_id agenteam_mcp.safe_id,
 result_credential_id agenteam_mcp.safe_id,
 result_credential_version bigint CHECK(result_credential_version>0),
 secret_write_binding text CHECK(secret_write_binding ~ '^sha256:[0-9a-f]{64}$'),
 error_code text NOT NULL DEFAULT '' CHECK(error_code ~ '^[a-z0-9_]{0,80}$'),
 io_retired boolean NOT NULL DEFAULT false,
 lease_released boolean NOT NULL DEFAULT false,
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 retired_at timestamptz(6),
 CHECK((credential_id IS NULL)=(credential_version IS NULL)),
 CHECK((result_credential_id IS NULL)=(result_credential_version IS NULL)),
 CHECK(credential_lease_id IS NULL OR credential_id IS NOT NULL),
 CHECK(io_retired=(retired_at IS NOT NULL)),
 CHECK(NOT lease_released OR io_retired),
 CHECK((user_id IS NOT NULL AND session_id IS NOT NULL AND command_digest IS NOT NULL
        AND execution_id IS NULL AND operation_id IS NULL AND attempt_id IS NULL
        AND backend_request_id IS NULL AND runtime_binding_id IS NULL)
    OR (user_id IS NULL AND session_id IS NULL AND command_digest IS NULL
        AND execution_id IS NOT NULL AND operation_id IS NOT NULL AND attempt_id IS NOT NULL
        AND backend_request_id IS NOT NULL AND runtime_binding_id IS NOT NULL)),
 CHECK(operation NOT IN ('begin','exchange') OR flow_id IS NOT NULL),
 CHECK(operation<>'begin' OR credential_id IS NULL),
 CHECK(operation<>'refresh' OR credential_id IS NOT NULL)
);
CREATE UNIQUE INDEX mcp_oauth_single_refresh ON agenteam_mcp.oauth_requests(credential_id)
 WHERE operation='refresh' AND phase IN ('pending','claimed','unknown');
CREATE INDEX mcp_oauth_request_retirement ON agenteam_mcp.oauth_requests(created_at,id)
 WHERE NOT io_retired OR NOT lease_released;

-- Legacy rows cannot acquire new callback authority. Newly issued flows bind
-- the exact configuration revision, connection version and original begin.
ALTER TABLE agenteam_mcp.oauth_flows
 ADD COLUMN config_id agenteam_mcp.safe_id,
 ADD COLUMN config_revision bigint CHECK(config_revision>0),
 ADD COLUMN connection_version bigint CHECK(connection_version>0),
 ADD COLUMN begin_request_id agenteam_mcp.safe_id REFERENCES agenteam_mcp.oauth_requests(id),
 ADD COLUMN exchange_request_id agenteam_mcp.safe_id REFERENCES agenteam_mcp.oauth_requests(id),
 ADD COLUMN credential_version bigint CHECK(credential_version>0),
 ADD COLUMN phase text NOT NULL DEFAULT 'legacy' CHECK(phase IN ('legacy','ready','exchanging','complete','failed','expired')),
 ADD CONSTRAINT mcp_oauth_current_flow CHECK(phase='legacy' OR
  (config_id IS NOT NULL AND config_revision IS NOT NULL AND connection_version IS NOT NULL
   AND begin_request_id IS NOT NULL AND credential_version IS NOT NULL)),
 ADD CONSTRAINT mcp_oauth_claimed_flow CHECK((phase NOT IN ('exchanging','complete')) OR
  (exchange_request_id IS NOT NULL AND consumed_at IS NOT NULL));

-- These references describe Secret's current token version. Historical runtime
-- shadows retain their credential ID independently of live connection changes.
CREATE TABLE agenteam_mcp.oauth_tokens (
 credential_id agenteam_mcp.safe_id PRIMARY KEY,
 project_id agenteam_mcp.safe_id NOT NULL,
 connection_id agenteam_mcp.safe_id NOT NULL,
 config_id agenteam_mcp.safe_id NOT NULL,
 config_revision bigint NOT NULL CHECK(config_revision>0),
 secret_version bigint NOT NULL CHECK(secret_version>0),
 issuer text NOT NULL CHECK(octet_length(issuer) BETWEEN 1 AND 16384),
 resource text NOT NULL CHECK(octet_length(resource) BETWEEN 1 AND 16384),
 expires_at timestamptz(6),
 refreshable boolean NOT NULL,
 last_request_id agenteam_mcp.safe_id NOT NULL REFERENCES agenteam_mcp.oauth_requests(id),
 updated_at timestamptz(6) NOT NULL DEFAULT clock_timestamp()
);
