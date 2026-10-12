package scheduler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"sync"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/scheduler/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type projectCallLane string

const (
	projectLaneCoordinator projectCallLane = "coordinator"
	projectLaneVisitor     projectCallLane = "visitor"
	projectLaneHandoff     projectCallLane = "handoff"
	projectLaneBusy        projectCallLane = "busy"
	projectLaneFailure     projectCallLane = "failure"
	projectLaneRelaunch    projectCallLane = "relaunch"
	projectLaneRunner      projectCallLane = "runner"
	maxProjectCallOwners                   = 256
)

type projectCallID struct{}
type projectCallInstance struct{}
type projectCallProcessProof interface {
	RequireCurrentProcessInTx(context.Context, f.Tx, oc.ProcessID) error
	ConfirmStopped(context.Context, oc.ProcessID) error
}

// ProjectCallDependencies fixes the same six owners used by a ProjectRunner.
// The PendingAuthority must already be the one borrowed by the Work adapters.
// Construction performs no I/O and starts neither a runner nor an Execution.
type ProjectCallDependencies struct {
	Coordinator       CoordinatorDependencies
	Handoff           LaunchHandoffDependencies
	Busy              BusyCompensatorDependencies
	Failure           LaunchFailureFinalizerDependencies
	RelaunchTasks     wc.SchedulerTaskRelaunches
	Tasks             wc.SchedulerTaskReader
	Occupancy         ec.WorkOccupancyReader
	RelaunchSkipCount int64
	RetryPolicy       *LaunchRetryPolicy
}

type projectCallRecord struct {
	ID       f.ID[projectCallID]       `json:"id"`
	Project  i.ProjectID               `json:"project_id"`
	Process  oc.ProcessID              `json:"process_id"`
	Instance f.ID[projectCallInstance] `json:"instance_id"`
	Lane     projectCallLane           `json:"lane"`
	Identity string                    `json:"invocation_identity"`
	Digest   f.Digest                  `json:"invocation_digest"`
	Phase    string                    `json:"phase"`
}

type projectCallEntry struct {
	record                                           projectCallRecord
	identity                                         f.CommandIdentity
	activeMethod                                     string
	ctx                                              context.Context
	cancel                                           context.CancelFunc
	registration                                     f.CommitResult
	registrationKnown                                bool
	bodyReturned, returned, settling, closed, absent bool
	unknown                                          *f.CommitResult
	unknownRegistration                              bool
	hadUnknown                                       bool
	// This is the original business result. Technical custody confirmation
	// cannot resolve a dispatch/Launch/Work outcome on its behalf.
	outcomeErr    error
	outcome       *projectCallOutcome
	parent        *projectCallEntry
	children      []*projectCallEntry
	childOutcomes []error
}

type projectCallState struct {
	store       Store
	pending     *PendingAuthority
	proof       projectCallProcessProof
	process     oc.ProcessID
	instance    f.ID[projectCallInstance]
	authority   sc.ProjectStopAuthority
	coordinator *Coordinator
	visitor     *PendingVisitor
	handoff     *LaunchHandoff
	busy        *BusyCompensator
	failure     *LaunchFailureFinalizer
	relaunch    *RelaunchCoordinator
	mu          sync.Mutex
	stopped     bool
	owners      map[f.ID[projectCallID]]*projectCallEntry
	stops       map[i.ProjectID]*ownedProjectStop
	changed     chan struct{}
}

type ProjectCallOwnership struct{ state *projectCallState }

func NewProjectCallOwnership(a *PendingAuthority, deps ProjectCallDependencies, guard *object.ProcessGuard, expected oc.ProcessID, authority sc.ProjectStopAuthority) (*ProjectCallOwnership, error) {
	return newProjectCallOwnership(a, deps, guard, expected, authority)
}

func sameProjectCallPort(a, b any) bool {
	if nilPort(a) || nilPort(b) {
		return false
	}
	x, y := reflect.ValueOf(a), reflect.ValueOf(b)
	return x.Type() == y.Type() && x.Comparable() && x.Interface() == y.Interface()
}

