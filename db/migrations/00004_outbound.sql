-- agenteam:transaction tx
-- +goose Up
CREATE SCHEMA agenteam_outbound;
CREATE TABLE agenteam_outbound.outbound_policy (
  singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
  version bigint NOT NULL CHECK (version > 0),
  rules jsonb NOT NULL CHECK (jsonb_typeof(rules) = 'array' AND jsonb_array_length(rules) <= 256 AND octet_length(rules::text) <= 524288)
);
INSERT INTO agenteam_outbound.outbound_policy(singleton,version,rules) VALUES(true,1,'[]');
CREATE TABLE agenteam_outbound.outbound_policy_receipts (
  command_digest text PRIMARY KEY CHECK (command_digest ~ '^sha256:[0-9a-f]{64}$'),
  semantic_digest text NOT NULL CHECK (semantic_digest ~ '^sha256:[0-9a-f]{64}$'),
  user_id uuid NOT NULL,
  version bigint NOT NULL UNIQUE CHECK (version > 1),
  rule_count bigint NOT NULL CHECK (rule_count BETWEEN 0 AND 256),
  audit_id uuid NOT NULL REFERENCES agenteam_audit.audit_records(id),
  created_at timestamptz(6) NOT NULL
);
