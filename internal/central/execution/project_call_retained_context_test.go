package execution

import (
	"context"
	"errors"
	"testing"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// Exercise the original finish/returned/borrow/stop ownership over controlled
// call records. This does not claim a PostgreSQL business outcome or join.
func retainedContextEntry(t *testing.T, fixture *executionCallsFixture, parent context.Context) (*projectCallEntry, *directTextCall) {
	t.Helper()
	s := fixture.pair.state
	ctx, cancel := context.WithCancel(parent)
	e := &projectCallEntry{record: projectCallRecord{ID: newTestID[projectCallID](t), Project: fixture.p, Lane: projectLaneDirect}, execution: newTestID[i.Execution](t), cancel: cancel, registration: f.CommittedResult(), registrationKnown: true}
	e.ctx = context.WithValue(ctx, projectCallContextKey{}, projectCallWitness{s, e})
	runCtx, runCancel := context.WithCancel(e.ctx)
	run := &directTextCall{ctx: runCtx, cancel: runCancel, request: c.PreparationRequest{ExecutionID: e.execution}, returned: true, unresolved: fault(f.DependencyUnavailable)}
	s.mu.Lock()
	s.owners[e.record.ID] = e
	s.mu.Unlock()
	s.direct.state.mu.Lock()
	s.direct.state.calls[e.execution] = run
	s.direct.state.mu.Unlock()
	t.Cleanup(func() {
		runCancel()
		s.direct.state.mu.Lock()
		delete(s.direct.state.calls, e.execution)
		s.direct.state.mu.Unlock()
		s.mu.Lock()
		e.closed, e.returned, e.settling = true, true, true
		s.mu.Unlock()
		s.retireProjectEntry(e)
		cancel()
	})
	if err := s.finish(e, run.unresolved); !errors.Is(err, run.unresolved) {
		t.Fatal("original retained error changed", err)
	}
	s.returned(e)
	if run.ctx.Err() != nil || e.retainedCancel == nil || !e.returned {
		t.Fatal("returned callback cancelled its retained run")
	}
	return e, run
}

func TestExecutionProjectCallRetainedContextRecoveryAndRetirement(t *testing.T) {
	v := newExecutionCalls(t)
	e, run := retainedContextEntry(t, v, context.Background())
	s := v.pair.state
	for range 2 {
		s.mu.Lock()
		s.borrowProjectCallLocked(context.Background(), e, "resolve_unknown")
		borrowed := e.ctx
		s.mu.Unlock()
		s.finish(e, run.unresolved)
		s.returned(e)
		if !errors.Is(borrowed.Err(), context.Canceled) || run.ctx.Err() != nil {
			t.Fatal("recovery return failed to release only its temporary context")
		}
	}
	s.mu.Lock()
	e.closed, e.settling = true, true
	s.mu.Unlock()
	s.retireProjectEntry(e)
	if !errors.Is(run.ctx.Err(), context.Canceled) || e.retainedCancel != nil {
		t.Fatal("retirement left original context owned")
	}
}

func TestExecutionProjectCallRetainedContextCancellation(t *testing.T) {
	for _, mode := range []string{"parent", "lane", "project", "global"} {
		t.Run(mode, func(t *testing.T) {
			v := newExecutionCalls(t)
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			e, run := retainedContextEntry(t, v, parent)
			// Stop must reach the original handle even while a new call owns e.ctx.
			v.pair.state.mu.Lock()
			v.pair.state.borrowProjectCallLocked(context.Background(), e, "resolve_unknown")
			v.pair.state.mu.Unlock()
			switch mode {
			case "parent":
				cancel()
			case "lane":
				v.pair.state.stopLane(projectLaneDirect)
			case "project":
				v.pair.state.cancelProjectCalls(v.p)
			case "global":
				v.pair.Stop()
			}
			if !errors.Is(run.ctx.Err(), context.Canceled) {
				t.Fatal("original cancellation did not reach retained run")
			}
			v.pair.state.finish(e, run.unresolved)
			v.pair.state.returned(e)
		})
	}
}

func TestExecutionProjectCallRetainedParentContext(t *testing.T) {
	v := newExecutionCalls(t)
	s := v.pair.state
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	parent := &projectCallEntry{record: projectCallRecord{ID: newTestID[projectCallID](t), Project: v.p, Lane: projectLaneAssociated}, cancel: cancel, registration: f.CommittedResult(), registrationKnown: true}
	parent.ctx = context.WithValue(ctx, projectCallContextKey{}, projectCallWitness{s, parent})
	s.owners[parent.record.ID] = parent
	child, run := retainedContextEntry(t, v, parent.ctx)
	s.mu.Lock()
	child.parent = parent
	parent.children = []*projectCallEntry{child}
	parent.childOutcomes = []error{run.unresolved}
	s.mu.Unlock()
	s.finish(parent, run.unresolved)
	s.returned(parent)
	if run.ctx.Err() != nil || parent.retainedCancel == nil || parent.outcome == nil || !parent.outcome.parentOnly {
		t.Fatal("returned associated parent cancelled its original retained child")
	}
	// Close only the controlled technical parent; the child keeps its own
	// retained record until the cleanup registered above retires it.
	s.mu.Lock()
	parent.closed, parent.settling = true, true
	s.mu.Unlock()
	s.retireProjectEntry(parent)
	if !errors.Is(run.ctx.Err(), context.Canceled) {
		t.Fatal("parent retirement lost its original cancellation handle")
	}
}
