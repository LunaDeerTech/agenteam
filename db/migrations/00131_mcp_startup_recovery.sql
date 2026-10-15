-- agenteam:transaction tx
-- +goose Up
-- A startup refresh is a technical request of the original Central process.
-- It must not reuse a Human command/session or infer a stopped process from age.
CREATE TABLE agenteam_mcp.startup_requests (
 id agenteam_mcp.safe_id PRIMARY KEY,
 process_id agenteam_object.safe_id NOT NULL REFERENCES agenteam_object.process_claims(process_id),
 retirement_process_id agenteam_object.safe_id REFERENCES agenteam_object.process_claims(process_id),
 project_id agenteam_mcp.safe_id NOT NULL,
 connection_id agenteam_mcp.safe_id NOT NULL,
 config_id agenteam_mcp.safe_id NOT NULL,
 config_revision bigint NOT NULL CHECK(config_revision>0),
 connection_version bigint NOT NULL CHECK(connection_version>0),
 runtime_binding_id agenteam_mcp.safe_id,
 credential_id agenteam_mcp.safe_id,
 credential_lease_id agenteam_mcp.safe_id,
 phase text NOT NULL DEFAULT 'pending' CHECK(phase IN ('pending','complete','abandoned')),
 io_started boolean NOT NULL DEFAULT false,
 io_retired boolean NOT NULL DEFAULT false,
 lease_released boolean NOT NULL DEFAULT false,
 result_version bigint CHECK(result_version>0),
 error_code text NOT NULL DEFAULT '' CHECK(error_code ~ '^[a-z0-9_]{0,80}$'),
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 retired_at timestamptz(6),
 completed_at timestamptz(6),
 UNIQUE(process_id,connection_id),
 CHECK((credential_id IS NULL)=(credential_lease_id IS NULL)),
 CHECK(io_retired=(retired_at IS NOT NULL)),
 CHECK(io_retired=(retirement_process_id IS NOT NULL)),
 CHECK(NOT lease_released OR io_retired),
 CHECK((phase='pending')=(completed_at IS NULL)),
 CHECK(phase<>'complete' OR (io_retired AND result_version IS NOT NULL)),
 CHECK(phase<>'abandoned' OR io_retired)
);
CREATE INDEX mcp_startup_retirement ON agenteam_mcp.startup_requests(created_at,id)
 WHERE phase='pending' OR NOT lease_released;

-- Historical rows keep an unknown original process; no process is invented
-- for them. New materializations persist the actual guarded Process before Put.
ALTER TABLE agenteam_mcp.materializations
 ADD COLUMN process_id agenteam_object.safe_id REFERENCES agenteam_object.process_claims(process_id);

-- Exact recovery ownership survives a live Config/Connection hard deletion.
-- Object's original upload ledger remains the authority for upload/object IDs.
-- No stored body, credential material, HTTP token or upload receipt is copied.
CREATE TABLE agenteam_mcp.content_recoveries (
 id agenteam_mcp.safe_id PRIMARY KEY,
 materialization_id agenteam_mcp.safe_id NOT NULL UNIQUE REFERENCES agenteam_mcp.materializations(id),
 project_id agenteam_mcp.safe_id NOT NULL,
 original_process_id agenteam_object.safe_id NOT NULL REFERENCES agenteam_object.process_claims(process_id),
 recovery_process_id agenteam_object.safe_id NOT NULL REFERENCES agenteam_object.process_claims(process_id),
 upload_id agenteam_object.safe_id,
 object_id agenteam_object.safe_id,
 phase text NOT NULL DEFAULT 'observing' CHECK(phase IN ('observing','reference','object','complete')),
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 completed_at timestamptz(6),
 CHECK(original_process_id<>recovery_process_id),
 CHECK((upload_id IS NULL)=(object_id IS NULL)),
 CHECK(phase NOT IN ('reference','object') OR object_id IS NOT NULL),
 CHECK((phase='complete')=(completed_at IS NOT NULL))
);
CREATE INDEX mcp_content_recovery_pending ON agenteam_mcp.content_recoveries(created_at,id)
 WHERE phase<>'complete';
