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
		if err := s.retryPublicationRetirements(ctx, f.ID[command]{}); err != nil {
			return err
		}
		st.mu.Lock()
		if len(st.calls) == 0 {
			st.mu.Unlock()
			return nil
		}
		// A failed caller can transfer retirement between the snapshot above
		// and this lock. Consume it before waiting on the replacement channel.
		if len(st.retiringPublications) != 0 {
			st.mu.Unlock()
			continue
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

// Transfer only the failed retirement of a completed synchronous operation.
// The original admitted call stays registered; no goroutine, new work claim or
// timeout is started here. Attempt identity prevents an older call from taking
// ownership of a successor's resources.
func (s *Service) deferPublicationRetirement(retirement *publicationRetirement, done func()) error {
	st := s.state()
	if st == nil || retirement == nil || retirement.service != s || done == nil {
		return internal(nil)
	}
	retirement.mu.Lock()
	defer retirement.mu.Unlock()
	if retirement.joined {
		return internal(nil)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.retiringPublications == nil {
		st.retiringPublications = make(map[f.ID[publicationAttempt]]*publicationRetirement)
	}
	if old := st.retiringPublications[retirement.work.attempt]; old != nil {
		if old == retirement {
			return nil
		}
		return internal(nil)
	}
	retirement.done = done
	st.retiringPublications[retirement.work.attempt] = retirement
	close(st.changed)
	st.changed = make(chan struct{})
	return nil
}

func (s *Service) retryPublicationRetirements(ctx context.Context, commandID f.ID[command]) error {
	if ctx == nil || s.state() == nil {
		return internal(nil)
	}
	st := s.state()
	st.mu.Lock()
	var pending []*publicationRetirement
	for _, retirement := range st.retiringPublications {
		if commandID == (f.ID[command]{}) || retirement.work.command == commandID {
			pending = append(pending, retirement)
		}
	}
	st.mu.Unlock()
	for _, retirement := range pending {
		if err := retirement.join(ctx); err != nil {
			return err
		}
	}
	return nil
}
