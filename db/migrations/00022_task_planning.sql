-- agenteam:transaction tx
-- +goose Up
ALTER TABLE agenteam_work.sprints ADD CONSTRAINT sprints_project_milestone_id_key UNIQUE(project_id,milestone_id,id);

CREATE TABLE agenteam_work.tasks (
 id agenteam_work.safe_id PRIMARY KEY,
 project_id agenteam_work.safe_id NOT NULL,
 milestone_id agenteam_work.safe_id NOT NULL,
 sprint_id agenteam_work.safe_id NOT NULL,
 title text NOT NULL CHECK(octet_length(title) BETWEEN 1 AND 1024 AND char_length(title)<=256),
 description text NOT NULL CHECK(octet_length(description)<=32768),
 type text NOT NULL CHECK(type IN ('feature','bug','task','spike','chore')),
 priority text NOT NULL CHECK(priority IN ('low','medium','high','critical')),
 state text NOT NULL CHECK(state IN ('backlog','todo','in_progress','in_review','blocked','done','cancelled')),
 assignee_agent_id agenteam_work.safe_id,
 plan text NOT NULL CHECK(octet_length(plan)<=32768),
 manual_rank text COLLATE "C" NOT NULL CHECK(manual_rank ~ '^[0-9a-f]{32}$' AND manual_rank>'00000000000000000000000000000000' AND manual_rank<'ffffffffffffffffffffffffffffffff'),
 version bigint NOT NULL CHECK(version>0),
 created_at timestamptz(6) NOT NULL,
 updated_at timestamptz(6) NOT NULL CHECK(updated_at>=created_at),
 CONSTRAINT tasks_project_id_key UNIQUE(project_id,id),
 CONSTRAINT tasks_parent_fk FOREIGN KEY(project_id,milestone_id,sprint_id) REFERENCES agenteam_work.sprints(project_id,milestone_id,id) ON DELETE RESTRICT,
 CONSTRAINT tasks_group_rank_key UNIQUE(project_id,sprint_id,state,priority,manual_rank) DEFERRABLE INITIALLY DEFERRED,
 CHECK(state NOT IN ('todo','in_progress','in_review') OR assignee_agent_id IS NOT NULL)
);
CREATE INDEX tasks_page ON agenteam_work.tasks(project_id,sprint_id,
 (CASE state WHEN 'backlog' THEN 0 WHEN 'todo' THEN 1 WHEN 'in_progress' THEN 2 WHEN 'in_review' THEN 3 WHEN 'blocked' THEN 4 WHEN 'done' THEN 5 WHEN 'cancelled' THEN 6 END),
 (CASE priority WHEN 'critical' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 WHEN 'low' THEN 3 END),manual_rank COLLATE "C",id);
CREATE INDEX tasks_sprint_membership ON agenteam_work.tasks(project_id,sprint_id);

CREATE TABLE agenteam_work.task_order_groups (
 project_id agenteam_work.safe_id NOT NULL,
 milestone_id agenteam_work.safe_id NOT NULL,
 sprint_id agenteam_work.safe_id NOT NULL,
 state text NOT NULL CHECK(state IN ('backlog','todo','in_progress','in_review','blocked','done','cancelled')),
 priority text NOT NULL CHECK(priority IN ('low','medium','high','critical')),
 order_generation bigint NOT NULL CHECK(order_generation>0),
 PRIMARY KEY(project_id,sprint_id,state,priority),
 CONSTRAINT task_order_groups_parent_fk FOREIGN KEY(project_id,milestone_id,sprint_id) REFERENCES agenteam_work.sprints(project_id,milestone_id,id) ON DELETE RESTRICT
);
CREATE TABLE agenteam_work.task_query_generations (
 project_id agenteam_work.safe_id PRIMARY KEY,
 query_generation bigint NOT NULL CHECK(query_generation>0)
);

