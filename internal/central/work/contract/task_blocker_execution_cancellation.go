package contract

import (
	"fmt"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"log/slog"
)

// This is a persisted read projection. Only the Execution cancellation
// transaction can create it; TaskBlockerCreate continues to reject this arm.
const MaxTaskExecutionCancellationMetadataBytes = 8 << 10

type TaskBlockerExecutionCancellationMetadata struct {
	ExecutionID i.ExecutionID `json:"execution_id"`
	CancelledBy i.UserID      `json:"cancelled_by"`
	Reason      *string       `json:"reason"`
}

func (v TaskBlockerExecutionCancellationMetadata) Validate() error {
	if v.ExecutionID.Validate() != nil || v.CancelledBy.Validate() != nil || v.Reason != nil && !taskBlockerText(*v.Reason, 1024, true) {
		return invalid("/metadata", "INVALID_EXECUTION_CANCELLATION")
	}
	return nil
}
func (v TaskBlockerExecutionCancellationMetadata) Clone() TaskBlockerExecutionCancellationMetadata {
	v.Reason = taskClonePtr(v.Reason)
	return v
}
func (v TaskBlockerExecutionCancellationMetadata) MarshalJSON() ([]byte, error) {
	type wire TaskBlockerExecutionCancellationMetadata
	return checkedLimit(wire(v), v.Validate(), MaxTaskExecutionCancellationMetadataBytes)
}
func (v *TaskBlockerExecutionCancellationMetadata) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("/metadata", "INVALID_EXECUTION_CANCELLATION")
	}
	type wire TaskBlockerExecutionCancellationMetadata
	w, err := decodeFieldsLimit[wire](raw, []string{"execution_id", "cancelled_by", "reason"}, nil, []string{"reason"}, MaxTaskExecutionCancellationMetadataBytes)
	if err != nil {
		return err
	}
	n := TaskBlockerExecutionCancellationMetadata(w)
	if err = n.Validate(); err != nil {
		return err
	}
	*v = n
	return nil
}
func (v TaskBlocker) validateExecutionCancellation() error {
	if v.ID.Validate() != nil || v.ProjectID.Validate() != nil || v.TaskID.Validate() != nil || !taskBlockerText(v.Description, MaxTaskBlockerDescriptionBytes, false) || v.CreatedAt.Validate() != nil || v.CreatedBy.Validate() != nil || v.ExecutionCancellation == nil || v.ExecutionCancellation.Validate() != nil || v.CreatedBy.UserID != v.ExecutionCancellation.CancelledBy || v.Technical != nil || v.SchedulerCreatedBy != nil || v.Metadata.RelyOn != nil || v.Metadata.WaitingForHuman != nil {
		return invalid("", "INVALID_EXECUTION_CANCELLATION")
	}
	return v.validateResolution()
}
func (v TaskBlocker) validateResolution() error {
	if v.ResolvedAt == nil {
		if v.ResolvedBy != nil || v.TransitionResolvedBy != nil || v.ResolutionComment != nil {
			return invalid("", "INVALID_BLOCKER_RESOLUTION")
		}
		return nil
	}
	if v.ResolvedAt.Validate() != nil || v.ResolvedAt.Time().Before(v.CreatedAt.Time()) {
		return invalid("", "INVALID_BLOCKER_RESOLUTION")
	}
	if v.TransitionResolvedBy != nil {
		if v.ResolvedBy != nil || v.TransitionResolvedBy.Validate() != nil || v.TransitionResolvedBy.AgentID.Validate() != nil {
			return invalid("", "INVALID_BLOCKER_RESOLUTION")
		}
	} else if v.ResolvedBy == nil || v.ResolvedBy.Validate() != nil {
		return invalid("", "INVALID_BLOCKER_RESOLUTION")
	}
	return (TaskBlockerResolve{BlockerID: v.ID, ResolutionComment: v.ResolutionComment}).Validate()
}
func (TaskBlockerExecutionCancellationMetadata) Format(w fmt.State, r rune) { blockerSafeFormat(w, r) }
func (TaskBlockerExecutionCancellationMetadata) LogValue() slog.Value       { return blockerSafeLog() }
