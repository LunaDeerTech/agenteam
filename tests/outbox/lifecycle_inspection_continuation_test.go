//go:build integration

package outbox_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// The actual Service/PG/runtime remain real. This formal authority boundary
// supplies distinct lock plans and injects revocation at a precise discovery.
type inspectionBoundary struct {
	*lifecycleAuthority
	inspectKey, stopKey foundation.LockKey
	denyStop, failPlan  bool
	onStopPlan          func()
	stopPlans           int
	mu                  sync.Mutex
	validated           map[foundation.Tx][]oc.LifecycleStep
}

func (a *inspectionBoundary) Discover(ctx context.Context, r oc.ProjectRequest) (oc.Dependencies, error) {
	d, err := a.lifecycleAuthority.Discover(ctx, r)
	if err != nil || r.Details().Kind != oc.LifecycleProject {
		return d, err
	}
	key := a.inspectKey
	if r.Details().LifecycleStep == oc.LifecycleStop {
		a.stopPlans++
		key = a.stopKey
		if a.onStopPlan != nil {
			a.onStopPlan()
		}
		if a.failPlan {
			return oc.Dependencies{}, foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
		}
	}
	return oc.NewDependencies(a.issuer, d.Binding(), append(d.Locks(), foundation.LockRequest{Key: key, Mode: foundation.Shared}), d.Opaque())
}
func (a *inspectionBoundary) ValidateInTx(ctx context.Context, tx foundation.Tx, r oc.ProjectRequest, d oc.Dependencies) error {
	if err := a.lifecycleAuthority.ValidateInTx(ctx, tx, r, d); err != nil {
		return err
	}
	if r.Details().Kind != oc.LifecycleProject {
		return nil
	}
	a.mu.Lock()
	if a.validated == nil {
		a.validated = make(map[foundation.Tx][]oc.LifecycleStep)
	}
	a.validated[tx] = append(a.validated[tx], r.Details().LifecycleStep)
	a.mu.Unlock()
	key := a.inspectKey
	if r.Details().LifecycleStep == oc.LifecycleStop {
		key = a.stopKey
	}
	if err := a.base.store.RequireHeldLocks(ctx, tx, []foundation.LockRequest{{Key: key, Mode: foundation.Shared}}); err != nil {
		return err
	}
	if r.Details().LifecycleStep == oc.LifecycleStop && a.denyStop {
		return foundation.NewFault(foundation.InvalidState, foundation.NotStarted)
	}
	return nil
}

type inspectionUnionStore struct {
	*postgres.Store
	mu           sync.Mutex
	acquires     map[foundation.Tx]int
	transactions int
}

