package runtime

import (
	"bytes"
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// LookupWriteFile performs only current-authorized reads of the original record
// and historical publication result. It never resolves a new Mount target, prepares a RID,
// dispatches, or turns an absent row into proof of an earlier Unknown rollback.
func (e *WriteFileExecutor) LookupWriteFile(ctx context.Context, execution i.ExecutionID) (WriteFileOutcome, bool, error) {
	if err := contextError(ctx); err != nil {
		return WriteFileOutcome{}, false, err
	}
	if e == nil || e.data == nil || execution.Validate() != nil {
		return WriteFileOutcome{}, false, fail(f.InvalidArgument)
	}
	d := e.data()
	callCtx, returned, err := d.authority.beginLookup(ctx)
	if err != nil {
		return WriteFileOutcome{}, false, err
	}
	defer returned()
	return lookupWriteFile(callCtx, d, execution)
}

// An Execute replay is already registered under its original handoff. It uses
// the same read logic without creating an independently admitted public call.
func lookupWriteFile(ctx context.Context, d *writeFileExecutorData, execution i.ExecutionID) (WriteFileOutcome, bool, error) {
	h, err := discoverWriteFile(ctx, d, execution)
	if err != nil {
		return WriteFileOutcome{}, false, err
	}
	defer clearWriteFileArguments(h)
	var discovered *writeFileOperationRecord
	_, err = writeFileTransaction(ctx, d.authority, h, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		return consumeWriteFileInput(ctx, tx, d.authority, h, func(authorized context.Context, _ []byte) error {
			var err error
			discovered, err = loadWriteFileOperation(authorized, x, writeFileBindingProjection(h.binding))
			return err
		})
	})
	if err != nil {
		return WriteFileOutcome{}, false, err
	}
	if discovered == nil {
		return WriteFileOutcome{}, false, nil
	}
	if err = bindWriteFileOperationLock(h, discovered.ID); err != nil {
		return WriteFileOutcome{}, false, err
	}
	var observation writeFileObservation
	_, err = writeFileTransaction(ctx, d.authority, h, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		return consumeWriteFileInput(ctx, tx, d.authority, h, func(authorized context.Context, _ []byte) error {
			r, err := loadWriteFileOperation(authorized, x, writeFileBindingProjection(h.binding))
			if err != nil {
				return err
			}
			if r == nil || r.ID != discovered.ID || r.InputDigest != discovered.InputDigest {
				return fail(f.ConfirmationStale)
			}
			observation, err = loadWriteFileTerminal(authorized, x, *r, h.arguments.Arguments)
			return err
		})
	})
	if err != nil {
		return WriteFileOutcome{}, false, err
	}
	if err := ctx.Err(); err != nil {
		return WriteFileOutcome{}, false, err
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
				if old.terminal != nil && sameWriteFileTerminal(*old.terminal, *observation.terminal) && old.record.ID == observation.record.ID && old.record.InputDigest == observation.record.InputDigest {
					clear(old.payload)
					old.payload = nil
					clearWriteFileArguments(old)
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
	return WriteFileOutcome{data: func() writeFileObservation { return observation }}, true, nil
}

func sameWriteFileTerminal(a, b writeFileTerminal) bool {
	x, err := encode(a)
	if err != nil {
		return false
	}
	y, err := encode(b)
	return err == nil && bytes.Equal(x, y)
}

// No write recovery entry point is exposed for this profile. An uncertain
// database publication retains its original owner and can only be observed by
// LookupWriteFile; a missing/running row never authorizes a resend or readback.
