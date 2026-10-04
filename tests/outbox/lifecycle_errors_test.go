//go:build integration

package outbox_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type lifecycleErrorProcesses struct {
	base    oc.ProcessAuthority
	mu      sync.Mutex
	confirm func(context.Context, oc.ProcessID) error
	calls   atomic.Int64
}

func (p *lifecycleErrorProcesses) CurrentProcess() oc.ProcessID { return p.base.CurrentProcess() }
func (p *lifecycleErrorProcesses) ConfirmStopped(ctx context.Context, id oc.ProcessID) error {
	p.calls.Add(1)
	p.mu.Lock()
	fn := p.confirm
	p.mu.Unlock()
	if fn == nil {
		return foundation.NewFault(foundation.ResourceBusy, foundation.NotStarted)
	}
	return fn(ctx, id)
}
func (p *lifecycleErrorProcesses) set(fn func(context.Context, oc.ProcessID) error) {
	p.mu.Lock()
	p.confirm = fn
	p.mu.Unlock()
}

type lifecycleErrorFixture struct {
	f           *fixture
	svc         *outbox.Service
	processes   *lifecycleErrorProcesses
	actor       identity.Actor
	cause       oc.LifecycleCause
	foreign     []event.Event
	deliveries  []oc.DeliveryID
	owners      []oc.ProcessID
	independent event.Event
}

func prepareLifecycleErrors(t *testing.T, f *fixture, step string, count int, stores ...outbox.Store) lifecycleErrorFixture {
	t.Helper()
	p := &lifecycleErrorProcesses{base: f.auth}
	svc, _ := lifecycleService(t, f, p, stores...)
	h := f.handler("lifecycle.errors")
	definition := h.definition(1)
	definition.Effect = oc.DomainIngress
	if _, err := svc.RegisterHandler(ctxFor(t), definition); err != nil {
		t.Fatal(err)
	}
	result := lifecycleErrorFixture{f: f, svc: svc, processes: p}
	for range count {
		e := distinctEvent(t, f, "protected lifecycle fact")
		_, commit := f.append(t, svc, f.store, f.actor, e)
		state(t, commit, foundation.Committed)
		d := f.delivery(t, e, string(h.name))
		f.claim(t, d)
		owner := id[oc.Process](t)
		f.sql(t, `UPDATE agenteam_outbox.attempts SET process_id=$1 WHERE delivery_id=$2`, owner.String(), d.String())
		result.foreign = append(result.foreign, e)
		result.deliveries = append(result.deliveries, d)
		result.owners = append(result.owners, owner)
	}
	result.actor, result.cause = lifecycleCause(t, f, oc.DeleteProject, 2)
	initial, err := svc.RequestStop(ctxFor(t), result.actor, result.cause)
	if err != nil || initial.Stopped || p.calls.Load() != int64(count) {
		t.Fatalf("protected baseline not established: %+v %v calls=%d", initial, err, p.calls.Load())
	}
	// A real empty public scan wraps the persisted cursor; the fault batch then
	// includes the protected prefix and a separately authorized stopping fact.
	if _, err = svc.InspectStop(ctxFor(t), result.actor, result.cause); err != nil {
		t.Fatal(err)
	}
	result.independent = distinctEvent(t, f, "independent stopping fact")
	_, commit := f.append(t, svc, f.store, result.actor, result.independent)
	state(t, commit, foundation.Committed)
	if step == "cleanup" {
		f.sql(t, `UPDATE outbox_fixture.lifecycle SET others=true WHERE project_id=$1`, f.project.String())
	}
	return result
}

func (f lifecycleErrorFixture) run(ctx context.Context, step string) (bool, error) {
	switch step {
	case "stop":
		r, err := f.svc.RequestStop(ctx, f.actor, f.cause)
		return r.Stopped, err
	case "inspect":
		r, err := f.svc.InspectStop(ctx, f.actor, f.cause)
		return r.Stopped, err
	default:
		r, err := f.svc.Cleanup(ctx, f.actor, f.cause)
		return r.Completed, err
	}
}
func (f lifecycleErrorFixture) check(t *testing.T, step string, progressed bool) {
	t.Helper()
	for i, e := range f.foreign {
		if f.f.count(t, `SELECT count(*) FROM agenteam_outbox.events WHERE id=$1`, e.Header().EventID.String()) != 1 || f.f.count(t, `SELECT count(*) FROM agenteam_outbox.attempts WHERE delivery_id=$1 AND finished_at IS NULL AND joined_at IS NULL`, f.deliveries[i].String()) != 1 {
			t.Fatal("unproved foreign work was deleted or retired")
		}
	}
	var actual bool
	if step == "cleanup" {
		actual = f.f.count(t, `SELECT count(*) FROM agenteam_outbox.events WHERE id=$1`, f.independent.Header().EventID.String()) == 0
	} else {
		actual = f.f.count(t, `SELECT count(*) FROM agenteam_outbox.deliveries WHERE event_id=$1 AND phase='dead_letter' AND safe_reason='project_stopped'`, f.independent.Header().EventID.String()) == 1
	}
	if actual != progressed {
		t.Fatalf("independent persisted progress=%v wanted=%v", actual, progressed)
	}
	if f.f.count(t, `SELECT count(*) FROM agenteam_outbox.project_lifecycle WHERE project_id=$1 AND phase='completed'`, f.f.project.String()) != 0 {
		t.Fatal("protected work reported completed")
	}
}

