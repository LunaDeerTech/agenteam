package execution

import (
	"context"
	"reflect"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	gc "github.com/LunaDeerTech/agenteam/internal/central/governance/approval/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

func (s *directTextState) consumeApprovalRejection(ctx context.Context, run *directTextCall, b tc.ToolCallBinding, reference string) (c.DirectTextReceipt, error) {
	if nilPort(s.multi.rejected) {
		return s.loopFailure(run, "approval-wait", fault(f.DependencyUnbound))
	}
	plan, err := s.multi.rejected.DiscoverRejectedResult(ctx, b, reference)
	if err != nil {
		return s.loopFailure(run, "approval-wait", portError(err))
	}
	if nilPort(plan) || !sameToolCallBinding(plan.Binding(), b) || plan.Waiting().RequestID != reference || plan.Waiting().Status != gc.Rejected {
		return s.loopFailure(run, "approval-wait", fault(f.Forbidden))
	}
	callPlan, err := s.multi.authority.DiscoverToolCall(ctx, b)
	if err != nil {
		return s.loopFailure(run, "approval-wait", err)
	}
	skillPlan, err := s.discoverSkillRound(ctx, run, s.multi.skills)
	if err != nil {
		return s.loopFailure(run, "approval-wait", err)
	}
	locks, err := oc.NormalizeLocks(append(append(append([]f.LockRequest{}, run.locks...), plan.RequiredLocks()...), skillPlan.RequiredLocks()...))
	if err != nil {
		return s.loopFailure(run, "approval-wait", err)
	}
	cause, _ := f.NewJobCause("execution-approval-result", b.ExecutionID.String(), reference)
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := requireProjectCallInTx(ctx, tx, s.store); err != nil {
			return err
		}
		if err := s.multi.guard.RequireCurrentProcessInTx(ctx, tx, s.multiProcess()); err != nil {
			return portError(err)
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		row, err := loadExecution(ctx, x, b.ExecutionID)
		if err != nil {
			return err
		}
		current, err := loadDirectText(ctx, x, b.ExecutionID)
		if err != nil {
			return err
		}
		if row == nil || row.summary.Status != c.Waiting || row.summary.CancelRequestedAt != nil || !sameDirectText(current, run.snapshot, run.round) || current.toolPending == nil {
			return fault(f.ConfirmationStale)
		}
		if err = s.multi.authority.withToolCallStateInTx(ctx, tx, b, callPlan, reference, func(ctx context.Context, _ tc.ToolCallBinding, _ []byte) error {
			return s.requireCurrentToolFacts(ctx, tx, run)
		}); err != nil {
			return err
		}
		transition, err := s.consumeSkillRoundInTx(ctx, tx, run, skillPlan)
		if err != nil {
			return err
		}
		return s.multi.rejected.WithRejectedResultInTx(ctx, tx, plan, func(ctx context.Context, w gc.Waiting, part mc.ToolResultPart) error {
			if w != plan.Waiting() || w.Status != gc.Rejected || w.RequestID != reference || part.CallID != b.CallID || !part.IsError {
				return fault(f.Forbidden)
			}
			message := mc.Message{Role: "tool", Parts: []mc.MessagePart{{ToolResult: &part}}}
			if message.Validate() != nil {
				return invalid()
			}
			if run.next == nil {
				at, err := captureTime(ctx, x)
				if err != nil {
					return err
				}
				n, err := newLoopSuccessor(ctx, run, tc.RunnerResultBinding{}, message, at, row.summary.Version+1, transition)
				if err != nil {
					return err
				}
				n.approvalID = reference
				run.next = n
			}
			n := run.next
			if n.approvalID != reference || n.version != row.summary.Version+1 || !reflect.DeepEqual(n.message, message) {
				return fault(f.ConfirmationStale)
			}
			r, v, old := n.round.Fields(), n.round.LoopRound.Fields(), run.round.Fields()
			toolSequence := roundAssistantSequence(run.round) + 1
			if err = validateSuccessorSkills(n, transition, toolSequence+1); err != nil {
				return err
			}
			if err = insertDirectTextEntry(ctx, x, old.ExecutionID, old.ID, int64(toolSequence), "tool_result", message, r.CreatedAt); err != nil {
				return err
			}
			raw, err := directTextJSON(message)
			if err != nil {
				return err
			}
			tag, err := x.Exec(ctx, `INSERT INTO agenteam_execution.loop_approval_results(execution_id,round_id,approval_id,operation_id,fingerprint,call_id,message_digest,transcript_sequence,checkpoint_version,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, b.ExecutionID.String(), old.ID.String(), reference, w.OperationID.String(), string(w.Fingerprint), b.CallID, string(c.TriggerInputDigest(raw)), int64(toolSequence), int64(n.version), r.CreatedAt.Time())
			if err != nil {
				return unavailable(err)
			}
			if tag.RowsAffected() != 1 {
				return unavailable(nil)
			}
			tag, err = x.Exec(ctx, `INSERT INTO agenteam_execution.rounds(id,execution_id,snapshot_id,start_id,input_binding_id,call_id,input_digest,context_digest,schema_version,content,digest,created_at,transcript_through,round_ordinal,predecessor_round_id,transcript_start) VALUES($1,$2,$3,$4,$5,$6,$7,$8,4,$9,$10,$11,$12,$13,$14,$15)`, r.ID.String(), r.ExecutionID.String(), r.SnapshotID.String(), r.StartID.String(), r.InputBindingID.String(), r.CallID.String(), string(r.Input.Digest), string(r.ContextDigest), n.round.CanonicalBytes(), string(n.round.Digest()), r.CreatedAt.Time(), int64(v.TranscriptStart-1), int64(v.Ordinal), old.ID.String(), int64(v.TranscriptStart))
			if err != nil {
				return unavailable(err)
			}
			if tag.RowsAffected() != 1 {
				return unavailable(nil)
			}
			if err = insertSuccessorSkills(ctx, x, n); err != nil {
				return err
			}
			tag, err = x.Exec(ctx, `UPDATE agenteam_execution.executions SET status='running',waiting_reason=NULL,waiting_reference=NULL,waiting_at=NULL,version=version+1,updated_at=GREATEST($4,updated_at+interval '1 microsecond') WHERE id=$1 AND version=$2 AND status='waiting' AND waiting_reference=$3 AND cancel_requested_at IS NULL`, b.ExecutionID.String(), int64(row.summary.Version), reference, r.CreatedAt.Time())
			if err != nil {
				return unavailable(err)
			}
			if tag.RowsAffected() != 1 {
				return fault(f.ConfirmationStale)
			}
			return ctx.Err()
		})
	})
	if err = commitError(result); err != nil {
		stage := "approval-wait"
		if directTextUnknown(err) {
			stage = "loop-successor"
		}
		return s.loopFailure(run, stage, err)
	}
	s.publishRuntime(run)
	return s.acceptLoopSuccessor(ctx, run)
}
