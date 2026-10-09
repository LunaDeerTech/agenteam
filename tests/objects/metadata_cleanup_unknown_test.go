//go:build integration

package objects_test

import (
	"context"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func TestObjectMetadataCleanupFinalCommitUnknown(t *testing.T) {
	for _, after := range []bool{false, true} {
		name := "commit-not-forwarded"
		if after {
			name = "commit-response-lost"
		}
		t.Run(name, func(t *testing.T) {
			base := newFixture(t, false)
			raw, proxy := proxyStore(t, base, after)
			f, _ := metadataCleanupFixtureOn(t, base, objectAuditOptions{pg: raw})
			put := f.put(t, "metadata-unknown", "real canonical bytes")
			object := put.Meta.ID
			cleanup := metadataCleanupCause(t, f, object)
			if result := metadataRelease(t, f, cleanup, object); result.State() != foundation.Committed {
				t.Fatal(result.Fault())
			}
			ctx, cancel := context.WithTimeout(contextFor(t), 2*time.Second)
			out, err := f.service.DeleteUnreferenced(ctx, cleanup, object)
			cancel()
			if err != nil || out.State != oc.CleanupCompleted {
				t.Fatal("physical prerequisite", err)
			}
			f.sql(t, `UPDATE object_fixture.metadata_cleanup SET phase='completed' WHERE object_id=$1`, object.String())
			request, err := oc.NewObjectCleanupAccess(oc.AccessRequestDetails{Operation: oc.PurgeDeletedObjectMetadataAccess, Cleanup: cleanup, ObjectID: object})
			if err != nil {
				t.Fatal(err)
			}
			// Confirm historical progress normally first. Only the exact final
			// transaction's real COMMIT frame is selected below.
			for round := 0; metadataRows(t, base, object) > 4; round++ {
				if round == 8 {
					t.Fatal("history did not reach final anchors")
				}
				plan, err := f.service.DiscoverAccess(contextFor(t), request)
				if err != nil {
					t.Fatal(err)
				}
				r := plannedTx(raw, f.service, contextFor(t), cause(t), plan, func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
					batch, err := f.service.PurgeDeletedObjectMetadataInTx(ctx, tx, cleanup, object, plan, locked)
					if err == nil && batch.State != oc.CleanupPending {
						return fault(foundation.InvalidState)
					}
					return err
				})
				if r.State() != foundation.Committed {
					t.Fatal(r.Fault())
				}
			}
			if metadataRows(t, base, object) != 4 {
				t.Fatal("missing original anchors before final transaction")
			}
			plan, err := f.service.DiscoverAccess(contextFor(t), request)
			if err != nil {
				t.Fatal(err)
			}
			attempt := cause(t)
			callCtx, stop := context.WithTimeout(contextFor(t), 2*time.Second)
			defer stop()
			finished := make(chan struct{})
			var result foundation.CommitResult
			go func() {
				defer close(finished)
				result = plannedTx(raw, f.service, callCtx, attempt, plan, func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
					batch, err := f.service.PurgeDeletedObjectMetadataInTx(ctx, tx, cleanup, object, plan, locked)
					if err != nil {
						return err
					}
					if batch.State != oc.CleanupCompleted || batch.OperationID != cleanup.Details().OperationID {
						return fault(foundation.InvalidState)
					}
					e, err := raw.InTx(tx)
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
					// No background Runtime or other call uses this isolated
					// proxy. Arm only after the exact final native/parent writes.
					proxy.armed.Store(1)
					return nil
				})
			}()
			defer func() {
				stop()
				proxy.Close()
				select {
				case <-finished:
				case <-time.After(3 * time.Second):
					t.Error("final transaction did not actually return")
				}
			}()
			select {
			case <-proxy.reached:
				close(proxy.release)
			case <-finished:
				t.Fatal("final transaction did not reach actual selected COMMIT", result.Fault())
			case <-callCtx.Done():
				t.Fatal("selected COMMIT not observed")
			}
			select {
			case <-finished:
			case <-time.After(3 * time.Second):
				t.Fatal("selected transaction did not return")
			}
			if result.State() != foundation.Unknown || result.AttemptID().Validate() != nil || result.Cause().Validate() != nil {
				t.Fatal("lost COMMIT response was not Unknown", result.Fault())
			}
			gotCause, originalCause := result.Cause().Details(), attempt.Details()
			if gotCause.Kind != originalCause.Kind || gotCause.Owner != originalCause.Owner || gotCause.RecoveryRunID != originalCause.RecoveryRunID || gotCause.CheckpointRef != originalCause.CheckpointRef {
				t.Fatal("Unknown lost the original typed recovery cause")
			}
			// This independent observer is the original direct Store, never
			// the connection whose COMMIT outcome was withheld.
			remaining := metadataRows(t, base, object)
			var parent bool
			if err := base.store.QueryRow(contextFor(t), `SELECT EXISTS(SELECT 1 FROM object_fixture.metadata_cleanup WHERE object_id=$1)`, object.String()).Scan(&parent); err != nil {
				t.Fatal(err)
			}
			if after {
				select {
				case <-proxy.committed:
				default:
					t.Fatal("no actual server COMMIT completion")
				}
				if remaining != 0 || parent {
					t.Fatal("committed final transaction split native and parent anchors")
				}
				if _, err := f.service.DiscoverAccess(contextFor(t), request); err == nil {
					t.Fatal("Unknown all-empty became standalone metadata permission")
				}
			} else {
				if remaining != 4 || !parent {
					t.Fatal("unforwarded COMMIT partially removed anchors")
				}
				// Original cause and freshly discovered union can resume. The
				// completed branch never issues fresh physical cleanup/Audit.
				plan, err := f.service.DiscoverAccess(contextFor(t), request)
				if err != nil {
					t.Fatal(err)
				}
				r := plannedTx(raw, f.service, contextFor(t), attempt, plan, func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
					batch, err := f.service.PurgeDeletedObjectMetadataInTx(ctx, tx, cleanup, object, plan, locked)
					if err != nil {
						return err
					}
					if batch.State != oc.CleanupCompleted {
						return fault(foundation.InvalidState)
					}
					e, err := raw.InTx(tx)
					if err != nil {
						return err
					}
					_, err = e.Exec(ctx, `DELETE FROM object_fixture.metadata_cleanup WHERE object_id=$1`, object.String())
					return err
				})
				if r.State() != foundation.Committed || metadataRows(t, base, object) != 0 {
					t.Fatal("original final cause did not recover", r.Fault())
				}
			}
			if objectAuditCount(t, base, ac.ObjectDelete) != 1 {
				t.Fatal("Unknown metadata recovery repeated native Audit")
			}
		})
	}
}