func TestOutboxLifecycleProofHardErrorsRemainVisibleAfterProgress(t *testing.T) {
	for _, step := range []string{"stop", "inspect", "cleanup"} {
		t.Run(step, func(t *testing.T) {
			f := prepareLifecycleErrors(t, newFixture(t), step, 2)
			first, second := errors.New("first proof failure"), errors.New("second proof failure")
			f.processes.set(func(ctx context.Context, owner oc.ProcessID) error {
				if owner == f.owners[0] {
					// A deadline does not erase an independently returned hard error.
					<-ctx.Done()
					return foundation.NewFault(foundation.InternalError, foundation.NotStarted).WithCause(first)
				}
				return foundation.NewFault(foundation.InvalidState, foundation.NotStarted).WithCause(second)
			})
			calls := f.processes.calls.Load()
			completed, err := f.run(ctxFor(t), step)
			code(t, err, foundation.InternalError)
			if completed || !errors.Is(err, first) || errors.Is(err, second) || f.processes.calls.Load()-calls != 2 {
				t.Fatal("first hard proof error or bounded independent inspection lost")
			}
			f.check(t, step, true)
		})
	}
}

func TestOutboxLifecycleProofRefusalsAndItemDeadlineRemainProtected(t *testing.T) {
	for _, step := range []string{"stop", "inspect", "cleanup"} {
		t.Run(step, func(t *testing.T) {
			f := prepareLifecycleErrors(t, newFixture(t), step, 4)
			f.processes.set(func(ctx context.Context, owner oc.ProcessID) error {
				for i, code := range []foundation.Code{foundation.ResourceBusy, foundation.NotFound, foundation.DependencyUnavailable} {
					if owner == f.owners[i] {
						return foundation.NewFault(code, foundation.NotStarted)
					}
				}
				<-ctx.Done()
				return ctx.Err()
			})
			calls := f.processes.calls.Load()
			ctx := ctxFor(t)
			completed, err := f.run(ctx, step)
			if err != nil || completed || ctx.Err() != nil || f.processes.calls.Load()-calls != 4 {
				t.Fatalf("unproved or item-budget claim turned into a hard failure: %v", err)
			}
			f.check(t, step, true)
		})
	}
}

func TestOutboxLifecycleCurrentFailureOverridesEarlierProofError(t *testing.T) {
	for _, step := range []string{"stop", "inspect", "cleanup"} {
		for _, failure := range []string{"authorization", "database", "parent-cancel"} {
			t.Run(step+"/"+failure, func(t *testing.T) {
				f := prepareLifecycleErrors(t, newFixture(t), step, 2)
				first := errors.New("earlier proof failure")
				ctx, cancel := context.WithCancel(ctxFor(t))
				defer cancel()
				f.processes.set(func(context.Context, oc.ProcessID) error {
					switch failure {
					case "authorization":
						f.f.sql(t, `UPDATE outbox_fixture.lifecycle SET actor_cause='revoked' WHERE project_id=$1`, f.f.project.String())
					case "database":
						// Applied once: the next transaction must see the real missing
						// relation and cannot return the earlier ProcessAuthority fault.
						f.f.sql(t, `ALTER TABLE agenteam_outbox.project_lifecycle RENAME TO lifecycle_missing`)
						f.processes.set(func(context.Context, oc.ProcessID) error {
							return foundation.NewFault(foundation.InternalError, foundation.NotStarted).WithCause(first)
						})
					case "parent-cancel":
						cancel()
					}
					return foundation.NewFault(foundation.InternalError, foundation.NotStarted).WithCause(first)
				})
				calls := f.processes.calls.Load()
				completed, err := f.run(ctx, step)
				if failure == "database" {
					f.f.sql(t, `ALTER TABLE agenteam_outbox.lifecycle_missing RENAME TO project_lifecycle`)
				}
				if err == nil || completed || errors.Is(err, first) {
					t.Fatalf("current operation failure hidden by proof aggregation: %v", err)
				}
				switch failure {
				case "authorization":
					code(t, err, foundation.Forbidden)
				case "database":
					var state interface{ SQLState() string }
					if !errors.As(err, &state) || state.SQLState() != "42P01" {
						t.Fatalf("real missing-relation error lost: %v", err)
					}
				case "parent-cancel":
					if !errors.Is(err, context.Canceled) || f.processes.calls.Load()-calls != 1 {
						t.Fatal("parent cancellation did not stop the next item")
					}
				}
				f.check(t, step, false)
			})
		}
	}
}

