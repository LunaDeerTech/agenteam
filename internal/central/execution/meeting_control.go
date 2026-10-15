package execution

import (
	"context"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"math"
)

type MeetingExecutionControl struct {
	cancellation *CancellationService
	authority    c.MeetingControlAuthority
}

func NewMeetingExecutionControl(cancellation *CancellationService, authority c.MeetingControlAuthority) (*MeetingExecutionControl, error) {
	if cancellation == nil || cancellation.owner == nil || cancellation.owner.state == nil || nilPort(authority) {
		return nil, fault(f.DependencyUnbound)
	}
	return &MeetingExecutionControl{cancellation, authority}, nil
}
func (s *MeetingExecutionControl) executor(ctx context.Context, tx f.Tx, locks []f.LockRequest) (postgres.SQLExecutor, error) {
	if s == nil || s.cancellation == nil || ctx == nil || !tx.Valid() {
		return nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store := s.cancellation.owner.state.store
	x, err := store.InTx(tx)
	if err != nil {
		return nil, portError(err)
	}
	if err = store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return nil, portError(err)
	}
	if err = s.authority.RequireMeetingControlBindingInTx(ctx, tx, store); err != nil {
		return nil, portError(err)
	}
	return x, nil
}
func (s *MeetingExecutionControl) ReadMeetingAgentSlotInTx(ctx context.Context, tx f.Tx, p i.ProjectID, a i.AgentID, trigger c.Trigger) (c.AgentSlotObservation, error) {
	if trigger.Validate("meeting/response") != nil || trigger.Kind != "meeting" {
		return c.AgentSlotObservation{}, invalid()
	}
	locks, err := c.MeetingAgentSlotLocks(p, a)
	if err != nil {
		return c.AgentSlotObservation{}, err
	}
	x, err := s.executor(ctx, tx, locks)
	if err != nil {
		return c.AgentSlotObservation{}, err
	}
	if err = s.authority.RequireMeetingSlotInTx(ctx, tx, p, a, trigger); err != nil {
		return c.AgentSlotObservation{}, portError(err)
	}
	row, err := scanExecution(x.QueryRow(ctx, agentSlotObservationSQL, a.String()))
	if ctx.Err() != nil {
		return c.AgentSlotObservation{}, ctx.Err()
	}
	if err != nil {
		return c.AgentSlotObservation{}, err
	}
	if row == nil {
		return c.AgentSlotObservation{}, nil
	}
	if row.summary.AgentID != a || row.summary.ProjectID != p || row.summary.Status.Terminal() {
		return c.AgentSlotObservation{}, unavailable(nil)
	}
	value := row.summary.Clone()
	return c.AgentSlotObservation{Occupied: true, ExecutionID: &value.ID, Status: value.Status, CancelRequestedAt: value.CancelRequestedAt}, nil
}
func meetingControlMatches(row *executionRecord, p i.ProjectID, m c.AssociatedMeeting) bool {
	if row == nil {
		return false
	}
	v := associatedIdentity{AssociatedDispatch: c.AssociatedDispatch{ExecutionID: m.ExecutionID, AgentID: m.AgentID, Key: m.Key, Digest: m.Digest}, meeting: m}
	return row.summary.ID == m.ExecutionID && row.summary.ProjectID == p && row.summary.AgentID == m.AgentID && row.digest == m.Digest && v.matchesLaunch(row.launch)
}
func (s *MeetingExecutionControl) RequestMeetingCancelInTx(ctx context.Context, tx f.Tx, p i.ProjectID, m c.AssociatedMeeting) (c.Summary, error) {
	locks, err := c.MeetingExecutionReadLocks(p, m)
	if err != nil {
		return c.Summary{}, err
	}
	x, err := s.executor(ctx, tx, locks)
	if err != nil {
		return c.Summary{}, err
	}
	if err = s.authority.RequireMeetingCancellationInTx(ctx, tx, p, m); err != nil {
		return c.Summary{}, portError(err)
	}
	state := s.cancellation.owner.state
	if err = state.proof.RequireCurrentProcessInTx(ctx, tx, state.process); err != nil {
		return c.Summary{}, portError(err)
	}
	row, err := loadExecution(ctx, x, m.ExecutionID)
	if err != nil {
		return c.Summary{}, err
	}
	if !meetingControlMatches(row, p, m) {
		return c.Summary{}, fault(f.ConfirmationStale)
	}
	return requestCancellationInTx(ctx, x, row, nil)
}

// Shared with Task cancellation: only the source authorization differs. The
// original Execution owners perform all cancellation and Approval settlement.
func requestCancellationInTx(ctx context.Context, x postgres.SQLExecutor, row *executionRecord, expected *f.Version) (c.Summary, error) {
	if expected != nil && (expected.Validate() != nil || row.summary.Version != *expected) {
		return c.Summary{}, fault(f.VersionConflict)
	}
	if row.summary.Status.Terminal() || row.summary.CancelRequestedAt != nil {
		return row.summary.Clone(), nil
	}
	if row.summary.Version == f.Version(math.MaxInt64) {
		return c.Summary{}, fault(f.ResourceBusy)
	}
	at, err := captureTime(ctx, x)
	if err != nil {
		return c.Summary{}, err
	}
	tag, err := x.Exec(ctx, `UPDATE agenteam_execution.executions SET cancel_requested_at=$3,version=version+1,updated_at=GREATEST($3,updated_at+interval '1 microsecond') WHERE id=$1 AND version=$2 AND cancel_requested_at IS NULL AND status NOT IN('succeeded','failed','cancelled')`, row.summary.ID.String(), int64(row.summary.Version), at.Time())
	if err != nil {
		return c.Summary{}, unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return c.Summary{}, fault(f.VersionConflict)
	}
	row.summary.Version++
	row.summary.CancelRequestedAt = &at
	return row.summary.Clone(), nil
}

// Notify cannot introduce a request. It rechecks the durable full association
// and current ProcessGuard before signalling the same original runtime owner.
func (s *MeetingExecutionControl) NotifyMeetingCancellation(ctx context.Context, p i.ProjectID, m c.AssociatedMeeting) error {
	if s == nil || ctx == nil {
		return invalid()
	}
	locks, err := c.MeetingExecutionReadLocks(p, m)
	if err != nil {
		return err
	}
	meeting, _ := f.AggregateLock(f.MeetingAggregate, m.Trigger.MeetingID)
	locks = append(locks, f.LockRequest{Key: meeting, Mode: f.Exclusive})
	state := s.cancellation.owner.state
	ctx, done, err := state.service.begin(ctx)
	if err != nil {
		return err
	}
	defer done()
	cause, _ := f.NewRecoveryCause("execution.meeting.cancel.notify", m.ExecutionID.String(), p.String())
	requested := false
	result := state.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := state.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := state.proof.RequireCurrentProcessInTx(ctx, tx, state.process); err != nil {
			return portError(err)
		}
		if err := s.authority.RequireMeetingCancellationNotificationInTx(ctx, tx, p, m); err != nil {
			return portError(err)
		}
		x, err := state.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		row, err := loadExecution(ctx, x, m.ExecutionID)
		if err != nil {
			return err
		}
		if !meetingControlMatches(row, p, m) {
			return fault(f.ConfirmationStale)
		}
		requested = row.summary.CancelRequestedAt != nil && !row.summary.Status.Terminal()
		return ctx.Err()
	})
	if err = commitError(result); err != nil {
		return err
	}
	if requested {
		s.cancellation.signal(m.ExecutionID)
	}
	return nil
}

var _ c.MeetingControl = (*MeetingExecutionControl)(nil)
