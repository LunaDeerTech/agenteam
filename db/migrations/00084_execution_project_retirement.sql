-- agenteam:transaction tx
-- +goose Up
-- One complete Execution and its owned references retire in the original
-- Project transaction. SQL state is an additional invariant, never the private
-- Prepared/Applied witness or proof that an original call has returned.
-- Business Execution IDs are TEXT; Project/Skill/call-ownership IDs are UUID.
-- +goose StatementBegin
CREATE FUNCTION agenteam_execution.project_retirement_current(target_project text)
RETURNS boolean LANGUAGE sql STABLE AS $$
SELECT EXISTS (
 SELECT 1 FROM agenteam_project.projects p
 JOIN agenteam_project.lifecycle_operations o ON o.id=p.current_lifecycle_operation_id AND o.project_id=p.id
 JOIN agenteam_project.work_claims w ON w.work_kind='lifecycle' AND w.work_id=o.id AND w.project_id=p.id
 JOIN agenteam_project.lifecycle_participants lp ON lp.operation_id=o.id AND lp.participant_name='execution'
 JOIN agenteam_project.lifecycle_participants predecessor ON predecessor.operation_id=o.id AND predecessor.participant_name='scheduler'
 WHERE p.id::text=target_project AND p.lifecycle='deleting' AND p.initialized_at IS NOT NULL
 AND o.owner_user_id=p.owner_user_id AND o.project_version=p.version
 AND o.action='delete' AND o.state='cleaning' AND o.cleanup_stage='domains'
 AND w.phase='running' AND lp.stop_state='stopped' AND lp.cleanup_state IN ('required','pending') AND lp.attempt_count>0
 AND predecessor.stop_state='stopped' AND predecessor.cleanup_state='completed'
 AND NOT EXISTS(SELECT 1 FROM agenteam_project.lifecycle_participants other WHERE other.operation_id=o.id AND other.stop_state<>'stopped')
);
$$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION agenteam_execution.project_retirement_allowed(target_execution text)
RETURNS boolean LANGUAGE sql STABLE AS $$
SELECT EXISTS(SELECT 1 FROM agenteam_execution.executions e WHERE e.id=target_execution
 AND agenteam_execution.project_retirement_current(e.project_id)
 AND NOT EXISTS(SELECT 1 FROM agenteam_execution.project_calls calls WHERE calls.project_id::text=e.project_id AND calls.phase<>'business_closed'))
AND NOT EXISTS(SELECT 1 FROM agenteam_execution.tool_call_arguments WHERE execution_id=target_execution)
AND NOT EXISTS(SELECT 1 FROM agenteam_execution.tool_calls WHERE execution_id=target_execution)
AND NOT EXISTS(SELECT 1 FROM agenteam_execution.tool_result_consumptions WHERE execution_id=target_execution)
AND NOT EXISTS(SELECT 1 FROM agenteam_execution.round_inputs WHERE execution_id=target_execution)
AND NOT EXISTS(SELECT 1 FROM agenteam_execution.skill_assignment_dispositions WHERE execution_id=target_execution)
AND NOT EXISTS(SELECT 1 FROM agenteam_execution.control_inputs WHERE execution_id=target_execution)
AND NOT EXISTS(SELECT 1 FROM agenteam_execution.round_model_requests WHERE execution_id=target_execution)
AND NOT EXISTS(SELECT 1 FROM agenteam_execution.round_model_claims WHERE execution_id=target_execution)
;
$$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_tool.immutable_execution_capture() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' AND agenteam_execution.project_retirement_allowed(OLD.execution_id::text) THEN RETURN OLD; END IF;
 RAISE EXCEPTION 'TOOL_EXECUTION_CAPTURE_IMMUTABLE' USING ERRCODE='23514';
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_projectvariable.immutable_execution_environment() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' AND agenteam_execution.project_retirement_allowed(OLD.execution_id::text) THEN RETURN OLD; END IF;
 RAISE EXCEPTION 'EXECUTION_ENVIRONMENT_IMMUTABLE' USING ERRCODE='23514';
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_mount.immutable_execution_capture() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' AND agenteam_execution.project_retirement_allowed(OLD.execution_id::text) THEN RETURN OLD; END IF;
 RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='mount_execution_capture_immutable', MESSAGE='immutable mount execution capture';
