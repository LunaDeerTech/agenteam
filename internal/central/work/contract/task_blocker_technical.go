package contract

import (
	"fmt"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// TaskBlockerTechnicalMetadata is a persisted read projection. It is not a
// Human add/transfer request and does not establish a Scheduler failure proof.
type TaskBlockerTechnicalMetadata struct {
	Code        string `json:"code"`
	Source      string `json:"source"`
	ReferenceID string `json:"reference_id"`
}

func (v TaskBlockerTechnicalMetadata) Validate() error {
	if v.Code != "scheduler_launch_failed" || v.Source != "scheduler_dispatch" {
		return invalid("/metadata", "INVALID_TECHNICAL_BLOCKER")
	}
	if _, err := f.ParseID[SchedulerClaim](v.ReferenceID); err != nil {
		return invalid("/metadata", "INVALID_TECHNICAL_BLOCKER")
	}
	return nil
}
func (v TaskBlockerTechnicalMetadata) MarshalJSON() ([]byte, error) {
	type wire TaskBlockerTechnicalMetadata
	return checkedLimit(wire(v), v.Validate(), MaxTaskBlockerMetadataBytes)
}
func (v *TaskBlockerTechnicalMetadata) UnmarshalJSON(raw []byte) error {
	type wire TaskBlockerTechnicalMetadata
	n, err := decodeFieldsLimit[wire](raw, []string{"code", "source", "reference_id"}, nil, nil, MaxTaskBlockerMetadataBytes)
	if v == nil || err != nil {
		return invalid("/metadata", "INVALID_TECHNICAL_BLOCKER")
	}
	next := TaskBlockerTechnicalMetadata(n)
	if err = next.Validate(); err != nil {
		return err
	}
	*v = next
	return nil
}
func (v TaskBlocker) validateTechnicalRecord() error {
	base := TaskBlockerCreate{BlockerID: v.ID, Type: v.Type, Description: v.Description}
	if base.validateCommon() != nil || v.ProjectID.Validate() != nil || v.TaskID.Validate() != nil || v.CreatedAt.Validate() != nil ||
		v.Technical == nil || v.Technical.Validate() != nil || v.SchedulerCreatedBy == nil || v.SchedulerCreatedBy.Validate() != nil ||
		v.Technical.ReferenceID != v.SchedulerCreatedBy.CauseID || v.CreatedBy != (TaskEventActor{}) ||
		v.Metadata.RelyOn != nil || v.Metadata.WaitingForHuman != nil {
		return invalid("", "INVALID_TECHNICAL_BLOCKER")
	}
	// Resolution preserves the original Scheduler creation proof. The resolver
	// is independently the current Human Owner; this read shape grants no write.
	if (v.ResolvedAt == nil) != (v.ResolvedBy == nil) {
		return invalid("", "INVALID_BLOCKER_RESOLUTION")
	}
	if v.ResolvedAt == nil {
		if v.ResolutionComment != nil {
			return invalid("/resolution_comment", "INVALID_BLOCKER_RESOLUTION")
		}
		return nil
	}
	if v.ResolvedAt.Validate() != nil || v.ResolvedBy.Validate() != nil || v.ResolvedAt.Time().Before(v.CreatedAt.Time()) {
		return invalid("", "INVALID_BLOCKER_RESOLUTION")
	}
	return (TaskBlockerResolve{BlockerID: v.ID, ResolutionComment: v.ResolutionComment}).Validate()
}
func (TaskBlockerTechnicalMetadata) Format(w fmt.State, r rune) { blockerSafeFormat(w, r) }
func (TaskBlockerTechnicalMetadata) LogValue() slog.Value       { return blockerSafeLog() }
