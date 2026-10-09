package skill

import (
	"context"
	"errors"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// Independent controls consume frozen production methods and the explicit
// controlled SQL fixture. They make no PostgreSQL or real ProcessGuard claim.
type independentStopLockFailure struct {
	Store
	failure  error
	acquires int
}

func (s *independentStopLockFailure) AcquireAll(context.Context, f.Tx, []f.LockRequest) error {
	s.acquires++
	return s.failure
}

func TestIndependentSkillStopAuthorityFailurePrecedesCancellation(t *testing.T) {
	s, store, projects, processes := newStopFixture(t, pc.Delete)
	original, err := s.beginProjectWork(context.Background(), projects.scope.ProjectID, initializationWork)
	if err != nil {
		t.Fatal(err)
	}
	defer s.end(original)
	sentinel := f.NewFault(f.ResourceBusy, f.NotStarted)
	blocked := &independentStopLockFailure{Store: store, failure: sentinel}
	s.state().authority.state().store = blocked
	report, err := s.RequestStop(context.Background(), projects.actor, projects.cause, projects.scope)
	if err != sentinel || blocked.acquires != 1 || projects.calls != 0 || processes.calls != 0 || original.ctx.Err() != nil || report.Matches(pc.SkillsParticipant, projects.cause, projects.scope) {
		t.Fatal("lock failure escaped into current gate, cancellation, proof or report", err)
	}
}

func TestIndependentSkillStopRetiredWorkIsNotReturnedCall(t *testing.T) {
	s, store, projects, processes := newStopFixture(t, pc.Delete)
	row, err := loadInitialization(context.Background(), store, projects.scope.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	original, err := s.beginProjectWork(context.Background(), projects.scope.ProjectID, initializationWork)
	if err != nil {
		t.Fatal(err)
	}
	defer s.end(original)
	w, err := s.newOwnedWork(*row, initializationWork, original)
	if err != nil {
		t.Fatal(err)
	}
	putStopWork(store, w.fact)
	s.state().mu.Lock()
	w.returned = true // actual I/O returned; outer admitted call still owns its tail
	s.state().mu.Unlock()
	report, err := s.InspectStop(context.Background(), projects.actor, projects.cause, projects.scope)
	if err != nil || report.Details().State != pc.StopPending || len(report.Details().ActiveRefs) != 1 || processes.calls != 0 || original.ctx.Err() != nil {
		t.Fatal("durable join retired an original call or inspection cancelled it", err)
	}
	if store.work[w.fact.id.String()][5] != "joined" || s.state().work[w.fact.id] != nil {
		t.Fatal("actual returned I/O accounting did not join")
	}
	s.end(original)
	report, err = s.InspectStop(context.Background(), projects.actor, projects.cause, projects.scope)
	if err != nil || report.Details().State != pc.Stopped {
		t.Fatal("actual call end was not observed", err)
	}
}

func TestIndependentSkillStopRejectsPostProofOwnerChange(t *testing.T) {
	for _, field := range []string{"process", "kind"} {
		t.Run(field, func(t *testing.T) {
			s, store, projects, processes := newStopFixture(t, pc.Delete)
			row, err := loadInitialization(context.Background(), store, projects.scope.ProjectID)
			if err != nil {
				t.Fatal(err)
			}
			w := workFact{id: stateID[skillWork](231), project: row.request.ProjectID, skill: row.skill, process: processes.want, kind: initializationWork, phase: workRunning, fence: 1, created: row.created}
			putStopWork(store, w)
			processes.after = func() {
				if field == "process" {
					store.work[w.id.String()][3] = stateID[oc.Process](232).String()
				} else {
					store.work[w.id.String()][4] = string(packageReaderWork)
				}
			}
			report, err := s.InspectStop(context.Background(), projects.actor, projects.cause, projects.scope)
			var fault *f.Fault
			if !errors.As(err, &fault) || fault.Code != f.ResourceBusy || processes.calls != 1 || projects.calls != 2 || store.work[w.id.String()][5] == "joined" || report.Details().State == pc.Stopped {
				t.Fatal("old exact proof joined changed work identity", err)
			}
		})
	}
}

func TestIndependentSkillStopUnknownGateDoesNotAskForDeath(t *testing.T) {
	s, store, projects, processes := newStopFixture(t, pc.Delete)
	row, err := loadInitialization(context.Background(), store, projects.scope.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	w := workFact{id: stateID[skillWork](233), project: row.request.ProjectID, skill: row.skill, process: processes.want, kind: initializationWork, phase: workRunning, fence: 1, created: row.created}
	putStopWork(store, w)
	store.unknownAt = 1
	report, err := s.InspectStop(context.Background(), projects.actor, projects.cause, projects.scope)
	unknown, ok := UnknownAttempt(err)
	if !ok || unknown.State() != f.Unknown || unknown.AttemptID().Validate() != nil || processes.calls != 0 || projects.calls != 1 || store.work[w.id.String()][5] == "joined" || report.Matches(pc.SkillsParticipant, projects.cause, projects.scope) {
		t.Fatal("unconfirmed gate consumed foreign proof or lost original Unknown", err)
	}
}
