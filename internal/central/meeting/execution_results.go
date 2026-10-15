package meeting

import (
	"context"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
)

type resultWitnessKey struct{}
type resultWitness struct {
	authority *Authority
	tx        f.Tx
	project   i.ProjectID
	meeting   c.MeetingID
	intent    intentRecord
	active    bool
}

func (a *Authority) RequireBindingInTx(ctx context.Context, tx f.Tx, store any) error {
	if a == nil || ctx == nil {
		return fault(f.DependencyUnbound)
	}
	w, _ := ctx.Value(resultWitnessKey{}).(*resultWitness)
	if w == nil || !w.active || w.authority != a || w.tx != tx || !sameStore(a.store, store) {
		return fault(f.Forbidden)
	}
	_, err := a.store.InTx(tx)
	return portError(err)
}
func (a *Authority) RequireMeetingResultInTx(ctx context.Context, tx f.Tx, p i.ProjectID, m ec.AssociatedMeeting) error {
	if err := a.RequireBindingInTx(ctx, tx, a.store); err != nil {
		return err
	}
	w := ctx.Value(resultWitnessKey{}).(*resultWitness)
	if p != w.project || m != w.intent.association() {
		return fault(f.Forbidden)
	}
	if err := a.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(p, f.Shared), meetingLock(w.meeting, f.Exclusive)}); err != nil {
		return portError(err)
	}
	x, err := a.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	current, err := loadIntentForRequest(ctx, x, w.intent.request)
	if err != nil {
		return err
	}
	if current.execution == nil || *current.execution != m.ExecutionID {
		return fault(f.ConfirmationStale)
	}
	return nil
}
func (r *Runtime) readResult(ctx context.Context, p i.ProjectID, m c.MeetingID, intent intentRecord) (ec.MeetingExecutionResult, error) {
	association := intent.association()
	locks, err := ec.MeetingExecutionReadLocks(p, association)
	if err != nil {
		return ec.MeetingExecutionResult{}, err
	}
	locks, err = r.runtimeLocks(ctx, p, m, append(locks, meetingLock(m, f.Exclusive)))
	if err != nil {
		return ec.MeetingExecutionResult{}, err
	}
	cause, err := readCause("meeting.execution-result")
	if err != nil {
		return ec.MeetingExecutionResult{}, err
	}
	var out ec.MeetingExecutionResult
	result := r.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := r.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := r.requireRuntimeProjectInTx(ctx, tx, p, m); err != nil {
			return err
		}
		w := &resultWitness{authority: r.deps.Authority, tx: tx, project: p, meeting: m, intent: intent, active: true}
		defer func() { w.active = false }()
		var err error
		out, err = r.deps.Results.ReadMeetingExecutionInTx(context.WithValue(ctx, resultWitnessKey{}, w), tx, p, association)
		return portError(err)
	})
	if err = commitError(result); err != nil {
		return ec.MeetingExecutionResult{}, err
	}
	return out, nil
}

var _ ec.MeetingResultAuthority = (*Authority)(nil)
