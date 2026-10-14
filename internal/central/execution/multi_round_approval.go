package execution

import (
	"context"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	gc "github.com/LunaDeerTech/agenteam/internal/central/governance/approval/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// This runs before the original dispatcher is marked attempted. A persisted
// waiting reference always resolves the same request and reserved operation.
func (s *directTextState) beforeApprovalDispatch(ctx context.Context, run *directTextCall, b tc.ToolCallBinding) (c.DirectTextReceipt, bool, error) {
	fail := func(err error) (c.DirectTextReceipt, bool, error) {
		r, e := s.loopFailure(run, "approval-wait", err)
		return r, true, e
	}
	if run.ctx.Err() != nil {
		r, e := s.finishUndispatchedTool(run, context.Canceled)
		return r, true, e
	}
	plan, err := s.multi.authority.DiscoverToolCall(ctx, b)
	if err != nil {
		return fail(err)
	}
	var reference string
	var allow, cancelled bool
	cause, _ := f.NewRecoveryCause("execution.approval.inspect", b.ExecutionID.String(), b.RoundID)
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
			return portError(err)
		}
		if err := requireProjectCallInTx(ctx, tx, s.store); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		row, err := loadExecution(ctx, x, b.ExecutionID)
		if err != nil {
			return err
		}
		if row == nil {
			return fault(f.NotFound)
		}
		if row.summary.CancelRequestedAt != nil {
			cancelled = true
			return nil
		}
		if row.summary.Status == c.Waiting {
			ref, err := loadWaitingReference(ctx, x, b.ExecutionID)
			if err != nil {
				return err
			}
			if ref == nil {
				return fault(f.InvalidState)
			}
			reference = *ref
			return s.multi.authority.withToolCallStateInTx(ctx, tx, b, plan, reference, nil)
		}
		return s.multi.authority.withToolCallInTx(ctx, tx, b, plan, func(ctx context.Context, _ tc.ToolCallBinding, _ []byte) error {
			w, err := s.authority.commandWitness(ctx, tx)
			if err != nil {
				return err
			}
			allow = w.approval == ac.ApprovalAllow
			return nil
		})
	})
	if err = commitError(result); err != nil {
		return fail(err)
	}
	if cancelled {
		r, e := s.finishUndispatchedTool(run, context.Canceled)
		return r, true, e
	}
	if allow {
		return c.DirectTextReceipt{}, false, nil
	}
	if nilPort(s.multi.approvals) {
		return fail(fault(f.CapabilityUnsupported))
	}
	var waiting gc.Waiting
	if reference == "" {
		waiting, err = s.multi.approvals.Ensure(ctx, b)
		if err != nil {
			return fail(portError(err))
		}
		if err = validateApprovalWaiting(waiting); err != nil {
			return fail(err)
		}
		if waiting.Status == gc.Approved {
			return c.DirectTextReceipt{}, false, nil
		}
		reference = waiting.RequestID
		if err = s.changeApprovalWaiting(ctx, run, b, plan, reference, true); err != nil {
			return fail(err)
		}
	}
	waiting, err = s.awaitApprovalDecision(ctx, run, b, reference)
	if run.ctx.Err() != nil {
		r, e := s.finishUndispatchedTool(run, run.ctx.Err())
		return r, true, e
	}
	if err != nil {
		return fail(portError(err))
	}
	switch waiting.Status {
	case gc.Approved:
		if err = s.changeApprovalWaiting(ctx, run, b, plan, reference, false); err != nil {
			return fail(err)
		}
		return c.DirectTextReceipt{}, false, nil
	case gc.Rejected:
		r, e := s.consumeApprovalRejection(ctx, run, b, reference)
		return r, true, e
	default:
		return fail(fault(f.InvalidState))
	}
}

