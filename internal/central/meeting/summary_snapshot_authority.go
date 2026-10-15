package meeting

import (
	"context"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

type summarySnapshotKey struct{}

// This private witness belongs to one original pin or terminal transition. It
// remains live for read-only confirmation after an uncertain commit.
type summarySnapshotWitness struct {
	model   *SummaryModel
	input   SummaryInput
	request mc.ServingSnapshotRetentionRequest
	call    *summaryModelCall
	active  bool
}

func (a *SummaryAuthority) snapshotOwner(ctx context.Context, request mc.ServingSnapshotRetentionRequest) (*summarySnapshotWitness, error) {
	if a == nil || ctx == nil || request.Validate() != nil {
		return nil, fault(f.Forbidden)
	}
	w, _ := ctx.Value(summarySnapshotKey{}).(*summarySnapshotWitness)
	if w == nil || !w.active || w.model == nil || w.model.deps.Authority != a || request.Owner.Kind != mc.MeetingConsumer || request.Owner.AgentID != nil || request.Owner.ProjectID != w.input.ProjectID || request.Owner.GenerationID != w.input.GenerationID.String() {
		return nil, fault(f.Forbidden)
	}
	want, err := mc.ServingSnapshotRetentionBinding(w.request)
	got, actual := mc.ServingSnapshotRetentionBinding(request)
	if err != nil || actual != nil || want != got {
		return nil, fault(f.Forbidden)
	}
	if request.Action == mc.RetainServingSnapshot && (w.call == nil || w.call.done || w.call.owner == nil || !w.call.owner.active || w.call.owner.authority != a || w.call.owner.input.GenerationID != w.input.GenerationID || w.call.resolved == nil || w.call.resolved.Snapshot.ID != request.SnapshotID) {
		return nil, fault(f.Forbidden)
	}
	return w, nil
}

func (a *SummaryAuthority) DiscoverServingSnapshot(ctx context.Context, request mc.ServingSnapshotRetentionRequest) (mc.ConsumerDependencies, error) {
	w, err := a.snapshotOwner(ctx, request)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	binding, err := mc.ServingSnapshotRetentionBinding(request)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	locks, err := normalize(summaryLocks(w.input.ProjectID, w.input.MeetingID))
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	return mc.NewConsumerDependencies(a.issuer, mc.ConsumerDependencyDetails{Binding: binding, Mapping: w.input.InputDigest, Locks: locks})
}

func (a *SummaryAuthority) ValidateServingSnapshotInTx(ctx context.Context, tx f.Tx, request mc.ServingSnapshotRetentionRequest, plan mc.ConsumerDependencies) error {
	return a.validateSummarySnapshot(ctx, tx, request, plan, false)
}
func (a *SummaryAuthority) ValidateServingSnapshotObservationInTx(ctx context.Context, tx f.Tx, request mc.ServingSnapshotRetentionRequest, plan mc.ConsumerDependencies) error {
	return a.validateSummarySnapshot(ctx, tx, request, plan, true)
}
func (a *SummaryAuthority) validateSummarySnapshot(ctx context.Context, tx f.Tx, request mc.ServingSnapshotRetentionRequest, plan mc.ConsumerDependencies, observation bool) error {
	w, err := a.snapshotOwner(ctx, request)
	if err != nil {
		return err
	}
	binding, err := mc.ServingSnapshotRetentionBinding(request)
	if err != nil || !plan.Matches(a.issuer, binding, w.input.InputDigest) {
		return fault(f.Forbidden)
	}
	if err = a.store.RequireHeldLocks(ctx, tx, summaryLocks(w.input.ProjectID, w.input.MeetingID)); err != nil {
		return portError(err)
	}
	if err = a.guard.RequireCurrentProcessInTx(ctx, tx, a.process); err != nil {
		return portError(err)
	}
	x, err := a.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	var project, meeting, turn, state, turnState, inputDigest string
	var snapshot *string
	var retained, released *time.Time
	var version, previous int64
	var attempt int
	var initial, cancel, deleting bool
	err = x.QueryRow(ctx, `SELECT s.project_id,g.meeting_id,g.turn_id,g.status,t.status,g.input_digest,g.snapshot_id,g.retention_version,g.retained_at,g.released_at,g.attempt,g.expected_summary_version,g.initial,t.cancel_requested,s.deleting FROM agenteam_meeting.summary_generations g JOIN agenteam_meeting.turns t ON t.id=g.turn_id JOIN agenteam_meeting.sessions s ON s.id=g.meeting_id WHERE g.id=$1`, w.input.GenerationID.String()).Scan(&project, &meeting, &turn, &state, &turnState, &inputDigest, &snapshot, &version, &retained, &released, &attempt, &previous, &initial, &cancel, &deleting)
	if err != nil {
		return portError(err)
	}
	if project != w.input.ProjectID.String() || meeting != w.input.MeetingID.String() || turn != w.input.TurnID.String() || inputDigest != w.input.InputDigest.String() || previous != w.input.PreviousVersion || initial != w.input.Initial {
		return fault(f.ConfirmationStale)
	}
	if request.Action == mc.RetainServingSnapshot {
		before := observation && snapshot == nil && retained == nil && released == nil && version == 1
		if request.OwnerVersion != 1 || state != "generating" || !before && (snapshot == nil || *snapshot != request.SnapshotID.String() || retained == nil || released != nil || version != 1) {
			return fault(f.InvalidState)
		}
		if !observation {
			if _, err = a.projects.RequirePreparingProjectInTx(ctx, tx, w.input.ProjectID); err != nil {
				return portError(err)
			}
			if turnState != "finalizing" || cancel || deleting {
				return fault(f.InvalidState)
			}
		}
		row, err := loadSummaryCall(ctx, x, w.call.owner.call)
		if err != nil {
			return err
		}
		if row.input.GenerationID != w.input.GenerationID || row.input.ProjectID != w.input.ProjectID || row.input.MeetingID != w.input.MeetingID || row.returned != nil || row.retired != nil || !before && (row.snapshot == nil || *row.snapshot != request.SnapshotID.String()) {
			return fault(f.Forbidden)
		}
		return nil
	}
	if request.OwnerVersion != 2 || snapshot == nil || *snapshot != request.SnapshotID.String() || retained == nil {
		return fault(f.ConfirmationStale)
	}
	before := observation && released == nil && version == 1
	terminal := state == "published" || state == "cancelled" || state == "failed" && attempt == 3
	if !before && (!terminal || released == nil || version != 2) {
		return fault(f.InvalidState)
	}
	var active bool
	if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_meeting.summary_model_calls WHERE generation_id=$1 AND retired_at IS NULL)`, w.input.GenerationID.String()).Scan(&active); err != nil {
		return portError(err)
	}
	if active {
		return fault(f.ResourceBusy)
	}
	return nil
}

var _ mc.ServingSnapshotAuthority = (*SummaryAuthority)(nil)
