package contract

import (
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const DurableFinalTextV1 = "durable_final_text_v1"
const DurableFinalTextV2 = "durable_final_text_v2"
const MaxRuntimeTextBytes = 16 << 20
const MaxRuntimeViewBytes = MaxRuntimeTextBytes + (64 << 10)

type RuntimeAvailability string

const (
	RuntimePending     RuntimeAvailability = "pending"
	RuntimeAvailable   RuntimeAvailability = "available"
	RuntimeNotRecorded RuntimeAvailability = "not_recorded"
)

// This is a complete, durable terminal projection, not an active stream or a
// claim that unflushed Model frames have been included. Generation is the
// original start's SnapshotID within this schema, not a WebSocket epoch.
type RuntimeSnapshot struct {
	Schema                 string            `json:"schema"`
	Generation             SnapshotID        `json:"generation"`
	IncludedUpdateProgress f.Sequence        `json:"included_update_progress"`
	ExecutionVersion       f.Version         `json:"execution_version"`
	Items                  []RuntimeTextItem `json:"items"`
}

type RuntimeTextItem struct {
	ID              RoundID               `json:"id"`
	ExecutionID     i.ExecutionID         `json:"execution_id"`
	Seq             f.Sequence            `json:"seq"`
	Type            string                `json:"type"`
	Status          string                `json:"status"`
	Revision        f.Version             `json:"revision"`
	StartedAt       f.Instant             `json:"started_at"`
	UpdatedAt       f.Instant             `json:"updated_at"`
	CompletedAt     f.Instant             `json:"completed_at"`
	SourceReference RuntimeRoundReference `json:"source_reference"`
	Data            RuntimeTextData       `json:"data"`
}
type RuntimeRoundReference struct {
	Kind string  `json:"kind"`
	ID   RoundID `json:"id"`
}
type RuntimeTextData struct {
	Content string `json:"content"`
}
type TaskRuntimeView struct {
	Execution    TaskExecution       `json:"execution"`
	Availability RuntimeAvailability `json:"availability"`
	Snapshot     *RuntimeSnapshot    `json:"snapshot"`
}

// ValidateProjection checks the closed public shape and its canonical binding.
// Current authorization, original Store return and durable provenance remain
// responsibilities of the original reader and producer.
func (v TaskRuntimeView) ValidateProjection() error {
	e := v.Execution
	if e.ID.Validate() != nil || e.ProjectID.Validate() != nil || e.Version.Validate() != nil || !e.Status.Valid() {
		return f.NewFault(f.DependencyUnavailable, f.NotStarted)
	}
	if v.Availability == RuntimePending && !e.Status.Terminal() && v.Snapshot == nil {
		return nil
	}
	if v.Availability == RuntimeNotRecorded && e.Status.Terminal() && v.Snapshot == nil {
		return nil
	}
	s := v.Snapshot
	if v.Availability != RuntimeAvailable || !e.Status.Terminal() || s == nil || e.SnapshotID == nil || e.StartedAt == nil || e.CompletedAt == nil || e.Reason == nil || (s.Schema != DurableFinalTextV1 && s.Schema != DurableFinalTextV2) || s.Generation.Validate() != nil || s.Generation != *e.SnapshotID || s.ExecutionVersion != e.Version || s.IncludedUpdateProgress != 1 || s.Items == nil {
		return f.NewFault(f.DependencyUnavailable, f.NotStarted)
	}
	if e.Status != Succeeded {
		if len(s.Items) == 0 && (e.Status == Failed || e.Status == Cancelled) {
			return nil
		}
		return f.NewFault(f.DependencyUnavailable, f.NotStarted)
	}
	if len(s.Items) != 1 {
		return f.NewFault(f.DependencyUnavailable, f.NotStarted)
	}
	t := s.Items[0]
	if t.ID.Validate() != nil || t.ExecutionID != e.ID || t.Seq != 1 || t.Type != "text" || t.Status != "completed" || t.Revision != 1 || t.StartedAt.Validate() != nil || t.StartedAt.Time().Before(e.StartedAt.Time()) || (s.Schema == DurableFinalTextV1 && t.StartedAt != *e.StartedAt) || t.UpdatedAt != *e.CompletedAt || t.CompletedAt != *e.CompletedAt || t.CompletedAt.Time().Before(t.StartedAt.Time()) || t.SourceReference.Kind != "execution_round" || t.SourceReference.ID != t.ID || len(t.Data.Content) > MaxRuntimeTextBytes || !utf8.ValidString(t.Data.Content) {
		return f.NewFault(f.DependencyUnavailable, f.NotStarted)
	}
	return nil
}
