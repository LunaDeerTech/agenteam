package model

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

// confirmMutation never applies or dispatches an attempt. It takes the original
// writer gates before observing the original IDs, then asks the real Usage
// writer for its confirmed receipt. No elapsed-time/no-row death inference.
func (c *runtimeCall) confirmMutation(ctx context.Context, m *runtimeMutation) error {
	unknown := m.originalUnknown
	if unknown == nil {
		unknown = f.NewFault(f.CommitUnknown, f.Unknown)
	}
	if runtimeContextError(ctx) != nil {
		return unknown
	}
	matched, absent := false, false
	result := c.runtime.store.WithinTx(ctx, m.plan.Cause(), func(ctx context.Context, tx f.Tx) error {
		if err := c.runtime.store.AcquireAll(ctx, tx, m.plan.RequiredLocks()); err != nil {
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
		matched = runtimeSameRecord(actual, m.desired)
		absent = m.action == uc.ReserveAction && actual == nil || m.action != uc.ReserveAction && runtimeSameRecord(actual, c.copyRecord())
		if !matched && !absent {
			return fault(f.ResourceBusy)
		}
		return nil
	})
	if result.State() != f.Committed {
		return unknown
	}
	if absent {
		// This only proves this mutation absent after its actual writer ended.
		// It never permits another wire handoff or upgrades the original error.
		c.clearMutation(m)
		return unknown
	}
	if !matched {
		return unknown
	}
	request := m.request.Clone()
	actor, err := c.technicalActor()
	if err != nil {
		return unknown
	}
	request.Actor, request.Access = actor, uc.ConfirmInvocation
	lookup, err := c.runtime.deps.Usage.LookupInvocation(context.WithValue(ctx, runtimeMutationKey{}, m), request)
	if err != nil || lookup.Validate() != nil || !lookup.Observed || lookup.Receipt.Action != m.action || lookup.Receipt.Sequence != m.desired.sequence || !resolutionEqual(lookup.Receipt.Value, m.desired.value) {
		return unknown
	}
	c.acceptMutation(m)
	return nil
}
