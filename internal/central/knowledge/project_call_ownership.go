package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type projectCallID struct{}
type projectCallInstance struct{}
type projectCallLane string

const (
	projectCallRead      projectCallLane = "read"
	projectCallCommand   projectCallLane = "command"
	projectCallSource    projectCallLane = "source"
	projectCallRecovery  projectCallLane = "recovery"
	maxProjectCallOwners                 = 256
)

type projectCallProof interface {
	RequireCurrentProcessInTx(context.Context, f.Tx, oc.ProcessID) error
	ConfirmStopped(context.Context, oc.ProcessID) error
}
type projectCallIdentity struct {
	Format   int      `json:"format_version"`
	Method   string   `json:"method"`
	User     string   `json:"user_id,omitempty"`
	Document string   `json:"document_id,omitempty"`
	Command  string   `json:"command,omitempty"`
	Key      string   `json:"command_key,omitempty"`
	Digest   f.Digest `json:"semantic_digest,omitempty"`
}
type projectCallRecord struct {
	ID         f.ID[projectCallID]       `json:"id"`
	Project    id.ProjectID              `json:"project_id"`
	Source     string                    `json:"source_project_id"`
	Process    oc.ProcessID              `json:"process_id"`
	Instance   f.ID[projectCallInstance] `json:"instance_id"`
	Lane       projectCallLane           `json:"lane"`
	Identity   json.RawMessage           `json:"identity"`
	Digest     f.Digest                  `json:"identity_digest"`
	Phase      string                    `json:"phase"`
	Registered string                    `json:"registered_at"`
	Closed     string                    `json:"closed_at"`
}

func (r projectCallRecord) touches(p id.ProjectID) bool {
	return r.Project == p || r.Source == p.String()
}
func (r projectCallRecord) projects() []id.ProjectID {
	out := []id.ProjectID{r.Project}
	if r.Source != "" && r.Source != r.Project.String() {
		p, _ := f.ParseID[id.Project](r.Source)
		out = append(out, p)
	}
	return out
}

type projectCallOwner struct {
	record                                                                                   projectCallRecord
	actor                                                                                    id.Actor
	ctx                                                                                      context.Context
	cancel                                                                                   context.CancelFunc
	originalDone                                                                             func()
	references                                                                               int
	bodyReturned, returned, returning, retiring, settling, registrationKnown, closed, absent bool
	unknown                                                                                  *f.CommitResult
	unknownRegistration                                                                      bool
	originalError                                                                            error
}

// An entry is one original outer call or one synchronous borrow of that call.
// A borrowed method returning never claims that the caller's Store returned.
type projectCallEntry struct {
	state   *projectCallState
	owner   *projectCallOwner
	root    bool
	cleanup *ownedProjectCleanup
	once    sync.Once
}
type projectCallState struct {
	store     Store
	authority *Authority
	service   *Service
	resolver  *SourceResolver
	reads     *object.SourceReads
	proof     projectCallProof
	process   oc.ProcessID
	instance  f.ID[projectCallInstance]
	stop      kc.ProjectStopAuthority
	mu        sync.Mutex
	stopped   bool
	owners    map[f.ID[projectCallID]]*projectCallOwner
	blocked   map[id.ProjectID]bool
	stops     map[id.ProjectID]*projectStopCall
	cleanups  map[id.ProjectID]*ownedProjectCleanup
	changed   chan struct{}
}
type ProjectCallOwnership struct{ state *projectCallState }

