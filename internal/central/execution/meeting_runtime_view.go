package execution

import (
	"bytes"
	"context"
	"errors"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/jackc/pgx/v5"
)

// ReadMeetingRuntimeInTx borrows the original Meeting reader and complete lock
// union. It does not enter a Task scope or create an Execution/transport owner.
func (r *MeetingExecutionReader) ReadMeetingRuntimeInTx(ctx context.Context, tx f.Tx, project i.ProjectID, association ec.AssociatedMeeting) (ec.MeetingRuntimeResult, error) {
	value, err := r.ReadMeetingExecutionInTx(ctx, tx, project, association)
	if err != nil {
		return ec.MeetingRuntimeResult{}, err
	}
	out := ec.MeetingRuntimeResult{Execution: value, Availability: ec.RuntimePending}
	if !value.Summary.Status.Terminal() {
		return out, out.ValidateProjection()
	}
	x, err := r.store.InTx(tx)
	if err != nil {
		return ec.MeetingRuntimeResult{}, portError(err)
	}
	var raw []byte
	var digest, snapshot, round, status string
	var version int64
	var completed time.Time
	err = x.QueryRow(ctx, `SELECT v.content,v.digest,v.snapshot_id::text,v.round_id::text,v.terminal_status,v.terminal_version,v.completed_at FROM agenteam_execution.runtime_snapshots v JOIN agenteam_execution.rounds r ON r.id=v.round_id AND r.execution_id=v.execution_id AND r.terminal_status=v.terminal_status AND r.terminal_version=v.terminal_version AND r.finished_at=v.completed_at WHERE v.execution_id=$1 AND v.project_id=$2`, association.ExecutionID.String(), project.String()).Scan(&raw, &digest, &snapshot, &round, &status, &version, &completed)
	if errors.Is(err, pgx.ErrNoRows) && value.Summary.Status != ec.Succeeded {
		out.Availability = ec.RuntimeNotRecorded
		return out, out.ValidateProjection()
	}
	if err != nil {
		return ec.MeetingRuntimeResult{}, portError(err)
	}
	out.Snapshot, err = decodeMeetingRuntimeSnapshot(value, raw, digest, snapshot, round, status, version, completed)
	if err != nil {
		return ec.MeetingRuntimeResult{}, err
	}
	out.Availability = ec.RuntimeAvailable
	if err := out.ValidateProjection(); err != nil {
		return ec.MeetingRuntimeResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return ec.MeetingRuntimeResult{}, err
	}
	return out, nil
}

func decodeMeetingRuntimeSnapshot(result ec.MeetingExecutionResult, raw []byte, digest, snapshotID, roundID, status string, version int64, completed time.Time) (*ec.RuntimeSnapshot, error) {
	e := result.Summary
	if !e.Status.Terminal() || e.SnapshotID == nil || e.CompletedAt == nil || len(raw) > ec.MaxRuntimeViewBytes || digest != string(ec.TriggerInputDigest(raw)) || snapshotID != e.SnapshotID.String() || status != string(e.Status) || version != int64(e.Version) || !completed.Equal(e.CompletedAt.Time()) {
		return nil, unavailable(nil)
	}
	round, err := f.ParseID[ec.Round](roundID)
	if err != nil {
		return nil, unavailable(nil)
	}
	var snapshot ec.RuntimeSnapshot
	if err := decodeDirectTextJSON(raw, &snapshot); err != nil {
		return nil, unavailable(err)
	}
	canonical, err := runtimeSnapshotBytes(&snapshot)
	if err != nil || !bytes.Equal(raw, canonical) || len(snapshot.Items) == 1 && snapshot.Items[0].ID != round {
		return nil, unavailable(nil)
	}
	view := ec.MeetingRuntimeResult{Execution: result, Availability: ec.RuntimeAvailable, Snapshot: &snapshot}
	if err := view.ValidateProjection(); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

var _ ec.MeetingRuntimeReader = (*MeetingExecutionReader)(nil)
