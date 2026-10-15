-- Explicit operator bootstrap, not part of the global migration sequence.
-- Run only against the intended database after its Knowledge/Memory schemas
-- exist. The caller must use psql -X -v ON_ERROR_STOP=1 -f this-file.
-- A repeated invocation fails if an index already exists; use the separate
-- readiness check to inspect an existing deployment rather than skipping DDL.
BEGIN;
DO $$
BEGIN
  IF current_setting('server_version_num')::integer <> 170008 THEN
    RAISE EXCEPTION 'PG_SEARCH_POSTGRES_VERSION_UNSUPPORTED';
  END IF;
  IF NOT ('pg_search' = ANY(regexp_split_to_array(current_setting('shared_preload_libraries'), '\s*,\s*'))) THEN
    RAISE EXCEPTION 'PG_SEARCH_PRELOAD_REQUIRED';
  END IF;
  IF (SELECT extversion FROM pg_extension WHERE extname='vector') IS DISTINCT FROM '0.8.1' THEN
    RAISE EXCEPTION 'PG_SEARCH_VECTOR_VERSION_UNSUPPORTED';
  END IF;
END $$;
CREATE EXTENSION IF NOT EXISTS pg_search VERSION '0.26.1';
DO $$
BEGIN
  IF (SELECT extversion FROM pg_extension WHERE extname='pg_search') IS DISTINCT FROM '0.26.1' THEN
    RAISE EXCEPTION 'PG_SEARCH_EXTENSION_VERSION_UNSUPPORTED';
  END IF;
END $$;
CREATE INDEX knowledge_chunks_pg_search
  ON agenteam_knowledge.index_chunks USING bm25
  (id, (index_text::pdb.chinese_compatible)) WITH (key_field='id');
CREATE INDEX memory_projections_pg_search
  ON agenteam_memory.projections USING bm25
  (memory_id, (index_text::pdb.chinese_compatible)) WITH (key_field='memory_id');
COMMIT;
