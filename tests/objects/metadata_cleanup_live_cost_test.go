//go:build integration

package objects_test

import (
	"context"
	_ "embed"
	"sort"
	"testing"
	"time"

	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

//go:embed testdata/metadata_cleanup_live_cost.sql
var metadataLiveCostSQL string

func TestObjectMetadataCleanupLiveTransferAndDownloadPlans(t *testing.T) {
	queries := metadataLiveCaptureStopQueries(t)
	objectQueries := metadataCostCaptureObjectQueries(t)
	db := pgfixture.NewDatabase(t)
	cfg := db.Config(t, nil)
	migrator, err := postgres.NewMigrator(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if result := migrator.Migrate(contextFor(t)); !result.Migrated {
		t.Fatal(result.Fault)
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
	// Native query capture and SQL costs never share their database. In
	// particular the marked grants/retirement evidence are never consumed by
	// Service, Downloads, an EvidenceAuthority or a ProcessGuard.
	conn := db.Connect(t)
	for _, sql := range []string{metadataProjectCostSQL, metadataTransferCostSQL, metadataLiveCostSQL} {
		if _, err := conn.Exec(contextFor(t), sql); err != nil {
			t.Fatal("isolated live SQL shapes", err)
		}
	}
	metadataLiveCostCardinalities(t, store, 68, 2, 33)
	target, absent := metadataCostID(0x01910000, 1), metadataCostID(0x01910000, 3)
	for _, deleting := range []bool{false, true} {
		want := map[string][]string{
			"stop-work":      {metadataCostID(0x01c70000, 1)},
			"stop-reserved":  metadataLiveIDs(0x01c00000, 33),
			"stop-leases":    append(metadataLiveIDs(0x01c40000, 33), metadataCostID(0x01c80000, 1)),
			"stop-grants":    nil,
			"stop-transfers": metadataLiveIDs(0x01c30000, 33),
		}
		if deleting {
			want["stop-work"] = append(want["stop-work"], metadataCostID(0x01c90000, 2))
			want["stop-leases"] = append(want["stop-leases"], metadataLiveIDs(0x01c60000, 33)...)
			want["stop-leases"] = append(want["stop-leases"], metadataCostID(0x01c90000, 2))
			want["stop-grants"] = metadataLiveIDs(0x01cb0000, 33)
			want["stop-transfers"] = append(want["stop-transfers"], metadataLiveIDs(0x01c50000, 33)...)
		}
		for _, name := range []string{"stop-work", "stop-reserved", "stop-leases", "stop-grants", "stop-transfers"} {
			metadataLiveCostPages(t, store, queries[name], "live/"+name, target, deleting, want[name])
			metadataLiveCostPages(t, store, queries[name], "absent/"+name, absent, deleting, nil)
		}
		metadataLivePending(t, store, queries["stop-full-pending"], "live-project", []any{target, deleting}, true)
		metadataLivePending(t, store, queries["stop-full-pending"], "absent-project", []any{absent, deleting}, false)
	}
	// The real gate SQL now has an open native private candidate plus an
	// external staging attempt. The current anchor is excluded from its batch;
	// it is still part of the separately observed full pending predicates.
	for _, n := range []int{1, 2} {
		object := metadataCostID(0x01c00000, n)
		current := metadataCostID(0x01c20000, n)
		var want []string
		if n == 1 {
			current = metadataCostID(0x01c70000, 1)
			want = []string{metadataCostID(0x01c20000, 1)}
		}
		q := objectQueries["gate-two-pending-sets"]
		metadataCostQueryIDs(t, store, q.sql, []any{object, current}, want)
		metadataCostExplain(t, store, "live-put", "gate-two-pending-sets", q.sql, []any{object, current}, len(want))
		for _, name := range []string{"metadata-full-pending", "physical-full-pending"} {
			args := []any{object}
			if name == "physical-full-pending" {
				args = append(args, []string{})
			}
			metadataLivePending(t, store, objectQueries[name], "live-put/"+name, args, true)
		}
	}

	// These mutations define distinct SQL snapshots, not a simulated Service
	// retirement or proof of actual I/O completion. Keep every historical row
	// and foreign Project in place throughout all later plans.
	if _, err := conn.Exec(contextFor(t), metadataCostRetirePUTShapeSQL); err != nil {
		t.Fatal("retired PUT SQL comparison snapshot", err)
	}
	metadataLiveCostCardinalities(t, store, 33, 0, 33)
	metadataLivePending(t, store, queries["stop-full-pending"], "get-and-download/archive", []any{target, false}, false)
	metadataLivePending(t, store, queries["stop-full-pending"], "get-and-download/delete", []any{target, true}, true)
	metadataLiveCostPages(t, store, queries["stop-transfers"], "get-only/archive", target, false, nil)
	metadataLiveCostPages(t, store, queries["stop-transfers"], "get-only/delete", target, true, metadataLiveIDs(0x01c50000, 33))
	if _, err := conn.Exec(contextFor(t), metadataCostRetireGETShapeSQL); err != nil {
		t.Fatal("retired GET SQL comparison snapshot", err)
	}
	metadataLiveCostCardinalities(t, store, 0, 0, 33)
	metadataLivePending(t, store, queries["stop-full-pending"], "downloads-only/delete", []any{target, true}, true)
	if _, err := conn.Exec(contextFor(t), `UPDATE agenteam_download.grants SET revoked=true WHERE project_id=$1 AND NOT revoked`, target); err != nil {
		t.Fatal(err)
	}
	metadataLiveCostCardinalities(t, store, 0, 0, 0)
	metadataLiveCostPages(t, store, queries["stop-grants"], "revoked-but-unresolved", target, true, nil)
	// Started-only must remain pending even with no active grants/leases and
	// its original work joined. No absence or elapsed time is a byte outcome.
	metadataLivePending(t, store, queries["stop-full-pending"], "revoked-started-only/delete", []any{target, true}, true)
	metadataLivePending(t, store, queries["stop-full-pending"], "revoked-started-only/archive", []any{target, false}, false)
	if _, err := conn.Exec(contextFor(t), `UPDATE agenteam_download.attempts SET pending_phase='failed',pending_bytes=0,pending_failure='cancelled' WHERE id=$1 AND phase='started'`, metadataCostID(0x01cc0000, 1)); err != nil {
		t.Fatal(err)
	}
	// This is solely the SQL query's checkpoint+joined shape. It does not
	// supply Downloads' private stream evidence or a terminal Audit result.
	metadataLivePending(t, store, queries["stop-full-pending"], "checkpoint-and-joined/delete", []any{target, true}, false)
	metadataLiveCostCardinalities(t, store, 0, 0, 0)
	var phase string
	if err := store.QueryRow(contextFor(t), `SELECT phase FROM agenteam_download.attempts WHERE id=$1`, metadataCostID(0x01cc0000, 1)).Scan(&phase); err != nil || phase != "started" {
		t.Fatal("SQL cost probe silently turned checkpoint into Audit completion", phase, err)
	}
}

// Reuse the existing real-query observation method without modifying the
// already compiled three-cost-top source or any production query.
func metadataLiveCaptureStopQueries(t *testing.T) map[string]metadataPlanQuery {
	t.Helper()
	base := newFixture(t, false)
	observed := &metadataPlanStore{Store: base.store}
	authority := newObjectStopAuthority(t, base, base.store)
	stopper := newObjectStopService(t, base, authority, stopServiceOptions{wrapper: observed})
	put := base.put(t, "live-cost-query-capture", "cost")
	reader, err := base.service.ReadObject(contextFor(t), base.actor, base.owner, put.Meta.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
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
			t.Fatal("foreign reader did not preserve all five actual query lanes", err)
		}
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	queries := make(map[string]metadataPlanQuery)
	observed.mu.Lock()
	for name, q := range observed.queries {
		queries[name] = q
	}
	observed.mu.Unlock()
	for _, name := range []string{"stop-work", "stop-reserved", "stop-leases", "stop-grants", "stop-transfers", "stop-full-pending"} {
		q := queries[name]
		n := 3
		if name == "stop-full-pending" {
			n = 2
		}
		if q.sql == "" || len(q.args) != n || q.args[0] != base.project.String() || q.args[n-1] != true || n == 3 && q.args[1] != nil {
			t.Fatal("missing original SQL/first-page parameter capture", name)
		}
	}
	return queries
}

func metadataLiveIDs(prefix, count int) []string {
	ids := make([]string, count)
	for i := range ids {
		ids[i] = metadataCostID(prefix, i+1)
	}
	return ids
}

func metadataLiveCostPages(t *testing.T, store *postgres.Store, q metadataPlanQuery, stage, project string, deleting bool, expected []string) {
	t.Helper()
	ids := append([]string(nil), expected...)
	sort.Strings(ids)
	// The closed expected identities drive pages; the observed SQL output is
	// never used to compute its own next cursor or expected result.
	for start := 0; ; start += 32 {
		var after any
		if start > 0 {
			after = ids[start-1]
		}
		end := min(start+32, len(ids))
		args := []any{project, after, deleting}
		metadataCostQueryIDs(t, store, q.sql, args, ids[start:end])
		metadataCostExplain(t, store, stage, "page", q.sql, args, end-start)
		if end == len(ids) {
			if len(ids) > 0 {
				args[1] = ids[len(ids)-1]
				metadataCostQueryIDs(t, store, q.sql, args, nil)
				metadataCostExplain(t, store, stage, "after-last", q.sql, args, 0)
			}
			break
		}
	}
}

func metadataLivePending(t *testing.T, store *postgres.Store, q metadataPlanQuery, stage string, args []any, want bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	deadline, _ := ctx.Deadline()
	var got bool
	err := store.QueryRow(ctx, q.sql, args...).Scan(&got)
	returned := time.Now()
	cancel()
	if err != nil || returned.After(deadline) || got != want {
		t.Fatal("full pending query/absolute observation deadline", stage, got, want, err)
	}
	metadataCostExplain(t, store, stage, "full-pending", q.sql, args, 1)
}

func metadataLiveCostCardinalities(t *testing.T, store *postgres.Store, liveLeases, liveWork, liveGrants int) {
	t.Helper()
	var transfers, attempts, cleanup, grants, foreignTransfers, foreignGrants, leases, work, active, reserved int
	err := store.QueryRow(contextFor(t), `SELECT
 (SELECT count(*) FROM agenteam_object.object_transfers),
 (SELECT count(*) FROM agenteam_object.upload_attempts),
 (SELECT count(*) FROM agenteam_object.cleanup_operations),
 (SELECT count(*) FROM agenteam_download.grants),
 (SELECT count(*) FROM agenteam_object.object_transfers WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_download.grants WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_object.object_leases WHERE state='active'),
 (SELECT count(*) FROM agenteam_object.project_work WHERE joined_at IS NULL),
 (SELECT count(*) FROM agenteam_download.grants WHERE NOT revoked),
 (SELECT count(*) FROM agenteam_object.uploads WHERE disposition='reserved')`, metadataCostID(0x01910000, 2)).Scan(&transfers, &attempts, &cleanup, &grants, &foreignTransfers, &foreignGrants, &leases, &work, &active, &reserved)
	wantReserved := 0
	if liveLeases == 68 {
		wantReserved = 33
	}
	if err != nil || transfers != 1132 || attempts != 4217 || cleanup != 4182 || grants != 11035 || foreignTransfers != 1001 || foreignGrants != 10001 || leases != liveLeases || work != liveWork || active != liveGrants || reserved != wantReserved {
		t.Fatal("SQL live/history comparison cardinalities", transfers, attempts, cleanup, grants, foreignTransfers, foreignGrants, leases, work, active, reserved, err)
	}
}

const metadataCostRetirePUTShapeSQL = `
BEGIN;
UPDATE agenteam_object.uploads SET disposition='revoked',state='failed' WHERE id BETWEEN '01c10000-0000-7000-8000-000000000001' AND '01c10000-0000-7000-8000-000000000021';
DELETE FROM agenteam_object.object_references WHERE upload_id BETWEEN '01c10000-0000-7000-8000-000000000001' AND '01c10000-0000-7000-8000-000000000021';
UPDATE agenteam_object.objects SET state='failed',version=2 WHERE id BETWEEN '01c00000-0000-7000-8000-000000000001' AND '01c00000-0000-7000-8000-000000000021';
UPDATE agenteam_object.upload_attempts SET io_closed=true,phase='abandoned',cleanup_gate=true WHERE upload_id BETWEEN '01c10000-0000-7000-8000-000000000001' AND '01c10000-0000-7000-8000-000000000021';
UPDATE agenteam_object.object_leases SET state='released',released_at=clock_timestamp() WHERE object_id BETWEEN '01c00000-0000-7000-8000-000000000001' AND '01c00000-0000-7000-8000-000000000021' AND state='active';
UPDATE agenteam_object.object_transfers SET revoked_at=clock_timestamp(),retirement_evidence=id,retirement_kind='stopped',retirement_digest=decode(repeat('44',32),'hex'),phase='failed',version=version+1,cleanup_gate=true WHERE id BETWEEN '01c30000-0000-7000-8000-000000000001' AND '01c30000-0000-7000-8000-000000000021';
UPDATE agenteam_object.project_work SET joined_at=clock_timestamp() WHERE object_id BETWEEN '01c00000-0000-7000-8000-000000000001' AND '01c00000-0000-7000-8000-000000000021' AND joined_at IS NULL;
SET CONSTRAINTS ALL IMMEDIATE;
COMMIT;`

const metadataCostRetireGETShapeSQL = `
BEGIN;
UPDATE agenteam_object.object_transfers SET revoked_at=clock_timestamp(),retirement_evidence=id,retirement_kind='stopped',retirement_digest=decode(repeat('44',32),'hex'),phase='failed',version=version+1,cleanup_gate=true WHERE id BETWEEN '01c50000-0000-7000-8000-000000000001' AND '01c50000-0000-7000-8000-000000000021';
UPDATE agenteam_object.object_leases SET state='released',released_at=clock_timestamp() WHERE id BETWEEN '01c60000-0000-7000-8000-000000000001' AND '01c60000-0000-7000-8000-000000000021';
SET CONSTRAINTS ALL IMMEDIATE;
COMMIT;`
