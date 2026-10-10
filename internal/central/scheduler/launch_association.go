package scheduler

import (
	"context"
	"math"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

func handoffCommand(p i.ProjectID, id DispatchID, name string) (f.CommandIdentity, error) {
	return f.NewCommandIdentity("scheduler", []string{p.String()}, name, launchKey(id))
}
func handoffLocks(p i.ProjectID, id DispatchID, name string, r *dispatchRecord) ([]f.LockRequest, error) {
	command, err := handoffCommand(p, id, name)
	if err != nil {
		return nil, err
	}
	ck, _ := f.CommandLock(command)
	dk, _ := f.AggregateLock(f.DispatchAggregate, id.String())
	locks := append(pendingLocks(p), f.LockRequest{Key: ck, Mode: f.Exclusive}, f.LockRequest{Key: dk, Mode: f.Exclusive})
	if r != nil {
		locks = append(locks, executionIntentLocks(r)...)
	}
	return oc.NormalizeLocks(locks)
}
func nextDispatch(r *dispatchRecord) (*dispatchRecord, error) {
	if r.version == f.Version(math.MaxInt64) {
		return nil, fault(f.InvalidState)
	}
	v := *r
	v.version++
	now := time.Now().UTC().Truncate(time.Microsecond)
	if !now.After(r.updatedAt.Time()) {
		now = r.updatedAt.Time().Add(time.Microsecond)
	}
	var err error
	v.updatedAt, err = f.NewInstant(now)
	return &v, err
}
func sameDispatch(a, b *dispatchRecord) bool {
	return a != nil && b != nil && a.id == b.id && a.project == b.project && a.agent == b.agent && a.sprint == b.sprint && a.task == b.task && a.digest == b.digest && a.launch.Meta.RequestID == b.launch.Meta.RequestID && a.launch.Meta.IdempotencyKey == b.launch.Meta.IdempotencyKey
}

func (s *LaunchHandoff) markSending(ctx context.Context, call *launchCall) (*dispatchRecord, bool, error) {
	locks, err := handoffLocks(call.project, call.id, "launch_handoff", nil)
	if err != nil {
		return nil, false, err
	}
	command, _ := handoffCommand(call.project, call.id, "launch_handoff")
	cause, _ := f.NewCommandsCause(command)
	var out *dispatchRecord
	send := false
	result := s.authority.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.authority.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := s.authority.inTx(ctx, tx, call.project)
		if err != nil {
			return err
		}
		r, err := loadDispatch(ctx, x, call.project, call.id)
		if err != nil {
			return err
		}
		if r == nil {
			return fault(f.NotFound)
		}
		out = r
		if r.status != Pending || r.outcome != NotSent {
			return ctx.Err()
		}
		// Only the real todo claim is currently bound. Relaunch/review and
		// subsequent known-rejection retries have no Work writer in this slice.
		if r.guard == nil || r.launch.Purpose != "task/work" || r.attempts != 0 {
			return fault(f.CapabilityUnsupported)
		}
		out, err = nextDispatch(r)
		if err != nil {
			return err
		}
		out.outcome, out.attempts, out.nextRetry = Unknown, 1, nil
		if err = updateDispatch(ctx, x, out, r.version); err != nil {
			return err
		}
		send = true
		return nil
	})
	if err = commitError(result); err != nil {
		return nil, false, err
	}
	if out == nil {
		return nil, false, unavailable(nil)
	}
	return out, send, nil
}

func (s *LaunchHandoff) readOriginal(ctx context.Context, call *launchCall) (*dispatchRecord, error) {
	locks, err := handoffLocks(call.project, call.id, "lookup_handoff", nil)
	if err != nil {
		return nil, err
	}
	command, _ := handoffCommand(call.project, call.id, "lookup_handoff")
	cause, _ := f.NewCommandsCause(command)
	var out *dispatchRecord
	result := s.authority.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.authority.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := s.authority.inTx(ctx, tx, call.project)
		if err != nil {
			return err
		}
		out, err = loadDispatch(ctx, x, call.project, call.id)
		if err != nil {
			return err
		}
		if out == nil {
			return fault(f.NotFound)
		}
		return ctx.Err()
	})
	if err = commitError(result); err != nil {
		return nil, err
	}
	if out == nil {
		return nil, unavailable(nil)
	}
	return out, nil
}

// associate is the one quota handoff: the original pending reservation is
// replaced by its exact created Execution under ScheduleEX in the same Tx.
// Observation reads all original key/digest/Dispatch lineage; a Launch return
// or an Execution ID alone cannot authorize this update.
func (s *LaunchHandoff) associate(ctx context.Context, call *launchCall, expected *dispatchRecord, execution i.ExecutionID) (*dispatchRecord, error) {
	locks, err := handoffLocks(call.project, call.id, "associate_launch", expected)
	if err != nil {
		return nil, err
	}
	command, _ := handoffCommand(call.project, call.id, "associate_launch")
	cause, _ := f.NewCommandsCause(command)
	var out *dispatchRecord
	result := s.authority.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.authority.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := s.authority.inTx(ctx, tx, call.project)
		if err != nil {
			return err
		}
		r, err := loadDispatch(ctx, x, call.project, call.id)
		if err != nil {
			return err
		}
		if !sameDispatch(r, expected) || r.attempts != expected.attempts {
			return fault(f.ConfirmationStale)
		}
		observation, err := s.deps.Observations.LookupLaunchInTx(ctx, tx, lookupKey(r), r.digest, r.id.String())
		if err != nil {
			return portError(err)
		}
		if !observation.Found || observation.Execution == nil || observation.RequestDigest != r.digest || observation.Execution.ID != execution || !matchesExecution(r, *observation.Execution) {
			return unavailable(nil)
		}
		if r.status == Launched && r.execution != nil && *r.execution == execution {
			out = r
			return ctx.Err()
		}
		if r.status != Pending || r.outcome != Unknown || r.version != expected.version {
			return fault(f.ConfirmationStale)
		}
		out, err = nextDispatch(r)
		if err != nil {
			return err
		}
		out.status, out.outcome, out.execution, out.nextRetry = Launched, Created, &execution, nil
		return updateDispatch(ctx, x, out, r.version)
	})
	if err = commitError(result); err != nil {
		return nil, err
	}
	if out == nil {
		return nil, unavailable(nil)
	}
	return out, nil
}

func (s *LaunchHandoff) recordRejected(ctx context.Context, call *launchCall, expected *dispatchRecord, busy bool) (*dispatchRecord, error) {
	locks, err := handoffLocks(call.project, call.id, "reject_launch", expected)
	if err != nil {
		return nil, err
	}
	command, _ := handoffCommand(call.project, call.id, "reject_launch")
	cause, _ := f.NewCommandsCause(command)
	var out *dispatchRecord
	result := s.authority.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.authority.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := s.authority.inTx(ctx, tx, call.project)
		if err != nil {
			return err
		}
		r, err := loadDispatch(ctx, x, call.project, call.id)
		if err != nil {
			return err
		}
		if !sameDispatch(r, expected) || r.status != Pending || r.outcome != Unknown || r.version != expected.version || r.attempts != expected.attempts {
			return fault(f.ConfirmationStale)
		}
		out, err = nextDispatch(r)
		if err != nil {
			return err
		}
		out.outcome = KnownNotCreated
		if busy {
			out.busyAttempt = r.attempts
		}
		return updateDispatch(ctx, x, out, r.version)
	})
	if err = commitError(result); err != nil {
		return nil, err
	}
	if out == nil {
		return nil, unavailable(nil)
	}
	return out, nil
}
