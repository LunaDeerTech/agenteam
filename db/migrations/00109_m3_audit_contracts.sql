-- agenteam:transaction tx
-- +goose Up
-- Preserve every predecessor predicate and add the two closed M3 producers.
-- The independent contracts below bind the expanded vocabulary to exact facts.
-- +goose StatementBegin
DO $$
DECLARE item record; expression text;
BEGIN
 FOR item IN SELECT * FROM (VALUES
  ('audit_records_action_check','action IN (''memory.retain'',''memory.delete'',''mcp.config.create'',''mcp.config.update'',''mcp.config.delete'',''mcp.connection.connect'',''mcp.connection.enable'',''mcp.connection.disable'',''mcp.connection.disconnect'',''mcp.discovery.refresh'')'),
  ('audit_records_resource_kind_check','resource_kind IN (''memory'',''memory_namespace'',''mcp_config'',''mcp_connection'')'),
  ('audit_records_producer_check','producer IN (''memory'',''mcp'')'),
  ('audit_records_check2','resource_kind IN (''memory'',''memory_namespace'',''mcp_config'',''mcp_connection'') AND resource_id IS NOT NULL')
 ) AS additions(name,extra) LOOP
  SELECT pg_get_expr(conbin,conrelid) INTO STRICT expression FROM pg_constraint
   WHERE conrelid='agenteam_audit.audit_records'::regclass AND conname=item.name AND contype='c';
  EXECUTE format('ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT %I',item.name);
  EXECUTE format('ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT %I CHECK ((%s) OR (%s))',item.name,expression,item.extra);
 END LOOP;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION agenteam_audit.valid_m3_counts(value jsonb,names text[]) RETURNS boolean
LANGUAGE sql IMMUTABLE AS $$
 SELECT bool_and(jsonb_typeof(value->name)='number' AND value->>name ~ '^(0|[1-9][0-9]{0,3}|10000)$')
 FROM unnest(names) AS name;
$$;
-- +goose StatementEnd
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_memory_contract CHECK ((
 (producer<>'memory' AND action NOT IN ('memory.retain','memory.delete') AND resource_kind NOT IN ('memory','memory_namespace'))
 OR (producer='memory' AND scope='project' AND outcome='success' AND ordinal=0
  AND tool_id IS NULL AND tool_call_id IS NULL AND request_id IS NULL AND approval_id IS NULL
  AND runner_id IS NULL AND correlation_id IS NULL AND http_trace_id IS NULL
  AND metadata ?& ARRAY['agent_id','added','updated','deleted','noop']
  AND jsonb_typeof(metadata->'agent_id')='string'
  AND metadata->>'agent_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
  AND agenteam_audit.valid_m3_counts(metadata,ARRAY['added','updated','deleted','noop'])
  AND ((action='memory.retain' AND actor_kind='agent_run' AND resource_kind='memory_namespace'
   AND resource_id=agent_id AND metadata->>'agent_id'=agent_id::text
   AND metadata ?& ARRAY['operation_id','gate'] AND metadata-ARRAY['agent_id','operation_id','gate','added','updated','deleted','noop']='{}'::jsonb
   AND jsonb_typeof(metadata->'operation_id')='string' AND metadata->>'operation_id'=operation_id::text
   AND operation_id::text=cause_ref AND jsonb_typeof(metadata->'gate')='string' AND metadata->>'gate' IN ('allow','mask'))
   OR (action='memory.delete' AND actor_kind='human' AND resource_kind='memory'
    AND execution_id IS NULL AND operation_id IS NULL
    AND metadata ? 'memory_id' AND metadata-ARRAY['agent_id','memory_id','added','updated','deleted','noop']='{}'::jsonb
    AND jsonb_typeof(metadata->'memory_id')='string' AND metadata->>'memory_id'=resource_id::text
    AND metadata->>'added'='0' AND metadata->>'updated'='0' AND metadata->>'deleted'='1' AND metadata->>'noop'='0')))
) IS TRUE);
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_mcp_contract CHECK ((
 (producer<>'mcp' AND action NOT IN ('mcp.config.create','mcp.config.update','mcp.config.delete','mcp.connection.connect','mcp.connection.enable','mcp.connection.disable','mcp.connection.disconnect','mcp.discovery.refresh') AND resource_kind NOT IN ('mcp_config','mcp_connection'))
 OR (producer='mcp' AND actor_kind='human' AND ordinal=0
  AND tool_id IS NULL AND execution_id IS NULL AND tool_call_id IS NULL AND operation_id IS NULL
  AND request_id IS NULL AND approval_id IS NULL AND runner_id IS NULL AND correlation_id IS NULL AND http_trace_id IS NULL
  AND metadata ?& ARRAY['config_id','version','tool_count','resource_count','invalid_count']
  AND jsonb_typeof(metadata->'config_id')='string'
  AND metadata->>'config_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
  AND jsonb_typeof(metadata->'version')='string' AND metadata->>'version' ~ '^[1-9][0-9]{0,18}$'
  AND agenteam_audit.valid_m3_counts(metadata,ARRAY['tool_count','resource_count','invalid_count'])
  AND ((outcome='success' AND NOT metadata ? 'error_code')
   OR (outcome='failed' AND action='mcp.discovery.refresh' AND jsonb_typeof(metadata->'error_code')='string'
    AND metadata->>'error_code' IN ('invalid_target','address_forbidden','private_not_allowed','port_denied','http_denied','tls_failed','dns_failed','redirect_denied','policy_unavailable','binding_invalid','consumer_denied','credential_denied','origin_denied','response_limit','timeout','cancelled','internal_error','storage_unavailable','integrity_mismatch')))
  AND ((action IN ('mcp.config.create','mcp.config.update','mcp.config.delete') AND resource_kind='mcp_config'
   AND metadata->>'config_id'=resource_id::text AND metadata-ARRAY['config_id','version','tool_count','resource_count','invalid_count']='{}'::jsonb)
   OR (action IN ('mcp.connection.connect','mcp.connection.enable','mcp.connection.disable','mcp.connection.disconnect','mcp.discovery.refresh')
    AND scope='project' AND resource_kind='mcp_connection' AND metadata ? 'connection_id'
    AND jsonb_typeof(metadata->'connection_id')='string' AND metadata->>'connection_id'=resource_id::text
    AND metadata-ARRAY['config_id','connection_id','version','tool_count','resource_count','invalid_count','error_code']='{}'::jsonb)))
) IS TRUE);
