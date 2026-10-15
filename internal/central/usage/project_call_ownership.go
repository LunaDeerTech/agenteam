package usage

import (
	"context"
	"errors"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

type projectCallID struct{}
type projectCallInstance struct{}
type projectCallLane string

const (
	projectCallList       projectCallLane = "list"
	projectCallAggregate  projectCallLane = "aggregate"
	projectCallSummary    projectCallLane = "summary_get"
	projectCallRebuild    projectCallLane = "summary_rebuild"
	projectCallInvocation projectCallLane = "invocation"
	maxProjectCallOwners                  = 256
)

type projectCallProcessProof interface {
	RequireCurrentProcessInTx(context.Context, f.Tx, oc.ProcessID) error
	ConfirmStopped(context.Context, oc.ProcessID) error
}
type projectCallRecord struct {
	ID         f.ID[projectCallID]       `json:"id"`
	Project    i.ProjectID               `json:"project_id"`
	Process    oc.ProcessID              `json:"process_id"`
	Instance   f.ID[projectCallInstance] `json:"instance_id"`
	Lane       projectCallLane           `json:"lane"`
	CallID     string                    `json:"call_id"`
	Phase      string                    `json:"phase"`
	Registered string                    `json:"registered_at"`
	Closed     string                    `json:"closed_at"`
}
type projectCallOwner struct {
	record                                                              projectCallRecord
	ctx                                                                 context.Context
	cancel                                                              context.CancelFunc
	actor                                                               i.Actor
	token                                                               *projectInvocationCall
	registrationKnown, bodyReturned, returned, settling, closed, absent bool
	unknown                                                             *f.CommitResult
	unknownRegistration                                                 bool
	active                                                              int
	rebuild                                                             *projectRebuildImage
	businessUnknown                                                     *f.CommitResult
}
type projectCallOwnershipState struct {
	store     Store
	service   *Service
	proof     projectCallProcessProof
	process   oc.ProcessID
	instance  f.ID[projectCallInstance]
	authority uc.ProjectStopAuthority
	mu        sync.Mutex
	stopped   bool
	owners    map[f.ID[projectCallID]]*projectCallOwner
	blocked   map[i.ProjectID]bool
	stops     map[i.ProjectID]*projectStopCall
	cleanups  map[i.ProjectID]*ownedProjectCleanup
	changed   chan struct{}
}
type ProjectCallOwnership struct{ state *projectCallOwnershipState }

// Construction fixes one original service and guard without initializing either.
// First admission validates that guard in the original registration transaction.
func NewProjectCallOwnership(store Store, a *Authority, d Dependencies, guard *object.ProcessGuard, expected oc.ProcessID, stop uc.ProjectStopAuthority) (*ProjectCallOwnership, error) {
	return newProjectCallOwnership(store, a, d, guard, expected, stop)
}
func newProjectCallOwnership(store Store, a *Authority, d Dependencies, proof projectCallProcessProof, expected oc.ProcessID, stop uc.ProjectStopAuthority) (*ProjectCallOwnership, error) {
	if nilPort(proof) || nilPort(stop) || expected.Validate() != nil {
		return nil, fault(f.DependencyUnbound)
	}
	if a.state() != nil && a.state().auth.Invocations != nil {
		facts, ok := a.state().auth.Invocations.(uc.ProjectInvocationFacts)
		if !ok || nilPort(facts) {
			return nil, fault(f.DependencyUnbound)
		}
	}
	service, err := New(store, a, d)
	if err != nil {
		return nil, err
	}
	instance, err := f.NewID[projectCallInstance]()
	if err != nil {
		return nil, portError(err)
	}
	s := &projectCallOwnershipState{store: store, service: service, proof: proof, process: expected, instance: instance, authority: stop, owners: map[f.ID[projectCallID]]*projectCallOwner{}, blocked: map[i.ProjectID]bool{}, stops: map[i.ProjectID]*projectStopCall{}, cleanups: map[i.ProjectID]*ownedProjectCleanup{}, changed: make(chan struct{})}
	service.state().ownership = s
	return &ProjectCallOwnership{s}, nil
}
func (p *ProjectCallOwnership) Service() *Service {
	if p == nil || p.state == nil {
		return nil
	}
	return p.state.service
}
func (s *projectCallOwnershipState) signalLocked() { close(s.changed); s.changed = make(chan struct{}) }
func unavailable(err error) error {
	return f.NewFault(f.DependencyUnavailable, f.NotStarted).WithCause(err)
}
func txError(r f.CommitResult) error { return commitError(r) }
func usageProjectLock(p i.ProjectID, mode f.LockMode) f.LockRequest {
	l := projectLock(p)
	l.Mode = mode
	return l
}
func rejectedProjectTx(err error) f.CommitResult {
	var ff *f.Fault
	if errors.As(err, &ff) {
		return f.NotCommittedResult(ff)
	}
	return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted).WithCause(err))
}

