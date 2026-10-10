package model

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
	"math"
	"sync"
)

type runtimeSecretKey struct{}
type runtimeSecretWitness struct {
	mu           sync.Mutex
	call         *runtimeCall
	request      sc.UsageRequest
	consumer     mc.ConsumerRequest
	consumerPlan mc.ConsumerDependencies
	locks        []f.LockRequest
	mapping      f.Digest
	active       bool
	validatedTx  f.Tx
}

func runtimeHasSecretWitness(ctx context.Context) bool {
	return ctx != nil && ctx.Value(runtimeSecretKey{}) != nil
}

func (c *runtimeCall) secretWitness(ctx context.Context, action sc.UsageAction) (*runtimeSecretWitness, error) {
	ref := c.request.Model.Snapshot.CredentialRef
	if ref == nil || c.request.Model.CredentialLease == nil {
		return nil, fault(f.CapabilityUnsupported)
	}
	actor, err := c.runtime.authority.state().auth.SecretService.Actor(c.request.Model.LeaseOwner.Details().ID, ref.Details().Scope)
	if err != nil {
		return nil, runtimePortError(err)
	}
	request := sc.UsageRequest{Actor: actor, Ref: *ref, Purpose: sc.Model, LeaseOwner: c.request.Model.LeaseOwner, LeaseID: c.request.Model.CredentialLease.LeaseID, Action: action}
	record := c.copyRecord()
	identity := mc.AttemptIdentity{CallID: record.value.CallID, InvocationID: record.value.ID, AttemptIndex: record.value.AttemptIndex, ProcessID: record.value.ProcessID, Fence: record.value.Fence}
	consumer := runtimeConsumerRequest(c.request, mc.ReadCredentialConsumer, &identity, nil)
	if action == sc.ReadLeaseUsage {
		request.RequestID = identity.InvocationID.String()
	} else if action == sc.ReleaseLeaseUsage {
		consumer = runtimeConsumerRequest(c.request, mc.RetireConsumer, nil, nil)
		consumer.Actor, err = c.technicalActor()
		if err != nil {
			return nil, err
		}
	} else {
		return nil, fault(f.InvalidArgument)
	}
	plan, err := c.runtime.authority.discoverConsumer(ctx, consumer)
	if err != nil {
		return nil, err
	}
	locks, err := runtimeCallLocks(c.request.Consumer.ProjectID, c.request.CallID, record.value.ID)
	if err != nil {
		return nil, err
	}
	locks, err = resolutionUnion(locks, plan.RequiredLocks())
	if err != nil {
		return nil, err
	}
	binding, err := sc.UsageBinding(request)
	if err != nil {
		return nil, runtimePortError(err)
	}
	raw, err := encoded(struct {
		Binding, Call, Consumer f.Digest
		Version                 f.Version
	}{binding, record.digest, plan.Details().Mapping, record.version})
	if err != nil {
		return nil, err
	}
	return &runtimeSecretWitness{call: c, request: request, consumer: consumer, consumerPlan: plan, locks: locks, mapping: hash(raw), active: true}, nil
}

func (a *RuntimeAuthority) secretWitness(ctx context.Context, request sc.UsageRequest) (*runtimeSecretWitness, error) {
	if err := runtimeContextError(ctx); err != nil {
		return nil, err
	}
	if a.state() == nil {
		return nil, fault(f.DependencyUnbound)
	}
	w, ok := ctx.Value(runtimeSecretKey{}).(*runtimeSecretWitness)
	if !ok || w == nil || w.call == nil || w.call.runtime.authority != a {
		return nil, fault(f.DependencyUnbound)
	}
	w.mu.Lock()
	active := w.active
	w.mu.Unlock()
	binding, err := sc.UsageBinding(request)
	expected, e := sc.UsageBinding(w.request)
	if !active || err != nil || e != nil || binding != expected {
		return nil, fault(f.Forbidden)
	}
	s := w.call.runtime
	s.mu.Lock()
	current := s.calls[w.call.request.CallID] == w.call
	s.mu.Unlock()
	if !current {
		return nil, fault(f.Forbidden)
	}
	return w, nil
}

