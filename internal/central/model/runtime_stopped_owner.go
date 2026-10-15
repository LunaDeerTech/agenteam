package model

import (
	"context"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

type stoppedRuntimeStartupKey struct{}
type stoppedRuntimeOwnerKey struct{}
type stoppedRuntimeStartup struct {
	runtime *runtimeState
	process oc.ProcessID
}
type stoppedRuntimeOwner struct {
	mu       sync.Mutex
	runtime  *runtimeState
	process  oc.ProcessID
	request  uc.StoppedInvocationOwner
	before   *runtimeRecord
	locks    []f.LockRequest
	mapping  f.Digest
	plan     uc.StoppedInvocationOwnerPlan
	active   bool
	original error
}

func (a *RuntimeAuthority) RequireStoppedInvocationStartupInTx(ctx context.Context, tx f.Tx) error {
	if err := runtimeContextError(ctx); err != nil {
		return err
	}
	w, ok := ctx.Value(stoppedRuntimeStartupKey{}).(*stoppedRuntimeStartup)
	if a.state() == nil || !ok || w == nil || w.runtime == nil || w.runtime.authority != a {
		return fault(f.DependencyUnbound)
	}
	w.runtime.mu.Lock()
	valid := w.runtime.initializing && !w.runtime.ready && !w.runtime.stopped && w.runtime.startup == w
	w.runtime.mu.Unlock()
	if !valid {
		return fault(f.Forbidden)
	}
	return runtimePortError(a.state().auth.Process.RequireCurrentProcessInTx(ctx, tx, w.process))
}

func (s *runtimeState) recoverStoppedInvocationOwners(ctx context.Context, process oc.ProcessID) error {
	port, ok := s.deps.Usage.(uc.StoppedInvocationOwnerRecovery)
	if !ok || nilPort(port) {
		return fault(f.DependencyUnbound)
	}
	if err := s.confirmStoppedOwners(ctx); err != nil {
		return err
	}
	startup := &stoppedRuntimeStartup{runtime: s, process: process}
	s.mu.Lock()
	if s.startup != nil || !s.initializing || s.ready || s.stopped {
		s.mu.Unlock()
		return fault(f.ResourceBusy)
	}
	s.startup = startup
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if s.startup == startup {
			s.startup = nil
		}
		s.mu.Unlock()
	}()
	proofCtx := context.WithValue(ctx, stoppedRuntimeStartupKey{}, startup)
	for {
		owners, err := port.DiscoverStoppedInvocationOwners(proofCtx)
		if err != nil {
			return runtimePortError(err)
		}
		if len(owners) > 64 {
			return fault(f.Forbidden)
		}
		if len(owners) == 0 {
			return nil
		}
		for _, owner := range owners {
			w, err := s.prepareStoppedOwner(proofCtx, process, owner)
			if err != nil {
				return err
			}
			if err = w.close(ctx); err != nil {
				return err
			}
		}
	}
}

