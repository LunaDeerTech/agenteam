package execution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"sync"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

type projectCallLane string

const (
	projectLaneService     projectCallLane = "service"
	projectLanePreparation projectCallLane = "preparation"
	projectLaneDirect      projectCallLane = "direct"
	projectLaneAssociated  projectCallLane = "associated"
	maxProjectCallOwners                   = 256
)

type projectCallID struct{}
type projectCallInstance struct{}
type projectCallProcessProof interface {
	RequireCurrentProcessInTx(context.Context, f.Tx, oc.ProcessID) error
	ConfirmStopped(context.Context, oc.ProcessID) error
}

// ProjectCallDependencies binds the four original owners. The factory does no
// I/O and does not start the Associated lifetime or accept a Model session.
// Additional tool drivers and the default App registry are not bound by it.
type ProjectCallDependencies struct {
	Service     Dependencies
	Preparation PreparationDependencies
	Direct      DirectTextDependencies
	Associated  AssociatedExecutorOptions
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
	serviceDone                                      func()
	actor                                            i.Actor
	launchKey                                        ec.LaunchLookupKey
	launchDigest                                     f.Digest
	execution                                        i.ExecutionID
	job                                              *associatedCall
	inspected                                        bool
	registration                                     f.CommitResult
	registrationKnown                                bool
	bodyReturned, returned, settling, closed, absent bool
	unknown                                          *f.CommitResult
	unknownRegistration                              bool
	hadUnknown                                       bool
	priorReturned                                    bool
	technicalOnly                                    bool
	outcomeErr                                       error
	outcome                                          *projectCallOutcome
	parent                                           *projectCallEntry
	children                                         []*projectCallEntry
	childOutcomes                                    []error
}
type projectCallState struct {
	store       Store
	proof       projectCallProcessProof
	process     oc.ProcessID
	instance    f.ID[projectCallInstance]
	authority   ec.ProjectStopAuthority
	service     *Service
	preparation *PreparationDriver
	direct      *DirectTextDriver
	associated  *AssociatedExecutor
	mu          sync.Mutex
	stopped     bool
	closedLanes map[projectCallLane]bool
	owners      map[f.ID[projectCallID]]*projectCallEntry
	stops       map[i.ProjectID]*ownedProjectStop
	changed     chan struct{}
}
type ProjectCallOwnership struct{ state *projectCallState }

