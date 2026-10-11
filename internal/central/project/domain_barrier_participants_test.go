package project

import (
	"context"
	"errors"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// These are explicit Store boundary controls, not physical COMMIT evidence.
// Canonical current/terminal rows and successful reports use the PG fixture.
type barrierControlStore struct {
	Store
	within  func(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult
	acquire func(context.Context, f.Tx, []f.LockRequest) error
	inTx    func(f.Tx) (postgres.SQLExecutor, error)
	held    func(context.Context, f.Tx, []f.LockRequest) error
}

func (s *barrierControlStore) WithinTx(ctx context.Context, cause f.TransactionCause, callback func(context.Context, f.Tx) error) f.CommitResult {
	return s.within(ctx, cause, callback)
}
func (s *barrierControlStore) AcquireAll(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	return s.acquire(ctx, tx, locks)
}
func (s *barrierControlStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) { return s.inTx(tx) }
func (s *barrierControlStore) RequireHeldLocks(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	return s.held(ctx, tx, locks)
}

func barrierControl(t *testing.T, store Store, name c.ParticipantName) c.ProjectLifecycleParticipant {
	t.Helper()
	a, err := NewLifecycleAuthority(store, registryTestManifest(t, registryTestEntries()), nil)
	if err != nil {
		t.Fatal(err)
	}
	constructor := NewSecretStopParticipant
	if name == c.AuditParticipant {
		constructor = NewAuditStopParticipant
	}
	p, err := constructor(a)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDomainBarrierConstructionAndIdentity(t *testing.T) {
	actor, project, cause := lifecycleTestActor(t)
	scope := c.ScopeRef{Kind: c.ProjectScope, ProjectID: project}
	for _, constructor := range []func(*LifecycleAuthority) (c.ProjectLifecycleParticipant, error){NewSecretStopParticipant, NewAuditStopParticipant} {
		for _, a := range []*LifecycleAuthority{nil, {}} {
			p, err := constructor(a)
			hasCode(t, err, f.DependencyUnbound)
			if p != nil {
				t.Fatal("unbound authority returned participant")
			}
		}
	}
	for _, name := range []c.ParticipantName{c.SecretParticipant, c.AuditParticipant} {
		p := barrierControl(t, &barrierControlStore{within: func(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult {
			t.Fatal("invalid input reached Store")
			return f.CommittedResult()
		}}, name)
		if p.Name() != name {
			t.Fatal("participant identity drift")
		}
		meeting := testID[c.Meeting](t)
		for _, test := range []struct {
			actor identity.Actor
			cause c.LifecycleCause
			scope c.ScopeRef
			code  f.Code
		}{
			{testActor(t), cause, scope, f.Forbidden},
			{actor, c.LifecycleCause{}, scope, f.InvalidArgument},
			{actor, cause, c.ScopeRef{}, f.InvalidArgument},
			{actor, cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: testID[identity.Project](t)}, f.Forbidden},
			{actor, cause, c.ScopeRef{Kind: c.MeetingScope, ProjectID: project, MeetingID: &meeting}, f.DependencyUnbound},
		} {
			for _, call := range []func(context.Context, identity.Actor, c.LifecycleCause, c.ScopeRef) (c.StopReport, error){p.RequestStop, p.InspectStop} {
				report, err := call(context.Background(), test.actor, test.cause, test.scope)
				hasCode(t, err, test.code)
				if report.Details().State != "" {
					t.Fatal("rejection exposed report")
				}
			}
		}
		_, err := p.RequestStop(nil, actor, cause, scope)
		hasCode(t, err, f.InvalidArgument)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err = p.InspectStop(ctx, actor, cause, scope)
		if err == nil {
			t.Fatal("cancelled call admitted")
		}
		result, err := p.Cleanup(context.Background(), actor, cause, scope, nil)
		hasCode(t, err, f.DependencyUnbound)
		if result.Details().State != "" {
			t.Fatal("Cleanup fabricated progress")
		}
	}
}

func TestDomainBarrierTransactionOutcome(t *testing.T) {
	actor, project, cause := lifecycleTestActor(t)
	scope := c.ScopeRef{Kind: c.ProjectScope, ProjectID: project}
	for _, name := range []c.ParticipantName{c.SecretParticipant, c.AuditParticipant} {
		for _, outcome := range []string{"unknown", "not_committed", "empty_committed"} {
			t.Run(string(name)+"/"+outcome, func(t *testing.T) {
				attempt := testID[f.TransactionAttempt](t)
				var original f.TransactionCause
				p := barrierControl(t, &barrierControlStore{within: func(_ context.Context, txCause f.TransactionCause, _ func(context.Context, f.Tx) error) f.CommitResult {
					original = txCause
					switch outcome {
					case "unknown":
						return f.UnknownResult(attempt, txCause)
					case "not_committed":
						return f.NotCommittedResult(f.NewFault(f.Forbidden, f.NotCommitted))
					default:
						return f.CommittedResult()
					}
				}}, name)
				report, err := p.RequestStop(context.Background(), actor, cause, scope)
				if report.Details().State != "" || err == nil {
					t.Fatal("unconfirmed callback produced report")
				}
				if outcome == "unknown" {
					hasCode(t, err, f.CommitUnknown)
					result, ok := UnknownAttempt(err)
					var fault *f.Fault
					gotCause, wantCause := result.Cause().Details(), original.Details()
					if !ok || result.AttemptID() != attempt || gotCause.Kind != f.RecoveryCause || gotCause.Owner != wantCause.Owner || gotCause.RecoveryRunID != wantCause.RecoveryRunID || gotCause.CheckpointRef != wantCause.CheckpointRef || !errors.As(err, &fault) || fault.CauseID != attempt.String() || fault.CommitState != f.Unknown {
						t.Fatal("physical attempt/cause lost")
					}
				} else if outcome == "not_committed" {
					hasCode(t, err, f.Forbidden)
				} else {
					hasCode(t, err, f.DependencyUnavailable)
				}
			})
		}
	}
}

func TestDomainBarrierWaitsForActualStoreReturn(t *testing.T) {
	actor, project, cause := lifecycleTestActor(t)
	scope := c.ScopeRef{Kind: c.ProjectScope, ProjectID: project}
	entered, release := make(chan struct{}), make(chan struct{})
	p := barrierControl(t, &barrierControlStore{within: func(ctx context.Context, _ f.TransactionCause, _ func(context.Context, f.Tx) error) f.CommitResult {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 2*time.Second {
			t.Error("participant budget missing")
		}
		close(entered)
		<-release
		return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted))
	}}, c.SecretParticipant)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		report, err := p.RequestStop(ctx, actor, cause, scope)
		if err == nil || report.Details().State != "" {
			t.Error("cancelled Store returned report")
		}
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("Store not entered")
	}
	cancel()
	select {
	case <-returned:
		close(release)
		t.Fatal("context cancellation substituted for original Store return")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("original call did not return")
	}
}

