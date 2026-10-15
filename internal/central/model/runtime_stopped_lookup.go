package model

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

// LookupStoppedCall observes only the exact original call. It never resumes a
// provider request, runs startup recovery, or returns fabricated response text.
func (r *Runtime) LookupStoppedCall(ctx context.Context, request mc.StoppedCallObservationRequest) (mc.StoppedCallObservation, error) {
	var empty mc.StoppedCallObservation
	if err := runtimeContextError(ctx); err != nil {
		return empty, err
	}
	s := r.state()
	if s == nil || nilPort(s.authority.state().auth.StoppedConsumers) {
		return empty, fault(f.DependencyUnbound)
	}
	if request.Validate() != nil {
		return empty, fault(f.InvalidArgument)
	}
	ctx, cancel := context.WithCancel(ctx)
	admission := &runtimeAdmission{cancel: cancel, done: make(chan struct{})}
	s.mu.Lock()
	if s.stopped || !s.ready {
		s.mu.Unlock()
		cancel()
		return empty, fault(f.ShuttingDown)
	}
	s.admissions[admission] = struct{}{}
	s.mu.Unlock()
	defer func() {
		cancel()
		s.mu.Lock()
		delete(s.admissions, admission)
		close(admission.done)
		s.mu.Unlock()
	}()
	request = request.Clone()
	consumer := s.authority.state().auth.StoppedConsumers
	plan, err := consumer.DiscoverStoppedCall(ctx, request)
	if err != nil {
		return empty, runtimePortError(err)
	}
	binding, err := mc.StoppedCallObservationBinding(request)
	if err != nil || plan.Validate() != nil || plan.Details().Binding != binding || plan.Details().RetryPolicy != nil {
		return empty, fault(f.Forbidden)
	}
	cause, err := runtimeCallCause(request.Consumer.ProjectID, request.CallID)
	if err != nil {
		return empty, err
	}
	locks, err := resolutionUnion(plan.RequiredLocks(), []f.LockRequest{commandLock(cause.Details().Primary), projectLock(request.Consumer.ProjectID.String())})
	if err != nil {
		return empty, err
	}
	var old *runtimeRecord
	check := func(ctx context.Context, tx f.Tx) error {
		if err := s.store.RequireHeldLocks(ctx, tx, locks); err != nil {
			return runtimePortError(err)
		}
		if err := consumer.ValidateStoppedCallInTx(ctx, tx, request, plan); err != nil {
			return runtimePortError(err)
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return runtimePortError(err)
		}
		actual, err := loadRuntimeRecord(ctx, x, request.CallID)
		if err != nil {
			return err
		}
		if actual == nil {
			if old != nil {
				return fault(f.ResourceBusy)
			}
			return nil
		}
		b := actual.binding
		if !b.Consumer.Equal(request.Consumer) || b.SnapshotID != request.SnapshotID || !resolutionEqual(b.Input, request.Input) {
			return fault(f.Forbidden)
		}
		if old != nil && !runtimeSameRecord(actual, old) {
			return fault(f.ResourceBusy)
		}
		old = actual
		return nil
	}
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return runtimePortError(err)
		}
		return check(ctx, tx)
	})
	if err = commitError(result); err != nil {
		return empty, err
	}
	if old == nil {
		return empty, nil
	}
	if !old.retired || old.value.Final == nil {
		return empty, fault(f.ResourceBusy)
	}
	w, err := s.prepareStoppedCall(ctx, old)
	if err != nil {
		return empty, err
	}
	defer w.release()
	locks, err = resolutionUnion(locks, w.locks)
	if err != nil {
		return empty, err
	}
	proofCtx := context.WithValue(ctx, stoppedRuntimeKey{}, w)
	result = s.store.WithinTx(proofCtx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return runtimePortError(err)
		}
		if err := check(ctx, tx); err != nil {
			return err
		}
		closed, err := s.deps.Usage.(uc.StoppedInvocationRecovery).StoppedInvocationAppliedInTx(ctx, tx, w.request, w.usage)
		if err != nil {
			return runtimePortError(err)
		}
		if !closed {
			return fault(f.ResourceBusy)
		}
		if old.binding.Owner.Kind == sc.ModelCallOwner {
			observer, ok := s.deps.SecretUsage.(sc.ModelCallLeaseRetirementObservation)
			if !ok {
				return fault(f.DependencyUnbound)
			}
			released, err := observer.ModelCallLeaseReleasedInTx(ctx, tx, w.secret, w.secretPlan)
			if err != nil {
				return runtimePortError(err)
			}
			if !released {
				return fault(f.ResourceBusy)
			}
		}
		return nil
	})
	if err = commitError(result); err != nil {
		return empty, err
	}
	a := w.request.Identity.Attempt
	return mc.StoppedCallObservation{Found: true, Attempt: &a, Status: string(old.value.Final.Status), Retired: true}, nil
}

var _ mc.StoppedCallObserver = (*Runtime)(nil)