func (a *RuntimeAuthority) DiscoverUsage(ctx context.Context, request sc.UsageRequest) (sc.UsageDependencies, error) {
	w, err := a.secretWitness(ctx, request)
	if err != nil {
		return sc.UsageDependencies{}, err
	}
	binding, err := sc.UsageBinding(request)
	if err != nil {
		return sc.UsageDependencies{}, runtimePortError(err)
	}
	return sc.NewUsageDependencies(a.state().secretIssuer, binding, w.mapping, w.locks)
}

func (a *RuntimeAuthority) ValidateUsageInTx(ctx context.Context, tx f.Tx, request sc.UsageRequest, plan sc.UsageDependencies) error {
	w, err := a.secretWitness(ctx, request)
	if err != nil {
		return err
	}
	binding, err := sc.UsageBinding(request)
	if err != nil || !plan.Matches(a.state().secretIssuer, binding, w.mapping) {
		return fault(f.Forbidden)
	}
	if err = a.state().store.RequireHeldLocks(ctx, tx, w.locks); err != nil {
		return runtimePortError(err)
	}
	x, err := a.state().store.InTx(tx)
	if err != nil {
		return runtimePortError(err)
	}
	if err = a.validateConsumer(ctx, tx, w.consumer, w.consumerPlan); err != nil {
		return err
	}
	record, err := loadRuntimeRecord(ctx, x, w.call.request.CallID)
	if err != nil {
		return err
	}
	current := w.call.copyRecord()
	if !runtimeSameRecord(record, current) || record.retired {
		return fault(f.ResourceBusy)
	}
	process, err := a.state().auth.Process.CurrentProcess()
	if err != nil || process != record.value.ProcessID {
		return unavailable(err)
	}
	w.call.mu.Lock()
	eligible := w.call.accepted
	if request.Action == sc.ReadLeaseUsage {
		eligible = eligible && record.value.Dispatch == uc.Reserved && record.value.Final == nil && !w.call.handoff && !w.call.materialOwned
	} else {
		eligible = eligible && record.value.Final != nil && w.call.ioRetired && !w.call.materialOwned
	}
	w.call.mu.Unlock()
	if !eligible {
		return fault(f.Forbidden)
	}
	if _, err = loadRuntimeSnapshot(ctx, x, w.call.request); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.active {
		return fault(f.Forbidden)
	}
	w.validatedTx = tx
	return nil
}

func (a *RuntimeAuthority) authorizeRuntimeLease(ctx context.Context, tx f.Tx, actor id.Actor, ref sc.CredentialRef, owner sc.CredentialLeaseOwner, action sc.LeaseAction) (sc.UseGrant, error) {
	if ctx == nil {
		return sc.UseGrant{}, fault(f.InvalidArgument)
	}
	w, ok := ctx.Value(runtimeSecretKey{}).(*runtimeSecretWitness)
	if !ok || w == nil {
		return sc.UseGrant{}, fault(f.DependencyUnbound)
	}
	if _, err := a.secretWitness(ctx, w.request); err != nil {
		return sc.UseGrant{}, err
	}
	w.mu.Lock()
	valid := w.active && w.validatedTx == tx
	w.mu.Unlock()
	want := sc.ReadLease
	if w.request.Action == sc.ReleaseLeaseUsage {
		want = sc.ReleaseLease
	}
	if !valid || action != want || !actor.Equal(w.request.Actor) || !ref.Equal(w.request.Ref) || !owner.Equal(w.request.LeaseOwner) {
		return sc.UseGrant{}, fault(f.Forbidden)
	}
	return sc.UseGrant{Subject: actor, Consumer: sc.Model, OperationID: w.call.request.Consumer.OperationID}, nil
}

func (c *runtimeCall) readMaterial(ctx context.Context) error {
	w, err := c.secretWitness(ctx, sc.ReadLeaseUsage)
	if err != nil {
		return err
	}
	defer func() { w.mu.Lock(); w.active = false; w.mu.Unlock() }()
	material, err := c.runtime.deps.SecretReader.ReadCredentialForUsage(context.WithValue(ctx, runtimeSecretKey{}, w), w.request)
	if err != nil {
		return runtimePortError(err)
	}
	c.mu.Lock()
	c.material, c.materialOwned = material, true
	c.mu.Unlock()
	return runtimeContextError(ctx)
}

