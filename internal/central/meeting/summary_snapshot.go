package meeting

import (
	"context"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type summaryPin struct {
	witness *summarySnapshotWitness
	plan    mc.ServingSnapshotRetentionPlan
	unknown bool
	applied bool
}

func (m *SummaryModel) resolutionRequest(ctx context.Context, call *summaryModelCall, row summaryCallRecord) (mc.ResolveRequest, error) {
	if err := ctx.Err(); err != nil {
		return mc.ResolveRequest{}, err
	}
	return decodeSummaryResolve(call.actor, row)
}

func (m *SummaryModel) pinSummarySnapshot(ctx context.Context, call *summaryModelCall) error {
	if nilPort(m.deps.Retention) || call.resolved == nil {
		return fault(f.DependencyUnbound)
	}
	if call.pin == nil {
		request := mc.ServingSnapshotRetentionRequest{Actor: call.actor,
			Owner:      mc.ServingSnapshotOwner{Kind: mc.MeetingConsumer, ProjectID: call.owner.input.ProjectID, GenerationID: call.owner.input.GenerationID.String()},
			SnapshotID: call.resolved.Snapshot.ID, OwnerVersion: 1, Action: mc.RetainServingSnapshot}
		w := &summarySnapshotWitness{model: m, input: call.owner.input, request: request, call: call, active: true}
		plan, err := m.deps.Retention.DiscoverServingSnapshotRetention(context.WithValue(ctx, summarySnapshotKey{}, w), request)
		if err != nil {
			w.active = false
			return err
		}
		call.pin = &summaryPin{witness: w, plan: plan}
	}
	pin := call.pin
	if pin.applied {
		return nil
	}
	ctx = context.WithValue(ctx, summarySnapshotKey{}, pin.witness)
	if pin.unknown {
		confirmed := false
		err := m.tx(ctx, call.owner.input, "confirm-snapshot-pin", pin.plan.RequiredLocks(), func(ctx context.Context, tx f.Tx, _ postgres.SQLExecutor) error {
			var e error
			confirmed, e = m.deps.Retention.ServingSnapshotRetentionAppliedInTx(ctx, tx, pin.witness.request, pin.plan)
			return e
		})
		if err != nil {
			return err
		}
		if !confirmed {
			return unknownSummary()
		}
		pin.unknown, pin.applied = false, true
		return nil
	}
	err := m.tx(ctx, call.owner.input, "pin-snapshot", pin.plan.RequiredLocks(), func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		at := now()
		tag, err := x.Exec(ctx, `UPDATE agenteam_meeting.summary_generations SET snapshot_id=COALESCE(snapshot_id,$2),retained_at=COALESCE(retained_at,$3) WHERE id=$1 AND status='generating' AND released_at IS NULL AND (snapshot_id IS NULL OR snapshot_id=$2)`, call.owner.input.GenerationID.String(), call.resolved.Snapshot.ID.String(), at)
		if err != nil {
			return portError(err)
		}
		if tag.RowsAffected() != 1 {
			return fault(f.ConfirmationStale)
		}
		tag, err = x.Exec(ctx, `UPDATE agenteam_meeting.summary_model_calls SET status='resolved',snapshot_id=$2,resolution_unknown=false WHERE call_id=$1 AND status IN('resolving','resolved') AND (snapshot_id IS NULL OR snapshot_id=$2)`, call.owner.call.String(), call.resolved.Snapshot.ID.String())
		if err != nil {
			return portError(err)
		}
		if tag.RowsAffected() != 1 {
			return fault(f.ConfirmationStale)
		}
		return m.deps.Retention.ApplyServingSnapshotRetentionInTx(ctx, tx, pin.witness.request, pin.plan)
	})
	pin.unknown = isUnknown(err)
	pin.applied = err == nil
	return err
}

func validateSummaryResolveSource(ctx context.Context, x postgres.SQLExecutor, r mc.ConsumerRequest, row summaryCallRecord) error {
	resolve := r.Resolve
	if resolve == nil || resolve.Purpose != row.purpose || resolve.ModelRef != nil {
		return fault(f.Forbidden)
	}
	original, err := decodeSummaryResolve(resolve.Actor, row)
	if err != nil {
		return err
	}
	want, err := mc.ResolveBinding(original)
	got, actual := mc.ResolveBinding(*resolve)
	if err != nil || actual != nil || got != want {
		return fault(f.ConfirmationStale)
	}
	var snapshot *string
	var released *time.Time
	if err := x.QueryRow(ctx, `SELECT snapshot_id,released_at FROM agenteam_meeting.summary_generations WHERE id=$1`, row.input.GenerationID.String()).Scan(&snapshot, &released); err != nil {
		return portError(err)
	}
	if released != nil {
		return fault(f.InvalidState)
	}
	switch resolve.Source {
	case mc.CurrentSelectionSource:
		if row.attempt != 1 || resolve.Selection == nil || resolve.Selection.Kind != "platform" || resolve.Selection.Selector != mc.MeetingSummarySelector || snapshot != nil && *snapshot != r.SnapshotID.String() {
			return fault(f.Forbidden)
		}
	case mc.ServingSnapshotSource:
		if resolve.Selection != nil || resolve.ServingSnapshotID == nil || snapshot == nil || *snapshot != resolve.ServingSnapshotID.String() || *snapshot != r.SnapshotID.String() || resolve.ServingGenerationID != row.input.GenerationID.String() {
			return fault(f.Forbidden)
		}
	default:
		return fault(f.Forbidden)
	}
	return nil
}