END;
$$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_mount.immutable_execution_use() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' AND agenteam_execution.project_retirement_allowed(OLD.execution_id::text) THEN RETURN OLD; END IF;
 RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='mount_execution_use_immutable', MESSAGE='immutable mount execution use';
END;
$$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_secret.guard_project_variable_execution_lease_retirement()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN
  IF OLD.released AND OLD.released_at IS NOT NULL AND agenteam_execution.project_retirement_allowed(OLD.execution_id)
  AND EXISTS(SELECT 1 FROM agenteam_execution.executions e WHERE e.id=OLD.execution_id AND e.project_id=OLD.project_id AND e.agent_id=OLD.agent_id)
  THEN RETURN OLD; END IF;
  RAISE EXCEPTION 'EXECUTION_ENVIRONMENT_HISTORY_IMMUTABLE' USING ERRCODE='23514';
 END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.released OR NEW.released_at IS NOT NULL THEN
   RAISE EXCEPTION 'EXECUTION_ENVIRONMENT_LEASE_ALREADY_RELEASED' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF (NEW.lease_id,NEW.execution_id,NEW.project_id,NEW.agent_id,NEW.variable_id,
     NEW.variable_version,NEW.credential_id,NEW.credential_version,NEW.agent_version,NEW.attempt_binding)
  IS DISTINCT FROM
    (OLD.lease_id,OLD.execution_id,OLD.project_id,OLD.agent_id,OLD.variable_id,
     OLD.variable_version,OLD.credential_id,OLD.credential_version,OLD.agent_version,OLD.attempt_binding)
  OR (OLD.released AND (NOT NEW.released OR NEW.released_at IS DISTINCT FROM OLD.released_at)) THEN
  RAISE EXCEPTION 'EXECUTION_ENVIRONMENT_LEASE_RETIREMENT_IMMUTABLE' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_execution.guard_direct_text_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' AND TG_TABLE_NAME IN ('snapshots','transcript_entries')
 AND agenteam_execution.project_retirement_allowed(OLD.execution_id) THEN RETURN OLD; END IF;
 RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='execution_direct_text_immutable', MESSAGE='immutable execution input';
END;
$$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_execution.guard_round_completion() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN
  IF agenteam_execution.project_retirement_allowed(OLD.execution_id) THEN RETURN OLD; END IF;
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='execution_round_immutable', MESSAGE='immutable execution round';
 END IF;
 IF OLD.terminal_status IS NOT NULL OR NEW.terminal_status IS NULL OR
 ROW(NEW.id,NEW.execution_id,NEW.snapshot_id,NEW.start_id,NEW.input_binding_id,NEW.call_id,NEW.input_digest,NEW.context_digest,NEW.schema_version,NEW.content,NEW.digest,NEW.created_at)
 IS DISTINCT FROM ROW(OLD.id,OLD.execution_id,OLD.snapshot_id,OLD.start_id,OLD.input_binding_id,OLD.call_id,OLD.input_digest,OLD.context_digest,OLD.schema_version,OLD.content,OLD.digest,OLD.created_at) THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='execution_round_immutable', MESSAGE='immutable execution round';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_execution.guard_skill_assignment_ingress_retirement() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE target_project uuid; target_agent uuid;
