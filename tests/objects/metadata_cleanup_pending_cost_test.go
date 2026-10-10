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
	"github.com/jackc/pgx/v5"
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
	metadataPendingTailPlans(t, store, conn, queries["physical-full-pending"])
}

func metadataPendingTailPlans(t *testing.T, store *postgres.Store, conn *pgx.Conn, query metadataPlanQuery) {
	t.Helper()
	// A separate, explicitly SQL-only comparison snapshot advances the native
	// checkpoints without deleting any history. Completed cleanup can precede
	// the original call's work join (cleanup.go checkpoint/finalize); likewise
	// ConfirmStopped may persist retirement and release a GET lease without
	// revoking the transfer (transfer_recovery.go). Keep Object Available and
	// cleaning throughout: neither snapshot asserts Project Stop completion,
	// Object Deleted/Audit, a private witness, or a successful Purge.
	if _, err := conn.Exec(contextFor(t), metadataPendingTailCostSQL); err != nil {
		t.Fatal("pending tail SQL comparison snapshots", err)
	}
	object, worker, transfer := metadataCostID(0x01e00000, 1), metadataCostID(0x01e90000, 1), metadataCostID(0x01e60000, 66)
	args := []any{object, []string{}}
	plan := metadataLivePending(t, store, query, "completed-checkpoint-unjoined-tail", args, true)
	if !metadataCostRelationExecuted(plan, "project_work", true) {
		t.Fatal("pending work branch was planned but never executed with its original tail")
	}
	tag, err := store.Exec(contextFor(t), `UPDATE agenteam_object.project_work SET joined_at=clock_timestamp() WHERE id=$1 AND joined_at IS NULL`, worker)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatal("SQL tail comparison did not join exactly the original cleanup worker", err)
	}
	plan = metadataLivePending(t, store, query, "joined-retired-get-not-revoked", args, true)
	if !metadataCostRelationExecuted(plan, "project_work", false) || !metadataCostRelationExecuted(plan, "object_transfers", true) {
		t.Fatal("nonempty transfer tail remained hidden behind an earlier predicate")
	}
	tag, err = store.Exec(contextFor(t), `UPDATE agenteam_object.object_transfers SET revoked_at=clock_timestamp(),cleanup_gate=true,version=version+1 WHERE id=$1 AND revoked_at IS NULL AND retirement_evidence IS NOT NULL`, transfer)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatal("SQL comparison did not revoke exactly the already retired GET", err)
	}
	plan = metadataLivePending(t, store, query, "all-tail-predicates-empty-with-history", args, false)
	if !metadataCostRelationExecuted(plan, "project_work", false) || !metadataCostRelationExecuted(plan, "object_transfers", false) {
		t.Fatal("empty work/transfer ranges did not actually execute after history")
	}
	var attempts, cleaned, cleanup, work, joined, transfers, retired, foreignTransfers, foreignWork int
	var available bool
	err = store.QueryRow(contextFor(t), `SELECT
 (SELECT count(*) FROM agenteam_object.upload_attempts WHERE object_id=$1),
 (SELECT count(*) FROM agenteam_object.upload_attempts WHERE object_id=$1 AND phase='cleaned'),
 (SELECT count(*) FROM agenteam_object.cleanup_operations WHERE object_id=$1 AND phase='completed'),
 (SELECT count(*) FROM agenteam_object.project_work WHERE object_id=$1),
 (SELECT count(*) FROM agenteam_object.project_work WHERE object_id=$1 AND joined_at IS NOT NULL),
 (SELECT count(*) FROM agenteam_object.object_transfers WHERE object_id=$1),
 (SELECT count(*) FROM agenteam_object.object_transfers t JOIN agenteam_object.object_leases l ON l.id=t.lease_id WHERE t.object_id=$1 AND t.revoked_at IS NOT NULL AND t.retirement_evidence IS NOT NULL AND l.state='released'),
 (SELECT count(*) FROM agenteam_object.object_transfers WHERE project_id=$2),
 (SELECT count(*) FROM agenteam_object.project_work WHERE project_id=$2),
 EXISTS(SELECT 1 FROM agenteam_object.objects WHERE id=$1 AND state='available' AND cleaning)`, object, metadataCostID(0x01910000, 2)).Scan(&attempts, &cleaned, &cleanup, &work, &joined, &transfers, &retired, &foreignTransfers, &foreignWork, &available)
	if err != nil || attempts != 67 || cleaned != 67 || cleanup != 67 || work != 68 || joined != 68 || transfers != 66 || retired != 66 || foreignTransfers != 1001 || foreignWork != 11002 || !available {
		t.Fatal("pending tail cost lost historical rows or invented Object completion", attempts, cleaned, cleanup, work, joined, transfers, retired, foreignTransfers, foreignWork, available, err)
	}
}

