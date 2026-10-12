package project

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// This fixed port and the controlled Store observe actual Go call returns.
// They do not claim a real lifecycle COMMIT; the complete canonical cleanup
// transition and the concrete D06 provider are exercised by the PG test.
type cleanupDriverPort struct {
	c.ProjectLifecycleParticipant
	name c.ParticipantName
	call func(context.Context, identity.Actor, c.LifecycleCause, c.ScopeRef, *c.CleanupCheckpoint) (c.CleanupReport, error)
}

func (p *cleanupDriverPort) Name() c.ParticipantName { return p.name }
func (p *cleanupDriverPort) Cleanup(ctx context.Context, actor identity.Actor, cause c.LifecycleCause, scope c.ScopeRef, cp *c.CleanupCheckpoint) (c.CleanupReport, error) {
	return p.call(ctx, actor, cause, scope, cp)
}

func cleanupDriverFixture(t *testing.T, store Store, call func(context.Context, identity.Actor, c.LifecycleCause, c.ScopeRef, *c.CleanupCheckpoint) (c.CleanupReport, error)) (*LifecycleCleanupDriver, lifecycleCleanupRound) {
	t.Helper()
	a, err := NewLifecycleAuthority(store, registryTestManifest(t, registryTestEntries()), nil)
	if err != nil {
		t.Fatal(err)
	}
	d, err := NewLifecycleCleanupDriver(store, a, &testProcess{id: testID[oc.Process](t)}, &cleanupDriverPort{name: c.OutboxParticipant, call: call})
	if err != nil {
		t.Fatal(err)
	}
	project, operation := testID[identity.Project](t), testID[c.Operation](t)
	round := lifecycleCleanupRound{claim: lifecycleStopClaim{project: project, operation: operation, process: d.state.process, attempt: testID[struct{}](t).String(), fence: 1, phase: "running"}, cause: c.LifecycleCause{OperationID: operation, Action: c.Delete, ProjectVersion: 3}, operationVersion: 4, rowVersion: 2}
	return d, round
}

func TestLifecycleCleanupDriverConstructionAndInput(t *testing.T) {
	store := &stopRoundGateStore{}
	d, round := cleanupDriverFixture(t, store, nil)
	var absent *cleanupDriverPort
	for _, tc := range []struct {
		name string
		run  func() error
	}{
		{"nil-store", func() error {
			_, e := NewLifecycleCleanupDriver(nil, d.state.authority, d.state.processes, d.state.participant)
			return e
		}},
		{"foreign-store", func() error {
			_, e := NewLifecycleCleanupDriver(&stopRoundGateStore{}, d.state.authority, d.state.processes, d.state.participant)
			return e
		}},
		{"nil-participant", func() error {
			_, e := NewLifecycleCleanupDriver(store, d.state.authority, d.state.processes, absent)
			return e
		}},
		{"wrong-participant", func() error {
			_, e := NewLifecycleCleanupDriver(store, d.state.authority, d.state.processes, &cleanupDriverPort{name: c.AuditParticipant})
			return e
		}},
		{"invalid-process", func() error {
			_, e := NewLifecycleCleanupDriver(store, d.state.authority, &testProcess{}, d.state.participant)
			return e
		}},
		{"nil-context", func() error { return d.Run(nil, round.claim.project, round.claim.operation) }},
		{"invalid-project", func() error { return d.Run(context.Background(), c.ProjectID{}, round.claim.operation) }},
		{"stopped", func() error { d.Stop(); return d.Run(context.Background(), round.claim.project, round.claim.operation) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); err == nil || store.transactions != 0 {
				t.Fatal("invalid assembly/input started a transaction", err)
			}
		})
	}
}

