-- agenteam:transaction tx
-- +goose Up
CREATE SCHEMA agenteam_governance;
CREATE DOMAIN agenteam_governance.safe_id AS text CHECK(VALUE ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$');
CREATE TABLE agenteam_governance.approval_requests (
 id agenteam_governance.safe_id PRIMARY KEY,
 project_id agenteam_governance.safe_id NOT NULL,
 agent_id agenteam_governance.safe_id NOT NULL,
 execution_id agenteam_governance.safe_id NOT NULL,
 round_id agenteam_governance.safe_id NOT NULL,
 tool_id agenteam_governance.safe_id NOT NULL,
 tool_call_id text NOT NULL CHECK(octet_length(tool_call_id) BETWEEN 1 AND 256),
 operation_id agenteam_governance.safe_id NOT NULL UNIQUE,
 fingerprint text NOT NULL CHECK(fingerprint ~ '^sha256:[0-9a-f]{64}$'),
 binding_data bytea NOT NULL CHECK(octet_length(binding_data) BETWEEN 2 AND 32768),
 binding_digest text NOT NULL CHECK(binding_digest='sha256:'||encode(sha256(binding_data),'hex')),
 status text NOT NULL CHECK(status IN('pending','approved','rejected')),
 version bigint NOT NULL CHECK(version IN(1,2)),
 created_at timestamptz(6) NOT NULL,
 resolved_at timestamptz(6),
 resolved_by agenteam_governance.safe_id,
 requested_event_id agenteam_governance.safe_id NOT NULL UNIQUE,
 resolved_event_id agenteam_governance.safe_id UNIQUE,
 CHECK((status='pending' AND version=1 AND resolved_at IS NULL AND resolved_by IS NULL AND resolved_event_id IS NULL) OR
 (status IN('approved','rejected') AND version=2 AND resolved_at>=created_at AND resolved_by IS NOT NULL AND resolved_event_id IS NOT NULL)),
 UNIQUE(execution_id,round_id,tool_call_id)
);
CREATE INDEX approval_project_pending ON agenteam_governance.approval_requests(project_id,created_at,id) WHERE status='pending';
CREATE TABLE agenteam_governance.one_time_approvals (
 request_id agenteam_governance.safe_id PRIMARY KEY REFERENCES agenteam_governance.approval_requests(id),
 project_id agenteam_governance.safe_id NOT NULL,
 agent_id agenteam_governance.safe_id NOT NULL,
 operation_id agenteam_governance.safe_id NOT NULL UNIQUE,
 tool_id agenteam_governance.safe_id NOT NULL,
 fingerprint text NOT NULL CHECK(fingerprint ~ '^sha256:[0-9a-f]{64}$'),
 created_at timestamptz(6) NOT NULL
);
CREATE TABLE agenteam_governance.approval_commands (
 id agenteam_governance.safe_id PRIMARY KEY,
 project_id agenteam_governance.safe_id NOT NULL,
 user_id agenteam_governance.safe_id NOT NULL,
 idempotency_key text NOT NULL CHECK(idempotency_key ~ '^[A-Za-z0-9._:/-]{1,128}$'),
 semantic_digest text NOT NULL CHECK(semantic_digest ~ '^sha256:[0-9a-f]{64}$'),
 request_id agenteam_governance.safe_id NOT NULL REFERENCES agenteam_governance.approval_requests(id),
 decision text NOT NULL CHECK(decision IN('approve','reject')),
 expected_version bigint NOT NULL CHECK(expected_version=1),
 receipt bytea NOT NULL CHECK(octet_length(receipt) BETWEEN 2 AND 8192),
 committed_at timestamptz(6) NOT NULL,
 UNIQUE(project_id,idempotency_key)
);
-- A rejected call terminates its original operation without inventing an RPC
-- attempt. The immutable denial belongs to Tool, and remains distinct from
-- any backend error, cancellation or unknown side effect.
CREATE TABLE agenteam_tool.approval_denials (
 operation_id agenteam_tool.safe_id PRIMARY KEY,
 request_id agenteam_tool.safe_id NOT NULL UNIQUE,
 fingerprint text NOT NULL CHECK(fingerprint ~ '^sha256:[0-9a-f]{64}$'),
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp()
);
-- +goose StatementBegin
CREATE FUNCTION agenteam_governance.immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='immutable approval fact'; END;
$$;
-- +goose StatementEnd
CREATE TRIGGER approval_grant_immutable BEFORE UPDATE OR DELETE ON agenteam_governance.one_time_approvals FOR EACH ROW EXECUTE FUNCTION agenteam_governance.immutable();
CREATE TRIGGER approval_command_immutable BEFORE UPDATE OR DELETE ON agenteam_governance.approval_commands FOR EACH ROW EXECUTE FUNCTION agenteam_governance.immutable();
CREATE TRIGGER tool_approval_denial_immutable BEFORE UPDATE OR DELETE ON agenteam_tool.approval_denials FOR EACH ROW EXECUTE FUNCTION agenteam_governance.immutable();
-- +goose StatementBegin
CREATE FUNCTION agenteam_governance.guard_request() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='immutable approval request'; END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.status<>'pending' OR NOT EXISTS(SELECT 1 FROM agenteam_tool.operations o WHERE o.operation_id=NEW.operation_id AND o.project_id=NEW.project_id AND o.agent_id=NEW.agent_id AND o.execution_id=NEW.execution_id AND o.call_id=NEW.tool_call_id AND o.tool_id=NEW.tool_id AND o.fingerprint=NEW.fingerprint AND o.phase='created' AND o.active_attempt_id IS NULL) THEN
   RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='approval requires original unattempted operation';
  END IF;
 ELSE
  IF OLD.status<>'pending' OR NEW.status NOT IN('approved','rejected') OR ROW(NEW.id,NEW.project_id,NEW.agent_id,NEW.execution_id,NEW.round_id,NEW.tool_id,NEW.tool_call_id,NEW.operation_id,NEW.fingerprint,NEW.binding_data,NEW.binding_digest,NEW.created_at,NEW.requested_event_id) IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.agent_id,OLD.execution_id,OLD.round_id,OLD.tool_id,OLD.tool_call_id,OLD.operation_id,OLD.fingerprint,OLD.binding_data,OLD.binding_digest,OLD.created_at,OLD.requested_event_id) THEN
   RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='immutable approval decision';
  END IF;
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER approval_request_guard BEFORE INSERT OR UPDATE OR DELETE ON agenteam_governance.approval_requests FOR EACH ROW EXECUTE FUNCTION agenteam_governance.guard_request();
-- All request/decision publication requires the matching typed unified Outbox
-- fact in the original transaction; no private notification queue is created.
-- +goose StatementBegin
CREATE FUNCTION agenteam_governance.check_publication() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE r record;
BEGIN
 SELECT * INTO STRICT r FROM agenteam_governance.approval_requests WHERE id=NEW.id;
 IF NOT EXISTS(SELECT 1 FROM agenteam_outbox.events e WHERE e.id=r.requested_event_id AND e.producer='governance' AND e.event_type='governance.approval_requested' AND e.aggregate_id=r.id AND e.aggregate_version=1 AND e.project_id=r.project_id) OR
 (r.status<>'pending' AND NOT EXISTS(SELECT 1 FROM agenteam_outbox.events e WHERE e.id=r.resolved_event_id AND e.producer='governance' AND e.event_type='governance.approval_resolved' AND e.aggregate_id=r.id AND e.aggregate_version=2 AND e.project_id=r.project_id)) THEN
  RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='approval requires transactional publication';
 END IF;
 IF r.status='approved' AND NOT EXISTS(SELECT 1 FROM agenteam_governance.one_time_approvals g WHERE g.request_id=r.id AND g.operation_id=r.operation_id AND g.project_id=r.project_id AND g.agent_id=r.agent_id AND g.tool_id=r.tool_id AND g.fingerprint=r.fingerprint AND g.created_at=r.resolved_at) THEN
  RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='approval requires bound grant';
 END IF;
 IF r.status='rejected' AND NOT EXISTS(SELECT 1 FROM agenteam_tool.approval_denials d JOIN agenteam_tool.operations o ON o.operation_id=d.operation_id WHERE d.request_id=r.id AND d.operation_id=r.operation_id AND d.fingerprint=r.fingerprint AND o.phase='failed' AND o.active_attempt_id IS NULL) THEN
  RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='rejection requires zero-attempt terminal';
 END IF;
 RETURN NULL;
