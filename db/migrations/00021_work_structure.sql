-- agenteam:transaction tx
-- +goose Up
CREATE SCHEMA agenteam_work;
CREATE DOMAIN agenteam_work.safe_id AS uuid CHECK (VALUE::text ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$');

CREATE TABLE agenteam_work.milestones (
 id agenteam_work.safe_id PRIMARY KEY,
 project_id agenteam_work.safe_id NOT NULL,
 title text NOT NULL CHECK(octet_length(title) BETWEEN 1 AND 1024 AND char_length(title)<=256),
 description text NOT NULL CHECK(octet_length(description)<=32768),
 manual_rank text COLLATE "C" NOT NULL CHECK(manual_rank ~ '^[0-9a-f]{32}$' AND manual_rank>'00000000000000000000000000000000' AND manual_rank<'ffffffffffffffffffffffffffffffff'),
 version bigint NOT NULL CHECK(version>0),
 created_at timestamptz(6) NOT NULL,
 updated_at timestamptz(6) NOT NULL CHECK(updated_at>=created_at),
 CONSTRAINT milestones_project_id_key UNIQUE(project_id,id),
 CONSTRAINT milestones_group_rank_key UNIQUE(project_id,manual_rank) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX milestones_page ON agenteam_work.milestones(project_id,manual_rank,id);

CREATE TABLE agenteam_work.sprints (
 id agenteam_work.safe_id PRIMARY KEY,
 project_id agenteam_work.safe_id NOT NULL,
 milestone_id agenteam_work.safe_id NOT NULL,
 title text NOT NULL CHECK(octet_length(title) BETWEEN 1 AND 1024 AND char_length(title)<=256),
 description text NOT NULL CHECK(octet_length(description)<=32768),
 manual_rank text COLLATE "C" NOT NULL CHECK(manual_rank ~ '^[0-9a-f]{32}$' AND manual_rank>'00000000000000000000000000000000' AND manual_rank<'ffffffffffffffffffffffffffffffff'),
 version bigint NOT NULL CHECK(version>0),
 created_at timestamptz(6) NOT NULL,
 updated_at timestamptz(6) NOT NULL CHECK(updated_at>=created_at),
 started_at timestamptz(6),started_by jsonb,
 completed_at timestamptz(6),completed_by jsonb,
 CONSTRAINT sprints_parent_fk FOREIGN KEY(project_id,milestone_id) REFERENCES agenteam_work.milestones(project_id,id) ON DELETE RESTRICT,
 CONSTRAINT sprints_group_rank_key UNIQUE(project_id,milestone_id,manual_rank) DEFERRABLE INITIALLY DEFERRED,
 CHECK((started_at IS NULL)=(started_by IS NULL)),
 CHECK((completed_at IS NULL)=(completed_by IS NULL)),
 CHECK(started_by IS NULL OR (jsonb_typeof(started_by)='object' AND octet_length(started_by::text)<=1024)),
 CHECK(completed_by IS NULL OR (jsonb_typeof(completed_by)='object' AND octet_length(completed_by::text)<=1024)),
 CHECK(started_at IS NULL OR (started_at>=created_at AND started_at<=updated_at)),
 CHECK(completed_at IS NULL OR (started_at IS NOT NULL AND completed_at>=started_at AND completed_at<=updated_at))
);
CREATE INDEX sprints_page ON agenteam_work.sprints(project_id,milestone_id,manual_rank,id);
CREATE UNIQUE INDEX sprints_one_started ON agenteam_work.sprints(project_id) WHERE started_at IS NOT NULL AND completed_at IS NULL;

CREATE TABLE agenteam_work.milestone_order_groups (
 project_id agenteam_work.safe_id PRIMARY KEY,
 order_generation bigint NOT NULL CHECK(order_generation>0)
);
CREATE TABLE agenteam_work.sprint_order_groups (
 project_id agenteam_work.safe_id NOT NULL,
 milestone_id agenteam_work.safe_id NOT NULL,
 order_generation bigint NOT NULL CHECK(order_generation>0),
 PRIMARY KEY(project_id,milestone_id),
 CONSTRAINT sprint_order_groups_parent_fk FOREIGN KEY(project_id,milestone_id) REFERENCES agenteam_work.milestones(project_id,id) ON DELETE RESTRICT
);

CREATE TABLE agenteam_work.structure_commands (
 id agenteam_work.safe_id PRIMARY KEY,
 project_id agenteam_work.safe_id NOT NULL,
 actor_user_id agenteam_work.safe_id NOT NULL,
 command_name text NOT NULL CHECK(command_name IN ('work.milestone.create','work.milestone.update','work.milestone.reorder','work.sprint.create','work.sprint.update','work.sprint.reorder')),
 idempotency_key text NOT NULL CHECK(octet_length(idempotency_key) BETWEEN 1 AND 128 AND idempotency_key ~ '^[A-Za-z0-9._:/-]+$'),
 semantic_digest text NOT NULL CHECK(semantic_digest ~ '^sha256:[0-9a-f]{64}$'),
 request jsonb NOT NULL CHECK(jsonb_typeof(request)='object' AND octet_length(request::text)<=262144),
 plan_revision bigint NOT NULL CHECK(plan_revision>=1),
 state text NOT NULL CHECK(state IN ('planned','completed')),
 plan jsonb CHECK(plan IS NULL OR (jsonb_typeof(plan)='object' AND octet_length(plan::text)<=1048576)),
 event_id agenteam_work.safe_id UNIQUE,
 receipt jsonb CHECK(receipt IS NULL OR (jsonb_typeof(receipt)='object' AND octet_length(receipt::text)<=262144)),
 created_at timestamptz(6) NOT NULL,
 committed_at timestamptz(6) CHECK(committed_at IS NULL OR committed_at>=created_at),
 CONSTRAINT structure_commands_identity_key UNIQUE(project_id,command_name,idempotency_key),
 CHECK((plan IS NULL)=(event_id IS NULL)),
 CHECK(((state='planned' AND plan IS NOT NULL AND receipt IS NULL AND committed_at IS NULL)
 OR (state='completed' AND receipt IS NOT NULL AND committed_at IS NOT NULL
 AND jsonb_typeof(receipt->'changed')='boolean'
 AND ((receipt->'changed'='true'::jsonb AND plan IS NOT NULL AND receipt->>'event_id'=event_id::text)
 OR (receipt->'changed'='false'::jsonb AND plan IS NULL AND receipt->'event_id'='null'::jsonb)))) IS TRUE)
);
