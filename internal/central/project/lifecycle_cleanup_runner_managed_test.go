package project

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mtc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func runnerManagedCleanupCheckpoint(t *testing.T, r lifecycleCleanupRound, stage string) c.CleanupCheckpoint {
	t.Helper()
	p := mtc.ProjectCleanupProgress{ProjectID: r.claim.project, OperationID: r.cause.OperationID, ProjectVersion: r.cause.ProjectVersion, ProcessID: testID[oc.Process](t), InstanceID: testID[struct{}](t).String(), Stage: stage}
	cp, err := mtc.NewProjectCleanupCheckpoint(p, r.cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: r.claim.project})
	if err != nil {
		t.Fatal(err)
	}
	return cp
}

func TestLifecycleCleanupRunnerManagedConstruction(t *testing.T) {
	d, _, s, _, _ := runnerManagedCleanupFixture(t)
	for _, mode := range []string{"foreign-store", "wrong-participant", "invalid-process"} {
		t.Run(mode, func(t *testing.T) {
			store, processes, port := Store(s), d.state.processes, d.state.runnerManaged
			switch mode {
			case "foreign-store":
				store = &stopRoundGateStore{}
			case "wrong-participant":
				port = &cleanupDriverPort{name: c.WorkParticipant}
			case "invalid-process":
				processes = &testProcess{}
			}
			if _, err := NewLifecycleCleanupDriverForRunnerManaged(store, d.state.authority, processes, port); err == nil {
				t.Fatal("foreign fixed assembly accepted")
			}
		})
	}
	if _, err := NewRunnerManagedProjectCleanupAuthority(d.state.authority, &stopRoundGateStore{}); err == nil {
		t.Fatal("cleanup accepted another Store")
	}
	if _, err := NewRunnerManagedStopAuthority(d.state.authority, &stopRoundGateStore{}); err == nil {
		t.Fatal("stop accepted another Store")
	}
	if s.transactions.Load() != 0 || len(s.seen()) != 0 {
		t.Fatal("construction performed I/O")
	}
}

func TestLifecycleCleanupRunnerManagedCheckpoint(t *testing.T) {
	_, _, _, r, _ := runnerManagedCleanupFixture(t)
	scope := c.ScopeRef{Kind: c.ProjectScope, ProjectID: r.claim.project}
	for _, stage := range []string{"agents", "completed"} {
		cp := runnerManagedCleanupCheckpoint(t, r, stage)
		p, err := mtc.DecodeProjectCleanupCheckpoint(&cp, r.cause, scope)
		if err != nil || p.Stage != stage {
			t.Fatal("closed RunnerManaged stage rejected", stage, err)
		}
	}
	cp := runnerManagedCleanupCheckpoint(t, r, "agents")
	for _, raw := range [][]byte{
		bytes.Replace(cp.Bytes(), []byte(`"stage":"agents"`), []byte(`"stage":"project_calls"`), 1),
		bytes.Replace(cp.Bytes(), []byte(`"stage":"agents"`), []byte(`"stage":"agents","stage":"agents"`), 1),
		bytes.Replace(cp.Bytes(), []byte(`"stage":"agents"`), []byte(`"stage":"agents","completed":true`), 1),
	} {
		changed, err := c.NewCleanupCheckpoint(c.RunnerManagedParticipant, r.cause, scope, 1, raw)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = mtc.DecodeProjectCleanupCheckpoint(&changed, r.cause, scope); err == nil {
			t.Fatal("foreign or noncanonical checkpoint accepted")
		}
	}
	report, err := c.NewCleanupReport(c.RunnerManagedParticipant, r.cause, scope, c.CleanupDetails{State: c.CleanupCompleted, Checkpoint: &cp})
	if err != nil {
		t.Fatal(err)
	}
	if cleanupRoundReport(r, report) == nil {
		t.Fatal("nonterminal RunnerManaged checkpoint completed cleanup")
	}
}

