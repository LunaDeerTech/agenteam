// Package lexical supplies the fixed SQL syntax of actual database retrieval
// backends. It does not tokenize text or imitate an extension's ranker.
package lexical

import (
	"strconv"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

type Backend string

const (
	NativePostgres Backend = "native_pg_fts"
	PGroonga       Backend = "pgroonga"
	PGSearch       Backend = "pg_search"
)

type Relation string

const (
	KnowledgeChunks  Relation = "c"
	MemoryProjection Relation = "p"
)

type SQL struct {
	Match string
	Score string
}

// PGSearchReadinessSQL must run on the application's own database before
// publishing a service explicitly configured with PGSearch. Merely accepting
// that backend name, or finding the extension on another database, is not ready.
// It neither installs objects nor changes the active domain profile/generation.
const PGSearchReadinessSQL = `SELECT
 current_setting('server_version_num')::integer=170008
 AND 'pg_search'=ANY(regexp_split_to_array(current_setting('shared_preload_libraries'),'\s*,\s*'))
 AND EXISTS(SELECT 1 FROM pg_extension WHERE extname='pg_search' AND extversion='0.26.1')
 AND EXISTS(SELECT 1 FROM pg_extension WHERE extname='vector' AND extversion='0.8.1')
 AND (SELECT count(*)=2
 FROM (VALUES
  ('agenteam_knowledge','index_chunks','knowledge_chunks_pg_search','id'),
  ('agenteam_memory','projections','memory_projections_pg_search','memory_id')
 ) AS expected(schema_name,table_name,index_name,key_name)
 JOIN pg_namespace ns ON ns.nspname=expected.schema_name
 JOIN pg_class tab ON tab.relnamespace=ns.oid AND tab.relname=expected.table_name
 JOIN pg_class idx ON idx.relnamespace=ns.oid AND idx.relname=expected.index_name
 JOIN pg_index i ON i.indexrelid=idx.oid AND i.indrelid=tab.oid
 JOIN pg_am am ON am.oid=idx.relam AND am.amname='bm25'
 JOIN pg_attribute key ON key.attrelid=tab.oid AND key.attname=expected.key_name AND NOT key.attisdropped
 JOIN pg_attribute expression ON expression.attrelid=idx.oid AND expression.attnum=2
 JOIN pg_type tokenizer ON tokenizer.oid=expression.atttypid AND tokenizer.typname='chinese_compatible'
 JOIN pg_namespace tokenizer_ns ON tokenizer_ns.oid=tokenizer.typnamespace AND tokenizer_ns.nspname='pdb'
 WHERE i.indisvalid AND i.indisready AND i.indislive
 AND NOT i.indisunique AND i.indnatts=2 AND i.indnkeyatts=2
 AND i.indkey[0]=key.attnum AND i.indkey[1]=0 AND i.indpred IS NULL
 AND idx.reloptions=ARRAY['key_field='||expected.key_name]
 AND regexp_replace(pg_get_indexdef(idx.oid,2,true),'[()[:space:]]','','g')
     IN ('index_text::pdb.chinese_compatible','index_text::chinese_compatible'))`

// Expressions only accepts the two domain-owned relation aliases and a bound
// query parameter. Scope/current-version predicates remain in the domain SQL.
// Selecting a Backend here does not install, enable or select it in production.
func Expressions(backend Backend, relation Relation, queryParameter int) (SQL, error) {
	if relation != KnowledgeChunks && relation != MemoryProjection || queryParameter < 1 || queryParameter > 64 {
		return SQL{}, f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	column, query := string(relation)+".index_text", "$"+strconv.Itoa(queryParameter)
	switch backend {
	case NativePostgres:
		vector := "to_tsvector('simple'," + column + ")"
		parsed := "websearch_to_tsquery('simple'," + query + ")"
		return SQL{Match: vector + " @@ " + parsed, Score: "ts_rank_cd(" + vector + "," + parsed + ",0)"}, nil
	case PGroonga:
		return SQL{Match: column + " &@~ " + query, Score: "pgroonga_score(" + string(relation) + ".tableoid," + string(relation) + ".ctid)"}, nil
	case PGSearch:
		// pg_search 0.26.1 identifies the relation from this column reference;
		// score does not look up rows by the value of a unique key. In Memory,
		// the same memory_id may occur in several generations and revisions.
		id := string(relation) + ".id"
		if relation == MemoryProjection {
			id = "p.memory_id"
		}
		return SQL{
			Match: id + " @@@ paradedb.match('index_text'," + query + ",conjunction_mode=>false)",
			Score: "paradedb.score(" + id + ")",
		}, nil
	default:
		return SQL{}, f.NewFault(f.CapabilityUnsupported, f.NotStarted)
	}
}
