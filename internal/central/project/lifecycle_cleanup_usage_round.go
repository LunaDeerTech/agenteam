package project

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// Usage retains its original registration and checkpoint separately from the
// provider's own Unknown. The authority reservation survives Run's return,
// including a registration that physically rolled back to an absent claim.
type usageCleanupRound struct {
	invalidResult                          bool
	round                                  lifecycleCleanupRound
	before, registeredImage, finishedImage *usageCleanupImage
	registrationUnknown, checkpointUnknown *f.CommitResult
	registered                             bool
	report                                 *c.CleanupReport
	callErr                                error
}

// These are private, bounded Project rows, not Usage resource bodies. Keeping
// their complete DB projection also protects next_attempt_at and diagnostic refs.
type usageCleanupImage struct {
	claim *lifecycleStopClaim
	rows  []byte
}

func sameUsageCleanupImage(a, b *usageCleanupImage) bool {
	return a != nil && b != nil && sameExecutionClaim(a.claim, b.claim) && bytes.Equal(a.rows, b.rows)
}

func usageCleanupUnknown(result f.CommitResult, round lifecycleCleanupRound) bool {
	if !executionRetirementUnknown(result) {
		return false
	}
	actual, expected := result.Cause().Details(), round.claim.cause().Details()
	return actual.Kind == expected.Kind && actual.JobType == expected.JobType && actual.JobID == expected.JobID && actual.JobAttemptID == expected.JobAttemptID
}

func (st *lifecycleCleanupState) readUsageCleanupImage(ctx context.Context, x postgres.SQLExecutor, r lifecycleCleanupRound) (*usageCleanupImage, error) {
	cause, _, name, _, err := st.current(ctx, x, r.claim.project, r.claim.operation)
	if err != nil {
		return nil, err
	}
	if name != c.UsageParticipant || cause != r.cause {
		return nil, fault(f.Forbidden)
	}
	claim, err := loadLifecycleStopClaim(ctx, x, r.claim.operation)
	if err != nil {
		return nil, err
	}
	var raw []byte
	err = x.QueryRow(ctx, `SELECT jsonb_build_object('operation',to_jsonb(o),'participant',to_jsonb(p)) FROM agenteam_project.lifecycle_operations o JOIN agenteam_project.lifecycle_participants p ON p.operation_id=o.id WHERE o.id=$1 AND o.project_id=$2 AND p.participant_name='usage'`, r.claim.operation.String(), r.claim.project.String()).Scan(&raw)
	if err != nil {
		return nil, unavailable(err)
	}
	if len(raw) == 0 || !json.Valid(raw) {
		return nil, unavailable(nil)
	}
	return &usageCleanupImage{claim: claim, rows: bytes.Clone(raw)}, nil
}

func (st *lifecycleCleanupState) usageCleanupExecutor(ctx context.Context, tx f.Tx, p c.ProjectID) (postgres.SQLExecutor, error) {
	if err := st.store.AcquireAll(ctx, tx, []f.LockRequest{projectLock(p, f.Exclusive)}); err != nil {
		return nil, portError(err)
	}
	x, err := st.authority.lifecycleExecutor(ctx, tx, p)
	if err != nil {
		return nil, err
	}
	if nilPort(x) {
		return nil, fault(f.DependencyUnbound)
	}
	return x, nil
}

func (st *lifecycleCleanupState) prepareUsageCleanup(ctx context.Context, p c.ProjectID, op c.OperationID) (*usageCleanupRound, bool, error) {
	previous, err := loadLifecycleStopClaim(ctx, st.store, op)
	if err != nil {
		return nil, false, err
	}
	if previous != nil && previous.project != p {
		return nil, false, fault(f.Forbidden)
	}
	if err = st.canJoin(ctx, previous); err != nil {
		return nil, false, err
	}
	attempt, err := f.NewID[struct{}]()
	if err != nil {
		return nil, false, unavailable(err)
	}
	w := &usageCleanupRound{round: lifecycleCleanupRound{claim: lifecycleStopClaim{project: p, operation: op, process: st.process, attempt: attempt.String(), fence: 1, phase: "running"}, previous: previous, participant: c.UsageParticipant}}
	r := &w.round
	if previous != nil {
		if previous.fence == math.MaxInt64 {
			return nil, false, fault(f.InvalidState)
		}
		r.claim.fence = previous.fence + 1
	}
	valid, replay := false, false
	result := st.store.WithinTx(ctx, r.claim.cause(), func(ctx context.Context, tx f.Tx) error {
		x, err := st.usageCleanupExecutor(ctx, tx, p)
		if err != nil {
			return err
		}
		cause, record, name, completed, err := st.current(ctx, x, p, op)
		if err != nil {
			return err
		}
		if name != c.UsageParticipant {
			return fault(f.Forbidden)
		}
		r.cause = cause
		version, attempts, cp, err := cleanupParticipantRow(ctx, x, *r)
		if err != nil {
			return err
		}
		w.before, err = st.readUsageCleanupImage(ctx, x, *r)
		if err != nil {
			return err
		}
		if !sameExecutionClaim(w.before.claim, previous) {
			return fault(f.ResourceBusy)
		}
		if completed {
			if previous == nil || previous.phase != "terminal" || attempts < 1 || cp == nil {
				return fault(f.ResourceBusy)
			}
			report, err := c.NewCleanupReport(c.UsageParticipant, cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: p}, c.CleanupDetails{State: c.CleanupCompleted, Checkpoint: cp})
			if err != nil {
				return portError(err)
			}
			if err = cleanupRoundReport(*r, report); err != nil {
				return err
			}
			replay, valid = true, true
			return ctx.Err()
		}
		if version == math.MaxInt64 || attempts == math.MaxInt64 {
			return fault(f.InvalidState)
		}
		r.operationVersion, r.rowVersion, r.checkpoint = record.operation.Version, version+1, cp
		valid = true
		return ctx.Err()
	})
	if err = commitError(result); err != nil {
		return nil, false, err
	}
	if !valid {
		return nil, false, unavailable(nil)
	}
	if replay {
		return nil, true, portError(ctx.Err())
	}
	if err = st.authority.retainUsageCleanup(w); err != nil {
		return nil, false, err
	}
	return w, false, portError(ctx.Err())
}