BEGIN
 IF TG_OP='DELETE' AND OLD.outcome='queued' AND OLD.execution_id IS NOT NULL
 AND agenteam_execution.project_retirement_allowed(OLD.execution_id)
 AND EXISTS(SELECT 1 FROM agenteam_execution.executions e WHERE e.id=OLD.execution_id AND e.project_id=OLD.project_id AND e.agent_id=OLD.agent_id)
 THEN RETURN OLD; END IF;
 target_project:=OLD.project_id::uuid; target_agent:=OLD.agent_id::uuid;
 IF TG_OP='DELETE' AND OLD.outcome='no_active_execution' AND OLD.execution_id IS NULL
 AND EXISTS (
 SELECT 1 FROM agenteam_project.projects p
 JOIN agenteam_project.lifecycle_operations o ON o.id=p.current_lifecycle_operation_id AND o.project_id=p.id
 JOIN agenteam_project.work_claims w ON w.work_kind='lifecycle' AND w.work_id=o.id AND w.project_id=p.id
 JOIN agenteam_project.lifecycle_participants lp ON lp.operation_id=o.id AND lp.participant_name='agent-skills-variables'
 WHERE p.id=target_project AND p.lifecycle='deleting' AND p.initialized_at IS NOT NULL
 AND o.owner_user_id=p.owner_user_id AND o.project_version=p.version
 AND o.action='delete' AND o.state='cleaning' AND o.cleanup_stage='domains'
 AND w.phase='running' AND lp.stop_state='stopped' AND lp.cleanup_state IN ('required','pending')
 AND NOT EXISTS (SELECT 1 FROM agenteam_project.lifecycle_participants other WHERE other.operation_id=o.id AND other.stop_state<>'stopped')
)
AND NOT EXISTS (SELECT 1 FROM agenteam_agent.agents WHERE id=target_agent)
AND NOT EXISTS (SELECT 1 FROM agenteam_execution.executions WHERE agent_id=target_agent::text)
 AND NOT EXISTS (SELECT 1 FROM agenteam_execution.skill_assignment_ingress WHERE agent_id=OLD.agent_id AND (project_id<>OLD.project_id OR outcome<>'no_active_execution' OR execution_id IS NOT NULL))
 THEN RETURN OLD; END IF;
 RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='execution_direct_text_immutable', MESSAGE='immutable execution input';
END;
$$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION agenteam_skill.guard_execution_retirement_pending() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' AND agenteam_execution.project_retirement_allowed(OLD.execution_id::text)
 AND EXISTS(SELECT 1 FROM agenteam_execution.executions e WHERE e.id=OLD.execution_id::text AND e.project_id=OLD.project_id::text AND e.agent_id=OLD.agent_id::text)
 AND EXISTS(SELECT 1 FROM agenteam_execution.skill_assignment_ingress ingress WHERE ingress.change_id=OLD.change_id::text AND ingress.project_id=OLD.project_id::text AND ingress.agent_id=OLD.agent_id::text AND ingress.execution_id=OLD.execution_id::text AND ingress.outcome='queued')
 THEN RETURN OLD; END IF;
 RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='immutable Skill assignment fact';
END;
$$;
-- +goose StatementEnd
DROP TRIGGER skill_assignment_pending_immutable ON agenteam_skill.assignment_pending_refs;
CREATE TRIGGER skill_assignment_pending_immutable BEFORE UPDATE OR DELETE ON agenteam_skill.assignment_pending_refs
FOR EACH ROW EXECUTE FUNCTION agenteam_skill.guard_execution_retirement_pending();
-- +goose StatementBegin
CREATE FUNCTION agenteam_execution.guard_retirement_delete() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE target text;
BEGIN
 IF TG_TABLE_NAME='executions' THEN target:=OLD.id; ELSE target:=OLD.execution_id; END IF;
 IF agenteam_execution.project_retirement_allowed(target) THEN RETURN OLD; END IF;
 RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='execution history retained';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER execution_retirement_delete BEFORE DELETE ON agenteam_execution.executions FOR EACH ROW EXECUTE FUNCTION agenteam_execution.guard_retirement_delete();
