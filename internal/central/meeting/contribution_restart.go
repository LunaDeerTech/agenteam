package meeting

import (
	"context"
	"encoding/json"
	"math"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// A completed Turn stays completed when a Human replaces one contribution.
// Only its exact durable replacement intent can continue in that Turn.
func contributionTurnActive(status string, request ec.LaunchRequest) bool {
	return status == "running" || status == "completed" && (request.Lineage.RetryOf != nil || request.Lineage.RegenerateOf != nil)
}

func loadHistoricalIntents(ctx context.Context, x postgres.SQLExecutor, meeting c.MeetingID) ([]intentRecord, error) {
	rows, err := x.Query(ctx, `SELECT c.id,c.generation,c.attempt,c.cancel_requested FROM agenteam_meeting.contributions c JOIN agenteam_meeting.turns t ON t.id=c.turn_id WHERE c.meeting_id=$1 AND t.status='completed' AND c.kind='agent' AND c.status IN('pending','waiting_for_agent','running') ORDER BY t.created_at,t.id,c.sort_order LIMIT 32`, meeting.String())
	if err != nil {
		return nil, portError(err)
	}
	type target struct {
		id                  c.ContributionID
		generation, attempt f.Version
		cancel              bool
	}
	targets := []target{}
	for rows.Next() {
		var raw string
		var generation, attempt int64
		v := target{}
		if err = rows.Scan(&raw, &generation, &attempt, &v.cancel); err != nil {
			rows.Close()
			return nil, portError(err)
		}
		v.id, err = f.ParseID[c.Contribution](raw)
		if err != nil {
			rows.Close()
			return nil, fault(f.InternalError)
		}
		v.generation, v.attempt = f.Version(generation), f.Version(attempt)
		targets = append(targets, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, portError(err)
	}
	out := []intentRecord{}
	for _, v := range targets {
		intent, err := loadIntentByContribution(ctx, x, v.id, v.generation, v.attempt)
		if err != nil {
			return nil, err
		}
		if !contributionTurnActive("completed", intent.request) {
			return nil, fault(f.InvalidState)
		}
		intent.cancelRequested = v.cancel
		out = append(out, intent)
	}
	return out, nil
}

func restartLineage(previous ec.LaunchRequest, execution ec.Summary, action string) (ec.Lineage, error) {
	if previous.Lineage.ContributionGeneration == nil || previous.Lineage.ContributionAttempt == nil || execution.ID.Validate() != nil || execution.Trigger != previous.Trigger || execution.ProjectID != previous.ProjectID || execution.AgentID != previous.AgentID {
		return ec.Lineage{}, fault(f.ConfirmationStale)
	}
	generation, attempt := *previous.Lineage.ContributionGeneration, *previous.Lineage.ContributionAttempt
	next := ec.Lineage{ContributionGeneration: &generation, ContributionAttempt: &attempt}
	switch action {
	case "retry-contribution":
		if execution.Status != ec.Failed || attempt == math.MaxInt64 {
			return ec.Lineage{}, fault(f.InvalidState)
		}
		attempt++
		next.RetryOf = &execution.ID
	case "regenerate-contribution":
		if execution.Status != ec.Succeeded || generation == math.MaxInt64 {
			return ec.Lineage{}, fault(f.InvalidState)
		}
		generation++
		attempt = 1
		next.RegenerateOf = &execution.ID
	default:
		return ec.Lineage{}, fault(f.InvalidArgument)
	}
	return next, nil
}

// restartContribution records a new logical intent under the Owner/Meeting
// gate and original Execution read locks. It does not call Launch, and a lost
// response is resolved by the same Control receipt as any other Human command.
func (s *Service) restartContribution(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, session c.Session, r c.ControlRequest, initial intentRecord) error {
	if session.Status != "active" {
		return fault(f.InvalidState)
	}
	current, err := loadCurrentContributionIntent(ctx, x, r.ProjectID, r.MeetingID, *r.ContributionID)
	if err != nil {
		return err
	}
	if current.id != initial.id || current.association() != initial.association() || current.request.Trigger.TurnID != r.TurnID.String() {
		return fault(f.ConfirmationStale)
	}
	var status, turnStatus string
	var cancel, finalizing bool
	err = x.QueryRow(ctx, `SELECT c.status,t.status,(c.cancel_requested OR t.cancel_requested),EXISTS(SELECT 1 FROM agenteam_meeting.turns active WHERE active.meeting_id=c.meeting_id AND active.status='finalizing') FROM agenteam_meeting.contributions c JOIN agenteam_meeting.turns t ON t.id=c.turn_id WHERE c.id=$1 AND c.meeting_id=$2`, r.ContributionID.String(), r.MeetingID.String()).Scan(&status, &turnStatus, &cancel, &finalizing)
	if err != nil {
		return portError(err)
	}
	// A frozen summary generation cannot be invalidated by a new replacement.
	// Already admitted replacements are joined before the next finalize starts.
	if cancel || finalizing || (turnStatus != "running" && turnStatus != "completed") || r.Action == "retry-contribution" && status != "failed" || r.Action == "regenerate-contribution" && status != "completed" {
		return fault(f.InvalidState)
	}
	w := &resultWitness{authority: s.deps.DecisionAuthority, tx: tx, project: r.ProjectID, meeting: r.MeetingID, intent: current, active: true}
	defer func() { w.active = false }()
	result, err := s.deps.DecisionResults.ReadMeetingExecutionInTx(context.WithValue(ctx, resultWitnessKey{}, w), tx, r.ProjectID, current.association())
	if err != nil {
		return portError(err)
	}
	lineage, err := restartLineage(current.request, result.Summary, r.Action)
	if err != nil || result.Summary.ID != *current.execution {
		if err != nil {
			return err
		}
		return fault(f.ConfirmationStale)
	}
	contribution := contributionRecord{id: *r.ContributionID, agent: current.request.AgentID, generation: *lineage.ContributionGeneration, attempt: *lineage.ContributionAttempt}
	contribution.participant, err = f.ParseID[c.Participant](current.request.Trigger.ParticipantID)
	if err != nil {
		return fault(f.InternalError)
	}
	request := launchRequest(r.ProjectID, r.MeetingID, *r.TurnID, contribution)
	request.Lineage = lineage
	request.Policy = current.request.Policy.Clone()
	inputID := current.inputID
	if r.Action == "regenerate-contribution" {
		frozen, e := captureContext(ctx, x, r.ProjectID, r.MeetingID, *r.TurnID)
		if e != nil {
			return e
		}
		inputID, err = saveFrozenContext(ctx, x, r.MeetingID, frozen)
		if err != nil {
			return err
		}
	}
	if _, err = saveLaunchIntent(ctx, x, r.MeetingID, *r.ContributionID, inputID, request); err != nil {
		return err
	}
	tag, err := x.Exec(ctx, `UPDATE agenteam_meeting.contributions SET status='pending',generation=$2,attempt=$3,current_execution_id=NULL,waiting_on_execution_id=NULL,cancel_requested=false,started_at=NULL,completed_at=NULL WHERE id=$1 AND current_execution_id=$4`, r.ContributionID.String(), int64(contribution.generation), int64(contribution.attempt), current.execution.String())
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.ConfirmationStale)
	}
	_, err = x.Exec(ctx, `UPDATE agenteam_meeting.timeline_items SET version=version+1 WHERE contribution_id=$1`, r.ContributionID.String())
	return portError(err)
}

