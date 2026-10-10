package model

import (
	"context"
	"math"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

func (c *runtimeCall) agentRetry() bool { return c.record.binding.AgentTiming != nil }
func (c *runtimeCall) wireContext() context.Context {
	if c.attemptCtx != nil {
		return c.attemptCtx
	}
	return c.ctx
}

func runtimeGrowDuration(value, cap time.Duration, multiplier uint32) time.Duration {
	if value >= cap || value > cap/time.Duration(multiplier) {
		return cap
	}
	return value * time.Duration(multiplier)
}

// Only an actual adapter failure can authorize this decision. A consumer,
// persistence, policy or Unknown error never becomes a retryable wire failure.
func (c *runtimeCall) mayRetry(e *mc.ModelError, dispatch uc.Dispatch) bool {
	if !c.agentRetry() || !c.wireFailure || c.ctx.Err() != nil || e == nil || dispatch == uc.DispatchUnknown || e.Category == "cancelled" {
		return false
	}
	if !e.Retryable && e.Category != "timeout" {
		return false
	}
	for _, category := range c.record.binding.Policy.Categories {
		if category == e.Category {
			return true
		}
	}
	return false
}

// Called under the original call gate. The preceding attempt is already
// finalized in Usage and its real Exchange and borrowed material have retired.
func (c *runtimeCall) nextAgentAttempt(ctx context.Context) error {
	// Next has its own cancellation budget. It may stop the original call,
	// but a successful Next return must not cancel the live stream.
	stop := context.AfterFunc(ctx, c.cancel)
	defer stop()
	current := c.copyRecord()
	if !c.retryPending || current.phase != "retry_wait" || current.value.Final == nil || !c.ioRetired || c.pending != nil || c.retirement != nil {
		return fault(f.InvalidState)
	}
	c.retryPending = false
	timer := time.NewTimer(c.retryBackoff)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return unavailable(ctx.Err())
	case <-timer.C:
	}
	if err := runtimeContextError(ctx); err != nil {
		return err
	}
	consumer := runtimeConsumerRequest(c.request, mc.InvokeConsumer, nil, nil)
	plan, err := c.runtime.authority.discoverConsumer(ctx, consumer)
	if err != nil {
		return err
	}
	policy, err := runtimeRetryPolicy(c.request, plan)
	if err != nil {
		return err
	}
	if !resolutionEqual(policy, current.binding.Policy) {
		return fault(f.ResourceBusy)
	}
	if current.value.AttemptIndex == math.MaxInt64 || current.version == math.MaxInt64 {
		return fault(f.InvalidState)
	}
	id, err := f.NewID[mc.Invocation]()
	if err != nil {
		return unavailable(err)
	}
	_, at, err := runtimePreflight(ctx, c.runtime, c.request, id, consumer, plan)
	if err != nil {
		return err
	}
	next := *current
	next.value = uc.Invocation{ID: id, CallID: current.value.CallID, AttemptIndex: current.value.AttemptIndex + 1, Consumer: current.value.Consumer.Clone(), SnapshotID: current.value.SnapshotID, Identity: current.value.Identity, ProcessID: current.value.ProcessID, Fence: current.value.Fence, Dispatch: uc.Reserved, StartedAt: at, Usage: mc.Usage{Source: mc.UnknownUsage}}
	next.phase, next.sequence, next.version = "accepted", 1, current.version+1
	next.cancelledAt = nil
	timing := *current.binding.AgentTiming
	if current.value.Final.Error != nil && current.value.Final.Error.Category == "timeout" {
		c.requestTimeout = runtimeGrowDuration(c.requestTimeout, timing.MaxRequestTimeout, timing.TimeoutMultiplier)
	}
	c.retryBackoff = runtimeGrowDuration(c.retryBackoff, timing.MaxBackoff, 2)
	if c.attemptCancel != nil {
		c.attemptCancel()
	}
	c.attemptCtx, c.attemptCancel = nil, nil
	c.mu.Lock()
	c.exchange, c.materialOwned, c.ioRetired, c.handoff, c.startReturned, c.err = nil, false, false, false, false, nil
	c.mu.Unlock()
	c.wireFailure, c.result = false, nil
	clear(c.text)
	c.text = nil
	return c.startRecord(consumer, plan, &next)
}

// An Execution owns the captured lease across logical calls. This retires only
// the Model call after original I/O and Usage finalization. It never asks Secret
// to release that lease or broadens RetireConsumer's existing owner semantics.
func (c *runtimeCall) retireAgentCall(ctx context.Context) error {
	current := c.copyRecord()
	if current.value.Final == nil || !c.ioRetired || c.materialOwned {
		return fault(f.InvalidState)
	}
	consumer, plan, err := c.finalizationPlan(ctx, current)
	if err != nil {
		return err
	}
	locks, err := runtimeCallLocks(c.request.Consumer.ProjectID, c.request.CallID, current.value.ID)
	if err != nil {
		return err
	}
	locks, err = resolutionUnion(locks, plan.RequiredLocks())
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
	next.retired, next.version = true, current.version+1
	if next.phase == "retry_wait" {
		next.phase = string(next.value.Final.Status)
		if c.ctx.Err() != nil {
			at, err := dbNow(ctx, c.runtime.store)
			if err != nil {
				return err
			}
			next.phase, next.cancelledAt = string(uc.Cancelled), &at
		}
	}
	result := c.runtime.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := c.runtime.store.AcquireAll(ctx, tx, locks); err != nil {
			return runtimePortError(err)
		}
		if err := c.runtime.authority.validateConsumer(ctx, tx, consumer, plan); err != nil {
			return err
		}
		x, err := c.runtime.store.InTx(tx)
		if err != nil {
			return runtimePortError(err)
		}
		actual, err := loadRuntimeRecord(ctx, x, c.request.CallID)
		if err != nil {
			return err
		}
		if !runtimeSameRecord(actual, current) {
			return fault(f.ResourceBusy)
		}
		return updateRuntimeRecord(ctx, x, &next, current.version)
	})
	if result.State() != f.Committed {
		original := commitError(result)
		if result.State() != f.Unknown {
			return original
		}
		pending := &runtimeRetirement{desired: &next, locks: locks, cause: cause, original: original}
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
