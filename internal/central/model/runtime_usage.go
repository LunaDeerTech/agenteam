package model

import (
	"context"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

type runtimeMutationKey struct{}
type runtimeHandoffKey struct{}

// This witness is private to one Runtime mutation and one original Tx. A
// caller-supplied InvocationRequest contains no observation or authorization.
type runtimeMutation struct {
	mu              sync.Mutex
	call            *runtimeCall
	desired         *runtimeRecord
	action          uc.InvocationAction
	consumer        mc.ConsumerRequest
	consumerPlan    mc.ConsumerDependencies
	request         uc.InvocationRequest
	plan            uc.InvocationPlan
	locks           []f.LockRequest
	mapping         f.Digest
	active, staged  bool
	tx              f.Tx
	originalUnknown error
}

func (c *runtimeCall) invocationRequest(action uc.InvocationAction, record *runtimeRecord) (uc.InvocationRequest, error) {
	v := record.value
	actor := c.request.Actor
	if action == uc.FinalizeAction || action == uc.ObserveAction && v.Dispatch != uc.Authorized {
		var err error
		actor, err = c.technicalActor()
		if err != nil {
			return uc.InvocationRequest{}, err
		}
	}
	r := uc.InvocationRequest{Actor: actor, Access: uc.ApplyInvocation, Action: action, Sequence: record.sequence, Identity: uc.InvocationIdentity{Attempt: mc.AttemptIdentity{CallID: v.CallID, InvocationID: v.ID, AttemptIndex: v.AttemptIndex, ProcessID: v.ProcessID, Fence: v.Fence}, Consumer: record.binding.Consumer.Clone(), SnapshotID: record.binding.SnapshotID, Input: record.binding.Input.Clone()}}
	if r.Validate() != nil {
		return uc.InvocationRequest{}, fault(f.InvalidArgument)
	}
	return r, nil
}

func (a *RuntimeAuthority) mutation(ctx context.Context, request uc.InvocationRequest) (*runtimeMutation, error) {
	if err := runtimeContextError(ctx); err != nil {
		return nil, err
	}
	if a.state() == nil {
		return nil, fault(f.DependencyUnbound)
	}
	m, ok := ctx.Value(runtimeMutationKey{}).(*runtimeMutation)
	if !ok || m == nil || m.call == nil || m.call.runtime.authority != a || request.Validate() != nil {
		return nil, fault(f.DependencyUnbound)
	}
	m.mu.Lock()
	active := m.active
	m.mu.Unlock()
	if !active || m.request.Action != request.Action || m.request.Sequence != request.Sequence || !m.request.Identity.Equal(request.Identity) {
		return nil, fault(f.Forbidden)
	}
	s := m.call.runtime
	s.mu.Lock()
	current := s.calls[request.Identity.Attempt.CallID] == m.call
	s.mu.Unlock()
	if !current {
		return nil, fault(f.Forbidden)
	}
	return m, nil
}

func (a *RuntimeAuthority) Discover(ctx context.Context, request uc.InvocationRequest) (uc.InvocationDependencies, error) {
	m, err := a.mutation(ctx, request)
	if err != nil {
		return uc.InvocationDependencies{}, err
	}
	binding, err := uc.InvocationBinding(request)
	if err != nil {
		return uc.InvocationDependencies{}, runtimePortError(err)
	}
	return uc.NewInvocationDependencies(a.state().usageIssuer, uc.InvocationDependencyDetails{Binding: binding, Mapping: m.mapping, Locks: m.locks})
}

func (a *RuntimeAuthority) ValidateInTx(ctx context.Context, tx f.Tx, request uc.InvocationRequest, dependencies uc.InvocationDependencies) (uc.InvocationFact, error) {
	m, err := a.mutation(ctx, request)
	if err != nil {
		return uc.InvocationFact{}, err
	}
	binding, err := uc.InvocationBinding(request)
	if err != nil || !dependencies.Matches(a.state().usageIssuer, binding, m.mapping) {
		return uc.InvocationFact{}, fault(f.Forbidden)
	}
	x, err := a.state().store.InTx(tx)
	if err != nil {
		return uc.InvocationFact{}, runtimePortError(err)
	}
	if err = a.state().store.RequireHeldLocks(ctx, tx, m.locks); err != nil {
		return uc.InvocationFact{}, runtimePortError(err)
	}
	if request.Access == uc.ApplyInvocation {
		m.mu.Lock()
		staged := m.staged && m.tx == tx
		m.mu.Unlock()
		if !staged {
			return uc.InvocationFact{}, fault(f.Forbidden)
		}
	}
	record, err := loadRuntimeRecord(ctx, x, request.Identity.Attempt.CallID)
	if err != nil {
		return uc.InvocationFact{}, err
	}
	if !runtimeSameRecord(record, m.desired) {
		return uc.InvocationFact{}, fault(f.ResourceBusy)
	}
	fact := uc.InvocationFact{Identity: request.Identity.Clone(), Initiator: m.call.request.Actor, Sequence: record.sequence, Value: record.value.Clone()}
	if fact.Validate() != nil {
		return uc.InvocationFact{}, unavailable(nil)
	}
	return fact, nil
}

func runtimeSameRecord(a, b *runtimeRecord) bool {
	return a != nil && b != nil && a.digest == b.digest && a.sequence == b.sequence && a.phase == b.phase && a.retired == b.retired && a.version == b.version && resolutionEqual(a.value, b.value)
}

func (c *runtimeCall) persist(ctx context.Context, action uc.InvocationAction, desired *runtimeRecord, consumer mc.ConsumerRequest, consumerPlan mc.ConsumerDependencies) error {
	if err := desired.validate(); err != nil {
		return err
	}
	request, err := c.invocationRequest(action, desired)
	if err != nil {
		return err
	}
	locks, err := runtimeCallLocks(c.request.Consumer.ProjectID, c.request.CallID, desired.value.ID)
	if err != nil {
		return err
	}
	locks, err = resolutionUnion(locks, consumerPlan.RequiredLocks())
	if err != nil {
		return err
	}
	raw, err := encoded(struct {
		Record          runtimeAttemptDTO
		Binding         f.Digest
		ConsumerMapping f.Digest
		Action          uc.InvocationAction
	}{runtimeAttemptDTO{1, desired.sequence, desired.value}, desired.digest, consumerPlan.Details().Mapping, action})
	if err != nil {
		return err
	}
	m := &runtimeMutation{call: c, desired: desired, action: action, consumer: consumer.Clone(), consumerPlan: consumerPlan, request: request, locks: locks, mapping: hash(raw), active: true}
	c.mu.Lock()
	if c.pending != nil {
		c.mu.Unlock()
		return f.NewFault(f.CommitUnknown, f.Unknown)
	}
	c.pending = m
	c.mu.Unlock()
	applyCtx := context.WithValue(ctx, runtimeMutationKey{}, m)
	plan, err := c.runtime.deps.Usage.DiscoverInvocation(applyCtx, request)
	if err != nil {
		c.clearMutation(m)
		return runtimePortError(err)
	}
	binding, err := uc.InvocationBinding(request)
	if err != nil || plan.Validate() != nil || !plan.Details().Facts.Matches(c.runtime.authority.state().usageIssuer, binding, m.mapping) {
		c.clearMutation(m)
		return fault(f.Forbidden)
	}
	m.plan = plan
	locks, err = resolutionUnion(locks, plan.RequiredLocks())
	if err != nil {
		c.clearMutation(m)
		return err
	}
	result := c.runtime.store.WithinTx(applyCtx, plan.Cause(), func(ctx context.Context, tx f.Tx) error {
		if err := c.runtime.store.AcquireAll(ctx, tx, locks); err != nil {
			return runtimePortError(err)
		}
		if err := c.runtime.authority.validateConsumer(ctx, tx, consumer, consumerPlan); err != nil {
			return err
		}
		x, err := c.runtime.store.InTx(tx)
		if err != nil {
			return runtimePortError(err)
		}
		old, err := loadRuntimeRecord(ctx, x, c.request.CallID)
		if err != nil {
			return err
		}
		if action == uc.ReserveAction {
			if old != nil {
				return runtimeDuplicateError(old, desired.digest)
			}
			if _, err = loadRuntimeSnapshot(ctx, x, c.request); err != nil {
				return err
			}
			if err = insertRuntimeRecord(ctx, x, desired); err != nil {
				return err
			}
		} else {
			if !runtimeSameRecord(old, c.copyRecord()) {
				return fault(f.ResourceBusy)
			}
			if err = updateRuntimeRecord(ctx, x, desired, old.version); err != nil {
				return err
			}
		}
		// Only this path, after actual current Consumer validation and canonical
		// SQL writes, can expose facts to the real Usage callback in this Tx.
		m.mu.Lock()
		m.tx, m.staged = tx, true
		m.mu.Unlock()
		defer func() { m.mu.Lock(); m.staged = false; m.mu.Unlock() }()
		var receipt uc.InvocationReceipt
		switch action {
		case uc.ReserveAction:
			receipt, err = c.runtime.deps.Usage.ReserveInvocationInTx(ctx, tx, request, plan)
		case uc.ObserveAction:
			receipt, err = c.runtime.deps.Usage.ObserveInvocationInTx(ctx, tx, request, plan)
		case uc.FinalizeAction:
			receipt, err = c.runtime.deps.Usage.FinalizeInvocationInTx(ctx, tx, request, plan)
		default:
			return fault(f.InvalidArgument)
		}
		if err != nil {
			return runtimePortError(err)
		}
		if receipt.Validate() != nil || receipt.Action != action || receipt.Sequence != desired.sequence || !resolutionEqual(receipt.Value, desired.value) {
			return fault(f.Forbidden)
		}
		return nil
	})
	switch result.State() {
	case f.Committed:
		c.acceptMutation(m)
		return nil
	case f.NotCommitted:
		c.clearMutation(m)
		return commitError(result)
	default:
		m.originalUnknown = commitError(result)
		return c.confirmMutation(ctx, m)
	}
}

func (c *runtimeCall) clearMutation(m *runtimeMutation) {
	m.mu.Lock()
	m.active = false
	m.mu.Unlock()
	c.mu.Lock()
	if c.pending == m {
		c.pending = nil
	}
	c.mu.Unlock()
}

func (c *runtimeCall) acceptMutation(m *runtimeMutation) {
	c.mu.Lock()
	c.record, c.accepted = m.desired, true
	c.mu.Unlock()
	c.clearMutation(m)
}

var _ uc.InvocationFacts = (*RuntimeAuthority)(nil)
