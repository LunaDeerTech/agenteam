package outbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

func localRuntime() *runtimeState {
	s := &serviceState{}
	r := &runtimeState{svc: &Service{data: func() *serviceState { return s }}, changed: make(chan struct{}), wake: make(chan struct{}, 1), active: make(map[oc.AttemptID]*execution), controlCancels: make(map[uint64]context.CancelFunc)}
	r.options, _ = (Options{}).normalized()
	return r
}

func TestRecoveryBudgetPreservesIndependentHardFailureAndUnknown(t *testing.T) {
	parent := context.Background()
	expired, cancel := context.WithDeadline(parent, time.Now().Add(-time.Second))
	defer cancel()
	hard := unavailable(&pgconn.PgError{Code: "42P01", Message: "private missing structure"})
	if recoveryBudgetError(expired, parent, hard) != hard {
		t.Fatal("deadline hid actual structural failure")
	}
	unknown := foundation.NewFault(foundation.CommitUnknown, foundation.Unknown).WithCause(context.DeadlineExceeded)
	if recoveryBudgetError(expired, parent, unknown) != unknown {
		t.Fatal("unknown commit changed outcome")
	}
	for _, err := range []error{context.DeadlineExceeded, unavailable(&pgconn.PgError{Code: "57014", Message: "private query cancelled"})} {
		if safeFaultCode(recoveryBudgetError(expired, parent, err)) != foundation.ResourceBusy {
			t.Fatal("cancelled bounded inspection not protected")
		}
	}
}
func TestRuntimeJoinRequiresEveryActualOwner(t *testing.T) {
	r := localRuntime()
	control, release, err := r.beginControl(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	id, _ := foundation.NewID[oc.Attempt]()
	task, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	r.active[id] = &execution{id: id, cancel: cancel, done: done}
	r.workerDone = make(chan struct{})
	ctx, end := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer end()
	if err = r.Force(ctx); err == nil {
		t.Fatal("unjoined owners reported drained")
	}
	if control.Err() != context.Canceled || task.Err() != context.Canceled {
		t.Fatal("force did not cancel all owners")
	}
	if r.Joined() {
		t.Fatal("deadline is not join proof")
	}
	release()
	close(done)
	if r.Joined() {
		t.Fatal("worker still owns cleanup")
	}
	close(r.workerDone)
	if !r.Joined() {
		t.Fatal("all real done channels closed")
	}
	if _, _, err = r.beginControl(context.Background()); err == nil {
		t.Fatal("late technical I/O admitted")
	}
}
func TestRuntimeForceNeverRenewsCheckpointBudget(t *testing.T) {
	r := localRuntime()
	first, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if err := r.Force(first); err != nil {
		t.Fatal(err)
	}
	later, end := context.WithTimeout(context.Background(), time.Hour)
	defer end()
	if err := r.Force(later); err != nil {
		t.Fatal(err)
	}
	checkpoint, finish := r.checkpointContext(context.Background())
	defer finish()
	if !errors.Is(checkpoint.Err(), context.DeadlineExceeded) {
		t.Fatal("force deadline refreshed")
	}
	want, _ := first.Deadline()
	got, _ := checkpoint.Deadline()
	if !got.Equal(want) {
		t.Fatal("original expired deadline not preserved")
	}
}
func TestRuntimeOnlyAllowsTighterLimits(t *testing.T) {
	for _, o := range []Options{{Concurrent: 5}, {PerHandler: 3}, {Batch: 65}, {AttemptTimeout: 31 * time.Second}, {QueryTimeout: 3 * time.Second}, {RetryBase: 2 * time.Second}, {PollInterval: -1}} {
		if _, err := o.normalized(); err == nil {
			t.Fatalf("increased/unbounded limit accepted: %#v", o)
		}
	}
	if _, err := (Options{Concurrent: 1, PerHandler: 1, Batch: 1, AttemptTimeout: time.Millisecond, PollInterval: time.Millisecond}).normalized(); err != nil {
		t.Fatal(err)
	}
}
