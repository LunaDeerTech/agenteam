package meeting

import (
	"context"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type summaryStoppedResolutionKey struct{}
type summaryStoppedResolutionOwner struct {
	owner   *summaryOwner
	request mc.StoppedResolutionRequest
}

func (a *SummaryAuthority) stoppedResolutionOwner(ctx context.Context, request mc.StoppedResolutionRequest) (*summaryStoppedResolutionOwner, error) {
	if a == nil || ctx == nil || request.Validate() != nil {
		return nil, fault(f.Forbidden)
	}
	w, _ := ctx.Value(summaryStoppedResolutionKey{}).(*summaryStoppedResolutionOwner)
	if w == nil || w.owner == nil || !w.owner.active || w.owner.authority != a {
		return nil, fault(f.Forbidden)
	}
	want, err := mc.StoppedResolutionBinding(w.request)
	got, actual := mc.StoppedResolutionBinding(request)
	d := request.Resolve.Actor.Details()
	consumer := request.Resolve.Consumer
	if err != nil || actual != nil || want != got || request.CallID != w.owner.call || d.Kind != i.Service || d.ServiceName != i.MeetingRuntime || d.ProjectID != w.owner.input.ProjectID.String() || d.CauseRef != w.owner.input.GenerationID.String() || consumer.Kind != mc.MeetingConsumer || consumer.ProjectID != w.owner.input.ProjectID || consumer.MeetingID != w.owner.input.MeetingID.String() || consumer.OperationID != w.owner.input.GenerationID.String() {
		return nil, fault(f.Forbidden)
	}
	return w, nil
}

func (a *SummaryAuthority) DiscoverStoppedResolution(ctx context.Context, request mc.StoppedResolutionRequest) (mc.ConsumerDependencies, error) {
	w, err := a.stoppedResolutionOwner(ctx, request)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	binding, err := mc.StoppedResolutionBinding(request)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	locks, err := normalize(summaryLocks(w.owner.input.ProjectID, w.owner.input.MeetingID))
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	return mc.NewConsumerDependencies(a.issuer, mc.ConsumerDependencyDetails{Binding: binding, Mapping: request.Input.Digest, Locks: locks})
}

func (a *SummaryAuthority) ValidateStoppedResolutionInTx(ctx context.Context, tx f.Tx, request mc.StoppedResolutionRequest, plan mc.ConsumerDependencies) error {
	w, err := a.stoppedResolutionOwner(ctx, request)
	if err != nil {
		return err
	}
	binding, err := mc.StoppedResolutionBinding(request)
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
	return matchStoppedSummaryResolve(row, request)
}

func matchStoppedSummaryResolve(row summaryCallRecord, request mc.StoppedResolutionRequest) error {
	original, err := decodeSummaryResolve(request.Resolve.Actor, row)
	if err != nil {
		return err
	}
	want, err := mc.ResolveBinding(original)
	got, actual := mc.ResolveBinding(request.Resolve)
	if err != nil || actual != nil || want != got || row.call != request.CallID || row.preparedProcess == nil || *row.preparedProcess != request.PreparedProcessID.String() || request.Input.OperationID != row.input.GenerationID.String() || request.Input.Digest != row.digest || request.Input.SchemaVersion != 1 {
		return fault(f.ConfirmationStale)
	}
	// Cleanup may observe a cancelled/deleting Meeting, but grants no admission.
	return nil
}

func (m *SummaryModel) recoverStoppedResolution(ctx context.Context, call *summaryModelCall, row summaryCallRecord) (SummaryCandidate, error) {
	observer, ok := m.deps.Stopped.(mc.StoppedResolutionObserver)
	if !ok || nilPort(observer) || call.stoppedResolutionChecked || row.preparedProcess == nil {
		return SummaryCandidate{}, unknownSummary()
	}
	resolve, err := decodeSummaryResolve(call.actor, row)
	if err != nil {
		return SummaryCandidate{}, err
	}
	process, err := f.ParseID[oc.Process](*row.preparedProcess)
	if err != nil {
		return SummaryCandidate{}, fault(f.InternalError)
	}
	request := mc.StoppedResolutionRequest{Resolve: resolve, CallID: row.call, Input: mc.InputIdentity{OperationID: row.input.GenerationID.String(), Digest: row.digest, SchemaVersion: 1}, PreparedProcessID: process}
	w := &summaryStoppedResolutionOwner{owner: call.owner, request: request}
	cleanup, cancel := context.WithTimeout(context.WithValue(ctx, summaryStoppedResolutionKey{}, w), 5*time.Second)
	defer cancel()
	observed, err := observer.LookupStoppedResolution(cleanup, request)
	if err != nil {
		return SummaryCandidate{}, err
	}
	if observed.Validate() != nil || !observed.Settled {
		return SummaryCandidate{}, unknownSummary()
	}
	// Never clear the dispatch-unknown gate before the formal old-process and
	// exact original resolution observation, including when no Model row exists.
	if observed.Model != nil {
		if !observed.Model.Consumer.Equal(resolve.Consumer) || observed.Model.LeaseOwner.Details() != resolve.LeaseOwner.Details() {
			return SummaryCandidate{}, fault(f.ConfirmationStale)
		}
		if err = call.owner.bindResolved(*observed.Model); err != nil {
			return SummaryCandidate{}, err
		}
		model := observed.Model.Clone()
		call.resolved = &model
	}
	var pin bool
	err = m.tx(cleanup, call.owner.input, "stopped-resolution", nil, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		if e := m.deps.Authority.guard.RequireCurrentProcessInTx(ctx, tx, m.deps.Authority.process); e != nil {
			return portError(e)
		}
		current, e := loadSummaryCall(ctx, x, row.call)
		if e != nil {
			return e
		}
		if e = matchStoppedSummaryResolve(current, request); e != nil {
			return e
		}
		if observed.Model == nil {
			if current.snapshot != nil {
				return fault(f.ConfirmationStale)
			}
			if _, e = x.Exec(ctx, `UPDATE agenteam_meeting.summary_model_calls SET resolution_unknown=false WHERE call_id=$1 AND snapshot_id IS NULL`, row.call.String()); e != nil {
				return portError(e)
			}
		} else {
			snapshot := observed.Model.Snapshot.ID.String()
			if current.snapshot != nil && *current.snapshot != snapshot {
				return fault(f.ConfirmationStale)
			}
			if _, e = x.Exec(ctx, `UPDATE agenteam_meeting.summary_model_calls SET snapshot_id=COALESCE(snapshot_id,$2),resolution_unknown=false WHERE call_id=$1`, row.call.String(), snapshot); e != nil {
				return portError(e)
			}
			current.snapshot = &snapshot
			if !observed.CallFound && current.returned == nil {
				var retained *string
				e = x.QueryRow(ctx, `SELECT g.snapshot_id,g.status='generating' AND NOT t.cancel_requested AND NOT s.deleting FROM agenteam_meeting.summary_generations g JOIN agenteam_meeting.turns t ON t.id=g.turn_id JOIN agenteam_meeting.sessions s ON s.id=g.meeting_id WHERE g.id=$1`, row.input.GenerationID.String()).Scan(&retained, &pin)
				if e != nil {
					return portError(e)
				}
				if retained != nil && *retained != snapshot {
					return fault(f.ConfirmationStale)
				}
				pin = pin && retained == nil
			}
		}
		row = current
		return nil
	})
	if err != nil {
		return SummaryCandidate{}, err
	}
	call.resolve = &resolve
	call.stoppedResolutionChecked = true
	if observed.CallFound {
		return m.recoverStoppedCall(cleanup, call, row)
	}
	// If Resolve committed before the process stopped, preserve its exact
	// snapshot for a later explicit finite retry while its lease still exists.
	// Cancelled generations only retire that lease; they cannot admit a retry.
	if pin {
		if err = m.pinSummarySnapshot(cleanup, call); err != nil {
			call.stoppedResolutionChecked = false
			return SummaryCandidate{}, err
		}
	}
	call.stoppedSnapshotSettled = true
	call.dispatchUnknown, call.resolutionUnknown = false, false
	call.failure = fault(f.DependencyUnavailable)
	if err = m.finishCall(context.WithValue(cleanup, summaryOwnerKey{}, call.owner), call, false); err != nil {
		return SummaryCandidate{}, err
	}
	return SummaryCandidate{}, call.failure
}

var _ mc.StoppedResolutionAuthority = (*SummaryAuthority)(nil)
