package project

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
)

type stopRoundGateStore struct {
	Store
	row          postgres.Row
	transactions int
	commit       f.CommitResult
	entered      chan struct{}
	release      chan struct{}
}

func (s *stopRoundGateStore) QueryRow(context.Context, string, ...any) postgres.Row {
	if s.row != nil {
		return s.row
	}
	return rowFunc(func(...any) error { return pgx.ErrNoRows })
}
func (s *stopRoundGateStore) WithinTx(_ context.Context, _ f.TransactionCause, _ func(context.Context, f.Tx) error) f.CommitResult {
	s.transactions++
	if s.entered != nil {
		close(s.entered)
		<-s.release
	}
	return s.commit
}
func stopRoundTestDriver(t *testing.T, s Store, step LifecycleLocalStopStep) *LifecycleStopDriver {
	t.Helper()
	a, err := NewLifecycleAuthority(s, registryTestManifest(t, registryTestEntries()), nil)
	if err != nil {
		t.Fatal(err)
	}
	d, err := NewLifecycleStopDriver(s, a, &testProcess{id: testID[oc.Process](t)}, step)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func TestLifecycleStopRoundConstructionAndOriginalStep(t *testing.T) {
	store := &stopRoundGateStore{}
	project, operation := testID[identity.Project](t), testID[c.Operation](t)
	cause := c.LifecycleCause{OperationID: operation, Action: c.Archive, ProjectVersion: 3}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	calls := 0
	original := errors.New("safe test error")
	d := stopRoundTestDriver(t, store, func(got context.Context, actor identity.Actor, actual c.LifecycleCause, scope c.ScopeRef) error {
		calls++
		ad := actor.Details()
		actualDeadline, ok := got.Deadline()
		if actual != cause || scope.ProjectID != project || scope.Kind != c.ProjectScope || ad.ServiceName != identity.ProjectLifecycle || ad.CauseRef != operation.String() || ad.ProjectID != project.String() || !ok || actualDeadline.After(deadline) {
			t.Error("original scope/cause/budget changed")
		}
		return original
	})
	if err := d.state.invoke(ctx, project, operation, cause); !errors.Is(err, original) || calls != 1 {
		t.Fatal("lost original local result", err, calls)
	}
	cancel()
	if err := d.state.invoke(ctx, project, operation, cause); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatal("canceled call reached provider")
	}
	var nilStore *stopRoundGateStore
	for _, s := range []Store{nil, nilStore, &stopRoundGateStore{}} {
		if _, err := NewLifecycleStopDriver(s, d.state.authority, d.state.processes, d.state.step); err == nil {
			t.Fatal("nil/foreign Store bound")
		}
	}
	if _, err := NewLifecycleStopDriver(store, d.state.authority, d.state.processes, nil); err == nil {
		t.Fatal("nil step bound")
	}
	if _, err := NewLifecycleStopDriver(store, &LifecycleAuthority{}, d.state.processes, d.state.step); err == nil {
		t.Fatal("zero authority bound")
	}
}
func TestLifecycleStopRoundUnconfirmedHasNoProviderCall(t *testing.T) {
	cause, _ := f.NewJobCause("test-stop", testID[c.Operation](t).String(), testID[struct{}](t).String())
	unknown := f.UnknownResult(testID[f.TransactionAttempt](t), cause)
	for _, tc := range []struct {
		name   string
		result f.CommitResult
		code   f.Code
	}{{"unknown", unknown, f.CommitUnknown}, {"rollback", f.NotCommittedResult(fault(f.InvalidState)), f.InvalidState}} {
		t.Run(tc.name, func(t *testing.T) {
			store := &stopRoundGateStore{commit: tc.result}
			calls := 0
			d := stopRoundTestDriver(t, store, func(context.Context, identity.Actor, c.LifecycleCause, c.ScopeRef) error { calls++; return nil })
			err := d.Run(context.Background(), testID[identity.Project](t), testID[c.Operation](t))
			hasCode(t, err, tc.code)
			if calls != 0 || store.transactions != 1 {
				t.Fatal("unconfirmed phase escaped into provider/terminal")
			}
			if tc.name == "unknown" {
				saved, ok := UnknownAttempt(err)
				if !ok || saved.AttemptID() != unknown.AttemptID() {
					t.Fatal("unknown provenance lost")
				}
			}
			if err = d.Drain(context.Background()); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestLifecycleStopRoundAdmissionAndActualDrain(t *testing.T) {
	store := &stopRoundGateStore{commit: f.NotCommittedResult(fault(f.InvalidState)), entered: make(chan struct{}), release: make(chan struct{})}
	d := stopRoundTestDriver(t, store, func(context.Context, identity.Actor, c.LifecycleCause, c.ScopeRef) error {
		t.Error("uncommitted step")
		return nil
	})
	project, operation := testID[identity.Project](t), testID[c.Operation](t)
	returned := make(chan error, 1)
	go func() { returned <- d.Run(context.Background(), project, operation) }()
	<-store.entered
	hasCode(t, d.Run(context.Background(), project, operation), f.ResourceBusy)
	d.Stop()
	hasCode(t, d.Run(context.Background(), testID[identity.Project](t), operation), f.ShuttingDown)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if d.Drain(ctx) == nil {
		t.Error("Stop fabricated original transaction return")
	}
	close(store.release)
	hasCode(t, <-returned, f.InvalidState)
	if err := d.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func TestLifecycleStopRoundStepCancellationStillWaits(t *testing.T) {
	store := &stopRoundGateStore{}
	entered, release := make(chan struct{}), make(chan struct{})
	d := stopRoundTestDriver(t, store, func(ctx context.Context, _ identity.Actor, _ c.LifecycleCause, _ c.ScopeRef) error {
		close(entered)
		<-ctx.Done()
		<-release
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	project, operation := testID[identity.Project](t), testID[c.Operation](t)
	returned := make(chan error, 1)
	go func() {
		returned <- d.state.invoke(ctx, project, operation, c.LifecycleCause{OperationID: operation, Action: c.Delete, ProjectVersion: 2})
	}()
	<-entered
	cancel()
	select {
	case <-returned:
		t.Fatal("caller cancel replaced actual provider return")
	default:
	}
	close(release)
	if err := <-returned; !errors.Is(err, context.Canceled) {
		t.Fatal("canceled successful provider lost deadline", err)
	}
}

func TestLifecycleStopRoundCheckpointErrorIsSafe(t *testing.T) {
	cause, _ := f.NewJobCause("project-lifecycle", testID[c.Operation](t).String(), testID[struct{}](t).String())
	physical := f.UnknownResult(testID[f.TransactionAttempt](t), cause)
	private := errors.New("private-stop-provider-value-DoNotExpose")
	err := lifecycleStopRoundError(commitError(physical), private)
	hasCode(t, err, f.CommitUnknown)
	saved, ok := UnknownAttempt(err)
	if !ok || saved.AttemptID() != physical.AttemptID() || !errors.Is(err, private) || strings.Contains(err.Error(), private.Error()) {
		t.Fatal("checkpoint error lost provenance or exposed private provider material")
	}
}
