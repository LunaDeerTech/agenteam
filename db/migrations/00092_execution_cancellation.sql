-- agenteam:transaction tx
-- +goose Up
-- The original cancellation receipt is immutable and is written atomically
-- with Work protection and the Execution cancellation request.
CREATE TABLE agenteam_execution.cancel_commands (
 project_id agenteam_execution.safe_id NOT NULL,
 task_id agenteam_execution.safe_id NOT NULL,
 execution_id agenteam_execution.safe_id NOT NULL REFERENCES agenteam_execution.executions(id),
 idempotency_key text COLLATE "C" NOT NULL CHECK(octet_length(idempotency_key) BETWEEN 1 AND 128 AND idempotency_key ~ '^[A-Za-z0-9._:/-]+$'),
 user_id agenteam_execution.safe_id NOT NULL,
 request_digest text NOT NULL CHECK(request_digest ~ '^sha256:[0-9a-f]{64}$'),
 receipt bytea NOT NULL CHECK(octet_length(receipt) BETWEEN 1 AND 16384),
 receipt_digest text NOT NULL CHECK(receipt_digest='sha256:'||encode(sha256(receipt),'hex')),
 created_at timestamptz(6) NOT NULL,
 PRIMARY KEY(project_id,task_id,execution_id,idempotency_key)
);
-- +goose StatementBegin
CREATE FUNCTION agenteam_execution.guard_cancel_command() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' AND agenteam_execution.project_retirement_allowed(OLD.execution_id::text) THEN RETURN OLD; END IF;
 RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='immutable execution cancellation receipt';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER execution_cancel_command_immutable BEFORE UPDATE OR DELETE ON agenteam_execution.cancel_commands
 FOR EACH ROW EXECUTE FUNCTION agenteam_execution.guard_cancel_command();
