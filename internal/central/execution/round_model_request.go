package execution

import (
	"bytes"
	"context"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

func (b *RoundModelRequestBinder) Prepare(ctx context.Context, e i.ExecutionID, next c.RoundID) (c.NextRoundModelRequest, error) {
	var zero c.NextRoundModelRequest
	ctx, leave, err := b.enter(ctx, e, next, false)
	if err != nil {
		return zero, err
	}
	defer leave()
	scope, err := b.scope(ctx, e, next)
	if err != nil {
		return zero, err
	}
	call, err := f.NewID[mc.Call]()
	if err != nil {
		return zero, err
	}
	locks, err := oc.NormalizeLocks(roundInputLocks(scope.summary.ProjectID, scope.summary.AgentID, e))
	if err != nil {
		return zero, portError(err)
	}
	cause, err := f.NewJobCause("execution.round-model-request.prepare", e.String(), next.String())
	if err != nil {
		return zero, err
	}
	var candidate c.NextRoundModelRequest
	calls, complete := 0, false
	result := b.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		calls++
		if calls != 1 {
			return fault(f.InvalidState)
		}
		if err := b.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		var err error
		candidate, _, err = b.requestInTx(ctx, tx, scope, next, call, locks, true)
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		complete = true
		return nil
	})
	if result.State() == f.Unknown {
		err = commitError(result)
		if candidate.Validate() == nil {
			call = candidate.Fields().CallID
		}
		b.mu.Lock()
		b.pending[e] = &roundModelRequestUnknown{round: next, call: call, candidate: candidate, err: err}
		b.mu.Unlock()
		return zero, err
	}
	if err = commitError(result); err != nil {
		return zero, err
	}
	if !complete || calls != 1 || candidate.Validate() != nil {
		return zero, fault(f.InvalidState)
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	return candidate, nil
}

// Lookup never writes, calls Prepare, resolves a Model, or grants dispatch.
// Absence is an observation, not proof that an original Unknown rolled back.
func (b *RoundModelRequestBinder) Lookup(ctx context.Context, e i.ExecutionID, next c.RoundID) (c.NextRoundModelRequest, bool, error) {
	var zero c.NextRoundModelRequest
	ctx, leave, err := b.enter(ctx, e, next, true)
	if err != nil {
		return zero, false, err
	}
	defer leave()
	scope, err := b.scope(ctx, e, next)
	if err != nil {
		return zero, false, err
	}
	locks, err := oc.NormalizeLocks(roundInputLocks(scope.summary.ProjectID, scope.summary.AgentID, e))
	if err != nil {
		return zero, false, portError(err)
	}
	var candidate c.NextRoundModelRequest
	found := false
	err = b.readTx(ctx, "lookup", e, next, locks, func(ctx context.Context, tx f.Tx) error {
		var err error
		candidate, found, err = b.requestInTx(ctx, tx, scope, next, mc.CallID{}, locks, false)
		return err
	})
	if err != nil || !found {
		return zero, false, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if original := b.pending[e]; original != nil {
		if !sameRoundModelRequestObservation(original, candidate) {
			return zero, false, fault(f.ConfirmationStale)
		}
		delete(b.pending, e)
	}
	return candidate, true, nil
}

func sameRoundModelRequestObservation(original *roundModelRequestUnknown, observed c.NextRoundModelRequest) bool {
	if original == nil || observed.Validate() != nil {
		return false
	}
	fields := observed.Fields()
	if original.round != fields.RoundInputID || original.call != fields.CallID {
		return false
	}
	if original.candidate.Validate() == nil {
		a, z := original.candidate.CanonicalBytes(), observed.CanonicalBytes()
		defer clear(a)
		defer clear(z)
		return bytes.Equal(a, z)
	}
	return true
}

func (b *RoundModelRequestBinder) scope(ctx context.Context, e i.ExecutionID, next c.RoundID) (*executionRecord, error) {
	var scope *executionRecord
	err := b.readTx(ctx, "scope", e, next, []f.LockRequest{executionLock(e)}, func(ctx context.Context, tx f.Tx) error {
		x, err := b.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		scope, err = loadExecution(ctx, x, e)
		if err != nil {
			return err
		}
		if scope == nil {
			return fault(f.NotFound)
		}
		return nil
	})
	return scope, err
}

func (b *RoundModelRequestBinder) readTx(ctx context.Context, kind string, e i.ExecutionID, next c.RoundID, locks []f.LockRequest, consume func(context.Context, f.Tx) error) error {
	cause, err := f.NewRecoveryCause("execution.round-model-request."+kind, e.String(), next.String())
	if err != nil {
		return err
	}
	calls, complete := 0, false
	result := b.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		calls++
		if calls != 1 {
			return fault(f.InvalidState)
		}
		if err := b.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := b.store.RequireHeldLocks(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := consume(ctx, tx); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		complete = true
		return nil
	})
	if err = commitError(result); err != nil {
		return err
	}
	if !complete || calls != 1 {
		return fault(f.InvalidState)
	}
	return ctx.Err()
}