func TestDomainBarrierRequiresOriginalStoreAndHeldLock(t *testing.T) {
	actor, project, cause := lifecycleTestActor(t)
	scope := c.ScopeRef{Kind: c.ProjectScope, ProjectID: project}
	for _, stage := range []string{"acquire", "foreign-tx", "held-lock"} {
		t.Run(stage, func(t *testing.T) {
			tx := f.NewTx()
			heldCalled := false
			st := &barrierControlStore{}
			st.within = func(ctx context.Context, _ f.TransactionCause, callback func(context.Context, f.Tx) error) f.CommitResult {
				err := callback(ctx, tx)
				var fault *f.Fault
				if !errors.As(err, &fault) {
					t.Fatal("invalid Store boundary escaped callback", err)
				}
				return f.NotCommittedResult(fault)
			}
			st.acquire = func(_ context.Context, got f.Tx, locks []f.LockRequest) error {
				if got != tx || len(locks) != 1 || locks[0].Mode != f.Shared || f.CompareLockKeys(locks[0].Key, projectLock(project, f.Shared).Key) != 0 {
					t.Fatal("not exact Project SH")
				}
				if stage == "acquire" {
					return errors.New("controlled acquisition failure")
				}
				return nil
			}
			st.inTx = func(got f.Tx) (postgres.SQLExecutor, error) {
				if got != tx || stage == "foreign-tx" {
					return nil, errors.New("foreign transaction")
				}
				return st, nil
			}
			st.held = func(_ context.Context, got f.Tx, locks []f.LockRequest) error {
				heldCalled = true
				if got != tx || len(locks) != 1 || locks[0].Mode != f.Shared {
					t.Fatal("held-lock check drift")
				}
				return errors.New("not actually held")
			}
			p := barrierControl(t, st, c.AuditParticipant)
			report, err := p.InspectStop(context.Background(), actor, cause, scope)
			hasCode(t, err, f.DependencyUnavailable)
			if report.Details().State != "" || heldCalled != (stage == "held-lock") {
				t.Fatal("boundary order or report drift")
			}
		})
	}
}