// The registered round is controlled input. Run invokes its actual provider,
// waits for its return, and reaches canonical SQL's sentinel. No successful
// persisted registration or checkpoint is claimed by this fixture.
func TestLifecycleCleanupRunnerManagedProviderReturnAndCheckpointRollback(t *testing.T) {
	d, _, s, r, _ := runnerManagedCleanupFixture(t)
	w := &runnerManagedCleanupRound{round: r, registered: true, registeredImage: &runnerManagedCleanupImage{claim: &r.claim, rows: []byte(`{"registered":true}`)}}
	d.state.owners[r.claim.operation] = lifecycleCleanupOwner{project: r.claim.project, runnerManaged: w}
	cp := runnerManagedCleanupCheckpoint(t, r, "agents")
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	var saved context.Context
	invoked := 0
	d.state.runnerManaged = &cleanupDriverPort{name: c.RunnerManagedParticipant, call: func(ctx context.Context, actor i.Actor, cause c.LifecycleCause, target c.ScopeRef, prior *c.CleanupCheckpoint) (c.CleanupReport, error) {
		invoked++
		saved = ctx
		if actor.Details().ServiceName != i.ProjectLifecycle || cause != r.cause || target.ProjectID != r.claim.project || prior != nil {
			t.Error("original provider input changed")
		}
		close(entered)
		<-release
		return c.NewCleanupReport(c.RunnerManagedParticipant, cause, target, c.CleanupDetails{State: c.CleanupPending, Checkpoint: &cp})
	}}
	// finish reads the operation first, so do not substitute a claim projection.
	s.claim = nil
	s.within = func(ctx context.Context, _ f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
		err := fn(ctx, s.tx)
		if !errors.Is(err, s.readErr) {
			t.Error("checkpoint did not reach canonical SQL", err)
		}
		return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted).WithCause(err))
	}
	done := make(chan error, 1)
	joined := false
	t.Cleanup(func() {
		free()
		if !joined {
			<-done
		}
	})
	go func() { done <- d.Run(context.Background(), r.claim.project, r.claim.operation) }()
	retirementPrepAwait(t, entered)
	if s.transactions.Load() != 0 || w.report != nil {
		t.Fatal("checkpoint started before provider actually returned")
	}
	select {
	case <-done:
		joined = true
		t.Fatal("Run detached original provider")
	default:
	}
	free()
	err := <-done
	joined = true
	if !errors.Is(err, s.readErr) || w.report == nil {
		t.Fatal("actual provider report was not retained across rollback", err)
	}
	original := w.report
	private, _ := saved.Value(runnerManagedCleanupScopeKey{}).(*lifecycleCleanupScope)
	common, _ := saved.Value(lifecycleCleanupScopeKey{}).(*lifecycleCleanupScope)
	if private == nil || private != common || private.open {
		t.Fatal("provider scope was not original and closed on actual return")
	}
	if err = d.Run(context.Background(), r.claim.project, r.claim.operation); !errors.Is(err, s.readErr) {
		t.Fatal(err)
	}
	if invoked != 1 || w.report != original || d.state.owners[r.claim.operation].runnerManaged != w || w.checkpointUnknown != nil || s.transactions.Load() != 2 || slices.Contains(s.seen(), "forbidden-write") {
		t.Fatal("known checkpoint rollback repeated provider or lost original report")
	}
}

