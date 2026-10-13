//go:build integration

package searchbenchmark

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5"
)

const nativeSchemaSQL = `CREATE TABLE public.lexical_sources (
 source_id text COLLATE "C" PRIMARY KEY,
 index_text text NOT NULL,
 search_vector tsvector GENERATED ALWAYS AS
   (to_tsvector('pg_catalog.simple'::regconfig, index_text)) STORED
)`
const nativeIndexSQL = `CREATE INDEX lexical_sources_fts ON public.lexical_sources USING gin (search_vector)`
const nativeQuerySQL = `WITH query AS (
 SELECT websearch_to_tsquery('pg_catalog.simple'::regconfig, $1) AS value
)
SELECT source_id, ts_rank_cd(search_vector, query.value, 0)::double precision AS score
FROM public.lexical_sources CROSS JOIN query
WHERE search_vector @@ query.value
ORDER BY score DESC, source_id COLLATE "C" ASC LIMIT 20`

type nativeHit struct {
	SourceID    string  `json:"source_id"`
	Rank        int     `json:"rank"`
	NativeScore float64 `json:"native_score"`
}
type nativeResult struct {
	QueryID string      `json:"query_id"`
	Hits    []nativeHit `json:"hits"`
}
type nativeLatency struct {
	QueryID   string    `json:"query_id"`
	Mode      string    `json:"mode"`
	SamplesMS []float64 `json:"samples_ms"`
}

