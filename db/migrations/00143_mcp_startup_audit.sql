-- agenteam:transaction tx
-- +goose Up
-- Preserve both predecessor predicates, including every Human MCP record.
-- The only added actor is the Project-bound MCP startup discovery producer.
-- Registration alone grants no access; its domain authority still validates
-- the original startup request and private process witness in the same Tx.
-- +goose StatementBegin
DO $$
DECLARE
 item record;
 expression text;
 runtime_actor text := $actor$
  actor_kind='service' AND scope='project' AND project_id IS NOT NULL
  AND actor_project_id=project_id
  AND user_id IS NULL AND session_id IS NULL AND agent_id IS NULL AND actor_execution_id IS NULL
  AND service_name='mcp-runtime' AND service_cause=cause_ref
  AND cause_ref ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
  AND producer='mcp' AND action='mcp.discovery.refresh'
  AND resource_kind='mcp_connection' AND resource_id IS NOT NULL
 $actor$;
 runtime_fields text := $fields$
  AND ordinal=0
  AND tool_id IS NULL AND execution_id IS NULL AND tool_call_id IS NULL AND operation_id IS NULL
  AND request_id IS NULL AND approval_id IS NULL AND runner_id IS NULL AND correlation_id IS NULL AND http_trace_id IS NULL
  AND metadata ?& ARRAY['config_id','connection_id','version','tool_count','resource_count','invalid_count']
  AND metadata-ARRAY['config_id','connection_id','version','tool_count','resource_count','invalid_count','error_code']='{}'::jsonb
  AND jsonb_typeof(metadata->'config_id')='string'
  AND metadata->>'config_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
  AND jsonb_typeof(metadata->'connection_id')='string'
  AND metadata->>'connection_id'=resource_id::text
  AND metadata->>'connection_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
  AND CASE WHEN jsonb_typeof(metadata->'version')='string' AND metadata->>'version' ~ '^[1-9][0-9]{0,18}$'
      THEN (metadata->>'version')::numeric<=9223372036854775807 ELSE false END
  AND agenteam_audit.valid_m3_counts(metadata,ARRAY['tool_count','resource_count','invalid_count'])
  AND ((outcome='success' AND NOT metadata ? 'error_code')
   OR (outcome='failed' AND jsonb_typeof(metadata->'error_code')='string'
    AND metadata->>'error_code' IN ('invalid_target','address_forbidden','private_not_allowed','port_denied','http_denied','tls_failed','dns_failed','redirect_denied','policy_unavailable','binding_invalid','consumer_denied','credential_denied','origin_denied','response_limit','timeout','cancelled','internal_error','storage_unavailable','integrity_mismatch')))
 $fields$;
BEGIN
 FOR item IN SELECT * FROM (VALUES
  ('audit_records_check1',runtime_actor),
  ('audit_records_mcp_contract',runtime_actor || runtime_fields)
 ) AS additions(name,extra) LOOP
  SELECT pg_get_expr(conbin,conrelid) INTO STRICT expression FROM pg_constraint
   WHERE conrelid='agenteam_audit.audit_records'::regclass AND conname=item.name AND contype='c';
  EXECUTE format('ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT %I',item.name);
  EXECUTE format('ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT %I CHECK ((%s) OR ((%s) IS TRUE))',item.name,expression,item.extra);
 END LOOP;
END $$;
-- +goose StatementEnd
