-- agenteam:transaction tx
-- +goose Up
-- Canonical documents remain in D12. Every derived generation is replaceable
-- and is eligible only while its document version and serving profile match.
CREATE TABLE agenteam_knowledge.index_profiles (
 id agenteam_knowledge.safe_id PRIMARY KEY,
 project_id agenteam_knowledge.safe_id NOT NULL REFERENCES agenteam_project.projects(id) ON DELETE RESTRICT,
 version bigint NOT NULL CHECK(version>0),
 state text NOT NULL CHECK(state IN ('building','ready','active','retired','failed')),
 parser_profile text NOT NULL CHECK(octet_length(parser_profile) BETWEEN 1 AND 256),
 chunker_profile text NOT NULL CHECK(octet_length(chunker_profile) BETWEEN 1 AND 256),
 soft_chunk_bytes integer NOT NULL CHECK(soft_chunk_bytes BETWEEN 1 AND 1048576),
 hard_chunk_bytes integer NOT NULL CHECK(hard_chunk_bytes BETWEEN soft_chunk_bytes AND 1048576),
 forced_overlap_bytes integer NOT NULL CHECK(forced_overlap_bytes>=0 AND forced_overlap_bytes<hard_chunk_bytes),
 lexical_backend text NOT NULL CHECK(lexical_backend IN ('native_pg_fts','pgroonga','pg_search')),
 lexical_config jsonb NOT NULL CHECK(jsonb_typeof(lexical_config)='object' AND octet_length(lexical_config::text)<=16384),
 snapshot_id agenteam_knowledge.safe_id,
 dimension integer NOT NULL CHECK(dimension BETWEEN 1 AND 4096),
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 activated_at timestamptz(6),
 UNIQUE(project_id,id),
 UNIQUE(project_id,id,snapshot_id,dimension),
 UNIQUE(project_id,version),
 FOREIGN KEY(snapshot_id,project_id) REFERENCES agenteam_model.snapshots(id,project_id) ON DELETE RESTRICT,
 CHECK(state NOT IN ('ready','active') OR snapshot_id IS NOT NULL),
 CHECK((state='active' AND activated_at IS NOT NULL) OR state<>'active')
);
CREATE UNIQUE INDEX knowledge_one_active_profile ON agenteam_knowledge.index_profiles(project_id) WHERE state='active';

