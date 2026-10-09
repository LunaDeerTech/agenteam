package work

import (
	"context"
	"errors"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"testing"
)

func TestTaskBlockerUnknownConfirmationCannotReplaceWriterProvenance(t *testing.T) {
	a := pureActor(t, 2)
	p := pureID[i.Project](t, 5)
	id, err := c.TaskBlockerCommandIdentity(p, c.TaskBlockerCommandResolve, "private-task-key")
	if err != nil {
		t.Fatal(err)
	}
	cause, err := f.NewCommandsCause(id)
	if err != nil {
		t.Fatal(err)
	}
	original := f.UnknownResult(pureID[f.TransactionAttempt](t, 90), cause)
	for _, denied := range []f.CommitResult{f.NotCommittedResult(f.NewFault(f.SessionRevoked, f.NotCommitted)), f.UnknownResult(pureID[f.TransactionAttempt](t, 91), cause)} {
		store := &confirmationFailureStore{result: denied}
		st := &blockerServiceState{store: store, calls: map[*call]struct{}{}, changed: make(chan struct{})}
		service := &BlockerService{data: func() *blockerServiceState { return st }}
		ctx, entry, done, err := service.begin(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		_, err = service.confirmBlockerUnknown(ctx, entry, a, c.TaskBlockerCommandLookupRequest{ProjectID: p, Command: c.TaskBlockerCommandResolve, IdempotencyKey: "private-task-key", SemanticDigest: f.Digest("sha256:0000000000000000000000000000000000000000000000000000000000000000")}, original)
		done()
		var public *f.Fault
		var private commitFailure
		if !errors.As(err, &public) || !errors.As(err, &private) || public.Code != f.CommitUnknown || public.CommitState != f.Unknown || public.CauseID != original.AttemptID().String() || private.result.AttemptID() != original.AttemptID() || private.result.Cause().Details().Primary.Canonical() != cause.Details().Primary.Canonical() {
			t.Fatal("confirmation replaced original writer", err)
		}
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := taskTxError(cancelled, f.CommittedResult()); err != nil {
		t.Fatal("delivery cancellation overturned commit", err)
	}
	if err := taskTxError(cancelled, f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted))); !errors.Is(err, context.Canceled) {
		t.Fatal("known rollback lost cancellation", err)
	}
	if err := taskTxError(cancelled, original); err == nil {
		t.Fatal("unknown cancellation became success")
	}
}
