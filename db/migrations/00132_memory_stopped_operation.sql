-- agenteam:transaction tx
-- +goose Up
-- Existing rows keep NULL provenance. Missing evidence never becomes proof
-- that their original process, resolution or side effects have settled.
ALTER TABLE agenteam_memory.operations
 ADD COLUMN prepared_process_id agenteam_memory.safe_id,
 ADD COLUMN failure_code text,
 ADD COLUMN failed_at timestamptz(6),
 DROP CONSTRAINT operations_phase_check,
 DROP CONSTRAINT operations_check;
ALTER TABLE agenteam_memory.operations
 ADD CONSTRAINT memory_operation_phase CHECK(phase IN ('prepared','committed','failed')),
 ADD CONSTRAINT memory_operation_result CHECK((
  (phase='prepared' AND receipt IS NULL AND committed_at IS NULL AND failure_code IS NULL AND failed_at IS NULL)
  OR (phase='committed' AND jsonb_typeof(receipt)='object' AND committed_at IS NOT NULL AND failure_code IS NULL AND failed_at IS NULL)
  OR (phase='failed' AND receipt IS NULL AND committed_at IS NULL AND prepared_process_id IS NOT NULL
   AND failure_code='DEPENDENCY_UNAVAILABLE' AND failed_at IS NOT NULL)
 ) IS TRUE);

ALTER TABLE agenteam_memory.model_calls
 ADD COLUMN resolve_source jsonb,
 ADD COLUMN resolve_binding text,
 ADD CONSTRAINT memory_model_resolution_origin CHECK((
  (resolve_source IS NULL AND resolve_binding IS NULL)
  OR (resolve_source IS NOT NULL AND resolve_binding IS NOT NULL AND process_id IS NOT NULL
   AND resolve_binding ~ '^sha256:[0-9a-f]{64}$' AND octet_length(resolve_source::text)<=4096
   AND jsonb_typeof(resolve_source)='object' AND resolve_source->'schema'='"1"'::jsonb
   AND (resolve_source-ARRAY['schema','source','selection','serving_snapshot_id','serving_generation_id'])='{}'::jsonb
   AND (
    (resolve_source->>'source'='current_selection' AND resolve_source ? 'selection'
     AND NOT(resolve_source ?| ARRAY['serving_snapshot_id','serving_generation_id'])
     AND jsonb_typeof(resolve_source->'selection')='object'
     AND ((resolve_source->'selection')-ARRAY['kind','selector','version'])='{}'::jsonb
     AND resolve_source->'selection'->>'kind'='platform'
     AND ((purpose='memory_embedding' AND resolve_source->'selection'->>'selector'='embedding')
       OR (purpose IN ('memory_extraction','memory_consolidation','memory_reflection') AND resolve_source->'selection'->>'selector'='memory'))
     AND (NOT((resolve_source->'selection') ? 'version') OR (jsonb_typeof(resolve_source->'selection'->'version')='string' AND (resolve_source->'selection'->>'version') ~ '^[1-9][0-9]{0,18}$')))
    OR (resolve_source->>'source'='serving_snapshot' AND purpose='memory_embedding' AND NOT(resolve_source ? 'selection')
     AND (resolve_source->>'serving_snapshot_id') ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
     AND (resolve_source->>'serving_generation_id') ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')
   ))
 ) IS TRUE);

-- +goose StatementBegin
CREATE FUNCTION agenteam_memory.operation_origin_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.operation_id,NEW.project_id,NEW.agent_id,NEW.execution_id,NEW.input_digest,NEW.gate_outcome,NEW.created_at,NEW.prepared_process_id)
 IS DISTINCT FROM (OLD.operation_id,OLD.project_id,OLD.agent_id,OLD.execution_id,OLD.input_digest,OLD.gate_outcome,OLD.created_at,OLD.prepared_process_id)
 OR OLD.phase IN ('committed','failed') AND to_jsonb(NEW) IS DISTINCT FROM to_jsonb(OLD) THEN
  RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='memory_operation_origin_immutable',MESSAGE='immutable memory operation';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER memory_operation_origin_immutable BEFORE UPDATE ON agenteam_memory.operations
 FOR EACH ROW EXECUTE FUNCTION agenteam_memory.operation_origin_immutable();

-- +goose StatementBegin
CREATE FUNCTION agenteam_memory.model_origin_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.call_id,NEW.operation_id,NEW.project_id,NEW.agent_id,NEW.execution_id,NEW.purpose,NEW.input_digest,NEW.created_at,NEW.process_id,NEW.resolve_source,NEW.resolve_binding)
 IS DISTINCT FROM (OLD.call_id,OLD.operation_id,OLD.project_id,OLD.agent_id,OLD.execution_id,OLD.purpose,OLD.input_digest,OLD.created_at,OLD.process_id,OLD.resolve_source,OLD.resolve_binding) THEN
  RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='memory_model_origin_immutable',MESSAGE='immutable memory model origin';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER memory_model_origin_immutable BEFORE UPDATE ON agenteam_memory.model_calls
 FOR EACH ROW EXECUTE FUNCTION agenteam_memory.model_origin_immutable();
CREATE INDEX memory_stopped_operations ON agenteam_memory.operations(prepared_process_id,created_at) WHERE phase='prepared';
