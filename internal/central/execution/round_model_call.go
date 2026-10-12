package execution

import (
	"context"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

func (o *RoundModelCallOwner) Claim(ctx context.Context, e i.ExecutionID, next c.RoundID) (c.RoundModelClaimReceipt, error) {
	work, err := o.enter(ctx, e, next, false)
	if err != nil {
		return nil, err
	}
	defer o.leave(work)
	ctx = work.ctx
	scope, request, err := o.scope(ctx, work, true)
	if err != nil {
		return nil, err
	}
	candidate, err := o.candidate(work, request)
	if err != nil {
		return nil, err
	}
	locks, err := oc.NormalizeLocks(roundInputLocks(scope.summary.ProjectID, scope.summary.AgentID, e))
	if err != nil {
		return nil, portError(err)
	}
	if work.owned != nil {
		// A known original reservation is only re-read. Failure of that read
		// cannot replace its known write outcome with a new pending write.
		err = o.readTx(ctx, work, "replay", locks, func(ctx context.Context, tx f.Tx) error {
			observed, found, err := o.claimInTx(ctx, tx, work, scope, &candidate, locks)
			if err != nil {
				return err
			}
			if !found || observed == nil || observed.fields != candidate.fields {
				return fault(f.ConfirmationStale)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		o.mu.Lock()
		defer o.mu.Unlock()
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		return candidate, nil
	}
	cause, err := f.NewJobCause("execution.round-model-call.claim", e.String(), next.String())
	if err != nil {
		return nil, err
	}
	var observed *roundModelClaimRecord
	calls, complete := 0, false
	result := o.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		calls++
		if calls != 1 {
			return fault(f.InvalidState)
		}
		if err := o.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		var found bool
		var err error
		observed, found, err = o.claimInTx(ctx, tx, work, scope, &candidate, locks)
		if err != nil {
			return err
		}
		if !found || observed == nil || observed.fields != candidate.fields {
			return fault(f.InvalidState)
		}
		if err = o.requireWork(ctx, work); err != nil {
			return err
		}
		complete = true
		return nil
	})
	if result.State() == f.Unknown {
		err = commitError(result)
		o.mu.Lock()
		o.pending[e] = &roundModelCallUnknown{key: work.key, candidate: candidate, result: result, err: err}
		o.mu.Unlock()
		return nil, err
	}
	if err = commitError(result); err != nil {
		return nil, err
	}
	if !complete || calls != 1 || observed == nil || observed.Validate() != nil || observed.fields != candidate.fields {
		return nil, f.NewFault(f.InvalidState, f.Committed)
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	// Remember a known commit even if cancellation prevents publication. This
	// map is private to this original instance, not restored by foreign Lookup.
	o.owned[work.key] = candidate
	if err = ctx.Err(); err != nil {
		known := f.NewFault(f.DependencyUnavailable, f.Committed).WithCause(err)
		known.RetryHint = "lookup"
		return nil, known
	}
	return candidate, nil
}

// Lookup observes the original reservation without taking it over. A missing
// row cannot resolve a prior Unknown; an observed row never grants a new
// instance the old owner's private registration, regardless of ProcessID.
func (o *RoundModelCallOwner) Lookup(ctx context.Context, e i.ExecutionID, next c.RoundID) (c.RoundModelClaimReceipt, bool, error) {
	work, err := o.enter(ctx, e, next, true)
	if err != nil {
		return nil, false, err
	}
	defer o.leave(work)
	ctx = work.ctx
	scope, _, err := o.scope(ctx, work, false)
	if err != nil {
		return nil, false, err
	}
	locks, err := oc.NormalizeLocks(roundInputLocks(scope.summary.ProjectID, scope.summary.AgentID, e))
	if err != nil {
		return nil, false, portError(err)
	}
	var observed *roundModelClaimRecord
	found := false
	err = o.readTx(ctx, work, "lookup", locks, func(ctx context.Context, tx f.Tx) error {
		var err error
		observed, found, err = o.claimInTx(ctx, tx, work, scope, nil, locks)
		return err
	})
	if err != nil || !found {
		return nil, false, err
	}
	if observed == nil || observed.Validate() != nil {
		return nil, false, fault(f.InvalidState)
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if err = ctx.Err(); err != nil {
		return nil, false, err
	}
	if original := o.pending[e]; original != nil {
		if !sameRoundModelClaimObservation(original, *observed) {
			return nil, false, fault(f.ConfirmationStale)
		}
		delete(o.pending, e)
		o.owned[work.key] = *observed
	}
	return *observed, true, nil
}

func (o *RoundModelCallOwner) candidate(work *roundModelCallWork, request *roundModelRequestRecord) (roundModelClaimRecord, error) {
	var zero roundModelClaimRecord
	if work == nil || request == nil || request.request.Validate() != nil {
		return zero, invalid()
	}
	q := request.request.Fields()
	if q.ExecutionID != work.key.execution || q.RoundInputID != work.key.next {
		return zero, fault(f.ConfirmationStale)
	}
	if work.owned != nil {
		if work.owned.fields.ProcessID != o.process || !work.owned.matchesRequest(request.request, request.at) {
			return zero, fault(f.ConfirmationStale)
		}
		return *work.owned, nil
	}
	claim, err := f.NewID[c.RoundModelClaim]()
	if err != nil {
		return zero, err
	}
	at, err := f.NewInstant(time.Now())
	if err != nil {
		return zero, err
	}
	if at.Time().Before(request.at.Time()) {
		at = request.at
	}
	r := roundModelClaimRecord{fields: c.RoundModelClaimFields{SchemaVersion: 1, ClaimID: claim, ProjectID: q.ProjectID, AgentID: q.AgentID, ExecutionID: q.ExecutionID, RoundInputID: q.RoundInputID, RoundInputDigest: q.RoundInputDigest, SnapshotID: q.SnapshotID, SnapshotDigest: q.SnapshotDigest, ProjectionDigest: q.ProjectionDigest, CallID: q.CallID, ProcessID: o.process, InputDigest: request.request.InputDigest(), RequestDigest: request.request.Digest(), CreatedAt: at}}
	return r, r.Validate()
}

// Discovery decodes the real immutable 72 record to fix every candidate field
// before a possible Unknown write. It grants no permission. The write/lookup
// transaction still performs the existing complete historical reconstruction.
func (o *RoundModelCallOwner) scope(ctx context.Context, work *roundModelCallWork, requestRequired bool) (*executionRecord, *roundModelRequestRecord, error) {
	var row *executionRecord
	var request *roundModelRequestRecord
	err := o.readTx(ctx, work, "scope", []f.LockRequest{executionLock(work.key.execution)}, func(ctx context.Context, tx f.Tx) error {
		x, err := o.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		row, err = loadExecution(ctx, x, work.key.execution)
		if err != nil {
			return err
		}
		if row == nil {
			return fault(f.NotFound)
		}
		if requestRequired {
			request, err = loadRoundModelRequest(ctx, x, work.key.execution, work.key.next)
			if err != nil {
				return err
			}
			if request == nil {
				return fault(f.NotFound)
			}
			q := request.request.Fields()
			if q.ProjectID != row.summary.ProjectID || q.AgentID != row.summary.AgentID || row.summary.SnapshotID == nil || q.SnapshotID != *row.summary.SnapshotID {
				return fault(f.ConfirmationStale)
			}
		}
		return nil
	})
	return row, request, err
}

func (o *RoundModelCallOwner) readTx(ctx context.Context, work *roundModelCallWork, kind string, locks []f.LockRequest, consume func(context.Context, f.Tx) error) error {
	if err := o.requireWork(ctx, work); err != nil {
		return err
	}
	cause, err := f.NewRecoveryCause("execution.round-model-call."+kind, work.key.execution.String(), work.key.next.String())
	if err != nil {
		return err
	}
	calls, complete := 0, false
	result := o.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		calls++
		if calls != 1 {
			return fault(f.InvalidState)
		}
		if err := o.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := o.store.RequireHeldLocks(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := o.guard.RequireCurrentProcessInTx(ctx, tx, o.process); err != nil {
			return portError(err)
		}
		if err := consume(ctx, tx); err != nil {
			return err
		}
		if err := o.requireWork(ctx, work); err != nil {
			return err
		}
		complete = true
		return nil
	})
	if err = commitError(result); err != nil {
		return err
	}
	if calls != 1 || !complete {
		return fault(f.InvalidState)
	}
	return ctx.Err()
}
