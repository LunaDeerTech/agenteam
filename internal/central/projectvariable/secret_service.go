package projectvariable

import (
	"context"
	"reflect"
	"sync"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type SecretDependencies struct {
	Authority      *Authority
	Writes         *SecretWriteAuthority
	Secrets        sc.ProjectVariableWrites
	Projects       pc.ProjectAuthority
	Events         oc.Appender
	VariableEvents c.SecretVariableEvents
	Audit          ac.Appender
	Activity       ActivityAuthority
	Cursors        cursor.Keyring
}

// SecretService owns only its calls. Stop/Drain never stop shared dependencies.
type SecretService struct{ data func() *secretServiceState }
type secretServiceState struct {
	store   Store
	deps    SecretDependencies
	mu      sync.Mutex
	stopped bool
	calls   map[*call]struct{}
	changed chan struct{}
}

func NewSecret(store Store, deps SecretDependencies) (*SecretService, error) {
	if nilPort(store) || deps.Authority.state() == nil || deps.Writes.state() == nil ||
		!sameStore(store, deps.Authority.state().store) || !sameStore(store, deps.Writes.state().store) ||
		nilPort(deps.Secrets) || nilPort(deps.Projects) || nilPort(deps.Events) || nilPort(deps.Audit) || nilPort(deps.Activity) ||
		!deps.VariableEvents.Valid() || deps.Cursors.Validate() != nil {
		return nil, fault(f.DependencyUnbound)
	}
	// One immutable Project authority must govern both D04 stages and the final
	// Owner transaction. Comparable port identity is required at construction.
	if !reflect.TypeOf(deps.Projects).Comparable() || reflect.TypeOf(deps.Projects) != reflect.TypeOf(deps.Writes.state().projects) || deps.Projects != deps.Writes.state().projects {
		return nil, fault(f.DependencyUnbound)
	}
	st := &secretServiceState{store: store, deps: deps, calls: map[*call]struct{}{}, changed: make(chan struct{})}
	return &SecretService{data: func() *secretServiceState { return st }}, nil
}
func (s *SecretService) state() *secretServiceState {
	if s == nil || s.data == nil {
		return nil
	}
	return s.data()
}
func (s *SecretService) begin(ctx context.Context) (context.Context, *call, func(), error) {
	return s.beginProject(ctx, c.ProjectID{}, controlCall)
}
func (s *SecretService) beginProject(ctx context.Context, project c.ProjectID, kind callKind) (context.Context, *call, func(), error) {
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
	entry := &call{project: project, kind: kind, cancel: cancel, confirmations: map[*confirmation]struct{}{}}
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
func (s *SecretService) Stop() {
	st := s.state()
	if st == nil {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	st.stopped = true
	for entry := range st.calls {
		entry.cancel()
		for confirmation := range entry.confirmations {
			confirmation.cancel()
		}
	}
}
func (s *SecretService) Drain(ctx context.Context) error {
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