func (st *lifecycleCleanupState) registerUsageCleanup(ctx context.Context, w *usageCleanupRound) (f.CommitResult, bool) {
	r := w.round
	valid := false
	result := st.store.WithinTx(ctx, r.claim.cause(), func(ctx context.Context, tx f.Tx) error {
		x, err := st.usageCleanupExecutor(ctx, tx, r.claim.project)
		if err != nil {
			return err
		}
		current, err := st.readUsageCleanupImage(ctx, x, r)
		if err != nil {
			return err
		}
		if !sameUsageCleanupImage(current, w.before) {
			return fault(f.ResourceBusy)
		}
		if current.claim == nil {
			tag, err := x.Exec(ctx, `INSERT INTO agenteam_project.work_claims(work_kind,work_id,project_id,process_id,attempt_id,fence,phase) VALUES('lifecycle',$1,$2,$3,$4,1,'running')`, r.claim.operation.String(), r.claim.project.String(), r.claim.process.String(), r.claim.attempt)
			if err = affected(tag, err); err != nil {
				return err
			}
		} else {
			prior := current.claim
			tag, err := x.Exec(ctx, `UPDATE agenteam_project.work_claims SET process_id=$2,attempt_id=$3,fence=$4,phase='running' WHERE work_kind='lifecycle' AND work_id=$1 AND project_id=$5 AND process_id=$6 AND attempt_id=$7 AND fence=$8 AND phase=$9`, r.claim.operation.String(), r.claim.process.String(), r.claim.attempt, r.claim.fence, r.claim.project.String(), prior.process.String(), prior.attempt, prior.fence, prior.phase)
			if err = affected(tag, err); err != nil {
				return err
			}
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_project.lifecycle_participants SET attempt_count=attempt_count+1,version=version+1,next_attempt_at=NULL WHERE operation_id=$1 AND participant_name='usage' AND version=$2 AND cleanup_state IN ('required','pending')`, r.claim.operation.String(), r.rowVersion-1)
		if err = affected(tag, err); err != nil {
			return err
		}
		after, err := st.readUsageCleanupImage(ctx, x, r)
		if err != nil {
			return err
		}
		if !sameExecutionClaim(after.claim, &r.claim) {
			return fault(f.ResourceBusy)
		}
		w.registeredImage = after
		valid = true
		return ctx.Err()
	})
	return result, valid
}

func (st *lifecycleCleanupState) finishUsageCleanup(ctx context.Context, w *usageCleanupRound) (f.CommitResult, bool) {
	valid := false
	r := w.round
	result := st.store.WithinTx(ctx, r.claim.cause(), func(ctx context.Context, tx f.Tx) error {
		x, err := st.usageCleanupExecutor(ctx, tx, r.claim.project)
		if err != nil {
			return err
		}
		actual, err := st.readUsageCleanupImage(ctx, x, r)
		if err != nil {
			return err
		}
		if !sameUsageCleanupImage(actual, w.registeredImage) {
			return fault(f.ResourceBusy)
		}
		if err = st.finishInTx(ctx, tx, r, *w.report, w.callErr); err != nil {
			return err
		}
		after, err := st.readUsageCleanupImage(ctx, x, r)
		if err != nil {
			return err
		}
		terminal := r.claim
		terminal.phase = "terminal"
		if !sameExecutionClaim(after.claim, &terminal) {
			return fault(f.ResourceBusy)
		}
		w.finishedImage = after
		valid = true
		return ctx.Err()
	})
	return result, valid
}

// The caller returns immediately after this read-only Store call. Applied and
// NotApplied both require its real return; neither can cause a write this Run.
func (st *lifecycleCleanupState) confirmUsageCleanup(ctx context.Context, w *usageCleanupRound, registration bool) (bool, error) {
	original, before, after := w.checkpointUnknown, w.registeredImage, w.finishedImage
	if registration {
		original, before, after = w.registrationUnknown, w.before, w.registeredImage
	}
	if original == nil || !usageCleanupUnknown(*original, w.round) {
		return false, fault(f.Forbidden)
	}
	valid, applied := false, false
	result := st.store.WithinTx(ctx, original.Cause(), func(ctx context.Context, tx f.Tx) error {
		x, err := st.usageCleanupExecutor(ctx, tx, w.round.claim.project)
		if err != nil {
			return err
		}
		actual, err := st.readUsageCleanupImage(ctx, x, w.round)
		if err != nil {
			return err
		}
		applied = sameUsageCleanupImage(actual, after)
		if !applied && !sameUsageCleanupImage(actual, before) {
			return fault(f.ResourceBusy)
		}
		valid = true
		return ctx.Err()
	})
	return w.acceptUsageConfirmation(ctx, registration, result, valid, applied)
}

// valid and applied are set only by the original read-only callback above.
func (w *usageCleanupRound) acceptUsageConfirmation(ctx context.Context, registration bool, result f.CommitResult, valid, applied bool) (bool, error) {
	original := w.checkpointUnknown
	if registration {
		original = w.registrationUnknown
	}
	if original == nil || !usageCleanupUnknown(*original, w.round) {
		return false, fault(f.Forbidden)
	}
	if err := commitError(result); err != nil {
		return false, lifecycleStopRoundError(commitError(*original), err)
	}
	if !valid {
		return false, lifecycleStopRoundError(commitError(*original), unavailable(nil))
	}
	if err := ctx.Err(); err != nil {
		return false, lifecycleStopRoundError(commitError(*original), portError(err))
	}
	if registration {
		w.registrationUnknown = nil
		w.registered = applied
		if !applied {
			w.registeredImage = nil
		}
	} else {
		w.checkpointUnknown = nil
		if !applied {
			w.finishedImage = nil
		}
	}
	return applied, nil
}

func (st *lifecycleCleanupState) runUsageCleanup(ctx context.Context, p c.ProjectID, op c.OperationID, w *usageCleanupRound) (*usageCleanupRound, bool, error) {
	if w != nil && w.invalidResult {
		return w, false, unavailable(nil)
	}
	if w != nil && w.registrationUnknown != nil {
		_, err := st.confirmUsageCleanup(ctx, w, true)
		return w, false, err
	}
	if w != nil && w.checkpointUnknown != nil {
		applied, err := st.confirmUsageCleanup(ctx, w, false)
		return w, applied, lifecycleStopRoundError(err, w.callErr)
	}
	if w == nil {
		prepared, replay, err := st.prepareUsageCleanup(ctx, p, op)
		if err != nil || replay {
			return prepared, replay, err
		}
		w = prepared
	}
	// Re-acquire only this exact retained pointer, never a replacement owner.
	if err := st.authority.retainUsageCleanup(w); err != nil {
		return w, false, err
	}
	if !w.registered {
		result, valid := st.registerUsageCleanup(ctx, w)
		if result.State() == f.Unknown {
			if !usageCleanupUnknown(result, w.round) {
				w.invalidResult = true
				return w, false, unavailable(nil)
			}
			w.registrationUnknown = &result
			return w, false, commitError(result)
		}
		if err := commitError(result); err != nil {
			st.authority.releaseUsageCleanup(w)
			return nil, false, err
		}
		if !valid {
			w.invalidResult = true
			return w, false, unavailable(nil)
		}
		w.registered = true
	}
	if w.round.usageUnknown {
		if err := st.resumeUsage(ctx, w.round); err != nil {
			return w, false, lifecycleStopRoundError(w.round.usageError, err)
		}
		report, err := st.invoke(ctx, w.round)
		if err != nil {
			return w, false, lifecycleStopRoundError(w.round.usageError, err)
		}
		w.report, w.callErr = &report, nil
		w.round.usageUnknown, w.round.usageError = false, nil
		return w, false, nil
	}
	if w.report == nil {
		report, callErr := st.invoke(ctx, w.round)
		var unknown *f.Fault
		if errors.As(callErr, &unknown) && unknown.CommitState == f.Unknown {
			w.round.usageUnknown, w.round.usageError = true, callErr
			return w, false, callErr
		}
		w.report, w.callErr = &report, callErr
	}
	// The provider has actually returned. Preserve that report while retrying a
	// known checkpoint rollback; never re-run the provider to replace this anchor.
	checkpoint, end := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	result, valid := st.finishUsageCleanup(checkpoint, w)
	end()
	if result.State() == f.Unknown {
		if !usageCleanupUnknown(result, w.round) {
			w.invalidResult = true
			return w, false, lifecycleStopRoundError(unavailable(nil), w.callErr)
		}
		w.checkpointUnknown = &result
		return w, false, lifecycleStopRoundError(commitError(result), w.callErr)
	}
	if err := commitError(result); err != nil {
		return w, false, lifecycleStopRoundError(err, w.callErr)
	}
	if !valid {
		w.invalidResult = true
		return w, false, lifecycleStopRoundError(unavailable(nil), w.callErr)
	}
	return w, true, w.callErr
}
