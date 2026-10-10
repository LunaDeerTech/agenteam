//go:build integration

package objects_test

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

// The SQL fixture is embedded so the candidate cannot silently consume a later
// edited runtime file. These are SQL-cost states, not native completion proofs.
// No Object Service is constructed against the database containing these rows.
//
//go:embed testdata/metadata_cleanup_project_cost.sql
var metadataProjectCostSQL string

func metadataCostID(prefix, n int) string {
	return fmt.Sprintf("%08x-0000-7000-8000-%012x", prefix, n)
}

func TestObjectMetadataCleanupProjectHistoryPlans(t *testing.T) {
	// Capture unchanged SQL through the public Service with the real Store/Tx.
	// A reader belonging to the first Service keeps this separate Project
	// pending while the second Service visits all five lanes. The second has
	// no ProcessGuard proof for the first process and cannot retire its I/O.
	base := newFixture(t, false)
	observed := &metadataPlanStore{Store: base.store}
	authority := newObjectStopAuthority(t, base, base.store)
	stopProcess := id[oc.Process](t)
	stopper := newObjectStopService(t, base, authority, stopServiceOptions{wrapper: observed, process: stopProcess})
	// Small objects reach EOF and release their native lease in ReadObject's
	// constructor. Keep bytes beyond its integrity prefetch unread instead.
	put := base.put(t, "cost-query-capture", strings.Repeat("x", 2*oc.StreamBufferSize+64))
	reader, err := base.service.ReadObject(contextFor(t), base.actor, base.owner, put.Meta.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	readerProcess := metadataCostReaderProcess(t, base, put.Meta.ID, stopProcess)
	actor, stop := activateObjectStop(t, base, oc.ProjectStopDelete)
	total, end := context.WithTimeout(contextFor(t), 3*time.Second)
	defer end()
	for range 5 {
		ctx, cancel := context.WithTimeout(total, 2*time.Second)
		deadline, _ := ctx.Deadline()
		report, err := stopper.RequestProjectStop(ctx, actor, stop)
		returned := time.Now()
		cancel()
		if err != nil || returned.After(deadline) || report.Details().State != oc.ProjectStopPending {
			t.Fatalf("real foreign reader did not retain the finite Stop scan: state=%s late=%t err=%v", report.Details().State, returned.After(deadline), err)
		}
		assertStopReaderLifetime(t, base, put.Meta.ID, readerProcess, true)
	}
	if err := reader.Close(); err != nil {
		t.Fatal("actual query-capture reader close", err)
	}
	assertStopReaderLifetime(t, base, put.Meta.ID, readerProcess, false)
	end()
	names := []string{"stop-work", "stop-reserved", "stop-leases", "stop-grants", "stop-transfers", "stop-full-pending"}
	queries := make(map[string]metadataPlanQuery, len(names))
	observed.mu.Lock()
	for _, name := range names {
		queries[name] = observed.queries[name]
	}
	observed.mu.Unlock()
	for _, name := range names {
		q := queries[name]
		wantArgs := 3
		if name == "stop-full-pending" {
			wantArgs = 2
		}
		if q.sql == "" || len(q.args) != wantArgs || q.args[0] != base.project.String() || q.args[wantArgs-1] != true {
			t.Fatal("actual Delete Stop SQL was not captured", name)
		}
		if wantArgs == 3 && q.args[1] != nil {
			t.Fatal("cost source lost the actual first-page cursor", name)
		}
	}

	// Isolate SQL-only state from every Service callback and maintenance scan.
	db := pgfixture.NewDatabase(t)
	cfg := db.Config(t, nil)
	migrator, err := postgres.NewMigrator(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if result := migrator.Migrate(contextFor(t)); !result.Migrated {
		t.Fatal("cost database migration", result.Fault)
	}
	store, err := postgres.Open(contextFor(t), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := store.ForceClose(ctx); err != nil {
			t.Error(err)
		}
	})
	// Explicit BEGIN/SET CONSTRAINTS/COMMIT belong to this owned fixture
	// connection; production Store correctly forbids transaction-control SQL.
	seed := db.Connect(t)
	if _, err := seed.Exec(contextFor(t), metadataProjectCostSQL); err != nil {
		t.Fatal("schema-constrained SQL cost fixture", err)
	}
	target, absent := metadataCostID(0x01910000, 1), metadataCostID(0x01910000, 3)
	metadataCostCardinalities(t, store, false)
	metadataCostProjectPlans(t, store, queries, "retired-target", target, false)
	metadataCostProjectPlans(t, store, queries, "absent-target-with-other-data", absent, false)

	// Only this SQL database gains 33 active-reader shapes. They target the
	// separate available Object, never one of the 2050 deleted Objects. This
	// tests predicates and keyset cost; it does not simulate ProcessGuard/join.
	if _, err := seed.Exec(contextFor(t), metadataCostActiveReadersSQL); err != nil {
		t.Fatal("active SQL cost state", err)
	}
	metadataCostCardinalities(t, store, true)
	metadataCostProjectPlans(t, store, queries, "active-reader-tail", target, true)
	// Check absence again while another Project AND active target rows remain.
	metadataCostProjectPlans(t, store, queries, "absent-with-active-other-project", absent, false)
	metadataCostCardinalities(t, store, true)
}

func metadataCostReaderProcess(t *testing.T, f *fixture, object oc.ObjectID, stopper oc.ProcessID) oc.ProcessID {
	t.Helper()
	var raw string
	if err := f.store.QueryRow(contextFor(t), `SELECT process_id::text FROM agenteam_object.object_leases WHERE object_id=$1 AND owner_kind='reader'`, object.String()).Scan(&raw); err != nil {
		t.Fatal("query-capture reader process", err)
	}
	process, err := foundation.ParseID[oc.Process](raw)
	if err != nil || process == stopper {
		t.Fatal("query-capture reader is not owned by a distinct actual process", err)
	}
	assertStopReaderLifetime(t, f, object, process, true)
	return process
}

func metadataCostCardinalities(t *testing.T, store *postgres.Store, active bool) {
	t.Helper()
	var target, other, available, targetHistory, otherHistory, liveLeases, liveWork, attempts, cleanup int
	err := store.QueryRow(contextFor(t), `SELECT
 (SELECT count(*) FROM agenteam_object.objects WHERE project_id=$1 AND state='deleted'),
 (SELECT count(*) FROM agenteam_object.objects WHERE project_id=$2 AND state='deleted'),
 (SELECT count(*) FROM agenteam_object.objects WHERE project_id=$1 AND state='available'),
 (SELECT count(*) FROM agenteam_object.project_work WHERE project_id=$1 AND joined_at IS NOT NULL),
 (SELECT count(*) FROM agenteam_object.project_work WHERE project_id=$2 AND joined_at IS NOT NULL),
 (SELECT count(*) FROM agenteam_object.object_leases WHERE state='active'),
 (SELECT count(*) FROM agenteam_object.project_work WHERE joined_at IS NULL),
 (SELECT count(*) FROM agenteam_object.upload_attempts),
 (SELECT count(*) FROM agenteam_object.cleanup_operations)`, metadataCostID(0x01910000, 1), metadataCostID(0x01910000, 2)).Scan(&target, &other, &available, &targetHistory, &otherHistory, &liveLeases, &liveWork, &attempts, &cleanup)
	wantLive := 0
	if active {
		wantLive = 33
	}
	if err != nil || target != 1025 || other != 1025 || available != 1 || targetHistory != 1001 || otherHistory != 10001 || liveLeases != wantLive || liveWork != wantLive || attempts != 2116 || cleanup != 2115 {
		t.Fatalf("SQL cost cardinalities err=%v objects=%d/%d available=%d history=%d/%d active=%d/%d attempts=%d cleanup=%d", err, target, other, available, targetHistory, otherHistory, liveLeases, liveWork, attempts, cleanup)
	}
}

func metadataCostProjectPlans(t *testing.T, store *postgres.Store, queries map[string]metadataPlanQuery, stage, project string, active bool) {
	t.Helper()
	for _, deleting := range []bool{false, true} {
		action := "archive"
		if deleting {
			action = "delete"
		}
		for _, name := range []string{"stop-work", "stop-reserved", "stop-leases", "stop-grants", "stop-transfers"} {
			q := queries[name]
			prefix := 0x019b0000
			if name == "stop-leases" {
				prefix = 0x019a0000
			}
			for _, after := range []any{nil, metadataCostID(prefix, 32)} {
				// Only legal bind parameters change; the observed SQL is verbatim.
				args := []any{project, after, deleting}
				var want []string
				if active && deleting && (name == "stop-work" || name == "stop-leases") {
					first, last := 1, 32
					if after != nil {
						first, last = 33, 33
					}
					for n := first; n <= last; n++ {
						want = append(want, metadataCostID(prefix, n))
					}
				}
				metadataCostQueryIDs(t, store, q.sql, args, want)
				page := "first"
				if after != nil {
					page = "after-32"
				}
				metadataCostExplain(t, store, stage+"/"+action+"/"+page, name, q.sql, args, len(want))
			}
		}
		q := queries["stop-full-pending"]
		args := []any{project, deleting}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		deadline, _ := ctx.Deadline()
		var pending bool
		err := store.QueryRow(ctx, q.sql, args...).Scan(&pending)
		returned := time.Now()
		cancel()
		if err != nil || returned.After(deadline) || pending != (active && deleting) {
			t.Fatal("full SQL pending predicate", stage, action, pending, err)
		}
		metadataCostExplain(t, store, stage+"/"+action, "stop-full-pending", q.sql, args, 1)
	}
}

func metadataCostQueryIDs(t *testing.T, store *postgres.Store, sql string, args []any, want []string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	rows, err := store.Query(ctx, sql, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		got = append(got, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil || time.Now().After(deadline) {
		t.Fatal("actual query/Rows close exceeded its original observation deadline", err)
	}
	if len(got) != len(want) {
		t.Fatalf("cost query cardinality got=%d want=%d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatal("cost keyset changed identity or ordering", i)
		}
	}
}

func metadataCostExplain(t *testing.T, store *postgres.Store, stage, name, sql string, args []any, wantRows int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	deadline, _ := ctx.Deadline()
	var raw []byte
	err := store.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+sql, args...).Scan(&raw)
	returned := time.Now()
	cancel()
	var plans []struct {
		Plan metadataCostPlanNode `json:"Plan"`
	}
	if err != nil || returned.After(deadline) || json.Unmarshal(raw, &plans) != nil || len(plans) != 1 || plans[0].Plan.NodeType == "" || plans[0].Plan.Rows != float64(wantRows) || plans[0].Plan.Loops != 1 {
		t.Fatal("executed cost plan/result mismatch or late return", stage, name, err)
	}
	// Preserve the original full plan before rejecting work hidden below a
	// one-row Boolean result or LIMIT. No scan/index name is forced or banned.
	t.Logf("D05 SQL-cost EXPLAIN stage=%s query=%s plan=%s", stage, name, raw)
	if err := metadataCostPlanWithinFixtureBounds(plans[0].Plan); err != nil {
		t.Fatal("cost plan traversed beyond the fixture's current/batch work", stage, name, err)
	}
}

const metadataCostActiveReadersSQL = `
BEGIN;
INSERT INTO agenteam_object.object_leases(id,object_id,owner_kind,owner_id,process_id,state)
SELECT ('019a0000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,
 '01920000-0000-7000-8000-000000000803','reader',
 ('019a0000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,
 '01910000-0000-7000-8000-000000000005','active' FROM generate_series(1,33) n;
INSERT INTO agenteam_object.project_work(id,project_id,process_id,kind,resource_id,object_id,admission_version)
SELECT ('019b0000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,
 '01910000-0000-7000-8000-000000000001','01910000-0000-7000-8000-000000000005','reader',
 ('019a0000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,
 '01920000-0000-7000-8000-000000000803',0 FROM generate_series(1,33) n;
SET CONSTRAINTS ALL IMMEDIATE;
COMMIT;
ANALYZE agenteam_object.object_leases;
ANALYZE agenteam_object.project_work;`
