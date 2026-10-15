package meeting

import (
	"context"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// Only the Summary owner's cleanup path lends this witness. A persisted call
// ID, even with a matching snapshot, cannot authorize cold observation alone.
type summaryStoppedKey struct{}
type summaryStoppedOwner struct {
	owner   *summaryOwner
	request mc.StoppedCallObservationRequest
}

func (a *SummaryAuthority) stoppedOwner(ctx context.Context, request mc.StoppedCallObservationRequest) (*summaryStoppedOwner, error) {
	if a == nil || ctx == nil || request.Validate() != nil {
		return nil, fault(f.Forbidden)
	}
	w, _ := ctx.Value(summaryStoppedKey{}).(*summaryStoppedOwner)
	if w == nil || w.owner == nil || !w.owner.active || w.owner.authority != a {
		return nil, fault(f.Forbidden)
	}
	want, err := mc.StoppedCallObservationBinding(w.request)
	got, actual := mc.StoppedCallObservationBinding(request)
	d := request.Actor.Details()
	if err != nil || actual != nil || want != got || d.Kind != i.Service || d.ServiceName != i.MeetingRuntime || d.ProjectID != w.owner.input.ProjectID.String() || d.CauseRef != w.owner.input.GenerationID.String() || request.CallID != w.owner.call || request.Consumer.Kind != mc.MeetingConsumer || request.Consumer.ProjectID != w.owner.input.ProjectID || request.Consumer.MeetingID != w.owner.input.MeetingID.String() || request.Consumer.OperationID != w.owner.input.GenerationID.String() {
		return nil, fault(f.Forbidden)
	}
	return w, nil
}

func (a *SummaryAuthority) DiscoverStoppedCall(ctx context.Context, request mc.StoppedCallObservationRequest) (mc.ConsumerDependencies, error) {
	w, err := a.stoppedOwner(ctx, request)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	binding, err := mc.StoppedCallObservationBinding(request)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	locks, err := normalize(summaryLocks(w.owner.input.ProjectID, w.owner.input.MeetingID))
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	return mc.NewConsumerDependencies(a.issuer, mc.ConsumerDependencyDetails{Binding: binding, Mapping: request.Input.Digest, Locks: locks})
}

func (a *SummaryAuthority) ValidateStoppedCallInTx(ctx context.Context, tx f.Tx, request mc.StoppedCallObservationRequest, plan mc.ConsumerDependencies) error {
	w, err := a.stoppedOwner(ctx, request)
	if err != nil {
		return err
	}
	binding, err := mc.StoppedCallObservationBinding(request)
	if err != nil || !plan.Matches(a.issuer, binding, request.Input.Digest) {
		return fault(f.Forbidden)
	}
	if err = a.store.RequireHeldLocks(ctx, tx, summaryLocks(w.owner.input.ProjectID, w.owner.input.MeetingID)); err != nil {
		return portError(err)
	}
	if err = a.guard.RequireCurrentProcessInTx(ctx, tx, a.process); err != nil {
		return portError(err)
	}
	x, err := a.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	row, err := loadSummaryCall(ctx, x, request.CallID)
	if err != nil {
		return err
	}
	if row.input.ProjectID != request.Consumer.ProjectID || row.input.MeetingID.String() != request.Consumer.MeetingID || row.input.GenerationID.String() != request.Consumer.OperationID || row.purpose != request.Consumer.Purpose || row.digest != request.Input.Digest || request.Input.SchemaVersion != 1 || row.snapshot == nil || *row.snapshot != request.SnapshotID.String() {
		return fault(f.ConfirmationStale)
	}
	// A cancelled/deleting Meeting still owns cleanup. This deliberately does
	// not call the preparing gate and grants no Resolve or Begin capability.
	return nil
}

// Existing rows are not admission authority, including pre-admission rows
// without a Model call. Only this process's original fresh prepare may begin;
// retained warm owners recover separately through their original handles.
func (r summaryCallRecord) mayBegin() bool {
	return r.fresh && r.status == "prepared" && !r.unknown && r.returned == nil && r.retired == nil
}

func (m *SummaryModel) recoverStoppedCall(ctx context.Context, call *summaryModelCall, row summaryCallRecord) (SummaryCandidate, error) {
	if nilPort(m.deps.Stopped) {
		return SummaryCandidate{}, unknownSummary()
	}
	if row.snapshot == nil {
		return m.recoverStoppedResolution(ctx, call, row)
	}
	snapshot, err := f.ParseID[mc.Snapshot](*row.snapshot)
	if err != nil {
		return SummaryCandidate{}, fault(f.InternalError)
	}
	request := mc.StoppedCallObservationRequest{
		Actor: call.actor, CallID: row.call, SnapshotID: snapshot,
		Consumer: mc.Consumer{Kind: mc.MeetingConsumer, ProjectID: row.input.ProjectID, MeetingID: row.input.MeetingID.String(), OperationID: row.input.GenerationID.String(), Purpose: row.purpose},
		Input:    mc.InputIdentity{OperationID: row.input.GenerationID.String(), Digest: row.digest, SchemaVersion: 1},
	}
	w := &summaryStoppedOwner{owner: call.owner, request: request}
	cleanup, cancel := context.WithTimeout(context.WithValue(ctx, summaryStoppedKey{}, w), 5*time.Second)
	defer cancel()
	observed, err := m.deps.Stopped.LookupStoppedCall(cleanup, request)
	if err != nil {
		return SummaryCandidate{}, err
	}
	if observed.Validate() != nil {
		return SummaryCandidate{}, unknownSummary()
	}
	if !observed.Found {
		return m.recoverStoppedResolution(ctx, call, row)
	}
	if !observed.Retired || observed.Attempt.CallID != row.call {
		return SummaryCandidate{}, unknownSummary()
	}
	// ModelCallOwner Retired includes the exact original resolution lease's
	// released fact. Never infer it from call absence or a local process ID.
	var settled summaryCallRecord
	err = m.tx(cleanup, call.owner.input, "stopped-call", nil, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		if e := m.deps.Authority.guard.RequireCurrentProcessInTx(ctx, tx, m.deps.Authority.process); e != nil {
			return portError(e)
		}
		current, e := loadSummaryCall(ctx, x, row.call)
		if e != nil {
			return e
		}
		if current.digest != row.digest || current.snapshot == nil || *current.snapshot != snapshot.String() || current.input.GenerationID != row.input.GenerationID {
			return fault(f.ConfirmationStale)
		}
		if current.retired == nil {
			at := now()
			// Preserve a domain result already durably returned. If no result
			// survived, record failure; the stopped Model response is not replayed.
			_, e = x.Exec(ctx, `UPDATE agenteam_meeting.summary_model_calls SET status=CASE WHEN returned_at IS NULL THEN 'failed' ELSE status END,returned_at=COALESCE(returned_at,$2),terminal_version=1,retired_at=$2,error_code=CASE WHEN returned_at IS NULL THEN $3 ELSE error_code END WHERE call_id=$1 AND retired_at IS NULL`, row.call.String(), at, string(f.DependencyUnavailable))
			if e != nil {
				return portError(e)
			}
		}
		settled, e = loadSummaryCall(ctx, x, row.call)
		return e
	})
	if err != nil {
		return SummaryCandidate{}, err
	}
	call.done, call.dispatchUnknown = true, false
	call.owner.active = false
	candidate, err := m.completedCandidate(ctx, call.owner.input, settled)
	call.failure = err
	if err == nil {
		call.candidate = &candidate
	}
	return candidate, err
}

var _ mc.StoppedCallAuthority = (*SummaryAuthority)(nil)
