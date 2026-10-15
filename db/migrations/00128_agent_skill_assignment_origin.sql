-- agenteam:transaction tx
-- +goose Up
-- Agent assignment reuses the original command and ingress ledgers. Historical
-- Human input/candidate bytes and idempotency keys remain unchanged.
ALTER TABLE agenteam_skill.assignment_commands
 ALTER COLUMN actor_user_id DROP NOT NULL,
 ADD COLUMN actor_kind text NOT NULL DEFAULT 'human',
 ADD COLUMN run_origin jsonb;
ALTER TABLE agenteam_skill.assignment_commands ADD CONSTRAINT assignment_command_actor_origin CHECK ((
 (actor_kind='human' AND actor_user_id IS NOT NULL AND run_origin IS NULL
  AND NOT (convert_from(input,'UTF8')::jsonb ? 'run')
  AND convert_from(input,'UTF8')::jsonb->>'actor_user_id'=actor_user_id::text
  AND convert_from(candidate,'UTF8')::jsonb->'origin'->>'Kind'='human'
  AND convert_from(candidate,'UTF8')::jsonb->'origin'->>'UserID'=actor_user_id::text)
 OR (actor_kind='agent_run' AND actor_user_id IS NULL
  AND jsonb_typeof(run_origin)='object' AND octet_length(run_origin::text)<=4096
  AND run_origin ?& ARRAY['caller_agent_id','execution_id','operation_id','attempt_id','request_id','tool_call_id','tool_id','spec_revision','input_digest']
  AND run_origin-ARRAY['caller_agent_id','execution_id','operation_id','attempt_id','request_id','tool_call_id','tool_id','spec_revision','input_digest']='{}'::jsonb
  AND jsonb_typeof(run_origin->'caller_agent_id')='string' AND run_origin->>'caller_agent_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
  AND jsonb_typeof(run_origin->'execution_id')='string' AND run_origin->>'execution_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
  AND jsonb_typeof(run_origin->'operation_id')='string' AND run_origin->>'operation_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
  AND jsonb_typeof(run_origin->'attempt_id')='string' AND run_origin->>'attempt_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
  AND jsonb_typeof(run_origin->'request_id')='string' AND run_origin->>'request_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
  AND jsonb_typeof(run_origin->'tool_call_id')='string' AND run_origin->>'tool_call_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
  AND jsonb_typeof(run_origin->'tool_id')='string' AND run_origin->>'tool_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
  AND jsonb_typeof(run_origin->'spec_revision')='string'
  AND CASE WHEN run_origin->>'spec_revision' ~ '^[1-9][0-9]{0,18}$' THEN (run_origin->>'spec_revision')::numeric BETWEEN 1 AND 9223372036854775807 ELSE false END
  AND jsonb_typeof(run_origin->'input_digest')='string' AND run_origin->>'input_digest' ~ '^sha256:[0-9a-f]{64}$'
  AND run_origin->>'operation_id'=idempotency_key
  AND convert_from(input,'UTF8')::jsonb->>'actor_user_id'=''
  AND convert_from(input,'UTF8')::jsonb->'run'=run_origin
  AND convert_from(input,'UTF8')::jsonb->'request'->>'project_id'=project_id::text
  AND convert_from(input,'UTF8')::jsonb->'request'->>'agent_id'=agent_id::text
  AND convert_from(candidate,'UTF8')::jsonb->'origin'->>'Kind'='agent_run'
  AND convert_from(candidate,'UTF8')::jsonb->'origin'->>'UserID'=''
  AND convert_from(candidate,'UTF8')::jsonb->'origin'->>'SessionID'=''
  AND convert_from(candidate,'UTF8')::jsonb->'origin'->>'ProjectID'=project_id::text
  AND convert_from(candidate,'UTF8')::jsonb->'origin'->>'AgentID'=run_origin->>'caller_agent_id'
  AND convert_from(candidate,'UTF8')::jsonb->'origin'->>'ExecutionID'=run_origin->>'execution_id')
) IS TRUE);

-- Source is immutable alongside the original full input and postimage.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_skill.guard_assignment_command() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' OR OLD.state='completed' OR NEW.state<>'completed'
 OR ROW(NEW.id,NEW.project_id,NEW.agent_id,NEW.actor_user_id,NEW.actor_kind,NEW.run_origin,NEW.command_name,NEW.idempotency_key,NEW.semantic_digest,NEW.input,NEW.candidate,NEW.created_at)
 IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.agent_id,OLD.actor_user_id,OLD.actor_kind,OLD.run_origin,OLD.command_name,OLD.idempotency_key,OLD.semantic_digest,OLD.input,OLD.candidate,OLD.created_at)
 THEN RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='immutable Skill assignment command'; END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- The target resource is independent of the calling Agent. These associations
-- name the original caller's Execution and protected Tool payload, not a body
-- read permission. The Skill writer separately proves the exact live attempt.
ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT audit_records_skill_assignment_contract;
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_skill_assignment_contract CHECK ((
 (producer<>'skill' AND action NOT IN ('skill.assign','skill.remove'))
 OR (producer='skill' AND action IN ('skill.assign','skill.remove')
  AND scope='project' AND project_id IS NOT NULL AND outcome='success' AND ordinal=0
  AND ((actor_kind='human' AND user_id IS NOT NULL AND session_id IS NOT NULL
        AND tool_id IS NULL AND execution_id IS NULL AND tool_call_id IS NULL AND operation_id IS NULL)
    OR (actor_kind='agent_run' AND user_id IS NULL AND session_id IS NULL
        AND actor_project_id=project_id AND agent_id IS NOT NULL AND actor_execution_id=execution_id
        AND tool_id IS NOT NULL AND execution_id IS NOT NULL AND tool_call_id IS NOT NULL AND operation_id IS NOT NULL))
  AND resource_kind='agent' AND resource_id IS NOT NULL AND cause_ref ~ '^sha256:[0-9a-f]{64}$'
  AND request_id IS NULL AND approval_id IS NULL AND runner_id IS NULL AND correlation_id IS NULL AND http_trace_id IS NULL
  AND jsonb_typeof(metadata)='object' AND octet_length(metadata::text)<=4096
  AND metadata ?& ARRAY['agent_id','assignment_id','skill_id','change_id','version','assignment_sequence','enabled']
  AND metadata-ARRAY['agent_id','assignment_id','skill_id','change_id','version','assignment_sequence','enabled']='{}'::jsonb
  AND jsonb_typeof(metadata->'agent_id')='string' AND metadata->>'agent_id'=resource_id::text
  AND jsonb_typeof(metadata->'assignment_id')='string' AND metadata->>'assignment_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
  AND jsonb_typeof(metadata->'skill_id')='string' AND metadata->>'skill_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
  AND jsonb_typeof(metadata->'change_id')='string' AND metadata->>'change_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
  AND jsonb_typeof(metadata->'version')='string'
  AND CASE WHEN metadata->>'version' ~ '^[1-9][0-9]{0,18}$' THEN (metadata->>'version')::numeric BETWEEN 2 AND 9223372036854775807 ELSE false END
  AND jsonb_typeof(metadata->'assignment_sequence')='string'
  AND CASE WHEN metadata->>'assignment_sequence' ~ '^[1-9][0-9]{0,18}$' THEN (metadata->>'assignment_sequence')::numeric BETWEEN 2 AND 9223372036854775807 ELSE false END
  AND jsonb_typeof(metadata->'enabled')='boolean' AND (action<>'skill.remove' OR metadata->'enabled'='false'::jsonb))
) IS TRUE);

