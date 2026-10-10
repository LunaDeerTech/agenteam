//go:build integration

package objects_test

import (
	"context"
	_ "embed"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

//go:embed testdata/metadata_cleanup_pending_cost.sql
var metadataPendingCostSQL string

func TestObjectMetadataCleanupPendingHistoryAndCausePlans(t *testing.T) {
	queries := metadataPendingCaptureQueries(t)
	stop := metadataLiveCaptureStopQueries(t)["stop-full-pending"]
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
	// The real public Service supplied the queries. This different database
	// supplies only SQL-cost shapes; no marked cleanup/Audit row is a witness.
	conn := db.Connect(t)
	for _, sql := range []string{metadataProjectCostSQL, metadataTransferCostSQL, metadataPendingCostSQL} {
		if _, err := conn.Exec(contextFor(t), sql); err != nil {
			t.Fatal("pending history SQL fixture", err)
		}
	}
	object, absent := metadataCostID(0x01e00000, 1), metadataCostID(0x01e00000, 2)
	current, old := metadataCostID(0x01e20000, 67), metadataCostID(0x01e20000, 66)
	metadataPendingCostCounts(t, store)
	for _, id := range []string{object, absent} {
		q := queries["earliest-cleanup-cause"]
		var want []string
		if id == object {
			want = []string{metadataCostID(0x01e10000, 1)}
		}
		// The smaller current cleanup ID has the later ProjectDeleted cause.
		// An ID-only order returns a different operation; expected cause comes
		// from the fixture's original historical AbandonedAttempt, not output.
		metadataCostQueryIDs(t, store, q.sql, []any{id}, want)
		metadataCostExplain(t, store, "history-cause", "created-at-id", q.sql, []any{id}, len(want))
		q = queries["gate-two-pending-sets"]
		want = nil
		if id == object {
			want = []string{old}
		}
		// Both arms contain the same old attempt and must deduplicate it. The
		// 65 cleaned rows and the current gated anchor remain in the table.
		metadataCostQueryIDs(t, store, q.sql, []any{id, current}, want)
		metadataCostExplain(t, store, "pending-after-history", "gate-union", q.sql, []any{id, current}, len(want))
		metadataLivePending(t, store, queries["physical-full-pending"], "pending-after-history/object", []any{id, []string{}}, id == object)
	}
	target := metadataCostID(0x01910000, 1)
	for _, deleting := range []bool{false, true} {
		metadataLivePending(t, store, stop, "applying-original-worker", []any{target, deleting}, true)
	}
	// Controlled negative SQL snapshot: a joined record with the wrong claim
	// fence is NOT the applying cleanup's join proof. This is never presented
	// to Service as an authorized or successfully retired native operation.
	if _, err := conn.Exec(contextFor(t), `UPDATE agenteam_object.project_work SET joined_at=clock_timestamp(),cleanup_claim_fence=6 WHERE id=$1 AND cleanup_claim_fence=7`, metadataCostID(0x01e40000, 1)); err != nil {
		t.Fatal(err)
	}
	for _, deleting := range []bool{false, true} {
		metadataLivePending(t, store, stop, "wrong-fence-joined", []any{target, deleting}, true)
	}
	if _, err := conn.Exec(contextFor(t), `UPDATE agenteam_object.project_work SET cleanup_claim_fence=7 WHERE id=$1 AND cleanup_claim_fence=6`, metadataCostID(0x01e40000, 1)); err != nil {
		t.Fatal(err)
	}
	for _, deleting := range []bool{false, true} {
		metadataLivePending(t, store, stop, "exact-fence-joined", []any{target, deleting}, false)
	}
	// Exact local work completion can finish Stop's SQL predicate while the
	// original physical cleanup is still applying. It cannot complete Purge.
	metadataLivePending(t, store, queries["physical-full-pending"], "joined-but-physical-incomplete", []any{object, []string{}}, true)
	metadataPendingCostCounts(t, store)
	var phase string
	if err := store.QueryRow(contextFor(t), `SELECT phase FROM agenteam_object.cleanup_operations WHERE id=$1`, metadataCostID(0x01e30000, 66)).Scan(&phase); err != nil || phase != "applying" {
		t.Fatal("SQL join probe changed original cleanup checkpoint", phase, err)
	}
}

// Extend the existing observer for one actual finalization SELECT. All SQL
// still delegates to the original Store/Tx; no row or permission is replaced.
type metadataCausePlanStore struct{ *metadataPlanStore }
type metadataCausePlanExecutor struct {
	postgres.SQLExecutor
	owner *metadataCausePlanStore
}

func (s *metadataCausePlanStore) InTx(tx foundation.Tx) (postgres.SQLExecutor, error) {
	e, err := s.metadataPlanStore.InTx(tx)
	if err != nil {
		return nil, err
	}
	return &metadataCausePlanExecutor{SQLExecutor: e, owner: s}, nil
}

func (e *metadataCausePlanExecutor) QueryRow(ctx context.Context, sql string, args ...any) postgres.Row {
	if strings.HasPrefix(sql, "SELECT operation_id::text FROM agenteam_object.cleanup_operations WHERE object_id=$1 ORDER BY created_at,id LIMIT 1") {
		e.owner.mu.Lock()
		if e.owner.queries == nil {
			e.owner.queries = make(map[string]metadataPlanQuery)
		}
		if _, exists := e.owner.queries["earliest-cleanup-cause"]; !exists {
			e.owner.queries["earliest-cleanup-cause"] = metadataPlanQuery{sql: sql, args: append([]any(nil), args...)}
		}
		e.owner.mu.Unlock()
	}
	return e.SQLExecutor.QueryRow(ctx, sql, args...)
}

func metadataPendingCaptureQueries(t *testing.T) map[string]metadataPlanQuery {
	t.Helper()
	base := newFixture(t, false)
	observed := &metadataCausePlanStore{metadataPlanStore: &metadataPlanStore{Store: base.store}}
	f, _ := metadataCleanupFixtureOn(t, base, objectAuditOptions{store: observed})
	put := f.put(t, "pending-cost-cause-capture", "actual bytes")
	stopDeadline := time.Now().Add(3 * time.Second)
	cause := metadataCleanupCause(t, f, put.Meta.ID)
	if time.Now().After(stopDeadline) {
		t.Fatal("real query-source Stop exceeded its original convergence budget")
	}
	if result := metadataRelease(t, f, cause, put.Meta.ID); result.State() != foundation.Committed {
		t.Fatal(result.Fault())
	}
	ctx, cancel := context.WithTimeout(contextFor(t), 2*time.Second)
	deadline, _ := ctx.Deadline()
	result, err := f.service.DeleteUnreferenced(ctx, cause, put.Meta.ID)
	returned := time.Now()
	cancel()
	if err != nil || returned.After(deadline) || result.State != oc.CleanupCompleted {
		t.Fatal("real physical finalization query source", err)
	}
	out := make(map[string]metadataPlanQuery)
	observed.mu.Lock()
	for _, name := range []string{"earliest-cleanup-cause", "gate-two-pending-sets", "physical-full-pending"} {
		out[name] = observed.queries[name]
	}
	observed.mu.Unlock()
	for name, q := range out {
		if q.sql == "" || len(q.args) == 0 || q.args[0] != put.Meta.ID.String() {
			t.Fatal("actual query-source mapping was not observed", name)
		}
	}
	return out
}

func metadataPendingCostCounts(t *testing.T, store *postgres.Store) {
	t.Helper()
	var attempts, cleanup, cleaned, nonterminal, currentGated, workers, transfers, foreignWork int
	err := store.QueryRow(contextFor(t), `SELECT
 (SELECT count(*) FROM agenteam_object.upload_attempts),
 (SELECT count(*) FROM agenteam_object.cleanup_operations),
 (SELECT count(*) FROM agenteam_object.upload_attempts WHERE object_id=$1 AND phase='cleaned'),
 (SELECT count(*) FROM agenteam_object.upload_attempts WHERE object_id=$1 AND phase NOT IN ('published','cleaned')),
 (SELECT count(*) FROM agenteam_object.upload_attempts a JOIN agenteam_object.uploads u ON u.current_attempt_id=a.id WHERE a.object_id=$1 AND a.phase='abandoned' AND a.cleanup_gate AND a.io_closed),
 (SELECT count(*) FROM agenteam_object.project_work WHERE object_id=$1 AND kind='cleanup'),
 (SELECT count(*) FROM agenteam_object.object_transfers),
 (SELECT count(*) FROM agenteam_object.project_work WHERE project_id=$2)`, metadataCostID(0x01e00000, 1), metadataCostID(0x01910000, 2)).Scan(&attempts, &cleanup, &cleaned, &nonterminal, &currentGated, &workers, &transfers, &foreignWork)
	if err != nil || attempts != 4250 || cleanup != 4249 || cleaned != 65 || nonterminal != 2 || currentGated != 1 || workers != 1 || transfers != 1066 || foreignWork != 11002 {
		t.Fatal("pending/cause plans lost original historical workload", attempts, cleanup, cleaned, nonterminal, currentGated, workers, transfers, foreignWork, err)
	}
}
