package contract

import (
	"encoding/json"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func (v TaskLaunchFailureRequest) OriginKind() TaskDispatchOriginKind {
	if v.Relaunch != nil {
		return TaskDispatchRelaunch
	}
	return TaskDispatchTodoClaim
}
func (v TaskLaunchFailureRequest) Equal(other TaskLaunchFailureRequest) bool {
	if v.Claim != other.Claim || v.DispatchVersion != other.DispatchVersion || v.LaunchAttempt != other.LaunchAttempt || (v.Relaunch == nil) != (other.Relaunch == nil) {
		return false
	}
	return v.Relaunch == nil || *v.Relaunch == *other.Relaunch
}
func (v TaskLaunchFailureRequest) ProjectID() ProjectID {
	if v.Relaunch != nil {
		return v.Relaunch.ProjectID
	}
	return v.Claim.ProjectID
}
func (v TaskLaunchFailureRequest) TaskID() TaskID {
	if v.Relaunch != nil {
		return v.Relaunch.TaskID
	}
	return v.Claim.TaskID
}
func (v TaskLaunchFailureRequest) AgentID() i.AgentID {
	if v.Relaunch != nil {
		return v.Relaunch.AgentID
	}
	return v.Claim.AgentID
}
func (v TaskLaunchFailureRequest) SprintID() SprintID {
	if v.Relaunch != nil {
		return v.Relaunch.CurrentSprintID
	}
	return v.Claim.CurrentSprintID
}
func (v TaskLaunchFailureRequest) DispatchID() string {
	if v.Relaunch != nil {
		return v.Relaunch.DispatchID
	}
	return v.Claim.DispatchID
}
func (v TaskLaunchFailureRequest) RequestID() f.ID[f.Request] {
	if v.Relaunch != nil {
		return v.Relaunch.RequestID
	}
	return v.Claim.RequestID
}
func (v TaskLaunchFailureRequest) Purpose() string {
	if v.Relaunch != nil {
		return v.Relaunch.Purpose
	}
	return v.Claim.Purpose
}

// Preserve the exact historical Claim/DispatchVersion/LaunchAttempt encoding.
// The new arm replaces Claim with Relaunch; null, both arms and unknown fields
// are rejected instead of interpreting a missing claim guard as a relaunch.
func (v TaskLaunchFailureRequest) MarshalJSON() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	if v.Relaunch == nil {
		return json.Marshal(struct {
			Claim           TaskClaimRequest
			DispatchVersion f.Version
			LaunchAttempt   int64
		}{v.Claim, v.DispatchVersion, v.LaunchAttempt})
	}
	return json.Marshal(struct {
		Relaunch        TaskRelaunchRequest
		DispatchVersion f.Version
		LaunchAttempt   int64
	}{*v.Relaunch, v.DispatchVersion, v.LaunchAttempt})
}
func (v *TaskLaunchFailureRequest) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_TASK_LAUNCH_FAILURE")
	}
	fields, err := decodeFieldsLimit[map[string]json.RawMessage](raw, []string{"DispatchVersion", "LaunchAttempt"}, []string{"Claim", "Relaunch"}, nil, MaxTaskLookupRequestBytes)
	if err != nil {
		return err
	}
	_, claim := fields["Claim"]
	_, relaunch := fields["Relaunch"]
	if claim == relaunch {
		return invalid("", "INVALID_TASK_LAUNCH_FAILURE")
	}
	type wire TaskLaunchFailureRequest
	required := []string{"Claim", "DispatchVersion", "LaunchAttempt"}
	if relaunch {
		required[0] = "Relaunch"
	}
	w, err := decodeFieldsLimit[wire](raw, required, nil, nil, MaxTaskLookupRequestBytes)
	if err != nil {
		return err
	}
	n := TaskLaunchFailureRequest(w)
	if err = n.Validate(); err != nil {
		return err
	}
	*v = n
	return nil
}

func zeroFailureGuard(v TaskClaimGuard) bool {
	return v.TaskID == (TaskID{}) && v.ClaimedVersion == 0 && v.SourceState == "" && v.SourceAssigneeID == (i.AgentID{}) && v.SourcePriority == "" && v.SourceSprintID == (SprintID{}) && v.SourceOrderGeneration == 0 && v.PredecessorID == nil && v.SuccessorID == nil
}
func (v TaskLaunchFailureFacts) ValidateFor(r TaskLaunchFailureRequest) error {
	if r.Validate() != nil || v.Reason.Validate() != nil || v.OccurredAt.Validate() != nil {
		return invalid("", "INVALID_TASK_LAUNCH_FAILURE_FACTS")
	}
	if r.Relaunch != nil {
		if !zeroFailureGuard(v.Guard) || v.Relaunch == nil || v.Relaunch.Validate() != nil || v.Relaunch.Request != *r.Relaunch {
			return invalid("", "INVALID_TASK_LAUNCH_FAILURE_FACTS")
		}
		return nil
	}
	g := v.Guard
	if v.Relaunch != nil || g.TaskID != r.Claim.TaskID || g.ClaimedVersion < 2 || g.ClaimedVersion-1 != r.Claim.ExpectedTaskVersion || g.SourceState != TaskStateTodo || g.SourceAssigneeID != r.Claim.AgentID || g.SourceSprintID != r.Claim.CurrentSprintID || g.SourceOrderGeneration <= 0 || g.SourcePriority.Validate() != nil {
		return invalid("", "INVALID_TASK_LAUNCH_FAILURE_FACTS")
	}
	return nil
}
func (v TaskLaunchFailureFacts) MarshalJSON() ([]byte, error) {
	if v.Relaunch == nil {
		return json.Marshal(struct {
			Guard      TaskClaimGuard
			Reason     TaskLaunchFailureReason
			OccurredAt f.Instant
		}{v.Guard, v.Reason, v.OccurredAt})
	}
	if !zeroFailureGuard(v.Guard) || v.Relaunch.Validate() != nil {
		return nil, invalid("", "INVALID_TASK_LAUNCH_FAILURE_FACTS")
	}
	return json.Marshal(struct {
		Relaunch   TaskRelaunchSource
		Reason     TaskLaunchFailureReason
		OccurredAt f.Instant
	}{*v.Relaunch, v.Reason, v.OccurredAt})
}
func (v *TaskLaunchFailureFacts) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_TASK_LAUNCH_FAILURE_FACTS")
	}
	fields, err := decodeFieldsLimit[map[string]json.RawMessage](raw, []string{"Reason", "OccurredAt"}, []string{"Guard", "Relaunch"}, nil, MaxTaskLookupRequestBytes)
	if err != nil {
		return err
	}
	_, guard := fields["Guard"]
	_, relaunch := fields["Relaunch"]
	if guard == relaunch {
		return invalid("", "INVALID_TASK_LAUNCH_FAILURE_FACTS")
	}
	type wire TaskLaunchFailureFacts
	required := []string{"Guard", "Reason", "OccurredAt"}
	if relaunch {
		required[0] = "Relaunch"
	}
	w, err := decodeFieldsLimit[wire](raw, required, nil, nil, MaxTaskLookupRequestBytes)
	if err != nil {
		return err
	}
	n := TaskLaunchFailureFacts(w)
	if n.Reason.Validate() != nil || n.OccurredAt.Validate() != nil || relaunch && (n.Relaunch == nil || n.Relaunch.Validate() != nil) {
		return invalid("", "INVALID_TASK_LAUNCH_FAILURE_FACTS")
	}
	*v = n
	return nil
}
