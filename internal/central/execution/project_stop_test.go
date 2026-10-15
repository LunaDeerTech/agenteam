package execution

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// These private owner controls do not issue a LifecycleStopDriver scope or
// simulate committed SQL. Project tests and the real PG graph cover that grant.
func associatedStopTarget(t *testing.T, p i.ProjectID) (pc.LifecycleCause, pc.ScopeRef) {
	t.Helper()
	cause := pc.LifecycleCause{OperationID: newTestID[pc.Operation](t), Action: pc.Delete, ProjectVersion: 2}
	scope := pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: p}
	if cause.Validate() != nil || pc.RequireProjectScope(scope) != nil {
		t.Fatal("invalid stop fixture")
	}
	return cause, scope
}

func TestAssociatedProjectStopKeepsOtherProjectAndOriginalReturn(t *testing.T) {
	pa, da := associatedTestIdentity(t)
	pb, db := associatedTestIdentity(t)
	cause, scope := associatedStopTarget(t, pa)
	enteredA, enteredB := make(chan context.Context, 1), make(chan context.Context, 1)
	releaseA, releaseB := make(chan struct{}), make(chan struct{})
	var onceA, onceB sync.Once
	defer onceA.Do(func() { close(releaseA) })
	defer onceB.Do(func() { close(releaseB) })
	var starts atomic.Int32
	e := associatedTestExecutor(associatedExecutorOps{
		inspect: func(context.Context, i.ProjectID, associatedIdentity) (associatedFacts, error) {
			return associatedFacts{status: c.Created}, nil
		},
		prepare: func(ctx context.Context, id i.ExecutionID) error {
			if id == da.ExecutionID {
				enteredA <- ctx
				<-releaseA
			} else {
				enteredB <- ctx
				<-releaseB
			}
			return ctx.Err()
		},
		start: func(context.Context, i.ExecutionID) (c.DirectTextReceipt, error) {
			starts.Add(1)
			return c.DirectTextReceipt{}, fault(f.DependencyUnbound)
		},
		owners: func(*associatedCall) associatedOwners { return associatedOwners{} },
	}, 2, time.Second)
	done := associatedTestRun(t, e, context.Background())
	defer e.Stop()
	if _, err := e.Advance(context.Background(), pa, da); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Advance(context.Background(), pb, db); err != nil {
		t.Fatal(err)
	}
	ctxA, ctxB := associatedTestReceive(t, enteredA), associatedTestReceive(t, enteredB)
	report, err := e.state.stopProject(context.Background(), cause, scope, false)
	if err != nil || !report.Matches(cause, scope) || report.Details().LocalJoined || report.Details().PendingJobs != 1 {
		t.Fatal("target stop lost original call", err, report.Details())
	}
	associatedTestReceive(t, ctxA.Done())
	if ctxB.Err() != nil || e.Joined() {
		t.Fatal("target stop cancelled the shared lifetime or B")
	}
	joined := make(chan error, 1)
	joinedRead := false
	t.Cleanup(func() {
		if !joinedRead {
			associatedTestReceive(t, joined)
		}
	})
	go func() {
		report, err := e.state.stopProject(context.Background(), cause, scope, true)
		if err == nil && !report.Details().LocalJoined {
			err = errors.New("target did not join")
		}
		joined <- err
	}()
	select {
	case err := <-joined:
		joinedRead = true
		t.Fatal("Drain returned before the original source", err)
	default:
	}
	onceA.Do(func() { close(releaseA) })
	err = associatedTestReceive(t, joined)
	joinedRead = true
	if err != nil {
		t.Fatal(err)
	}
	if ctxB.Err() != nil || starts.Load() != 0 {
		t.Fatal("A shutdown started Model or stopped B")
	}
	if got, err := e.Advance(context.Background(), pb, db); err != nil || !got.Active {
		t.Fatal("B lost its existing original job", err)
	}
	e.Stop()
	onceB.Do(func() { close(releaseB) })
	if err := associatedTestReceive(t, done); err != nil || !e.Joined() {
		t.Fatal("global original calls did not return", err)
	}
}

