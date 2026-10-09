package work

import (
	"context"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type BlockerDependencies struct {
	Structure     *Reader
	BlockerEvents c.TaskBlockerEvents
	Authority     *Authority
	Events        oc.Appender
	Activity      ActivityAuthority
}
type BlockerService struct{ data func() *blockerServiceState }
type blockerServiceState struct {
	store   Store
	deps    BlockerDependencies
	mu      sync.Mutex
	stopped bool
	calls   map[*call]struct{}
	changed chan struct{}
}

func NewBlocker(store Store, deps BlockerDependencies) (*BlockerService, error) {
	if nilPort(store) || deps.Authority.state() == nil || !sameStore(store, deps.Authority.state().store) || nilPort(deps.Events) || nilPort(deps.Activity) || !deps.BlockerEvents.Valid() || deps.Structure.state() == nil || !sameStore(store, deps.Structure.state().store) || deps.Structure.state().authority != deps.Authority {
		return nil, fault(f.DependencyUnbound)
	}
	st := &blockerServiceState{store: store, deps: deps, calls: map[*call]struct{}{}, changed: make(chan struct{})}
	return &BlockerService{data: func() *blockerServiceState { return st }}, nil
}
func (s *BlockerService) state() *blockerServiceState {
	if s == nil || s.data == nil {
		return nil
	}
	return s.data()
}
func (s *BlockerService) begin(ctx context.Context) (context.Context, *call, func(), error) {
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
func (s *BlockerService) Stop() {
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
func (s *BlockerService) Drain(ctx context.Context) error {
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

var _ c.TaskBlockerCommands = (*BlockerService)(nil)
