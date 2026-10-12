package agent

import (
	"context"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type callKind uint8

const (
	controlCall callKind = iota
	readCall
	mutationCall
)

type ownedCall struct {
	project       i.ProjectID
	kind          callKind
	cancel        context.CancelCauseFunc
	stopRequested bool // protected with confirmations by the original callSet mutex
	returned      bool
	confirmations map[*ownedConfirmation]struct{}
}
type ownedConfirmation struct{ cancel context.CancelCauseFunc }
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
	ctx, _, done, err := s.beginProject(ctx, i.ProjectID{}, controlCall)
	return ctx, done, err
}
func (s *callSet) beginProject(ctx context.Context, project i.ProjectID, kind callKind) (context.Context, *ownedCall, func(), error) {
	if ctx == nil {
		return nil, nil, nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, err
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return nil, nil, nil, fault(f.ShuttingDown)
	}
	own, cancel := context.WithCancelCause(ctx)
	call := &ownedCall{project: project, kind: kind, cancel: cancel, confirmations: make(map[*ownedConfirmation]struct{})}
	s.active[call] = struct{}{}
	s.mu.Unlock()
	var once sync.Once
	done := func() {
		once.Do(func() {
			cancel(nil)
			s.mu.Lock()
			call.returned = true
			s.retireLocked(call)
			s.mu.Unlock()
		})
	}
	return own, call, done, nil
}

// The independent confirmation context remains a child of the exact original
// mutation. Neither delivery cancellation nor a new admission can replace it.
func (s *callSet) beginConfirmation(ctx context.Context, original *ownedCall) (context.Context, func(), error) {
	if ctx == nil || original == nil {
		return nil, nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.active[original]; !exists || original.returned || original.kind != mutationCall {
		return nil, nil, invalid()
	}
	if s.stopped || original.stopRequested {
		return nil, nil, fault(f.ShuttingDown)
	}
	run, cancel := context.WithCancelCause(ctx)
	confirmation := &ownedConfirmation{cancel: cancel}
	original.confirmations[confirmation] = struct{}{}
	var once sync.Once
	done := func() {
		once.Do(func() {
			cancel(nil)
			s.mu.Lock()
			delete(original.confirmations, confirmation)
			s.retireLocked(original)
			s.mu.Unlock()
		})
	}
	return run, done, nil
}

// A call is retired only after both its original function and every admitted
// confirmation have actually returned. The caller always holds s.mu.
func (s *callSet) retireLocked(call *ownedCall) {
	if !call.returned || len(call.confirmations) != 0 {
		return
	}
	if _, exists := s.active[call]; !exists {
		return
	}
	delete(s.active, call)
	if s.stopped && len(s.active) == 0 {
		close(s.drained)
	}
}
func (s *callSet) stop() {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	cancels := make([]context.CancelCauseFunc, 0, len(s.active))
	for c := range s.active {
		cancels = append(cancels, c.cancel)
		for confirmation := range c.confirmations {
			cancels = append(cancels, confirmation.cancel)
		}
	}
	if len(s.active) == 0 {
		close(s.drained)
	}
	s.mu.Unlock()
	for _, cancel := range cancels {
		cancel(fault(f.ShuttingDown))
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