func NewProjectCallOwnership(store Store, deps ProjectCallDependencies, guard *object.ProcessGuard, expected oc.ProcessID, authority ec.ProjectStopAuthority) (*ProjectCallOwnership, error) {
	if guard == nil {
		return nil, fault(f.DependencyUnbound)
	}
	current, err := guard.CurrentProcess()
	if err != nil {
		return nil, portError(err)
	}
	if current != expected {
		return nil, fault(f.DependencyUnbound)
	}
	return newProjectCallOwnership(store, deps, guard, expected, authority)
}
func sameProjectCallPort(a, b any) bool {
	if nilPort(a) || nilPort(b) {
		return false
	}
	x, y := reflect.ValueOf(a), reflect.ValueOf(b)
	return x.Type() == y.Type() && x.Comparable() && x.Interface() == y.Interface()
}
func newProjectCallOwnership(store Store, deps ProjectCallDependencies, proof projectCallProcessProof, expected oc.ProcessID, authority ec.ProjectStopAuthority) (*ProjectCallOwnership, error) {
	if nilPort(store) || nilPort(proof) || nilPort(authority) || expected.Validate() != nil || !sameProjectCallPort(deps.Preparation.Projects, deps.Direct.Projects) || nilPort(deps.Preparation.Processes) || nilPort(deps.Direct.Processes) || deps.Preparation.Processes.CurrentProcess().String() != expected.String() || deps.Direct.Processes.CurrentProcess().String() != expected.String() {
		return nil, fault(f.DependencyUnbound)
	}
	service, err := New(store, deps.Service)
	if err != nil {
		return nil, err
	}
	preparation, err := NewPreparationDriver(store, deps.Service.Authority, deps.Preparation)
	if err != nil {
		return nil, err
	}
	direct, err := NewDirectTextDriver(store, deps.Service.Authority, deps.Direct)
	if err != nil {
		return nil, err
	}
	associated, err := NewAssociatedExecutor(preparation, direct, deps.Associated)
	if err != nil {
		return nil, err
	}
	instance, err := f.NewID[projectCallInstance]()
	if err != nil {
		return nil, portError(err)
	}
	s := &projectCallState{store: store, proof: proof, process: expected, instance: instance, authority: authority, service: service, preparation: preparation, direct: direct, associated: associated, owners: map[f.ID[projectCallID]]*projectCallEntry{}, stops: map[i.ProjectID]*ownedProjectStop{}, changed: make(chan struct{})}
	service.projectCalls, preparation.state.projectCalls, direct.state.projectCalls, associated.state.projectCalls = s, s, s, s
	return &ProjectCallOwnership{s}, nil
}
func (p *ProjectCallOwnership) Service() *Service {
	if p == nil || p.state == nil {
		return nil
	}
	return p.state.service
}
func (p *ProjectCallOwnership) Preparation() *PreparationDriver {
	if p == nil || p.state == nil {
		return nil
	}
	return p.state.preparation
}
func (p *ProjectCallOwnership) Direct() *DirectTextDriver {
	if p == nil || p.state == nil {
		return nil
	}
	return p.state.direct
}
func (p *ProjectCallOwnership) Associated() *AssociatedExecutor {
	if p == nil || p.state == nil {
		return nil
	}
	return p.state.associated
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

func (s *projectCallState) admitService(ctx context.Context, actor i.Actor, key ec.LaunchLookupKey, expected f.Digest, method string) (context.Context, *projectCallEntry, error) {
	if s == nil {
		return ctx, nil, nil
	}
	if ctx == nil || launchActor(actor) != nil || key.Validate() != nil || expected.Validate() != nil || (method != "launch" && method != "lookup_launch") {
		return ctx, nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return ctx, nil, err
	}
	identity, err := f.NewCommandIdentity("execution", []string{key.ProjectID.String(), key.AgentID.String()}, method, key.IdempotencyKey)
	if err != nil {
		return ctx, nil, err
	}
	// Only an exact original unresolved invocation may bypass the stopped
	// legacy callSet. Fresh reads and writes use its real admission, not a
	// snapshot of its stopped bit, before any technical transaction begins.
	if method == "lookup_launch" {
		s.mu.Lock()
		for _, old := range s.owners {
			if old.record.Lane == projectLaneService && old.launchKey == key && old.launchDigest == expected && old.actor.Equal(actor) && old.returned && !old.settling && old.registrationKnown && old.unknown == nil && old.outcomeErr != nil {
				if s.projectKeyBusyLocked(key.ProjectID, projectLaneService, identity, old) {
					s.mu.Unlock()
					return ctx, nil, fault(f.ResourceBusy)
				}
				s.borrowProjectCallLocked(ctx, old, method)
				s.mu.Unlock()
				return old.ctx, old, nil
			}
		}
		s.mu.Unlock()
	}
	run, done, err := s.service.begin(ctx)
	if err != nil {
		return ctx, nil, err
	}
	template := &projectCallEntry{actor: actor, launchKey: key, launchDigest: expected}
	run, e, err := s.admit(run, key.ProjectID, projectLaneService, identity, template, method == "lookup_launch")
	if e == nil {
		done()
		return run, nil, err
	}
	s.mu.Lock()
	e.serviceDone = done
	s.mu.Unlock()
	return run, e, err
}
func (s *projectCallState) admitDriver(ctx context.Context, execution i.ExecutionID, lane projectCallLane, method string) (context.Context, *projectCallEntry, error) {
	if s == nil {
		return ctx, nil, nil
	}
	if ctx == nil || execution.Validate() != nil || (lane != projectLanePreparation && lane != projectLaneDirect) {
		return ctx, nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return ctx, nil, err
	}
	recovery := method == "resolve_preparation" || method == "resolve_direct"
	if recovery {
		// Resolve borrows the same original entry. In particular Direct recovery
		// keeps the original run.ctx values, so it must not replace this witness.
		s.mu.Lock()
		for _, e := range s.owners {
			if e.record.Lane == lane && e.execution == execution && e.returned && e.registrationKnown && e.unknown == nil && e.outcomeErr != nil && !e.settling {
				if s.projectKeyBusyLocked(e.record.Project, lane, e.identity, e) {
					s.mu.Unlock()
					return ctx, nil, fault(f.ResourceBusy)
				}
				s.borrowProjectCallLocked(ctx, e, method)
				s.mu.Unlock()
				return e.ctx, e, nil
			}
		}
		s.mu.Unlock()
		return ctx, nil, fault(f.DependencyUnbound)
	}
	if lane == projectLanePreparation && method != "prepare" || lane == projectLaneDirect && method != "start" {
		return ctx, nil, invalid()
	}
	w, ok := ctx.Value(projectCallContextKey{}).(projectCallWitness)
	job, jobOK := ctx.Value(associatedContextKey{}).(*associatedCall)
	s.mu.Lock()
	valid := ok && jobOK && job != nil && w.state == s && w.entry != nil && s.owners[w.entry.record.ID] == w.entry && !w.entry.bodyReturned && w.entry.registrationKnown && w.entry.inspected && w.entry.job == job && w.entry.execution == execution && w.entry.record.Project == job.project
	s.mu.Unlock()
	if !valid {
		return ctx, nil, fault(f.DependencyUnbound)
	}
	d := job.dispatch
	identity, err := f.NewCommandIdentity("execution", []string{job.project.String(), execution.String(), d.AgentID.String(), d.DispatchID}, method, f.IdempotencyKey(execution.String()))
	if err != nil {
		return ctx, nil, err
	}
	return s.admit(ctx, job.project, lane, identity, &projectCallEntry{execution: execution, job: job, launchDigest: d.Digest}, false)
}
func (s *projectCallState) admit(ctx context.Context, project i.ProjectID, lane projectCallLane, identity f.CommandIdentity, template *projectCallEntry, recovery bool) (context.Context, *projectCallEntry, error) {
	if err := ctx.Err(); err != nil {
		return ctx, nil, err
	}
	s.mu.Lock()
	laneStopped := s.closedLanes[lane]
	s.mu.Unlock()
	if laneStopped {
		return ctx, nil, fault(f.ShuttingDown)
	}
	if err := s.resumePrevious(ctx, project, lane, identity, recovery, template); err != nil {
		return ctx, nil, err
	}
	s.mu.Lock()
	if s.projectKeyBusyLocked(project, lane, identity, nil) {
		s.mu.Unlock()
		return ctx, nil, fault(f.ResourceBusy)
	}
	for _, old := range s.owners {
		if old.record.Project == project && old.record.Lane == lane && sameProjectInvocationKey(old.identity, identity) && old.returned {
			if recovery && old.registrationKnown && old.unknown == nil && old.outcomeErr != nil && !old.settling && old.launchKey == template.launchKey && old.launchDigest == template.launchDigest && old.actor.Equal(template.actor) {
				s.borrowProjectCallLocked(ctx, old, identity.Command())
				s.mu.Unlock()
				return old.ctx, old, nil
			}
			s.mu.Unlock()
			return ctx, nil, fault(f.ResourceBusy)
		}
	}
	if s.stopped || s.closedLanes[lane] || s.stops[project] != nil || len(s.owners) >= maxProjectCallOwners {
		s.mu.Unlock()
		return ctx, nil, fault(f.ResourceBusy)
	}
	e, err := s.newEntryLocked(ctx, project, lane, identity, template)
	s.mu.Unlock()
	if err != nil {
		return ctx, nil, err
	}
	result, validated := s.register(e.ctx, e)
	return s.registered(e, result, validated)
}
func (s *projectCallState) newEntryLocked(ctx context.Context, project i.ProjectID, lane projectCallLane, identity f.CommandIdentity, e *projectCallEntry) (*projectCallEntry, error) {
	id, err := f.NewID[projectCallID]()
	if err != nil {
		return nil, portError(err)
	}
	canonical, err := json.Marshal(struct {
		Invocation json.RawMessage `json:"invocation"`
		Digest     string          `json:"request_digest"`
	}{json.RawMessage(identity.Canonical()), e.launchDigest.String()})
	if err != nil || len(canonical) > 8192 {
		return nil, invalid()
	}
	e.record = projectCallRecord{ID: id, Project: project, Process: s.process, Instance: s.instance, Lane: lane, Identity: string(canonical), Digest: projectInvocationDigest(string(canonical)), Phase: "running"}
	e.identity = identity
	e.activeMethod = identity.Command()
	run, cancel := context.WithCancel(ctx)
	e.cancel = cancel
	e.ctx = context.WithValue(run, projectCallContextKey{}, projectCallWitness{s, e})
	if parent, ok := ctx.Value(projectCallContextKey{}).(projectCallWitness); ok && parent.state == s && parent.entry != nil && s.owners[parent.entry.record.ID] == parent.entry && !parent.entry.bodyReturned && parent.entry.record.Project == project {
		e.parent = parent.entry
		parent.entry.children = append(parent.entry.children, e)
	}
	s.owners[id] = e
	s.signalLocked()
	return e, nil
}
func (s *projectCallState) registered(e *projectCallEntry, result f.CommitResult, validated bool) (context.Context, *projectCallEntry, error) {
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

// reserveAssociated is called while the original executor mutex is held. It
// only registers a local original worker and never takes that mutex in return.
func (s *projectCallState) reserveAssociated(ctx context.Context, job *associatedCall) (*projectCallEntry, error) {
	if s == nil {
		return nil, nil
	}
	if ctx == nil || job == nil || validateAssociated(job.project, job.dispatch) != nil {
		return nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	d := job.dispatch
	identity, err := f.NewCommandIdentity("execution", []string{job.project.String(), d.ExecutionID.String(), d.AgentID.String(), d.DispatchID}, "advance", d.Key)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if old := job.projectCall; old != nil && s.owners[old.record.ID] == old {
		if !old.returned || old.settling || s.projectKeyBusyLocked(job.project, projectLaneAssociated, identity, old) {
			return nil, fault(f.ResourceBusy)
		}
		// This is the same original job. It may recover after global stop without
		// admitting a new execution or laundering a registration Unknown.
		technical := old.unknown != nil || old.outcomeErr == nil || old.outcome != nil && old.outcome.associationRead
		s.borrowProjectCallLocked(ctx, old, "advance")
		old.technicalOnly = technical
		return old, nil
	}
	if s.stopped || s.closedLanes[projectLaneAssociated] || s.stops[job.project] != nil || len(s.owners) >= maxProjectCallOwners || s.projectKeyBusyLocked(job.project, projectLaneAssociated, identity, nil) {
		return nil, fault(f.ResourceBusy)
	}
	return s.newEntryLocked(ctx, job.project, projectLaneAssociated, identity, &projectCallEntry{execution: d.ExecutionID, job: job, launchDigest: d.Digest})
}

// Registration runs on the original worker before any Execution SQL. A
// technical confirmation is one explicit action and cannot fall into Prepare.
func (s *projectCallState) registerAssociated(ctx context.Context, e *projectCallEntry) (context.Context, bool, error) {
	if s == nil || e == nil {
		return ctx, true, nil
	}
	s.mu.Lock()
	technical := e.technicalOnly
	registered := e.registrationKnown
	s.mu.Unlock()
	if technical {
		s.mu.Lock()
		e.bodyReturned = true
		s.mu.Unlock()
		_, err := s.settle(e.ctx, e, nil, false)
		return e.ctx, false, err
	}
	if registered {
		return e.ctx, true, e.ctx.Err()
	}
	result, validated := s.register(e.ctx, e)
	run, _, err := s.registered(e, result, validated)
	return run, err == nil, err
}
func (s *projectCallState) projectCallPending(e *projectCallEntry) bool {
	if s == nil || e == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.owners[e.record.ID] == e && !e.closed && !e.absent && e.registration.State() != f.NotCommitted
}
func markAssociatedInspected(ctx context.Context, job *associatedCall) error {
	if ctx == nil {
		return invalid()
	}
	w, ok := ctx.Value(projectCallContextKey{}).(projectCallWitness)
	if !ok {
		return nil
	}
	if w.state == nil || w.entry == nil || job == nil {
		return fault(f.DependencyUnbound)
	}
	s, e := w.state, w.entry
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.owners[e.record.ID] != e || e.job != job || !e.registrationKnown || e.bodyReturned || e.execution != job.dispatch.ExecutionID || e.record.Project != job.project {
		return fault(f.InvalidState)
	}
	e.inspected = true
	return ctx.Err()
}

// Called after the original body, resource cleanup and recovery defers. The
// separate returned hook encloses the original executor's final worker tail.
func (s *projectCallState) finish(e *projectCallEntry, original error) error {
	if s == nil || e == nil {
		return original
	}
	outcome, retained := s.captureProjectOutcome(e, original)
	s.mu.Lock()
	if s.owners[e.record.ID] != e {
		s.mu.Unlock()
		return original
	}
	s.rememberParentErrorLocked(e, original)
	e.bodyReturned = true
	if retained != nil && e.unknown == nil && e.registrationKnown && e.outcomeErr == nil {
		e.hadUnknown = true
		e.outcomeErr = retained
		e.outcome = outcome
		s.rememberParentErrorLocked(e, retained)
	}
	closeAllowed := !e.technicalOnly && e.registrationKnown && e.unknown == nil && e.outcomeErr == nil
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
	done := e.serviceDone
	e.serviceDone = nil
	s.mu.Unlock()
	// The original callSet return is part of this outer call. It may itself
	// block; do not advertise a returned P owner or allow reborrowing first.
	if done != nil {
		done()
	}
	if cancel != nil {
		cancel()
	}
	s.mu.Lock()
	retire := false
	if s.owners[e.record.ID] == e {
		e.returned = true
		retire = e.registration.State() == f.NotCommitted || e.closed || e.absent
		if retire {
			e.settling = true
		}
		s.signalLocked()
	}
	s.mu.Unlock()
	if retire {
		s.retireProjectEntry(e)
		s.mu.Lock()
		e.settling = false
		s.signalLocked()
		s.mu.Unlock()
	}
}

// The original executor can retain a job solely because its call checkpoint
// was uncertain. Retire that contribution only under this entry's reservation,
// after the original worker tail and every real child owner have returned.
func (s *projectCallState) retireProjectEntry(e *projectCallEntry) {
	s.mu.Lock()
	ready := s.owners[e.record.ID] == e && e.returned && e.settling && (e.closed || e.absent || e.registration.State() == f.NotCommitted)
	s.mu.Unlock()
	if !ready {
		return
	}
	if e.job != nil && e.record.Lane == projectLaneAssociated {
		a := s.associated.state
		owners := a.ops.owners(e.job)
		if owners.foreign || owners.preparation || owners.direct {
			return
		}
		a.mu.Lock()
		current := a.jobs[e.execution]
		if current != nil && (current != e.job || current.active || current.projectCall != e) {
			a.mu.Unlock()
			return
		}
		if current == e.job {
			current.retained = false
			a.notifyLocked()
		}
		a.mu.Unlock()
	}
	s.mu.Lock()
	if s.owners[e.record.ID] == e && e.returned && e.settling {
		delete(s.owners, e.record.ID)
		if !e.hadUnknown && e.parent != nil {
			for n, c := range e.parent.children {
				if c == e {
					e.parent.children = append(e.parent.children[:n], e.parent.children[n+1:]...)
					break
				}
			}
		}
		s.signalLocked()
	}
	s.mu.Unlock()
}

func (s *projectCallState) borrowProjectCallLocked(ctx context.Context, e *projectCallEntry, method string) {
	e.priorReturned = e.returned
	// A later original Associated recovery can own a new outer call after an
	// earlier known failure returned. Preserve the old parent's immutable facts,
	// while attributing this actual new child invocation to its real caller.
	if parent, ok := ctx.Value(projectCallContextKey{}).(projectCallWitness); ok && parent.state == s && parent.entry != nil && parent.entry != e && s.owners[parent.entry.record.ID] == parent.entry && !parent.entry.bodyReturned && parent.entry.record.Project == e.record.Project && e.job != nil && parent.entry.job == e.job {
		e.parent = parent.entry
		present := false
		for _, child := range parent.entry.children {
			if child == e {
				present = true
				break
			}
		}
		if !present {
			parent.entry.children = append(parent.entry.children, e)
		}
	}
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
	e.bodyReturned, e.returned, e.technicalOnly = false, false, false
	s.signalLocked()
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
		pending := false
		var next *projectCallEntry
		for _, e := range s.owners {
			if lane != "" && e.record.Lane != lane || len(projects) != 0 && e.record.Project != projects[0] {
				continue
			}
			pending = true
			leaf := s.projectLeafLocked(e)
			if leaf != nil && (next == nil || leaf.record.ID.String() < next.record.ID.String()) {
				next = leaf
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
	for _, c := range cancels {
		c()
	}
	s.associated.Stop()
	s.service.Stop()
	s.preparation.Stop()
	s.direct.Stop()
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
	for _, drain := range []func(context.Context) error{s.associated.Drain, s.service.Drain, s.preparation.Drain, s.direct.Drain} {
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
	return done && s.service.Joined() && s.preparation.Joined() && s.direct.Joined() && s.associated.Joined()
}

// Retained outcomes need not be resolved for the original worker tail to have
// returned. The lifetime Run uses this observation without starting SQL.
func (s *projectCallState) projectCallsReturned(lane projectCallLane) (bool, <-chan struct{}) {
	if s == nil {
		return true, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.owners {
		if (lane == "" || e.record.Lane == lane) && !e.returned {
			return false, s.changed
		}
	}
	return true, s.changed
}

// Each original driver Stop closes only its own lane. Fresh registration and
// this marker share the same local linearization point; admitted original
// Store calls remain counted through their actual return.
func (s *projectCallState) stopLane(lane projectCallLane) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.closedLanes == nil {
		s.closedLanes = map[projectCallLane]bool{}
	}
	if s.closedLanes[lane] {
		s.mu.Unlock()
		return
	}
	s.closedLanes[lane] = true
	var cancels []context.CancelFunc
	for _, e := range s.owners {
		if e.record.Lane == lane {
			cancels = append(cancels, e.cancel)
		}
	}
	s.signalLocked()
	s.mu.Unlock()
	for _, cancel := range cancels {
		if cancel != nil {
			cancel()
		}
	}
}