func TestLifecycleCleanupDriverUnconfirmedAndBoundedOwners(t *testing.T) {
	cause, _ := f.NewJobCause("test-cleanup", testID[c.Operation](t).String(), testID[struct{}](t).String())
	unknown := f.UnknownResult(testID[f.TransactionAttempt](t), cause)
	for _, tc := range []struct {
		name   string
		result f.CommitResult
		code   f.Code
	}{{"unknown", unknown, f.CommitUnknown}, {"rollback", f.NotCommittedResult(fault(f.InvalidState)), f.InvalidState}, {"committed-without-callback", f.CommittedResult(), f.DependencyUnavailable}} {
		t.Run(tc.name, func(t *testing.T) {
			store := &stopRoundGateStore{commit: tc.result}
			d, round := cleanupDriverFixture(t, store, func(context.Context, identity.Actor, c.LifecycleCause, c.ScopeRef, *c.CleanupCheckpoint) (c.CleanupReport, error) {
				t.Error("unconfirmed canonical start reached Outbox")
				return c.CleanupReport{}, nil
			})
			err := d.Run(context.Background(), round.claim.project, round.claim.operation)
			hasCode(t, err, tc.code)
			if store.transactions != 1 {
				t.Fatal("unconfirmed round wrote a checkpoint")
			}
			if tc.name != "unknown" {
				if len(d.state.owners) != 0 {
					t.Fatal("known rejected start retained an owner")
				}
				return
			}
			saved, ok := UnknownAttempt(err)
			if !ok || saved.AttemptID() != unknown.AttemptID() {
				t.Fatal("lost original Unknown")
			}
			retained := d.state.owners[round.claim.operation]
			if retained.claim == nil || retained.claim.project != round.claim.project {
				t.Fatal("lost original attempt owner")
			}
			for n := 0; n < 3; n++ {
				hasCode(t, d.Run(context.Background(), testID[identity.Project](t), testID[c.Operation](t)), f.CommitUnknown)
			}
			hasCode(t, d.Run(context.Background(), testID[identity.Project](t), testID[c.Operation](t)), f.ResourceBusy)
			if len(d.state.owners) != 4 || store.transactions != 4 {
				t.Fatal("Unknown escaped the owner bound")
			}
			if err := d.Drain(context.Background()); err != nil || len(d.state.owners) != 4 {
				t.Fatal("local drain retired durable Unknown", err)
			}
		})
	}
}

func TestLifecycleCleanupDriverWaitsForOriginalStoreReturn(t *testing.T) {
	store := &stopRoundGateStore{commit: f.NotCommittedResult(fault(f.InvalidState)), entered: make(chan struct{}), release: make(chan struct{})}
	d, round := cleanupDriverFixture(t, store, nil)
	returned := make(chan error, 1)
	var once sync.Once
	release := func() { once.Do(func() { close(store.release) }) }
	joined := false
	t.Cleanup(func() {
		release()
		if !joined {
			select {
			case <-returned:
			case <-time.After(5 * time.Second):
				t.Error("original Run did not return")
			}
		}
	})
	go func() { returned <- d.Run(context.Background(), round.claim.project, round.claim.operation) }()
	select {
	case <-store.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("original Store was not reached")
	}
	hasCode(t, d.Run(context.Background(), round.claim.project, round.claim.operation), f.ResourceBusy)
	d.Stop()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := d.Drain(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("Stop replaced original Store return", err)
	}
	select {
	case <-returned:
		joined = true
		t.Fatal("Run returned before Store")
	default:
	}
	release()
	select {
	case err := <-returned:
		joined = true
		hasCode(t, err, f.InvalidState)
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}
	if err := d.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestLifecycleCleanupDriverOriginalAdapterReturnAndScope(t *testing.T) {
	for _, mode := range []string{"completed", "completed-with-error", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			free := func() { once.Do(func() { close(release) }) }
			private := errors.New("cleanup-private-material-not-for-log")
			var saved context.Context
			d, round := cleanupDriverFixture(t, &stopRoundGateStore{}, func(ctx context.Context, actor identity.Actor, cause c.LifecycleCause, scope c.ScopeRef, cp *c.CleanupCheckpoint) (c.CleanupReport, error) {
				saved = ctx
				if actor.Details().ServiceName != identity.ProjectLifecycle || actor.Details().CauseRef != cause.OperationID.String() || scope.Kind != c.ProjectScope || cp != nil {
					t.Error("changed original adapter inputs")
				}
				close(entered)
				<-release
				report, err := c.NewCleanupReport(c.OutboxParticipant, cause, scope, c.CleanupDetails{State: c.CleanupCompleted})
				if err != nil {
					return c.CleanupReport{}, err
				}
				if mode == "completed-with-error" {
					return report, private
				}
				return report, nil
			})
			ctx, cancel := context.WithCancel(context.Background())
			type outcome struct {
				report c.CleanupReport
				err    error
			}
			returned := make(chan outcome, 1)
			joined := false
			t.Cleanup(func() {
				cancel()
				free()
				if !joined {
					select {
					case <-returned:
					case <-time.After(5 * time.Second):
						t.Error("adapter not joined")
					}
				}
			})
			go func() { report, err := d.state.invoke(ctx, round); returned <- outcome{report, err} }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("adapter not entered")
			}
			if mode == "cancelled" {
				cancel()
			}
			select {
			case <-returned:
				joined = true
				t.Fatal("adapter return invented")
			default:
			}
			free()
			var got outcome
			select {
			case got = <-returned:
				joined = true
			case <-time.After(5 * time.Second):
				t.Fatal("adapter not returned")
			}
			switch mode {
			case "completed":
				if got.err != nil || got.report.Details().State != c.CleanupCompleted {
					t.Fatal("lost original success", got.err)
				}
			case "completed-with-error":
				if !errors.Is(got.err, private) || strings.Contains(got.err.Error(), private.Error()) || got.report.Details().State != "" {
					t.Fatal("error report advanced or leaked", got.err)
				}
			case "cancelled":
				if !errors.Is(got.err, context.Canceled) || got.report.Details().State != "" {
					t.Fatal("cancelled call published success", got.err)
				}
			}
			// Removing cancellation preserves values but cannot revive the closed
			// private scope or use it as public authorization after actual return.
			hasCode(t, d.state.authority.requireOutboxCleanupClaimInTx(context.WithoutCancel(saved), f.NewTx(), round.cause, round.claim.project), f.Forbidden)
		})
	}
}

