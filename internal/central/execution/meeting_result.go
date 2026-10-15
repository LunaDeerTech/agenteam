package execution

import (
	"bytes"
	"context"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"time"
	"unicode/utf8"
)

type MeetingExecutionReader struct {
	store     Store
	authority ec.MeetingResultAuthority
}

func NewMeetingExecutionReader(store Store, authority ec.MeetingResultAuthority) (*MeetingExecutionReader, error) {
	if nilPort(store) || nilPort(authority) {
		return nil, fault(f.DependencyUnbound)
	}
	return &MeetingExecutionReader{store, authority}, nil
}

// This synchronous read borrows Meeting's original live transaction and private
// runtime witness. It creates no goroutine, transaction, process owner or I/O.
func (r *MeetingExecutionReader) ReadMeetingExecutionInTx(ctx context.Context, tx f.Tx, project i.ProjectID, m ec.AssociatedMeeting) (ec.MeetingExecutionResult, error) {
	if r == nil || nilPort(r.store) || nilPort(r.authority) {
		return ec.MeetingExecutionResult{}, fault(f.DependencyUnbound)
	}
	if ctx == nil || !tx.Valid() {
		return ec.MeetingExecutionResult{}, invalid()
	}
	locks, err := ec.MeetingExecutionReadLocks(project, m)
	if err != nil {
		return ec.MeetingExecutionResult{}, err
	}
	if err = ctx.Err(); err != nil {
		return ec.MeetingExecutionResult{}, err
	}
	x, err := r.store.InTx(tx)
	if err != nil {
		return ec.MeetingExecutionResult{}, portError(err)
	}
	if err = r.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return ec.MeetingExecutionResult{}, portError(err)
	}
	if err = r.authority.RequireBindingInTx(ctx, tx, r.store); err != nil {
		return ec.MeetingExecutionResult{}, portError(err)
	}
	if err = r.authority.RequireMeetingResultInTx(ctx, tx, project, m); err != nil {
		return ec.MeetingExecutionResult{}, portError(err)
	}
	row, err := loadLaunch(ctx, x, ec.LaunchLookupKey{ProjectID: project, AgentID: m.AgentID, IdempotencyKey: m.Key})
	if err != nil {
		return ec.MeetingExecutionResult{}, err
	}
	if row == nil {
		return ec.MeetingExecutionResult{}, fault(f.NotFound)
	}
	identity := associatedIdentity{AssociatedDispatch: ec.AssociatedDispatch{ExecutionID: m.ExecutionID, AgentID: m.AgentID, Key: m.Key, Digest: m.Digest}, meeting: m}
	if row.digest != m.Digest || row.summary.ID != m.ExecutionID || row.summary.ProjectID != project || row.summary.AgentID != m.AgentID || !identity.matchesLaunch(row.launch) {
		return ec.MeetingExecutionResult{}, fault(f.ConfirmationStale)
	}
	out := ec.MeetingExecutionResult{Summary: row.summary.Clone()}
	var waitingReason, waitingReference *string
	if err = x.QueryRow(ctx, `SELECT waiting_reason,waiting_reference::text FROM agenteam_execution.executions WHERE id=$1 AND project_id=$2`, m.ExecutionID.String(), project.String()).Scan(&waitingReason, &waitingReference); err != nil {
		return ec.MeetingExecutionResult{}, portError(err)
	}
	if out.Summary.Status == ec.Waiting {
		if waitingReason == nil || waitingReference == nil || (*waitingReason != "approval" && *waitingReason != "decision") {
			return ec.MeetingExecutionResult{}, unavailable(nil)
		}
		if _, err = f.ParseID[struct{}](*waitingReference); err != nil {
			return ec.MeetingExecutionResult{}, unavailable(nil)
		}
		out.WaitingReason, out.WaitingReference = waitingReason, waitingReference
	} else if waitingReason != nil || waitingReference != nil {
		return ec.MeetingExecutionResult{}, unavailable(nil)
	}
	if out.Summary.Status == ec.Succeeded {
		var raw []byte
		var digest, snapshot, round, status string
		var version int64
		var completed time.Time
		err = x.QueryRow(ctx, `SELECT v.content,v.digest,v.snapshot_id::text,v.round_id::text,v.terminal_status,v.terminal_version,v.completed_at FROM agenteam_execution.runtime_snapshots v JOIN agenteam_execution.rounds r ON r.id=v.round_id AND r.execution_id=v.execution_id AND r.terminal_status=v.terminal_status AND r.terminal_version=v.terminal_version AND r.finished_at=v.completed_at WHERE v.execution_id=$1 AND v.project_id=$2 AND r.terminal_reason='completed'`, m.ExecutionID.String(), project.String()).Scan(&raw, &digest, &snapshot, &round, &status, &version, &completed)
		if err != nil {
			return ec.MeetingExecutionResult{}, portError(err)
		}
		final, err := meetingFinalText(out.Summary, raw, digest, snapshot, round, status, version, completed)
		if err != nil {
			return ec.MeetingExecutionResult{}, err
		}
		out.FinalText = &final
	}
	if err = ctx.Err(); err != nil {
		return ec.MeetingExecutionResult{}, err
	}
	return out, nil
}
func meetingFinalText(summary ec.Summary, raw []byte, digest, snapshotID, roundID, status string, version int64, at time.Time) (string, error) {
	if summary.Status != ec.Succeeded || summary.SnapshotID == nil || summary.StartedAt == nil || summary.CompletedAt == nil || len(raw) > ec.MaxRuntimeViewBytes || digest != string(ec.TriggerInputDigest(raw)) || snapshotID != summary.SnapshotID.String() || status != string(summary.Status) || version != int64(summary.Version) || !at.Equal(summary.CompletedAt.Time()) {
		return "", unavailable(nil)
	}
	var snapshot ec.RuntimeSnapshot
	if err := decodeDirectTextJSON(raw, &snapshot); err != nil {
		return "", unavailable(err)
	}
	canonical, err := runtimeSnapshotBytes(&snapshot)
	if err != nil || !bytes.Equal(canonical, raw) {
		return "", unavailable(nil)
	}
	if (snapshot.Schema != ec.DurableFinalTextV1 && snapshot.Schema != ec.DurableFinalTextV2) || snapshot.Generation != *summary.SnapshotID || snapshot.ExecutionVersion != summary.Version || snapshot.IncludedUpdateProgress != 1 || len(snapshot.Items) != 1 {
		return "", unavailable(nil)
	}
	item := snapshot.Items[0]
	if item.ID.Validate() != nil || item.ID.String() != roundID || item.ExecutionID != summary.ID || item.Seq != 1 || item.Type != "text" || item.Status != "completed" || item.Revision != 1 || item.StartedAt.Validate() != nil || item.StartedAt.Time().Before(summary.StartedAt.Time()) || snapshot.Schema == ec.DurableFinalTextV1 && item.StartedAt != *summary.StartedAt || item.UpdatedAt != *summary.CompletedAt || item.CompletedAt != *summary.CompletedAt || item.CompletedAt.Time().Before(item.StartedAt.Time()) || item.SourceReference.Kind != "execution_round" || item.SourceReference.ID != item.ID || len(item.Data.Content) > ec.MaxRuntimeTextBytes || !utf8.ValidString(item.Data.Content) {
		return "", unavailable(nil)
	}
	return item.Data.Content, nil
}

var _ ec.MeetingResultReader = (*MeetingExecutionReader)(nil)