// No marked retirement below is given to Service. These are SQL cost shapes
// of 65 retired/revoked GETs and one retirement-confirmed but not-yet-revoked
// GET, each with its original released external lease. This does not separate
// retirement proof and lease release, or create live transfers over Deleted.
const metadataPendingTailCostSQL = `
BEGIN;
UPDATE agenteam_object.upload_attempts SET phase='cleaned' WHERE object_id='01e00000-0000-7000-8000-000000000001' AND phase='abandoned' AND cleanup_gate AND io_closed;
UPDATE agenteam_object.cleanup_operations SET phase='completed' WHERE object_id='01e00000-0000-7000-8000-000000000001' AND phase IN ('gated','applying');
UPDATE agenteam_object.cleanup_operations SET worker_id='01e90000-0000-7000-8000-000000000001',fence=1 WHERE id='01e30000-0000-7000-8000-000000000000';
INSERT INTO agenteam_object.project_work(id,project_id,process_id,kind,resource_id,object_id,admission_version,cleanup_claim_fence)
VALUES('01e90000-0000-7000-8000-000000000001','01910000-0000-7000-8000-000000000001','01910000-0000-7000-8000-000000000005','cleanup','01e30000-0000-7000-8000-000000000000','01e00000-0000-7000-8000-000000000001',0,1);
INSERT INTO agenteam_object.object_leases(id,object_id,owner_kind,owner_id,state,released_at)
SELECT ('01e70000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,'01e00000-0000-7000-8000-000000000001','transfer',('01e60000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,'released',clock_timestamp() FROM generate_series(1,66) n;
INSERT INTO agenteam_object.object_transfers(id,issue_hash,issue_key,issue_request_id,semantic_digest,actor_json,stable_actor,project_id,owner_kind,owner_id,runner_id,operation_id,runner_generation,operation_version,execution_id,direction,object_id,lease_id,media_type,byte_size,sha256,candidate_key,duration_seconds,expires_at,phase,version,revoked_at,completed_evidence,completed_digest,retirement_evidence,retirement_kind,retirement_digest,cleanup_gate)
SELECT ('01e60000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,decode('06'||lpad(to_hex(n),62,'0'),'hex'),'pending-tail-get-'||n,('01e60000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,decode(repeat('22',32),'hex'),
 '{"Kind":"human","UserID":"01910000-0000-7000-8000-000000000004","SessionID":"01910000-0000-7000-8000-000000000006"}'::jsonb,
 '{"Agent":"","Cause":"","Execution":"","Project":"","Service":"","User":"01910000-0000-7000-8000-000000000004","kind":"human"}',
 '01910000-0000-7000-8000-000000000001','skill_revision','01e00000-0000-7000-8000-000000000001','01910000-0000-7000-8000-000000000007','01910000-0000-7000-8000-000000000008',1,1,'01910000-0000-7000-8000-000000000009','get','01e00000-0000-7000-8000-000000000001',('01e70000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,'text/plain',4,decode(repeat('00',32),'hex'),'candidate/01e20000-0000-7000-8000-000000000043',60,'2026-01-01T00:01:00Z','complete',2,CASE WHEN n<=65 THEN '2026-01-01T00:02:00Z'::timestamptz END,('01e60000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,decode(repeat('33',32),'hex'),('01e60000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,'completed',decode(repeat('33',32),'hex'),n<=65 FROM generate_series(1,66) n;
INSERT INTO agenteam_object.project_work(id,project_id,process_id,kind,resource_id,object_id,admission_version,joined_at)
SELECT ('01e80000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,'01910000-0000-7000-8000-000000000001','01910000-0000-7000-8000-000000000005','transfer_get',('01e60000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,'01e00000-0000-7000-8000-000000000001',0,clock_timestamp() FROM generate_series(1,66) n;
SET CONSTRAINTS ALL IMMEDIATE;
COMMIT;
ANALYZE agenteam_object.upload_attempts;
ANALYZE agenteam_object.cleanup_operations;
ANALYZE agenteam_object.project_work;
ANALYZE agenteam_object.object_leases;
ANALYZE agenteam_object.object_transfers;`

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