CREATE TRIGGER execution_retirement_delete BEFORE DELETE ON agenteam_execution.preparation_inputs FOR EACH ROW EXECUTE FUNCTION agenteam_execution.guard_retirement_delete();
CREATE TRIGGER execution_retirement_delete BEFORE DELETE ON agenteam_execution.preparation_claims FOR EACH ROW EXECUTE FUNCTION agenteam_execution.guard_retirement_delete();
CREATE TRIGGER execution_retirement_delete BEFORE DELETE ON agenteam_execution.preparation_attempts FOR EACH ROW EXECUTE FUNCTION agenteam_execution.guard_retirement_delete();
-- +goose StatementBegin
CREATE FUNCTION agenteam_execution.project_retirement_empty(target_project uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
SELECT agenteam_execution.project_retirement_current(target_project::text)
 AND NOT EXISTS(SELECT 1 FROM agenteam_execution.executions WHERE project_id=target_project::text)
 AND NOT EXISTS(SELECT 1 FROM agenteam_execution.skill_assignment_ingress WHERE project_id=target_project::text AND execution_id IS NOT NULL)
 AND NOT EXISTS(SELECT 1 FROM agenteam_execution.project_calls WHERE project_id=target_project AND phase<>'business_closed');
$$;
-- +goose StatementEnd
-- A replayed invocation is not proof of the original business INSERT.
-- This optional result is minted once by the original private business hook.
ALTER TABLE agenteam_execution.project_calls ADD COLUMN original_result jsonb;

-- +goose StatementBegin
CREATE FUNCTION agenteam_execution.project_call_original_result_valid(value jsonb,target_project uuid,target_process uuid,target_lane text)
RETURNS boolean LANGUAGE plpgsql AS $$
DECLARE local_xid numeric;
BEGIN
 IF (jsonb_typeof(value)='object' AND octet_length(value::text)<=2048
 AND value ?& ARRAY['schema','kind','execution_id','subject_id','related_id','version','digest']
 AND value-ARRAY['schema','kind','execution_id','subject_id','related_id','version','digest']='{}'::jsonb
 AND value->'schema'='1'::jsonb
 AND jsonb_typeof(value->'kind')='string' AND jsonb_typeof(value->'execution_id')='string'
 AND jsonb_typeof(value->'subject_id')='string' AND jsonb_typeof(value->'related_id')='string'
 AND jsonb_typeof(value->'version')='number' AND jsonb_typeof(value->'digest')='string'
 AND value->>'execution_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
 AND value->>'subject_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
 AND value->>'version' ~ '^[1-9][0-9]{0,18}$'
 AND value->>'digest' ~ '^sha256:[0-9a-f]{64}$') IS NOT TRUE THEN RETURN false; END IF;
 IF (value->>'version')::numeric>9223372036854775807 THEN RETURN false; END IF;
 -- xmin identifies this transaction's actual inserted fact. It is checked
 -- only while minting, never used later as a substitute for original return.
 local_xid:=pg_current_xact_id()::text::numeric % 4294967296;
 IF value->>'kind'='created' AND target_lane='service' AND value->>'subject_id'=value->>'execution_id'
 AND value->>'related_id'='' AND value->'version'='1'::jsonb THEN
  RETURN EXISTS(SELECT 1 FROM agenteam_execution.executions e WHERE e.id=value->>'execution_id' AND e.project_id=target_project::text AND e.version=1 AND e.request_digest=value->>'digest' AND e.xmin::text::numeric=local_xid);
 ELSIF value->>'kind'='preparation' AND target_lane='preparation' AND value->>'related_id'='' THEN
  RETURN EXISTS(SELECT 1 FROM agenteam_execution.preparation_attempts a
   JOIN agenteam_execution.executions e ON e.id=a.execution_id AND e.project_id=a.project_id AND e.agent_id=a.agent_id
   JOIN agenteam_execution.preparation_claims claim ON claim.execution_id=a.execution_id AND claim.project_id=a.project_id AND claim.agent_id=a.agent_id AND claim.attempt_id=a.attempt_id AND claim.process_id=a.process_id AND claim.fence=a.fence
   WHERE a.execution_id=value->>'execution_id' AND a.project_id=target_project::text AND a.process_id=target_process::text
   AND a.attempt_id=value->>'subject_id' AND a.fence=(value->>'version')::bigint AND a.phase='running' AND a.returned_at IS NULL
   AND e.request_digest=value->>'digest' AND a.xmin::text::numeric=local_xid);
 ELSIF value->>'kind'='snapshot' AND target_lane='direct' AND (value->>'version')::bigint>=2
 AND value->>'related_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$' THEN
  RETURN EXISTS(SELECT 1 FROM agenteam_execution.snapshots sn WHERE sn.execution_id=value->>'execution_id' AND sn.project_id=target_project::text AND sn.process_id=target_process::text AND sn.id=value->>'subject_id' AND sn.start_id=value->>'related_id' AND sn.started_version=(value->>'version')::bigint AND sn.digest=value->>'digest' AND sn.xmin::text::numeric=local_xid);
 END IF;
 RETURN false;
END;
$$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_execution.guard_project_call() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN
  IF OLD.phase='business_closed' AND agenteam_execution.project_retirement_empty(OLD.project_id) THEN RETURN OLD; END IF;
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='project_calls_retained', MESSAGE='execution call history is retained';
 END IF;
 IF TG_OP='INSERT' AND NEW.original_result IS NOT NULL THEN
  RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='execution original result requires original business transaction';
 END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.phase<>'running' OR NEW.closed_at IS NOT NULL THEN
   RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='project_calls_initial', MESSAGE='execution call must start running';
  END IF;
  RETURN NEW;
 END IF;
 IF NEW.original_result IS DISTINCT FROM OLD.original_result THEN
  IF OLD.phase='running' AND NEW.phase='running' AND OLD.original_result IS NULL AND NEW.original_result IS NOT NULL
  AND (to_jsonb(NEW)-'original_result')=(to_jsonb(OLD)-'original_result')
  AND agenteam_execution.project_call_original_result_valid(NEW.original_result,OLD.project_id,OLD.process_id,OLD.lane)
  THEN RETURN NEW; END IF;
  RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='immutable execution original result';
 END IF;
 IF ROW(NEW.id,NEW.project_id,NEW.process_id,NEW.instance_id,NEW.lane,NEW.invocation_identity,NEW.invocation_digest,NEW.registered_at)
 IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.process_id,OLD.instance_id,OLD.lane,OLD.invocation_identity,OLD.invocation_digest,OLD.registered_at)
 OR OLD.phase<>'running' OR NEW.phase<>'business_closed' THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='project_calls_monotonic', MESSAGE='immutable execution call ownership';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