// Only the chosen real transaction is armed. It retains the PostgreSQL writer
// through the existing TCP proxy; no CommitResult or authorization is faked.
type lifecycleErrorCommitStore struct {
	*postgres.Store
	proxy   *commitProxy
	phase   string
	enabled atomic.Bool
	fired   atomic.Bool
	after   func()
}

func (s *lifecycleErrorCommitStore) WithinTx(ctx context.Context, cause foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	result := s.Store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if err := fn(ctx, tx); err != nil {
			return err
		}
		if s.proxy != nil && s.enabled.Load() && cause.Details().Owner == s.phase && s.fired.CompareAndSwap(false, true) {
			s.proxy.armed.Store(true)
		}
		return nil
	})
	if s.after != nil && s.enabled.Load() && result.State() == foundation.Committed && cause.Details().Owner == s.phase && s.fired.CompareAndSwap(false, true) {
		s.after()
	}
	return result
}

func TestOutboxLifecycleCommitUnknownOverridesEarlierProofError(t *testing.T) {
	for _, step := range []string{"stop", "inspect", "cleanup"} {
		t.Run(step, func(t *testing.T) {
			base := newFixture(t)
			copy, _, proxy := administrationProxy(t, base, true, "unused")
			phase := "outbox.stop-progress"
			if step == "cleanup" {
				phase = "outbox.cleanup-batch"
			}
			store := &lifecycleErrorCommitStore{Store: copy.store, proxy: proxy, phase: phase}
			f := prepareLifecycleErrors(t, copy, step, 1, store)
			first := errors.New("earlier proof failure")
			f.processes.set(func(context.Context, oc.ProcessID) error {
				return foundation.NewFault(foundation.InternalError, foundation.NotStarted).WithCause(first)
			})
			store.enabled.Store(true)
			ctx := ctxFor(t)
			done := make(chan error, 1)
			go func() { _, err := f.run(ctx, step); done <- err }()
			reached(t, proxy.reached)
			var err error
			select {
			case err = <-done:
			case <-ctx.Done():
				t.Fatal("unknown transaction did not return")
			}
			code(t, err, foundation.CommitUnknown)
			var fault *foundation.Fault
			if !errors.As(err, &fault) || fault.CommitState != foundation.Unknown || errors.Is(err, first) {
				t.Fatal("unknown checkpoint replaced by proof failure")
			}
			f.check(t, step, false)
			close(proxy.release)
			reached(t, proxy.completed)
			f.check(t, step, true)
		})
	}
}

func TestOutboxLifecycleFinalAuthorizationOverridesEarlierProofError(t *testing.T) {
	for _, step := range []string{"stop", "inspect"} {
		t.Run(step, func(t *testing.T) {
			base := newFixture(t)
			store := &lifecycleErrorCommitStore{Store: base.store, phase: "outbox.stop-progress"}
			f := prepareLifecycleErrors(t, base, step, 1, store)
			first := errors.New("earlier proof failure")
			f.processes.set(func(context.Context, oc.ProcessID) error {
				return foundation.NewFault(foundation.InternalError, foundation.NotStarted).WithCause(first)
			})
			store.after = func() {
				base.sql(t, `UPDATE outbox_fixture.lifecycle SET actor_cause='revoked' WHERE project_id=$1`, base.project.String())
			}
			store.enabled.Store(true)
			completed, err := f.run(ctxFor(t), step)
			code(t, err, foundation.Forbidden)
			if completed || errors.Is(err, first) || !store.fired.Load() {
				t.Fatal("final current authorization hidden by earlier proof failure")
			}
			f.check(t, step, true)
		})
	}
}
