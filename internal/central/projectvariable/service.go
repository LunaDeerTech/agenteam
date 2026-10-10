package projectvariable

import (
	"context"
	"reflect"
	"sync"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

type ActivityAuthority interface {
	TouchActivityInTx(context.Context, f.Tx, i.Actor) error
}
type Dependencies struct {
	Authority      *Authority
	Projects       pc.ProjectAuthority
	Events         oc.Appender
	VariableEvents c.VariableEvents
	Audit          ac.Appender
	Activity       ActivityAuthority
	Cursors        cursor.Keyring
}
type Authority struct{ data func() *authorityState }
type authorityState struct {
	store  Store
	issuer oc.PlanIssuer
}

func NewAuthority(store Store) (*Authority, error) {
	if nilPort(store) {
		return nil, fault(f.DependencyUnbound)
	}
	if !reflect.TypeOf(store).Comparable() {
		return nil, fault(f.InvalidArgument)
	}
	state := &authorityState{store: store, issuer: oc.NewPlanIssuer()}
	return &Authority{data: func() *authorityState { return state }}, nil
}
func (a *Authority) state() *authorityState {
	if a == nil || a.data == nil {
		return nil
	}
	return a.data()
}

type Service struct{ data func() *serviceState }
type serviceState struct {
	store   Store
	deps    Dependencies
	mu      sync.Mutex
	stopped bool
	calls   map[*call]struct{}
	changed chan struct{}
}
type call struct {
	cancel        context.CancelFunc
	confirmations map[*confirmation]struct{}
}
type confirmation struct{ cancel context.CancelFunc }

func New(store Store, deps Dependencies) (*Service, error) {
	if nilPort(store) || deps.Authority.state() == nil || !sameStore(store, deps.Authority.state().store) || nilPort(deps.Events) || nilPort(deps.Activity) || nilPort(deps.Projects) || nilPort(deps.Audit) || !deps.VariableEvents.Valid() || deps.Cursors.Validate() != nil {
		return nil, fault(f.DependencyUnbound)
	}
	st := &serviceState{store: store, deps: deps, calls: map[*call]struct{}{}, changed: make(chan struct{})}
	return &Service{data: func() *serviceState { return st }}, nil
}
func (s *Service) state() *serviceState {
	if s == nil || s.data == nil {
		return nil
	}
	return s.data()
}
func (s *Service) begin(ctx context.Context) (context.Context, *call, func(), error) {
	st := s.state()
	if st == nil {
		return nil, nil, nil, fault(f.DependencyUnbound)
	}
	if ctx == nil {
		return nil, nil, nil, fault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, canceled(err)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.stopped {
		return nil, nil, nil, fault(f.ShuttingDown)
	}
	run, cancel := context.WithCancel(ctx)
	entry := &call{cancel: cancel, confirmations: map[*confirmation]struct{}{}}
	st.calls[entry] = struct{}{}
	var once sync.Once
	done := func() {
		once.Do(func() {
			cancel()
			st.mu.Lock()
			delete(st.calls, entry)
			close(st.changed)
			st.changed = make(chan struct{})
			st.mu.Unlock()
		})
	}
	return run, entry, done, nil
}
func (s *Service) Stop() {
	st := s.state()
	if st == nil {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	st.stopped = true
	for entry := range st.calls {
		entry.cancel()
		for c := range entry.confirmations {
			c.cancel()
		}
	}
}
func (s *Service) Drain(ctx context.Context) error {
	if ctx == nil {
		return fault(f.InvalidArgument)
	}
	st := s.state()
	if st == nil {
		return nil
	}
	for {
		st.mu.Lock()
		if len(st.calls) == 0 {
			st.mu.Unlock()
			return nil
		}
		changed := st.changed
		st.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

var _ c.Queries = (*Service)(nil)
