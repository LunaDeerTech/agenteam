package projectvariable

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

type projectCallIdentity struct{}
type projectCallInstance struct{}
type projectCallLane string

const (
	projectCallOrdinary  projectCallLane = "ordinary"
	projectCallSecret    projectCallLane = "secret"
	maxProjectCallOwners                 = 256
)

// Only the concrete ProcessGuard is accepted by the public factory. This
// private port also permits controlled package tests without fabricating an
// Object guard's private bound state or claiming a real death proof.
type projectCallProcessProof interface {
	RequireCurrentProcessInTx(context.Context, f.Tx, oc.ProcessID) error
	ConfirmStopped(context.Context, oc.ProcessID) error
}

type projectCallRecord struct {
	ID       f.ID[projectCallIdentity] `json:"id"`
	Project  i.ProjectID               `json:"project_id"`
	Process  oc.ProcessID              `json:"process_id"`
	Instance f.ID[projectCallInstance] `json:"instance_id"`
	Lane     projectCallLane           `json:"lane"`
	Class    string                    `json:"class"`
	Phase    string                    `json:"phase"`
}
type projectCallOwner struct {
	record       projectCallRecord
	call         *call
	returned     bool
	settling     bool
	registration f.CommitResult
	checkpoint   f.CommitResult
	err          error
}
type projectCallOwnershipState struct {
	store    Store
	proof    projectCallProcessProof
	process  oc.ProcessID
	instance f.ID[projectCallInstance]
	service  *Service
	secrets  *SecretService
	stopper  *ProjectCallStopper
	writes   *SecretWriteAuthority
	mu       sync.Mutex
	owners   map[f.ID[projectCallIdentity]]*projectCallOwner
	changed  chan struct{}
}

// ProjectCallOwnership is an explicitly opted-in pair. Its registered-call
// observations cover Project business, not foreign Secret technical SQL, a
// full Project participant or reference cleanup authority. The composition root
// must hold the guard until Joined is true.
type ProjectCallOwnership struct{ state *projectCallOwnershipState }

// Construction performs no I/O and permits construction before guard binding.
// Every admission and terminal checkpoint checks the actual original live Tx.
func NewProjectCallOwnership(store Store, ordinary Dependencies, secrets SecretDependencies, guard *object.ProcessGuard, expected oc.ProcessID) (*ProjectCallOwnership, error) {
	return newProjectCallOwnership(store, ordinary, secrets, guard, expected)
}

func newProjectCallOwnership(store Store, ordinary Dependencies, secrets SecretDependencies, proof projectCallProcessProof, expected oc.ProcessID) (*ProjectCallOwnership, error) {
	if nilPort(proof) || expected.Validate() != nil {
		return nil, fault(f.DependencyUnbound)
	}
	service, err := New(store, ordinary)
	if err != nil {
		return nil, err
	}
	secretService, err := NewSecret(store, secrets)
	if err != nil {
		return nil, err
	}
	stopper, err := NewProjectCallStopper(service, secretService)
	if err != nil {
		return nil, err
	}
	instance, err := f.NewID[projectCallInstance]()
	if err != nil {
		return nil, unavailable(err)
	}
	state := &projectCallOwnershipState{store: store, proof: proof, process: expected, instance: instance, service: service, secrets: secretService, stopper: stopper, writes: secrets.Writes, owners: make(map[f.ID[projectCallIdentity]]*projectCallOwner), changed: make(chan struct{})}
	service.state().ownership, secretService.state().ownership = state, state
	return &ProjectCallOwnership{state: state}, nil
}
func (p *ProjectCallOwnership) Service() *Service {
	if p == nil || p.state == nil {
		return nil
	}
	return p.state.service
}
func (p *ProjectCallOwnership) SecretService() *SecretService {
	if p == nil || p.state == nil {
		return nil
	}
	return p.state.secrets
}
func (p *ProjectCallOwnership) LocalStopper() *ProjectCallStopper {
	if p == nil || p.state == nil {
		return nil
	}
	return p.state.stopper
}
func (p *ProjectCallOwnership) Stop() {
	if p != nil && p.state != nil {
		p.state.service.Stop()
		p.state.secrets.Stop()
	}
}
func (p *ProjectCallOwnership) Drain(ctx context.Context) error {
	if p == nil || p.state == nil {
		return fault(f.DependencyUnbound)
	}
	p.Stop()
	if err := p.state.service.Drain(ctx); err != nil {
		return err
	}
	return p.state.secrets.Drain(ctx)
}
func (p *ProjectCallOwnership) Joined() bool {
	if p == nil || p.state == nil {
		return false
	}
	s := p.state
	a, b := s.service.state(), s.secrets.state()
	a.mu.Lock()
	b.mu.Lock()
	s.mu.Lock()
	defer a.mu.Unlock()
	defer b.mu.Unlock()
	defer s.mu.Unlock()
	return len(a.calls) == 0 && len(b.calls) == 0 && len(s.owners) == 0
}
func (ProjectCallOwnership) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "project_variable_call_ownership")
}
func (ProjectCallOwnership) LogValue() slog.Value {
	return slog.StringValue("project_variable_call_ownership")
}

