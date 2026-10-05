package outbox

import (
	"context"
	"slices"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

func (p lifecyclePlan) inspection() bool {
	return p.request.Details().LifecycleStep == oc.LifecycleInspect
}
func terminalLifecycle(row lifecycleRow) bool {
	return row.phase == "stopped" || row.phase == "completed"
}

// Merge both complete plans before the physical transaction's sole acquisition.
// Dependencies are already validated; the Project mode may strengthen their SH.
func (p lifecyclePlan) unionLocks(mode foundation.LockMode) []foundation.LockRequest {
	locks := p.dependencies.Locks()
	if p.continuation != nil {
		locks = append(locks, p.continuation.dependencies.Locks()...)
	}
	key, _ := foundation.ProjectLock(p.cause.ProjectID.String())
	locks = append(locks, foundation.LockRequest{Key: key, Mode: mode})
	byKey := make(map[string]foundation.LockRequest, len(locks))
	for _, lock := range locks {
		if byKey[lock.Key.Canonical()].Mode != foundation.Exclusive {
			byKey[lock.Key.Canonical()] = lock
		}
	}
	union := make([]foundation.LockRequest, 0, len(byKey))
	for _, lock := range byKey {
		union = append(union, lock)
	}
	slices.SortFunc(union, func(a, b foundation.LockRequest) int { return foundation.CompareLockKeys(a.Key, b.Key) })
	return union
}

// A terminal inspection has no need to discover Stop. Even this observation
// must have a confirmed commit before returning a receipt or planning progress.
func (s *Service) inspectLifecycleReceipt(ctx context.Context, p lifecyclePlan) (lifecycleRow, error) {
	var row lifecycleRow
	commit := s.state().store.WithinTx(ctx, recoveryCause("outbox.stop-observe"), func(ctx context.Context, tx foundation.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, p.locks(foundation.Shared)); err != nil {
			return unavailable(err)
		}
		if err := s.validateLifecycle(ctx, tx, p); err != nil {
			return err
		}
		x, err := executor(s, tx)
		if err != nil {
			return err
		}
		var found bool
		row, found, err = readLifecycle(ctx, x, p.cause.ProjectID)
		if err != nil {
			return err
		}
		if !found || !lifecycleMatches(row, p.cause) {
			return failure(foundation.InvalidState, nil)
		}
		return nil
	})
	if err := commitError(commit); err != nil {
		return lifecycleRow{}, err
	}
	return row, nil
}

// Called after Inspect validation and an exact nonterminal receipt, before
// capture or any write, in the same transaction and complete lock union.
func (s *Service) validateInspectionContinuation(ctx context.Context, tx foundation.Tx, p lifecyclePlan) error {
	if !p.inspection() {
		return nil
	}
	if p.continuation == nil {
		return failure(foundation.DependencyUnbound, nil)
	}
	return s.validateLifecycle(ctx, tx, *p.continuation)
}
