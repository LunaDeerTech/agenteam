-- agenteam:transaction tx
-- +goose Up
CREATE SCHEMA agenteam_meeting;
CREATE DOMAIN agenteam_meeting.safe_id AS text CHECK(VALUE ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$');
CREATE TABLE agenteam_meeting.sessions (
 id agenteam_meeting.safe_id PRIMARY KEY, project_id agenteam_meeting.safe_id NOT NULL,
 status text NOT NULL CHECK(status IN('proposed','active','archive')), version bigint NOT NULL CHECK(version>0),
 title text, proposal_content text, summary jsonb, summary_version bigint NOT NULL DEFAULT 0 CHECK(summary_version>=0),
 summary_input_digest text, deleting boolean NOT NULL DEFAULT false,
 created_at timestamptz(6) NOT NULL, updated_at timestamptz(6) NOT NULL,
 UNIQUE(project_id,id), CHECK(updated_at>=created_at),
 CHECK((summary_version=0 AND summary IS NULL AND summary_input_digest IS NULL AND title IS NULL) OR (summary_version>0 AND summary IS NOT NULL AND summary_input_digest ~ '^sha256:[0-9a-f]{64}$' AND title IS NOT NULL))
);
CREATE INDEX meeting_project_sessions ON agenteam_meeting.sessions(project_id,updated_at DESC,id DESC);
CREATE TABLE agenteam_meeting.participants (
 id agenteam_meeting.safe_id PRIMARY KEY, meeting_id agenteam_meeting.safe_id NOT NULL REFERENCES agenteam_meeting.sessions(id),
 kind text NOT NULL CHECK(kind IN('user','agent')), source_id agenteam_meeting.safe_id NOT NULL,
 name text NOT NULL, description text NOT NULL DEFAULT '', sort_order integer NOT NULL CHECK(sort_order>=0),
 active boolean NOT NULL DEFAULT true, created_at timestamptz(6) NOT NULL,
 UNIQUE(meeting_id,id)
);
CREATE UNIQUE INDEX meeting_active_participant ON agenteam_meeting.participants(meeting_id,kind,source_id) WHERE active;
CREATE TABLE agenteam_meeting.meeting_references (
 meeting_id agenteam_meeting.safe_id NOT NULL REFERENCES agenteam_meeting.sessions(id),
 kind text NOT NULL CHECK(kind IN('task','knowledge','link','file')), stable_id text NOT NULL, label text NOT NULL DEFAULT '',
 sort_order integer NOT NULL CHECK(sort_order>=0), PRIMARY KEY(meeting_id,kind,stable_id)
);
CREATE TABLE agenteam_meeting.messages (
 id agenteam_meeting.safe_id PRIMARY KEY, meeting_id agenteam_meeting.safe_id NOT NULL REFERENCES agenteam_meeting.sessions(id),
 participant_id agenteam_meeting.safe_id NOT NULL, content jsonb NOT NULL CHECK(jsonb_typeof(content)='array'),
 content_digest text NOT NULL CHECK(content_digest ~ '^sha256:[0-9a-f]{64}$'), reply_to_id agenteam_meeting.safe_id,
 execution_id agenteam_meeting.safe_id UNIQUE, created_at timestamptz(6) NOT NULL,
 UNIQUE(meeting_id,id), FOREIGN KEY(meeting_id,participant_id) REFERENCES agenteam_meeting.participants(meeting_id,id),
 FOREIGN KEY(meeting_id,reply_to_id) REFERENCES agenteam_meeting.messages(meeting_id,id)
);
CREATE TABLE agenteam_meeting.message_references (
 message_id agenteam_meeting.safe_id NOT NULL REFERENCES agenteam_meeting.messages(id), node_index integer NOT NULL CHECK(node_index>=0),
 kind text NOT NULL CHECK(kind IN('task','knowledge','link','file')), stable_id text NOT NULL, PRIMARY KEY(message_id,node_index)
);
CREATE TABLE agenteam_meeting.turns (
 id agenteam_meeting.safe_id PRIMARY KEY, meeting_id agenteam_meeting.safe_id NOT NULL REFERENCES agenteam_meeting.sessions(id),
 mode text NOT NULL CHECK(mode IN('sequential','parallel')), status text NOT NULL CHECK(status IN('queued','running','finalizing','completed','cancelled')),
 version bigint NOT NULL CHECK(version>0), result text CHECK(result IN('success','partial_failure')), participants jsonb NOT NULL,
 interrupted_by_id agenteam_meeting.safe_id, cancel_requested boolean NOT NULL DEFAULT false,
 summary_error text NOT NULL DEFAULT '', created_at timestamptz(6) NOT NULL, completed_at timestamptz(6),
 UNIQUE(meeting_id,id), FOREIGN KEY(meeting_id,interrupted_by_id) REFERENCES agenteam_meeting.turns(meeting_id,id) DEFERRABLE INITIALLY DEFERRED
);
CREATE UNIQUE INDEX meeting_one_active_turn ON agenteam_meeting.turns(meeting_id) WHERE status IN('running','finalizing');
CREATE INDEX meeting_turn_queue ON agenteam_meeting.turns(meeting_id,created_at,id) WHERE status='queued';
CREATE TABLE agenteam_meeting.contributions (
 id agenteam_meeting.safe_id PRIMARY KEY, meeting_id agenteam_meeting.safe_id NOT NULL,
 turn_id agenteam_meeting.safe_id NOT NULL, participant_id agenteam_meeting.safe_id NOT NULL,
 kind text NOT NULL CHECK(kind IN('user','agent')), sort_order integer NOT NULL CHECK(sort_order>=0),
 status text NOT NULL CHECK(status IN('pending','waiting_for_agent','running','completed','failed','cancelled','skipped')),
 generation bigint NOT NULL DEFAULT 1 CHECK(generation>0), attempt bigint NOT NULL DEFAULT 1 CHECK(attempt>0),
 current_execution_id agenteam_meeting.safe_id, current_message_id agenteam_meeting.safe_id,
 waiting_on_execution_id agenteam_meeting.safe_id, started_at timestamptz(6), completed_at timestamptz(6),
 UNIQUE(turn_id,participant_id), UNIQUE(turn_id,sort_order), UNIQUE(meeting_id,id),
 FOREIGN KEY(meeting_id,turn_id) REFERENCES agenteam_meeting.turns(meeting_id,id),
 FOREIGN KEY(meeting_id,participant_id) REFERENCES agenteam_meeting.participants(meeting_id,id),
 FOREIGN KEY(meeting_id,current_message_id) REFERENCES agenteam_meeting.messages(meeting_id,id),
 CHECK((status='waiting_for_agent' AND current_execution_id IS NULL AND waiting_on_execution_id IS NOT NULL) OR (status<>'waiting_for_agent' AND waiting_on_execution_id IS NULL)),
 CHECK(kind='agent' OR (status='completed' AND current_execution_id IS NULL AND current_message_id IS NOT NULL))
);
CREATE TABLE agenteam_meeting.inputs (
 id agenteam_meeting.safe_id PRIMARY KEY, meeting_id agenteam_meeting.safe_id NOT NULL REFERENCES agenteam_meeting.sessions(id),
 input_data bytea NOT NULL CHECK(octet_length(input_data) BETWEEN 2 AND 1048576),
 input_digest text NOT NULL CHECK(input_digest='sha256:'||encode(sha256(input_data),'hex')),
 created_at timestamptz(6) NOT NULL
);
CREATE TABLE agenteam_meeting.launch_intents (
 id agenteam_meeting.safe_id PRIMARY KEY, meeting_id agenteam_meeting.safe_id NOT NULL,
 contribution_id agenteam_meeting.safe_id NOT NULL, generation bigint NOT NULL CHECK(generation>0), attempt bigint NOT NULL CHECK(attempt>0),
 agent_id agenteam_meeting.safe_id NOT NULL, input_id agenteam_meeting.safe_id NOT NULL REFERENCES agenteam_meeting.inputs(id),
 request_id agenteam_meeting.safe_id NOT NULL, idempotency_key text NOT NULL UNIQUE,
 request_data bytea NOT NULL, request_digest text NOT NULL CHECK(request_digest ~ '^sha256:[0-9a-f]{64}$'),
 execution_id agenteam_meeting.safe_id UNIQUE, launch_unknown boolean NOT NULL DEFAULT false,
 created_at timestamptz(6) NOT NULL, UNIQUE(contribution_id,generation,attempt),
 FOREIGN KEY(meeting_id,contribution_id) REFERENCES agenteam_meeting.contributions(meeting_id,id)
);
CREATE TABLE agenteam_meeting.contribution_executions (
 execution_id agenteam_meeting.safe_id PRIMARY KEY, meeting_id agenteam_meeting.safe_id NOT NULL,
 contribution_id agenteam_meeting.safe_id NOT NULL, generation bigint NOT NULL CHECK(generation>0), attempt bigint NOT NULL CHECK(attempt>0),
 status text NOT NULL CHECK(status IN('created','preparing','running','waiting','succeeded','failed','cancelled')),
 message_id agenteam_meeting.safe_id REFERENCES agenteam_meeting.messages(id), created_at timestamptz(6) NOT NULL,
 UNIQUE(contribution_id,generation,attempt), FOREIGN KEY(meeting_id,contribution_id) REFERENCES agenteam_meeting.contributions(meeting_id,id)
);
CREATE TABLE agenteam_meeting.decision_requests (
 id agenteam_meeting.safe_id PRIMARY KEY, meeting_id agenteam_meeting.safe_id NOT NULL,
 contribution_id agenteam_meeting.safe_id NOT NULL, execution_id agenteam_meeting.safe_id NOT NULL,
 operation_id agenteam_meeting.safe_id NOT NULL UNIQUE, round_id agenteam_meeting.safe_id NOT NULL,
 question text NOT NULL, options jsonb NOT NULL CHECK(jsonb_typeof(options)='array'),
 status text NOT NULL CHECK(status IN('pending','answered','skipped','cancelled')), version bigint NOT NULL CHECK(version>0),
 answer text, created_at timestamptz(6) NOT NULL, resolved_at timestamptz(6), resolved_by agenteam_meeting.safe_id,
 FOREIGN KEY(meeting_id,contribution_id) REFERENCES agenteam_meeting.contributions(meeting_id,id),
 CHECK((status='pending' AND resolved_at IS NULL AND answer IS NULL) OR (status<>'pending' AND resolved_at IS NOT NULL)),
 CHECK((status='answered' AND answer IS NOT NULL) OR (status<>'answered' AND answer IS NULL))
);
CREATE TABLE agenteam_meeting.approval_references (
 approval_id agenteam_meeting.safe_id PRIMARY KEY, meeting_id agenteam_meeting.safe_id NOT NULL,
 contribution_id agenteam_meeting.safe_id NOT NULL, execution_id agenteam_meeting.safe_id NOT NULL,
 FOREIGN KEY(meeting_id,contribution_id) REFERENCES agenteam_meeting.contributions(meeting_id,id)
);
CREATE TABLE agenteam_meeting.summary_generations (
 id agenteam_meeting.safe_id PRIMARY KEY, meeting_id agenteam_meeting.safe_id NOT NULL, turn_id agenteam_meeting.safe_id NOT NULL,
 expected_summary_version bigint NOT NULL CHECK(expected_summary_version>=0), input_data bytea NOT NULL,
 input_digest text NOT NULL CHECK(input_digest='sha256:'||encode(sha256(input_data),'hex')),
 initial boolean NOT NULL, attempt integer NOT NULL DEFAULT 0 CHECK(attempt BETWEEN 0 AND 3),
 status text NOT NULL CHECK(status IN('prepared','generating','failed','published','cancelled')),
 model_resolution_id agenteam_meeting.safe_id, candidate jsonb, error_code text NOT NULL DEFAULT '',
 created_at timestamptz(6) NOT NULL, FOREIGN KEY(meeting_id,turn_id) REFERENCES agenteam_meeting.turns(meeting_id,id),
 UNIQUE(turn_id,input_digest,expected_summary_version)
);
CREATE TABLE agenteam_meeting.timeline_items (
 contribution_id agenteam_meeting.safe_id PRIMARY KEY REFERENCES agenteam_meeting.contributions(id),
 meeting_id agenteam_meeting.safe_id NOT NULL REFERENCES agenteam_meeting.sessions(id),
 occurred_at timestamptz(6) NOT NULL, version bigint NOT NULL CHECK(version>0)
);
CREATE INDEX meeting_timeline_order ON agenteam_meeting.timeline_items(meeting_id,occurred_at,contribution_id);
CREATE TABLE agenteam_meeting.commands (
 project_id agenteam_meeting.safe_id NOT NULL, idempotency_key text NOT NULL,
 actor_id agenteam_meeting.safe_id NOT NULL, command text NOT NULL, semantic_digest text NOT NULL CHECK(semantic_digest ~ '^sha256:[0-9a-f]{64}$'),
 meeting_id agenteam_meeting.safe_id NOT NULL, receipt bytea NOT NULL, committed_at timestamptz(6) NOT NULL,
 PRIMARY KEY(project_id,idempotency_key)
);
-- Immutable message/input bodies can be deleted only by the owning Meeting's
-- explicit hard-delete flow; ordinary mutation never overwrites their content.
-- +goose StatementBegin
CREATE FUNCTION agenteam_meeting.immutable_body() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='immutable meeting fact'; END;
$$;
-- +goose StatementEnd
CREATE TRIGGER meeting_message_immutable BEFORE UPDATE ON agenteam_meeting.messages FOR EACH ROW EXECUTE FUNCTION agenteam_meeting.immutable_body();
CREATE TRIGGER meeting_input_immutable BEFORE UPDATE ON agenteam_meeting.inputs FOR EACH ROW EXECUTE FUNCTION agenteam_meeting.immutable_body();
CREATE TRIGGER meeting_command_immutable BEFORE UPDATE OR DELETE ON agenteam_meeting.commands FOR EACH ROW EXECUTE FUNCTION agenteam_meeting.immutable_body();