// Run only through the original nonce-owned PG-only driver, after its image
// preparation window is authorized. No TestMain, download, model or migration.
func TestNativePGFTSBenchmark(t *testing.T) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	check := func(stage string, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("native PG FTS %s failed", stage)
		}
	}
	input, err := loadNativeInput(os.Getenv("AGENTEAM_NATIVE_FTS_EXPORT"))
	check("answer-free export", err)
	output := os.Getenv("AGENTEAM_NATIVE_FTS_OUTPUT")
	if _, err := os.Lstat(output); !filepath.IsAbs(output) || !errors.Is(err, os.ErrNotExist) {
		t.Fatal("absolute fresh candidate directory required before fixture use")
	}
	var outputs map[string]any
	// Registered before database/connection cleanup: publish only after their
	// original cleanup actually returns, without changing their lifecycle owner.
	t.Cleanup(func() {
		if t.Failed() || outputs == nil {
			return
		}
		if time.Since(started) > 75*time.Second {
			t.Error("native PG FTS body and database retirement exceeded 75s")
			return
		}
		check("candidate publication", publishNative(output, outputs))
	})
	fixture, err := pgfixture.Load()
	check("original owned fixture", err)
	if fixture.Image != pgfixture.Image {
		t.Fatal("fixed PostgreSQL 17 image required")
	}
	// This material comes from the same verified, already running container.
	// It is outside every indexing/query timing interval. Do not fetch license
	// text from the network or silently substitute a different package version.
	packageVersion, err := pgfixture.Docker(ctx, "exec", fixture.ContainerID, "dpkg-query", "-W", "-f=${Version}", "postgresql-17")
	check("installed package version", err)
	if string(packageVersion) != "17.8-1.pgdg12+1" {
		t.Fatal("fixed PostgreSQL package version required")
	}
	copyright, err := pgfixture.Docker(ctx, "exec", fixture.ContainerID, "cat", "/usr/share/doc/postgresql-17/copyright")
	check("installed package copyright", err)
	if len(copyright) == 0 || len(copyright) > maxInputBytes || !strings.Contains(string(copyright), "PostgreSQL Global Development Group") || !strings.Contains(string(copyright), "Permission to use, copy, modify, and distribute this software") {
		t.Fatal("actual PostgreSQL copyright and permission text required before indexing")
	}
	licenseDigest := sha256.Sum256(copyright)
	db := pgfixture.NewDatabase(t)
	connect := func() *pgx.Conn {
		t.Helper()
		conn, err := db.Fixture.Connect(ctx, db.Name)
		check("owned database connection", err)
		t.Cleanup(func() {
			closeCtx, stop := context.WithTimeout(context.Background(), 2*time.Second)
			defer stop()
			check("actual connection close", conn.Close(closeCtx))
		})
		return conn
	}
	conn := connect()
	var versionNum int
	var version, encoding, collation, ctype string
	check("server version and locale", conn.QueryRow(ctx, `SELECT current_setting('server_version_num')::int,
 current_setting('server_version'), current_setting('server_encoding'),
 current_setting('lc_collate'), current_setting('lc_ctype')`).Scan(&versionNum, &version, &encoding, &collation, &ctype))
	if versionNum != 170008 || encoding != "UTF8" {
		t.Fatal("actual PostgreSQL 17.8 UTF8 server required")
	}
	_, err = conn.Exec(ctx, `SET statement_timeout = '2s'`)
	check("bounded SQL", err)
	indexStart := time.Now()
	tx, err := conn.Begin(ctx)
	check("index transaction", err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, nativeSchemaSQL)
	check("table and exact-text vector", err)
	for _, source := range input.Sources {
		_, err = tx.Exec(ctx, `INSERT INTO public.lexical_sources (source_id,index_text) VALUES ($1,$2)`, source.ID, source.Text)
		check("fixed source insert", err)
	}
	_, err = tx.Exec(ctx, nativeIndexSQL)
	check("single GIN", err)
	_, err = tx.Exec(ctx, `ANALYZE public.lexical_sources`)
	check("fixed analyze", err)
	check("index commit", tx.Commit(ctx))
	indexingMS := float64(time.Since(indexStart)) / float64(time.Millisecond)
	// A different connection observes committed inputs and index catalog facts.
	// It does not use the candidate query to derive an expected ranking.
	observer := connect()
	rows, err := observer.Query(ctx, `SELECT source_id,index_text FROM public.lexical_sources ORDER BY source_id COLLATE "C"`)
	check("independent source read", err)
	defer rows.Close()
	var persisted []inputText
	for rows.Next() {
		var source inputText
		check("independent source scan", rows.Scan(&source.ID, &source.Text))
		persisted = append(persisted, source)
	}
	rows.Close()
	check("independent source EOF", rows.Err())
	if !reflect.DeepEqual(persisted, input.Sources) {
		t.Fatal("committed indexing text differs from all 64 exported sources")
	}
	var indexBytes int64
	var indexDefinition string
	var nativeOnly bool
	check("index and extension catalog", observer.QueryRow(ctx, `SELECT pg_relation_size('public.lexical_sources_fts'::regclass),
 pg_get_indexdef('public.lexical_sources_fts'::regclass),
 NOT EXISTS (SELECT 1 FROM pg_extension WHERE extname <> 'plpgsql')`).Scan(&indexBytes, &indexDefinition, &nativeOnly))
	if indexBytes < 0 || !strings.Contains(indexDefinition, "USING gin (search_vector)") || !nativeOnly {
		t.Fatal("single native FTS index without optional extensions required")
	}
	results := make([]nativeResult, len(input.Queries))
	latencies := make([]nativeLatency, len(input.Queries))
	known := make(map[string]bool, len(input.Sources))
	for _, source := range input.Sources {
		known[source.ID] = true
	}
	// One persistent connection and one fixed query order across all six passes.
	// Pass zero records results and warms; only passes 1..5 are measured as warm.
	for pass := 0; pass <= 5; pass++ {
		for i, query := range input.Queries {
			hits, elapsed, err := queryNative(ctx, conn, query.Text, known)
			check("fixed candidate query", err)
			if pass == 0 {
				results[i] = nativeResult{QueryID: query.ID, Hits: hits}
				latencies[i] = nativeLatency{QueryID: query.ID, Mode: "warm", SamplesMS: make([]float64, 0, 5)}
			} else {
				if !reflect.DeepEqual(hits, results[i].Hits) {
					t.Fatal("candidate ranking changed across fixed warm passes")
				}
				latencies[i].SamplesMS = append(latencies[i].SamplesMS, elapsed)
			}
		}
	}
	currentInput, err := loadNativeInput(os.Getenv("AGENTEAM_NATIVE_FTS_EXPORT"))
	check("unchanged export", err)
	if !reflect.DeepEqual(currentInput.SHA256, input.SHA256) {
		t.Fatal("export changed during the fixed candidate run")
	}
	environment := fmt.Sprintf("isolated Docker PG17.8; %s/%s; client NumCPU=%d GOMAXPROCS=%d; 64 sections; one persistent TLS loopback pgx connection; no competing benchmark; 1 untimed full pass then 5 warm full passes; Query through Rows EOF/Close; GIN bytes only; no cold-cache or capacity claim", runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), runtime.GOMAXPROCS(0))
	outputs = map[string]any{
		"run.json": map[string]any{
			"format_version": 1, "dataset_revision": datasetRevision, "backend": "native_pg_fts", "backend_version": version,
			"config":       map[string]any{"analyzer": "pg_catalog.simple: built-in default parser and simple dictionary; exact index_text; no stemming, stopwords or external dictionary", "query_mode": "websearch_to_tsquery(pg_catalog.simple, exact query text); @@; no fallback", "ranking": "ts_rank_cd(search_vector, query, 0) descending; no weights or normalization; not BM25", "candidate_k": 20, "tie_break": "source_id_ascii", "adapter_revision": nativeRevision},
			"results":      results,
			"measurements": map[string]any{"index_bytes": indexBytes, "indexing_ms": indexingMS, "environment": environment, "latencies": latencies},
		},
		"source.json": map[string]any{
			"adapter_revision": nativeRevision, "image": fixture.Image, "server_version_num": versionNum, "server_version": version, "package_version": string(packageVersion), "server_encoding": encoding, "lc_collate": collation, "lc_ctype": ctype,
			"export_sha256": input.SHA256, "source_rows": len(persisted), "query_rows": len(results), "candidate_queries": 432, "warm_samples_per_query": 5,
			"schema_sql": nativeSchemaSQL, "index_sql": nativeIndexSQL, "query_sql": nativeQuerySQL, "actual_index_definition": indexDefinition, "native_extensions_only": nativeOnly,
			"indexing_interval": "Begin + table/64 inserts + GIN + ANALYZE + commit; excludes fixture startup, license/input reads and validation",
			"query_interval":    "one persistent connection: Query through row scan, actual EOF and Close; excludes startup, connection, fixture and docker exec",
			"copyright_path":    "/usr/share/doc/postgresql-17/copyright", "copyright_sha256": hex.EncodeToString(licenseDigest[:]),
			"acceptance": "requires original driver actual Wait and exact owned resource retirement; this file is not whole-window acceptance",
		},
		"package-copyright.json": map[string]any{"image": fixture.Image, "package": "postgresql-17", "version": string(packageVersion), "path": "/usr/share/doc/postgresql-17/copyright", "text": string(copyright)},
	}
	t.Log("native PG FTS actual SQL: 64 committed source texts independently matched; 72 results; 432 candidate queries; 360 warm samples; no qrels consumed")
}

func queryNative(ctx context.Context, conn *pgx.Conn, text string, known map[string]bool) ([]nativeHit, float64, error) {
	started := time.Now()
	rows, err := conn.Query(ctx, nativeQuerySQL, text)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	hits := make([]nativeHit, 0, 20)
	seen := make(map[string]bool, 20)
	for rows.Next() {
		hit := nativeHit{Rank: len(hits) + 1}
		if err := rows.Scan(&hit.SourceID, &hit.NativeScore); err != nil {
			return nil, 0, err
		}
		if !known[hit.SourceID] || seen[hit.SourceID] || math.IsNaN(hit.NativeScore) || math.IsInf(hit.NativeScore, 0) || len(hits) >= 20 {
			return nil, 0, errors.New("invalid actual candidate hit")
		}
		if len(hits) != 0 {
			previous := hits[len(hits)-1]
			if hit.NativeScore > previous.NativeScore || hit.NativeScore == previous.NativeScore && hit.SourceID <= previous.SourceID {
				return nil, 0, errors.New("actual SQL ranking or tie break mismatch")
			}
		}
		hits = append(hits, hit)
		seen[hit.SourceID] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return hits, float64(time.Since(started)) / float64(time.Millisecond), nil
}
