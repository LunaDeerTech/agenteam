-- agenteam:transaction tx
-- +goose Up
-- Live use is distinct from immutable preparation material and references.
-- Neither cancellation nor a configuration change deletes either record.
CREATE TABLE agenteam_mount.execution_uses (
 execution_id agenteam_execution.safe_id PRIMARY KEY
  REFERENCES agenteam_mount.execution_captures(execution_id) ON DELETE RESTRICT,
 project_id agenteam_execution.safe_id NOT NULL,
 agent_id agenteam_execution.safe_id NOT NULL,
 agent_version bigint NOT NULL CHECK(agent_version>0),
 attempt_binding text NOT NULL CHECK(attempt_binding ~ '^sha256:[0-9a-f]{64}$'),
 capture_digest text NOT NULL CHECK(capture_digest ~ '^sha256:[0-9a-f]{64}$'),
 input_digest text NOT NULL CHECK(input_digest ~ '^sha256:[0-9a-f]{64}$'),
 snapshot_digest text NOT NULL CHECK(snapshot_digest ~ '^sha256:[0-9a-f]{64}$'),
 FOREIGN KEY(execution_id,project_id,agent_id)
  REFERENCES agenteam_execution.executions(id,project_id,agent_id) ON DELETE RESTRICT,
 FOREIGN KEY(execution_id) REFERENCES agenteam_execution.preparation_inputs(execution_id) ON DELETE RESTRICT,
 FOREIGN KEY(execution_id) REFERENCES agenteam_execution.snapshots(execution_id) ON DELETE RESTRICT
);
CREATE INDEX mount_execution_use_project ON agenteam_mount.execution_uses(project_id,execution_id);

CREATE TABLE agenteam_mount.execution_use_retirements (
 execution_id agenteam_execution.safe_id PRIMARY KEY
  REFERENCES agenteam_mount.execution_uses(execution_id) ON DELETE RESTRICT,
 terminal_version bigint NOT NULL CHECK(terminal_version>0),
 terminal_event_id uuid NOT NULL UNIQUE,
 -- The original Execution writer appends this event later in the same Tx.
 FOREIGN KEY(terminal_event_id) REFERENCES agenteam_outbox.events(id) DEFERRABLE INITIALLY DEFERRED
);

-- +goose StatementBegin
CREATE FUNCTION agenteam_mount.immutable_execution_use() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='mount_execution_use_immutable', MESSAGE='immutable mount execution use';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER mount_execution_use_immutable BEFORE UPDATE OR DELETE
 ON agenteam_mount.execution_uses FOR EACH ROW
 EXECUTE FUNCTION agenteam_mount.immutable_execution_use();
CREATE TRIGGER mount_execution_use_retirement_immutable BEFORE UPDATE OR DELETE
 ON agenteam_mount.execution_use_retirements FOR EACH ROW
 EXECUTE FUNCTION agenteam_mount.immutable_execution_use();

-- Bind one immutable capture to the actual sealed input and Snapshot. These
-- constraints do not replace the same-Tx Execution owner's running witness.
-- +goose StatementBegin
CREATE FUNCTION agenteam_mount.check_execution_use() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(
  SELECT 1 FROM agenteam_mount.execution_captures c
   JOIN agenteam_execution.preparation_inputs i ON i.execution_id=c.execution_id
   JOIN agenteam_execution.snapshots s ON s.execution_id=c.execution_id
  WHERE c.execution_id=NEW.execution_id
   AND c.project_id=NEW.project_id AND c.agent_id=NEW.agent_id
   AND c.agent_version=NEW.agent_version AND c.attempt_binding=NEW.attempt_binding
   AND c.capture_digest=NEW.capture_digest
   AND i.project_id=NEW.project_id AND i.agent_id=NEW.agent_id
   AND i.attempt_binding=NEW.attempt_binding AND i.input_digest=NEW.input_digest
   AND s.project_id=NEW.project_id AND s.agent_id=NEW.agent_id
   AND s.input_digest=NEW.input_digest AND s.digest=NEW.snapshot_digest
 ) THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='mount_execution_use_binding', MESSAGE='mount execution use binding mismatch';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER mount_execution_use_binding AFTER INSERT
 ON agenteam_mount.execution_uses DEFERRABLE INITIALLY DEFERRED
 FOR EACH ROW EXECUTE FUNCTION agenteam_mount.check_execution_use();

-- A terminal row/event is necessary but is not proof that callbacks joined.
-- Actual join is checked by the Execution owner's private retirement witness.
-- +goose StatementBegin
CREATE FUNCTION agenteam_mount.check_execution_use_retirement() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(
  SELECT 1 FROM agenteam_mount.execution_uses u
   JOIN agenteam_execution.snapshots s ON s.execution_id=u.execution_id
   JOIN agenteam_execution.rounds r ON r.execution_id=u.execution_id AND r.snapshot_id=s.id
   JOIN agenteam_execution.executions e ON e.id=u.execution_id
  WHERE u.execution_id=NEW.execution_id AND s.digest=u.snapshot_digest
   AND r.terminal_version=NEW.terminal_version AND r.terminal_event_id=NEW.terminal_event_id
   AND e.version=NEW.terminal_version AND e.status=r.terminal_status
   AND e.status IN ('succeeded','failed','cancelled')
 ) THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='mount_execution_use_terminal', MESSAGE='mount execution retirement mismatch';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER mount_execution_use_terminal AFTER INSERT
 ON agenteam_mount.execution_use_retirements DEFERRABLE INITIALLY DEFERRED
 FOR EACH ROW EXECUTE FUNCTION agenteam_mount.check_execution_use_retirement();
