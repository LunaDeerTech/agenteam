package outbox

import (
	"context"
	"errors"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

type inspectionTestAuthority struct {
	store                                *terminalInspectStore
	issuer                               oc.PlanIssuer
	stopPlans, stopChecks, inspectChecks int
	denyStop, failPlan                   bool
	onStopPlan                           func()
	inspectLock, stopLock                foundation.LockKey
}

func (a *inspectionTestAuthority) Discover(_ context.Context, r oc.ProjectRequest) (oc.Dependencies, error) {
	key := a.inspectLock
	if r.Details().LifecycleStep == oc.LifecycleStop {
		a.stopPlans++
		key = a.stopLock
		if a.onStopPlan != nil {
			a.onStopPlan()
		}
		if a.failPlan {
			return oc.Dependencies{}, failure(foundation.Forbidden, nil)
		}
	}
	binding, _ := oc.LifecycleBinding(r)
	return oc.NewDependencies(a.issuer, binding, []foundation.LockRequest{{Key: key, Mode: foundation.Shared}}, nil)
}
func (a *inspectionTestAuthority) ValidateInTx(_ context.Context, tx foundation.Tx, r oc.ProjectRequest, d oc.Dependencies) error {
	binding, _ := oc.LifecycleBinding(r)
	if tx != a.store.tx || !d.Matches(a.issuer, binding) {
		return errors.New("foreign transaction/plan")
	}
	for _, required := range d.Locks() {
		found := false
		for _, held := range a.store.locks {
			if required.Key.Canonical() == held.Key.Canonical() {
				found = true
			}
		}
		if !found {
			return errors.New("incomplete union")
		}
	}
	if r.Details().LifecycleStep == oc.LifecycleStop {
		a.stopChecks++
		if a.denyStop {
			return failure(foundation.InvalidState, nil)
		}
	} else {
		a.inspectChecks++
	}
	return nil
}
func inspectionTestBinding(s *Service, store *terminalInspectStore) *inspectionTestAuthority {
	inspect, _ := foundation.ProjectLock("01900000-0000-7000-8000-000000000090")
	stop, _ := foundation.ProjectLock("01900000-0000-7000-8000-000000000091")
	a := &inspectionTestAuthority{store: store, issuer: oc.NewPlanIssuer(), inspectLock: inspect, stopLock: stop}
	s.state().auth.Projects = a
	return a
}

func TestOutboxLifecycleInspectionContinuationRechecks(t *testing.T) {
	for _, mode := range []string{"terminal", "denied", "planning failure", "phase revoked after plan", "terminal before preflight", "terminal before gate", "terminal before progress", "terminal before report"} {
		t.Run(mode, func(t *testing.T) {
			s, store, _, actor, cause, cancelled := terminalInspectFixture(t, oc.DeleteProject, "stopping")
			a := inspectionTestBinding(s, store)
			switch mode {
			case "terminal":
				store.row.phase = "stopped"
				a.failPlan = true
			case "denied":
				a.denyStop = true
			case "planning failure":
				a.failPlan = true
			case "phase revoked after plan":
				a.onStopPlan = func() { a.denyStop = true }
			case "terminal before preflight":
				a.onStopPlan = func() { store.row.phase = "completed"; a.denyStop = true }
			case "terminal before gate":
				store.beforeTx = func(c foundation.TransactionCause) {
					if c.Details().Owner == "outbox.stop-gate" {
						store.row.phase = "completed"
						a.denyStop = true
					}
				}
			}
			if mode == "terminal before progress" || mode == "terminal before report" {
				p, err := s.planLifecycle(context.Background(), actor, cause, oc.LifecycleInspect)
				if err != nil {
					t.Fatal(err)
				}
				stop, err := s.planLifecycle(context.Background(), actor, cause, oc.LifecycleStop)
				if err != nil {
					t.Fatal(err)
				}
				p.continuation = &stop
				store.row.phase = "completed"
				a.denyStop = true
				if mode == "terminal before progress" {
					terminal, err := s.progressLifecycleStop(context.Background(), p, nil, nil, p.locks(foundation.Exclusive))
					if err != nil || !terminal {
						t.Fatal(terminal, err)
					}
				} else {
					report, err := s.stopReport(context.Background(), p)
					if err != nil || !report.Stopped {
						t.Fatal(report, err)
					}
				}
				if a.stopChecks != 0 {
					t.Fatal("terminal demanded Stop")
				}
			} else {
				report, err := s.InspectStop(context.Background(), actor, cause)
				denied := mode == "denied" || mode == "planning failure" || mode == "phase revoked after plan"
				if denied && (err == nil || report.Stopped) || !denied && (err != nil || !report.Stopped) {
					t.Fatalf("report=%+v err=%v", report, err)
				}
				if mode == "terminal" && a.stopPlans != 0 {
					t.Fatal("terminal planned Stop")
				}
			}
			wantCancel := 0
			if mode == "terminal before gate" {
				wantCancel = 1
			}
			if *cancelled != wantCancel || store.writes != 0 || store.scans != 0 || store.transactions != store.acquisitions {
				t.Fatalf("effects cancel=%d writes=%d scans=%d tx/acquire=%d/%d", *cancelled, store.writes, store.scans, store.transactions, store.acquisitions)
			}
		})
	}
}

func TestOutboxLifecycleInspectionUnknownNeverCancels(t *testing.T) {
	for _, stage := range []string{"outbox.stop-observe", "outbox.stop-authorize"} {
		t.Run(stage, func(t *testing.T) {
			s, store, _, actor, cause, cancelled := terminalInspectFixture(t, oc.DeleteProject, "stopping")
			a := inspectionTestBinding(s, store)
			store.beforeTx = func(c foundation.TransactionCause) { store.unknown = c.Details().Owner == stage }
			report, err := s.InspectStop(context.Background(), actor, cause)
			var f *foundation.Fault
			if !errors.As(err, &f) || f.CommitState != foundation.Unknown || report.Stopped || *cancelled != 0 || store.writes != 0 || store.scans != 0 {
				t.Fatalf("unknown leaked effects: %+v %v cancel=%d writes=%d scans=%d", report, err, *cancelled, store.writes, store.scans)
			}
			if stage == "outbox.stop-observe" && a.stopPlans != 0 {
				t.Fatal("unknown observation planned Stop")
			}
		})
	}
}