func saveFrozenContext(ctx context.Context, x postgres.SQLExecutor, meeting c.MeetingID, frozen FrozenContext) (c.InputID, error) {
	raw, err := canonical(frozen)
	if err != nil {
		return c.InputID{}, err
	}
	if len(raw) > ec.MaxTriggerInputBytes {
		return c.InputID{}, fault(f.PayloadTooLarge)
	}
	id, err := f.NewID[c.Input]()
	if err != nil {
		return id, portError(err)
	}
	_, err = x.Exec(ctx, `INSERT INTO agenteam_meeting.inputs(id,meeting_id,input_data,input_digest,created_at) VALUES($1,$2,$3,$4,$5)`, id.String(), meeting.String(), raw, digest(raw).String(), now())
	return id, portError(err)
}

func saveLaunchIntent(ctx context.Context, x postgres.SQLExecutor, meeting c.MeetingID, contribution c.ContributionID, input c.InputID, request ec.LaunchRequest) (intentRecord, error) {
	id, err := f.NewID[c.Intent]()
	if err != nil {
		return intentRecord{}, portError(err)
	}
	request.Meta.RequestID, err = f.NewID[f.Request]()
	if err != nil {
		return intentRecord{}, portError(err)
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return intentRecord{}, portError(err)
	}
	requestDigest, err := request.Digest()
	if err != nil {
		return intentRecord{}, err
	}
	_, err = x.Exec(ctx, `INSERT INTO agenteam_meeting.launch_intents(id,meeting_id,contribution_id,generation,attempt,agent_id,input_id,request_id,idempotency_key,request_data,request_digest,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, id.String(), meeting.String(), contribution.String(), int64(*request.Lineage.ContributionGeneration), int64(*request.Lineage.ContributionAttempt), request.AgentID.String(), input.String(), request.Meta.RequestID.String(), request.Meta.IdempotencyKey.String(), raw, requestDigest.String(), now())
	if err != nil {
		return intentRecord{}, portError(err)
	}
	return loadIntentForRequest(ctx, x, request)
}
