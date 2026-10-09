package work

import (
	"context"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type TaskDependencies struct {
	Structure  *Reader
	TaskEvents c.TaskEvents
	Authority  *Authority
	Events     oc.Appender
	Activity   ActivityAuthority
}
type TaskService struct{ data func() *taskServiceState }
type taskServiceState struct {
	store   Store
	deps    TaskDependencies
	mu      sync.Mutex
	stopped bool
	calls   map[*call]struct{}
	changed chan struct{}
}

func NewTask(store Store, deps TaskDependencies) (*TaskService, error) {
	if nilPort(store) || deps.Authority.state() == nil || !sameStore(store, deps.Authority.state().store) || nilPort(deps.Events) || nilPort(deps.Activity) || !deps.TaskEvents.Valid() || deps.Structure.state() == nil || !sameStore(store, deps.Structure.state().store) || deps.Structure.state().authority != deps.Authority {
		return nil, fault(f.DependencyUnbound)
	}
	st := &taskServiceState{store: store, deps: deps, calls: map[*call]struct{}{}, changed: make(chan struct{})}
	return &TaskService{data: func() *taskServiceState { return st }}, nil
}
func (s *TaskService) state() *taskServiceState {
	if s == nil || s.data == nil {
		return nil
	}
	return s.data()
}
func (s *TaskService) begin(ctx context.Context) (context.Context, *call, func(), error) {
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
func (s *TaskService) Stop() {
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
func (s *TaskService) Drain(ctx context.Context) error {
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

var _ c.TaskCommands = (*TaskService)(nil)