-- Queued receipts are not rewritten. This immutable Skill-owned proof replaces
-- only the retired pending protection, and survives until exact Agent Applied.
CREATE TABLE agenteam_skill.assignment_execution_retirement_proofs (
 change_id agenteam_skill.safe_id PRIMARY KEY,
 project_id agenteam_skill.safe_id NOT NULL,
 agent_id agenteam_skill.safe_id NOT NULL,
 execution_id agenteam_execution.safe_id NOT NULL,
 execution_version bigint NOT NULL CHECK(execution_version>0),
 canonical_digest text NOT NULL CHECK(canonical_digest ~ '^sha256:[0-9a-f]{64}$'),
 operation_id agenteam_project.safe_id NOT NULL,
 project_version bigint NOT NULL CHECK(project_version>0),
 claim_process agenteam_execution.ownership_id NOT NULL,
 claim_attempt agenteam_execution.ownership_id NOT NULL,
 fence bigint NOT NULL CHECK(fence>0),
 transaction_id text NOT NULL CHECK(transaction_id ~ '^[1-9][0-9]{0,19}$'),
 proof_data bytea NOT NULL CHECK(octet_length(proof_data) BETWEEN 2 AND 131072 AND jsonb_typeof(convert_from(proof_data,'UTF8')::jsonb)='object'),
 proof_digest text NOT NULL CHECK(proof_digest='sha256:'||encode(sha256(proof_data),'hex')),
 FOREIGN KEY(project_id,agent_id,change_id) REFERENCES agenteam_skill.assignment_commands(project_id,agent_id,id) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX assignment_execution_retirement_agent ON agenteam_skill.assignment_execution_retirement_proofs(agent_id,change_id);
-- +goose StatementBegin
CREATE FUNCTION agenteam_skill.guard_execution_retirement_proof() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE body jsonb; command agenteam_skill.assignment_commands%ROWTYPE; runtime jsonb;
BEGIN
 IF TG_OP='DELETE' THEN
  IF agenteam_skill.project_agent_history_retirement_allowed(OLD.project_id,OLD.agent_id) THEN RETURN OLD; END IF;
  RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='Skill execution retirement proof retained';
 END IF;
 IF TG_OP='UPDATE' THEN RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='immutable Skill execution retirement proof'; END IF;
 IF NOT agenteam_execution.project_retirement_current(NEW.project_id::text)
 OR EXISTS(SELECT 1 FROM agenteam_execution.executions WHERE id=NEW.execution_id)
 OR EXISTS(SELECT 1 FROM agenteam_skill.assignment_pending_refs WHERE change_id=NEW.change_id)
 OR EXISTS(SELECT 1 FROM agenteam_execution.skill_assignment_ingress WHERE change_id=NEW.change_id::text)
 OR NEW.transaction_id<>pg_current_xact_id()::text
 OR NOT EXISTS(SELECT 1 FROM agenteam_project.projects p JOIN agenteam_project.lifecycle_operations o ON o.id=p.current_lifecycle_operation_id AND o.project_id=p.id JOIN agenteam_project.work_claims w ON w.work_kind='lifecycle' AND w.work_id=o.id AND w.project_id=p.id WHERE p.id=NEW.project_id AND o.id=NEW.operation_id AND o.project_version=NEW.project_version AND w.process_id=NEW.claim_process AND w.attempt_id=NEW.claim_attempt AND w.fence=NEW.fence AND w.phase='running')
 THEN RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='invalid Skill execution retirement transaction'; END IF;
 SELECT * INTO STRICT command FROM agenteam_skill.assignment_commands WHERE id=NEW.change_id AND project_id=NEW.project_id AND agent_id=NEW.agent_id AND state='completed';
 body:=convert_from(NEW.proof_data,'UTF8')::jsonb;
 runtime:=convert_from(command.runtime_receipt,'UTF8')::jsonb;
 IF NOT (body->'format'='1'::jsonb
 AND body->'runtime_receipt'=runtime
 AND runtime->'queued'='true'::jsonb AND runtime->>'execution_id'=NEW.execution_id
 AND body->'pending'->>'change_id'=NEW.change_id::text AND body->'pending'->>'project_id'=NEW.project_id::text
 AND body->'pending'->>'agent_id'=NEW.agent_id::text AND body->'pending'->>'execution_id'=NEW.execution_id
 AND body->>'command_digest'=command.semantic_digest
 AND body->>'receipt_digest'='sha256:'||encode(sha256(command.receipt),'hex')) IS TRUE
 THEN RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='invalid Skill queued retirement proof'; END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER skill_execution_retirement_proof_guard BEFORE INSERT OR UPDATE OR DELETE ON agenteam_skill.assignment_execution_retirement_proofs FOR EACH ROW EXECUTE FUNCTION agenteam_skill.guard_execution_retirement_proof();
