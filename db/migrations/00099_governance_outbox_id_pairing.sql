-- agenteam:transaction tx
-- +goose Up
-- Outbox safe_id is UUID; Governance safe_id is canonical UUIDv7 text.
-- Pair their underlying types explicitly while retaining the original event,
-- aggregate, project, version, grant and zero-attempt denial checks. Casting
-- the Governance value preserves the Outbox indexed UUID columns.
-- Existing deferred trigger bindings keep this function's identity.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_governance.check_publication() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE r record;
BEGIN
 SELECT * INTO STRICT r FROM agenteam_governance.approval_requests WHERE id=NEW.id;
 IF NOT EXISTS(SELECT 1 FROM agenteam_outbox.events e WHERE e.id=r.requested_event_id::uuid AND e.producer='governance' AND e.event_type='governance.approval_requested' AND e.aggregate_id=r.id::uuid AND e.aggregate_version=1 AND e.project_id=r.project_id::uuid) OR
 (r.status<>'pending' AND NOT EXISTS(SELECT 1 FROM agenteam_outbox.events e WHERE e.id=r.resolved_event_id::uuid AND e.producer='governance' AND e.event_type='governance.approval_resolved' AND e.aggregate_id=r.id::uuid AND e.aggregate_version=2 AND e.project_id=r.project_id::uuid)) THEN
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
