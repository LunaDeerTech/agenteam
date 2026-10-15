package meeting

import (
	"context"
	"errors"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

// GetContributionExecution reads only the current association. Discovery does
// not authorize it: the Owner, current generation/attempt and exact association
// are rechecked under the complete lock union before borrowing the formal
// Execution reader through Meeting's private transaction witness.
func (s *Service) GetContributionExecution(ctx context.Context, actor i.Actor, project i.ProjectID, meeting c.MeetingID, contribution c.ContributionID) (c.ContributionExecution, error) {
	if s == nil || ctx == nil || project.Validate() != nil || meeting.Validate() != nil || contribution.Validate() != nil {
		return c.ContributionExecution{}, fault(f.InvalidArgument)
	}
	if err := human(actor); err != nil {
		return c.ContributionExecution{}, err
	}
	if s.deps.DecisionAuthority == nil || !sameStore(s.store, s.deps.DecisionAuthority.store) || nilPort(s.deps.DecisionResults) {
		return c.ContributionExecution{}, fault(f.DependencyUnbound)
	}
	if _, err := s.GetSession(ctx, actor, project, meeting); err != nil {
		return c.ContributionExecution{}, err
	}
	initial, err := loadCurrentContributionIntent(ctx, s.store, project, meeting, contribution)
	if err != nil {
		return c.ContributionExecution{}, err
	}
	locks, err := ec.MeetingExecutionReadLocks(project, initial.association())
	if err != nil {
		return c.ContributionExecution{}, err
	}
	locks, err = normalize(append(locks, userLock(actor), meetingLock(meeting, f.Exclusive)))
	if err != nil {
		return c.ContributionExecution{}, err
	}
	cause, err := readCause("meeting.contribution-execution")
	if err != nil {
		return c.ContributionExecution{}, err
	}
	var out c.ContributionExecution
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := s.owner(ctx, tx, actor, project, i.Read); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		current, err := loadCurrentContributionIntent(ctx, x, project, meeting, contribution)
		if err != nil {
			return err
		}
		if current.id != initial.id || current.association() != initial.association() {
			return fault(f.ConfirmationStale)
		}
		w := &resultWitness{authority: s.deps.DecisionAuthority, tx: tx, project: project, meeting: meeting, intent: current, active: true}
		defer func() { w.active = false }()
		value, err := s.deps.DecisionResults.ReadMeetingExecutionInTx(context.WithValue(ctx, resultWitnessKey{}, w), tx, project, current.association())
		if err != nil {
			return portError(err)
		}
		out, err = projectContributionExecution(current, value)
		return err
	})
	if err = commitError(result); err != nil {
		return c.ContributionExecution{}, err
	}
	return out, nil
}

func loadCurrentContributionIntent(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID, meeting c.MeetingID, contribution c.ContributionID) (intentRecord, error) {
	var generation, attempt int64
	var execution *string
	var turn, participant, agent string
	err := x.QueryRow(ctx, `SELECT c.generation,c.attempt,c.current_execution_id,c.turn_id,c.participant_id,p.source_id FROM agenteam_meeting.contributions c JOIN agenteam_meeting.sessions s ON s.id=c.meeting_id JOIN agenteam_meeting.participants p ON p.id=c.participant_id AND p.meeting_id=c.meeting_id WHERE c.id=$1 AND c.meeting_id=$2 AND s.project_id=$3 AND c.kind='agent' AND p.kind='agent'`, contribution.String(), meeting.String(), project.String()).Scan(&generation, &attempt, &execution, &turn, &participant, &agent)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && execution == nil {
		return intentRecord{}, fault(f.NotFound)
	}
	if err != nil {
		return intentRecord{}, portError(err)
	}
	intent, err := loadIntentByContribution(ctx, x, contribution, f.Version(generation), f.Version(attempt))
	if err != nil {
		return intentRecord{}, err
	}
	r := intent.request
	if intent.execution == nil || intent.execution.String() != *execution || r.ProjectID != project || r.AgentID.String() != agent || r.Trigger != (ec.Trigger{Kind: "meeting", MeetingID: meeting.String(), TurnID: turn, ContributionID: contribution.String(), ParticipantID: participant}) {
		return intentRecord{}, fault(f.ConfirmationStale)
	}
	return intent, nil
}

func projectContributionExecution(intent intentRecord, value ec.MeetingExecutionResult) (c.ContributionExecution, error) {
	summary := value.Summary.Clone()
	r := intent.request
	if intent.execution == nil || summary.ID != *intent.execution || summary.ProjectID != r.ProjectID || summary.AgentID != r.AgentID || summary.Trigger != r.Trigger || summary.Purpose != "meeting/response" || r.Lineage.ContributionGeneration == nil || r.Lineage.ContributionAttempt == nil {
		return c.ContributionExecution{}, fault(f.ConfirmationStale)
	}
	out := c.ContributionExecution{ID: summary.ID, ProjectID: summary.ProjectID, AgentID: summary.AgentID, Generation: *r.Lineage.ContributionGeneration, Attempt: *r.Lineage.ContributionAttempt, Status: summary.Status, Version: summary.Version, CancelRequestedAt: summary.CancelRequestedAt, CreatedAt: summary.CreatedAt, StartedAt: summary.StartedAt, CompletedAt: summary.CompletedAt}
	if value.WaitingReason != nil {
		reason := *value.WaitingReason
		out.WaitingReason = &reason
	}
	if value.WaitingReference != nil {
		reference := *value.WaitingReference
		out.WaitingReference = &reference
	}
	var err error
	if out.MeetingID, err = f.ParseID[c.Meeting](r.Trigger.MeetingID); err != nil {
		return c.ContributionExecution{}, fault(f.InvalidState)
	}
	if out.TurnID, err = f.ParseID[c.Turn](r.Trigger.TurnID); err != nil {
		return c.ContributionExecution{}, fault(f.InvalidState)
	}
	if out.ContributionID, err = f.ParseID[c.Contribution](r.Trigger.ContributionID); err != nil {
		return c.ContributionExecution{}, fault(f.InvalidState)
	}
	if out.ParticipantID, err = f.ParseID[c.Participant](r.Trigger.ParticipantID); err != nil {
		return c.ContributionExecution{}, fault(f.InvalidState)
	}
	if out.Validate() != nil {
		return c.ContributionExecution{}, fault(f.InvalidState)
	}
	return out, nil
}
