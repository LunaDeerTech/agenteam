package contract

import (
	"context"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// MeetingRuntimeResult contains the same original Execution facts and durable
// terminal projection as the source-bound reader. Pending is not a live stream.
// The caller must borrow its original live transaction and private authority.
type MeetingRuntimeResult struct {
	Execution    MeetingExecutionResult
	Availability RuntimeAvailability
	Snapshot     *RuntimeSnapshot
}

type MeetingRuntimeReader interface {
	ReadMeetingRuntimeInTx(context.Context, f.Tx, i.ProjectID, AssociatedMeeting) (MeetingRuntimeResult, error)
}

func (v MeetingRuntimeResult) ValidateProjection() error {
	e := v.Execution.Summary
	invalid := func() error { return f.NewFault(f.DependencyUnavailable, f.NotStarted) }
	if e.ID.Validate() != nil || e.ProjectID.Validate() != nil || e.AgentID.Validate() != nil || e.Trigger.Validate(e.Purpose) != nil || e.Trigger.Kind != "meeting" || e.Purpose != "meeting/response" || e.Version.Validate() != nil || !e.Status.Valid() || e.CreatedAt.Validate() != nil || e.Status.Terminal() != (e.CompletedAt != nil) {
		return invalid()
	}
	if (e.Status == Running || e.Status == Waiting || e.Status == Succeeded) && e.StartedAt == nil {
		return invalid()
	}
	if v.Availability == RuntimePending && !e.Status.Terminal() && v.Snapshot == nil && v.Execution.FinalText == nil {
		return nil
	}
	if e.CompletedAt == nil || e.CompletedAt.Validate() != nil || e.CompletedAt.Time().Before(e.CreatedAt.Time()) {
		return invalid()
	}
	if v.Availability == RuntimeNotRecorded && e.Status.Terminal() && e.Status != Succeeded && v.Snapshot == nil && v.Execution.FinalText == nil {
		return nil
	}
	s := v.Snapshot
	if v.Availability != RuntimeAvailable || !e.Status.Terminal() || s == nil || e.SnapshotID == nil || e.StartedAt == nil || e.StartedAt.Validate() != nil || e.StartedAt.Time().Before(e.CreatedAt.Time()) || e.CompletedAt.Time().Before(e.StartedAt.Time()) || (s.Schema != DurableFinalTextV1 && s.Schema != DurableFinalTextV2) || s.Generation.Validate() != nil || s.Generation != *e.SnapshotID || s.ExecutionVersion != e.Version || s.IncludedUpdateProgress != 1 || s.Items == nil {
		return invalid()
	}
	if e.Status != Succeeded {
		if (e.Status == Failed || e.Status == Cancelled) && len(s.Items) == 0 && v.Execution.FinalText == nil {
			return nil
		}
		return invalid()
	}
	if len(s.Items) != 1 || v.Execution.FinalText == nil {
		return invalid()
	}
	item := s.Items[0]
	if item.ID.Validate() != nil || item.ExecutionID != e.ID || item.Seq != 1 || item.Type != "text" || item.Status != "completed" || item.Revision != 1 || item.StartedAt.Validate() != nil || item.StartedAt.Time().Before(e.StartedAt.Time()) || s.Schema == DurableFinalTextV1 && item.StartedAt != *e.StartedAt || item.UpdatedAt != *e.CompletedAt || item.CompletedAt != *e.CompletedAt || item.CompletedAt.Time().Before(item.StartedAt.Time()) || item.SourceReference.Kind != "execution_round" || item.SourceReference.ID != item.ID || len(item.Data.Content) > MaxRuntimeTextBytes || !utf8.ValidString(item.Data.Content) || item.Data.Content != *v.Execution.FinalText {
		return invalid()
	}
	return nil
}
