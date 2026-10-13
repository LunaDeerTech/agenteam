package work

import (
	"context"
	"sync"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type projectCallID struct{}
type projectCallInstance struct{}
type projectCallLane string
type projectCallClass string

const (
	projectCallStructure  projectCallLane  = "structure"
	projectCallTask       projectCallLane  = "task"
	projectCallBlocker    projectCallLane  = "blocker"
	projectCallTransition projectCallLane  = "transition"
	projectCallSprint     projectCallLane  = "sprint"
	projectCallRead       projectCallClass = "read"
	projectCallMutation   projectCallClass = "mutation"
	maxProjectCallOwners                   = 256
)

type projectCallProcessProof interface {
	RequireCurrentProcessInTx(context.Context, f.Tx, oc.ProcessID) error
	ConfirmStopped(context.Context, oc.ProcessID) error
}

type ProjectCallDependencies struct {
	Structure   Dependencies
	Tasks       TaskDependencies
	Transitions TaskTransitionDependencies
	Blockers    BlockerDependencies
	Sprints     SprintLifecycleDependencies
}

type projectCallRecord struct {
	ID       f.ID[projectCallID]       `json:"id"`
	Project  i.ProjectID               `json:"project_id"`
	Process  oc.ProcessID              `json:"process_id"`
	Instance f.ID[projectCallInstance] `json:"instance_id"`
	Lane     projectCallLane           `json:"lane"`
	Class    projectCallClass          `json:"class"`
	Phase    string                    `json:"phase"`
}

type projectCallOwner struct {
	record                                                     projectCallRecord
	entry                                                      *call
	ctx                                                        context.Context
	registration                                               f.CommitResult
	registrationKnown                                          bool
	classified                                                 bool
	replayActor                                                *i.Actor
	bodyReturned, returned, settling, closed, absent, stopping bool
	unknown                                                    *f.CommitResult
	unknownRegistration                                        bool
	confirmations                                              map[*confirmation]struct{}
}

type projectCallOwnershipState struct {
	store         Store
	proof         projectCallProcessProof
	process       oc.ProcessID
	instance      f.ID[projectCallInstance]
	authority     c.ProjectStopAuthority
	structure     *Service
	tasks         *TaskService
	transitions   *TaskTransitionService
	blockers      *BlockerService
	sprints       *SprintLifecycleService
	reader        *Reader
	taskReader    *TaskReader
	blockerReader *BlockerReader
	mu            sync.Mutex
	stopped       bool
	owners        map[f.ID[projectCallID]]*projectCallOwner
	stops         map[i.ProjectID]*projectStopCall
	cleanups      map[i.ProjectID]*ownedProjectCleanup
	changed       chan struct{}
}

// ProjectCallOwnership owns the five command services and, only with the
// WithReaders constructor, its three returned readers. Other independent
// readers and scheduler-owned borrowed transactions remain outside this graph.
// The original ProcessGuard must outlive its actual Drain/Joined tail.
type ProjectCallOwnership struct{ state *projectCallOwnershipState }

func NewProjectCallOwnership(store Store, deps ProjectCallDependencies, guard *object.ProcessGuard, expected oc.ProcessID, authority c.ProjectStopAuthority) (*ProjectCallOwnership, error) {
	return newProjectCallOwnership(store, deps, guard, expected, authority)
}

// NewProjectCallOwnershipWithReaders also owns the eight public HTTP reads.
// It constructs a private reader graph; readers supplied in deps are validated
// normally, but remain unchanged and do not acquire this bundle's lifetime.
func NewProjectCallOwnershipWithReaders(store Store, deps ProjectCallDependencies, keys cursor.Keyring, guard *object.ProcessGuard, expected oc.ProcessID, authority c.ProjectStopAuthority) (*ProjectCallOwnership, error) {
	return newProjectCallOwnershipWithReaders(store, deps, keys, guard, expected, authority)
}

func newProjectCallOwnershipWithReaders(store Store, deps ProjectCallDependencies, keys cursor.Keyring, proof projectCallProcessProof, expected oc.ProcessID, authority c.ProjectStopAuthority) (*ProjectCallOwnership, error) {
	pair, err := newProjectCallOwnership(store, deps, proof, expected, authority)
	if err != nil {
		return nil, err
	}
	s := pair.state
	if s.reader, err = NewReader(store, deps.Structure.Authority, keys); err != nil {
		return nil, err
	}
	if s.taskReader, err = NewTaskReader(store, deps.Structure.Authority, s.reader, keys); err != nil {
		return nil, err
	}
	if s.blockerReader, err = NewBlockerReader(store, deps.Structure.Authority, keys); err != nil {
		return nil, err
	}
	s.reader.state().ownership, s.taskReader.state().ownership, s.blockerReader.state().ownership = s, s, s
	// These original services retain their own outer call and Store return.
	// Placement reads only borrow that private witness inside their current Tx.
	s.tasks.state().deps.Structure = s.reader
	s.transitions.state().deps.Structure = s.reader
	s.blockers.state().deps.Structure = s.reader
	return pair, nil
}

func newProjectCallOwnership(store Store, deps ProjectCallDependencies, proof projectCallProcessProof, expected oc.ProcessID, authority c.ProjectStopAuthority) (*ProjectCallOwnership, error) {
	if nilPort(store) || nilPort(proof) || nilPort(authority) || expected.Validate() != nil || deps.Structure.Authority == nil ||
		deps.Tasks.Authority != deps.Structure.Authority || deps.Transitions.Authority != deps.Structure.Authority || deps.Blockers.Authority != deps.Structure.Authority || deps.Sprints.Authority != deps.Structure.Authority {
		return nil, fault(f.DependencyUnbound)
	}
	structure, err := New(store, deps.Structure)
	if err != nil {
		return nil, err
	}
	tasks, err := NewTask(store, deps.Tasks)
	if err != nil {
		return nil, err
	}
	transitions, err := NewTaskTransition(store, deps.Transitions)
	if err != nil {
		return nil, err
	}
	blockers, err := NewBlocker(store, deps.Blockers)
	if err != nil {
		return nil, err
	}
	sprints, err := NewSprintLifecycle(store, deps.Sprints)
	if err != nil {
		return nil, err
	}
	instance, err := f.NewID[projectCallInstance]()
	if err != nil {
		return nil, unavailable(err)
	}
	st := &projectCallOwnershipState{store: store, proof: proof, process: expected, instance: instance, authority: authority, structure: structure, tasks: tasks, transitions: transitions, blockers: blockers, sprints: sprints, owners: map[f.ID[projectCallID]]*projectCallOwner{}, stops: map[i.ProjectID]*projectStopCall{}, changed: make(chan struct{})}
	structure.state().ownership, tasks.state().ownership, transitions.state().ownership, blockers.state().ownership, sprints.base.state().ownership = st, st, st, st, st
	return &ProjectCallOwnership{st}, nil
}

func (p *ProjectCallOwnership) Service() *Service {
	if p == nil || p.state == nil {
		return nil
	}
	return p.state.structure
}
func (p *ProjectCallOwnership) TaskService() *TaskService {
	if p == nil || p.state == nil {
		return nil
	}
	return p.state.tasks
}
func (p *ProjectCallOwnership) TaskTransitionService() *TaskTransitionService {
	if p == nil || p.state == nil {
		return nil
	}
	return p.state.transitions
}
func (p *ProjectCallOwnership) BlockerService() *BlockerService {
	if p == nil || p.state == nil {
		return nil
	}
	return p.state.blockers
}
func (p *ProjectCallOwnership) SprintLifecycleService() *SprintLifecycleService {
	if p == nil || p.state == nil {
		return nil
	}
	return p.state.sprints
}

func (p *ProjectCallOwnership) Reader() *Reader {
	if p == nil || p.state == nil {
		return nil
	}
	return p.state.reader
}
func (p *ProjectCallOwnership) TaskReader() *TaskReader {
	if p == nil || p.state == nil {
		return nil
	}
	return p.state.taskReader
}
func (p *ProjectCallOwnership) BlockerReader() *BlockerReader {
	if p == nil || p.state == nil {
		return nil
	}
	return p.state.blockerReader
}

// Public readers use the matching original service call set, so Stop, Drain
// and Joined include their complete method and technical checkpoint tails.
func (s *projectCallOwnershipState) beginRead(ctx context.Context, actor i.Actor, project i.ProjectID, lane projectCallLane) (context.Context, func(), error) {
	if s == nil {
		return ctx, func() {}, nil
	}
	var entry *call
	var done func()
	var err error
	switch lane {
	case projectCallStructure:
		ctx, entry, done, err = s.structure.begin(ctx)
	case projectCallTask:
		ctx, entry, done, err = s.tasks.begin(ctx)
	case projectCallBlocker:
		ctx, entry, done, err = s.blockers.begin(ctx)
	default:
		return nil, nil, fault(f.DependencyUnbound)
	}
	if err != nil {
		return nil, nil, err
	}
	entry.project, entry.class = project, projectCallRead
	ctx, err = s.admit(ctx, entry, actor, lane)
	if err != nil {
		done()
		return nil, nil, err
	}
	return ctx, done, nil
}

func (p *ProjectCallOwnership) Stop() {
	if p == nil || p.state == nil {
		return
	}
	s := p.state
	s.mu.Lock()
	s.stopped = true
	s.signalLocked()
	s.mu.Unlock()
	s.structure.Stop()
	s.tasks.Stop()
	s.transitions.Stop()
	s.blockers.Stop()
	s.sprints.Stop()
}

func (p *ProjectCallOwnership) Drain(ctx context.Context) error {
	if p == nil || p.state == nil {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil {
		return fault(f.InvalidArgument)
	}
	p.Stop()
	for _, drain := range []func(context.Context) error{p.state.structure.Drain, p.state.tasks.Drain, p.state.transitions.Drain, p.state.blockers.Drain, p.state.sprints.Drain} {
		if err := drain(ctx); err != nil {
			return err
		}
	}
	for {
		p.state.mu.Lock()
		done := len(p.state.owners) == 0 && len(p.state.stops) == 0 && len(p.state.cleanups) == 0
		changed := p.state.changed
		p.state.mu.Unlock()
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
	joined := s.stopped && len(s.owners) == 0 && len(s.stops) == 0 && len(s.cleanups) == 0
	s.mu.Unlock()
	if !joined {
		return false
	}
	// Some original calls may still be validating before admission, or
	// unwinding a known registration failure. Keep every original local tail.
	for _, state := range []*serviceState{s.structure.state(), s.sprints.base.state()} {
		state.mu.Lock()
		done := state.stopped && len(state.calls) == 0
		state.mu.Unlock()
		if !done {
			return false
		}
	}
	tasks := s.tasks.state()
	tasks.mu.Lock()
	done := tasks.stopped && len(tasks.calls) == 0
	tasks.mu.Unlock()
	if !done {
		return false
	}
	transitions := s.transitions.state()
	transitions.mu.Lock()
	done = transitions.stopped && len(transitions.calls) == 0
	transitions.mu.Unlock()
	if !done {
		return false
	}
	blockers := s.blockers.state()
	blockers.mu.Lock()
	done = blockers.stopped && len(blockers.calls) == 0
	blockers.mu.Unlock()
	return done
}

func (s *projectCallOwnershipState) signalLocked() { close(s.changed); s.changed = make(chan struct{}) }

type projectCallContextKey struct{}
type projectCallWitness struct {
	state *projectCallOwnershipState
	owner *projectCallOwner
}

// The exact local owner exists before registration, and stays until the outer
// call's final returned hook even when registration is known not committed.
func (s *projectCallOwnershipState) admit(ctx context.Context, entry *call, actor i.Actor, lane projectCallLane) (context.Context, error) {
	if s == nil {
		return ctx, nil
	}
	if ctx == nil || entry == nil || entry.project.Validate() != nil || actor.Validate() != nil {
		return ctx, fault(f.InvalidArgument)
	}
	id, err := f.NewID[projectCallID]()
	if err != nil {
		return ctx, unavailable(err)
	}
	r := projectCallRecord{ID: id, Project: entry.project, Process: s.process, Instance: s.instance, Lane: lane, Class: entry.class, Phase: "running"}
	if !r.valid() {
		return ctx, fault(f.InvalidArgument)
	}
	o := &projectCallOwner{record: r, entry: entry, ctx: ctx, confirmations: map[*confirmation]struct{}{}}
	s.mu.Lock()
	if s.stopped || s.cleanups[entry.project] != nil || len(s.owners) >= maxProjectCallOwners {
		s.mu.Unlock()
		return ctx, fault(f.ResourceBusy)
	}
	if entry.owner != nil {
		s.mu.Unlock()
		return ctx, fault(f.InvalidState)
	}
	entry.owner = o
	s.owners[id] = o
	s.signalLocked()
	s.mu.Unlock()
	ctx = context.WithValue(ctx, projectCallContextKey{}, projectCallWitness{s, o})
	result, validated := s.register(ctx, actor, o)
	s.mu.Lock()
	o.registration = result
	o.registrationKnown = result.State() == f.Committed && validated
	if result.State() == f.Unknown {
		original := result
		o.unknown = &original
		o.unknownRegistration = true
	}
	s.signalLocked()
	s.mu.Unlock()
	if err = txError(result); err != nil {
		return ctx, err
	}
	if !validated {
		return ctx, unavailable(nil)
	}
	return ctx, ctx.Err()
}

func (s *projectCallOwnershipState) finish(entry *call) {
	if s == nil || entry == nil || entry.owner == nil {
		return
	}
	o := entry.owner
	s.mu.Lock()
	if s.owners[o.record.ID] != o {
		s.mu.Unlock()
		return
	}
	o.bodyReturned = true
	canClose := o.registrationKnown && o.unknown == nil
	s.signalLocked()
	s.mu.Unlock()
	if !canClose {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(o.ctx), 3*time.Second)
	defer cancel()
	_, _ = s.settle(ctx, o, nil, false)
}

// The entry's original local done tail calls this only after its own cleanup.
func (s *projectCallOwnershipState) returned(entry *call) {
	if s == nil || entry == nil || entry.owner == nil {
		return
	}
	o := entry.owner
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.owners[o.record.ID] != o {
		return
	}
	o.returned = true
	if o.registration.State() == f.NotCommitted || o.closed || o.absent {
		delete(s.owners, o.record.ID)
	}
	s.signalLocked()
}

func (s *projectCallOwnershipState) confirmation(entry *call, token *confirmation, add bool) {
	if s == nil || entry == nil || entry.owner == nil || token == nil {
		return
	}
	o := entry.owner
	s.mu.Lock()
	if add {
		o.confirmations[token] = struct{}{}
	} else {
		delete(o.confirmations, token)
	}
	cancel := add && (o.stopping || s.stopped)
	s.mu.Unlock()
	if cancel {
		token.cancel()
	}
}

func (s *projectCallOwnershipState) laneJoined(lane projectCallLane) bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, o := range s.owners {
		if o.record.Lane == lane {
			return false
		}
	}
	return true
}

func (s *projectCallOwnershipState) drainReturned(ctx context.Context, lane projectCallLane) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		return fault(f.InvalidArgument)
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		s.mu.Lock()
		var next *projectCallOwner
		pending := false
		for _, o := range s.owners {
			if o.record.Lane == lane {
				pending = true
				if o.returned && !o.settling {
					next = o
					break
				}
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