END;
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER approval_publication AFTER INSERT OR UPDATE ON agenteam_governance.approval_requests DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION agenteam_governance.check_publication();
-- +goose StatementBegin
CREATE FUNCTION agenteam_governance.check_resolution_fact() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE r record;
BEGIN
 SELECT * INTO STRICT r FROM agenteam_governance.approval_requests WHERE id=NEW.request_id;
 IF TG_TABLE_NAME='one_time_approvals' THEN
  IF r.status<>'approved' OR ROW(NEW.project_id,NEW.agent_id,NEW.operation_id,NEW.tool_id,NEW.fingerprint,NEW.created_at) IS DISTINCT FROM ROW(r.project_id,r.agent_id,r.operation_id,r.tool_id,r.fingerprint,r.resolved_at) THEN RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='invalid bound approval'; END IF;
 ELSIF TG_TABLE_NAME='approval_commands' THEN
  IF NEW.project_id<>r.project_id OR NEW.user_id<>r.resolved_by OR NEW.committed_at<>r.resolved_at OR (NEW.decision='approve')<>(r.status='approved') OR r.version<>2 THEN RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='invalid approval command receipt'; END IF;
 END IF;
 RETURN NULL;
END;
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER approval_grant_resolution AFTER INSERT ON agenteam_governance.one_time_approvals DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION agenteam_governance.check_resolution_fact();
CREATE CONSTRAINT TRIGGER approval_command_resolution AFTER INSERT ON agenteam_governance.approval_commands DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION agenteam_governance.check_resolution_fact();
-- +goose StatementBegin
CREATE FUNCTION agenteam_tool.check_approval_denial() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE d record; o record; r record;
BEGIN
 SELECT * INTO d FROM agenteam_tool.approval_denials WHERE operation_id=NEW.operation_id;
 IF NOT FOUND THEN RETURN NULL; END IF;
 SELECT * INTO STRICT o FROM agenteam_tool.operations WHERE operation_id=d.operation_id;
 SELECT * INTO STRICT r FROM agenteam_governance.approval_requests WHERE id=d.request_id;
 IF r.status<>'rejected' OR r.operation_id<>d.operation_id OR r.fingerprint<>d.fingerprint OR o.fingerprint<>d.fingerprint OR o.phase<>'failed' OR o.version<>2 OR o.active_attempt_id IS NOT NULL OR o.outcome<>'error'
 OR convert_from(o.terminal_data,'UTF8')::jsonb<>jsonb_build_object('kind','approval_rejected','request_id',d.request_id,'report_available',false)
 OR EXISTS(SELECT 1 FROM agenteam_tool.attempts a WHERE a.operation_id=d.operation_id) THEN
  RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='rejected operation cannot execute';
 END IF;
 RETURN NULL;
END;
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER tool_denial_terminal AFTER INSERT ON agenteam_tool.approval_denials DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION agenteam_tool.check_approval_denial();
CREATE CONSTRAINT TRIGGER tool_denial_not_revived AFTER INSERT OR UPDATE ON agenteam_tool.operations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION agenteam_tool.check_approval_denial();
CREATE CONSTRAINT TRIGGER tool_denial_no_attempt AFTER INSERT OR UPDATE ON agenteam_tool.attempts DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION agenteam_tool.check_approval_denial();