func (s *runtimeState) prepareStoppedOwner(ctx context.Context, process oc.ProcessID, r uc.StoppedInvocationOwner) (*stoppedRuntimeOwner, error) {
	if r.Validate() != nil || r.ProcessID == process {
		return nil, fault(f.Forbidden)
	}
	cause, err := runtimeCallCause(r.ProjectID, r.CallID)
	if err != nil {
		return nil, err
	}
	w := &stoppedRuntimeOwner{runtime: s, process: process, request: r, active: true, locks: []f.LockRequest{commandLock(cause.Details().Primary), projectLock(r.ProjectID.String())}}
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, w.locks); err != nil {
			return runtimePortError(err)
		}
		if err := s.authority.RequireStoppedInvocationStartupInTx(ctx, tx); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return runtimePortError(err)
		}
		w.before, err = loadRuntimeRecord(ctx, x, r.CallID)
		if err != nil {
			return err
		}
		return w.allowedRecord(w.before)
	})
	if err = commitError(result); err != nil {
		return nil, err
	}
	// Absence is not death. Both Model and Usage independently require the
	// real original guard before issuing a closeable owner plan.
	if err = s.authority.state().auth.Process.ConfirmStopped(ctx, r.ProcessID); err != nil {
		return nil, runtimePortError(err)
	}
	b, _ := uc.StoppedInvocationOwnerBinding(r)
	w.mapping, err = stoppedOwnerRecordMapping(b, process, w.before)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.calls[r.CallID] != nil || s.recoveries[r.CallID] != nil || s.stoppedOwners[r.CallID] != nil {
		s.mu.Unlock()
		return nil, fault(f.ResourceBusy)
	}
	if s.stoppedOwners == nil {
		s.stoppedOwners = make(map[mc.CallID]*stoppedRuntimeOwner)
	}
	s.stoppedOwners[r.CallID] = w
	s.mu.Unlock()
	proofCtx := context.WithValue(ctx, stoppedRuntimeOwnerKey{}, w)
	w.plan, err = s.deps.Usage.(uc.StoppedInvocationOwnerRecovery).DiscoverStoppedInvocationOwner(proofCtx, r)
	if err == nil && (w.plan.Validate() != nil || !w.plan.Details().Facts.Matches(s.authority.state().usageIssuer, b, w.mapping)) {
		err = fault(f.Forbidden)
	}
	if err == nil {
		w.locks, err = resolutionUnion(w.locks, w.plan.RequiredLocks())
	}
	if err != nil {
		w.release()
		return nil, runtimePortError(err)
	}
	return w, nil
}

func stoppedOwnerRecordMapping(binding f.Digest, current oc.ProcessID, record *runtimeRecord) (f.Digest, error) {
	// A pre-admission borrower has no Model version or digest. Typed zero
	// values are invalid, and inventing a version would misrepresent absence.
	var version *f.Version
	var digest *f.Digest
	if record != nil {
		version, digest = &record.version, &record.digest
	}
	raw, err := encoded(struct {
		Binding f.Digest
		Present bool
		Version *f.Version
		Digest  *f.Digest
		Current oc.ProcessID
	}{binding, record != nil, version, digest, current})
	if err != nil {
		return "", err
	}
	return hash(raw), nil
}

