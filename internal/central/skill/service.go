package skill

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// ObjectPorts is one actual Object instance, including the original caller's
// budget-aware cleanup. There is no successful fallback for an unbound port.
type ObjectPorts interface {
	oc.Objects
	oc.Uploads
	oc.ReferenceCleanup
	InspectReferences(context.Context, oc.ObjectID) (oc.ReferenceInspection, error)
	DeleteUnreferencedWithinBudget(context.Context, oc.ObjectCleanupCause, oc.ObjectID) (oc.CleanupResult, error)
}
type Dependencies struct {
	Authority *Authority
	Objects   ObjectPorts
	Processes oc.ProcessAuthority
	ProcessID oc.ProcessID
	Bundle    BuiltinBundle
}

const MaxInitializations = 16

type serviceCall struct {
	ctx            context.Context
	cancel         context.CancelFunc
	initialization bool
	project        id.ProjectID
	workID         skillWorkID
	kind           workKind
	once           sync.Once
}
type serviceState struct {
	authority       *Authority
	objects         ObjectPorts
	processes       oc.ProcessAuthority
	process         oc.ProcessID
	bundle          BuiltinBundle
	frozen          frozenBundle
	confirmation    pc.InitializationPlanIssuer
	mu              sync.Mutex
	calls           map[*serviceCall]struct{}
	work            map[skillWorkID]*ownedWork
	initializations int
	stopped         bool
	changed         chan struct{}
}
type Service struct{ data func() *serviceState }

func New(d Dependencies) (*Service, error) {
	if d.Authority.state() == nil || nilPort(d.Objects) || nilPort(d.Processes) || d.ProcessID.Validate() != nil || d.Bundle.Validate() != nil {
		return nil, fault(f.DependencyUnbound)
	}
	frozen, e := freezeBundle(d.Bundle)
	if e != nil {
		return nil, e
	}
	state := &serviceState{authority: d.Authority, objects: d.Objects, processes: d.Processes, process: d.ProcessID, bundle: d.Bundle, frozen: frozen, confirmation: pc.NewInitializationPlanIssuer(), calls: map[*serviceCall]struct{}{}, work: map[skillWorkID]*ownedWork{}, changed: make(chan struct{})}
	return &Service{func() *serviceState { return state }}, nil
}
func (s *Service) state() *serviceState {
	if s == nil || s.data == nil {
		return nil
	}
	return s.data()
}
func (Service) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "skill_service") }
func (Service) MarshalJSON() ([]byte, error) { return []byte(`"skill_service"`), nil }
func (*Service) UnmarshalJSON([]byte) error  { return invalid() }
func (Service) LogValue() slog.Value         { return slog.StringValue("skill_service") }
func (s *Service) begin(ctx context.Context, initialization bool) (*serviceCall, error) {
	return s.admit(ctx, initialization, id.ProjectID{}, skillWorkID{}, "")
}

// Physical calls acquire their immutable reference before admission. The same
// ID is persisted if work registration succeeds; lifecycle snapshots also see
// the admitted interval before that transaction has committed.
func (s *Service) beginProjectWork(ctx context.Context, project id.ProjectID, kind workKind) (*serviceCall, error) {
	if project.Validate() != nil || kind != initializationWork && kind != packageReaderWork && !installedWork(kind) {
		return nil, invalid()
	}
	workID, e := f.NewID[skillWork]()
	if e != nil {
		return nil, unavailable(e)
	}
	return s.admit(ctx, kind == initializationWork || kind == installationWork, project, workID, kind)
}

func (s *Service) admit(ctx context.Context, initialization bool, project id.ProjectID, workID skillWorkID, kind workKind) (*serviceCall, error) {
	state := s.state()
	if state == nil {
		return nil, fault(f.DependencyUnbound)
	}
	if ctx == nil {
		return nil, invalid()
	}
	if e := ctx.Err(); e != nil {
		return nil, portError(e)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.stopped {
		return nil, fault(f.ShuttingDown)
	}
	if initialization && state.initializations >= MaxInitializations {
		return nil, fault(f.ResourceBusy)
	}
	workCtx, cancel := context.WithCancel(ctx)
	call := &serviceCall{ctx: workCtx, cancel: cancel, initialization: initialization, project: project, workID: workID, kind: kind}
	state.calls[call] = struct{}{}
	if initialization {
		state.initializations++
	}
	return call, nil
}
func (s *Service) end(call *serviceCall) {
	if call == nil {
		return
	}
	state := s.state()
	call.once.Do(func() {
		call.cancel()
		state.mu.Lock()
		defer state.mu.Unlock()
		delete(state.calls, call)
		if call.initialization {
			state.initializations--
		}
		close(state.changed)
		state.changed = make(chan struct{})
	})
}

// Stop cancels admitted calls but retains ownership until each actual call and
// its database/I/O tail invokes end. Cancellation is not a successful join.
func (s *Service) Stop() {
	state := s.state()
	if state == nil {
		return
	}
	state.mu.Lock()
	state.stopped = true
	calls := make([]*serviceCall, 0, len(state.calls))
	for c := range state.calls {
		calls = append(calls, c)
	}
	state.mu.Unlock()
	for _, call := range calls {
		call.cancel()
	}
}
func (s *Service) Drain(ctx context.Context) error {
	state := s.state()
	if state == nil {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil {
		return invalid()
	}
	s.Stop()
	for {
		state.mu.Lock()
		empty := len(state.calls) == 0 && len(state.work) == 0
		ready := make([]*ownedWork, 0, len(state.work))
		for _, work := range state.work {
			if work.returned || work.fact.kind == installationWork && work.installationCallerReturned {
				ready = append(ready, work)
			}
		}
		changed := state.changed
		state.mu.Unlock()
		if empty {
			return nil
		}
		for _, work := range ready {
			if work.fact.kind == installationWork {
				if e := s.joinInstallationDiscard(work); e != nil {
					return e
				}
			}
			if e := s.retireOwnedWork(ctx, work); e != nil {
				return e
			}
		}
		if len(ready) > 0 {
			continue
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
func (s *Service) Joined() bool {
	state := s.state()
	if state == nil {
		return false
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.stopped && len(state.calls) == 0 && len(state.work) == 0
}