func TestAssociatedProjectStopCountsLateAdmissionUntilReturn(t *testing.T) {
	pa, da := associatedTestIdentity(t)
	pb, db := associatedTestIdentity(t)
	causeA, scopeA := associatedStopTarget(t, pa)
	causeB, scopeB := associatedStopTarget(t, pb)
	entered, release := make(chan context.Context, 1), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	var effects atomic.Int32
	e := associatedTestExecutor(associatedExecutorOps{
		admit: func(ctx context.Context, p i.ProjectID, _ associatedIdentity) error {
			if p == pa {
				return fault(f.InvalidState)
			}
			entered <- ctx
			<-release // A physical COMMIT alone is not the original read return.
			return nil
		},
		inspect: func(context.Context, i.ProjectID, associatedIdentity) (associatedFacts, error) {
			effects.Add(1)
			return associatedFacts{status: c.Succeeded}, nil
		},
		owners: func(*associatedCall) associatedOwners { return associatedOwners{} },
	}, 2, time.Second)
	done := associatedTestRun(t, e, context.Background())
	defer e.Stop()
	if report, err := e.state.stopProject(context.Background(), causeA, scopeA, true); err != nil || !report.Details().LocalJoined {
		t.Fatal("empty target control failed", err)
	}
	if _, err := e.Advance(context.Background(), pa, da); err == nil {
		t.Fatal("late target admission bypassed canonical gate")
	}
	returned := make(chan error, 1)
	go func() { _, err := e.Advance(context.Background(), pb, db); returned <- err }()
	ctx := associatedTestReceive(t, entered)
	if _, err := e.Advance(context.Background(), pb, db); err == nil {
		t.Fatal("second call bypassed original admission")
	}
	report, err := e.state.stopProject(context.Background(), causeB, scopeB, false)
	if err != nil || report.Details().LocalJoined || report.Details().PendingAdmissions != 1 {
		t.Fatal("stop missed the original admission", err)
	}
	associatedTestReceive(t, ctx.Done())
	e.Stop()
	if e.Joined() {
		t.Fatal("global Joined ignored original admission return")
	}
	select {
	case err := <-done:
		t.Fatal("Run ignored original admission", err)
	default:
	}
	once.Do(func() { close(release) })
	if err := associatedTestReceive(t, returned); !errors.Is(err, context.Canceled) {
		t.Fatal("late nil read result revived a cancelled admission", err)
	}
	if err := associatedTestReceive(t, done); err != nil || !e.Joined() || effects.Load() != 0 {
		t.Fatal("late admission launched a worker", err)
	}
}

