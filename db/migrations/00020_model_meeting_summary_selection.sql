-- agenteam:transaction tx
-- +goose Up
-- Independent, initially unconfigured system Meeting Rolling Summary choice.
CREATE TABLE agenteam_model.meeting_summary_selection (
 id agenteam_model.safe_id PRIMARY KEY,
 singleton boolean NOT NULL DEFAULT true UNIQUE CHECK(singleton),
 version bigint NOT NULL CHECK(version>0),
 model_id agenteam_model.safe_id REFERENCES agenteam_model.models(id) ON DELETE RESTRICT,
 updated_at timestamptz(6) NOT NULL
);

-- Preserve all old owner/role branches; only the new system role is added.
ALTER TABLE agenteam_model.references DROP CONSTRAINT references_check;
ALTER TABLE agenteam_model.references ADD CONSTRAINT references_check CHECK (
 (owner_kind='platform_selector' AND project_id IS NULL AND role IN ('embedding','memory','reranker','image'))
 OR (owner_kind='platform_selector' AND project_id IS NULL AND role='meeting_summary' AND reasoning_effort='')
 OR (owner_kind='agent' AND project_id IS NOT NULL AND role IN ('agent_model','approval_model'))
 OR (owner_kind='project_summary' AND project_id IS NOT NULL AND owner_id=project_id AND role='meeting_summary')
);
