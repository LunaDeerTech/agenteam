package meeting

import (
	"context"
	"errors"
	"reflect"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/jackc/pgx/v5"
)

type decisionWitnessKey struct{}
type decisionWitness struct {
	authority *Authority
	tx        f.Tx
	call      ec.MeetingDecisionCall
	meeting   c.MeetingID
	decision  c.DecisionID
	phase     string
	active    bool
}

func (a *Authority) RequireMeetingDecisionBindingInTx(ctx context.Context, tx f.Tx, store any) error {
	if a == nil || ctx == nil || !sameStore(a.store, store) {
		return fault(f.DependencyUnbound)
	}
	_, err := a.store.InTx(tx)
	return portError(err)
}

// The immutable launch is discovered for lock planning, then re-read under the
// complete union before it can authorize any domain mutation.
func decisionIntent(ctx context.Context, x postgres.SQLExecutor, call ec.MeetingDecisionCall) (intentRecord, error) {
	var contribution string
	var generation, attempt int64
	err := x.QueryRow(ctx, `SELECT c.id,c.generation,c.attempt FROM agenteam_meeting.launch_intents l JOIN agenteam_meeting.contributions c ON c.id=l.contribution_id JOIN agenteam_meeting.sessions s ON s.id=l.meeting_id WHERE l.execution_id=$1 AND l.agent_id=$2 AND s.project_id=$3 AND c.current_execution_id=l.execution_id AND c.generation=l.generation AND c.attempt=l.attempt`, call.Call.ExecutionID.String(), call.Call.AgentID.String(), call.Call.ProjectID.String()).Scan(&contribution, &generation, &attempt)
	if errors.Is(err, pgx.ErrNoRows) {
		return intentRecord{}, fault(f.Forbidden)
	}
	if err != nil {
		return intentRecord{}, portError(err)
	}
	id, err := f.ParseID[c.Contribution](contribution)
	if err != nil {
		return intentRecord{}, fault(f.InternalError)
	}
	return loadIntentByContribution(ctx, x, id, f.Version(generation), f.Version(attempt))
}

func sameDecisionCall(a, b ec.MeetingDecisionCall) bool {
	if !a.Call.Actor.Equal(b.Call.Actor) {
		return false
	}
	a.Call.Actor, b.Call.Actor = i.Actor{}, i.Actor{}
	return a.OperationID == b.OperationID && a.AttemptID == b.AttemptID && a.RequestID == b.RequestID && reflect.DeepEqual(a.Call, b.Call)
}

func (a *Authority) RequireMeetingDecisionCallInTx(ctx context.Context, tx f.Tx, call ec.MeetingDecisionCall, trigger ec.Trigger, reference, phase string) error {
	if a == nil || ctx == nil || call.Validate() != nil {
		return fault(f.Forbidden)
	}
	w, _ := ctx.Value(decisionWitnessKey{}).(*decisionWitness)
	if w == nil || !w.active || w.authority != a || w.tx != tx || !sameDecisionCall(w.call, call) || w.decision.String() != reference || w.phase != phase || trigger.Kind != "meeting" || trigger.MeetingID != w.meeting.String() {
		return fault(f.Forbidden)
	}
	if err := a.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(call.Call.ProjectID, f.Shared), meetingLock(w.meeting, f.Exclusive), decisionLock(w.decision)}); err != nil {
		return portError(err)
	}
	x, err := a.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	intent, err := decisionIntent(ctx, x, call)
	if err != nil {
		return err
	}
	if intent.execution == nil || *intent.execution != call.Call.ExecutionID || intent.request.Trigger != trigger {
		return fault(f.ConfirmationStale)
	}
	var status string
	var cancel, deleting bool
	err = x.QueryRow(ctx, `SELECT t.status,(t.cancel_requested OR c.cancel_requested),s.deleting FROM agenteam_meeting.turns t JOIN agenteam_meeting.sessions s ON s.id=t.meeting_id JOIN agenteam_meeting.contributions c ON c.turn_id=t.id AND c.meeting_id=s.id WHERE t.id=$1 AND s.id=$2 AND s.project_id=$3 AND c.id=$4`, trigger.TurnID, trigger.MeetingID, call.Call.ProjectID.String(), trigger.ContributionID).Scan(&status, &cancel, &deleting)
	if err != nil {
		return portError(err)
	}
	if !contributionTurnActive(status, intent.request) || cancel || deleting {
		return fault(f.InvalidState)
	}
	if phase == "create" {
		return nil
	}
	v, err := loadDecision(ctx, x, call.Call.ProjectID, w.meeting, w.decision)
	if err != nil {
		return err
	}
	if v.operation != call.OperationID.String() || v.round != call.Call.RoundID || v.view.ExecutionID != call.Call.ExecutionID || v.view.ContributionID.String() != trigger.ContributionID {
		return fault(f.Forbidden)
	}
	switch phase {
	case "enter":
		if v.view.Status != "pending" {
			return fault(f.InvalidState)
		}
	case "wait":
		if v.view.Status != "pending" && v.view.Status != "answered" && v.view.Status != "skipped" {
			return fault(f.InvalidState)
		}
	case "resume":
		if v.view.Status != "answered" && v.view.Status != "skipped" {
			return fault(f.InvalidState)
		}
	default:
		return fault(f.Forbidden)
	}
	return ctx.Err()
}

