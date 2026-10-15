-- agenteam:transaction tx
-- +goose Up
-- Model owns the immutable reference binding. Consumer authorization remains
-- mandatory; this FK records identity and never confers business authority.
CREATE TABLE agenteam_model.serving_snapshot_retentions (
 reference_id agenteam_model.safe_id PRIMARY KEY,
 project_id agenteam_model.safe_id NOT NULL,
 consumer_kind text NOT NULL CHECK(consumer_kind IN ('knowledge','memory')),
 agent_id agenteam_model.safe_id,
 generation_id agenteam_model.safe_id NOT NULL,
 owner_version bigint NOT NULL CHECK(owner_version>0),
 released_version bigint CHECK(released_version>=owner_version),
 snapshot_id agenteam_model.safe_id NOT NULL,
 snapshot_digest text NOT NULL CHECK(snapshot_digest ~ '^sha256:[0-9a-f]{64}$'),
 credential_id agenteam_model.safe_id,
 credential_scope text CHECK(credential_scope IN ('system','project')),
 credential_project_id agenteam_model.safe_id,
 state text NOT NULL CHECK(state IN ('retained','released')),
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 released_at timestamptz(6),
 UNIQUE(project_id,consumer_kind,generation_id),
 FOREIGN KEY(snapshot_id,project_id) REFERENCES agenteam_model.snapshots(id,project_id),
 CHECK((consumer_kind='knowledge' AND agent_id IS NULL) OR (consumer_kind='memory' AND agent_id IS NOT NULL)),
 CHECK((credential_id IS NULL AND credential_scope IS NULL AND credential_project_id IS NULL)
  OR (credential_id IS NOT NULL AND ((credential_scope='system' AND credential_project_id IS NULL)
   OR (credential_scope='project' AND credential_project_id=project_id))) IS TRUE),
 CHECK((state='retained' AND released_at IS NULL AND released_version IS NULL)
  OR (state='released' AND released_at IS NOT NULL AND released_at>=created_at AND released_version IS NOT NULL))
);
CREATE INDEX model_serving_snapshot_retentions_snapshot ON agenteam_model.serving_snapshot_retentions(snapshot_id) WHERE state='retained';
