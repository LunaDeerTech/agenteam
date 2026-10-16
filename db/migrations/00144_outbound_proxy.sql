-- agenteam:transaction tx
-- +goose Up
-- Configuration is additive and defaults to direct. Deployment environment
-- proxy grants are deliberately not migrated into credentials or policy.
CREATE TABLE agenteam_outbound.proxy_settings (
 singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
 version bigint NOT NULL CHECK(version>0),
 mode text NOT NULL CHECK(mode IN ('direct','proxy')),
 gateway jsonb NOT NULL DEFAULT 'null',
 credential_id uuid REFERENCES agenteam_secret.secrets(id),
 CHECK(gateway='null'::jsonb OR (jsonb_typeof(gateway)='object'
  AND gateway ?& ARRAY['protocol','host','port','username','credential_present']
  AND gateway-ARRAY['protocol','host','port','username','credential_present']='{}'::jsonb
  AND gateway->>'protocol' IN ('http','https','socks5')
  AND jsonb_typeof(gateway->'host')='string' AND octet_length(gateway->>'host') BETWEEN 1 AND 253
  AND jsonb_typeof(gateway->'port')='number' AND (gateway->>'port')::numeric BETWEEN 1 AND 65535
  AND (gateway->>'port')::numeric=trunc((gateway->>'port')::numeric)
  AND jsonb_typeof(gateway->'username')='string' AND octet_length(gateway->>'username')<=255
  AND gateway->'credential_present'=to_jsonb(credential_id IS NOT NULL))),
 CHECK(mode='direct' OR gateway<>'null'::jsonb),
 CHECK((gateway='null'::jsonb AND credential_id IS NULL)
  OR (gateway<>'null'::jsonb AND ((gateway->>'username'<>'')=(credential_id IS NOT NULL))))
);
INSERT INTO agenteam_outbound.proxy_settings(singleton,version,mode) VALUES(true,1,'direct');
CREATE TABLE agenteam_outbound.proxy_receipts (
 command_digest text PRIMARY KEY CHECK(command_digest ~ '^sha256:[0-9a-f]{64}$'),
 semantic_kid text NOT NULL CHECK(semantic_kid ~ '^[A-Za-z0-9_-]{1,64}$'),
 semantic_mac bytea NOT NULL CHECK(octet_length(semantic_mac)=32),
 user_id uuid NOT NULL,
 version bigint NOT NULL UNIQUE CHECK(version>1),
 audit_id uuid NOT NULL REFERENCES agenteam_audit.audit_records(id),
 created_at timestamptz(6) NOT NULL
);
-- Every short-lived Secret lease has a durable original-process witness.
-- Removed only in the same transaction as its idempotent Secret release.
CREATE TABLE agenteam_outbound.proxy_credential_operations (
 operation_id uuid PRIMARY KEY,
 process_id uuid NOT NULL,
 lease_id uuid NOT NULL UNIQUE,
 credential_id uuid NOT NULL REFERENCES agenteam_secret.secrets(id),
 proxy_version bigint NOT NULL CHECK(proxy_version > 0)
);
CREATE INDEX proxy_credential_operations_process ON agenteam_outbound.proxy_credential_operations(process_id);
ALTER TABLE agenteam_secret.secret_leases DROP CONSTRAINT secret_leases_owner_kind_check;
ALTER TABLE agenteam_secret.secret_leases ADD CONSTRAINT secret_leases_owner_kind_check
 CHECK(owner_kind IN ('execution','model_call','account_delivery_attempt','account_response','mcp_connection_request','outbound_proxy'));
ALTER TABLE agenteam_secret.secret_leases ADD CONSTRAINT secret_leases_outbound_proxy_consumer
 CHECK(owner_kind<>'outbound_proxy' OR (consumer='system' AND scope='system' AND project_id IS NULL));
-- +goose StatementBegin
DO $$
DECLARE original text;
BEGIN
 SELECT pg_get_expr(conbin,conrelid) INTO STRICT original FROM pg_constraint
 WHERE conrelid='agenteam_audit.audit_records'::regclass AND conname='audit_records_action_check' AND contype='c';
 EXECUTE 'ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT audit_records_action_check';
 EXECUTE format('ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_action_check CHECK ((%s) OR action=''outbound.proxy.update'')',original);
END $$;
-- +goose StatementEnd
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_outbound_proxy_contract CHECK (
 action<>'outbound.proxy.update' OR (
  producer='outbound.policy' AND scope='system' AND actor_kind='human' AND outcome='success'
  AND resource_kind='outbound_policy' AND resource_id IS NULL AND ordinal=0
  AND metadata ? 'version' AND metadata-ARRAY['version']='{}'::jsonb
  AND jsonb_typeof(metadata->'version')='string'
  AND CASE WHEN metadata->>'version' ~ '^[1-9][0-9]{0,18}$'
      THEN (metadata->>'version')::numeric<=9223372036854775807 ELSE false END
  AND tool_id IS NULL AND execution_id IS NULL AND tool_call_id IS NULL AND operation_id IS NULL
  AND request_id IS NULL AND approval_id IS NULL AND runner_id IS NULL AND correlation_id IS NULL
 )
);
