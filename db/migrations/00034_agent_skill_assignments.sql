-- agenteam:transaction tx
-- +goose Up
-- Agent owns its canonical configuration and creation witness. These are
-- Skills-owned facts, written only in that same caller transaction. No
-- cross-domain FK or Agent-table SQL substitutes for the owner authority.
ALTER TABLE agenteam_skill.skills ADD CONSTRAINT skill_project_identity UNIQUE(project_id,id);

CREATE TABLE agenteam_skill.agent_assignment_heads (
 project_id agenteam_skill.safe_id NOT NULL,
 agent_id agenteam_skill.safe_id NOT NULL UNIQUE,
 creation_command text NOT NULL CHECK(octet_length(creation_command) BETWEEN 1 AND 1024),
 plan_revision bigint NOT NULL CHECK(plan_revision>0),
 request_binding text NOT NULL CHECK(request_binding ~ '^sha256:[0-9a-f]{64}$'),
 add_skills_enabled boolean NOT NULL,
 assignment_sequence bigint NOT NULL CHECK(assignment_sequence>0),
 initial_skill_id agenteam_skill.safe_id NOT NULL,
 observed_revision bigint NOT NULL CHECK(observed_revision>0),
 initial_assignment_id agenteam_skill.safe_id,
 created_at timestamptz(6) NOT NULL,
 PRIMARY KEY(project_id,agent_id),
 UNIQUE(project_id,creation_command),
 CHECK(add_skills_enabled=(initial_assignment_id IS NOT NULL)),
 FOREIGN KEY(project_id,initial_skill_id) REFERENCES agenteam_skill.skills(project_id,id) DEFERRABLE INITIALLY DEFERRED
);
CREATE TABLE agenteam_skill.agent_assignments (
 id agenteam_skill.safe_id PRIMARY KEY,
 project_id agenteam_skill.safe_id NOT NULL,
 agent_id agenteam_skill.safe_id NOT NULL,
 skill_id agenteam_skill.safe_id NOT NULL,
 enabled boolean NOT NULL,
 assignment_sequence bigint NOT NULL CHECK(assignment_sequence>0),
 created_at timestamptz(6) NOT NULL,
 removed_at timestamptz(6) CHECK(removed_at>=created_at),
 UNIQUE(project_id,agent_id,id),
 FOREIGN KEY(project_id,agent_id) REFERENCES agenteam_skill.agent_assignment_heads(project_id,agent_id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(project_id,skill_id) REFERENCES agenteam_skill.skills(project_id,id) DEFERRABLE INITIALLY DEFERRED
);
ALTER TABLE agenteam_skill.agent_assignment_heads ADD CONSTRAINT skill_initial_assignment_identity
 FOREIGN KEY(project_id,agent_id,initial_assignment_id)
 REFERENCES agenteam_skill.agent_assignments(project_id,agent_id,id) DEFERRABLE INITIALLY DEFERRED;
CREATE UNIQUE INDEX skill_live_agent_assignment ON agenteam_skill.agent_assignments(project_id,agent_id,skill_id) WHERE removed_at IS NULL;
CREATE INDEX skill_assignment_project ON agenteam_skill.agent_assignments(project_id,id);
