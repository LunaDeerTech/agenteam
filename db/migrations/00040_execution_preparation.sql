-- agenteam:transaction tx
-- +goose Up
-- These are preparation-call ownership facts, not a Snapshot or proof that
-- all preparation dependencies were captured. Terminal means this attempt's
-- original call/transaction tail returned; Execution may still be preparing.
CREATE TABLE agenteam_execution.preparation_attempts (
 execution_id agenteam_execution.safe_id NOT NULL,
 project_id agenteam_execution.safe_id NOT NULL,
 agent_id agenteam_execution.safe_id NOT NULL,
 attempt_id agenteam_execution.safe_id NOT NULL,
 process_id agenteam_execution.safe_id NOT NULL,
 fence bigint NOT NULL CHECK(fence>=1),
 phase text NOT NULL CHECK(phase IN ('running','terminal')),
 started_at timestamptz(6) NOT NULL,
 returned_at timestamptz(6),
 PRIMARY KEY(execution_id,attempt_id),
 UNIQUE(execution_id,fence),
 UNIQUE(execution_id,attempt_id,process_id,fence),
 FOREIGN KEY(execution_id,project_id,agent_id)
  REFERENCES agenteam_execution.executions(id,project_id,agent_id) ON DELETE RESTRICT,
 CHECK((phase='terminal')=(returned_at IS NOT NULL)),
 CHECK(returned_at IS NULL OR returned_at>=started_at)
);

CREATE TABLE agenteam_execution.preparation_claims (
 execution_id agenteam_execution.safe_id PRIMARY KEY,
 project_id agenteam_execution.safe_id NOT NULL,
 agent_id agenteam_execution.safe_id NOT NULL,
 attempt_id agenteam_execution.safe_id NOT NULL,
 process_id agenteam_execution.safe_id NOT NULL,
 fence bigint NOT NULL CHECK(fence>=1),
 FOREIGN KEY(execution_id,project_id,agent_id)
  REFERENCES agenteam_execution.executions(id,project_id,agent_id) ON DELETE RESTRICT,
 FOREIGN KEY(execution_id,attempt_id,process_id,fence)
  REFERENCES agenteam_execution.preparation_attempts(execution_id,attempt_id,process_id,fence) ON DELETE RESTRICT
);
CREATE INDEX preparation_attempts_project ON agenteam_execution.preparation_attempts(project_id,execution_id,attempt_id);
CREATE INDEX preparation_claims_project ON agenteam_execution.preparation_claims(project_id,execution_id);

-- +goose StatementBegin
CREATE FUNCTION agenteam_execution.guard_preparation_attempt() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ROW(NEW.execution_id,NEW.project_id,NEW.agent_id,NEW.attempt_id,NEW.process_id,NEW.fence,NEW.started_at)
 IS DISTINCT FROM ROW(OLD.execution_id,OLD.project_id,OLD.agent_id,OLD.attempt_id,OLD.process_id,OLD.fence,OLD.started_at)
 OR OLD.phase<>'running' OR NEW.phase<>'terminal' OR NEW.returned_at IS NULL THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='preparation_attempt_immutable', MESSAGE='immutable preparation attempt';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER preparation_attempt_immutable BEFORE UPDATE ON agenteam_execution.preparation_attempts
 FOR EACH ROW EXECUTE FUNCTION agenteam_execution.guard_preparation_attempt();

-- +goose StatementBegin
CREATE FUNCTION agenteam_execution.guard_preparation_claim() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ROW(NEW.execution_id,NEW.project_id,NEW.agent_id) IS DISTINCT FROM ROW(OLD.execution_id,OLD.project_id,OLD.agent_id)
 OR OLD.fence=9223372036854775807 OR NEW.fence<>OLD.fence+1 OR NEW.attempt_id=OLD.attempt_id THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='preparation_claim_fence', MESSAGE='invalid preparation claim replacement';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER preparation_claim_fence BEFORE UPDATE ON agenteam_execution.preparation_claims
 FOR EACH ROW EXECUTE FUNCTION agenteam_execution.guard_preparation_claim();
