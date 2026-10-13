package knowledge

import (
	"context"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func (s *Service) begin(ctx context.Context) (context.Context, func(), error) {
	if s.state() == nil || s.state().ownership == nil {
		return s.beginLegacy(ctx)
	}
	if ctx == nil {
		return nil, nil, fault(f.InvalidArgument)
	}
	if w, ok := ctx.Value(projectCleanupContextKey{}).(projectCleanupWitness); ok && w.call != nil {
		run, entry, err := s.beginProjectCall(ctx, w.call.actor, w.call.scope.ProjectID, nil, projectCallRead)
		if err != nil {
			return nil, nil, err
		}
		return run, func() { _ = s.projectCallReturned(entry) }, nil
	}
	w, ok := ctx.Value(projectCallContextKey{}).(projectCallWitness)
	if !ok || w.state != s.state().ownership || w.owner == nil {
		return nil, nil, fault(f.DependencyUnbound)
	}
	run, entry, err := s.beginProjectCall(ctx, w.owner.actor, w.owner.record.Project, nil, projectCallRead)
	if err != nil {
		return nil, nil, err
	}
	return run, func() { _ = s.projectCallReturned(entry) }, nil
}
func (s *Service) beginLegacy(ctx context.Context) (context.Context, func(), error) {
	run, done, _, err := s.beginLocal(ctx)
	return run, done, err
}
func (s *Service) beginLocal(ctx context.Context) (context.Context, func(), *call, error) {
	if ctx == nil {
		return nil, nil, nil, fault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, unavailable(err)
	}
	st := s.state()
	if st == nil {
		return nil, nil, nil, fault(f.DependencyUnbound)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.stopped {
		return nil, nil, nil, fault(f.ShuttingDown)
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
	}, entry, nil
}

// Stop closes admission and requests cancellation. Completion is established
// only by Drain after the registered calls actually return.
func (s *Service) Stop() {
	if s.state() != nil && s.state().ownership != nil {
		(&ProjectCallOwnership{s.state().ownership}).Stop()
		return
	}
	s.stopLegacy()
}
func (s *Service) stopLegacy() {
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
	if s.state() != nil && s.state().ownership != nil {
		return (&ProjectCallOwnership{s.state().ownership}).Drain(ctx)
	}
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
func (s *Service) deferPublicationRetirement(retirement *publicationRetirement, done func(), checkpointFailure ...error) error {
	st := s.state()
	if st == nil || retirement == nil || retirement.service != s || done == nil || len(checkpointFailure) > 1 {
		return internal(nil)
	}
	retirement.mu.Lock()
	defer retirement.mu.Unlock()
	if retirement.joined && st.ownership == nil {
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
	if st.ownership != nil {
		if st.projectRetirementTails == nil {
			st.projectRetirementTails = make(map[*publicationRetirement]*projectRetirementTail)
		}
		tail := &projectRetirementTail{done: done}
		if len(checkpointFailure) == 1 {
			tail.checkpointError = checkpointFailure[0]
		}
		st.projectRetirementTails[retirement] = tail
		retirement.done = func() {}
	} else {
		retirement.done = done
	}
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
	for retirement := range st.projectRetirementTails {
		if commandID != (f.ID[command]{}) && retirement.work.command != commandID {
			continue
		}
		found := false
		for _, v := range pending {
			if v == retirement {
				found = true
				break
			}
		}
		if !found {
			pending = append(pending, retirement)
		}
	}
	st.mu.Unlock()
	for _, retirement := range pending {
		run := s.originalProjectContext(ctx, retirement.originalCall)
		if err := retirement.join(run); err != nil {
			return err
		}
		if st.ownership != nil {
			if err := s.continueOwnedPublicationJoin(run, retirement); err != nil {
				return err
			}
		}
		s.returnProjectRetirement(retirement)
	}
	return nil
}

func (s *Service) originalProjectContext(ctx, original context.Context) context.Context {
	if ctx == nil || original == nil {
		return ctx
	}
	if w, ok := original.Value(projectCallContextKey{}).(projectCallWitness); ok {
		return context.WithValue(ctx, projectCallContextKey{}, w)
	}
	return ctx
}
func (s *Service) retryProjectPublicationRetirements(ctx context.Context, actor id.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) error {
	st := s.state()
	st.mu.Lock()
	var pending []*publicationRetirement
	seen := map[*publicationRetirement]bool{}
	add := func(v *publicationRetirement) {
		if seen[v] || v.originalCall == nil {
			return
		}
		w, ok := v.originalCall.Value(projectCallContextKey{}).(projectCallWitness)
		if ok && w.state == st.ownership && w.owner != nil && w.owner.record.touches(scope.ProjectID) {
			seen[v] = true
			pending = append(pending, v)
		}
	}
	for _, v := range st.retiringPublications {
		add(v)
	}
	for v := range st.projectRetirementTails {
		add(v)
	}
	st.mu.Unlock()
	for _, v := range pending {
		run := s.originalProjectContext(ctx, v.originalCall)
		if err := v.join(run); err != nil {
			return err
		}
		if err := s.checkpointKnowledgeProjectPublicationJoin(run, actor, cause, scope, v); err != nil {
			st.mu.Lock()
			if tail := st.projectRetirementTails[v]; tail != nil && tail.checkpointError == nil {
				tail.checkpointError = err
			}
			st.mu.Unlock()
			return err
		}
		s.returnProjectRetirement(v)
	}
	return nil
}

type projectRetirementTail struct {
	checkpointError error
	done            func()
	once            sync.Once
}

func (s *Service) returnProjectRetirement(r *publicationRetirement) {
	st := s.state()
	st.mu.Lock()
	tail := st.projectRetirementTails[r]
	st.mu.Unlock()
	if tail == nil {
		return
	}
	tail.once.Do(tail.done)
	st.mu.Lock()
	if st.projectRetirementTails[r] == tail {
		delete(st.projectRetirementTails, r)
		if st.retiringPublications[r.work.attempt] == r {
			delete(st.retiringPublications, r.work.attempt)
		}
		close(st.changed)
		st.changed = make(chan struct{})
	}
	st.mu.Unlock()
}