type projectCallContextKey struct{}
type projectCallWitness struct {
	state *projectCallOwnershipState
	owner *projectCallOwner
}

func (s *projectCallOwnershipState) newOwner(ctx context.Context, actor i.Actor, p i.ProjectID, lane projectCallLane, call string, token *projectInvocationCall) (*projectCallOwner, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if p.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	id, err := f.NewID[projectCallID]()
	if err != nil {
		return nil, portError(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || s.blocked[p] || s.cleanups[p] != nil || s.stops[p] != nil || len(s.owners) >= maxProjectCallOwners {
		return nil, fault(f.ResourceBusy)
	}
	run, cancel := context.WithCancel(ctx)
	o := &projectCallOwner{record: projectCallRecord{ID: id, Project: p, Process: s.process, Instance: s.instance, Lane: lane, CallID: call, Phase: "running"}, ctx: run, cancel: cancel, actor: actor, token: token}
	if token != nil {
		token.owner = o
	}
	s.owners[id] = o
	s.signalLocked()
	return o, nil
}
func (s *projectCallOwnershipState) begin(ctx context.Context, actor i.Actor, p i.ProjectID, lane projectCallLane) (context.Context, *projectCallOwner, error) {
	if s == nil {
		return ctx, nil, nil
	}
	if actor.Details().Kind != i.Human {
		return nil, nil, fault(f.Forbidden)
	}
	o, err := s.newOwner(ctx, actor, p, lane, "", nil)
	if err != nil {
		return nil, nil, err
	}
	run := context.WithValue(o.ctx, projectCallContextKey{}, projectCallWitness{s, o})
	result, valid := s.register(run, o)
	s.mu.Lock()
	o.registrationKnown = result.State() == f.Committed && valid
	if result.State() == f.Unknown {
		saved := result
		o.unknown = &saved
		o.unknownRegistration = true
	}
	if result.State() == f.NotCommitted {
		o.absent = true
	}
	s.signalLocked()
	s.mu.Unlock()
	if err = txError(result); err != nil {
		return run, o, err
	}
	if !valid {
		return run, o, unavailable(nil)
	}
	return run, o, nil
}
func (s *projectCallOwnershipState) finish(o *projectCallOwner, original *error) {
	if s == nil || o == nil {
		return
	}
	s.mu.Lock()
	o.bodyReturned = true
	firstUnknown := o.unknown != nil
	businessUnknown := o.businessUnknown != nil
	s.signalLocked()
	s.mu.Unlock()
	// Do not confirm a registration or business Unknown in its original call.
	if !firstUnknown && !businessUnknown && !o.absent {
		_, err := s.settle(o.ctx, o, nil, false)
		if original != nil && *original == nil && err != nil {
			*original = err
		}
	}
	o.cancel()
	s.mu.Lock()
	o.returned = true
	if (o.closed || o.absent) && o.unknown == nil && o.businessUnknown == nil && o.active == 0 {
		delete(s.owners, o.record.ID)
	}
	s.signalLocked()
	s.mu.Unlock()
}
func (p *ProjectCallOwnership) Stop() {
	if p == nil || p.state == nil {
		return
	}
	s := p.state
	s.mu.Lock()
	s.stopped = true
	owners := make([]*projectCallOwner, 0, len(s.owners))
	for _, o := range s.owners {
		owners = append(owners, o)
	}
	s.signalLocked()
	s.mu.Unlock()
	for _, o := range owners {
		s.cancelOwner(o)
	}
}
func (s *projectCallOwnershipState) cancelOwner(o *projectCallOwner) {
	if o.token != nil {
		// The fixed Runtime facts cancel only this original admission/call.
		_ = o.token.facts.CancelOriginalInvocation(o.token.original, o.token.store, o.token.request, o.token)
	} else {
		o.cancel()
	}
}
func (p *ProjectCallOwnership) Joined() bool {
	if p == nil || p.state == nil {
		return false
	}
	s := p.state
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped && len(s.owners) == 0 && len(s.stops) == 0 && len(s.cleanups) == 0
}
func (p *ProjectCallOwnership) Drain(ctx context.Context) error {
	if p == nil || p.state == nil {
		return fault(f.DependencyUnbound)
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	p.Stop()
	s := p.state
	for {
		s.mu.Lock()
		if len(s.owners) == 0 && len(s.stops) == 0 && len(s.cleanups) == 0 {
			s.mu.Unlock()
			return nil
		}
		var next *projectCallOwner
		for _, o := range s.owners {
			if o.returned && !o.settling && o.active == 0 && (next == nil || o.record.ID.String() < next.record.ID.String()) {
				next = o
			}
		}
		changed := s.changed
		s.mu.Unlock()
		if next != nil {
			observed, err := s.settle(ctx, next, nil, false)
			if err != nil {
				return err
			}
			if observed {
				return fault(f.ResourceBusy)
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

// There is no public token constructor. The original Runtime authority must
// recognize its private admission before this Service may register a loan.
type projectInvocationCall struct {
	state    *projectCallOwnershipState
	owner    *projectCallOwner
	facts    uc.ProjectInvocationFacts
	store    Store
	request  mc.ModelRequest
	original context.Context
}

func (svc *Service) ProjectInvocationBorrower() uc.ProjectInvocationBorrower {
	if svc.state() == nil || svc.state().ownership == nil {
		return nil
	}
	return svc
}

func (svc *Service) AcquireProjectInvocation(ctx context.Context, callerStore any, request mc.ModelRequest) (uc.ProjectInvocationCall, error) {
	if svc.state() == nil || svc.state().ownership == nil {
		return nil, fault(f.DependencyUnbound)
	}
	s := svc.state().ownership
	store, ok := callerStore.(Store)
	facts, has := svc.state().authority.state().auth.Invocations.(uc.ProjectInvocationFacts)
	if !ok || !sameStore(s.store, store) || !has || nilPort(facts) {
		return nil, fault(f.DependencyUnbound)
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if request.ValidateInvocation() != nil {
		return nil, fault(f.InvalidArgument)
	}
	request = request.Clone()
	if err := facts.RequireOriginalInvocationCall(ctx, callerStore, request, nil); err != nil {
		return nil, portError(err)
	}
	token := &projectInvocationCall{state: s, facts: facts, store: store, request: request, original: ctx}
	o, err := s.newOwner(ctx, request.Actor, request.Consumer.ProjectID, projectCallInvocation, request.CallID.String(), token)
	if err != nil {
		return nil, err
	}
	result, valid := s.register(ctx, o)
	s.mu.Lock()
	o.registrationKnown = result.State() == f.Committed && valid
	if result.State() == f.Unknown {
		saved := result
		o.unknown = &saved
		o.unknownRegistration = true
	}
	if result.State() == f.NotCommitted {
		o.absent = true
	}
	s.signalLocked()
	s.mu.Unlock()
	if err = txError(result); err != nil {
		return token, err
	}
	if !valid {
		return token, unavailable(nil)
	}
	return token, nil
}
func (t *projectInvocationCall) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	if t == nil || t.state == nil || fn == nil {
		return rejectedProjectTx(fault(f.DependencyUnbound))
	}
	if err := t.facts.RequireOriginalInvocationCall(ctx, t.store, t.request, t); err != nil {
		return rejectedProjectTx(portError(err))
	}
	s, o := t.state, t.owner
	s.mu.Lock()
	valid := s.owners[o.record.ID] == o && o.registrationKnown && !o.bodyReturned && !o.settling
	if valid {
		o.active++
		s.signalLocked()
	}
	s.mu.Unlock()
	if !valid {
		return rejectedProjectTx(fault(f.ResourceBusy))
	}
	defer func() { s.mu.Lock(); o.active--; s.signalLocked(); s.mu.Unlock() }()
	run := context.WithValue(ctx, projectCallContextKey{}, projectCallWitness{s, o})
	// Preserve the caller's full lock order. Its callback acquires the complete
	// original union; Usage writes require this witness before their first SQL.
	return t.store.WithinTx(run, cause, func(run context.Context, tx f.Tx) error {
		if err := fn(run, tx); err != nil {
			return err
		}
		return s.requireRunning(run, tx, o.record.Project, projectCallInvocation)
	})
}
func (t *projectInvocationCall) Returned(ctx context.Context) error {
	if t == nil || t.state == nil {
		return fault(f.DependencyUnbound)
	}
	t.state.mu.Lock()
	already := t.owner.returned && (t.owner.closed || t.owner.absent) && t.owner.unknown == nil && t.owner.businessUnknown == nil && t.state.owners[t.owner.record.ID] != t.owner
	t.state.mu.Unlock()
	if already {
		return nil
	}
	if err := t.facts.RequireOriginalInvocationReturned(ctx, t.store, t.request, t); err != nil {
		return portError(err)
	}
	s, o := t.state, t.owner
	s.mu.Lock()
	if s.owners[o.record.ID] != o {
		s.mu.Unlock()
		return nil
	}
	if o.active != 0 || o.settling {
		s.mu.Unlock()
		return fault(f.ResourceBusy)
	}
	first := !o.returned
	o.bodyReturned = true
	original := o.unknown
	absent := o.absent
	s.signalLocked()
	s.mu.Unlock()
	var err error
	// A first return following failed admission never confirms its own Unknown.
	if first && original != nil {
		err = txError(*original)
	} else if !absent {
		_, err = s.settle(ctx, o, nil, false)
	}
	s.mu.Lock()
	o.returned = true
	done := (o.closed || o.absent) && o.unknown == nil && o.businessUnknown == nil
	if done {
		delete(s.owners, o.record.ID)
	}
	s.signalLocked()
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if !done {
		if original != nil {
			return txError(*original)
		}
		return fault(f.ResourceBusy)
	}
	return nil
}
func (s *projectCallOwnershipState) borrowInvocation(ctx context.Context, r uc.InvocationRequest) (context.Context, func(), error) {
	if s == nil {
		return ctx, func() {}, nil
	}
	if err := contextError(ctx); err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	var o *projectCallOwner
	for _, candidate := range s.owners {
		if candidate.record.Lane == projectCallInvocation && candidate.record.Project == r.Identity.Consumer.ProjectID && candidate.record.CallID == r.Identity.Attempt.CallID.String() {
			o = candidate
			break
		}
	}
	s.mu.Unlock()
	if o == nil || o.token == nil {
		return nil, nil, fault(f.DependencyUnbound)
	}
	t := o.token
	if err := t.facts.RequireOriginalInvocationCall(ctx, t.store, t.request, t); err != nil {
		return nil, nil, portError(err)
	}
	s.mu.Lock()
	valid := s.owners[o.record.ID] == o && o.registrationKnown && !o.bodyReturned && !o.settling
	if valid {
		o.active++
		s.signalLocked()
	}
	s.mu.Unlock()
	if !valid {
		return nil, nil, fault(f.ResourceBusy)
	}
	return context.WithValue(ctx, projectCallContextKey{}, projectCallWitness{s, o}), func() { s.mu.Lock(); o.active--; s.signalLocked(); s.mu.Unlock() }, nil
}

var _ uc.ProjectInvocationBorrower = (*Service)(nil)
var _ uc.ProjectInvocationOwnership = (*Service)(nil)
var _ uc.ProjectInvocationCall = (*projectInvocationCall)(nil)
