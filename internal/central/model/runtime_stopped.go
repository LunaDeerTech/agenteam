package model

import (
	"context"
	"math"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

type stoppedRuntimeKey struct{}

// Only the current Runtime creates this object, after reading its original
// immutable call and obtaining the actual old ProcessGuard flock proof. It is
// never reconstructed from a public request, ActorDetails or absence of a map.
type stoppedRuntimeCall struct {
	mu                      sync.Mutex
	runtime                 *runtimeState
	process                 oc.ProcessID
	before, after           *runtimeRecord
	request                 uc.StoppedInvocationRequest
	usage                   uc.StoppedInvocationPlan
	secret                  sc.UsageRequest
	secretPlan              sc.UsageDependencies
	locks                   []f.LockRequest
	mapping                 f.Digest
	active, stopped, staged bool
	tx                      f.Tx
	validatedSecret         f.Tx
	original                error
}

func stoppedRuntimeNext(before *runtimeRecord, now f.Instant) (*runtimeRecord, uc.StoppedInvocationRequest, error) {
	if before.validate() != nil || now.Validate() != nil || before.version == math.MaxInt64 {
		return nil, uc.StoppedInvocationRequest{}, fault(f.InvalidState)
	}
	next := *before
	next.value = before.value.Clone()
	if next.value.Final == nil {
		status := uc.Unknown
		steps := f.Sequence(1)
		switch next.value.Dispatch {
		case uc.Reserved:
			next.value.Dispatch, status, steps = uc.NotSent, uc.Cancelled, 2
		case uc.Authorized:
			next.value.Dispatch, steps = uc.DispatchUnknown, 2
		case uc.NotSent:
			status = uc.Cancelled
		}
		if next.sequence > f.Sequence(math.MaxInt64)-steps {
			return nil, uc.StoppedInvocationRequest{}, fault(f.InvalidState)
		}
		next.sequence += steps
		next.value.Final = &uc.Final{Status: status, Version: 1, FinishedAt: now}
		next.phase = string(status)
	} else if next.phase == "retry_wait" {
		// The completed attempt is immutable; stopping cannot create a retry.
		next.phase = string(next.value.Final.Status)
	}
	if !next.retired {
		next.retired, next.version = true, next.version+1
	}
	v := before.value
	r := uc.StoppedInvocationRequest{Identity: uc.InvocationIdentity{Attempt: mc.AttemptIdentity{CallID: v.CallID, InvocationID: v.ID, ProcessID: v.ProcessID, AttemptIndex: v.AttemptIndex, Fence: v.Fence}, Consumer: before.binding.Consumer.Clone(), SnapshotID: before.binding.SnapshotID, Input: before.binding.Input.Clone()}, BeforeSequence: before.sequence, Before: before.value.Clone(), AfterSequence: next.sequence, After: next.value.Clone()}
	if next.validate() != nil || r.Validate() != nil {
		return nil, uc.StoppedInvocationRequest{}, fault(f.InvalidState)
	}
	return &next, r, nil
}

func (s *runtimeState) recoverStoppedCalls(ctx context.Context) error {
	if err := s.confirmStoppedRecoveries(ctx); err != nil {
		return err
	}
	// Bounded pages avoid collecting a global history. Retiring a page removes
	// it from this query. Context bounds the total startup work.
	for {
		if err := runtimeContextError(ctx); err != nil {
			return err
		}
		rows, err := s.store.Query(ctx, `SELECT id::text FROM agenteam_model.calls WHERE NOT retired ORDER BY id LIMIT 64`)
		if err != nil {
			return unavailable(err)
		}
		var ids []mc.CallID
		for rows.Next() {
			var raw string
			if err = rows.Scan(&raw); err != nil {
				break
			}
			id, e := f.ParseID[mc.Call](raw)
			if e != nil {
				err = e
				break
			}
			ids = append(ids, id)
		}
		rows.Close()
		if err == nil {
			err = rows.Err()
		}
		if err != nil {
			return unavailable(err)
		}
		if len(ids) == 0 {
			return nil
		}
		for _, id := range ids {
			old, err := loadRuntimeRecord(ctx, s.store, id)
			if err != nil {
				return err
			}
			if old == nil {
				return fault(f.ResourceBusy)
			}
			if old.retired {
				continue
			}
			w, err := s.prepareStoppedCall(ctx, old)
			if err != nil {
				return err
			}
			if err = w.apply(ctx); err != nil {
				return err
			}
		}
	}
}

func (s *runtimeState) prepareStoppedCall(ctx context.Context, old *runtimeRecord) (*stoppedRuntimeCall, error) {
	port, ok := s.deps.Usage.(uc.StoppedInvocationRecovery)
	if !ok || nilPort(port) {
		return nil, fault(f.DependencyUnbound)
	}
	current, err := s.authority.state().auth.Process.CurrentProcess()
	if err != nil {
		return nil, runtimePortError(err)
	}
	if err = s.authority.state().auth.Process.ConfirmStopped(ctx, old.value.ProcessID); err != nil {
		return nil, runtimePortError(err)
	}
	at, err := dbNow(ctx, s.store)
	if err != nil {
		return nil, err
	}
	after, request, err := stoppedRuntimeNext(old, at)
	if err != nil {
		return nil, err
	}
	locks, err := runtimeCallLocks(old.binding.Consumer.ProjectID, old.binding.CallID, old.value.ID)
	if err != nil {
		return nil, err
	}
	identity, err := resolutionUnitIdentity(old.binding.Consumer, old.binding.Consumer.Purpose, old.binding.Owner)
	if err != nil {
		return nil, err
	}
	locks, err = resolutionUnion(locks, []f.LockRequest{commandLock(identity), recordLock(f.ReferenceRecordLock, "model-snapshot:"+old.binding.SnapshotID.String())})
	if err != nil {
		return nil, err
	}
	bytes, err := encoded(struct {
		Binding                     f.Digest
		BeforeVersion, AfterVersion f.Version
		BeforePhase, AfterPhase     string
		BeforeRetired, AfterRetired bool
		Current                     oc.ProcessID
		Request                     uc.StoppedInvocationRequest
	}{old.digest, old.version, after.version, old.phase, after.phase, old.retired, after.retired, current, request})
	if err != nil {
		return nil, err
	}
	// Request has a redacted public encoding; include its explicit private binding.
	binding, err := uc.StoppedInvocationBinding(request)
	if err != nil {
		return nil, runtimePortError(err)
	}
	w := &stoppedRuntimeCall{runtime: s, process: current, before: old, after: after, request: request, locks: locks, mapping: hash(append(bytes, []byte(binding)...)), active: true, stopped: true}
	s.mu.Lock()
	if s.calls[old.binding.CallID] != nil || s.recoveries[old.binding.CallID] != nil {
		s.mu.Unlock()
		return nil, fault(f.ResourceBusy)
	}
	if s.recoveries == nil {
		s.recoveries = make(map[mc.CallID]*stoppedRuntimeCall)
	}
	s.recoveries[old.binding.CallID] = w
	s.mu.Unlock()
	ready := false
	defer func() {
		if !ready {
			w.release()
		}
	}()
	proofCtx := context.WithValue(ctx, stoppedRuntimeKey{}, w)
	w.usage, err = port.DiscoverStoppedInvocation(proofCtx, request)
	if err != nil {
		return nil, runtimePortError(err)
	}
	if w.usage.Validate() != nil || !w.usage.Details().Facts.Matches(s.authority.state().usageIssuer, binding, w.mapping) {
		return nil, fault(f.Forbidden)
	}
	w.locks, err = resolutionUnion(w.locks, w.usage.RequiredLocks())
	if err != nil {
		return nil, err
	}
	if old.binding.Owner.Kind == sc.ModelCallOwner {
		if err = w.prepareSecret(proofCtx); err != nil {
			return nil, err
		}
	}
	ready = true
	return w, nil
}

func (w *stoppedRuntimeCall) release() {
	w.mu.Lock()
	w.active = false
	w.mu.Unlock()
	w.runtime.mu.Lock()
	if w.runtime.recoveries[w.before.binding.CallID] == w {
		delete(w.runtime.recoveries, w.before.binding.CallID)
	}
	w.runtime.mu.Unlock()
}

func (a *RuntimeAuthority) stoppedCall(ctx context.Context) (*stoppedRuntimeCall, error) {
	if err := runtimeContextError(ctx); err != nil {
		return nil, err
	}
	if a.state() == nil {
		return nil, fault(f.DependencyUnbound)
	}
	w, ok := ctx.Value(stoppedRuntimeKey{}).(*stoppedRuntimeCall)
	if !ok || w == nil || w.runtime == nil || w.runtime.authority != a {
		return nil, fault(f.DependencyUnbound)
	}
	w.mu.Lock()
	active := w.active && w.stopped
	w.mu.Unlock()
	a.state().mu.Lock()
	current := a.state().runtime == w.runtime
	a.state().mu.Unlock()
	w.runtime.mu.Lock()
	registered := w.runtime.recoveries[w.before.binding.CallID] == w && w.runtime.calls[w.before.binding.CallID] == nil
	w.runtime.mu.Unlock()
	if !active || !current || !registered {
		return nil, fault(f.Forbidden)
	}
	return w, nil
}

func (w *stoppedRuntimeCall) validateInTx(ctx context.Context, tx f.Tx) (*runtimeRecord, error) {
	if _, err := w.runtime.authority.stoppedCall(ctx); err != nil {
		return nil, err
	}
	if err := w.runtime.store.RequireHeldLocks(ctx, tx, w.locks); err != nil {
		return nil, runtimePortError(err)
	}
	if err := w.runtime.authority.state().auth.Process.RequireCurrentProcessInTx(ctx, tx, w.process); err != nil {
		return nil, runtimePortError(err)
	}
	x, err := w.runtime.store.InTx(tx)
	if err != nil {
		return nil, runtimePortError(err)
	}
	r, err := loadRuntimeRecord(ctx, x, w.before.binding.CallID)
	if err != nil {
		return nil, err
	}
	if !runtimeSameRecord(r, w.before) && !runtimeSameRecord(r, w.after) {
		return nil, fault(f.ResourceBusy)
	}
	return r, nil
}

func (a *RuntimeAuthority) DiscoverStoppedInvocationFacts(ctx context.Context, r uc.StoppedInvocationRequest) (uc.InvocationDependencies, error) {
	w, err := a.stoppedCall(ctx)
	if err != nil {
		return uc.InvocationDependencies{}, err
	}
	b, err := uc.StoppedInvocationBinding(r)
	want, e := uc.StoppedInvocationBinding(w.request)
	if err != nil || e != nil || b != want {
		return uc.InvocationDependencies{}, fault(f.Forbidden)
	}
	return uc.NewInvocationDependencies(a.state().usageIssuer, uc.InvocationDependencyDetails{Binding: b, Mapping: w.mapping, Locks: w.locks})
}

func (a *RuntimeAuthority) ValidateStoppedInvocationFactsInTx(ctx context.Context, tx f.Tx, r uc.StoppedInvocationRequest, p uc.InvocationDependencies) error {
	w, err := a.stoppedCall(ctx)
	if err != nil {
		return err
	}
	b, err := uc.StoppedInvocationBinding(r)
	want, e := uc.StoppedInvocationBinding(w.request)
	if err != nil || e != nil || b != want || !p.Matches(a.state().usageIssuer, b, w.mapping) {
		return fault(f.Forbidden)
	}
	_, err = w.validateInTx(ctx, tx)
	return err
}

func (w *stoppedRuntimeCall) apply(ctx context.Context) error {
	port := w.runtime.deps.Usage.(uc.StoppedInvocationRecovery)
	proofCtx := context.WithValue(ctx, stoppedRuntimeKey{}, w)
	result := w.runtime.store.WithinTx(proofCtx, w.usage.Cause(), func(ctx context.Context, tx f.Tx) error {
		if err := w.runtime.store.AcquireAll(ctx, tx, w.locks); err != nil {
			return runtimePortError(err)
		}
		old, err := w.validateInTx(ctx, tx)
		if err != nil {
			return err
		}
		if !runtimeSameRecord(old, w.before) {
			return fault(f.ResourceBusy)
		}
		x, err := w.runtime.store.InTx(tx)
		if err != nil {
			return runtimePortError(err)
		}
		if !runtimeSameRecord(w.before, w.after) {
			if err = updateRuntimeRecord(ctx, x, w.after, w.before.version); err != nil {
				return err
			}
		}
		w.mu.Lock()
		w.staged, w.tx = true, tx
		w.mu.Unlock()
		defer func() { w.mu.Lock(); w.staged = false; w.tx = f.Tx{}; w.mu.Unlock() }()
		if w.before.binding.Owner.Kind == sc.ModelCallOwner {
			r, err := w.runtime.deps.SecretUsage.ApplyUsageInTx(ctx, tx, w.secret, w.secretPlan)
			if err != nil {
				return runtimePortError(err)
			}
			if r.Action() != sc.ReleaseLeaseUsage {
				return fault(f.Forbidden)
			}
		}
		return runtimePortError(port.RecoverStoppedInvocationInTx(ctx, tx, w.request, w.usage))
	})
	if result.State() == f.Committed {
		w.release()
		return nil
	}
	if result.State() == f.NotCommitted {
		w.release()
		return commitError(result)
	}
	w.mu.Lock()
	w.original = commitError(result)
	w.mu.Unlock()
	return w.confirm(ctx)
}

func (s *runtimeState) confirmStoppedRecoveries(ctx context.Context) error {
	s.mu.Lock()
	var pending []*stoppedRuntimeCall
	for _, w := range s.recoveries {
		pending = append(pending, w)
	}
	s.mu.Unlock()
	for _, w := range pending {
		w.mu.Lock()
		original := w.original
		w.mu.Unlock()
		if original == nil {
			return fault(f.ResourceBusy)
		}
		if err := w.confirm(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (w *stoppedRuntimeCall) confirm(ctx context.Context) error {
	w.mu.Lock()
	original := w.original
	w.mu.Unlock()
	if original == nil {
		return fault(f.InvalidState)
	}
	if runtimeContextError(ctx) != nil {
		return original
	}
	proofCtx := context.WithValue(ctx, stoppedRuntimeKey{}, w)
	after, before := false, false
	result := w.runtime.store.WithinTx(proofCtx, w.usage.Cause(), func(ctx context.Context, tx f.Tx) error {
		if err := w.runtime.store.AcquireAll(ctx, tx, w.locks); err != nil {
			return runtimePortError(err)
		}
		actual, err := w.validateInTx(ctx, tx)
		if err != nil {
			return err
		}
		applied, err := w.runtime.deps.Usage.(uc.StoppedInvocationRecovery).StoppedInvocationAppliedInTx(ctx, tx, w.request, w.usage)
		if err != nil {
			return runtimePortError(err)
		}
		released := true
		if w.before.binding.Owner.Kind == sc.ModelCallOwner {
			observer, ok := w.runtime.deps.SecretUsage.(sc.ModelCallLeaseRetirementObservation)
			if !ok {
				return fault(f.DependencyUnbound)
			}
			released, err = observer.ModelCallLeaseReleasedInTx(ctx, tx, w.secret, w.secretPlan)
			if err != nil {
				return runtimePortError(err)
			}
		}
		after = runtimeSameRecord(actual, w.after) && applied && released
		before = runtimeSameRecord(actual, w.before) && !applied
		return nil
	})
	if result.State() != f.Committed {
		return original
	}
	if after {
		w.release()
		return nil
	}
	if before {
		w.release()
	}
	// Proved absence ends this mutation; a later explicit Initialize may make
	// a fresh cleanup attempt. This confirmation never applies or dispatches.
	return original
}

var _ uc.StoppedInvocationFacts = (*RuntimeAuthority)(nil)
