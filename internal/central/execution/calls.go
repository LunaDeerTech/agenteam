package execution

import (
	"context"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

type ownedCall struct{ cancel context.CancelCauseFunc }
type callSet struct {
	mu      sync.Mutex
	stopped bool
	active  map[*ownedCall]struct{}
	drained chan struct{}
}

func newCalls() *callSet {
	return &callSet{active: make(map[*ownedCall]struct{}), drained: make(chan struct{})}
}
func (s *callSet) begin(ctx context.Context) (context.Context, func(), error) {
	if ctx == nil {
		return nil, nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return nil, nil, fault(f.ShuttingDown)
	}
	owned, cancel := context.WithCancelCause(ctx)
	call := &ownedCall{cancel}
	s.active[call] = struct{}{}
	s.mu.Unlock()
	var once sync.Once
	return owned, func() {
		once.Do(func() {
			cancel(nil)
			s.mu.Lock()
			delete(s.active, call)
			if s.stopped && len(s.active) == 0 {
				close(s.drained)
			}
			s.mu.Unlock()
		})
	}, nil
}
func (s *callSet) stop() {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	calls := make([]*ownedCall, 0, len(s.active))
	for call := range s.active {
		calls = append(calls, call)
	}
	if len(calls) == 0 {
		close(s.drained)
	}
	s.mu.Unlock()
	for _, call := range calls {
		call.cancel(fault(f.ShuttingDown))
	}
}
func (s *callSet) drain(ctx context.Context) error {
	if ctx == nil {
		return invalid()
	}
	select {
	case <-s.drained:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *callSet) joined() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped && len(s.active) == 0
}
