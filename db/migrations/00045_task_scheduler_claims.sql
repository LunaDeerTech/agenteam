-- agenteam:transaction tx
-- +goose Up
-- Work's immutable claim fact and the Scheduler pending fact are written in
-- one caller transaction. Neither table depends on an as-yet absent peer row.
CREATE TABLE agenteam_work.task_scheduler_claims (
 id agenteam_work.safe_id PRIMARY KEY,
 project_id agenteam_work.safe_id NOT NULL,
 task_id agenteam_work.safe_id NOT NULL,
 agent_id agenteam_work.safe_id NOT NULL,
 request_id agenteam_work.safe_id NOT NULL,
 expected_version bigint NOT NULL CHECK(expected_version>0 AND expected_version<9223372036854775807),
 claimed_version bigint NOT NULL CHECK(claimed_version=expected_version+1),
 task_event_id agenteam_work.safe_id NOT NULL UNIQUE,
 event_id agenteam_work.safe_id NOT NULL UNIQUE,
 record jsonb NOT NULL,
 created_at timestamptz(6) NOT NULL,
 UNIQUE(project_id,id),
 FOREIGN KEY(project_id,task_id) REFERENCES agenteam_work.tasks(project_id,id) ON DELETE RESTRICT,
 CHECK((jsonb_typeof(record)='object' AND octet_length(record::text)<=4194304
  AND record->'request'->>'DispatchID'=id::text
  AND record->'request'->>'ProjectID'=project_id::text
  AND record->'request'->>'TaskID'=task_id::text
  AND record->'request'->>'AgentID'=agent_id::text
  AND record->'request'->>'RequestID'=request_id::text
  AND record->'request'->>'Purpose'='task/work'
  AND record->'before'->>'state'='todo'
  AND record->'after'->>'state'='in_progress'
  AND record->'before'->>'id'=task_id::text AND record->'after'->>'id'=task_id::text
  AND record->'history'->>'id'=task_event_id::text
  AND record->'header'->>'event_id'=event_id::text) IS TRUE)
);

-- +goose StatementBegin
CREATE FUNCTION agenteam_work.reject_scheduler_claim_rewrite() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='task_scheduler_claims_immutable', MESSAGE='immutable task scheduler claim';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER task_scheduler_claims_immutable BEFORE UPDATE ON agenteam_work.task_scheduler_claims
 FOR EACH ROW EXECUTE FUNCTION agenteam_work.reject_scheduler_claim_rewrite();

ALTER TABLE agenteam_work.task_events ADD COLUMN claim_operation_id agenteam_work.safe_id;
ALTER TABLE agenteam_work.task_events ADD CONSTRAINT task_events_claim_operation_fk
 FOREIGN KEY(project_id,claim_operation_id) REFERENCES agenteam_work.task_scheduler_claims(project_id,id) ON DELETE RESTRICT;
ALTER TABLE agenteam_work.task_events DROP CONSTRAINT task_events_operation_kind_check;
ALTER TABLE agenteam_work.task_events ADD CONSTRAINT task_events_operation_kind_check CHECK((
 (type IN ('task_created','fields_updated') AND operation_id IS NOT NULL AND blocker_operation_id IS NULL AND transition_operation_id IS NULL AND claim_operation_id IS NULL AND correlation_id=operation_id)
 OR (type IN ('blocker_added','blocker_resolved') AND operation_id IS NULL AND blocker_operation_id IS NOT NULL AND transition_operation_id IS NULL AND claim_operation_id IS NULL AND correlation_id=blocker_operation_id)
 OR (type IN ('state_changed','assignee_changed','blocker_added','blocker_resolved','comment') AND operation_id IS NULL AND blocker_operation_id IS NULL AND transition_operation_id IS NOT NULL AND claim_operation_id IS NULL AND correlation_id=transition_operation_id)
 OR (type='state_changed' AND operation_id IS NULL AND blocker_operation_id IS NULL AND transition_operation_id IS NULL AND claim_operation_id IS NOT NULL AND correlation_id=claim_operation_id)
) IS TRUE);

-- Keep the exact existing Human predicate. Only the new independent claim arm
-- accepts the Scheduler projection; no other service or reason is enabled.
-- +goose StatementBegin
DO $$
DECLARE old_name text; old_expr text; matches integer;
BEGIN
 SELECT count(*),min(conname),min(pg_get_expr(conbin,conrelid)) INTO matches,old_name,old_expr
 FROM pg_constraint WHERE conrelid='agenteam_work.task_events'::regclass AND contype='c'
 AND pg_get_constraintdef(oid) LIKE '%jsonb_typeof(actor)%'
 AND pg_get_constraintdef(oid) LIKE '%task_domain%';
 IF matches<>1 THEN RAISE EXCEPTION 'unexpected task history actor constraint'; END IF;
 EXECUTE format('ALTER TABLE agenteam_work.task_events DROP CONSTRAINT %I',old_name);
 EXECUTE format($check$ALTER TABLE agenteam_work.task_events ADD CONSTRAINT task_events_actor_source CHECK((
 (claim_operation_id IS NULL AND (%s)) OR
 (claim_operation_id IS NOT NULL AND jsonb_typeof(actor)='object' AND octet_length(actor::text)<=1024
 AND actor ?& ARRAY['type','service_name','cause_id','source']
 AND actor - ARRAY['type','service_name','cause_id','source'] = '{}'::jsonb
 AND actor->>'type'='system' AND actor->>'service_name'='scheduler' AND actor->>'source'='scheduler'
 AND actor->>'cause_id'=claim_operation_id::text
 AND payload = jsonb_build_object('from_state','todo','to_state','in_progress','reason_code','scheduler_claim'))
 ) IS TRUE)$check$,old_expr);
END;
$$;
-- +goose StatementEnd
CREATE INDEX task_events_claim_operation ON agenteam_work.task_events(project_id,claim_operation_id,id) WHERE claim_operation_id IS NOT NULL;
