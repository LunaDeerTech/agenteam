-- agenteam:transaction tx
-- +goose Up
-- An accepted delete is not a completed purge. The original command receipt
-- and this minimal lifecycle receipt survive removal of Meeting-owned data.
CREATE TABLE agenteam_meeting.deletion_requests (
 id agenteam_meeting.safe_id PRIMARY KEY,
 project_id agenteam_meeting.safe_id NOT NULL,
 meeting_id agenteam_meeting.safe_id NOT NULL,
 actor_id agenteam_meeting.safe_id NOT NULL,
 request_id agenteam_meeting.safe_id NOT NULL,
 idempotency_key text NOT NULL,
 semantic_digest text NOT NULL CHECK(semantic_digest ~ '^sha256:[0-9a-f]{64}$'),
 expected_version bigint NOT NULL CHECK(expected_version>0),
 accepted_version bigint NOT NULL CHECK(accepted_version>expected_version AND accepted_version-expected_version=1),
 phase text NOT NULL CHECK(phase IN('cancelling','deleted')),
 error_code text NOT NULL DEFAULT '',
 created_at timestamptz(6) NOT NULL,
 deleted_at timestamptz(6),
 deleted_records bigint CHECK(deleted_records>0),
 UNIQUE(project_id,meeting_id),
 UNIQUE(project_id,idempotency_key),
 FOREIGN KEY(project_id,idempotency_key) REFERENCES agenteam_meeting.commands(project_id,idempotency_key),
 CONSTRAINT meeting_deletion_completion CHECK((phase='cancelling' AND deleted_at IS NULL AND deleted_records IS NULL) OR
       (phase='deleted' AND deleted_at IS NOT NULL AND deleted_at>=created_at AND deleted_records IS NOT NULL AND error_code=''))
);
CREATE INDEX meeting_pending_deletions ON agenteam_meeting.deletion_requests(id) WHERE phase='cancelling';
-- +goose StatementBegin
CREATE FUNCTION agenteam_meeting.guard_deletion_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN
  RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='immutable meeting deletion receipt';
 END IF;
 IF ROW(NEW.id,NEW.project_id,NEW.meeting_id,NEW.actor_id,NEW.request_id,NEW.idempotency_key,NEW.semantic_digest,NEW.expected_version,NEW.accepted_version,NEW.created_at)
 IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.meeting_id,OLD.actor_id,OLD.request_id,OLD.idempotency_key,OLD.semantic_digest,OLD.expected_version,OLD.accepted_version,OLD.created_at)
 OR (OLD.phase='deleted' AND NEW IS DISTINCT FROM OLD) THEN
  RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='immutable meeting deletion identity';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER meeting_deletion_identity BEFORE UPDATE OR DELETE ON agenteam_meeting.deletion_requests FOR EACH ROW EXECUTE FUNCTION agenteam_meeting.guard_deletion_identity();
