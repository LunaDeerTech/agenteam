-- agenteam:transaction tx
-- +goose Up
-- The development rule model is intentionally reset. Account, SMTP, proxy
-- configuration/credentials, their receipts and audit history are preserved.
-- Stop the old Central binary before applying this schema revision.
ALTER TABLE agenteam_outbound.outbound_policy
  DROP CONSTRAINT outbound_policy_rules_check,
  ADD COLUMN rule_mode text NOT NULL DEFAULT 'denylist'
    CHECK (rule_mode IN ('allowlist','denylist'));
UPDATE agenteam_outbound.outbound_policy
  SET version=1, rule_mode='denylist', rules='[{"target":"0.0.0.0/8","ports":"all"},{"target":"100.64.0.0/10","ports":"all"},{"target":"127.0.0.0/8","ports":"all"},{"target":"168.63.129.16/32","ports":"all"},{"target":"169.254.0.0/16","ports":"all"},{"target":"192.0.0.0/24","ports":"all"},{"target":"192.0.2.0/24","ports":"all"},{"target":"192.31.196.0/24","ports":"all"},{"target":"192.52.193.0/24","ports":"all"},{"target":"192.88.99.0/24","ports":"all"},{"target":"192.175.48.0/24","ports":"all"},{"target":"198.18.0.0/15","ports":"all"},{"target":"198.51.100.0/24","ports":"all"},{"target":"203.0.113.0/24","ports":"all"},{"target":"224.0.0.0/4","ports":"all"},{"target":"240.0.0.0/4","ports":"all"},{"target":"::/3","ports":"all"},{"target":"2001::/23","ports":"all"},{"target":"2001:db8::/32","ports":"all"},{"target":"2002::/16","ports":"all"},{"target":"2620:4f:8000::/48","ports":"all"},{"target":"3fff::/20","ports":"all"},{"target":"4000::/2","ports":"all"},{"target":"8000::/2","ports":"all"},{"target":"c000::/3","ports":"all"},{"target":"e000::/4","ports":"all"},{"target":"f000::/5","ports":"all"},{"target":"f800::/6","ports":"all"},{"target":"fd00:ec2::254/128","ports":"all"},{"target":"fe00::/7","ports":"all"},{"target":"metadata.google.internal","ports":"all"},{"target":"metadata.goog","ports":"all"}]'::jsonb;
ALTER TABLE agenteam_outbound.outbound_policy
  ADD CONSTRAINT outbound_policy_rules_check CHECK (
    jsonb_typeof(rules)='array' AND jsonb_array_length(rules)<=512
    AND octet_length(rules::text)<=2097152);
DELETE FROM agenteam_outbound.outbound_policy_receipts;
ALTER TABLE agenteam_outbound.outbound_policy_receipts
  DROP CONSTRAINT outbound_policy_receipts_rule_count_check,
  ADD CONSTRAINT outbound_policy_receipts_rule_count_check
    CHECK (rule_count BETWEEN 0 AND 512);

-- Keep the existing closed MCP audit contract, adding only the new safe
-- rule denial reason for both Human and startup discovery records.
-- +goose StatementBegin
DO $$
DECLARE original text;
BEGIN
 SELECT pg_get_expr(conbin,conrelid) INTO STRICT original FROM pg_constraint
 WHERE conrelid='agenteam_audit.audit_records'::regclass
   AND conname='audit_records_mcp_contract' AND contype='c';
 IF position('''private_not_allowed''::text' IN original)=0 THEN
  RAISE EXCEPTION 'MCP audit predecessor reason contract missing';
 END IF;
 original := replace(original,'''private_not_allowed''::text','''rule_not_allowed''::text, ''private_not_allowed''::text');
 ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT audit_records_mcp_contract;
 EXECUTE format('ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_mcp_contract CHECK (%s)',original);
END $$;
-- +goose StatementEnd
