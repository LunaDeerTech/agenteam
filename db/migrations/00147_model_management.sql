-- agenteam:transaction tx
-- +goose Up
-- Management probes have durable receipts, independent of configuration CAS.
-- A pending operation is never sent again after restart or response loss.
CREATE TABLE agenteam_model.management_operations (
 id uuid PRIMARY KEY,
 command_digest text NOT NULL UNIQUE CHECK(command_digest ~ '^sha256:[0-9a-f]{64}$'),
 semantic_digest text NOT NULL CHECK(semantic_digest ~ '^sha256:[0-9a-f]{64}$'),
 user_id uuid NOT NULL,
 session_id uuid NOT NULL,
 process_id uuid NOT NULL,
 action text NOT NULL CHECK(action IN ('discovery','test')),
 provider_id uuid NOT NULL,
 provider_version bigint NOT NULL CHECK(provider_version>0),
 model_id uuid,
 model_version bigint,
 -- Historical identity only: live Secret protection belongs to its lease.
 credential_id uuid,
 credential_version bigint,
 lease_id uuid UNIQUE,
 result jsonb,
 created_at timestamptz(6) NOT NULL,
 CHECK(((action='discovery' AND model_id IS NULL AND model_version IS NULL)
   OR (action='test' AND model_id IS NOT NULL AND model_version>0)) IS TRUE),
 CHECK(((credential_id IS NULL AND credential_version IS NULL AND lease_id IS NULL)
   OR (credential_id IS NOT NULL AND credential_version>0 AND lease_id IS NOT NULL)) IS TRUE),
 CHECK(result IS NULL OR jsonb_typeof(result)='object')
);
CREATE INDEX model_management_pending ON agenteam_model.management_operations(process_id) WHERE result IS NULL;
ALTER TABLE agenteam_secret.secret_leases DROP CONSTRAINT secret_leases_owner_kind_check;
ALTER TABLE agenteam_secret.secret_leases ADD CONSTRAINT secret_leases_owner_kind_check
 CHECK(owner_kind IN ('execution','model_call','account_delivery_attempt','account_response','mcp_connection_request','outbound_proxy','model_management_request'));
ALTER TABLE agenteam_secret.secret_leases ADD CONSTRAINT secret_leases_model_management_consumer
 CHECK(owner_kind<>'model_management_request' OR (consumer='model' AND scope='system' AND project_id IS NULL));