// Only a persisted waiting reference enters this loop. The original callback
// remains counted by Associated/ProjectCall ownership, with no transaction or
// lock held while waiting. A failed lookup remains an actual error; only the
// canonical pending decision is polled. Recovery caller cancellation does not
// cancel the retained run, while its own cancellation still ends the wait.
func (s *directTextState) awaitApprovalDecision(ctx context.Context, run *directTextCall, b tc.ToolCallBinding, reference string) (gc.Waiting, error) {
	for {
		if err := run.ctx.Err(); err != nil {
			return gc.Waiting{}, err
		}
		if err := ctx.Err(); err != nil {
			return gc.Waiting{}, err
		}
		waiting, err := s.multi.approvals.Resolve(ctx, b, reference)
		if err != nil {
			return gc.Waiting{}, err
		}
		if err = validateApprovalWaiting(waiting); err != nil {
			return gc.Waiting{}, err
		}
		if waiting.RequestID != reference {
			return gc.Waiting{}, fault(f.Forbidden)
		}
		if waiting.Status != gc.Pending {
			return waiting, nil
		}
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-timer.C:
		case <-run.ctx.Done():
			timer.Stop()
			return gc.Waiting{}, run.ctx.Err()
		case <-ctx.Done():
			timer.Stop()
			return gc.Waiting{}, ctx.Err()
		}
	}
}

func validateApprovalWaiting(w gc.Waiting) error {
	if _, err := f.ParseID[gc.RequestIdentity](w.RequestID); err != nil {
		return fault(f.InvalidState)
	}
	if w.OperationID.Validate() != nil || w.Fingerprint.Validate() != nil || (w.Status != gc.Pending && w.Status != gc.Approved && w.Status != gc.Rejected) {
		return fault(f.InvalidState)
	}
	return nil
}

func (s *directTextState) changeApprovalWaiting(ctx context.Context, run *directTextCall, b tc.ToolCallBinding, plan tc.ToolCallPlan, ref string, enter bool) error {
	cause, _ := f.NewJobCause("execution-approval-wait", b.ExecutionID.String(), ref)
	var summary c.Summary
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
			return portError(err)
		}
		if err := requireProjectCallInTx(ctx, tx, s.store); err != nil {
			return err
		}
		expected := ""
		if !enter {
			expected = ref
		}
		if err := s.multi.authority.withToolCallStateInTx(ctx, tx, b, plan, expected, nil); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		row, err := loadExecution(ctx, x, b.ExecutionID)
		if err != nil {
			return err
		}
		if row == nil {
			return fault(f.NotFound)
		}
		query := `UPDATE agenteam_execution.executions SET status='waiting',waiting_reason='approval',waiting_reference=$3,waiting_at=GREATEST(clock_timestamp(),started_at),version=version+1,updated_at=GREATEST(clock_timestamp(),updated_at+interval '1 microsecond') WHERE id=$1 AND version=$2 AND status='running' AND cancel_requested_at IS NULL`
		if !enter {
			query = `UPDATE agenteam_execution.executions SET status='running',waiting_reason=NULL,waiting_reference=NULL,waiting_at=NULL,version=version+1,updated_at=GREATEST(clock_timestamp(),updated_at+interval '1 microsecond') WHERE id=$1 AND version=$2 AND status='waiting' AND waiting_reference=$3 AND cancel_requested_at IS NULL`
		}
		tag, err := x.Exec(ctx, query, b.ExecutionID.String(), int64(row.summary.Version), ref)
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return fault(f.ConfirmationStale)
		}
		summary = row.summary.Clone()
		summary.Version++
		summary.Status = c.Running
		if enter {
			summary.Status = c.Waiting
		}
		return ctx.Err()
	})
	if err := commitError(result); err != nil {
		return err
	}
	s.mu.Lock()
	run.summary = summary
	s.mu.Unlock()
	return nil
}

func (s *directTextState) finishUndispatchedTool(run *directTextCall, err error) (c.DirectTextReceipt, error) {
	// The Model session was joined before its ToolCall checkpoint; no Tool
	// dispatch was accepted. Preserve that real join proof for normal retirement.
	if run.toolDispatched || !run.allJoined || !joinedLoopCheckpoint(run) {
		return s.loopFailure(run, "approval-wait", fault(f.ResourceBusy))
	}
	s.mu.Lock()
	run.invocation.callError = err
	run.invocation.response = nil
	run.phase = "closing"
	run.unresolved = nil
	run.uncertainty = ""
	s.mu.Unlock()
	return s.finish(run)
}
