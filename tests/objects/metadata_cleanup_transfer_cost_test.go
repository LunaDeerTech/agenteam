//go:build integration

package objects_test

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

//go:embed testdata/metadata_cleanup_transfer_cost.sql
var metadataTransferCostSQL string

func TestObjectMetadataCleanupTransferAndForeignKeyPlans(t *testing.T) {
	queries := metadataCostCaptureObjectQueries(t)
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
	conn := db.Connect(t)
	for _, sql := range []string{metadataProjectCostSQL, metadataTransferCostSQL} {
		if _, err := conn.Exec(contextFor(t), sql); err != nil {
			t.Fatal("isolated SQL cost package/FK shape", err)
		}
	}
	metadataTransferCostCardinalities(t, store)
	// The external lease cannot disappear while its original transfer still
	// points to it. Force the actual deferred check, then retire this failed
	// cost transaction by Rollback before any positive observation.
	negative, err := conn.Begin(contextFor(t))
	if err != nil {
		t.Fatal(err)
	}
	negCtx, negCancel := context.WithTimeout(context.Background(), 2*time.Second)
	_, negErr := negative.Exec(negCtx, `DELETE FROM agenteam_object.object_leases WHERE id=$1`, metadataCostID(0x019f0000, 1))
	if negErr == nil {
		_, negErr = negative.Exec(negCtx, "SET CONSTRAINTS ALL IMMEDIATE")
	}
	negCancel()
	rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), time.Second)
	rollbackErr := negative.Rollback(rollbackCtx)
	rollbackCancel()
	var pgErr *pgconn.PgError
	if !errors.As(negErr, &pgErr) || pgErr.Code != "23503" || rollbackErr != nil {
		t.Fatal("unclosed PUT lease FK was not rejected and actually rolled back", negErr, rollbackErr)
	}
	metadataTransferCostCardinalities(t, store)
	for _, object := range []string{metadataCostID(0x01920000, 1), metadataCostID(0x01920000, 2052)} {
		present := object == metadataCostID(0x01920000, 1)
		for _, spec := range []struct {
			name   string
			prefix int
			limit  int
		}{
			{"metadata-transfers", 0x019c0000, 32},
			{"metadata-leases", 0x01980000, 32},
			{"metadata-work", 0x01990000, 32},
			{"metadata-old-attempts", 0x01960000, 16},
			{"gate-two-pending-sets", 0, 0},
		} {
			q := queries[spec.name]
			args := []any{object}
			if spec.name == "metadata-old-attempts" || spec.name == "gate-two-pending-sets" {
				args = append(args, metadataCostID(0x01940000, 1))
			}
			var want []string
			if present {
				for n := 1; n <= spec.limit; n++ {
					want = append(want, metadataCostID(spec.prefix, n))
				}
			}
			metadataCostQueryIDs(t, store, q.sql, args, want)
			metadataCostExplain(t, store, "retired-put-history/"+object, spec.name, q.sql, args, len(want))
		}
		for _, name := range []string{"metadata-full-pending", "physical-full-pending"} {
			q := queries[name]
			args := []any{object}
			if name == "physical-full-pending" {
				args = append(args, []string{})
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			deadline, _ := ctx.Deadline()
			var pending bool
			err := store.QueryRow(ctx, q.sql, args...).Scan(&pending)
			returned := time.Now()
			cancel()
			if err != nil || returned.After(deadline) || pending {
				t.Fatal("retired SQL package was not a finite empty pending range", name, err)
			}
			metadataCostExplain(t, store, "retired-put-history/"+object, name, q.sql, args, 1)
		}
	}

	// Actual deferred FK work is observed in a rollback-only transaction. This
	// follows the metadata package's dependency order but does not call Purge
	// on fabricated physical-completion/Audit facts or claim its 32-row result.
	tx, err := conn.Begin(contextFor(t))
	if err != nil {
		t.Fatal(err)
	}
	rolledBack := false
	defer func() {
		if !rolledBack {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := tx.Rollback(ctx); err != nil {
				t.Error("PUT cost transaction rollback", err)
			}
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	for _, row := range []struct {
		table string
		id    string
	}{
		{"cleanup_operations", metadataCostID(0x01a20000, 1)},
		{"upload_attempts", metadataCostID(0x019d0000, 1)},
		{"object_transfers", metadataCostID(0x019c0000, 1)},
		{"cleanup_operations", metadataCostID(0x01970000, 1)},
		{"upload_attempts", metadataCostID(0x01960000, 1)},
		{"object_leases", metadataCostID(0x019f0000, 1)},
		{"object_leases", metadataCostID(0x01a00000, 1)},
	} {
		var existed bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM agenteam_object."+row.table+" WHERE id=$1)", row.id).Scan(&existed); err != nil || !existed {
			t.Fatal("exact FK cost parent was absent before DELETE", row.table, err)
		}
		var raw []byte
		err := tx.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) DELETE FROM agenteam_object."+row.table+" WHERE id=$1", row.id).Scan(&raw)
		if err != nil || time.Now().After(deadline) || !json.Valid(raw) {
			t.Fatal("PUT exact parent DELETE plan", row.table, err)
		}
		var remains bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM agenteam_object."+row.table+" WHERE id=$1)", row.id).Scan(&remains); err != nil || remains {
			t.Fatal("PUT target row did not disappear in original cost Tx", row.table, err)
		}
		t.Logf("D05 SQL-cost PUT parent=%s plan=%s", row.table, raw)
	}
	flush := time.Now()
	if _, err := tx.Exec(ctx, "SET CONSTRAINTS ALL IMMEDIATE"); err != nil || time.Now().After(deadline) {
		t.Fatal("PUT original deferred cycle did not close within observation budget", err)
	}
	flushed := time.Now()
	var foreign, target int
	err = tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE project_id=$1),count(*) FILTER(WHERE project_id=$2) FROM agenteam_object.object_transfers`, metadataCostID(0x01910000, 2), metadataCostID(0x01910000, 1)).Scan(&foreign, &target)
	if err != nil || foreign != 1001 || target != 64 {
		t.Fatal("FK observation removed the foreign or remaining target workload", foreign, target, err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	rolledBack = true
	if time.Now().After(deadline) {
		t.Fatal("PUT FK rollback tail exceeded original observation budget")
	}
	t.Logf("D05 SQL-cost PUT deferred_queue=%s (all original checks; not per-trigger timing)", flushed.Sub(flush))
	metadataTransferCostCardinalities(t, store)
}

func metadataTransferCostCardinalities(t *testing.T, store *postgres.Store) {
	t.Helper()
	var transfers, stages, sourceLeases, externalLeases, attempts, cleanup int
	err := store.QueryRow(contextFor(t), `SELECT
 (SELECT count(*) FROM agenteam_object.object_transfers),
 (SELECT count(*) FROM agenteam_object.upload_attempts WHERE kind='runner_staging'),
 (SELECT count(*) FROM agenteam_object.object_leases WHERE owner_kind='source'),
 (SELECT count(*) FROM agenteam_object.object_leases WHERE owner_kind='transfer'),
 (SELECT count(*) FROM agenteam_object.upload_attempts),
 (SELECT count(*) FROM agenteam_object.cleanup_operations)`).Scan(&transfers, &stages, &sourceLeases, &externalLeases, &attempts, &cleanup)
	if err != nil || transfers != 1066 || stages != 1066 || sourceLeases != 1066 || externalLeases != 1066 || attempts != 4183 || cleanup != 4182 {
		t.Fatal("complete SQL PUT packages/rollback", transfers, stages, sourceLeases, externalLeases, attempts, cleanup, err)
	}
}

func metadataCostCaptureObjectQueries(t *testing.T) map[string]metadataPlanQuery {
	t.Helper()
	base := newFixture(t, false)
	plans := &metadataPlanStore{Store: base.store}
	f, _ := metadataCleanupFixtureOn(t, base, objectAuditOptions{store: plans})
	put := f.put(t, "cost-object-query-capture", "actual bytes")
	reader, err := f.service.ReadObject(contextFor(t), f.actor, f.owner, put.Meta.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, readErr := io.Copy(io.Discard, reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil {
		t.Fatal("actual query-source reader", readErr, closeErr)
	}
	stopDeadline := time.Now().Add(3 * time.Second)
	c := metadataCleanupCause(t, f, put.Meta.ID)
	if time.Now().After(stopDeadline) {
		t.Fatal("query-source Stop setup exceeded original 3s convergence budget")
	}
	if result := metadataRelease(t, f, c, put.Meta.ID); result.State() != foundation.Committed {
		t.Fatal(result.Fault())
	}
	ctx, cancel := context.WithTimeout(contextFor(t), 2*time.Second)
	deadline, _ := ctx.Deadline()
	result, err := f.service.DeleteUnreferenced(ctx, c, put.Meta.ID)
	returned := time.Now()
	cancel()
	if err != nil || returned.After(deadline) || result.State != oc.CleanupCompleted {
		t.Fatal("real query-source physical cleanup", err)
	}
	f.sql(t, `UPDATE object_fixture.metadata_cleanup SET phase='completed' WHERE object_id=$1`, put.Meta.ID.String())
	request, err := oc.NewObjectCleanupAccess(oc.AccessRequestDetails{Operation: oc.PurgeDeletedObjectMetadataAccess, Cleanup: c, ObjectID: put.Meta.ID})
	if err != nil {
		t.Fatal(err)
	}
	completed := false
	for range 8 {
		ctx, cancel := context.WithTimeout(contextFor(t), 2*time.Second)
		deadline, _ := ctx.Deadline()
		plan, err := f.service.DiscoverAccess(ctx, request)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		var out oc.ObjectMetadataPurgeResult
		commit := plannedTx(f.store, f.service, ctx, cause(t), plan, func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
			var err error
			out, err = f.service.PurgeDeletedObjectMetadataInTx(ctx, tx, c, put.Meta.ID, plan, locked)
			if err != nil || out.State != oc.CleanupCompleted {
				return err
			}
			x, err := f.store.InTx(tx)
			if err != nil {
				return err
			}
			_, err = x.Exec(ctx, `DELETE FROM object_fixture.metadata_cleanup WHERE object_id=$1`, put.Meta.ID.String())
			return err
		})
		returned := time.Now()
		cancel()
		if returned.After(deadline) || commit.State() != foundation.Committed {
			t.Fatal("query-source metadata transaction did not return in budget", commit.Fault())
		}
		if out.State == oc.CleanupCompleted {
			completed = true
			break
		}
	}
	if !completed {
		t.Fatal("small real query-source fixture did not complete")
	}
	out := make(map[string]metadataPlanQuery)
	plans.mu.Lock()
	for _, name := range []string{"metadata-transfers", "metadata-leases", "metadata-work", "metadata-old-attempts", "gate-two-pending-sets", "physical-full-pending", "metadata-full-pending"} {
		out[name] = plans.queries[name]
	}
	plans.mu.Unlock()
	for name, q := range out {
		if q.sql == "" {
			t.Fatal("actual Object SQL was never reached", name)
		}
	}
	return out
}
