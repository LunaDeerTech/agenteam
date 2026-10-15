-- agenteam:transaction tx
-- +goose Up
-- Provenance is distinct from mutable pinned references. No MeetingProposal
-- object is introduced: sessions.status remains the only lifecycle authority.
CREATE TABLE agenteam_meeting.proposal_sources (
 meeting_id agenteam_meeting.safe_id PRIMARY KEY REFERENCES agenteam_meeting.sessions(id),
 execution_id agenteam_meeting.safe_id NOT NULL,
 created_by_participant_id agenteam_meeting.safe_id NOT NULL,
 origin_kind text CHECK(origin_kind IN('task','meeting')), origin_id agenteam_meeting.safe_id,
 CHECK((origin_kind IS NULL)=(origin_id IS NULL)),
 FOREIGN KEY(meeting_id,created_by_participant_id) REFERENCES agenteam_meeting.participants(meeting_id,id)
);
CREATE TRIGGER meeting_proposal_source_immutable BEFORE UPDATE ON agenteam_meeting.proposal_sources FOR EACH ROW EXECUTE FUNCTION agenteam_meeting.immutable_body();
CREATE TABLE agenteam_meeting.agent_commands (
 operation_id agenteam_meeting.safe_id PRIMARY KEY, attempt_id agenteam_meeting.safe_id NOT NULL,
 request_id agenteam_meeting.safe_id NOT NULL, project_id agenteam_meeting.safe_id NOT NULL,
 agent_id agenteam_meeting.safe_id NOT NULL, execution_id agenteam_meeting.safe_id NOT NULL,
 tool_call_id text NOT NULL, handler text NOT NULL CHECK(handler IN('meeting.list','meeting.request','meeting.add-agent','meeting.remove-agent','meeting.update','meeting.comment')),
 input_digest text NOT NULL CHECK(input_digest ~ '^sha256:[0-9a-f]{64}$'),
 meeting_id agenteam_meeting.safe_id, receipt bytea NOT NULL CHECK(octet_length(receipt) BETWEEN 2 AND 1048576),
 committed_at timestamptz(6) NOT NULL
);
CREATE TRIGGER meeting_agent_command_immutable BEFORE UPDATE OR DELETE ON agenteam_meeting.agent_commands FOR EACH ROW EXECUTE FUNCTION agenteam_meeting.immutable_body();
CREATE TABLE agenteam_meeting.lifecycle_events (
 event_id agenteam_meeting.safe_id PRIMARY KEY, project_id agenteam_meeting.safe_id NOT NULL,
 meeting_id agenteam_meeting.safe_id NOT NULL, event_type text NOT NULL,
 version bigint NOT NULL CHECK(version>0), status text NOT NULL CHECK(status IN('proposed','active','archive','deleted')),
 occurred_at timestamptz(6) NOT NULL, UNIQUE(meeting_id,version,event_type)
);
CREATE TRIGGER meeting_lifecycle_event_immutable BEFORE UPDATE OR DELETE ON agenteam_meeting.lifecycle_events FOR EACH ROW EXECUTE FUNCTION agenteam_meeting.immutable_body();
CREATE SCHEMA agenteam_humaninbox;
CREATE TABLE agenteam_humaninbox.items (
 id agenteam_meeting.safe_id PRIMARY KEY, project_id agenteam_meeting.safe_id NOT NULL,
 source_type text NOT NULL CHECK(source_type='meeting'), source_id agenteam_meeting.safe_id NOT NULL,
 action_type text NOT NULL CHECK(action_type='meeting.review_proposal'),
 status text NOT NULL CHECK(status IN('open','resolved','dismissed')),
 blocking boolean NOT NULL DEFAULT false, priority integer NOT NULL DEFAULT 0,
 dismissible boolean NOT NULL DEFAULT false CHECK(NOT dismissible),
 title_snapshot text NOT NULL, summary_snapshot text NOT NULL,
 source_version bigint NOT NULL CHECK(source_version>0), source_occurred_at timestamptz(6) NOT NULL,
 created_at timestamptz(6) NOT NULL, updated_at timestamptz(6) NOT NULL, resolved_at timestamptz(6),
 UNIQUE(source_type,source_id,action_type),
 CHECK((status='resolved')=(resolved_at IS NOT NULL))
);
CREATE INDEX humaninbox_project_order ON agenteam_humaninbox.items(project_id,status,blocking DESC,priority DESC,updated_at DESC,id DESC);
