package knowledge

import (
	"context"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func (s *Service) begin(ctx context.Context) (context.Context, func(), error) {
	if ctx == nil {
		return nil, nil, fault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, unavailable(err)
	}
	st := s.state()
	if st == nil {
		return nil, nil, fault(f.DependencyUnbound)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.stopped {
		return nil, nil, fault(f.ShuttingDown)
	}
	run, cancel := context.WithCancel(ctx)
	entry := &call{cancel: cancel}
	st.calls[entry] = struct{}{}
	var once sync.Once
	return run, func() {
		once.Do(func() {
			cancel()
			st.mu.Lock()
			delete(st.calls, entry)
			close(st.changed)
			st.changed = make(chan struct{})
			st.mu.Unlock()
		})
	}, nil
}

// Stop closes admission and requests cancellation. Completion is established
// only by Drain after the registered calls actually return.
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
