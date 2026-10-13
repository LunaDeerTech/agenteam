package object

import (
	"context"
	"errors"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// Exercise the private Knowledge gate with the original live Store transaction
// and a controlled exact cleanup checker. Real Project issuance remains PG.
func TestKnowledgeObjectCleanupAuthorization(t *testing.T) {
	for _, mode := range []string{"allow", "denied", "wrong-object"} {
		t.Run(mode, func(t *testing.T) {
			v := newKnowledgeSourcePure(t)
			d := v.h.source.Details()
			cause, err := oc.NewObjectCleanupCause(oc.CleanupDetails{
				OperationID: auditID[oc.CleanupOperation](t), Owner: d.Owner, Reason: oc.ProjectDeleted,
			})
			if err != nil {
				t.Fatal(err)
			}
			call := &knowledgeObjectCleanupCall{owner: v.reads.data(), cause: cause, object: d.Meta.ID, active: true}
			ctx := context.WithValue(context.Background(), knowledgeObjectCleanupKey{}, call)
			locks, err := call.requiredLocks()
			if err != nil {
				t.Fatal(err)
			}
			var original f.Tx
			calls := 0
			denied := errors.New("current Knowledge cleanup claim denied")
			v.service.state().auth.Cleanup = boundedCleanupAuthority{check: func(got context.Context, tx f.Tx, gotCause oc.ObjectCleanupCause, object oc.ObjectID) error {
				calls++
				if got != ctx || tx != original || object != d.Meta.ID || gotCause.Details().OperationID != cause.Details().OperationID || !gotCause.Details().Owner.Equal(cause.Details().Owner) || gotCause.Details().Reason != cause.Details().Reason {
					t.Error("cleanup checker received a different original call tuple")
					return failure(f.Forbidden, nil)
				}
				if err := v.store.RequireHeldLocks(got, tx, locks); err != nil {
					t.Error("cleanup checker lost the live original transaction or Project EX", err)
					return err
				}
				if mode == "denied" {
					return f.NewFault(f.Forbidden, f.NotCommitted).WithCause(denied)
				}
				return nil
			}}
			object := d.Meta.ID
			if mode == "wrong-object" {
				object = auditID[oc.StoredObject](t)
			}
			var authorization error
			result := v.store.WithinTx(ctx, recoveryCause(), func(run context.Context, tx f.Tx) error {
				original = tx
				if err := v.store.AcquireAll(run, tx, locks); err != nil {
					return err
				}
				active := v.service.knowledgeObjectCleanup(run)
				if active != call {
					return errors.New("original private Knowledge cleanup call missing")
				}
				authorization = v.service.requireKnowledgeObjectCleanup(run, tx, active, object)
				return authorization
			})
			switch mode {
			case "allow":
				if authorization != nil || result.State() != f.Committed || calls != 1 {
					t.Fatal("successful exact cleanup checker was converted into a failure", authorization, result.Fault(), calls)
				}
			case "denied":
				if !errors.Is(authorization, denied) || !hasCode(authorization, f.Forbidden) || result.State() != f.NotCommitted || calls != 1 {
					t.Fatal("original checker rejection or its cause was lost", authorization, calls)
				}
			case "wrong-object":
				if !hasCode(authorization, f.Forbidden) || result.State() != f.NotCommitted || calls != 0 {
					t.Fatal("another object borrowed the original cleanup call", authorization, calls)
				}
			}
		})
	}
}
