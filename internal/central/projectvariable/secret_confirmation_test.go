package projectvariable

import (
	"context"
	"errors"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// Only the confirmation owner's decisions are under test. This controlled
// D04 port is not a real prepared capability, authorization or physical COMMIT.
type secretConfirmationPrepared struct {
	sc.PreparedProjectVariableWrite
	locks []f.LockRequest
	err   error
}

func (p *secretConfirmationPrepared) RequiredLocks() ([]f.LockRequest, error) {
	return append([]f.LockRequest(nil), p.locks...), p.err
}

type secretConfirmationD04 struct {
	sc.ProjectVariableWrites
	store       *secretAuthorityStore
	prepared    *secretConfirmationPrepared
	observation sc.ProjectVariableWriteObservation
	err         error
	calls       int
	held        func(context.Context)
}

func (d *secretConfirmationD04) MatchProjectVariableIntentInTx(ctx context.Context, tx f.Tx, p sc.PreparedProjectVariableWrite) (sc.ProjectVariableWriteObservation, error) {
	d.calls++
	if p != d.prepared {
		return sc.ProjectVariableWriteObservation{}, errors.New("original intent handle replaced")
	}
	if err := d.store.RequireHeldLocks(ctx, tx, d.prepared.locks); err != nil {
		return sc.ProjectVariableWriteObservation{}, err
	}
	if d.held != nil {
		d.held(ctx)
	}
	if err := ctx.Err(); err != nil {
		return sc.ProjectVariableWriteObservation{}, err
	}
	return d.observation, d.err
}

func secretConfirmationFixture(t *testing.T) (*SecretService, *secretAuthorityStore, *secretConfirmationD04, sc.ProjectVariableWriteRequest, f.CommitResult) {
	t.Helper()
	r := secretStoredRecord(t, c.SecretCreateCommand, true, true)
	request := secretAuthorityRequest(t, r, 30)
	o, _ := r.Observation.Result()
	locks, err := sc.ProjectVariableWriteLocks(request, o.Ref)
	if err != nil {
		t.Fatal(err)
	}
	store := &secretAuthorityStore{record: r}
	d := &secretConfirmationD04{store: store, prepared: &secretConfirmationPrepared{locks: locks}, observation: r.Observation}
	st := &secretServiceState{store: store, deps: SecretDependencies{Secrets: d}, calls: map[*call]struct{}{}, changed: make(chan struct{})}
	unknown := f.UnknownResult(testID[f.TransactionAttempt](80), commandCause(request.Fields().Identity))
	return &SecretService{data: func() *secretServiceState { return st }}, store, d, request, unknown
}

func requireOriginalSecretUnknown(t *testing.T, err error, original f.CommitResult) {
	t.Helper()
	var problem *f.Fault
	var saved commitFailure
	if !errors.As(err, &problem) || problem.Code != f.CommitUnknown || problem.CommitState != f.Unknown || problem.CauseID != original.AttemptID().String() || problem.RetryHint != "lookup" ||
		!errors.As(err, &saved) || saved.result.AttemptID() != original.AttemptID() || !sameConfirmationCause(saved.result.Cause(), original.Cause()) {
		t.Fatal("original physical outcome replaced", err)
	}
}

func sameConfirmationCause(left, right f.TransactionCause) bool {
	l, r := left.Details(), right.Details()
	if left.Validate() != nil || right.Validate() != nil || left.Kind() != f.CommandsCause || right.Kind() != f.CommandsCause || l.Primary.Canonical() != r.Primary.Canonical() || len(l.Related) != len(r.Related) {
		return false
	}
	for n := range l.Related {
		if l.Related[n].Canonical() != r.Related[n].Canonical() {
			return false
		}
	}
	return true
}

func TestSecretOwnerConfirmationUsesOriginalIntentAndPreservesUnknown(t *testing.T) {
	for _, name := range []string{"complete", "different-intent", "not-observed", "missing-d10", "different-d04", "confirmation-unknown", "locks-unavailable", "caller-canceled"} {
		t.Run(name, func(t *testing.T) {
			s, store, d, request, original := secretConfirmationFixture(t)
			want := store.record.Receipt
			ctx, entry, done, err := s.begin(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer done()
			switch name {
			case "different-intent":
				d.err = fault(f.IdempotencyKeyReused)
			case "not-observed":
				d.observation = sc.ProjectVariableWriteNotObserved()
			case "missing-d10":
				store.record = nil
			case "different-d04":
				o, _ := d.observation.Result()
				o.ReceiptID = testID[sc.ProjectVariableReceipt](81)
				d.observation, err = sc.NewProjectVariableWriteObservation(o)
				if err != nil {
					t.Fatal(err)
				}
			case "confirmation-unknown":
				store.unknown = true
			case "locks-unavailable":
				d.prepared.err = errors.New("retired prepared")
			case "caller-canceled":
				entry.cancel()
			}
			deadlineObserved := false
			d.held = func(ctx context.Context) {
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > 3*time.Second {
					t.Error("confirmation budget changed")
				}
				deadlineObserved = true
			}
			got, err := s.confirmSecretUnknown(ctx, entry, request, d.prepared, original)
			if name == "complete" || name == "caller-canceled" {
				if err != nil || !sameValue(got, want) {
					t.Fatal("complete confirmation failed", err)
				}
			} else {
				requireOriginalSecretUnknown(t, err, original)
				if got.Validate() == nil {
					t.Fatal("failed confirmation leaked result")
				}
			}
			wantCalls := 1
			if name == "locks-unavailable" {
				wantCalls = 0
			}
			if store.transactions != wantCalls || d.calls != wantCalls || deadlineObserved != (wantCalls == 1) {
				t.Fatal("confirmation retried or bypassed original Match")
			}
			if len(entry.confirmations) != 0 {
				t.Fatal("confirmation cancellation hook not retired")
			}
		})
	}
}

func TestSecretOwnerStopCancelsConfirmationButDrainWaitsForReturn(t *testing.T) {
	s, _, d, request, original := secretConfirmationFixture(t)
	ctx, entry, done, err := s.begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan context.Context, 1)
	release := make(chan struct{})
	returned := make(chan error, 1)
	d.held = func(ctx context.Context) { entered <- ctx; <-release }
	go func() {
		defer done()
		_, err := s.confirmSecretUnknown(ctx, entry, request, d.prepared, original)
		returned <- err
	}()
	var confirmation context.Context
	select {
	case confirmation = <-entered:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("confirmation never entered")
	}
	s.Stop()
	if confirmation.Err() != context.Canceled || ctx.Err() != context.Canceled {
		t.Error("Stop missed confirmation")
	}
	bounded, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	if err = s.Drain(bounded); err != context.DeadlineExceeded {
		t.Error("Drain confused cancellation with returned callback", err)
	}
	cancel()
	close(release)
	select {
	case err = <-returned:
		requireOriginalSecretUnknown(t, err, original)
	case <-time.After(time.Second):
		t.Fatal("confirmation did not return")
	}
	joined, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err = s.Drain(joined); err != nil {
		t.Fatal("actual confirmation not joined", err)
	}
	if _, _, _, err = s.begin(context.Background()); err == nil {
		t.Fatal("admission reopened")
	}
}
