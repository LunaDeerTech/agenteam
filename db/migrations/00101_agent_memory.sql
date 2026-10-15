-- agenteam:transaction tx
-- +goose Up
CREATE SCHEMA agenteam_memory;
CREATE DOMAIN agenteam_memory.safe_id AS uuid
 CHECK(VALUE::text ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$');

-- Namespace is independent of Execution. Generations change atomically while
-- canonical changes invalidate the old revision immediately.
CREATE TABLE agenteam_memory.namespaces (
 project_id agenteam_memory.safe_id NOT NULL,
 agent_id agenteam_memory.safe_id NOT NULL,
 version bigint NOT NULL CHECK(version>0),
 serving_generation_id agenteam_memory.safe_id,
 PRIMARY KEY(project_id,agent_id)
);
CREATE TABLE agenteam_memory.memories (
 memory_id agenteam_memory.safe_id PRIMARY KEY,
 project_id agenteam_memory.safe_id NOT NULL,
 agent_id agenteam_memory.safe_id NOT NULL,
 current_revision bigint NOT NULL CHECK(current_revision>0),
 status text NOT NULL CHECK(status IN ('active','deleted')),
 created_at timestamptz(6) NOT NULL,
 updated_at timestamptz(6) NOT NULL CHECK(updated_at>=created_at),
 deleted_at timestamptz(6),
 UNIQUE(project_id,agent_id,memory_id),
 FOREIGN KEY(project_id,agent_id) REFERENCES agenteam_memory.namespaces(project_id,agent_id),
 CHECK((status='deleted')=(deleted_at IS NOT NULL))
);
CREATE TABLE agenteam_memory.revisions (
 memory_id agenteam_memory.safe_id NOT NULL,
 revision bigint NOT NULL CHECK(revision>0),
 project_id agenteam_memory.safe_id NOT NULL,
 agent_id agenteam_memory.safe_id NOT NULL,
 status text NOT NULL CHECK(status IN ('active','superseded','deleted')),
 type text NOT NULL CHECK(type IN ('fact','experience','procedure','observation')),
 body text NOT NULL CHECK(octet_length(body) BETWEEN 1 AND 8192),
 tags text[] NOT NULL CHECK(cardinality(tags)<=16),
 importance text NOT NULL CHECK(importance IN ('low','normal','high')),
 provenance jsonb NOT NULL CHECK(jsonb_typeof(provenance)='object' AND octet_length(provenance::text)<=16384),
 created_by_operation_id agenteam_memory.safe_id NOT NULL,
 created_at timestamptz(6) NOT NULL,
 PRIMARY KEY(memory_id,revision),
 UNIQUE(project_id,agent_id,memory_id,revision),
 FOREIGN KEY(project_id,agent_id,memory_id) REFERENCES agenteam_memory.memories(project_id,agent_id,memory_id)
);
CREATE UNIQUE INDEX memory_one_active_revision ON agenteam_memory.revisions(memory_id) WHERE status='active';
ALTER TABLE agenteam_memory.memories ADD CONSTRAINT memory_current_revision
 FOREIGN KEY(project_id,agent_id,memory_id,current_revision)
 REFERENCES agenteam_memory.revisions(project_id,agent_id,memory_id,revision) DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX memory_namespace_current ON agenteam_memory.memories(project_id,agent_id,updated_at DESC,memory_id DESC);

CREATE TABLE agenteam_memory.operations (
 operation_id agenteam_memory.safe_id PRIMARY KEY,
 project_id agenteam_memory.safe_id NOT NULL,
 agent_id agenteam_memory.safe_id NOT NULL,
 execution_id agenteam_memory.safe_id NOT NULL,
 input_digest text NOT NULL CHECK(input_digest ~ '^sha256:[0-9a-f]{64}$'),
 gate_outcome text NOT NULL CHECK(gate_outcome IN ('allow','mask')),
 phase text NOT NULL CHECK(phase IN ('prepared','committed')),
 receipt jsonb,
 created_at timestamptz(6) NOT NULL,
 committed_at timestamptz(6),
 UNIQUE(project_id,agent_id,operation_id),
 CHECK(((phase='prepared' AND receipt IS NULL AND committed_at IS NULL)
  OR (phase='committed' AND jsonb_typeof(receipt)='object' AND committed_at IS NOT NULL)) IS TRUE)
);
CREATE TABLE agenteam_memory.proposals (
 operation_id agenteam_memory.safe_id NOT NULL REFERENCES agenteam_memory.operations(operation_id),
 proposal_number smallint NOT NULL CHECK(proposal_number BETWEEN 1 AND 2),
 input_digest text NOT NULL CHECK(input_digest ~ '^sha256:[0-9a-f]{64}$'),
 input_targets jsonb NOT NULL CHECK(jsonb_typeof(input_targets)='array'),
 proposal jsonb NOT NULL CHECK(jsonb_typeof(proposal)='object' AND octet_length(proposal::text)<=524288),
 model_call_id agenteam_memory.safe_id NOT NULL,
 created_at timestamptz(6) NOT NULL,
 PRIMARY KEY(operation_id,proposal_number)
);
CREATE TABLE agenteam_memory.commands (
 project_id agenteam_memory.safe_id NOT NULL,
 agent_id agenteam_memory.safe_id NOT NULL,
 command_key text NOT NULL CHECK(octet_length(command_key) BETWEEN 1 AND 128),
 input_digest text NOT NULL CHECK(input_digest ~ '^sha256:[0-9a-f]{64}$'),
 actor_user_id agenteam_memory.safe_id NOT NULL,
 memory_id agenteam_memory.safe_id NOT NULL,
 receipt jsonb NOT NULL CHECK(jsonb_typeof(receipt)='object'),
 PRIMARY KEY(project_id,agent_id,command_key)
);
CREATE TABLE agenteam_memory.index_intents (
 project_id agenteam_memory.safe_id NOT NULL,
 agent_id agenteam_memory.safe_id NOT NULL,
 memory_id agenteam_memory.safe_id NOT NULL,
 revision bigint NOT NULL CHECK(revision>0),
 action text NOT NULL CHECK(action IN ('upsert','delete')),
 created_at timestamptz(6) NOT NULL,
 PRIMARY KEY(memory_id,revision,action),
 FOREIGN KEY(project_id,agent_id,memory_id,revision)
 REFERENCES agenteam_memory.revisions(project_id,agent_id,memory_id,revision)
);
CREATE TABLE agenteam_memory.generations (
 generation_id agenteam_memory.safe_id PRIMARY KEY,
 project_id agenteam_memory.safe_id NOT NULL,
 agent_id agenteam_memory.safe_id NOT NULL,
 snapshot_id agenteam_memory.safe_id NOT NULL,
 dimension integer NOT NULL CHECK(dimension BETWEEN 1 AND 4096),
 schema_version bigint NOT NULL CHECK(schema_version>0),
 namespace_version bigint NOT NULL CHECK(namespace_version>0),
 status text NOT NULL CHECK(status IN ('building','ready','retired')),
 created_at timestamptz(6) NOT NULL,
 UNIQUE(project_id,agent_id,generation_id),
 FOREIGN KEY(project_id,agent_id) REFERENCES agenteam_memory.namespaces(project_id,agent_id)
);
ALTER TABLE agenteam_memory.namespaces ADD CONSTRAINT memory_serving_generation
 FOREIGN KEY(project_id,agent_id,serving_generation_id)
 REFERENCES agenteam_memory.generations(project_id,agent_id,generation_id) DEFERRABLE INITIALLY DEFERRED;
CREATE TABLE agenteam_memory.projections (
 generation_id agenteam_memory.safe_id NOT NULL REFERENCES agenteam_memory.generations(generation_id),
 project_id agenteam_memory.safe_id NOT NULL,
 agent_id agenteam_memory.safe_id NOT NULL,
 memory_id agenteam_memory.safe_id NOT NULL,
 revision bigint NOT NULL CHECK(revision>0),
 embedding vector NOT NULL,
 index_text text NOT NULL CHECK(octet_length(index_text) BETWEEN 1 AND 8192),
 PRIMARY KEY(generation_id,memory_id,revision),
 FOREIGN KEY(project_id,agent_id,memory_id,revision)
 REFERENCES agenteam_memory.revisions(project_id,agent_id,memory_id,revision)
);
CREATE INDEX memory_projection_namespace ON agenteam_memory.projections(project_id,agent_id,generation_id);

-- Each real extraction/consolidation/reflection/embedding invocation has its
-- own durable binding and usage identity. No model input text is duplicated.
CREATE TABLE agenteam_memory.model_calls (
 call_id agenteam_memory.safe_id PRIMARY KEY,
 operation_id agenteam_memory.safe_id NOT NULL,
 project_id agenteam_memory.safe_id NOT NULL,
 agent_id agenteam_memory.safe_id NOT NULL,
 execution_id agenteam_memory.safe_id NOT NULL,
 purpose text NOT NULL CHECK(purpose IN ('memory_extraction','memory_consolidation','memory_reflection','memory_embedding')),
 input_digest text NOT NULL CHECK(input_digest ~ '^sha256:[0-9a-f]{64}$'),
 snapshot_id agenteam_memory.safe_id,
 invocation_id agenteam_memory.safe_id,
 retired boolean NOT NULL DEFAULT false,
 created_at timestamptz(6) NOT NULL
);

-- Content/provenance are immutable; only the canonical lifecycle may change
-- a revision from active to superseded or deleted.
-- +goose StatementBegin
CREATE FUNCTION agenteam_memory.revision_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' OR (to_jsonb(NEW)-'status') IS DISTINCT FROM (to_jsonb(OLD)-'status')
 OR OLD.status<>'active' OR NEW.status NOT IN ('superseded','deleted') THEN
  RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='memory_revision_immutable',MESSAGE='immutable memory revision';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER memory_revision_immutable BEFORE UPDATE OR DELETE ON agenteam_memory.revisions
 FOR EACH ROW EXECUTE FUNCTION agenteam_memory.revision_immutable();

-- +goose StatementBegin
CREATE FUNCTION agenteam_memory.check_current_revision() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE m agenteam_memory.memories%ROWTYPE; r agenteam_memory.revisions%ROWTYPE;
BEGIN
 SELECT * INTO STRICT m FROM agenteam_memory.memories WHERE memory_id=NEW.memory_id;
 SELECT * INTO STRICT r FROM agenteam_memory.revisions WHERE memory_id=m.memory_id AND revision=m.current_revision;
 IF r.status<>m.status THEN
  RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='memory_current_status',MESSAGE='invalid current memory revision';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER memory_current_status AFTER INSERT OR UPDATE ON agenteam_memory.memories
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION agenteam_memory.check_current_revision();
CREATE CONSTRAINT TRIGGER memory_revision_current_status AFTER INSERT OR UPDATE ON agenteam_memory.revisions
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION agenteam_memory.check_current_revision();
