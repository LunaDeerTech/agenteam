-- agenteam:transaction tx
-- +goose Up
-- Preserve format-1 Bounded calls. Format 2 records explicit Agent timing,
-- the original Execution lease owner and the unchanged logical input binding.
ALTER TABLE agenteam_model.calls
 DROP CONSTRAINT calls_request_data_check,
 DROP CONSTRAINT calls_phase_check,
 DROP CONSTRAINT calls_check,
 DROP CONSTRAINT calls_check1;

ALTER TABLE agenteam_model.calls
 ADD CONSTRAINT model_calls_request_shape CHECK((
  jsonb_typeof(request_data)='object'
  AND request_data->'format_version' IN ('1'::jsonb,'2'::jsonb)
  AND octet_length(request_data::text)<=32768
 ) IS TRUE),
 ADD CONSTRAINT model_calls_phase CHECK(phase IN ('accepted','running','retry_wait','succeeded','failed','cancelled','unknown')),
 ADD CONSTRAINT model_calls_terminal CHECK(
  (phase IN ('accepted','running','retry_wait') AND finished_at IS NULL AND NOT retired)
  OR (phase IN ('succeeded','failed','cancelled','unknown') AND finished_at IS NOT NULL AND finished_at>=accepted_at)
 ),
 ADD CONSTRAINT model_calls_binding CHECK((
  request_data->>'CallID'=id::text
  AND request_data#>>'{Consumer,project_id}'=project_id::text
  AND request_data->>'SnapshotID'=snapshot_id::text
  AND request_data->>'PreparationID'=preparation_id::text
  AND request_data->>'LeaseID'=lease_id::text
  AND (
   (request_data->'format_version'='1'::jsonb
    AND NOT request_data ? 'agent_retry_timing'
    AND request_data#>>'{Owner,kind}'='model_call'
    AND request_data#>>'{Owner,id}'=id::text
    AND phase<>'retry_wait')
   OR
   (request_data->'format_version'='2'::jsonb
    AND request_data#>>'{Owner,kind}'='execution'
    AND request_data#>>'{Owner,id}'=request_data#>>'{Consumer,execution_id}'
    AND request_data#>>'{Consumer,kind}'='agent'
    AND request_data#>>'{Consumer,purpose}'='agent_generation'
    AND request_data#>>'{Input,execution_id}'=request_data#>>'{Consumer,execution_id}'
    AND request_data#>>'{Initiator,Kind}'='agent_run'
    AND request_data#>>'{Initiator,ProjectID}'=project_id::text
    AND request_data#>>'{Initiator,AgentID}'=request_data#>>'{Consumer,agent_id}'
    AND request_data#>>'{Initiator,ExecutionID}'=request_data#>>'{Consumer,execution_id}'
    AND request_data#>>'{Policy,class}'='agent'
    AND NOT (request_data->'Policy') ?| ARRAY['deadline','max_attempts']
    AND jsonb_typeof(request_data->'agent_retry_timing')='object'
    AND (request_data#>>'{agent_retry_timing,initial_request_timeout}')::bigint>0
    AND (request_data#>>'{agent_retry_timing,max_request_timeout}')::bigint >= (request_data#>>'{agent_retry_timing,initial_request_timeout}')::bigint
    AND (request_data#>>'{agent_retry_timing,timeout_multiplier}')::bigint BETWEEN 2 AND 4294967295
    AND (request_data#>>'{agent_retry_timing,initial_backoff}')::bigint>0
    AND (request_data#>>'{agent_retry_timing,max_backoff}')::bigint >= (request_data#>>'{agent_retry_timing,initial_backoff}')::bigint)
  )
 ) IS TRUE),
 ADD CONSTRAINT model_calls_attempt_parent UNIQUE(id,project_id,process_id,fence);

-- Old attempt rows must survive rotation of the current pointer. The stable
-- parent keeps Call/Project/Process/Fence together; the reciprocal deferred FK
-- guarantees the current pointer names a real attempt at commit, including
-- initial insertion where the call necessarily precedes its first attempt.
-- +goose StatementBegin
DO $model_attempt_parent$
DECLARE constraint_name name;
BEGIN
 SELECT conname INTO STRICT constraint_name FROM pg_constraint
 WHERE conrelid='agenteam_model.runtime_attempts'::regclass
   AND confrelid='agenteam_model.calls'::regclass AND contype='f';
 EXECUTE format('ALTER TABLE agenteam_model.runtime_attempts DROP CONSTRAINT %I',constraint_name);
END;
$model_attempt_parent$;
-- +goose StatementEnd

ALTER TABLE agenteam_model.runtime_attempts
 DROP CONSTRAINT runtime_attempts_ordinal_check,
 DROP CONSTRAINT runtime_attempts_check,
 ADD CONSTRAINT model_runtime_attempt_ordinal CHECK(ordinal>0),
 ADD CONSTRAINT model_runtime_attempt_parent FOREIGN KEY(call_id,project_id,process_id,fence)
  REFERENCES agenteam_model.calls(id,project_id,process_id,fence),
 ADD CONSTRAINT model_runtime_attempt_current UNIQUE(call_id,id,project_id,process_id,fence),
 ADD CONSTRAINT model_runtime_attempt_fact CHECK((
  fact_data->>'Sequence'=sequence::text
  AND fact_data#>>'{Value,id}'=id::text
  AND fact_data#>>'{Value,call_id}'=call_id::text
  AND fact_data#>>'{Value,attempt_index}'=ordinal::text
  AND fact_data#>>'{Value,process_id}'=process_id::text
  AND fact_data#>>'{Value,fence}'=fence::text
  AND fact_data#>>'{Value,dispatch}'=dispatch
  AND fact_data#>>'{Value,consumer,project_id}'=project_id::text
 ) IS TRUE);

ALTER TABLE agenteam_model.calls
 ADD CONSTRAINT model_calls_current_attempt FOREIGN KEY(id,invocation_id,project_id,process_id,fence)
 REFERENCES agenteam_model.runtime_attempts(call_id,id,project_id,process_id,fence)
 DEFERRABLE INITIALLY DEFERRED;

-- The legacy profile remains one attempt even though the shared table now
-- supports Agent attempts. This is not a production Execution authorization.
-- +goose StatementBegin
CREATE FUNCTION agenteam_model.check_runtime_attempt_profile() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE profile integer;
BEGIN
 IF TG_OP='UPDATE' AND jsonb_typeof(OLD.fact_data#>'{Value,final}')='object'
    AND (NEW.call_id,NEW.id,NEW.project_id,NEW.ordinal,NEW.process_id,NEW.fence,NEW.dispatch,NEW.sequence,NEW.fact_data)
      IS DISTINCT FROM (OLD.call_id,OLD.id,OLD.project_id,OLD.ordinal,OLD.process_id,OLD.fence,OLD.dispatch,OLD.sequence,OLD.fact_data) THEN
  RAISE EXCEPTION 'final Model attempt is immutable';
 END IF;
 SELECT (request_data->>'format_version')::integer INTO STRICT profile
 FROM agenteam_model.calls WHERE id=NEW.call_id;
 IF profile=1 AND NEW.ordinal<>1 THEN
  RAISE EXCEPTION 'bounded Model call cannot add an attempt';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER model_runtime_attempt_profile BEFORE INSERT OR UPDATE
ON agenteam_model.runtime_attempts FOR EACH ROW EXECUTE FUNCTION agenteam_model.check_runtime_attempt_profile();