func newProjectCallOwnership(a *PendingAuthority, deps ProjectCallDependencies, proof projectCallProcessProof, expected oc.ProcessID, authority sc.ProjectStopAuthority) (*ProjectCallOwnership, error) {
	if a == nil || nilPort(a.store) || nilPort(proof) || nilPort(authority) || expected.Validate() != nil ||
		!sameProjectCallPort(deps.Coordinator.Projects, deps.Busy.Projects) || !sameProjectCallPort(deps.Coordinator.Projects, deps.Failure.Projects) ||
		!sameProjectCallPort(deps.Coordinator.Executions, deps.Handoff.Observations) {
		return nil, fault(f.DependencyUnbound)
	}
	var coordinator *Coordinator
	var err error
	if deps.RetryPolicy == nil {
		coordinator, err = NewCoordinator(a, deps.Coordinator)
	} else {
		coordinator, err = NewCoordinatorWithRetryPolicy(a, deps.Coordinator, *deps.RetryPolicy)
	}
	if err != nil {
		return nil, err
	}
	handoff, err := NewLaunchHandoffWithRetry(a, deps.Handoff, deps.Coordinator.Projects)
	if err != nil {
		return nil, err
	}
	busy, err := NewBusyCompensator(a, deps.Busy)
	if err != nil {
		return nil, err
	}
	failure, err := NewLaunchFailureFinalizer(a, deps.Failure)
	if err != nil {
		return nil, err
	}
	visitor, err := NewPendingVisitorWithFailure(a, handoff, busy, failure)
	if err != nil {
		return nil, err
	}
	relaunch, err := NewRelaunchCoordinator(coordinator, deps.RelaunchTasks, deps.Tasks, deps.Occupancy, deps.RelaunchSkipCount)
	if err != nil {
		return nil, err
	}
	instance, err := f.NewID[projectCallInstance]()
	if err != nil {
		return nil, portError(err)
	}
	s := &projectCallState{store: a.store, pending: a, proof: proof, process: expected, instance: instance, authority: authority,
		coordinator: coordinator, visitor: visitor, handoff: handoff, busy: busy, failure: failure, relaunch: relaunch,
		owners: map[f.ID[projectCallID]]*projectCallEntry{}, stops: map[i.ProjectID]*ownedProjectStop{}, changed: make(chan struct{})}
	coordinator.projectCalls, visitor.projectCalls, handoff.projectCalls, busy.projectCalls, failure.projectCalls, relaunch.projectCalls = s, s, s, s, s, s
	return &ProjectCallOwnership{s}, nil
}

func (p *ProjectCallOwnership) Coordinator() *Coordinator {
	if p == nil || p.state == nil {
		return nil
	}
	return p.state.coordinator
}
func (p *ProjectCallOwnership) Visitor() *PendingVisitor {
	if p == nil || p.state == nil {
		return nil
	}
	return p.state.visitor
}
func (p *ProjectCallOwnership) Handoff() *LaunchHandoff {
	if p == nil || p.state == nil {
		return nil
	}
	return p.state.handoff
}
func (p *ProjectCallOwnership) Busy() *BusyCompensator {
	if p == nil || p.state == nil {
		return nil
	}
	return p.state.busy
}
func (p *ProjectCallOwnership) Failure() *LaunchFailureFinalizer {
	if p == nil || p.state == nil {
		return nil
	}
	return p.state.failure
}
func (p *ProjectCallOwnership) Relaunch() *RelaunchCoordinator {
	if p == nil || p.state == nil {
		return nil
	}
	return p.state.relaunch
}

func (s *projectCallState) signalLocked() { close(s.changed); s.changed = make(chan struct{}) }
func projectInvocationDigest(identity string) f.Digest {
	sum := sha256.Sum256([]byte(identity))
	return f.Digest("sha256:" + hex.EncodeToString(sum[:]))
}

type projectCallContextKey struct{}
type projectCallWitness struct {
	state *projectCallState
	entry *projectCallEntry
}

