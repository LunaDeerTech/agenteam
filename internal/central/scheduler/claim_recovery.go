package scheduler

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// ResolveClaim only observes this process's original CommitUnknown attempt.
// It never invokes Work again or inserts another Dispatch. No row observed is
// not a negative commit proof: original ownership and error remain unchanged.
// It is permitted after Stop so an actual observation can finish the old call;
// it cannot issue a new claim, Task mutation or Launch through the old context.
func (s *Coordinator) ResolveClaim(ctx context.Context, r wc.TaskClaimRequest) (Dispatch, error) {
	if ctx == nil || r.Validate() != nil {
		return Dispatch{}, invalid()
	}
	if err := ctx.Err(); err != nil {
		return Dispatch{}, err
	}
	if s == nil {
		return Dispatch{}, fault(f.DependencyUnbound)
	}
	dispatch, _ := f.ParseID[DispatchIdentity](r.DispatchID)
	s.mu.Lock()
	call := s.unknown[dispatch]
	if call == nil || call.request != r {
		s.mu.Unlock()
		return Dispatch{}, fault(f.NotFound)
	}
	if call.resolving {
		s.mu.Unlock()
		return Dispatch{}, fault(f.ResourceBusy)
	}
	call.resolving = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); call.resolving = false; s.mu.Unlock() }()
	locks, err := claimLocks(r, call.launch)
	if err != nil {
		return Dispatch{}, err
	}
	cmd, _ := claimCommand(r.ProjectID, dispatch)
	cause, _ := f.NewCommandsCause(cmd)
	var found *dispatchRecord
	result := s.authority.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.authority.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := s.authority.inTx(ctx, tx, r.ProjectID)
		if err != nil {
			return err
		}
		found, err = loadDispatch(ctx, x, r.ProjectID, dispatch)
		if err != nil || found == nil {
			return err
		}
		// A public replay may observe another process's existing receipt, but
		// resolution of this physical Unknown must prove its original binding.
		if found.retryPolicy != call.retryPolicy {
			return fault(f.IdempotencyKeyReused)
		}
		return sameClaim(found, r, call.launch)
	})
	if result.State() != f.Committed || found == nil {
		return Dispatch{}, call.unknown
	}
	s.mu.Lock()
	delete(s.unknown, dispatch)
	s.mu.Unlock()
	s.release(call)
	return snapshot(found), nil
}