// The complete Source graph is constructed before publishing this pair. A
// legacy resolver or SourceReads must never be attached to an already live pair.
func NewProjectCallOwnership(store Store, authority *Authority, deps Dependencies, objects *object.Service, guard *object.ProcessGuard, expected oc.ProcessID, stop kc.ProjectStopAuthority, sourceStop oc.KnowledgeProjectSourceStopAuthority) (*ProjectCallOwnership, error) {
	if nilPort(store) || authority.state() == nil || !sameStore(store, authority.state().store) || objects == nil || guard == nil || expected.Validate() != nil || nilPort(stop) || nilPort(sourceStop) || !nilPort(deps.Sources) || !nilPort(deps.SourceReads) {
		return nil, fault(f.DependencyUnbound)
	}
	if nilPort(deps.Projects) || reflect.TypeOf(deps.Projects) != reflect.TypeOf(authority.state().projects) || !reflect.TypeOf(deps.Projects).Comparable() || deps.Projects != authority.state().projects {
		return nil, fault(f.DependencyUnbound)
	}
	if deps.Objects != objects || deps.Uploads != objects || deps.ReferenceCleanup != objects || deps.ObjectCleanup != objects {
		return nil, fault(f.DependencyUnbound)
	}
	if objects.RequireProjectCleanupStore(store) != nil || objects.RequireProjectGuardBinding(guard, expected) != nil {
		return nil, fault(f.DependencyUnbound)
	}
	if authority.state().ownership != nil {
		return nil, fault(f.DependencyUnbound)
	}
	if nilPort(deps.Processes) || deps.Processes.CurrentProcess().String() != expected.String() {
		return nil, fault(f.DependencyUnbound)
	}
	instance, err := f.NewID[projectCallInstance]()
	if err != nil {
		return nil, unavailable(err)
	}
	state := &projectCallState{store: store, authority: authority, proof: guard, process: expected, instance: instance, stop: stop, owners: map[f.ID[projectCallID]]*projectCallOwner{}, blocked: map[id.ProjectID]bool{}, stops: map[id.ProjectID]*projectStopCall{}, cleanups: map[id.ProjectID]*ownedProjectCleanup{}, changed: make(chan struct{})}
	resolver, err := newOwnedSourceResolver(store, authority, objects, state)
	if err != nil {
		return nil, err
	}
	reads, err := object.NewSourceReadsWithKnowledgeProjectStop(objects, resolver, sourceStop)
	if err != nil {
		return nil, err
	}
	deps.Sources, deps.SourceReads = resolver, reads
	service, err := New(store, deps)
	if err != nil {
		return nil, err
	}
	state.service, state.resolver, state.reads = service, resolver, reads
	service.state().ownership = state
	authority.state().ownership = state
	return &ProjectCallOwnership{state}, nil
}
func (p *ProjectCallOwnership) Service() *Service {
	if p == nil || p.state == nil {
		return nil
	}
	return p.state.service
}
func (p *ProjectCallOwnership) SourceResolver() *SourceResolver {
	if p == nil || p.state == nil {
		return nil
	}
	return p.state.resolver
}
func (p *ProjectCallOwnership) SourceReads() *object.SourceReads {
	if p == nil || p.state == nil {
		return nil
	}
	return p.state.reads
}
func (s *projectCallState) signalLocked() { close(s.changed); s.changed = make(chan struct{}) }
func projectCallDigest(raw []byte) f.Digest {
	v := sha256.Sum256(raw)
	return f.Digest("sha256:" + hex.EncodeToString(v[:]))
}

type projectCallContextKey struct{}
type projectIdentityContextKey struct{}
type projectCallWitness struct {
	state *projectCallState
	owner *projectCallOwner
}

