-- agenteam:transaction tx
-- +goose Up
-- The first supported run seals its immutable Context, first RoundInput and
-- initial Transcript with running and Started. No backend election/lease is
-- introduced: process_id identifies the actual in-process owner of this run.
CREATE TABLE agenteam_execution.snapshots (
 id agenteam_execution.safe_id PRIMARY KEY,
 execution_id agenteam_execution.safe_id NOT NULL UNIQUE,
 project_id agenteam_execution.safe_id NOT NULL,
 agent_id agenteam_execution.safe_id NOT NULL,
 start_id agenteam_execution.safe_id NOT NULL UNIQUE,
 process_id agenteam_execution.safe_id NOT NULL,
 input_digest text NOT NULL CHECK(input_digest ~ '^sha256:[0-9a-f]{64}$'),
 context_digest text NOT NULL CHECK(context_digest ~ '^sha256:[0-9a-f]{64}$'),
 schema_version bigint NOT NULL CHECK(schema_version=1),
 content bytea NOT NULL CHECK(octet_length(content) BETWEEN 2 AND 16781312),
 digest text NOT NULL CHECK(digest='sha256:'||encode(sha256(content),'hex')),
 started_version bigint NOT NULL CHECK(started_version>=2),
 started_event_id uuid NOT NULL UNIQUE,
 created_at timestamptz(6) NOT NULL,
 UNIQUE(execution_id,id),
 UNIQUE(execution_id,id,start_id),
 FOREIGN KEY(execution_id,project_id,agent_id) REFERENCES agenteam_execution.executions(id,project_id,agent_id) ON DELETE RESTRICT,
 FOREIGN KEY(execution_id) REFERENCES agenteam_execution.preparation_inputs(execution_id) ON DELETE RESTRICT,
 FOREIGN KEY(started_event_id) REFERENCES agenteam_outbox.events(id) DEFERRABLE INITIALLY DEFERRED,
 CHECK(jsonb_typeof(convert_from(content,'UTF8')::jsonb)='object')
);
ALTER TABLE agenteam_execution.executions ADD CONSTRAINT execution_snapshot_identity
 FOREIGN KEY(id,snapshot_id) REFERENCES agenteam_execution.snapshots(execution_id,id) DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE agenteam_execution.rounds (
 id agenteam_execution.safe_id PRIMARY KEY,
 execution_id agenteam_execution.safe_id NOT NULL UNIQUE,
 snapshot_id agenteam_execution.safe_id NOT NULL,
 start_id agenteam_execution.safe_id NOT NULL,
 input_binding_id agenteam_execution.safe_id NOT NULL UNIQUE,
 call_id agenteam_execution.safe_id NOT NULL UNIQUE,
 input_digest text NOT NULL CHECK(input_digest ~ '^sha256:[0-9a-f]{64}$'),
 context_digest text NOT NULL CHECK(context_digest ~ '^sha256:[0-9a-f]{64}$'),
 schema_version bigint NOT NULL CHECK(schema_version=1),
 content bytea NOT NULL CHECK(octet_length(content) BETWEEN 2 AND 16777216),
 digest text NOT NULL CHECK(digest='sha256:'||encode(sha256(content),'hex')),
 created_at timestamptz(6) NOT NULL,
 transcript_through bigint NOT NULL CHECK(transcript_through IN (1,2)),
 terminal_status text CHECK(terminal_status IN ('succeeded','failed','cancelled')),
 terminal_version bigint CHECK(terminal_version>=3),
 terminal_reason text,
 terminal_event_id uuid UNIQUE,
 response bytea CHECK(octet_length(response) BETWEEN 2 AND 16777216),
 response_digest text,
 finished_at timestamptz(6),
 UNIQUE(execution_id,id),
 FOREIGN KEY(execution_id,snapshot_id,start_id) REFERENCES agenteam_execution.snapshots(execution_id,id,start_id) ON DELETE RESTRICT,
 FOREIGN KEY(terminal_event_id) REFERENCES agenteam_outbox.events(id) DEFERRABLE INITIALLY DEFERRED,
 CHECK(jsonb_typeof(convert_from(content,'UTF8')::jsonb)='object'),
 CONSTRAINT execution_round_phase CHECK((
  (terminal_status IS NULL AND terminal_version IS NULL AND terminal_reason IS NULL AND terminal_event_id IS NULL AND response IS NULL AND response_digest IS NULL AND finished_at IS NULL AND transcript_through=1)
  OR (terminal_status IS NOT NULL AND terminal_version IS NOT NULL AND terminal_event_id IS NOT NULL AND finished_at>=created_at
    AND ((terminal_status='succeeded' AND terminal_reason='completed' AND transcript_through=2 AND response IS NOT NULL AND response_digest='sha256:'||encode(sha256(response),'hex'))
      OR (terminal_status='failed' AND terminal_reason IN ('model_failed','incomplete_response','runtime_failed') AND transcript_through=1 AND response IS NULL AND response_digest IS NULL)
      OR (terminal_status='cancelled' AND terminal_reason='cancelled' AND transcript_through=1 AND response IS NULL AND response_digest IS NULL)))
 ) IS TRUE)
);