func (s *projectCallState) admit(ctx context.Context, project i.ProjectID, lane projectCallLane, identity f.CommandIdentity) (context.Context, *projectCallEntry, error) {
	if s == nil {
		return ctx, nil, nil
	}
	if ctx == nil || project.Validate() != nil || !validProjectCallLane(lane) || identity.Validate() != nil || identity.Namespace() != "scheduler" {
		return ctx, nil, invalid()
	}
	ids := identity.OwnerIDs()
	if len(ids) != 1 || ids[0] != project.String() || len(identity.Canonical()) > 8192 {
		return ctx, nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return ctx, nil, err
	}
	if err := s.resumePrevious(ctx, project, lane, identity); err != nil {
		return ctx, nil, err
	}
	id, err := f.NewID[projectCallID]()
	if err != nil {
		return ctx, nil, portError(err)
	}
	s.mu.Lock()
	if s.projectKeyBusyLocked(project, lane, identity.Key(), nil) {
		s.mu.Unlock()
		return ctx, nil, fault(f.ResourceBusy)
	}
	if s.stopped {
		// Original recovery remains available after global Stop, but only by
		// reborrowing an exact retained original entry, without a new INSERT.
		if projectReadRecoveryMethod(identity.Command()) {
			var original *projectCallEntry
			for _, old := range s.owners {
				if old.record.Project == project && old.record.Lane == lane && old.identity.Key() == identity.Key() && old.returned && !old.settling && old.registrationKnown && old.unknown == nil && old.outcomeErr != nil {
					if original == nil || old.record.Identity == identity.Canonical() || old.record.ID.String() < original.record.ID.String() {
						original = old
					}
				}
			}
			if original != nil {
				s.borrowProjectCallLocked(ctx, original, identity.Command())
				s.mu.Unlock()
				return original.ctx, original, nil
			}
		}
		s.mu.Unlock()
		return ctx, nil, fault(f.ResourceBusy)
	}
	for _, old := range s.owners {
		if old.record.Project == project && old.record.Lane == lane && old.record.Identity == identity.Canonical() && old.returned {
			if !old.settling && projectCallCanResume(old) {
				s.borrowProjectCallLocked(ctx, old, identity.Command())
				s.mu.Unlock()
				return old.ctx, old, nil
			}
			if old.outcomeErr != nil {
				err = old.outcomeErr
			} else if old.unknown != nil {
				err = commitError(*old.unknown)
			} else {
				err = fault(f.ResourceBusy)
			}
			s.mu.Unlock()
			return ctx, nil, err
		}
	}
	if len(s.owners) >= maxProjectCallOwners {
		s.mu.Unlock()
		return ctx, nil, fault(f.ResourceBusy)
	}
	run, cancel := context.WithCancel(ctx)
	e := &projectCallEntry{record: projectCallRecord{ID: id, Project: project, Process: s.process, Instance: s.instance, Lane: lane, Identity: identity.Canonical(), Digest: projectInvocationDigest(identity.Canonical()), Phase: "running"}, identity: identity, activeMethod: identity.Command(), cancel: cancel}
	e.ctx = context.WithValue(run, projectCallContextKey{}, projectCallWitness{s, e})
	if parent, ok := ctx.Value(projectCallContextKey{}).(projectCallWitness); ok && parent.state == s && parent.entry != nil && s.owners[parent.entry.record.ID] == parent.entry && !parent.entry.bodyReturned && parent.entry.record.Project == project {
		e.parent = parent.entry
		parent.entry.children = append(parent.entry.children, e)
	}
	s.owners[id] = e
	s.signalLocked()
	s.mu.Unlock()
	result, validated := s.register(e.ctx, e)
	s.mu.Lock()
	e.registration = result
	e.registrationKnown = result.State() == f.Committed && validated
	if result.State() == f.Unknown {
		original := result
		e.unknown = &original
		e.unknownRegistration = true
		e.hadUnknown = true
		s.rememberParentErrorLocked(e, commitError(result))
	}
	s.signalLocked()
	s.mu.Unlock()
	if err := commitError(result); err != nil {
		return e.ctx, e, err
	}
	if !validated {
		return e.ctx, e, unavailable(nil)
	}
	return e.ctx, e, e.ctx.Err()
}

// The wrapper calls finish only after all original body defers have returned.
// Its final returned hook remains separate from the physical closing COMMIT.
func (s *projectCallState) finish(e *projectCallEntry, original error) error {
	if s == nil || e == nil {
		return original
	}
	var outcome *projectCallOutcome
	if uncertainProjectVisit(original) {
		outcome = s.captureProjectOutcome(e, original)
	}
	s.mu.Lock()
	if s.owners[e.record.ID] != e {
		s.mu.Unlock()
		return original
	}
	s.rememberParentErrorLocked(e, original)
	e.bodyReturned = true
	if uncertainProjectVisit(original) && e.unknown == nil && e.registrationKnown && e.outcomeErr == nil {
		e.hadUnknown = true
		e.outcomeErr = original
		e.outcome = outcome
	}
	closeAllowed := e.registrationKnown && e.unknown == nil && e.outcomeErr == nil
	s.signalLocked()
	s.mu.Unlock()
	if !closeAllowed {
		return original
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(e.ctx), 3*time.Second)
	defer cancel()
	_, err := s.settle(ctx, e, nil, false)
	if original != nil {
		return original
	}
	return err
}

