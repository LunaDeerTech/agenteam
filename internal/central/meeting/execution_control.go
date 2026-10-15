package meeting

import (
	"context"
	"errors"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type controlWitnessKey struct{}
type controlWitness struct {
	authority *Authority
	tx        f.Tx
	project   i.ProjectID
	meeting   c.MeetingID
	intent    intentRecord
	action    string
	active    bool
}

func (a *Authority) RequireMeetingControlBindingInTx(ctx context.Context, tx f.Tx, store any) error {
	if a == nil || ctx == nil || !sameStore(a.store, store) {
		return fault(f.Forbidden)
	}
	w, _ := ctx.Value(controlWitnessKey{}).(*controlWitness)
	if w == nil || !w.active || w.authority != a || w.tx != tx || w.action == "notify" {
		return fault(f.Forbidden)
	}
	_, err := a.store.InTx(tx)
	return portError(err)
}
func (a *Authority) control(ctx context.Context, tx f.Tx, p i.ProjectID, action string) (*controlWitness, error) {
	if a == nil || ctx == nil {
		return nil, fault(f.Forbidden)
	}
	w, _ := ctx.Value(controlWitnessKey{}).(*controlWitness)
	if w == nil || !w.active || w.authority != a || w.project != p || w.action != action || (action != "notify" && w.tx != tx) {
		return nil, fault(f.Forbidden)
	}
	if err := a.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(p, f.Shared), meetingLock(w.meeting, f.Exclusive), agentLock(w.intent.request.AgentID)}); err != nil {
		return nil, portError(err)
	}
	x, err := a.store.InTx(tx)
	if err != nil {
		return nil, portError(err)
	}
	current, err := loadIntentForRequest(ctx, x, w.intent.request)
	if err != nil {
		return nil, err
	}
	if current.id != w.intent.id || current.requestDigest != w.intent.requestDigest || (w.intent.execution != nil && (current.execution == nil || *current.execution != *w.intent.execution)) {
		return nil, fault(f.ConfirmationStale)
	}
	var project string
	var cancel, deleting bool
	var currentExecution *string
	var generation, attempt int64
	err = x.QueryRow(ctx, `SELECT s.project_id,s.deleting,(t.cancel_requested OR c.cancel_requested),c.current_execution_id,c.generation,c.attempt FROM agenteam_meeting.sessions s JOIN agenteam_meeting.turns t ON t.meeting_id=s.id JOIN agenteam_meeting.contributions c ON c.turn_id=t.id AND c.meeting_id=s.id WHERE s.id=$1 AND t.id=$2 AND c.id=$3`, w.meeting.String(), w.intent.request.Trigger.TurnID, w.intent.request.Trigger.ContributionID).Scan(&project, &deleting, &cancel, &currentExecution, &generation, &attempt)
	if err != nil {
		return nil, portError(err)
	}
	if project != p.String() || action != "slot" && !cancel && !deleting {
		return nil, fault(f.Forbidden)
	}
	if generation != int64(*w.intent.request.Lineage.ContributionGeneration) || attempt != int64(*w.intent.request.Lineage.ContributionAttempt) || (w.intent.execution != nil && (currentExecution == nil || *currentExecution != w.intent.execution.String())) {
		return nil, fault(f.ConfirmationStale)
	}
	return w, nil
}
func (a *Authority) RequireMeetingSlotInTx(ctx context.Context, tx f.Tx, p i.ProjectID, agent i.AgentID, trigger ec.Trigger) error {
	w, err := a.control(ctx, tx, p, "slot")
	if err != nil {
		return err
	}
	if agent != w.intent.request.AgentID || trigger != w.intent.request.Trigger {
		return fault(f.Forbidden)
	}
	return nil
}
func (a *Authority) RequireMeetingCancellationInTx(ctx context.Context, tx f.Tx, p i.ProjectID, m ec.AssociatedMeeting) error {
	w, err := a.control(ctx, tx, p, "cancel")
	if err != nil {
		return err
	}
	if m != w.intent.association() {
		return fault(f.Forbidden)
	}
	return nil
}
func (a *Authority) RequireMeetingCancellationNotificationInTx(ctx context.Context, tx f.Tx, p i.ProjectID, m ec.AssociatedMeeting) error {
	w, err := a.control(ctx, tx, p, "notify")
	if err != nil {
		return err
	}
	if m != w.intent.association() {
		return fault(f.Forbidden)
	}
	return nil
}
func (r *Runtime) observeBusy(ctx context.Context, p i.ProjectID, m c.MeetingID, intent intentRecord) error {
	if nilPort(r.deps.Control) {
		return fault(f.DependencyUnbound)
	}
	locks, err := ec.MeetingAgentSlotLocks(p, intent.request.AgentID)
	if err != nil {
		return err
	}
	locks, err = normalize(append(locks, meetingLock(m, f.Exclusive)))
	if err != nil {
		return err
	}
	cause, err := readCause("meeting.agent-slot")
	if err != nil {
		return err
	}
	return commitError(r.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := r.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if _, err := r.deps.Projects.RequirePreparingProjectInTx(ctx, tx, p); err != nil {
			return portError(err)
		}
		w := &controlWitness{authority: r.deps.Authority, tx: tx, project: p, meeting: m, intent: intent, action: "slot", active: true}
		defer func() { w.active = false }()
		slot, err := r.deps.Control.ReadMeetingAgentSlotInTx(context.WithValue(ctx, controlWitnessKey{}, w), tx, p, intent.request.AgentID, intent.request.Trigger)
		if err != nil {
			return portError(err)
		}
		if !slot.Occupied {
			return nil
		}
		if slot.ExecutionID == nil {
			return fault(f.InvalidState)
		}
		x, err := r.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_meeting.contributions SET status='waiting_for_agent',waiting_on_execution_id=$3 WHERE meeting_id=$1 AND id=$2 AND current_execution_id IS NULL AND status IN('pending','waiting_for_agent')`, m.String(), intent.request.Trigger.ContributionID, slot.ExecutionID.String())
		return portError(err)
	}))
}

var _ ec.MeetingControlAuthority = (*Authority)(nil)

func (r *Runtime) planCancellation(ctx context.Context, x postgres.SQLExecutor, m c.MeetingID, t c.TurnID, out *runtimePlan) error {
	out.phase = "cancelling"
	contributions, err := loadPendingContributions(ctx, x, t)
	if err != nil {
		return err
	}
	pending := 0
	for _, v := range contributions {
		intent, err := loadIntentByContribution(ctx, x, v.id, v.generation, v.attempt)
		var known *f.Fault
		if err != nil && (!errors.As(err, &known) || known.Code != f.NotFound) {
			return err
		}
		if err == nil && (intent.execution != nil || intent.unknown) {
			intent.cancelRequested = true
			out.intents = append(out.intents, intent)
			pending++
			continue
		}
		if _, err = x.Exec(ctx, `UPDATE agenteam_meeting.contributions SET status='cancelled',waiting_on_execution_id=NULL,completed_at=$2 WHERE id=$1 AND current_execution_id IS NULL AND status IN('pending','waiting_for_agent')`, v.id.String(), now()); err != nil {
			return portError(err)
		}
	}
	if pending == 0 {
		// A still-owned summary must settle through its original Model owner.
		var active bool
		if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_meeting.summary_generations WHERE turn_id=$1 AND status IN('prepared','generating','failed'))`, t.String()).Scan(&active); err != nil {
			return portError(err)
		}
		if active {
			out.phase = "summary-cancelling"
			return nil
		}
		if _, err = x.Exec(ctx, `UPDATE agenteam_meeting.turns SET status='cancelled',result='partial_failure',version=version+1,completed_at=$2 WHERE id=$1 AND status IN('queued','running','finalizing')`, t.String(), now()); err != nil {
			return portError(err)
		}
		out.phase = "cancelled"
	}
	return nil
}
func (r *Runtime) requestCancellation(ctx context.Context, p i.ProjectID, m c.MeetingID, intent intentRecord) error {
	if nilPort(r.deps.Control) {
		return fault(f.DependencyUnbound)
	}
	association := intent.association()
	locks, err := ec.MeetingExecutionReadLocks(p, association)
	if err != nil {
		return err
	}
	locks, err = normalize(append(locks, meetingLock(m, f.Exclusive)))
	if err != nil {
		return err
	}
	cause, err := readCause("meeting.cancel-execution")
	if err != nil {
		return err
	}
	err = commitError(r.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := r.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		w := &controlWitness{authority: r.deps.Authority, tx: tx, project: p, meeting: m, intent: intent, action: "cancel", active: true}
		defer func() { w.active = false }()
		_, err := r.deps.Control.RequestMeetingCancelInTx(context.WithValue(ctx, controlWitnessKey{}, w), tx, p, association)
		return portError(err)
	}))
	if err != nil {
		return err
	}
	w := &controlWitness{authority: r.deps.Authority, project: p, meeting: m, intent: intent, action: "notify", active: true}
	defer func() { w.active = false }()
	return portError(r.deps.Control.NotifyMeetingCancellation(context.WithValue(ctx, controlWitnessKey{}, w), p, association))
}