func (s *inspectionUnionStore) WithinTx(ctx context.Context, c foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	return s.Store.WithinTx(ctx, c, func(ctx context.Context, tx foundation.Tx) error {
		if c.Details().Owner != "outbox.stop-observe" && c.Details().Owner != "outbox.stop-authorize" && c.Details().Owner != "outbox.stop-gate" && c.Details().Owner != "outbox.stop-progress" && c.Details().Owner != "outbox.stop-inspect" {
			return fn(ctx, tx)
		}
		s.mu.Lock()
		s.acquires[tx] = 0
		s.transactions++
		s.mu.Unlock()
		err := fn(ctx, tx)
		s.mu.Lock()
		n := s.acquires[tx]
		delete(s.acquires, tx)
		s.mu.Unlock()
		if n != 1 {
			return fmt.Errorf("lifecycle physical transaction acquired %d unions", n)
		}
		return err
	})
}
func (s *inspectionUnionStore) AcquireAll(ctx context.Context, tx foundation.Tx, locks []foundation.LockRequest) error {
	s.mu.Lock()
	if _, ok := s.acquires[tx]; ok {
		s.acquires[tx]++
	}
	s.mu.Unlock()
	return s.Store.AcquireAll(ctx, tx, locks)
}
func inspectionGate(t *testing.T, f *fixture, c oc.LifecycleCause) {
	t.Helper()
	d := c.Details()
	f.sql(t, `INSERT INTO agenteam_outbox.project_lifecycle(project_id,operation_id,action,project_version,phase,scan_sequence,recovery_pass) VALUES($1,$2,$3,$4,'stopping',0,0)`, d.ProjectID.String(), d.OperationID.String(), string(d.Action), int64(d.ProjectVersion))
}
func inspectionSnapshot(t *testing.T, f *fixture) string {
	t.Helper()
	var snapshot string
	if err := f.store.QueryRow(ctxFor(t), `SELECT jsonb_build_array(
(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY project_id),'[]') FROM agenteam_outbox.project_lifecycle t),
(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id),'[]') FROM agenteam_outbox.deliveries t),
(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id),'[]') FROM agenteam_outbox.attempts t),
(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY delivery_id),'[]') FROM agenteam_outbox.processed t))::text`).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestOutboxLifecycleInspectionContinuationRechecks(t *testing.T) {
	for _, mode := range []string{"phase revoked after Stop discovery", "Stop discovery failure", "terminal wins after observation", "full union and actual join"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			_, base := lifecycleService(t, f, f.auth)
			inspectKey, _ := foundation.AggregateLock(foundation.ExecutionAggregate, "01900000-0000-7000-8000-000000000062")
			stopKey, _ := foundation.AggregateLock(foundation.ExecutionAggregate, "01900000-0000-7000-8000-000000000061")
			a := &inspectionBoundary{lifecycleAuthority: base, inspectKey: inspectKey, stopKey: stopKey}
			store := &inspectionUnionStore{Store: f.store, acquires: map[foundation.Tx]int{}}
			s, err := outbox.New(store, f.cat, outbox.Authorizations{Projects: a, Processes: f.auth, Producers: map[event.StableName]oc.ProducerAuthority{"fixture": lifecycleProducer{base}}})
			if err != nil {
				t.Fatal(err)
			}
			h := captureHandler(f)
			defer h.unblock(0)
			defer h.unblock(1)
			r := ownedRuntime(t, s, []oc.HandlerDefinition{h.definition()})
			if err = r.Start(ctxFor(t)); err != nil {
				t.Fatal(err)
			}
			_, commit := f.append(t, s, f.store, f.actor, f.event(t, true, 1, "inspection original callback"))
			state(t, commit, foundation.Committed)
			run := waitStopRun(t, h)
			actor, cause := lifecycleCause(t, f, oc.ArchiveProject, 2)
			inspectionGate(t, f, cause)
			before := inspectionSnapshot(t, f)
			switch mode {
			case "phase revoked after Stop discovery":
				a.onStopPlan = func() { a.denyStop = true }
			case "Stop discovery failure":
				a.failPlan = true
			case "terminal wins after observation":
				a.onStopPlan = func() {
					a.denyStop = true
					f.sql(t, `UPDATE agenteam_outbox.project_lifecycle SET phase='stopped' WHERE project_id=$1`, f.project.String())
					before = inspectionSnapshot(t, f)
				}
			}
			if mode == "full union and actual join" {
				type result struct {
					report oc.StopReport
					err    error
				}
				done := make(chan result, 1)
				go func() { report, err := s.InspectStop(ctxFor(t), actor, cause); done <- result{report, err} }()
				select {
				case <-run.ctx.Done():
				case <-ctxFor(t).Done():
					t.Fatal("authorized callback was not cancelled")
				}
				waitDB(t, f.db.Connect(t), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND cardinality(pg_blocking_pids(pid))>0)`)
				select {
				case got := <-done:
					t.Fatal("returned before original writer joined", got.report, got.err)
				default:
				}
				h.unblock(0)
				select {
				case got := <-done:
					if got.err != nil {
						t.Fatal(got.err)
					}
				case <-ctxFor(t).Done():
					t.Fatal("joined inspection did not return")
				}
				waitOutbox(t, func() bool {
					report, err := s.InspectStop(ctxFor(t), actor, cause)
					if err != nil {
						t.Fatal(err)
					}
					return report.Stopped
				})
			} else {
				report, err := s.InspectStop(ctxFor(t), actor, cause)
				if mode == "terminal wins after observation" {
					if err != nil || !report.Stopped {
						t.Fatal(report, err)
					}
				} else {
					if err == nil || report.Stopped {
						t.Fatal("revoked inspection succeeded", report, err)
					}
				}
				if run.ctx.Err() != nil || inspectionSnapshot(t, f) != before {
					t.Fatal("denied/terminal inspection cancelled or wrote")
				}
				h.unblock(0)
			}
			r.StopClaims()
			if err = r.Drain(ctxFor(t)); err != nil {
				t.Fatal(err)
			}
			if a.stopPlans == 0 || store.transactions == 0 {
				t.Fatal("continuation boundary unexercised")
			}
			if mode == "full union and actual join" {
				a.mu.Lock()
				dual := 0
				for _, steps := range a.validated {
					if len(steps) == 2 && steps[0] == oc.LifecycleInspect && steps[1] == oc.LifecycleStop {
						dual++
						continue
					}
					if len(steps) != 1 || steps[0] != oc.LifecycleInspect {
						t.Errorf("wrong same-Tx validation sequence: %v", steps)
					}
				}
				a.mu.Unlock()
				if dual < 4 {
					t.Fatalf("preflight/gate/progress/report not all dual-validated: %d", dual)
				}
			}
		})
	}
}

func TestOutboxLifecycleInspectionUnknownAndJoin(t *testing.T) {
	for _, stage := range []string{"outbox.stop-observe", "outbox.stop-authorize"} {
		for _, serverCommit := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/commit=%t", stage, serverCommit), func(t *testing.T) {
				f := newFixture(t)
				copy, store, proxy := administrationProxy(t, f, serverCommit, stage)
				s, _ := lifecycleService(t, copy, copy.auth, store)
				h := captureHandler(copy)
				defer h.unblock(0)
				defer h.unblock(1)
				r := ownedRuntime(t, s, []oc.HandlerDefinition{h.definition()})
				if err := r.Start(ctxFor(t)); err != nil {
					t.Fatal(err)
				}
				_, commit := f.append(t, s, copy.store, f.actor, f.event(t, true, 1, "unknown inspection"))
				state(t, commit, foundation.Committed)
				run := waitStopRun(t, h)
				actor, cause := lifecycleCause(t, f, oc.ArchiveProject, 2)
				inspectionGate(t, f, cause)
				before := inspectionSnapshot(t, f)
				done := make(chan error, 1)
				go func() { _, err := s.InspectStop(ctxFor(t), actor, cause); done <- err }()
				reached(t, proxy.reached)
				select {
				case err := <-done:
					code(t, err, foundation.CommitUnknown)
				case <-ctxFor(t).Done():
					t.Fatal("unknown inspection did not return")
				}
				if run.ctx.Err() != nil || inspectionSnapshot(t, f) != before {
					t.Fatal("unknown cancelled or wrote")
				}
				close(proxy.release)
				reached(t, proxy.completed)
				f.sql(t, `UPDATE outbox_fixture.lifecycle SET version=3 WHERE project_id=$1`, f.project.String())
				_, err := s.InspectStop(ctxFor(t), actor, cause)
				code(t, err, foundation.Forbidden)
				if run.ctx.Err() != nil || inspectionSnapshot(t, f) != before {
					t.Fatal("stale retry cancelled or wrote")
				}
				f.sql(t, `UPDATE outbox_fixture.lifecycle SET version=2 WHERE project_id=$1`, f.project.String())
				h.unblock(0)
				r.StopClaims()
				if err = r.Drain(ctxFor(t)); err != nil {
					t.Fatal(err)
				}
				// Use another real Service after this runtime drains; the retained
				// exact terminal attempt, not time or cancellation, permits progress.
				base := &lifecycleAuthority{base: copy.auth, issuer: oc.NewPlanIssuer()}
				final, err := outbox.New(copy.store, copy.cat, outbox.Authorizations{Projects: base, Processes: copy.auth})
				if err != nil {
					t.Fatal(err)
				}
				report, err := final.InspectStop(ctxFor(t), actor, cause)
				if err != nil || !report.Stopped {
					t.Fatal("confirmed retry", report, err)
				}
			})
		}
	}
}
