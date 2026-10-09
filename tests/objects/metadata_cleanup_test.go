//go:build integration

package objects_test

import (
	"context"
	"io"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func TestObjectMetadataCleanupBoundedHistoryAndFinalTransaction(t *testing.T) {
	f, _ := newMetadataCleanupFixture(t)
	put := f.put(t, "metadata-history", "actual published Skills-owned bytes")
	object := put.Meta.ID
	// Produce durable history through actual open/read/Close, not fabricated
	// joined_at or lease state. History spans more than two metadata batches.
	for range 65 {
		r, err := f.service.ReadObject(contextFor(t), f.actor, f.owner, object, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, readErr := io.Copy(io.Discard, r)
		closeErr := r.Close()
		if readErr != nil || closeErr != nil {
			t.Fatal("actual history reader failed", readErr, closeErr)
		}
	}
	c := metadataCleanupCause(t, f, object)
	if result := metadataRelease(t, f, c, object); result.State() != foundation.Committed {
		t.Fatal("atomic cleanup gate", result.Fault())
	}
	ctx, cancel := context.WithTimeout(contextFor(t), 2*time.Second)
	cleaned, err := f.service.DeleteUnreferenced(ctx, c, object)
	cancel()
	if err != nil || cleaned.State != oc.CleanupCompleted {
		t.Fatal("physical cleanup did not complete in original budget", err)
	}
	if objectAuditCount(t, f.fixture, ac.ObjectDelete) != 1 {
		t.Fatal("native ObjectDelete Audit was not unique")
	}
	f.sql(t, `UPDATE object_fixture.metadata_cleanup SET phase='completed' WHERE object_id=$1`, object.String())
	request, err := oc.NewObjectCleanupAccess(oc.AccessRequestDetails{Operation: oc.PurgeDeletedObjectMetadataAccess, Cleanup: c, ObjectID: object})
	if err != nil {
		t.Fatal(err)
	}
	purge := func(rollback, repeat bool) (oc.ObjectMetadataPurgeResult, foundation.CommitResult) {
		t.Helper()
		plan, err := f.service.DiscoverAccess(contextFor(t), request)
		if err != nil {
			t.Fatal(err)
		}
		var out oc.ObjectMetadataPurgeResult
		result := plannedTx(f.store, f.service, contextFor(t), cause(t), plan, func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
			var err error
			out, err = f.service.PurgeDeletedObjectMetadataInTx(ctx, tx, c, object, plan, locked)
			if err != nil {
				return err
			}
			if repeat {
				_, err = f.service.PurgeDeletedObjectMetadataInTx(ctx, tx, c, object, plan, locked)
				if err == nil {
					t.Error("second purge reused one live union's 32-row budget")
					return fault(foundation.InvalidState)
				}
				return err
			}
			if out.State == oc.CleanupCompleted {
				// Only fixture parent facts are removed here. The production
				// Skills final-five-row transaction remains separate acceptance.
				e, err := f.store.InTx(tx)
				if err != nil {
					return err
				}
				tag, err := e.Exec(ctx, `DELETE FROM object_fixture.metadata_cleanup WHERE object_id=$1`, object.String())
				if err != nil {
					return err
				}
				if tag.RowsAffected() != 1 {
					return fault(foundation.InvalidState)
				}
			}
			if rollback {
				return fault(foundation.InvalidState)
			}
			return nil
		})
		return out, result
	}
	before := metadataRows(t, f.fixture, object)
	if before <= 64 {
		t.Fatal("real history did not exercise multiple batches", before)
	}
	_, result := purge(false, true)
	if result.State() != foundation.NotCommitted || metadataRows(t, f.fixture, object) != before {
		t.Fatal("second-call rejection did not roll back the original batch")
	}
	finalRollback := false
	for round := 0; round < 32; round++ {
		before = metadataRows(t, f.fixture, object)
		if before == 4 && !finalRollback {
			out, result := purge(true, false)
			if out.State != oc.CleanupCompleted || result.State() != foundation.NotCommitted || metadataRows(t, f.fixture, object) != 4 {
				t.Fatal("final anchor rollback split the last transaction", result.Fault())
			}
			var retained bool
			if err := f.store.QueryRow(contextFor(t), `SELECT EXISTS(SELECT 1 FROM object_fixture.metadata_cleanup WHERE object_id=$1)`, object.String()).Scan(&retained); err != nil || !retained {
				t.Fatal("failed final transaction lost parent mapping", err)
			}
			finalRollback = true
		}
		out, result := purge(false, false)
		if result.State() != foundation.Committed {
			t.Fatal("metadata batch did not commit", result.Fault())
		}
		after := metadataRows(t, f.fixture, object)
		if deleted := before - after; deleted < 1 || deleted > oc.ObjectMetadataPurgeBatchLimit {
			t.Fatal("actual cross-table deletion budget", deleted)
		}
		if out.State == oc.CleanupCompleted {
			if after != 0 || !finalRollback || before != 4 {
				t.Fatal("completion did not use a separate final-four-anchor transaction")
			}
			break
		}
		if after < 4 || round == 31 {
			t.Fatal("pending lost anchors or bounded progress failed", after)
		}
	}
	if objectAuditCount(t, f.fixture, ac.ObjectDelete) != 1 {
		t.Fatal("metadata purge rewrote native Audit")
	}
	if _, err := f.service.DiscoverAccess(contextFor(t), request); err == nil {
		t.Fatal("missing anchors became standalone purge permission")
	}
}
