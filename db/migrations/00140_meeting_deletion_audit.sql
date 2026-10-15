-- agenteam:transaction tx
-- +goose Up
-- Add one Project-only producer while retaining every predecessor predicate.
-- The closed contract below prevents the added words from relabelling other
-- producers, resources, actors or outcomes.
-- +goose StatementBegin
DO $$
DECLARE item record; expression text;
BEGIN
 FOR item IN SELECT * FROM (VALUES
  ('audit_records_action_check','action IN (''meeting.delete.accepted'',''meeting.delete.completed'')'),
  ('audit_records_resource_kind_check','resource_kind=''meeting'''),
  ('audit_records_producer_check','producer=''meeting'''),
  ('audit_records_check2','resource_kind=''meeting'' AND resource_id IS NOT NULL'),
  ('audit_records_check1','actor_kind=''service'' AND scope=''project'' AND actor_project_id=project_id AND user_id IS NULL AND session_id IS NULL AND agent_id IS NULL AND actor_execution_id IS NULL AND service_name=''meeting-runtime'' AND service_cause=cause_ref AND producer=''meeting'' AND action=''meeting.delete.completed''')
 ) AS additions(name,extra) LOOP
  SELECT pg_get_expr(conbin,conrelid) INTO STRICT expression FROM pg_constraint
   WHERE conrelid='agenteam_audit.audit_records'::regclass AND conname=item.name AND contype='c';
  EXECUTE format('ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT %I',item.name);
  EXECUTE format('ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT %I CHECK ((%s) OR (%s))',item.name,expression,item.extra);
 END LOOP;
END $$;
-- +goose StatementEnd
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_meeting_deletion_contract CHECK ((
 (producer<>'meeting' AND action NOT IN ('meeting.delete.accepted','meeting.delete.completed') AND resource_kind<>'meeting' AND service_name IS DISTINCT FROM 'meeting-runtime')
 OR (producer='meeting' AND scope='project' AND project_id IS NOT NULL AND outcome='success'
  AND resource_kind='meeting' AND resource_id IS NOT NULL
  AND operation_id IS NOT NULL AND cause_ref=operation_id::text
  AND tool_id IS NULL AND execution_id IS NULL AND tool_call_id IS NULL AND request_id IS NULL
  AND approval_id IS NULL AND runner_id IS NULL AND correlation_id IS NULL AND http_trace_id IS NULL
  AND metadata ?& ARRAY['meeting_id','deletion_id','initiator_id','version','deleted_rows']
  AND metadata-ARRAY['meeting_id','deletion_id','initiator_id','version','deleted_rows']='{}'::jsonb
  AND jsonb_typeof(metadata->'meeting_id')='string' AND metadata->>'meeting_id'=resource_id::text
  AND metadata->>'meeting_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
  AND jsonb_typeof(metadata->'deletion_id')='string' AND metadata->>'deletion_id'=operation_id::text
  AND metadata->>'deletion_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
  AND jsonb_typeof(metadata->'initiator_id')='string'
  AND metadata->>'initiator_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
  AND agenteam_project.audit_version(metadata->'version')
  AND CASE WHEN jsonb_typeof(metadata->'deleted_rows')='string' AND metadata->>'deleted_rows' ~ '^(0|[1-9][0-9]{0,18})$'
      THEN (metadata->>'deleted_rows')::numeric<=9223372036854775807 ELSE false END
  AND ((action='meeting.delete.accepted' AND ordinal=0 AND actor_kind='human'
        AND metadata->>'initiator_id'=user_id::text AND metadata->>'deleted_rows'='0')
    OR (action='meeting.delete.completed' AND ordinal=1 AND actor_kind='service'
        AND service_name='meeting-runtime' AND service_cause=operation_id::text
        AND actor_project_id=project_id AND metadata->>'deleted_rows'<>'0')))
) IS TRUE);