var _ sc.UsagePlanner = (*RuntimeAuthority)(nil)

type runtimeRetirement struct {
	desired  *runtimeRecord
	locks    []f.LockRequest
	cause    f.TransactionCause
	original error
}

func (c *runtimeCall) retireLease(ctx context.Context) error {
	c.mu.Lock()
	pending := c.retirement
	c.mu.Unlock()
	if pending != nil {
		return c.confirmRetirement(ctx, pending)
	}
	current := c.copyRecord()
	if current.retired {
		c.releaseLocal()
		return nil
	}
	w, err := c.secretWitness(ctx, sc.ReleaseLeaseUsage)
	if err != nil {
		return err
	}
	defer func() { w.mu.Lock(); w.active = false; w.mu.Unlock() }()
	applyCtx := context.WithValue(ctx, runtimeSecretKey{}, w)
	plan, err := c.runtime.deps.SecretUsage.DiscoverUsage(applyCtx, w.request)
	if err != nil {
		return runtimePortError(err)
	}
	binding, _ := sc.UsageBinding(w.request)
	if plan.Validate() != nil || !plan.ProviderPlan().Matches(c.runtime.authority.state().secretIssuer, binding, w.mapping) {
		return fault(f.Forbidden)
	}
	locks, err := resolutionUnion(w.locks, plan.RequiredLocks())
	if err != nil {
		return err
	}
	cause, err := runtimeCallCause(c.request.Consumer.ProjectID, c.request.CallID)
	if err != nil {
		return err
	}
	if current.version == math.MaxInt64 {
		return fault(f.InvalidState)
	}
	next := *current
	next.retired = true
	next.version++
	result := c.runtime.store.WithinTx(applyCtx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := c.runtime.store.AcquireAll(ctx, tx, locks); err != nil {
			return runtimePortError(err)
		}
		x, err := c.runtime.store.InTx(tx)
		if err != nil {
			return runtimePortError(err)
		}
		old, err := loadRuntimeRecord(ctx, x, c.request.CallID)
		if err != nil {
			return err
		}
		if !runtimeSameRecord(old, current) {
			return fault(f.ResourceBusy)
		}
		receipt, err := c.runtime.deps.SecretUsage.ApplyUsageInTx(ctx, tx, w.request, plan)
		if err != nil {
			return runtimePortError(err)
		}
		if receipt.Action() != sc.ReleaseLeaseUsage {
			return fault(f.Forbidden)
		}
		return updateRuntimeRecord(ctx, x, &next, current.version)
	})
	if result.State() != f.Committed {
		original := commitError(result)
		if result.State() != f.Unknown {
			return original
		}
		pending = &runtimeRetirement{desired: &next, locks: locks, cause: cause, original: original}
		c.mu.Lock()
		c.retirement = pending
		c.mu.Unlock()
		return c.confirmRetirement(ctx, pending)
	}
	c.mu.Lock()
	c.record = &next
	c.mu.Unlock()
	c.releaseLocal()
	return nil
}

func (c *runtimeCall) confirmRetirement(ctx context.Context, pending *runtimeRetirement) error {
	if runtimeContextError(ctx) != nil {
		return pending.original
	}
	confirmed, absent := false, false
	check := c.runtime.store.WithinTx(ctx, pending.cause, func(ctx context.Context, tx f.Tx) error {
		if err := c.runtime.store.AcquireAll(ctx, tx, pending.locks); err != nil {
			return runtimePortError(err)
		}
		x, err := c.runtime.store.InTx(tx)
		if err != nil {
			return runtimePortError(err)
		}
		actual, err := loadRuntimeRecord(ctx, x, c.request.CallID)
		if err != nil {
			return err
		}
		confirmed = runtimeSameRecord(actual, pending.desired)
		absent = runtimeSameRecord(actual, c.copyRecord())
		return nil
	})
	if check.State() != f.Committed {
		return pending.original
	}
	if confirmed {
		c.mu.Lock()
		c.record = pending.desired
		c.retirement = nil
		c.mu.Unlock()
		c.releaseLocal()
		return nil
	}
	if absent {
		c.mu.Lock()
		c.retirement = nil
		c.mu.Unlock()
	}
	return pending.original
}
