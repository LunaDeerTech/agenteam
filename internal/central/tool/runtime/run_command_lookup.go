package runtime

import (
	"bytes"
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// LookupRunCommand performs only current-authorized reads of the original record
// and historical invocation report. It never resolves a new Mount target, prepares a RID,
// dispatches, or turns an absent row into proof of an earlier Unknown rollback.
func (e *RunCommandExecutor) LookupRunCommand(ctx context.Context, execution i.ExecutionID) (RunCommandOutcome, bool, error) {
	if err := contextError(ctx); err != nil {
		return RunCommandOutcome{}, false, err
	}
	if e == nil || e.data == nil || execution.Validate() != nil {
		return RunCommandOutcome{}, false, fail(f.InvalidArgument)
	}
	d := e.data()
	callCtx, returned, err := d.authority.beginLookup(ctx)
	if err != nil {
		return RunCommandOutcome{}, false, err
	}
	defer returned()
	return lookupRunCommand(callCtx, d, execution)
}

// An Execute replay is already registered under its original handoff. It uses
// the same read logic without creating an independently admitted public call.
func lookupRunCommand(ctx context.Context, d *runCommandExecutorData, execution i.ExecutionID) (RunCommandOutcome, bool, error) {
	h, err := discoverRunCommand(ctx, d, execution)
	if err != nil {
		return RunCommandOutcome{}, false, err
	}
	defer clearRunCommandArguments(h)
	var discovered *runCommandOperationRecord
	_, err = runCommandTransaction(ctx, d.authority, h, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		return consumeRunCommandInput(ctx, tx, d.authority, h, func(authorized context.Context, _ []byte) error {
			var err error
			discovered, err = loadRunCommandOperation(authorized, x, runCommandBindingProjection(h.binding))
			return err
		})
	})
	if err != nil {
		return RunCommandOutcome{}, false, err
	}
	if discovered == nil {
		return RunCommandOutcome{}, false, nil
	}
	if err = bindRunCommandOperationLock(h, discovered.ID); err != nil {
		return RunCommandOutcome{}, false, err
	}
	var observation runCommandObservation
	_, err = runCommandTransaction(ctx, d.authority, h, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		return consumeRunCommandInput(ctx, tx, d.authority, h, func(authorized context.Context, _ []byte) error {
			r, err := loadRunCommandOperation(authorized, x, runCommandBindingProjection(h.binding))
			if err != nil {
				return err
			}
			if r == nil || r.ID != discovered.ID || r.InputDigest != discovered.InputDigest {
				return fail(f.ConfirmationStale)
			}
			observation, err = loadRunCommandTerminal(authorized, x, *r)
			return err
		})
	})
	if err != nil {
		clear(observation.stdout)
		clear(observation.stderr)
		return RunCommandOutcome{}, false, err
	}
	if err := ctx.Err(); err != nil {
		clear(observation.stdout)
		clear(observation.stderr)
		return RunCommandOutcome{}, false, err
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
				if old.terminal != nil && sameRunCommandTerminal(*old.terminal, *observation.terminal) && old.record.ID == observation.record.ID && old.record.InputDigest == observation.record.InputDigest {
					clear(old.payload)
					old.payload = nil
					clear(old.stdout)
					clear(old.stderr)
					old.stdout, old.stderr = nil, nil
					clearRunCommandArguments(old)
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
	return RunCommandOutcome{data: func() runCommandObservation { return observation }}, true, nil
}

func sameRunCommandTerminal(a, b runCommandTerminal) bool {
	x, err := encode(a)
	if err != nil {
		return false
	}
	y, err := encode(b)
	return err == nil && bytes.Equal(x, y)
}

// No write recovery entry point is exposed for this profile. An uncertain
// database publication retains its original owner and can only be observed by
// LookupRunCommand; a missing/running row never authorizes a resend or readback.
