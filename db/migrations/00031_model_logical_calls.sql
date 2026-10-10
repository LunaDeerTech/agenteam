-- agenteam:transaction tx
-- +goose Up
-- Runtime is the canonical call/attempt owner. 00018 remains the Usage ledger.
-- This version supports exactly one attempt and stores no request/answer text.
CREATE TABLE agenteam_model.calls (
 id agenteam_model.safe_id PRIMARY KEY,
 project_id agenteam_model.safe_id NOT NULL,
 snapshot_id agenteam_model.safe_id NOT NULL,
 preparation_id agenteam_model.safe_id NOT NULL,
 lease_id agenteam_model.safe_id NOT NULL,
 invocation_id agenteam_model.safe_id NOT NULL UNIQUE,
 process_id agenteam_model.safe_id NOT NULL,
 fence bigint NOT NULL CHECK(fence=1),
 request_binding text NOT NULL CHECK(request_binding ~ '^sha256:[0-9a-f]{64}$'),
 request_data jsonb NOT NULL CHECK((jsonb_typeof(request_data)='object' AND request_data->'format_version'='1'::jsonb AND octet_length(request_data::text)<=32768) IS TRUE),
 phase text NOT NULL CHECK(phase IN ('accepted','running','succeeded','failed','cancelled','unknown')),
 accepted_at timestamptz(6) NOT NULL,
 finished_at timestamptz(6),
 retired boolean NOT NULL DEFAULT false,
 version bigint NOT NULL CHECK(version>0),
 CHECK((phase IN ('accepted','running') AND finished_at IS NULL AND NOT retired) OR (phase IN ('succeeded','failed','cancelled','unknown') AND finished_at IS NOT NULL AND finished_at>=accepted_at)),
 FOREIGN KEY(snapshot_id,project_id) REFERENCES agenteam_model.snapshots(id,project_id),
 FOREIGN KEY(preparation_id) REFERENCES agenteam_model.snapshot_bindings(preparation_id),
 UNIQUE(id,project_id),
 UNIQUE(id,invocation_id,project_id,process_id,fence),
 CHECK((request_data->>'CallID'=id::text AND request_data#>>'{Consumer,project_id}'=project_id::text AND request_data->>'SnapshotID'=snapshot_id::text AND request_data->>'PreparationID'=preparation_id::text AND request_data->>'LeaseID'=lease_id::text AND request_data#>>'{Owner,kind}'='model_call' AND request_data#>>'{Owner,id}'=id::text) IS TRUE)
);
CREATE INDEX model_calls_unretired ON agenteam_model.calls(process_id,id) WHERE NOT retired;

CREATE TABLE agenteam_model.runtime_attempts (
 id agenteam_model.safe_id PRIMARY KEY,
 call_id agenteam_model.safe_id NOT NULL,
 project_id agenteam_model.safe_id NOT NULL,
 ordinal bigint NOT NULL CHECK(ordinal=1),
 process_id agenteam_model.safe_id NOT NULL,
 fence bigint NOT NULL CHECK(fence=1),
 dispatch text NOT NULL CHECK(dispatch IN ('reserved','authorized','sent','not_sent','unknown')),
 sequence bigint NOT NULL CHECK(sequence>0),
 fact_data jsonb NOT NULL CHECK((jsonb_typeof(fact_data)='object' AND fact_data->'format_version'='1'::jsonb AND octet_length(fact_data::text)<=32768) IS TRUE),
 UNIQUE(call_id,ordinal),
 FOREIGN KEY(call_id,id,project_id,process_id,fence) REFERENCES agenteam_model.calls(id,invocation_id,project_id,process_id,fence),
 CHECK((fact_data->>'Sequence'=sequence::text AND fact_data#>>'{Value,id}'=id::text AND fact_data#>>'{Value,call_id}'=call_id::text AND fact_data#>>'{Value,attempt_index}'='1' AND fact_data#>>'{Value,process_id}'=process_id::text AND fact_data#>>'{Value,fence}'='1' AND fact_data#>>'{Value,dispatch}'=dispatch AND fact_data#>>'{Value,consumer,project_id}'=project_id::text) IS TRUE)
);