func TestAssociatedProjectStopRetainsOriginalUnknownWithoutNewStart(t *testing.T) {
	for _, stage := range []string{"preparation", "start"} {
		t.Run(stage, func(t *testing.T) {
			p, dispatch := associatedTestIdentity(t)
			cause, scope := associatedStopTarget(t, p)
			original := errors.New("original-attempt")
			unknown := f.NewFault(f.CommitUnknown, f.Unknown).WithCause(original)
			var owned atomic.Bool
			var prepares, starts, recoveries atomic.Int32
			var token any
			e := associatedTestExecutor(associatedExecutorOps{
				inspect: func(context.Context, i.ProjectID, associatedIdentity) (associatedFacts, error) {
					if stage == "start" || recoveries.Load() != 0 {
						return associatedFacts{status: c.Preparing, inputReady: true}, nil
					}
					return associatedFacts{status: c.Created}, nil
				},
				prepare: func(ctx context.Context, _ i.ExecutionID) error {
					prepares.Add(1)
					token = ctx.Value(associatedContextKey{})
					owned.Store(true)
					return unknown
				},
				start: func(ctx context.Context, _ i.ExecutionID) (c.DirectTextReceipt, error) {
					starts.Add(1)
					token = ctx.Value(associatedContextKey{})
					owned.Store(true)
					return c.DirectTextReceipt{}, unknown
				},
				recoverPreparation: func(ctx context.Context, id i.ExecutionID) error {
					if id != dispatch.ExecutionID || token == nil || ctx.Value(associatedContextKey{}) != token {
						return fault(f.Forbidden)
					}
					recoveries.Add(1)
					owned.Store(false)
					return nil
				},
				recoverStart: func(context.Context, i.ExecutionID) (c.DirectTextReceipt, error) {
					recoveries.Add(1)
					return c.DirectTextReceipt{}, errors.New("first Model call after stop")
				},
				owners: func(*associatedCall) associatedOwners {
					if !owned.Load() {
						return associatedOwners{}
					}
					return associatedOwners{preparation: stage == "preparation", direct: stage == "start", uncertainty: stage, err: unknown}
				},
			}, 1, time.Second)
			done := associatedTestRun(t, e, context.Background())
			defer e.Stop()
			if _, err := e.Advance(context.Background(), p, dispatch); err != nil {
				t.Fatal(err)
			}
			associatedTestIdle(t, e)
			e.state.mu.Lock()
			e.state.jobs[dispatch.ExecutionID].next = time.Time{}
			e.state.mu.Unlock()
			if report, err := e.state.stopProject(context.Background(), cause, scope, false); err != nil || !report.Details().Retained || report.Details().LocalJoined {
				t.Fatal("Unknown owner was dropped", err)
			}
			if got, err := e.Advance(context.Background(), p, dispatch); !errors.Is(err, original) || !got.Retained || got.Active {
				t.Fatal("visit restarted a stopped Unknown", err)
			}
			report, err := e.state.stopProject(context.Background(), cause, scope, true)
			if stage == "preparation" {
				if err != nil || !report.Details().LocalJoined || recoveries.Load() != 1 || prepares.Load() != 1 || starts.Load() != 0 {
					t.Fatal("cleanup started new business or lost original recovery", err)
				}
			} else {
				if !errors.Is(err, unknown) || !errors.Is(err, original) || recoveries.Load() != 0 || starts.Load() != 1 {
					t.Fatal("startup Unknown was advanced or replaced", err)
				}
				e.state.mu.Lock()
				job := e.state.jobs[dispatch.ExecutionID]
				kept := job != nil && job.err == unknown && job.retained
				e.state.mu.Unlock()
				if !kept {
					t.Fatal("startup original owner lost")
				}
			}
			e.Stop()
			err = associatedTestReceive(t, done)
			if stage == "start" {
				if !errors.Is(err, original) || e.Joined() {
					t.Fatal("global shutdown falsely joined retained original")
				}
			} else if err != nil || !e.Joined() {
				t.Fatal("closed original remained live", err)
			}
		})
	}
}

func TestAssociatedProjectStopControlsBelongToGlobalLifetime(t *testing.T) {
	e := associatedTestExecutor(associatedExecutorOps{}, 1, time.Second)
	done := associatedTestRun(t, e, context.Background())
	defer e.Stop()
	run, release, err := e.state.beginProjectStop(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	defer once.Do(release)
	if _, _, err := e.state.beginProjectStop(context.Background()); err == nil {
		t.Fatal("unbounded stop controls")
	}
	e.Stop()
	associatedTestReceive(t, run.Done())
	if e.Joined() {
		t.Fatal("Joined ignored original authorization/control return")
	}
	select {
	case err := <-done:
		t.Fatal("Run escaped original control", err)
	default:
	}
	once.Do(release)
	if err := associatedTestReceive(t, done); err != nil || !e.Joined() {
		t.Fatal("released control did not join", err)
	}
	if got, err := NewProjectStopper(e, nil); got != nil || err == nil {
		t.Fatal("private test executor became a public authority")
	}
	if (ProjectStopReport{}).Valid() {
		t.Fatal("zero report became permission")
	}
}
