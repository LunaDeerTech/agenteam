-- agenteam:transaction tx
-- +goose Up
-- Fixed revisions are Skill-owned protection in the caller's complete capture
-- transaction. No cross-domain SQL/FK or public identity is an Execution grant.
ALTER TABLE agenteam_skill.revisions ADD CONSTRAINT skill_revision_capture_identity
 UNIQUE(project_id,skill_id,id,revision,object_id);

ALTER TABLE agenteam_skill.agent_assignments ADD CONSTRAINT skill_assignment_capture_identity
 UNIQUE(project_id,agent_id,id,skill_id,assignment_sequence);

CREATE TABLE agenteam_skill.execution_binding_heads (
 execution_id agenteam_skill.safe_id PRIMARY KEY,
 project_id agenteam_skill.safe_id NOT NULL,
 agent_id agenteam_skill.safe_id NOT NULL,
 assignment_sequence bigint NOT NULL CHECK(assignment_sequence>0),
 attempt_binding text NOT NULL CHECK(attempt_binding ~ '^sha256:[0-9a-f]{64}$'),
 source_digest text NOT NULL CHECK(source_digest ~ '^sha256:[0-9a-f]{64}$'),
 binding_count bigint NOT NULL CHECK(binding_count IN (0,1)),
 record jsonb NOT NULL CHECK(jsonb_typeof(record)='object' AND octet_length(record::text)<=262144),
 captured_at timestamptz(6) NOT NULL,
 UNIQUE(execution_id,project_id,agent_id),
 FOREIGN KEY(project_id,agent_id) REFERENCES agenteam_skill.agent_assignment_heads(project_id,agent_id) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX skill_execution_capture_project ON agenteam_skill.execution_binding_heads(project_id,execution_id);

CREATE TABLE agenteam_skill.execution_bindings (
 execution_id agenteam_skill.safe_id NOT NULL,
 project_id agenteam_skill.safe_id NOT NULL,
 agent_id agenteam_skill.safe_id NOT NULL,
 skill_id agenteam_skill.safe_id NOT NULL,
 revision_id agenteam_skill.safe_id NOT NULL,
 revision bigint NOT NULL CHECK(revision>0),
 assignment_id agenteam_skill.safe_id NOT NULL,
 assignment_sequence bigint NOT NULL CHECK(assignment_sequence>0),
 object_id agenteam_skill.safe_id NOT NULL,
 PRIMARY KEY(execution_id,skill_id),
 UNIQUE(execution_id,assignment_id),
 FOREIGN KEY(execution_id,project_id,agent_id) REFERENCES agenteam_skill.execution_binding_heads(execution_id,project_id,agent_id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(project_id,agent_id,assignment_id,skill_id,assignment_sequence) REFERENCES agenteam_skill.agent_assignments(project_id,agent_id,id,skill_id,assignment_sequence) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(project_id,skill_id,revision_id,revision,object_id) REFERENCES agenteam_skill.revisions(project_id,skill_id,id,revision,object_id) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX skill_execution_revision_protection ON agenteam_skill.execution_bindings(project_id,skill_id,revision_id);

-- +goose StatementBegin
CREATE FUNCTION agenteam_skill.immutable_execution_binding() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='immutable Skill execution binding';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER skill_execution_head_immutable BEFORE UPDATE ON agenteam_skill.execution_binding_heads
 FOR EACH ROW EXECUTE FUNCTION agenteam_skill.immutable_execution_binding();
CREATE TRIGGER skill_execution_binding_immutable BEFORE UPDATE ON agenteam_skill.execution_bindings
 FOR EACH ROW EXECUTE FUNCTION agenteam_skill.immutable_execution_binding();

-- +goose StatementBegin
CREATE FUNCTION agenteam_skill.check_execution_binding_set() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE key_id agenteam_skill.safe_id; expected bigint; actual bigint; seq bigint;
BEGIN
 IF TG_OP='DELETE' THEN key_id:=OLD.execution_id; ELSE key_id:=NEW.execution_id; END IF;
 SELECT binding_count,assignment_sequence INTO expected,seq FROM agenteam_skill.execution_binding_heads WHERE execution_id=key_id;
 SELECT count(*) INTO actual FROM agenteam_skill.execution_bindings WHERE execution_id=key_id;
 IF expected IS NULL THEN
  IF actual<>0 THEN RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='missing Skill execution binding head'; END IF;
 ELSE
  IF actual<>expected OR EXISTS(SELECT 1 FROM agenteam_skill.execution_bindings WHERE execution_id=key_id AND assignment_sequence>seq) THEN
   RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='incomplete Skill execution binding set';
  END IF;
 END IF;
 RETURN NULL;
END;
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER skill_execution_head_complete AFTER INSERT OR DELETE ON agenteam_skill.execution_binding_heads
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION agenteam_skill.check_execution_binding_set();
CREATE CONSTRAINT TRIGGER skill_execution_set_complete AFTER INSERT OR DELETE ON agenteam_skill.execution_bindings
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION agenteam_skill.check_execution_binding_set();