// The mutation callback and confirmation both stop at canonical SQL's sentinel.
// Controlled rollback-ACK Unknown tests the real Run retention/return boundary;
// it is not a positive full-manifest fixture or physical COMMIT observation.
func TestLifecycleCleanupRunnerManagedProjectUnknownReturn(t *testing.T) {
	for _, registration := range []bool{true, false} {
		name := "checkpoint"
		if registration {
			name = "registration"
		}
		t.Run(name, func(t *testing.T) {
			d, _, s, r, _ := runnerManagedCleanupFixture(t)
			w := &runnerManagedCleanupRound{round: r, before: &runnerManagedCleanupImage{rows: []byte(`{"prior":true}`)}}
			if !registration {
				cp := runnerManagedCleanupCheckpoint(t, r, "completed")
				report, err := c.NewCleanupReport(c.RunnerManagedParticipant, r.cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: r.claim.project}, c.CleanupDetails{State: c.CleanupCompleted, Checkpoint: &cp})
				if err != nil {
					t.Fatal(err)
				}
				w.registered, w.report = true, &report
				w.registeredImage = &runnerManagedCleanupImage{claim: &r.claim, rows: []byte(`{"registered":true}`)}
			}
			d.state.owners[r.claim.operation] = lifecycleCleanupOwner{project: r.claim.project, runnerManaged: w}
			d.state.runnerManaged = &cleanupDriverPort{name: c.RunnerManagedParticipant, call: func(context.Context, i.Actor, c.LifecycleCause, c.ScopeRef, *c.CleanupCheckpoint) (c.CleanupReport, error) {
				t.Error("Project confirmation invoked provider")
				return c.CleanupReport{}, errors.New("unexpected provider")
			}}
			original := f.UnknownResult(testID[f.TransactionAttempt](t), r.claim.cause())
			s.claim = nil
			s.within = func(ctx context.Context, _ f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
				if err := fn(ctx, s.tx); !errors.Is(err, s.readErr) {
					t.Error("mutation did not reach canonical SQL", err)
				}
				return original
			}
			requireFinalizeUnknown(t, d.Run(context.Background(), r.claim.project, r.claim.operation), original)
			if d.state.owners[r.claim.operation].runnerManaged != w || d.state.authority.cleanupCalls.runnerManaged[r.claim.project] != w || registration && w.registrationUnknown == nil || !registration && w.checkpointUnknown == nil {
				t.Fatal("original raw Unknown round was not retained")
			}
			other, err := NewLifecycleCleanupDriverForRunnerManaged(s, d.state.authority, d.state.processes, d.state.runnerManaged)
			if err != nil {
				t.Fatal(err)
			}
			hasCode(t, other.Run(context.Background(), r.claim.project, r.claim.operation), f.ResourceBusy)
			if s.transactions.Load() != 1 {
				t.Fatal("second driver borrowed returned Unknown")
			}
			endB, err := d.state.authority.beginCleanupCall(context.Background(), testID[i.Project](t))
			if err != nil {
				t.Fatal("unrelated Project B blocked", err)
			}
			endB()
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			free := func() { once.Do(func() { close(release) }) }
			s.within = func(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
				got, want := cause.Details(), original.Cause().Details()
				if got.Kind != want.Kind || got.JobType != want.JobType || got.JobID != want.JobID || got.JobAttemptID != want.JobAttemptID {
					t.Error("confirmation changed the original transaction cause")
				}
				if err := fn(ctx, s.tx); !errors.Is(err, s.readErr) {
					t.Error("confirmation skipped canonical SQL", err)
				}
				close(entered)
				<-release
				return f.UnknownResult(testID[f.TransactionAttempt](t), cause)
			}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			joined := false
			t.Cleanup(func() {
				cancel()
				free()
				if !joined {
					<-done
				}
			})
			go func() { done <- d.Run(ctx, r.claim.project, r.claim.operation) }()
			retirementPrepAwait(t, entered)
			cancel()
			select {
			case <-done:
				joined = true
				t.Fatal("cancel replaced original confirmation return")
			default:
			}
			hasCode(t, other.Run(context.Background(), r.claim.project, r.claim.operation), f.ResourceBusy)
			free()
			err = <-done
			joined = true
			requireFinalizeUnknown(t, err, original)
			kept := w.checkpointUnknown
			if registration {
				kept = w.registrationUnknown
			}
			if kept == nil || kept.AttemptID() != original.AttemptID() || s.transactions.Load() != 2 || slices.Contains(s.seen(), "forbidden-write") || d.state.owners[r.claim.operation].runnerManaged != w {
				t.Fatal("read-only confirmation replaced first raw Unknown or original round")
			}
		})
	}
}

func TestLifecycleCleanupRunnerManagedSharedCapacity(t *testing.T) {
	d, _, _, r, _ := runnerManagedCleanupFixture(t)
	a := d.state.authority
	w := &runnerManagedCleanupRound{round: r}
	end, err := a.beginCleanupCall(context.Background(), r.claim.project)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.retainRunnerManagedCleanup(w); err != nil {
		t.Fatal(err)
	}
	end()
	defer a.releaseRunnerManagedCleanup(w)
	// Work retention and active generic cleanup consume the same four P slots.
	work := &workCleanupRound{round: r}
	work.round.claim.project = testID[i.Project](t)
	end, err = a.beginCleanupCall(context.Background(), work.round.claim.project)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.retainWorkCleanup(work); err != nil {
		t.Fatal(err)
	}
	end()
	defer a.releaseWorkCleanup(work)
	for n := 0; n < 2; n++ {
		end, err = a.beginCleanupCall(context.Background(), testID[i.Project](t))
		if err != nil {
			t.Fatal(err)
		}
		defer end()
	}
	_, err = a.beginCleanupCall(context.Background(), testID[i.Project](t))
	hasCode(t, err, f.ResourceBusy)
	_, err = a.beginCleanupRunnerManagedCall(context.Background(), r.claim.project, nil, nil, nil, nil, nil, &runnerManagedCleanupRound{round: r})
	hasCode(t, err, f.ResourceBusy)
	end, err = a.beginCleanupRunnerManagedCall(context.Background(), r.claim.project, nil, nil, nil, nil, nil, w)
	if err != nil {
		t.Fatal("original RunnerManaged pointer blocked at capacity", err)
	}
	end()
	a.releaseRunnerManagedCleanup(w)
	end, err = a.beginCleanupCall(context.Background(), testID[i.Project](t))
	if err != nil {
		t.Fatal("retired RunnerManaged slot leaked", err)
	}
	end()
}
