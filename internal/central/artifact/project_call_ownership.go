package artifact

import (
	"context"
	"errors"
	"sync"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	au "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

type artifactCallID struct{}
type artifactInstanceID struct{}
type projectCallProcessProof interface {
	RequireCurrentProcessInTx(context.Context, f.Tx, oc.ProcessID) error
	ConfirmStopped(context.Context, oc.ProcessID) error
}
type artifactCallRecord struct {
	ID       f.ID[artifactCallID] `json:"id"`
	Project  i.ProjectID          `json:"project_id"`
	Process  oc.ProcessID         `json:"process_id"`
	Instance string               `json:"instance_id"`
	Kind     string               `json:"kind"`
	Joined   bool                 `json:"joined"`
}
type artifactCall struct {
	record             artifactCallRecord
	cancel             context.CancelFunc
	returned, settling bool
	registration       f.CommitResult
	checkpoint         f.CommitResult
	firstUnknown       f.CommitResult
}
type artifactStopCall struct {
	actor            i.Actor
	cause            oc.ProjectStopCause
	active, retained bool
	original         f.CommitResult
}
type projectCallOwnershipState struct {
	service         *Service
	proof           projectCallProcessProof
	process         oc.ProcessID
	instance        f.ID[artifactInstanceID]
	authority       oc.ProjectStopAuthority
	sourceAuthority oc.ProjectSourceStopAuthority
	mu              sync.Mutex
	stopped         bool
	controls        int
	owners          map[f.ID[artifactCallID]]*artifactCall
	stops           map[i.ProjectID]*artifactStopCall
	changed         chan struct{}
}

// ProjectCallOwnership is an explicit composition. Its caller must keep the
// original Object runtime/ProcessGuard until Drain actually returns and Joined
// is true. Project Stop/Cleanup Unknowns must be explicitly confirmed before
// global Stop: after admission closes, Drain conservatively retains unresolved
// provider owners. It does not install a default Project participant.
type ProjectCallOwnership struct{ state *projectCallOwnershipState }

func NewProjectCallOwnership(store Store, owners *OwnerProvider, objects oc.Objects, uploads oc.Uploads, cleaner oc.Cleaner, reads oc.SourceReads, resolver oc.SourceResolver, auditing au.Appender, cursors cursor.Keyring, guard *object.ProcessGuard, expected oc.ProcessID, authority oc.ProjectStopAuthority) (*ProjectCallOwnership, error) {
	concrete, ok := objects.(*object.Service)
	if !ok || concrete == nil || uploads != concrete || cleaner != concrete || nilPort(authority) || owners == nil || owners.data == nil || concrete.RequireProjectCleanupStore(store) != nil || concrete.RequireProjectCleanupStore(owners.state().store) != nil || concrete.RequireProjectGuardBinding(guard, expected) != nil {
		return nil, failure(f.DependencyUnbound, nil)
	}
	service, err := New(store, owners, objects, uploads, cleaner, reads, resolver, auditing, cursors)
	if err != nil {
		return nil, err
	}
	return newProjectCallOwnership(service, guard, expected, authority)
}

func newProjectCallOwnership(service *Service, proof projectCallProcessProof, expected oc.ProcessID, authority oc.ProjectStopAuthority) (*ProjectCallOwnership, error) {
	if service == nil || service.data == nil || nilPort(proof) || expected.Validate() != nil || nilPort(authority) {
		return nil, failure(f.DependencyUnbound, nil)
	}
	instance, err := f.NewID[artifactInstanceID]()
	if err != nil {
		return nil, unavailable(err)
	}
	s := &projectCallOwnershipState{service: service, proof: proof, process: expected, instance: instance, authority: authority, owners: map[f.ID[artifactCallID]]*artifactCall{}, stops: map[i.ProjectID]*artifactStopCall{}, changed: make(chan struct{})}
	service.state().ownership = s
	return &ProjectCallOwnership{s}, nil
}
func (p *ProjectCallOwnership) Service() *Service {
	if p == nil || p.state == nil {
		return nil
	}
	return p.state.service
}
func (p *ProjectCallOwnership) Stop() {
	if p == nil || p.state == nil {
		return
	}
	s := p.state
	s.mu.Lock()
	s.stopped = true
	var cancels []context.CancelFunc
	for _, call := range s.owners {
		cancels = append(cancels, call.cancel)
	}
	s.signalLocked()
	s.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}
func (p *ProjectCallOwnership) Joined() bool {
	if p == nil || p.state == nil {
		return false
	}
	s := p.state
	s.mu.Lock()
	joined := s.stopped && len(s.owners) == 0 && len(s.stops) == 0 && s.controls == 0
	s.mu.Unlock()
	s.service.state().cleanupMu.Lock()
	clean := len(s.service.state().cleanupCalls) == 0
	s.service.state().cleanupMu.Unlock()
	r := s.service.state()
	r.mu.Lock()
	sources := len(r.projectSources) == 0 && (nilPort(s.sourceAuthority) || len(r.leases) == 0)
	r.mu.Unlock()
	return joined && clean && sources
}
func (p *ProjectCallOwnership) Drain(ctx context.Context) error {
	if ctx == nil {
		return invalid()
	}
	if p == nil || p.state == nil {
		return failure(f.DependencyUnbound, nil)
	}
	p.Stop()
	s := p.state
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		s.mu.Lock()
		var next *artifactCall
		for _, call := range s.owners {
			if call.returned && !call.settling {
				next = call
				break
			}
		}
		changed := s.changed
		s.mu.Unlock()
		if next != nil {
			if err := s.settle(ctx, next); err != nil {
				return err
			}
			continue
		}
		if p.Joined() {
			return nil
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
func (s *Service) RequireProjectStopBinding(objects *object.Service) error {
	if s == nil || s.data == nil || s.state().ownership == nil || objects == nil || s.state().objects != objects || s.state().uploads != objects || s.state().cleaner != objects {
		return failure(f.DependencyUnbound, nil)
	}
	return s.RequireProjectCleanupBinding()
}
func (s *projectCallOwnershipState) signalLocked() { close(s.changed); s.changed = make(chan struct{}) }

func (s *projectCallOwnershipState) requireProcess(ctx context.Context, tx f.Tx) error {
	if guard, ok := s.proof.(*object.ProcessGuard); ok {
		objects, same := s.service.state().objects.(*object.Service)
		if !same || objects == nil || objects.RequireProjectGuardBinding(guard, s.process) != nil {
			return failure(f.DependencyUnbound, nil)
		}
	}
	return s.proof.RequireCurrentProcessInTx(ctx, tx, s.process)
}

type artifactCallContextKey struct{}
type artifactCallRegistrationKey struct{}
type artifactCallWitness struct {
	state *projectCallOwnershipState
	call  *artifactCall
}

func (s *Service) beginProjectCall(ctx context.Context, invocation ac.Invocation, kind string) (context.Context, func(), error) {
	noop := func() {}
	if s == nil || s.data == nil {
		return ctx, noop, failure(f.DependencyUnbound, nil)
	}
	st := s.state().ownership
	if st == nil {
		return ctx, noop, nil
	}
	if ctx == nil || invocation.Validate() != nil || !artifactCallKind(kind) {
		return ctx, noop, invalid()
	}
	if err := ctx.Err(); err != nil {
		return ctx, noop, err
	}
	id, err := f.NewID[artifactCallID]()
	if err != nil {
		return ctx, noop, unavailable(err)
	}
	run, cancel := context.WithCancel(ctx)
	call := &artifactCall{record: artifactCallRecord{ID: id, Project: invocation.Details().ProjectID, Process: st.process, Instance: st.instance.String(), Kind: kind}, cancel: cancel}
	st.mu.Lock()
	if st.stopped || len(st.owners) >= 256 {
		st.mu.Unlock()
		cancel()
		return ctx, noop, failure(f.ResourceBusy, nil)
	}
	st.owners[id] = call
	st.signalLocked()
	st.mu.Unlock()
	run = context.WithValue(run, artifactCallContextKey{}, artifactCallWitness{st, call})
	result, registrationErr := st.register(run, invocation, call.record)
	st.mu.Lock()
	call.registration = result
	st.signalLocked()
	st.mu.Unlock()
	var once sync.Once
	done := func() { once.Do(func() { st.finish(run, call); cancel() }) }
	if registrationErr != nil {
		return run, done, registrationErr
	}
	if err = run.Err(); err != nil {
		return run, done, err
	}
	return run, done, nil
}
func (s *projectCallOwnershipState) finish(ctx context.Context, call *artifactCall) {
	s.mu.Lock()
	if s.owners[call.record.ID] != call {
		s.mu.Unlock()
		return
	}
	call.returned = true
	// Even a rejected registration owns the outer error/resource tail. Only
	// done, after body.Close and the original implementation have returned,
	// may retire it; NotCommitted has no ledger row to checkpoint.
	if call.registration.State() == f.NotCommitted {
		delete(s.owners, call.record.ID)
		s.signalLocked()
		s.mu.Unlock()
		return
	}
	s.signalLocked()
	s.mu.Unlock()
	checkpoint, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	_ = s.settle(checkpoint, call)
}
func (s *projectCallOwnershipState) settle(ctx context.Context, call *artifactCall) error {
	s.mu.Lock()
	if s.owners[call.record.ID] != call {
		s.mu.Unlock()
		return nil
	}
	if !call.returned || call.settling {
		s.mu.Unlock()
		return failure(f.ResourceBusy, nil)
	}
	call.settling = true
	s.signalLocked()
	s.mu.Unlock()
	result, err := s.checkpoint(ctx, call.record, call.registration.State() == f.Unknown)
	if err == nil {
		err = ctx.Err()
	}
	s.mu.Lock()
	call.checkpoint = result
	call.settling = false
	if err == nil {
		delete(s.owners, call.record.ID)
	}
	s.signalLocked()
	s.mu.Unlock()
	return err
}
func (s *projectCallOwnershipState) beginControl() (func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || s.controls >= 256 {
		return nil, failure(f.ResourceBusy, nil)
	}
	s.controls++
	s.signalLocked()
	return func() { s.mu.Lock(); s.controls--; s.signalLocked(); s.mu.Unlock() }, nil
}
func artifactCallKind(kind string) bool {
	return kind == "creation" || kind == "upload" || kind == "read" || kind == "converge"
}
func projectCleanupResultError(result f.CommitResult) error {
	if result.State() == f.Unknown {
		return projectCleanupUnknown(result)
	}
	return commitError(result)
}
func artifactJoinedError(ctx context.Context, result f.CommitResult, validated bool) error {
	err := projectCleanupResultError(result)
	if err == nil && (!validated || ctx.Err() != nil) {
		err = errors.Join(unavailable(nil), ctx.Err())
	}
	return err
}

func (s *projectCallOwnershipState) captureResult(ctx context.Context, result f.CommitResult) {
	if result.State() != f.Unknown || ctx.Value(artifactCallRegistrationKey{}) == s {
		return
	}
	w, ok := ctx.Value(artifactCallContextKey{}).(artifactCallWitness)
	if !ok || w.state != s || w.call == nil {
		return
	}
	s.mu.Lock()
	if s.owners[w.call.record.ID] == w.call && w.call.firstUnknown.AttemptID().Validate() != nil {
		w.call.firstUnknown = result
	}
	s.mu.Unlock()
}
func (s *Service) projectCallError(ctx context.Context, err error) error {
	if ctx == nil {
		return err
	}
	w, ok := ctx.Value(artifactCallContextKey{}).(artifactCallWitness)
	if !ok || w.state != s.state().ownership || w.call == nil {
		return err
	}
	w.state.mu.Lock()
	original := w.call.firstUnknown
	w.state.mu.Unlock()
	if original.AttemptID().Validate() == nil && original.State() == f.Unknown {
		return projectCleanupUnknown(original)
	}
	return err
}