func withProjectCallIdentity(ctx context.Context, identity projectCallIdentity) context.Context {
	if ctx == nil {
		return nil
	}
	return context.WithValue(ctx, projectIdentityContextKey{}, identity)
}
func (s *Service) beginProjectCall(ctx context.Context, actor id.Actor, target id.ProjectID, source *id.ProjectID, lane projectCallLane) (context.Context, *projectCallEntry, error) {
	if s.state() == nil {
		return nil, nil, fault(f.DependencyUnbound)
	}
	state := s.state().ownership
	if state == nil {
		run, done, err := s.begin(ctx)
		if err != nil {
			return nil, nil, err
		}
		return run, &projectCallEntry{owner: &projectCallOwner{originalDone: done}, root: true}, nil
	}
	if ctx == nil || target.Validate() != nil || source != nil && source.Validate() != nil {
		return nil, nil, fault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, unavailable(err)
	}
	if w, ok := ctx.Value(projectCleanupContextKey{}).(projectCleanupWitness); ok {
		if err := s.requireProjectCall(ctx, target, source, lane); err != nil {
			return nil, nil, err
		}
		state.mu.Lock()
		if !w.call.active || state.cleanups[target] != w.call {
			state.mu.Unlock()
			return nil, nil, fault(f.Forbidden)
		}
		w.call.refs++
		state.signalLocked()
		state.mu.Unlock()
		return ctx, &projectCallEntry{state: state, cleanup: w.call}, nil
	}
	if w, ok := ctx.Value(projectCallContextKey{}).(projectCallWitness); ok {
		if err := s.requireProjectCall(ctx, target, source, lane); err != nil {
			return nil, nil, err
		}
		state.mu.Lock()
		if state.owners[w.owner.record.ID] != w.owner || w.owner.returned || w.owner.settling || w.owner.references == 0 {
			state.mu.Unlock()
			return nil, nil, fault(f.Forbidden)
		}
		w.owner.references++
		state.signalLocked()
		state.mu.Unlock()
		return ctx, &projectCallEntry{state: state, owner: w.owner}, nil
	}
	if lane != projectCallRecovery && (actor.Validate() != nil || actor.Details().Kind != id.Human) {
		return nil, nil, fault(f.Forbidden)
	}
	identity, _ := ctx.Value(projectIdentityContextKey{}).(projectCallIdentity)
	if identity.Format == 0 {
		identity = projectCallIdentity{Format: 1, Method: string(lane), User: actor.Details().UserID}
	}
	if identity.Format != 1 || identity.Method == "" || len(identity.Method) > 64 || identity.User != actor.Details().UserID {
		return nil, nil, fault(f.InvalidArgument)
	}
	raw, err := json.Marshal(identity)
	if err != nil {
		return nil, nil, fault(f.InvalidArgument)
	}
	raw, err = canonicalProjectIdentity(raw)
	if err != nil {
		return nil, nil, fault(f.InvalidArgument)
	}
	switch lane {
	case projectCallRead, projectCallCommand, projectCallSource, projectCallRecovery:
	default:
		return nil, nil, fault(f.InvalidArgument)
	}
	key, err := f.NewID[projectCallID]()
	if err != nil {
		return nil, nil, unavailable(err)
	}
	run, done, local, err := s.beginLocal(ctx)
	if err != nil {
		return nil, nil, err
	}
	record := projectCallRecord{ID: key, Project: target, Process: state.process, Instance: state.instance, Lane: lane, Identity: raw, Digest: projectCallDigest(raw), Phase: "running"}
	if source != nil && *source != target {
		record.Source = source.String()
	}
	run, cancel := context.WithCancel(run)
	owner := &projectCallOwner{record: record, actor: actor, ctx: run, cancel: cancel, originalDone: done, references: 1}
	state.mu.Lock()
	denied := state.stopped || len(state.owners) >= maxProjectCallOwners
	for _, p := range record.projects() {
		denied = denied || state.blocked[p] || state.stops[p] != nil || state.cleanups[p] != nil
	}
	if denied {
		state.mu.Unlock()
		cancel()
		done()
		return nil, nil, fault(f.ResourceBusy)
	}
	state.owners[key] = owner
	state.signalLocked()
	state.mu.Unlock()
	st := s.state()
	st.mu.Lock()
	local.owned = true
	st.mu.Unlock()
	entry := &projectCallEntry{state: state, owner: owner, root: true}
	run = context.WithValue(run, projectCallContextKey{}, projectCallWitness{state, owner})
	owner.ctx = run
	result, valid := state.register(run, owner)
	state.mu.Lock()
	owner.registrationKnown = result.State() == f.Committed && valid
	owner.absent = result.State() == f.NotCommitted
	if result.State() == f.Unknown {
		saved := result
		owner.unknown = &saved
		owner.unknownRegistration = true
	}
	state.signalLocked()
	state.mu.Unlock()
	if err := txError(result); err != nil {
		return run, entry, err
	}
	if !valid {
		return run, entry, unavailable(nil)
	}
	return run, entry, nil
}
func (s *Service) requireProjectCall(ctx context.Context, target id.ProjectID, source *id.ProjectID, lane projectCallLane) error {
	if s.state() == nil {
		return fault(f.DependencyUnbound)
	}
	state := s.state().ownership
	if state == nil {
		return nil
	}
	if ctx == nil {
		return fault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return unavailable(err)
	}
	if w, ok := ctx.Value(projectCleanupContextKey{}).(projectCleanupWitness); ok {
		state.mu.Lock()
		defer state.mu.Unlock()
		if w.state != state || w.call == nil || !w.call.active || state.cleanups[target] != w.call || source != nil && *source != target {
			return fault(f.Forbidden)
		}
		if lane != projectCallRead && lane != projectCallSource && lane != projectCallRecovery {
			return fault(f.Forbidden)
		}
		return nil
	}
	w, ok := ctx.Value(projectCallContextKey{}).(projectCallWitness)
	if !ok || w.state != state || w.owner == nil {
		return fault(f.DependencyUnbound)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	o := w.owner
	if state.owners[o.record.ID] != o || !o.registrationKnown || o.returned || o.settling || o.references == 0 || !o.record.touches(target) || source != nil && !o.record.touches(*source) {
		return fault(f.Forbidden)
	}
	if lane != projectCallRead && lane != projectCallSource && lane != o.record.Lane {
		return fault(f.Forbidden)
	}
	return nil
}
func (s *Service) finishProjectCall(entry *projectCallEntry, resultErr *error) {
	if entry == nil || !entry.root || entry.state == nil {
		return
	}
	state := entry.state
	state.mu.Lock()
	defer state.mu.Unlock()
	if resultErr != nil && entry.owner.originalError == nil {
		entry.owner.originalError = *resultErr
	}
	entry.owner.bodyReturned = true
	state.signalLocked()
}
func (s *Service) projectCallReturned(entry *projectCallEntry) error {
	if entry == nil {
		return nil
	}
	var result error
	entry.once.Do(func() {
		if entry.state == nil {
			entry.owner.originalDone()
			return
		}
		if entry.cleanup != nil {
			entry.state.mu.Lock()
			entry.cleanup.refs--
			entry.state.signalLocked()
			entry.state.mu.Unlock()
			return
		}
		state, o := entry.state, entry.owner
		state.mu.Lock()
		o.references--
		if entry.root {
			o.bodyReturned = true
		}
		ready := o.bodyReturned && o.references == 0 && !o.returning && !o.returned
		if ready {
			o.returning = true
		}
		uncertain := o.unknown != nil
		absent := o.absent
		state.signalLocked()
		state.mu.Unlock()
		if !ready {
			return
		}
		// The first Unknown return never observes itself. A later explicit caller
		// must perform the read-only confirmation with its own original context.
		if !uncertain && !absent {
			_, result = state.settle(o.ctx, o, nil, false)
		}
		state.mu.Lock()
		o.returned = true
		o.returning = false
		state.signalLocked()
		state.mu.Unlock()
		state.retireOwner(o)
	})
	return result
}
func (s *projectCallState) retireOwner(o *projectCallOwner) {
	s.mu.Lock()
	ready := s.owners[o.record.ID] == o && o.returned && o.references == 0 && !o.retiring && (o.closed || o.absent) && o.unknown == nil
	if ready {
		o.retiring = true
		s.signalLocked()
	}
	s.mu.Unlock()
	if ready {
		o.cancel()
		o.originalDone()
		s.mu.Lock()
		delete(s.owners, o.record.ID)
		s.signalLocked()
		s.mu.Unlock()
	}
}
func (s *Service) projectCallDone(entry *projectCallEntry, resultErr *error) {
	s.finishProjectCall(entry, resultErr)
	err := s.projectCallReturned(entry)
	if resultErr != nil && *resultErr == nil && err != nil {
		*resultErr = err
	}
}
func (p *ProjectCallOwnership) Stop() {
	if p == nil || p.state == nil {
		return
	}
	s := p.state
	s.mu.Lock()
	s.stopped = true
	var owners []*projectCallOwner
	for _, o := range s.owners {
		owners = append(owners, o)
	}
	s.signalLocked()
	s.mu.Unlock()
	p.Service().stopLegacy()
	for _, o := range owners {
		o.cancel()
	}
}
func (p *ProjectCallOwnership) Joined() bool {
	if p == nil || p.state == nil {
		return false
	}
	s := p.state
	s.mu.Lock()
	ready := s.stopped && len(s.owners) == 0 && len(s.stops) == 0 && len(s.cleanups) == 0
	s.mu.Unlock()
	st := s.service.state()
	st.mu.Lock()
	defer st.mu.Unlock()
	return ready && len(st.calls) == 0 && len(st.retiringPublications) == 0 && len(st.projectRetirementTails) == 0
}
func (p *ProjectCallOwnership) Drain(ctx context.Context) error {
	if p == nil || p.state == nil {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil {
		return fault(f.InvalidArgument)
	}
	p.Stop()
	s := p.state
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.service.retryPublicationRetirements(ctx, f.ID[command]{}); err != nil {
			return err
		}
		s.mu.Lock()
		var next *projectCallOwner
		for _, o := range s.owners {
			if o.returned && o.references == 0 && !o.settling && !o.retiring && (next == nil || o.record.ID.String() < next.record.ID.String()) {
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
			s.retireOwner(next)
			if observed {
				return fault(f.ResourceBusy)
			}
			continue
		}
		if p.Joined() {
			return nil
		}
		st := s.service.state()
		st.mu.Lock()
		// A completed caller may transfer its original retirement after the
		// retry snapshot but before this replacement channel is observed.
		if len(st.retiringPublications) != 0 || len(st.projectRetirementTails) != 0 {
			st.mu.Unlock()
			continue
		}
		local := st.changed
		st.mu.Unlock()
		select {
		case <-changed:
		case <-local:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
func (s *Service) projectCallTx(ctx context.Context, tx f.Tx, p id.ProjectID) error {
	st := s.state()
	if st == nil {
		return fault(f.DependencyUnbound)
	}
	if st.ownership == nil {
		return nil
	}
	if err := s.requireProjectCall(ctx, p, nil, projectCallRead); err != nil {
		return err
	}
	if w, ok := ctx.Value(projectCleanupContextKey{}).(projectCleanupWitness); ok {
		return s.requireProjectCleanupCallInTx(ctx, tx, p, w.call.cause.OperationID)
	}
	w := ctx.Value(projectCallContextKey{}).(projectCallWitness)
	if err := st.store.RequireHeldLocks(ctx, tx, []f.LockRequest{knowledgeProjectLock(p, f.Shared)}); err != nil {
		return portError(err)
	}
	if err := st.ownership.proof.RequireCurrentProcessInTx(ctx, tx, st.ownership.process); err != nil {
		return portError(err)
	}
	x, err := st.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	row, err := loadProjectCall(ctx, x, w.owner.record.ID)
	if err != nil {
		return err
	}
	if row == nil || row.Phase != "running" || !w.owner.record.sameOwner(*row) {
		return fault(f.InvalidState)
	}
	return nil
}
func knowledgeProjectLock(p id.ProjectID, mode f.LockMode) f.LockRequest {
	k, _ := f.ProjectLock(p.String())
	return f.LockRequest{Key: k, Mode: mode}
}
func projectCallRawError(err error) (f.CommitResult, bool) {
	var original commitFailure
	if errors.As(err, &original) {
		return original.result, true
	}
	return f.CommitResult{}, false
}

var _ pc.ProjectLifecycleParticipant = (*projectCleanupParticipant)(nil)