CREATE TABLE agenteam_work.task_commands (
 id agenteam_work.safe_id PRIMARY KEY,
 project_id agenteam_work.safe_id NOT NULL,
 actor_user_id agenteam_work.safe_id NOT NULL,
 command_name text NOT NULL CHECK(command_name IN ('work.task.create','work.task.update','work.task.reorder')),
 idempotency_key text NOT NULL CHECK(octet_length(idempotency_key) BETWEEN 1 AND 128 AND idempotency_key ~ '^[A-Za-z0-9._:/-]+$'),
 semantic_digest text NOT NULL CHECK(semantic_digest ~ '^sha256:[0-9a-f]{64}$'),
 request jsonb NOT NULL CHECK(jsonb_typeof(request)='object' AND octet_length(request::text)<=524288),
 plan_revision bigint NOT NULL CHECK(plan_revision>=1),
 state text NOT NULL CHECK(state IN ('planned','completed')),
 plan jsonb CHECK(plan IS NULL OR (jsonb_typeof(plan)='object' AND octet_length(plan::text)<=4194304)),
 task_event_id agenteam_work.safe_id UNIQUE,
 event_id agenteam_work.safe_id UNIQUE,
 receipt jsonb CHECK(receipt IS NULL OR (jsonb_typeof(receipt)='object' AND octet_length(receipt::text)<=524288)),
 created_at timestamptz(6) NOT NULL,
 committed_at timestamptz(6) CHECK(committed_at IS NULL OR committed_at>=created_at),
 CONSTRAINT task_commands_project_id_key UNIQUE(project_id,id),
 CONSTRAINT task_commands_identity_key UNIQUE(project_id,command_name,idempotency_key),
 CHECK((plan IS NULL)=(task_event_id IS NULL) AND (task_event_id IS NULL)=(event_id IS NULL)),
 CHECK(((state='planned' AND plan IS NOT NULL AND receipt IS NULL AND committed_at IS NULL)
 OR (state='completed' AND receipt IS NOT NULL AND committed_at IS NOT NULL
 AND jsonb_typeof(receipt->'changed')='boolean'
 AND jsonb_typeof(receipt->'event_ids')='array'
 AND ((receipt->'changed'='true'::jsonb AND plan IS NOT NULL
 AND receipt->>'task_event_id'=task_event_id::text
 AND jsonb_array_length(receipt->'event_ids')=1 AND receipt->'event_ids'->>0=event_id::text)
 OR (receipt->'changed'='false'::jsonb AND plan IS NULL
 AND receipt->'task_event_id'='null'::jsonb AND jsonb_array_length(receipt->'event_ids')=0)))) IS TRUE)
);

CREATE TABLE agenteam_work.task_events (
 id agenteam_work.safe_id PRIMARY KEY,
 project_id agenteam_work.safe_id NOT NULL,
 task_id agenteam_work.safe_id NOT NULL,
 task_version bigint NOT NULL CHECK(task_version>0),
 type text NOT NULL CHECK(type IN ('task_created','fields_updated')),
 actor jsonb NOT NULL CHECK((jsonb_typeof(actor)='object' AND octet_length(actor::text)<=1024 AND actor->>'type'='human' AND actor->>'source'='task_domain' AND actor->>'user_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$') IS TRUE),
 operation_id agenteam_work.safe_id NOT NULL,
 correlation_id agenteam_work.safe_id NOT NULL CHECK(correlation_id=operation_id),
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object' AND octet_length(payload::text)<=8192),
 created_at timestamptz(6) NOT NULL,
 CONSTRAINT task_events_task_fk FOREIGN KEY(project_id,task_id) REFERENCES agenteam_work.tasks(project_id,id) ON DELETE RESTRICT,
 CONSTRAINT task_events_operation_fk FOREIGN KEY(project_id,operation_id) REFERENCES agenteam_work.task_commands(project_id,id) ON DELETE RESTRICT
);
CREATE INDEX task_events_timeline ON agenteam_work.task_events(project_id,task_id,created_at,id);