func (s *projectCallState) returned(e *projectCallEntry) {
	if s == nil || e == nil {
		return
	}
	s.mu.Lock()
	cancel := e.cancel
	if s.owners[e.record.ID] == e {
		e.returned = true
		if e.registration.State() == f.NotCommitted || e.closed || e.absent {
			delete(s.owners, e.record.ID)
			if !e.hadUnknown && e.parent != nil {
				for n, child := range e.parent.children {
					if child == e {
						e.parent.children = append(e.parent.children[:n], e.parent.children[n+1:]...)
						break
					}
				}
			}
		}
		s.signalLocked()
	}
	s.mu.Unlock()
	cancel()
}

func (s *projectCallState) laneJoined(lane projectCallLane, projects ...i.ProjectID) bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.owners {
		if (lane == "" || e.record.Lane == lane) && (len(projects) == 0 || e.record.Project == projects[0]) {
			return false
		}
	}
	if lane == "" {
		for p := range s.stops {
			if len(projects) == 0 || p == projects[0] {
				return false
			}
		}
	}
	return true
}

func (s *projectCallState) drainReturned(ctx context.Context, lane projectCallLane, projects ...i.ProjectID) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		return invalid()
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		s.mu.Lock()
		var next *projectCallEntry
		pending := false
		for _, e := range s.owners {
			if lane != "" && e.record.Lane != lane || len(projects) != 0 && e.record.Project != projects[0] {
				continue
			}
			pending = true
			if e.returned && !e.settling && (next == nil || e.record.ID.String() < next.record.ID.String()) {
				next = e
			}
		}
		changed := s.changed
		s.mu.Unlock()
		if !pending {
			return nil
		}
		if next != nil {
			if _, err := s.settle(ctx, next, nil, false); err != nil {
				return err
			}
			continue
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (p *ProjectCallOwnership) Stop() {
	if p == nil || p.state == nil {
		return
	}
	s := p.state
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	var cancels []context.CancelFunc
	for _, e := range s.owners {
		cancels = append(cancels, e.cancel)
	}
	s.signalLocked()
	s.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	s.coordinator.Stop()
	s.visitor.Stop()
	s.handoff.Stop()
	s.busy.Stop()
	s.failure.Stop()
	s.relaunch.Stop()
}
func (p *ProjectCallOwnership) Drain(ctx context.Context) error {
	if p == nil || p.state == nil {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil {
		return invalid()
	}
	p.Stop()
	s := p.state
	for _, drain := range []func(context.Context) error{s.coordinator.Drain, s.visitor.Drain, s.handoff.Drain, s.busy.Drain, s.failure.Drain, s.relaunch.Drain} {
		if err := drain(ctx); err != nil {
			return err
		}
	}
	if err := s.drainReturned(ctx, ""); err != nil {
		return err
	}
	for {
		s.mu.Lock()
		done := len(s.stops) == 0
		changed := s.changed
		s.mu.Unlock()
		if done {
			return nil
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
func (p *ProjectCallOwnership) Joined() bool {
	if p == nil || p.state == nil {
		return false
	}
	s := p.state
	s.mu.Lock()
	done := s.stopped && len(s.owners) == 0 && len(s.stops) == 0
	s.mu.Unlock()
	return done && s.coordinator.Joined() && s.visitor.Joined() && s.handoff.Joined() && s.busy.Joined() && s.failure.Joined() && s.relaunch.Joined()
}

func projectReadRecoveryMethod(method string) bool {
	switch method {
	case "resolve_claim", "resolve_relaunch", "lookup_launch", "lookup_busy_compensation", "lookup_launch_failure":
		return true
	}
	return false
}
func (s *projectCallState) borrowProjectCallLocked(ctx context.Context, e *projectCallEntry, method string) {
	run, cancel := context.WithCancel(ctx)
	e.cancel = cancel
	e.ctx = context.WithValue(run, projectCallContextKey{}, projectCallWitness{s, e})
	e.activeMethod = method
	kept := e.children[:0]
	for _, child := range e.children {
		if !child.returned || !child.closed && !child.absent && child.registration.State() != f.NotCommitted {
			kept = append(kept, child)
		}
	}
	clear(e.children[len(kept):])
	e.children = kept
	e.childOutcomes = nil
	e.bodyReturned, e.returned = false, false
	s.signalLocked()
}