type projectCallContextKey struct{}
type projectCallWitness struct {
	state *projectCallOwnershipState
	owner *projectCallOwner
}

func (s *projectCallOwnershipState) signalLocked() { close(s.changed); s.changed = make(chan struct{}) }
func (s *projectCallOwnershipState) laneJoined(lane projectCallLane) bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, owner := range s.owners {
		if owner.record.Lane == lane {
			return false
		}
	}
	return true
}

// admit runs after pure argument validation and before any business discovery.
// The returned context retains the exact owner even when registration Unknown
// forbids business work; its outer caller must still call finish.
func (s *projectCallOwnershipState) admit(ctx context.Context, call *call, actor i.Actor, lane projectCallLane) (context.Context, error) {
	if s == nil {
		return ctx, nil
	}
	if call == nil || call.project.Validate() != nil || (call.kind != readCall && call.kind != mutationCall) || (lane != projectCallOrdinary && lane != projectCallSecret) {
		return ctx, fault(f.InvalidArgument)
	}
	id, err := f.NewID[projectCallIdentity]()
	if err != nil {
		return ctx, unavailable(err)
	}
	class := "read"
	if call.kind == mutationCall {
		class = "mutation"
	}
	owner := &projectCallOwner{record: projectCallRecord{ID: id, Project: call.project, Process: s.process, Instance: s.instance, Lane: lane, Class: class, Phase: "running"}, call: call}
	s.mu.Lock()
	if len(s.owners) >= maxProjectCallOwners {
		s.mu.Unlock()
		return ctx, fault(f.ResourceBusy)
	}
	s.owners[id] = owner
	s.signalLocked()
	s.mu.Unlock()
	ctx = context.WithValue(ctx, projectCallContextKey{}, projectCallWitness{s, owner})
	result := s.register(ctx, actor, owner.record)
	s.mu.Lock()
	owner.registration = result
	if result.State() == f.NotCommitted {
		delete(s.owners, id)
	}
	s.signalLocked()
	s.mu.Unlock()
	if err = txError(result); err != nil {
		return ctx, err
	}
	return ctx, ctx.Err()
}

// finish never rewrites the already determined business result. The original
// local call map still owns this synchronous <=3s checkpoint until its actual return.
func (s *projectCallOwnershipState) finish(ctx context.Context) {
	if s == nil {
		return
	}
	w, ok := ctx.Value(projectCallContextKey{}).(projectCallWitness)
	if !ok || w.state != s || w.owner == nil {
		return
	}
	s.mu.Lock()
	if s.owners[w.owner.record.ID] != w.owner {
		s.mu.Unlock()
		return
	}
	w.owner.returned = true
	s.signalLocked()
	s.mu.Unlock()
	checkpoint, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	_ = s.settle(checkpoint, w.owner)
}

func (s *projectCallOwnershipState) settle(ctx context.Context, owner *projectCallOwner) error {
	s.mu.Lock()
	if s.owners[owner.record.ID] != owner {
		s.mu.Unlock()
		return nil
	}
	if !owner.returned || owner.settling {
		s.mu.Unlock()
		return fault(f.ResourceBusy)
	}
	owner.settling = true
	s.signalLocked()
	s.mu.Unlock()
	result := s.checkpoint(ctx, owner.record, owner.registration.State() == f.Unknown)
	err := txError(result)
	if err == nil {
		err = ctx.Err()
	}
	s.mu.Lock()
	owner.checkpoint, owner.err, owner.settling = result, err, false
	if err == nil {
		delete(s.owners, owner.record.ID)
	}
	s.signalLocked()
	s.mu.Unlock()
	return err
}

// This continuation consumes only retained original IDs/returned proofs. It
// does not reopen service admission after Stop or recurse through ObserveStop.
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
		for _, owner := range s.owners {
			if owner.record.Lane != lane {
				continue
			}
			pending = true
			if owner.returned && !owner.settling {
				next = owner
				break
			}
		}
		changed := s.changed
		s.mu.Unlock()
		if !pending {
			return nil
		}
		if next != nil {
			if err := s.settle(ctx, next); err != nil {
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

// The caller holds this lane's service mutex; ownership never takes a service
// mutex in reverse order. Returned-unconfirmed owners remain visible after the
// original service call has retired from its local map.
func (s *projectCallOwnershipState) retainedProjectCalls(lane projectCallLane, calls map[*call]struct{}, selected func(*call) bool) int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := 0
	for _, owner := range s.owners {
		if owner.record.Lane == lane && selected(owner.call) {
			if _, active := calls[owner.call]; !active {
				pending++
			}
		}
	}
	return pending
}
