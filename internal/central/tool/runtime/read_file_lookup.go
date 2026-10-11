package runtime

import (
	"bytes"
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// LookupReadFile performs only current-authorized reads of the original record
// and historical payload. It never resolves a new Mount target, prepares a RID,
// dispatches, or turns an absent row into proof of an earlier Unknown rollback.
func (e *ReadFileExecutor) LookupReadFile(ctx context.Context, execution i.ExecutionID) (ReadFileOutcome, bool, error) {
	if err := contextError(ctx); err != nil {
		return ReadFileOutcome{}, false, err
	}
	if e == nil || e.data == nil || execution.Validate() != nil {
		return ReadFileOutcome{}, false, fail(f.InvalidArgument)
	}
	d := e.data()
	callCtx, returned, err := d.authority.beginLookup(ctx)
	if err != nil {
		return ReadFileOutcome{}, false, err
	}
	defer returned()
	return lookupReadFile(callCtx, d, execution)
}

// An Execute replay is already registered under its original handoff. It uses
// the same read logic without creating an independently admitted public call.
func lookupReadFile(ctx context.Context, d *readFileExecutorData, execution i.ExecutionID) (ReadFileOutcome, bool, error) {
	h, err := discoverReadFile(ctx, d, execution)
	if err != nil {
		return ReadFileOutcome{}, false, err
	}
	var discovered *readFileOperationRecord
	_, err = readFileTransaction(ctx, d.authority, h, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		return consumeReadFileInput(ctx, tx, d.authority, h, func(authorized context.Context, _ []byte) error {
			var err error
			discovered, err = loadReadFileOperation(authorized, x, readFileBindingProjection(h.binding))
			return err
		})
	})
	if err != nil {
		return ReadFileOutcome{}, false, err
	}
	if discovered == nil {
		return ReadFileOutcome{}, false, nil
	}
	if err = bindReadFileOperationLock(h, discovered.ID); err != nil {
		return ReadFileOutcome{}, false, err
	}
	var observation readFileObservation
	_, err = readFileTransaction(ctx, d.authority, h, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		return consumeReadFileInput(ctx, tx, d.authority, h, func(authorized context.Context, _ []byte) error {
			r, err := loadReadFileOperation(authorized, x, readFileBindingProjection(h.binding))
			if err != nil {
				return err
			}
			if r == nil || r.ID != discovered.ID || r.InputDigest != discovered.InputDigest {
				return fail(f.ConfirmationStale)
			}
			observation, err = loadReadFileTerminal(authorized, x, *r, h.arguments.Arguments)
			return err
		})
	})
	if err != nil {
		return ReadFileOutcome{}, false, err
	}
	if err := ctx.Err(); err != nil {
		return ReadFileOutcome{}, false, err
	}
	// Only an exact final candidate, observed after the original call returned,
	// can release a retained final-commit owner. A running row cannot do so.
	if observation.terminal != nil {
		a := d.authority
		a.mu.Lock()
		old := a.active[execution]
		a.mu.Unlock()
		if old != nil {
			select {
			case <-old.done:
				old.mu.Lock()
				if !old.resolving && old.terminal != nil && sameReadFileTerminal(*old.terminal, *observation.terminal) && old.record.ID == observation.record.ID && old.record.InputDigest == observation.record.InputDigest {
					clear(old.payload)
					old.payload = nil
					old.retirement = nil
					a.mu.Lock()
					if a.active[execution] == old {
						delete(a.active, execution)
					}
					a.mu.Unlock()
				}
				old.mu.Unlock()
			default:
			}
		}
	}
	return ReadFileOutcome{data: func() readFileObservation { return observation }}, true, nil
}

func sameReadFileTerminal(a, b readFileTerminal) bool {
	x, err := encode(a)
	if err != nil {
		return false
	}
	y, err := encode(b)
	return err == nil && bytes.Equal(x, y)
}

// ResolveReadFile accounts for a retained original call only. It may retry the
// exact final database publication after the call actually returned; it never
// starts/resends a Runner request. No current permission is needed to record an
// already returned result, but this method exposes no result bytes. Lookup
// still crosses the original current-authorization boundary for those bytes.
func (e *ReadFileExecutor) ResolveReadFile(ctx context.Context, execution i.ExecutionID) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if e == nil || e.data == nil || execution.Validate() != nil {
		return fail(f.InvalidArgument)
	}
	d := e.data()
	a := d.authority
	a.mu.Lock()
	h := a.active[execution]
	a.mu.Unlock()
	if h == nil {
		return fail(f.NotFound)
	}
	select {
	case <-h.done:
	default:
		return fail(f.ResourceBusy)
	}
	h.mu.Lock()
	if h.resolving || h.retirement == nil {
		h.mu.Unlock()
		return fail(f.ResourceBusy)
	}
	h.resolving = true
	h.mu.Unlock()
	defer func() { h.mu.Lock(); h.resolving = false; h.mu.Unlock() }()
	var observed *readFileOperationRecord
	_, err := readFileTransaction(ctx, a, h, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		var err error
		observed, err = loadReadFileOperation(ctx, x, h.record.Input.Binding)
		if err != nil {
			return err
		}
		if observed == nil || observed.ID != h.record.ID || observed.InputDigest != h.record.InputDigest {
			return h.retirement
		}
		return nil
	})
	if err != nil {
		return err
	}
	if h.attempt.Validate() != nil {
		// The original created write is now observed; no Attempt/RID existed.
		return retireReadFileOwner(a, h)
	}
	if observed.Attempt != h.attempt {
		return h.retirement
	}
	if h.terminal == nil {
		// This exact Attempt transaction was Unknown and therefore Dispatch was
		// never invoked. Only its now-observed running row permits recording the
		// known non-send; absence/created cannot retire the original uncertainty.
		if observed.Phase != "running" {
			return h.retirement
		}
		h.record = *observed
		h.terminal = &readFileTerminal{Format: 1, Operation: h.record.ID, Attempt: h.attempt, Request: h.request, Outcome: "failure", Code: "not_dispatched", Attempted: false}
	}
	_, err = finishReadFileAttempt(ctx, a, h, d.schemas, h.terminal, h.payload)
	if err != nil {
		h.mu.Lock()
		h.retirement = err
		h.mu.Unlock()
		return err
	}
	return retireReadFileOwner(a, h)
}

func retireReadFileOwner(a *readFileAuthorityState, h *readFileHandoff) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.active[h.execution] != h {
		return fail(f.ConfirmationStale)
	}
	clear(h.payload)
	h.payload = nil
	h.retirement = nil
	delete(a.active, h.execution)
	return nil
}
