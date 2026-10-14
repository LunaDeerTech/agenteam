package runtime

import (
	"context"
	"errors"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"testing"
	"time"
)

type callLifecycleFixture struct {
	executor    *RunnerExecutor
	binding     tc.ToolCallBinding
	call, other context.Context
	unknown     func()
	complete    func()
	stopped     func() bool
}

func newCallLifecycleFixture(t *testing.T, kind string) callLifecycleFixture {
	t.Helper()
	var v callLifecycleFixture
	switch kind {
	case "read":
		h := readFileHandoffFixture(t)
		other := readFileHandoffFixture(t)
		selected, err := exactRunnerBinding(context.Background(), h.binding)
		if err != nil {
			t.Fatal(err)
		}
		h.ctx, h.cancel = context.WithCancel(selected)
		h.done = make(chan struct{})
		other.ctx, other.cancel = context.WithCancel(context.Background())
		other.done = make(chan struct{})
		t.Cleanup(h.cancel)
		t.Cleanup(other.cancel)
		a := &readFileAuthorityState{active: map[i.ExecutionID]*readFileHandoff{h.execution: h, other.execution: other}}
		v = callLifecycleFixture{
			executor: &RunnerExecutor{read: &ReadFileExecutor{data: func() *readFileExecutorData { return &readFileExecutorData{authority: a} }}},
			binding:  h.binding, call: h.ctx, other: other.ctx,
			unknown: func() {
				h.mu.Lock()
				h.retirement = f.NewFault(f.CommitUnknown, f.Unknown)
				h.mu.Unlock()
				close(h.done)
			},
			complete: func() { a.mu.Lock(); delete(a.active, h.execution); a.mu.Unlock(); close(h.done) },
			stopped:  func() bool { a.mu.Lock(); defer a.mu.Unlock(); return a.stopped },
		}
	case "write":
		h := writeFileHandoffFixture(t)
		other := writeFileHandoffFixture(t)
		selected, err := exactRunnerBinding(context.Background(), h.binding)
		if err != nil {
			t.Fatal(err)
		}
		h.ctx, h.cancel = context.WithCancel(selected)
		h.done = make(chan struct{})
		other.ctx, other.cancel = context.WithCancel(context.Background())
		other.done = make(chan struct{})
		t.Cleanup(h.cancel)
		t.Cleanup(other.cancel)
		a := &writeFileAuthorityState{active: map[i.ExecutionID]*writeFileHandoff{h.execution: h, other.execution: other}}
		v = callLifecycleFixture{
			executor: &RunnerExecutor{write: &WriteFileExecutor{data: func() *writeFileExecutorData { return &writeFileExecutorData{authority: a} }}},
			binding:  h.binding, call: h.ctx, other: other.ctx,
			unknown: func() {
				h.mu.Lock()
				h.retirement = f.NewFault(f.CommitUnknown, f.Unknown)
				h.mu.Unlock()
				close(h.done)
			},
			complete: func() { a.mu.Lock(); delete(a.active, h.execution); a.mu.Unlock(); close(h.done) },
			stopped:  func() bool { a.mu.Lock(); defer a.mu.Unlock(); return a.stopped },
		}
	case "command":
		h := runCommandHandoffFixture(t)
		other := runCommandHandoffFixture(t)
		selected, err := exactRunnerBinding(context.Background(), h.binding)
		if err != nil {
			t.Fatal(err)
		}
		h.ctx, h.cancel = context.WithCancel(selected)
		h.done = make(chan struct{})
		other.ctx, other.cancel = context.WithCancel(context.Background())
		other.done = make(chan struct{})
		t.Cleanup(h.cancel)
		t.Cleanup(other.cancel)
		a := &runCommandAuthorityState{active: map[i.ExecutionID]*runCommandHandoff{h.execution: h, other.execution: other}}
		v = callLifecycleFixture{
			executor: &RunnerExecutor{command: &RunCommandExecutor{data: func() *runCommandExecutorData { return &runCommandExecutorData{authority: a} }}},
			binding:  h.binding, call: h.ctx, other: other.ctx,
			unknown: func() {
				h.mu.Lock()
				h.retirement = f.NewFault(f.CommitUnknown, f.Unknown)
				h.mu.Unlock()
				close(h.done)
			},
			complete: func() { a.mu.Lock(); delete(a.active, h.execution); a.mu.Unlock(); close(h.done) },
			stopped:  func() bool { a.mu.Lock(); defer a.mu.Unlock(); return a.stopped },
		}
	}
	return v
}
func TestRunnerCallCancellationIsExactAndWaitsForReturn(t *testing.T) {
	for _, kind := range []string{"read", "write", "command"} {
		t.Run(kind, func(t *testing.T) {
			v := newCallLifecycleFixture(t, kind)
			bad := v.binding
			bad.CallID = "different-original-call"
			if err := v.executor.CancelCall(context.Background(), bad); err == nil || v.call.Err() != nil {
				t.Fatal("stale call cancelled an owner")
			}
			if err := v.executor.CancelCall(context.Background(), v.binding); err != nil {
				t.Fatal(err)
			}
			if v.call.Err() != context.Canceled || v.other.Err() != nil || v.stopped() {
				t.Fatal("per-call cancellation stopped another call or global owner")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
			err := v.executor.JoinCall(ctx, v.binding)
			cancel()
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("cancel signal became actual retirement", err)
			}
			v.complete()
			if err := v.executor.JoinCall(context.Background(), v.binding); err != nil {
				t.Fatal("original return not observed", err)
			}
			if v.other.Err() != nil || v.stopped() {
				t.Fatal("joining original call stopped another execution")
			}
		})
	}
}
func TestRunnerCallJoinPreservesUnknownOwner(t *testing.T) {
	for _, kind := range []string{"read", "write", "command"} {
		t.Run(kind, func(t *testing.T) {
			v := newCallLifecycleFixture(t, kind)
			v.unknown()
			for range 2 {
				if err := v.executor.CancelCall(context.Background(), v.binding); err != nil {
					t.Fatal(err)
				}
				err := v.executor.JoinCall(context.Background(), v.binding)
				var fault *f.Fault
				if !errors.As(err, &fault) || fault.Code != f.CommitUnknown || fault.CommitState != f.Unknown {
					t.Fatal("unknown accounting was retired", err)
				}
			}
			if v.other.Err() != nil || v.stopped() {
				t.Fatal("unknown join cancelled unrelated owner")
			}
		})
	}
}