func (a *Authority) decisionResultIdentity(ctx context.Context, x postgres.SQLExecutor, b tc.RunnerResultBinding) (c.MeetingID, c.DecisionID, error) {
	var m, d string
	err := x.QueryRow(ctx, `SELECT d.meeting_id,d.id FROM agenteam_meeting.decision_requests d JOIN agenteam_meeting.sessions s ON s.id=d.meeting_id WHERE d.operation_id=$1 AND d.execution_id=$2 AND d.round_id=$3 AND s.project_id=$4`, b.OperationID.String(), b.Call.ExecutionID.String(), b.Call.RoundID, b.Call.ProjectID.String()).Scan(&m, &d)
	if errors.Is(err, pgx.ErrNoRows) {
		return c.MeetingID{}, c.DecisionID{}, fault(f.ConfirmationStale)
	}
	if err != nil {
		return c.MeetingID{}, c.DecisionID{}, portError(err)
	}
	meeting, err := f.ParseID[c.Meeting](m)
	if err != nil {
		return c.MeetingID{}, c.DecisionID{}, fault(f.InternalError)
	}
	decision, err := f.ParseID[c.Decision](d)
	if err != nil {
		return c.MeetingID{}, c.DecisionID{}, fault(f.InternalError)
	}
	return meeting, decision, nil
}

func (a *Authority) DiscoverMeetingDecisionResult(ctx context.Context, b tc.RunnerResultBinding) ([]f.LockRequest, error) {
	if a == nil || ctx == nil || b.Validate() != nil || b.Call.Binding.HandlerID != "meeting.request-decision" || b.Outcome != "success" {
		return nil, fault(f.InvalidArgument)
	}
	m, d, err := a.decisionResultIdentity(ctx, a.store, b)
	if err != nil {
		return nil, err
	}
	return normalize([]f.LockRequest{projectLock(b.Call.ProjectID, f.Shared), meetingLock(m, f.Exclusive), decisionLock(d)})
}

func (a *Authority) ReadMeetingDecisionResultInTx(ctx context.Context, tx f.Tx, b tc.RunnerResultBinding, trigger ec.Trigger, part mc.ToolResultPart) (ec.MeetingDecisionResolution, error) {
	if a == nil || ctx == nil || b.Validate() != nil || b.Call.Binding.HandlerID != "meeting.request-decision" || b.Outcome != "success" || trigger.Kind != "meeting" {
		return ec.MeetingDecisionResolution{}, fault(f.InvalidArgument)
	}
	x, err := a.store.InTx(tx)
	if err != nil {
		return ec.MeetingDecisionResolution{}, portError(err)
	}
	m, d, err := a.decisionResultIdentity(ctx, x, b)
	if err != nil {
		return ec.MeetingDecisionResolution{}, err
	}
	if err = a.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(b.Call.ProjectID, f.Shared), meetingLock(m, f.Exclusive), decisionLock(d)}); err != nil {
		return ec.MeetingDecisionResolution{}, portError(err)
	}
	v, err := loadDecision(ctx, x, b.Call.ProjectID, m, d)
	if err != nil {
		return ec.MeetingDecisionResolution{}, err
	}
	if v.intent.execution == nil || *v.intent.execution != b.Call.ExecutionID || v.intent.request.Trigger != trigger || v.operation != b.OperationID.String() || v.round != b.Call.RoundID || v.view.ExecutionID != b.Call.ExecutionID || v.cancelled || !contributionTurnActive(v.turnStatus, v.intent.request) {
		return ec.MeetingDecisionResolution{}, fault(f.ConfirmationStale)
	}
	result := c.DecisionResult{DecisionID: d, Version: v.view.Version, Status: v.view.Status, Answer: v.view.Answer}
	if result.Validate() != nil {
		return ec.MeetingDecisionResolution{}, fault(f.InvalidState)
	}
	raw, err := canonical(result)
	if err != nil {
		return ec.MeetingDecisionResolution{}, err
	}
	if b.ResultDigest != digest(raw) || int64(b.ResultBytes) != int64(len(raw)) || part.CallID != b.Call.CallID || part.IsError || len(part.Parts) != 1 || part.Parts[0].Text == nil || part.Parts[0].Image != nil || part.Parts[0].File != nil || part.Parts[0].Text.Text != string(raw) {
		return ec.MeetingDecisionResolution{}, fault(f.Forbidden)
	}
	return ec.MeetingDecisionResolution{DecisionID: d.String(), Version: result.Version}, nil
}

var _ ec.MeetingDecisionAuthority = (*Authority)(nil)