func (w *stoppedRuntimeOwner) allowedRecord(record *runtimeRecord) error {
	if record != nil && (!record.retired || record.value.Final == nil || record.binding.Consumer.ProjectID != w.request.ProjectID || record.value.ProcessID != w.request.ProcessID) {
		return fault(f.ResourceBusy)
	}
	return nil
}
func (w *stoppedRuntimeOwner) release() {
	w.mu.Lock()
	w.active = false
	w.mu.Unlock()
	w.runtime.mu.Lock()
	if w.runtime.stoppedOwners[w.request.CallID] == w {
		delete(w.runtime.stoppedOwners, w.request.CallID)
	}
	w.runtime.mu.Unlock()
}
func (a *RuntimeAuthority) stoppedOwner(ctx context.Context, r uc.StoppedInvocationOwner) (*stoppedRuntimeOwner, error) {
	if err := runtimeContextError(ctx); err != nil {
		return nil, err
	}
	w, ok := ctx.Value(stoppedRuntimeOwnerKey{}).(*stoppedRuntimeOwner)
	if a.state() == nil || !ok || w == nil || w.runtime == nil || w.runtime.authority != a {
		return nil, fault(f.DependencyUnbound)
	}
	w.mu.Lock()
	active := w.active
	w.mu.Unlock()
	w.runtime.mu.Lock()
	valid := w.runtime.stoppedOwners[r.CallID] == w && w.runtime.calls[r.CallID] == nil && w.runtime.recoveries[r.CallID] == nil
	w.runtime.mu.Unlock()
	if !active || !valid || r != w.request {
		return nil, fault(f.Forbidden)
	}
	return w, nil
}
func (a *RuntimeAuthority) DiscoverStoppedInvocationOwnerFacts(ctx context.Context, r uc.StoppedInvocationOwner) (uc.InvocationDependencies, error) {
	w, err := a.stoppedOwner(ctx, r)
	if err != nil {
		return uc.InvocationDependencies{}, err
	}
	b, err := uc.StoppedInvocationOwnerBinding(r)
	if err != nil {
		return uc.InvocationDependencies{}, runtimePortError(err)
	}
	return uc.NewInvocationDependencies(a.state().usageIssuer, uc.InvocationDependencyDetails{Binding: b, Mapping: w.mapping, Locks: w.locks})
}
func (a *RuntimeAuthority) ValidateStoppedInvocationOwnerFactsInTx(ctx context.Context, tx f.Tx, r uc.StoppedInvocationOwner, p uc.InvocationDependencies) error {
	w, err := a.stoppedOwner(ctx, r)
	if err != nil {
		return err
	}
	b, err := uc.StoppedInvocationOwnerBinding(r)
	if err != nil || !p.Matches(a.state().usageIssuer, b, w.mapping) {
		return fault(f.Forbidden)
	}
	if err = a.state().store.RequireHeldLocks(ctx, tx, w.locks); err != nil {
		return runtimePortError(err)
	}
	if err = a.state().auth.Process.RequireCurrentProcessInTx(ctx, tx, w.process); err != nil {
		return runtimePortError(err)
	}
	x, err := a.state().store.InTx(tx)
	if err != nil {
		return runtimePortError(err)
	}
	actual, err := loadRuntimeRecord(ctx, x, r.CallID)
	if err != nil {
		return err
	}
	if err = w.allowedRecord(actual); err != nil {
		return err
	}
	if actual == nil && w.before == nil {
		return nil
	}
	if actual == nil || w.before == nil || !runtimeSameRecord(actual, w.before) {
		return fault(f.ResourceBusy)
	}
	return nil
}
func (w *stoppedRuntimeOwner) close(ctx context.Context) error {
	port := w.runtime.deps.Usage.(uc.StoppedInvocationOwnerRecovery)
	proofCtx := context.WithValue(ctx, stoppedRuntimeOwnerKey{}, w)
	result := w.runtime.store.WithinTx(proofCtx, w.plan.Cause(), func(ctx context.Context, tx f.Tx) error {
		if err := w.runtime.store.AcquireAll(ctx, tx, w.locks); err != nil {
			return runtimePortError(err)
		}
		return runtimePortError(port.CloseStoppedInvocationOwnerInTx(ctx, tx, w.request, w.plan))
	})
	if result.State() != f.Unknown {
		w.release()
		return commitError(result)
	}
	w.mu.Lock()
	w.original = commitError(result)
	w.mu.Unlock()
	return w.confirm(ctx)
}
func (w *stoppedRuntimeOwner) confirm(ctx context.Context) error {
	w.mu.Lock()
	original := w.original
	w.mu.Unlock()
	if original == nil {
		return fault(f.InvalidState)
	}
	if runtimeContextError(ctx) != nil {
		return original
	}
	var closed bool
	proofCtx := context.WithValue(ctx, stoppedRuntimeOwnerKey{}, w)
	result := w.runtime.store.WithinTx(proofCtx, w.plan.Cause(), func(ctx context.Context, tx f.Tx) error {
		if err := w.runtime.store.AcquireAll(ctx, tx, w.locks); err != nil {
			return runtimePortError(err)
		}
		var err error
		closed, err = w.runtime.deps.Usage.(uc.StoppedInvocationOwnerRecovery).StoppedInvocationOwnerClosedInTx(ctx, tx, w.request, w.plan)
		return runtimePortError(err)
	})
	if result.State() != f.Committed {
		return original
	}
	w.release()
	if closed {
		return nil
	}
	return original // Definite non-application; never replay from observation.
}
func (s *runtimeState) confirmStoppedOwners(ctx context.Context) error {
	s.mu.Lock()
	owners := make([]*stoppedRuntimeOwner, 0, len(s.stoppedOwners))
	for _, w := range s.stoppedOwners {
		owners = append(owners, w)
	}
	s.mu.Unlock()
	for _, w := range owners {
		w.mu.Lock()
		pending := w.original != nil
		w.mu.Unlock()
		if !pending {
			return fault(f.ResourceBusy)
		}
		if err := w.confirm(ctx); err != nil {
			return err
		}
	}
	return nil
}

var _ uc.StoppedInvocationOwnerFacts = (*RuntimeAuthority)(nil)
