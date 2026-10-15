package meeting

import (
	"context"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"math"
)

func (s *Service) LookupControl(ctx context.Context, a i.Actor, r c.ControlRequest) (c.Receipt, bool, error) {
	if err := human(a); err != nil {
		return c.Receipt{}, false, err
	}
	if err := r.Validate(); err != nil {
		return c.Receipt{}, false, err
	}
	d, err := commandDigest(a, "control", r)
	if err != nil {
		return c.Receipt{}, false, err
	}
	return s.lookup(ctx, a, r.ProjectID, r.Meta.IdempotencyKey, d)
}

// Control records Human intent atomically. Execution cancellation is applied
// by the queue visitor through the original source-bound Execution owners.
func (s *Service) Control(ctx context.Context, a i.Actor, r c.ControlRequest) (c.Receipt, error) {
	if s == nil || ctx == nil {
		return c.Receipt{}, fault(f.InvalidArgument)
	}
	if err := human(a); err != nil {
		return c.Receipt{}, err
	}
	if err := r.Validate(); err != nil {
		return c.Receipt{}, err
	}
	d, err := commandDigest(a, "control", r)
	if err != nil {
		return c.Receipt{}, err
	}
	command, err := commandIdentity(r.ProjectID, r.Meta.IdempotencyKey)
	if err != nil {
		return c.Receipt{}, err
	}
	locks := []f.LockRequest{userLock(a), projectLock(r.ProjectID, f.Shared), meetingLock(r.MeetingID, f.Exclusive), commandLock(command)}
	var lifecycle *lifecycleEventPlan
	if r.Action == "archive" || r.Action == "unarchive" {
		// A committed command is observed before preparing another event. The
		// transaction repeats the lookup to handle competing original retries.
		if old, found, e := s.lookup(ctx, a, r.ProjectID, r.Meta.IdempotencyKey, d); e != nil || found {
			return old, e
		}
		if s.deps.LifecycleEvents == nil || s.deps.LifecycleEvents.authority == nil || !sameStore(s.store, s.deps.LifecycleEvents.authority.store) {
			return c.Receipt{}, fault(f.DependencyUnbound)
		}
		if r.ExpectedVersion == f.Version(math.MaxInt64) {
			return c.Receipt{}, fault(f.InvalidState)
		}
		name, status := c.ArchivedName, "archive"
		if r.Action == "unarchive" {
			name, status = c.RestoredName, "active"
		}
		lifecycle, err = s.deps.LifecycleEvents.prepare(ctx, a, r.ProjectID, r.MeetingID, r.ExpectedVersion+1, name, status, now(), locks)
		if err != nil {
			return c.Receipt{}, err
		}
		locks = lifecycle.locks
	}
	var currentIntent *intentRecord
	if r.Action == "cancel-contribution" || r.Action == "retry-contribution" || r.Action == "regenerate-contribution" {
		// A replay survives the current Execution becoming terminal. New
		// cancellation intent must instead observe the original live source.
		if old, found, e := s.lookup(ctx, a, r.ProjectID, r.Meta.IdempotencyKey, d); e != nil || found {
			return old, e
		}
		if s.deps.DecisionAuthority == nil || !sameStore(s.store, s.deps.DecisionAuthority.store) || nilPort(s.deps.DecisionResults) {
			return c.Receipt{}, fault(f.DependencyUnbound)
		}
		if _, err = s.GetSession(ctx, a, r.ProjectID, r.MeetingID); err != nil {
			return c.Receipt{}, err
		}
		initial, e := loadCurrentContributionIntent(ctx, s.store, r.ProjectID, r.MeetingID, *r.ContributionID)
		if e != nil {
			return c.Receipt{}, e
		}
		more, e := ec.MeetingExecutionReadLocks(r.ProjectID, initial.association())
		if e != nil {
			return c.Receipt{}, e
		}
		locks = append(locks, more...)
		currentIntent = &initial
	}
	var decisions *decisionCancellation
	if r.Action == "cancel-turn" || r.Action == "cancel-contribution" {
		if old, found, e := s.lookup(ctx, a, r.ProjectID, r.Meta.IdempotencyKey, d); e != nil || found {
			return old, e
		}
		if _, e := s.GetSession(ctx, a, r.ProjectID, r.MeetingID); e != nil {
			return c.Receipt{}, e
		}
		scope := decisionEventScope{project: r.ProjectID, meeting: r.MeetingID}
		if r.TurnID != nil {
			scope.turn = *r.TurnID
		}
		if r.ContributionID != nil {
			scope.contribution = *r.ContributionID
		}
		decisions, err = prepareDecisionCancellation(ctx, s.store, s.deps.DecisionEvents, a, scope, locks)
		if err != nil {
			return c.Receipt{}, err
		}
		locks = decisions.locks
	}
	locks, err = normalize(locks)
	if err != nil {
		return c.Receipt{}, err
	}
	cause, _ := f.NewCommandsCause(command)
	var out c.Receipt
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := s.owner(ctx, tx, a, r.ProjectID, i.Read); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if old, found, err := lookupCommand(ctx, x, r.ProjectID, a, r.Meta.IdempotencyKey, d); err != nil || found {
			out = old
			return err
		}
		if err = s.owner(ctx, tx, a, r.ProjectID, i.Mutate); err != nil {
			return err
		}
		session, err := loadSession(ctx, x, r.ProjectID, r.MeetingID)
		if err != nil {
			return err
		}
		if session.Version != r.ExpectedVersion {
			return fault(f.VersionConflict)
		}
		if session.Deleting || session.Status == "proposed" {
			return fault(f.InvalidState)
		}
		if currentIntent != nil && r.Action == "cancel-contribution" {
			if err = s.requireCancellableContribution(ctx, tx, x, r, *currentIntent); err != nil {
				return err
			}
		}
		if r.Action == "retry-contribution" || r.Action == "regenerate-contribution" {
			err = s.restartContribution(ctx, tx, x, session, r, *currentIntent)
		} else {
			err = applyControl(ctx, x, session, r)
		}
		if err != nil {
			return err
		}
		if _, err = x.Exec(ctx, `UPDATE agenteam_meeting.sessions SET version=version+1,updated_at=$2 WHERE id=$1`, r.MeetingID.String(), now()); err != nil {
			return portError(err)
		}
		out = c.Receipt{MeetingID: r.MeetingID, TurnID: r.TurnID, Version: session.Version + 1}
		if err = saveCommand(ctx, x, r.ProjectID, a, r.Meta.IdempotencyKey, "control", d, out); err != nil {
			return err
		}
		if decisions != nil {
			if err = decisions.applyInTx(ctx, tx, s.store, func(ctx context.Context, tx f.Tx) error { return s.owner(ctx, tx, a, r.ProjectID, i.Mutate) }); err != nil {
				return err
			}
		}
		if lifecycle != nil {
			return s.deps.LifecycleEvents.appendInTx(ctx, tx, lifecycle)
		}
		return nil
	})
	if err = commitError(result); err != nil {
		return c.Receipt{}, err
	}
	s.invalidateMeeting(r.ProjectID, r.MeetingID)
	return out, nil
}
func applyControl(ctx context.Context, x postgres.SQLExecutor, s c.Session, r c.ControlRequest) error {
	var changed int64
	switch r.Action {
	case "archive", "unarchive":
		from, to := "active", "archive"
		if r.Action == "unarchive" {
			from, to = to, from
		}
		if s.Status != from {
			return fault(f.InvalidState)
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_meeting.sessions SET status=$2 WHERE id=$1`, r.MeetingID.String(), to)
		if err != nil {
			return portError(err)
		}
		changed = tag.RowsAffected()
	case "cancel-turn":
		tag, err := x.Exec(ctx, `UPDATE agenteam_meeting.turns SET cancel_requested=true,version=version+1 WHERE id=$1 AND meeting_id=$2 AND status IN('queued','running','finalizing') AND NOT cancel_requested`, r.TurnID.String(), r.MeetingID.String())
		if err != nil {
			return portError(err)
		}
		changed = tag.RowsAffected()
	case "skip-contribution":
		// Launch takes this same Meeting gate before leaving waiting. A
		// retained Unknown launch cannot be converted into a skipped slot.
		tag, err := x.Exec(ctx, `UPDATE agenteam_meeting.contributions c SET status='skipped',waiting_on_execution_id=NULL,completed_at=$4 WHERE c.id=$1 AND c.turn_id=$2 AND c.meeting_id=$3 AND c.kind='agent' AND c.status='waiting_for_agent' AND c.current_execution_id IS NULL AND EXISTS(SELECT 1 FROM agenteam_meeting.turns t WHERE t.id=c.turn_id AND (t.status='running' OR (t.status='completed' AND (c.generation>1 OR c.attempt>1))) AND NOT t.cancel_requested) AND NOT EXISTS(SELECT 1 FROM agenteam_meeting.launch_intents l WHERE l.contribution_id=c.id AND l.generation=c.generation AND l.attempt=c.attempt AND (l.launch_unknown OR l.execution_id IS NOT NULL))`, r.ContributionID.String(), r.TurnID.String(), r.MeetingID.String(), now())
		if err != nil {
			return portError(err)
		}
		changed = tag.RowsAffected()
	case "cancel-contribution":
		tag, err := x.Exec(ctx, `UPDATE agenteam_meeting.contributions c SET cancel_requested=true WHERE c.id=$1 AND c.turn_id=$2 AND c.meeting_id=$3 AND c.kind='agent' AND c.status='running' AND c.current_execution_id IS NOT NULL AND NOT c.cancel_requested AND EXISTS(SELECT 1 FROM agenteam_meeting.turns t WHERE t.id=c.turn_id AND (t.status='running' OR (t.status='completed' AND (c.generation>1 OR c.attempt>1))) AND NOT t.cancel_requested)`, r.ContributionID.String(), r.TurnID.String(), r.MeetingID.String())
		if err != nil {
			return portError(err)
		}
		changed = tag.RowsAffected()
	case "retry-summary":
		tag, err := x.Exec(ctx, `UPDATE agenteam_meeting.summary_generations g SET status='prepared',error_code='' WHERE g.turn_id=$1 AND g.meeting_id=$2 AND g.status='failed' AND g.attempt<3 AND EXISTS(SELECT 1 FROM agenteam_meeting.turns t WHERE t.id=g.turn_id AND t.status='finalizing' AND NOT t.cancel_requested) AND NOT EXISTS(SELECT 1 FROM agenteam_meeting.summary_model_calls mc WHERE mc.generation_id=g.id AND mc.retired_at IS NULL)`, r.TurnID.String(), r.MeetingID.String())
		if err != nil {
			return portError(err)
		}
		changed = tag.RowsAffected()
		if changed == 1 {
			if _, err = x.Exec(ctx, `UPDATE agenteam_meeting.turns SET summary_error='',version=version+1 WHERE id=$1`, r.TurnID.String()); err != nil {
				return portError(err)
			}
		}
	default:
		return fault(f.InvalidArgument)
	}
	if changed != 1 {
		return fault(f.InvalidState)
	}
	return nil
}

func (s *Service) requireCancellableContribution(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, r c.ControlRequest, initial intentRecord) error {
	current, err := loadCurrentContributionIntent(ctx, x, r.ProjectID, r.MeetingID, *r.ContributionID)
	if err != nil {
		return err
	}
	if current.id != initial.id || current.association() != initial.association() || current.request.Trigger.TurnID != r.TurnID.String() {
		return fault(f.ConfirmationStale)
	}
	w := &resultWitness{authority: s.deps.DecisionAuthority, tx: tx, project: r.ProjectID, meeting: r.MeetingID, intent: current, active: true}
	defer func() { w.active = false }()
	result, err := s.deps.DecisionResults.ReadMeetingExecutionInTx(context.WithValue(ctx, resultWitnessKey{}, w), tx, r.ProjectID, current.association())
	if err != nil {
		return portError(err)
	}
	if result.Summary.ID != *current.execution || result.Summary.ProjectID != r.ProjectID || result.Summary.AgentID != current.request.AgentID || result.Summary.Trigger != current.request.Trigger || result.Summary.Status.Terminal() || result.Summary.CancelRequestedAt != nil {
		return fault(f.InvalidState)
	}
	return nil
}
