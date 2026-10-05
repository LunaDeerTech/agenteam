-- agenteam:transaction tx
-- +goose Up
-- Resolution identities are local Model facts. No Secret or consumer FK grants access.
CREATE TABLE agenteam_model.resolution_preparations (
 id agenteam_model.safe_id PRIMARY KEY,
 resolution_identity text NOT NULL UNIQUE CHECK(octet_length(resolution_identity) BETWEEN 1 AND 4096),
 project_id agenteam_model.safe_id NOT NULL,
 owner_kind text NOT NULL CHECK(owner_kind IN ('execution','model_call')),
 owner_id agenteam_model.safe_id NOT NULL,
 semantic_digest text NOT NULL CHECK(semantic_digest ~ '^sha256:[0-9a-f]{64}$'),
 request_data jsonb NOT NULL CHECK((jsonb_typeof(request_data)='object' AND request_data->'format_version'='1'::jsonb AND octet_length(request_data::text)<=16384) IS TRUE),
 snapshot_id agenteam_model.safe_id NOT NULL UNIQUE,
 phase text NOT NULL CHECK(phase IN ('prepared','committed')),
 plan_version bigint NOT NULL CHECK(plan_version>0),
 draft_plan jsonb NOT NULL CHECK((jsonb_typeof(draft_plan)='object' AND draft_plan->'format_version'='1'::jsonb AND octet_length(draft_plan::text)<=262144) IS TRUE),
 created_at timestamptz(6) NOT NULL,
 updated_at timestamptz(6) NOT NULL CHECK(updated_at>=created_at),
 committed_at timestamptz(6),
 CHECK((phase='prepared' AND committed_at IS NULL) OR (phase='committed' AND committed_at IS NOT NULL AND committed_at>=created_at)),
 UNIQUE(id,snapshot_id,project_id,owner_kind,owner_id)
);
CREATE INDEX model_resolution_preparations_project ON agenteam_model.resolution_preparations(project_id,id);
CREATE UNIQUE INDEX model_resolution_single_call ON agenteam_model.resolution_preparations(project_id,owner_id) WHERE owner_kind='model_call';

CREATE TABLE agenteam_model.snapshots (
 id agenteam_model.safe_id PRIMARY KEY,
 project_id agenteam_model.safe_id NOT NULL,
 live_provider_id agenteam_model.safe_id REFERENCES agenteam_model.providers(id) ON DELETE SET NULL,
 live_model_id agenteam_model.safe_id REFERENCES agenteam_model.models(id) ON DELETE SET NULL,
 snapshot_data jsonb NOT NULL CHECK((jsonb_typeof(snapshot_data)='object' AND snapshot_data->'format_version'='1'::jsonb AND octet_length(snapshot_data::text)<=131072) IS TRUE),
 snapshot_digest text NOT NULL CHECK(snapshot_digest ~ '^sha256:[0-9a-f]{64}$'),
 provider_version bigint NOT NULL CHECK(provider_version>0),
 model_version bigint NOT NULL CHECK(model_version>0),
 created_at timestamptz(6) NOT NULL,
 UNIQUE(id,project_id)
);
CREATE INDEX model_snapshots_project ON agenteam_model.snapshots(project_id,id);

CREATE TABLE agenteam_model.snapshot_bindings (
 preparation_id agenteam_model.safe_id PRIMARY KEY,
 snapshot_id agenteam_model.safe_id NOT NULL,
 resolution_identity text NOT NULL UNIQUE,
 project_id agenteam_model.safe_id NOT NULL,
 owner_kind text NOT NULL CHECK(owner_kind IN ('execution','model_call')),
 owner_id agenteam_model.safe_id NOT NULL,
 request_data jsonb NOT NULL CHECK((jsonb_typeof(request_data)='object' AND request_data->'format_version'='1'::jsonb AND octet_length(request_data::text)<=16384) IS TRUE),
 credential_id agenteam_model.safe_id,
 credential_scope text CHECK(credential_scope IN ('system','project')),
 credential_project_id agenteam_model.safe_id,
 lease_id agenteam_model.safe_id,
 created_at timestamptz(6) NOT NULL,
 FOREIGN KEY(preparation_id,snapshot_id,project_id,owner_kind,owner_id) REFERENCES agenteam_model.resolution_preparations(id,snapshot_id,project_id,owner_kind,owner_id),
 FOREIGN KEY(snapshot_id,project_id) REFERENCES agenteam_model.snapshots(id,project_id),
 CHECK((credential_id IS NULL AND credential_scope IS NULL AND credential_project_id IS NULL AND lease_id IS NULL)
 OR (credential_id IS NOT NULL AND lease_id IS NOT NULL AND ((credential_scope='system' AND credential_project_id IS NULL) OR (credential_scope='project' AND credential_project_id=project_id))) IS TRUE)
);
CREATE INDEX model_snapshot_bindings_owner ON agenteam_model.snapshot_bindings(credential_id,credential_scope,credential_project_id,owner_kind,owner_id);
CREATE INDEX model_snapshot_bindings_project ON agenteam_model.snapshot_bindings(project_id,preparation_id);