CREATE TABLE agenteam_execution.transcript_entries (
 execution_id agenteam_execution.safe_id NOT NULL,
 sequence bigint NOT NULL CHECK(sequence IN (1,2)),
 round_id agenteam_execution.safe_id NOT NULL,
 kind text NOT NULL CHECK(kind IN ('input','assistant')),
 message bytea NOT NULL CHECK(octet_length(message) BETWEEN 2 AND 16777216),
 digest text NOT NULL CHECK(digest='sha256:'||encode(sha256(message),'hex')),
 created_at timestamptz(6) NOT NULL,
 PRIMARY KEY(execution_id,sequence),
 FOREIGN KEY(execution_id,round_id) REFERENCES agenteam_execution.rounds(execution_id,id) ON DELETE RESTRICT,
 CHECK((sequence=1 AND kind='input') OR (sequence=2 AND kind='assistant')),
 CHECK((jsonb_typeof(convert_from(message,'UTF8')::jsonb)='object'
  AND convert_from(message,'UTF8')::jsonb->>'role'=CASE kind WHEN 'input' THEN 'user' ELSE 'assistant' END) IS TRUE)
);

-- +goose StatementBegin
CREATE FUNCTION agenteam_execution.guard_direct_text_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='execution_direct_text_immutable', MESSAGE='immutable execution input';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER execution_snapshot_immutable BEFORE UPDATE OR DELETE ON agenteam_execution.snapshots FOR EACH ROW EXECUTE FUNCTION agenteam_execution.guard_direct_text_immutable();
CREATE TRIGGER execution_transcript_immutable BEFORE UPDATE OR DELETE ON agenteam_execution.transcript_entries FOR EACH ROW EXECUTE FUNCTION agenteam_execution.guard_direct_text_immutable();
-- +goose StatementBegin
CREATE FUNCTION agenteam_execution.guard_round_completion() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='execution_round_immutable', MESSAGE='immutable execution round';
 END IF;
 IF OLD.terminal_status IS NOT NULL OR NEW.terminal_status IS NULL OR
 ROW(NEW.id,NEW.execution_id,NEW.snapshot_id,NEW.start_id,NEW.input_binding_id,NEW.call_id,NEW.input_digest,NEW.context_digest,NEW.schema_version,NEW.content,NEW.digest,NEW.created_at)
 IS DISTINCT FROM ROW(OLD.id,OLD.execution_id,OLD.snapshot_id,OLD.start_id,OLD.input_binding_id,OLD.call_id,OLD.input_digest,OLD.context_digest,OLD.schema_version,OLD.content,OLD.digest,OLD.created_at) THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='execution_round_immutable', MESSAGE='immutable execution round';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER execution_round_immutable BEFORE UPDATE OR DELETE ON agenteam_execution.rounds FOR EACH ROW EXECUTE FUNCTION agenteam_execution.guard_round_completion();