CREATE TABLE agenteam_knowledge.index_state (
 project_id agenteam_knowledge.safe_id PRIMARY KEY REFERENCES agenteam_project.projects(id) ON DELETE RESTRICT,
 serving_profile_id agenteam_knowledge.safe_id,
 target_profile_id agenteam_knowledge.safe_id NOT NULL,
 version bigint NOT NULL CHECK(version>0),
 FOREIGN KEY(project_id,serving_profile_id) REFERENCES agenteam_knowledge.index_profiles(project_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(project_id,target_profile_id) REFERENCES agenteam_knowledge.index_profiles(project_id,id) ON DELETE RESTRICT
);

CREATE TABLE agenteam_knowledge.index_jobs (
 id agenteam_knowledge.safe_id PRIMARY KEY,
 project_id agenteam_knowledge.safe_id NOT NULL,
 document_id agenteam_knowledge.safe_id NOT NULL,
 document_version bigint NOT NULL CHECK(document_version>0),
 object_id agenteam_knowledge.safe_id NOT NULL,
 profile_id agenteam_knowledge.safe_id NOT NULL,
 state text NOT NULL CHECK(state IN ('pending','running','succeeded','failed','cancelled','obsolete')),
 attempt_count integer NOT NULL DEFAULT 0 CHECK(attempt_count>=0),
 process_id agenteam_knowledge.safe_id REFERENCES agenteam_object.process_claims(process_id) ON DELETE RESTRICT,
 error_code text CHECK(error_code IS NULL OR octet_length(error_code) BETWEEN 1 AND 128),
 retryable boolean NOT NULL DEFAULT false,
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 started_at timestamptz(6),
 finished_at timestamptz(6),
 UNIQUE(project_id,id),
 UNIQUE(project_id,id,document_id,document_version,profile_id),
 UNIQUE(document_id,document_version,profile_id),
 FOREIGN KEY(project_id,document_id) REFERENCES agenteam_knowledge.documents(project_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(project_id,profile_id) REFERENCES agenteam_knowledge.index_profiles(project_id,id) ON DELETE RESTRICT,
 CHECK((state='pending' AND process_id IS NULL AND started_at IS NULL AND finished_at IS NULL)
  OR (state='running' AND process_id IS NOT NULL AND started_at IS NOT NULL AND finished_at IS NULL)
  OR (state IN ('succeeded','failed','cancelled','obsolete') AND finished_at IS NOT NULL))
);
CREATE INDEX knowledge_index_jobs_pending ON agenteam_knowledge.index_jobs(project_id,created_at,id) WHERE state='pending';

CREATE TABLE agenteam_knowledge.index_generations (
 id agenteam_knowledge.safe_id PRIMARY KEY,
 project_id agenteam_knowledge.safe_id NOT NULL,
 job_id agenteam_knowledge.safe_id NOT NULL UNIQUE,
 document_id agenteam_knowledge.safe_id NOT NULL,
 document_version bigint NOT NULL CHECK(document_version>0),
 profile_id agenteam_knowledge.safe_id NOT NULL,
 snapshot_id agenteam_knowledge.safe_id NOT NULL,
 dimension integer NOT NULL CHECK(dimension BETWEEN 1 AND 4096),
 state text NOT NULL CHECK(state IN ('building','ready','retired')),
 expected_chunks integer NOT NULL CHECK(expected_chunks BETWEEN 0 AND 65536),
 lexical_complete boolean NOT NULL DEFAULT false,
 dense_complete boolean NOT NULL DEFAULT false,
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 ready_at timestamptz(6),
 UNIQUE(project_id,id),
 UNIQUE(project_id,id,dimension),
 UNIQUE(document_id,document_version,profile_id),
 FOREIGN KEY(project_id,job_id,document_id,document_version,profile_id) REFERENCES agenteam_knowledge.index_jobs(project_id,id,document_id,document_version,profile_id) ON DELETE RESTRICT,
 FOREIGN KEY(project_id,document_id) REFERENCES agenteam_knowledge.documents(project_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(project_id,profile_id,snapshot_id,dimension) REFERENCES agenteam_knowledge.index_profiles(project_id,id,snapshot_id,dimension) ON DELETE RESTRICT,
 FOREIGN KEY(snapshot_id,project_id) REFERENCES agenteam_model.snapshots(id,project_id) ON DELETE RESTRICT,
 CHECK(state<>'ready' OR (lexical_complete AND dense_complete AND ready_at IS NOT NULL))
);
CREATE INDEX knowledge_generation_lookup ON agenteam_knowledge.index_generations(project_id,profile_id,document_id) WHERE state='ready';

CREATE TABLE agenteam_knowledge.index_chunks (
 id agenteam_knowledge.safe_id PRIMARY KEY,
 project_id agenteam_knowledge.safe_id NOT NULL,
 generation_id agenteam_knowledge.safe_id NOT NULL,
 ordinal integer NOT NULL CHECK(ordinal>0),
 raw_text text NOT NULL CHECK(octet_length(raw_text) BETWEEN 1 AND 1048576),
 index_text text NOT NULL CHECK(octet_length(index_text) BETWEEN 1 AND 1064960),
 locator jsonb NOT NULL CHECK(jsonb_typeof(locator)='object' AND octet_length(locator::text)<=16384),
 embedding vector NOT NULL,
 dimension integer NOT NULL CHECK(dimension BETWEEN 1 AND 4096 AND vector_dims(embedding)=dimension),
 UNIQUE(generation_id,ordinal),
 FOREIGN KEY(project_id,generation_id,dimension) REFERENCES agenteam_knowledge.index_generations(project_id,id,dimension) ON DELETE CASCADE
);
CREATE INDEX knowledge_chunks_generation ON agenteam_knowledge.index_chunks(project_id,generation_id,ordinal);

-- Model consumer provenance is retained after completion for Usage/Secret
-- finalization. A job/query identity alone never grants service authority.
CREATE TABLE agenteam_knowledge.model_operations (
 id agenteam_knowledge.safe_id PRIMARY KEY,
 project_id agenteam_knowledge.safe_id NOT NULL REFERENCES agenteam_project.projects(id) ON DELETE RESTRICT,
 kind text NOT NULL CHECK(kind IN ('index','query','profile')),
 job_id agenteam_knowledge.safe_id,
 profile_id agenteam_knowledge.safe_id NOT NULL,
 generation_id agenteam_knowledge.safe_id NOT NULL,
 snapshot_id agenteam_knowledge.safe_id,
 call_id agenteam_knowledge.safe_id NOT NULL UNIQUE,
 process_id agenteam_knowledge.safe_id NOT NULL REFERENCES agenteam_object.process_claims(process_id) ON DELETE RESTRICT,
 invocation jsonb CHECK(invocation IS NULL OR (jsonb_typeof(invocation)='object' AND octet_length(invocation::text)<=8192)),
 input_digest text NOT NULL CHECK(input_digest ~ '^sha256:[0-9a-f]{64}$'),
 state text NOT NULL CHECK(state IN ('active','completed','failed','retired')),
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 FOREIGN KEY(project_id,job_id) REFERENCES agenteam_knowledge.index_jobs(project_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(project_id,profile_id) REFERENCES agenteam_knowledge.index_profiles(project_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(snapshot_id,project_id) REFERENCES agenteam_model.snapshots(id,project_id) ON DELETE RESTRICT,
 CHECK((kind='index' AND job_id IS NOT NULL AND invocation IS NULL)
  OR (kind='query' AND job_id IS NULL AND invocation IS NOT NULL)
  OR (kind='profile' AND job_id IS NULL AND invocation IS NULL))
);
CREATE INDEX knowledge_model_operations_project ON agenteam_knowledge.model_operations(project_id,id);
