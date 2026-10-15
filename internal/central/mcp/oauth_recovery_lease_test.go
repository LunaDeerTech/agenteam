package mcp

import (
	"context"
	"errors"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestOAuthRecoveryLeaseFactCannotAdmitRecovery(t *testing.T) {
	original := oauthRecoveryTestFact(t)
	original.retired, original.released = true, true
	locks, err := oauthRecoveryLocks(original)
	if err != nil {
		t.Fatal(err)
	}
	store := &oauthSourceStore{}
	s := &Service{store: store}
	w := &oauthRecoveryUse{service: s, original: original, locks: locks, stopped: true}
	w.active.Store(true)
	ctx := context.WithValue(context.Background(), oauthRecoveryKey{}, w)
	released, err := s.retireStoppedOAuthIO(ctx, w)
	var fault *f.Fault
	if released || !errors.As(err, &fault) || fault.Code != f.Forbidden || store.queries != 0 {
		t.Fatal("retired row data bypassed the actual recovery owner", err)
	}
}

func TestOAuthRecoveryLeaseUsesCurrentCommittedRetirement(t *testing.T) {
	original := oauthRecoveryTestFact(t)
	current := original
	current.phase, current.retired, current.released = "complete", true, true
	// Normal completion can release the lease after this recovery's initial
	// snapshot. Flow Delete can then remove the Secret lease tombstone.
	released, err := oauthRecoveryLeaseReleased(original, current, f.CommittedResult())
	if err != nil || !released {
		t.Fatal("canonical retirement did not prevent duplicate release", err)
	}
	// Neither an unfinished outcome nor a completed exchange alone proves
	// lease retirement. Both must still reach the actual Secret provider.
	for _, phase := range []string{"unknown", "complete"} {
		current.phase, current.released = phase, false
		released, err = oauthRecoveryLeaseReleased(original, current, f.CommittedResult())
		if err != nil || released {
			t.Fatal("unreleased original lease skipped its provider", phase, err)
		}
	}
	// The initial snapshot is not the source of the decision, even if it
	// claims release. Only the newly validated locked row is consumed.
	original.retired, original.released = true, true
	released, err = oauthRecoveryLeaseReleased(original, current, f.CommittedResult())
	if err != nil || released {
		t.Fatal("initial snapshot replaced the canonical lease fact", err)
	}
	begin := oauthRecoveryTestFact(t)
	begin.operation, begin.credential, begin.credentialVersion, begin.lease = "begin", "", 0, ""
	released, err = oauthRecoveryLeaseReleased(begin, begin, f.CommittedResult())
	if err != nil || !released {
		t.Fatal("original lease-free begin acquired a release obligation", err)
	}
}

func TestOAuthRecoveryLeaseRejectsForeignTupleAndUnknown(t *testing.T) {
	original := oauthRecoveryTestFact(t)
	current := original
	current.retired, current.released = true, true
	for _, change := range []func(*oauthRecoveryFact){
		func(v *oauthRecoveryFact) { v.process = v.project },
		func(v *oauthRecoveryFact) { v.request = v.flow },
		func(v *oauthRecoveryFact) { v.lease = v.credential },
		func(v *oauthRecoveryFact) { v.credentialVersion++ },
		func(v *oauthRecoveryFact) { v.version++ },
		func(v *oauthRecoveryFact) { v.session = v.user },
		func(v *oauthRecoveryFact) { v.retired = false },
	} {
		foreign := current
		change(&foreign)
		released, err := oauthRecoveryLeaseReleased(original, foreign, f.CommittedResult())
		if err == nil || released {
			t.Fatal("foreign or invalid canonical fact skipped lease release")
		}
	}
	attempt, err := f.NewID[f.TransactionAttempt]()
	if err != nil {
		t.Fatal(err)
	}
	cause, err := f.NewRecoveryCause("mcp.oauth.stopped_io", original.request, "")
	if err != nil {
		t.Fatal(err)
	}
	released, err := oauthRecoveryLeaseReleased(original, current, f.UnknownResult(attempt, cause))
	var unknown *UnknownError
	if released || !errors.As(err, &unknown) || unknown.AttemptID() != attempt {
		t.Fatal("Unknown retirement lost its original identity or granted cleanup", err)
	}
	got, want := unknown.Cause().Details(), cause.Details()
	if got.Kind != want.Kind || got.Owner != want.Owner || got.RecoveryRunID != want.RecoveryRunID || got.CheckpointRef != want.CheckpointRef {
		t.Fatal("Unknown retirement changed its original recovery cause")
	}
	released, err = oauthRecoveryLeaseReleased(original, current, f.NotCommittedResult(f.NewFault(f.Forbidden, f.NotCommitted)))
	var fault *f.Fault
	if released || !errors.As(err, &fault) || fault.Code != f.Forbidden {
		t.Fatal("rejected retirement granted cleanup", err)
	}
}