func TestLifecycleCleanupDriverExactClaimAndRecovery(t *testing.T) {
	store := &retirementPrepStore{tx: f.NewTx(), live: true, held: map[string]f.LockMode{}}
	d, round := cleanupDriverFixture(t, store, nil)
	project := round.claim.project
	store.required = []f.LockRequest{projectLock(project, f.Shared)}
	store.held[store.required[0].Key.Canonical()] = f.Exclusive
	store.claim = &round.claim
	witness := &lifecycleCleanupScope{state: d.state, round: round, open: true}
	ctx := context.WithValue(context.Background(), lifecycleCleanupScopeKey{}, witness)
	// A private controlled claim proves only the original exact tuple check.
	// No public context/DTO can create it; actual start COMMIT belongs to PG.
	if err := d.state.authority.requireOutboxCleanupClaimInTx(ctx, store.tx, round.cause, project); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		change  func()
		context context.Context
		tx      f.Tx
	}{
		{"public-context", func() {}, context.Background(), store.tx},
		{"foreign-tx", func() {}, ctx, f.NewTx()},
		{"missing-lock", func() { delete(store.held, store.required[0].Key.Canonical()) }, ctx, store.tx},
		{"stale-claim", func() { copy := round.claim; copy.fence++; store.claim = &copy }, ctx, store.tx},
		{"closed-scope", func() { witness.open = false }, ctx, store.tx},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store.rows = 0
			store.claim = &round.claim
			store.held[store.required[0].Key.Canonical()] = f.Exclusive
			witness.open = true
			tc.change()
			if err := d.state.authority.requireOutboxCleanupClaimInTx(tc.context, tc.tx, round.cause, project); err == nil {
				t.Fatal("invalid original claim accepted")
			}
		})
	}
	claim := round.claim
	hasCode(t, d.state.canJoin(context.Background(), &claim), f.ResourceBusy)
	d.state.owners[claim.operation] = lifecycleCleanupOwner{project: project, claim: &claim}
	if err := d.state.canJoin(context.Background(), &claim); err != nil {
		t.Fatal(err)
	}
	other := claim
	other.attempt = testID[struct{}](t).String()
	hasCode(t, d.state.canJoin(context.Background(), &other), f.ResourceBusy)
	// An Unknown replacement can either have installed its new tuple or kept
	// the original one. Both original calls returned; no third tuple qualifies.
	d.state.owners[claim.operation] = lifecycleCleanupOwner{project: project, claim: &other, prior: &claim}
	if err := d.state.canJoin(context.Background(), &claim); err != nil {
		t.Fatal("Unknown replacement lost the original uncommitted tuple", err)
	}
	if err := d.state.canJoin(context.Background(), &other); err != nil {
		t.Fatal("Unknown replacement lost its possible committed tuple", err)
	}
	other.process = testID[oc.Process](t)
	hasCode(t, d.state.canJoin(context.Background(), &other), f.ResourceBusy)
	process := d.state.processes.(*testProcess)
	if process.requested != other.process {
		t.Fatal("wrong foreign original owner")
	}
	process.stopped = true
	if err := d.state.canJoin(context.Background(), &other); err != nil {
		t.Fatal(err)
	}
}
