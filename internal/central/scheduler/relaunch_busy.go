package scheduler

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// A relaunch did not mutate Work, so a confirmed Busy closes only this intent.
// In particular it does not restore Task state, touch rank, or reset cooldown.
func (s *BusyCompensator) skipBusyRelaunch(ctx context.Context, call *busyCall, original *dispatchRecord) (Dispatch, bool, error) {
	if !validRelaunchOrigin(original) || !pendingBusy(original) {
		return Dispatch{}, false, fault(f.InvalidState)
	}
	s.mu.Lock()
	call.record = original
	s.mu.Unlock()
	locks, err := busyLocks(call.project, call.id, original)
	if err != nil {
		return Dispatch{}, false, err
	}
	cause, _ := f.NewCommandsCause(busyCommand(call.project, call.id))
	var settled *dispatchRecord
	result := s.authority.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.authority.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := s.authority.inTx(ctx, tx, call.project)
		if err != nil {
			return err
		}
		current, err := loadDispatch(ctx, x, call.project, call.id)
		if err != nil {
			return err
		}
		if !sameDispatch(current, original) || current.busyAttempt != original.busyAttempt {
			return fault(f.ConfirmationStale)
		}
		if completedBusy(current) {
			settled = current
			return ctx.Err()
		}
		if current.version != original.version || !pendingBusy(current) {
			return fault(f.ConfirmationStale)
		}
		if err = s.enabled(ctx, tx, call.project); err != nil {
			return err
		}
		settled, err = nextDispatch(current)
		if err != nil {
			return err
		}
		settled.status, settled.skipReason = Skipped, "agent_busy"
		at := settled.updatedAt
		settled.skippedAt = &at
		return updateDispatch(ctx, x, settled, current.version)
	})
	if err = commitError(result); err != nil {
		return Dispatch{}, result.State() == f.Unknown, err
	}
	if settled == nil {
		return Dispatch{}, false, unavailable(nil)
	}
	return snapshot(settled), false, nil
}
