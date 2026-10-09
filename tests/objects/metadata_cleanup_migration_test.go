//go:build integration

package objects_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"testing"
	"testing/fstest"

	"github.com/LunaDeerTech/agenteam/db/migrations"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const metadataIndexMigration = "00028_cleanup_indexes.sql"

type metadataIndex struct{ name, schema, table string }

func metadataIndexes(t *testing.T) ([]byte, []metadataIndex) {
	t.Helper()
	raw, err := fs.ReadFile(migrations.SQL, metadataIndexMigration)
	if err != nil {
		t.Fatal(err)
	}
	matches := regexp.MustCompile(`(?m)^CREATE INDEX ([a-z_]+) ON ([a-z_]+)\.([a-z_]+)\(`).FindAllSubmatch(raw, -1)
	if len(matches) == 0 {
		t.Fatal("cleanup index migration contains no candidate indexes")
	}
	var out []metadataIndex
	seen := map[string]bool{}
	for _, m := range matches {
		x := metadataIndex{string(m[1]), string(m[2]), string(m[3])}
		if seen[x.schema+"."+x.name] {
			t.Fatal("duplicate cleanup index name")
		}
		seen[x.schema+"."+x.name] = true
		out = append(out, x)
	}
	return raw, out
}

// Compare all pre-existing checks/FKs/columns/indexes. Only the exact new
// index identities are excluded, never an entire schema or table.
func metadataIndexCatalog(t *testing.T, ctx context.Context, conn *pgx.Conn, indexes []metadataIndex) string {
	t.Helper()
	var catalog map[string]json.RawMessage
	if err := json.Unmarshal([]byte(workMigrationCatalog(t, ctx, conn, false)), &catalog); err != nil {
		t.Fatal(err)
	}
	var rows [][]any
	if err := json.Unmarshal(catalog["indexes"], &rows); err != nil {
		t.Fatal(err)
	}
	var kept [][]any
	for _, row := range rows {
		exclude := false
		for _, index := range indexes {
			if len(row) >= 3 && row[0] == index.schema && row[1] == index.table && row[2] == index.name {
				exclude = true
			}
		}
		if !exclude {
			kept = append(kept, row)
		}
	}
	raw, err := json.Marshal(kept)
	if err != nil {
		t.Fatal(err)
	}
	catalog["indexes"] = raw
	raw, err = json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func metadataIndexState(t *testing.T, ctx context.Context, conn *pgx.Conn, indexes []metadataIndex, installed bool) {
	t.Helper()
	for _, index := range indexes {
		var present, valid bool
		err := conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND c.relname=$2),EXISTS(SELECT 1 FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid JOIN pg_namespace n ON n.oid=c.relnamespace JOIN pg_class t ON t.oid=i.indrelid JOIN pg_am a ON a.oid=c.relam WHERE n.nspname=$1 AND c.relname=$2 AND t.relname=$3 AND i.indisvalid AND i.indisready AND NOT i.indisunique AND a.amname='btree')`, index.schema, index.name, index.table).Scan(&present, &valid)
		if err != nil || present != installed || valid != installed {
			t.Fatal("candidate index actual catalog state", index.name, installed, err)
		}
	}
}

func metadataIndexJournal(t *testing.T, ctx context.Context, conn *pgx.Conn, checksum string, applied bool) {
	t.Helper()
	var state, sum string
	var journal, goose, version int
	err := conn.QueryRow(ctx, `SELECT (SELECT count(*) FROM agenteam_meta.migration_journal WHERE version=28),(SELECT state FROM agenteam_meta.migration_journal WHERE version=28 AND filename=$1 AND mode='tx'),(SELECT checksum FROM agenteam_meta.migration_journal WHERE version=28),(SELECT count(*) FROM agenteam_meta.goose_db_version WHERE version_id=28 AND is_applied),(SELECT max(version_id) FROM agenteam_meta.goose_db_version WHERE is_applied)`, metadataIndexMigration).Scan(&journal, &state, &sum, &goose, &version)
	wantState, wantGoose, wantVersion := "pending", 0, 27
	if applied {
		wantState, wantGoose, wantVersion = "applied", 1, 28
	}
	if err != nil || journal != 1 || state != wantState || sum != checksum || goose != wantGoose || version != wantVersion {
		t.Fatal("cleanup migration journal/Goose agreement", applied, err)
	}
}

func TestObjectMetadataCleanupIndexMigration(t *testing.T) {
	raw, indexes := metadataIndexes(t)
	for _, mode := range []string{"fresh", "populated-27", "atomic-failure-same-bytes-retry"} {
		t.Run(mode, func(t *testing.T) {
			ctx := projectMigrationContext(t)
			db := pgfixture.NewDatabase(t)
			conn := db.Connect(t)
			files := projectMigrationFiles(t, "00027")
			var facts, stable string
			if mode != "fresh" {
				projectMigrate(t, ctx, projectMigrationRunner(t, db, files), 27)
				// Existing migration fixture keeps a live source lease/applying
				// cleanup/unjoined work. It is schema data, not actual I/O proof;
				// this test never passes it to the cleanup Service.
				workMigrationSeed(t, ctx, conn)
				facts = workMigrationFacts(t, ctx, conn)
				stable = metadataIndexCatalog(t, ctx, conn, indexes)
				metadataIndexState(t, ctx, conn, indexes, false)
			}
			selected := append([]byte(nil), raw...)
			if mode == "atomic-failure-same-bytes-retry" {
				selected = append(selected, []byte("\nSELECT public.metadata_cleanup_fixture_dependency();\n")...)
			}
			files[metadataIndexMigration] = &fstest.MapFile{Data: selected}
			checksum := fmt.Sprintf("sha256:%x", sha256.Sum256(selected))
			runner := projectMigrationRunner(t, db, files)
			if mode == "atomic-failure-same-bytes-retry" {
				result := runner.Migrate(ctx)
				var pg *pgconn.PgError
				if result.Migrated || !errors.As(result.Fault, &pg) || pg.Code != "42883" {
					t.Fatal("missing end-of-DDL dependency did not fail atomically", result.Fault)
				}
				metadataIndexState(t, ctx, conn, indexes, false)
				metadataIndexJournal(t, ctx, conn, checksum, false)
				if facts != workMigrationFacts(t, ctx, conn) || stable != metadataIndexCatalog(t, ctx, conn, indexes) {
					t.Fatal("failed index migration changed old rows or schema")
				}
				// Restore only the injected dependency. Retrying uses the exact
				// same failed bytes/checksum, never a rewritten journal.
				workMigrationExec(t, ctx, conn, `CREATE FUNCTION public.metadata_cleanup_fixture_dependency() RETURNS void LANGUAGE SQL AS 'SELECT NULL::void'`)
			}
			projectMigrate(t, ctx, runner, 28)
			metadataIndexState(t, ctx, conn, indexes, true)
			metadataIndexJournal(t, ctx, conn, checksum, true)
			if mode != "fresh" && (facts != workMigrationFacts(t, ctx, conn) || stable != metadataIndexCatalog(t, ctx, conn, indexes)) {
				t.Fatal("index upgrade changed old rows/checks/FKs/columns/indexes")
			}
			before := workMigrationCatalog(t, ctx, conn, false)
			projectMigrate(t, ctx, runner, 28)
			metadataIndexJournal(t, ctx, conn, checksum, true)
			if before != workMigrationCatalog(t, ctx, conn, false) {
				t.Fatal("already applied retry changed the schema")
			}
			t.Logf("candidate indexes=%d mode=%s; migration checks do not prove query or FK-trigger cost", len(indexes), mode)
		})
	}
}
