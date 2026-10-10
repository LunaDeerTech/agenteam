//go:build integration

package objects_test

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

//go:embed testdata/metadata_cleanup_anchor_cost.sql
var metadataAnchorCostSQL string

func TestObjectMetadataCleanupFinalAnchorForeignKeyPlans(t *testing.T) {
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
	// These marked SQL facts are never bound to Object/Artifact Service. Only
	// the real schema and FK query costs are under observation in this DB.
	conn := db.Connect(t)
	for _, sql := range []string{metadataProjectCostSQL, metadataTransferCostSQL, metadataAnchorCostSQL} {
		if _, err := conn.Exec(contextFor(t), sql); err != nil {
			t.Fatal("anchor SQL cost fixture", err)
		}
	}
	metadataAnchorCostCounts(t, store)
	// Original Object2 is a terminal four-anchor target with no old history.
	// Keep all other target/foreign rows, including every incoming FK table,
	// in place. Deleting a parent out of order must really fail and roll back.
	for _, row := range []struct {
		table  string
		prefix int
	}{
		{"upload_attempts", 0x01940000},
		{"uploads", 0x01930000},
		{"objects", 0x01920000},
	} {
		metadataAnchorRejectParent(t, conn, row.table, metadataCostID(row.prefix, 2))
		metadataAnchorCostCounts(t, store)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rolledBack := false
	defer func() {
		if !rolledBack {
			cleanup, end := context.WithTimeout(context.Background(), time.Second)
			defer end()
			if err := tx.Rollback(cleanup); err != nil {
				t.Error("anchor observation transaction cleanup", err)
			}
		}
	}()
	object, upload, attempt := metadataCostID(0x01920000, 2), metadataCostID(0x01930000, 2), metadataCostID(0x01940000, 2)
	// This is the production metadata_cleanup.go final pointer mutation and
	// metadataDeleteSQL's exact parent DELETE. It is a SQL-only rollback probe,
	// not a reimplementation or invocation of Purge's private authorization.
	tag, err := tx.Exec(ctx, `UPDATE agenteam_object.uploads SET current_attempt_id=NULL WHERE id=$1 AND object_id=$2 AND current_attempt_id=$3`, upload, object, attempt)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatal("anchor pointer precondition", err)
	}
	for _, row := range []struct {
		table  string
		prefix int
	}{
		{"cleanup_operations", 0x01950000},
		{"upload_attempts", 0x01940000},
		{"uploads", 0x01930000},
		{"objects", 0x01920000},
	} {
		id := metadataCostID(row.prefix, 2)
		var before bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM agenteam_object."+row.table+" WHERE id=$1)", id).Scan(&before); err != nil || !before {
			t.Fatal("parent cost target was already absent", row.table, err)
		}
		var raw []byte
		err := tx.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) DELETE FROM agenteam_object."+row.table+" WHERE id=$1", id).Scan(&raw)
		var plans []struct {
			Plan struct {
				NodeType string `json:"Node Type"`
			} `json:"Plan"`
		}
		if err != nil || time.Now().After(deadline) || json.Unmarshal(raw, &plans) != nil || len(plans) != 1 || plans[0].Plan.NodeType != "ModifyTable" {
			t.Fatal("parent DELETE was not actually observed within original deadline", row.table, err)
		}
		var after bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM agenteam_object."+row.table+" WHERE id=$1)", id).Scan(&after); err != nil || after {
			t.Fatal("observed DELETE did not remove exact parent", row.table, err)
		}
		// Preserve complete immediate-trigger timings from EXPLAIN. Deferred
		// triggers may execute only at the following real queue flush; do not
		// invent per-trigger numbers from that aggregate elapsed duration.
		t.Logf("D05 SQL-cost final anchor=%s plan=%s", row.table, raw)
	}
	flush := time.Now()
	if _, err := tx.Exec(ctx, "SET CONSTRAINTS ALL IMMEDIATE"); err != nil || time.Now().After(deadline) {
		t.Fatal("original deferred FK checks were not actually completed in budget", err)
	}
	flushed := time.Now()
	var objects, artifacts, foreignTransfers int
	err = tx.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_object.objects),
 (SELECT count(*) FROM agenteam_artifact.artifacts),
 (SELECT count(*) FROM agenteam_object.object_transfers WHERE project_id=$1)`, metadataCostID(0x01910000, 2)).Scan(&objects, &artifacts, &foreignTransfers)
	if err != nil || objects != 3051 || artifacts != 1001 || foreignTransfers != 1001 {
		t.Fatal("parent probes changed unrelated workload", objects, artifacts, foreignTransfers, err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal("anchor original transaction rollback", err)
	}
	rolledBack = true
	if time.Now().After(deadline) {
		t.Fatal("parent observation including actual rollback exceeded original two seconds")
	}
	t.Logf("D05 SQL-cost final anchors deferred_queue=%s (actual aggregate queue; not per-trigger time)", flushed.Sub(flush))
	metadataAnchorCostCounts(t, store)
}

func metadataAnchorRejectParent(t *testing.T, conn *pgx.Conn, table, id string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rolledBack := false
	defer func() {
		if !rolledBack {
			cleanup, end := context.WithTimeout(context.Background(), time.Second)
			defer end()
			if err := tx.Rollback(cleanup); err != nil {
				t.Error(err)
			}
		}
	}()
	_, rejected := tx.Exec(ctx, "DELETE FROM agenteam_object."+table+" WHERE id=$1", id)
	if rejected == nil {
		_, rejected = tx.Exec(ctx, "SET CONSTRAINTS ALL IMMEDIATE")
	}
	var pgErr *pgconn.PgError
	if !errors.As(rejected, &pgErr) || pgErr.Code != "23503" {
		t.Fatal("still-referenced parent was not rejected by an actual FK", table, rejected)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	rolledBack = true
	if time.Now().After(deadline) {
		t.Fatal("rejected parent and actual rollback exceeded original deadline")
	}
}

func metadataAnchorCostCounts(t *testing.T, store *postgres.Store) {
	t.Helper()
	var objects, uploads, attempts, cleanup, refs, artifacts, writerLeases, transfers, foreignWork, targetAnchors int
	err := store.QueryRow(contextFor(t), `SELECT
 (SELECT count(*) FROM agenteam_object.objects),
 (SELECT count(*) FROM agenteam_object.uploads),
 (SELECT count(*) FROM agenteam_object.upload_attempts),
 (SELECT count(*) FROM agenteam_object.cleanup_operations),
 (SELECT count(*) FROM agenteam_object.object_references),
 (SELECT count(*) FROM agenteam_artifact.artifacts),
 (SELECT count(*) FROM agenteam_object.object_leases WHERE owner_kind='writer' AND attempt_id IS NOT NULL),
 (SELECT count(*) FROM agenteam_object.object_transfers),
 (SELECT count(*) FROM agenteam_object.project_work WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_object.objects o JOIN agenteam_object.uploads u ON u.object_id=o.id JOIN agenteam_object.upload_attempts a ON a.id=u.current_attempt_id JOIN agenteam_object.cleanup_operations c ON c.attempt_id=a.id WHERE o.id=$2)`, metadataCostID(0x01910000, 2), metadataCostID(0x01920000, 2)).Scan(&objects, &uploads, &attempts, &cleanup, &refs, &artifacts, &writerLeases, &transfers, &foreignWork, &targetAnchors)
	if err != nil || objects != 3052 || uploads != 3052 || attempts != 5184 || cleanup != 4182 || refs != 1002 || artifacts != 1001 || writerLeases != 1001 || transfers != 1066 || foreignWork != 12003 || targetAnchors != 1 {
		t.Fatal("remaining parent/FK workload or original target rollback mismatch", objects, uploads, attempts, cleanup, refs, artifacts, writerLeases, transfers, foreignWork, targetAnchors, err)
	}
}
