package execution

import (
	"context"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// This callback borrows both original owners: the private ToolCallPlan and
// Meeting's fixed authority/private transaction witness. It is not a DTO-based
// resume endpoint and cannot be used for Task or Approval waiting.
func (d *ToolCallDriver) WithMeetingDecisionToolCallInTx(ctx context.Context, tx f.Tx, call c.MeetingDecisionCall, plan tc.ToolCallPlan, reference string, waiting bool, consume func(context.Context, []byte) error) error {
	if d == nil || d.driver == nil || d.driver.state.multi == nil || nilPort(d.driver.state.multi.meetingDecisions) {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil || call.Validate() != nil || consume == nil {
		return invalid()
	}
	if _, err := f.ParseID[struct{}](reference); err != nil {
		return invalid()
	}
	s := d.driver.state
	if err := requireProjectCallInTx(ctx, tx, s.store); err != nil {
		return err
	}
	expected, phase := "", "create"
	if waiting {
		expected, phase = reference, "wait"
	}
	return d.withToolCallStateInTx(ctx, tx, call.Call, plan, expected, func(ctx context.Context, _ tc.ToolCallBinding, raw []byte) error {
		if err := d.requireMeetingDecisionInTx(ctx, tx, call, reference, phase, waiting); err != nil {
			return err
		}
		return consume(ctx, raw)
	})
}

func (d *ToolCallDriver) requireMeetingDecisionInTx(ctx context.Context, tx f.Tx, call c.MeetingDecisionCall, reference, phase string, waiting bool) error {
	s := d.driver.state
	x, err := s.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	row, err := loadExecution(ctx, x, call.Call.ExecutionID)
	if err != nil {
		return err
	}
	if row == nil || row.launch.Purpose != "meeting/response" || row.launch.Trigger.Kind != "meeting" || row.launch.Trigger.Validate(row.launch.Purpose) != nil || row.summary.ProjectID != call.Call.ProjectID || row.summary.AgentID != call.Call.AgentID || row.summary.CancelRequestedAt != nil {
		return fault(f.Forbidden)
	}
	if waiting {
		var reason, ref *string
		if err = x.QueryRow(ctx, `SELECT waiting_reason,waiting_reference::text FROM agenteam_execution.executions WHERE id=$1`, call.Call.ExecutionID.String()).Scan(&reason, &ref); err != nil {
			return unavailable(err)
		}
		if row.summary.Status != c.Waiting || reason == nil || *reason != "decision" || ref == nil || *ref != reference {
			return fault(f.ConfirmationStale)
		}
	} else if row.summary.Status != c.Running {
		return fault(f.ConfirmationStale)
	}
	if err = s.multi.meetingDecisions.RequireMeetingDecisionBindingInTx(ctx, tx, s.store); err != nil {
		return portError(err)
	}
	return portError(s.multi.meetingDecisions.RequireMeetingDecisionCallInTx(ctx, tx, call, row.launch.Trigger, reference, phase))
}

// Creation and entering waiting share the domain's transaction. Resumption
// similarly requires the persisted answer/skip in that same transaction. The
// original live runner reloads the committed version before consuming results;
// no in-memory version is advanced before the domain commit is known.
func (d *ToolCallDriver) SetMeetingDecisionWaitingInTx(ctx context.Context, tx f.Tx, call c.MeetingDecisionCall, plan tc.ToolCallPlan, reference string, enter bool) error {
	if d == nil || d.driver == nil || d.driver.state.multi == nil || nilPort(d.driver.state.multi.meetingDecisions) {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil || call.Validate() != nil {
		return invalid()
	}
	if _, err := f.ParseID[struct{}](reference); err != nil {
		return invalid()
	}
	s := d.driver.state
	if err := requireProjectCallInTx(ctx, tx, s.store); err != nil {
		return err
	}
	expected, phase := "", "enter"
	if !enter {
		expected, phase = reference, "resume"
	}
	return d.withToolCallStateInTx(ctx, tx, call.Call, plan, expected, func(ctx context.Context, _ tc.ToolCallBinding, _ []byte) error {
		if err := d.requireMeetingDecisionInTx(ctx, tx, call, reference, phase, !enter); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		query := `UPDATE agenteam_execution.executions SET status='waiting',waiting_reason='decision',waiting_reference=$2,waiting_at=GREATEST(clock_timestamp(),started_at),version=version+1,updated_at=GREATEST(clock_timestamp(),updated_at+interval '1 microsecond') WHERE id=$1 AND status='running' AND cancel_requested_at IS NULL`
		if !enter {
			query = `UPDATE agenteam_execution.executions SET status='running',waiting_reason=NULL,waiting_reference=NULL,waiting_at=NULL,version=version+1,updated_at=GREATEST(clock_timestamp(),updated_at+interval '1 microsecond') WHERE id=$1 AND status='waiting' AND waiting_reason='decision' AND waiting_reference=$2 AND cancel_requested_at IS NULL`
		}
		tag, err := x.Exec(ctx, query, call.Call.ExecutionID.String(), reference)
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return fault(f.ConfirmationStale)
		}
		return ctx.Err()
	})
}

// Discovery is read-only and must run before the complete result transaction
// lock union is acquired. Only successful request-decision results participate.
func (s *directTextState) meetingDecisionResultLocks(ctx context.Context, b tc.RunnerResultBinding) ([]f.LockRequest, error) {
	if b.Call.Binding.HandlerID != "meeting.request-decision" || b.Outcome != "success" {
		return nil, nil
	}
	if s.multi == nil || nilPort(s.multi.meetingDecisions) {
		return nil, fault(f.DependencyUnbound)
	}
	if b.Validate() != nil {
		return nil, invalid()
	}
	return s.multi.meetingDecisions.DiscoverMeetingDecisionResult(ctx, b)
}

// Called only inside the original WithRunnerResultInTx callback, after the
// immutable loop_tool_results insert and before its successor round. The SQL
// pair check additionally binds this decision to that exact consumed result.
func (s *directTextState) recordMeetingDecisionResultInTx(ctx context.Context, tx f.Tx, b tc.RunnerResultBinding, part mc.ToolResultPart, messageDigest f.Digest, sequence f.Progress, version f.Version, at f.Instant) error {
	if b.Call.Binding.HandlerID != "meeting.request-decision" || b.Outcome != "success" {
		return nil
	}
	if s.multi == nil || nilPort(s.multi.meetingDecisions) {
		return fault(f.DependencyUnbound)
	}
	if b.Validate() != nil || messageDigest.Validate() != nil || sequence < 1 || version.Validate() != nil || at.Validate() != nil {
		return invalid()
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	row, err := loadExecution(ctx, x, b.Call.ExecutionID)
	if err != nil {
		return err
	}
	if row == nil || row.summary.Status != c.Running || row.summary.CancelRequestedAt != nil || row.launch.Purpose != "meeting/response" || row.launch.Trigger.Kind != "meeting" {
		return fault(f.ConfirmationStale)
	}
	if err = s.multi.meetingDecisions.RequireMeetingDecisionBindingInTx(ctx, tx, s.store); err != nil {
		return portError(err)
	}
	resolution, err := s.multi.meetingDecisions.ReadMeetingDecisionResultInTx(ctx, tx, b, row.launch.Trigger, part)
	if err != nil {
		return portError(err)
	}
	if _, err = f.ParseID[struct{}](resolution.DecisionID); err != nil || resolution.Version.Validate() != nil {
		return fault(f.InvalidState)
	}
	tag, err := x.Exec(ctx, meetingDecisionResultInsert, b.Call.ExecutionID.String(), b.Call.RoundID, resolution.DecisionID, b.OperationID.String(), int64(resolution.Version), b.Call.CallID, string(messageDigest), int64(sequence), int64(version), at.Time(), b.AttemptID.String(), string(b.RequestID), string(b.ResultDigest), string(b.TerminalDigest))
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.ConfirmationStale)
	}
	return nil
}

var _ c.MeetingDecisionExecution = (*ToolCallDriver)(nil)

// Select the already-consumed row's typed facts. Reusing bare parameters in
// both an INSERT target domain and a comparison makes PostgreSQL infer
// incompatible domain/base types (42P08). The three new facts use base casts;
// the target constraints and exact original result tuple remain authoritative.
const meetingDecisionResultInsert = `INSERT INTO agenteam_execution.loop_decision_results(execution_id,round_id,decision_id,operation_id,decision_version,call_id,message_digest,transcript_sequence,checkpoint_version,created_at)
SELECT execution_id,round_id,$3::text,operation_id,$5::bigint,call_id,message_digest,transcript_sequence,checkpoint_version,$10::timestamptz
FROM agenteam_execution.loop_tool_results
WHERE execution_id=$1 AND round_id=$2 AND operation_id=$4 AND attempt_id=$11 AND request_id=$12 AND call_id=$6 AND result_digest=$13 AND terminal_digest=$14 AND message_digest=$7 AND transcript_sequence=$8 AND checkpoint_version=$9`
