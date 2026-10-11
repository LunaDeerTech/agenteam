package contract

func (v TaskRelaunchRequest) MarshalJSON() ([]byte, error) {
	type wire TaskRelaunchRequest
	return checkedLimit(wire(v), v.Validate(), MaxTaskLookupRequestBytes)
}
func (v *TaskRelaunchRequest) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_TASK_RELAUNCH")
	}
	type wire TaskRelaunchRequest
	w, err := decodeFieldsLimit[wire](raw, []string{"ProjectID", "TaskID", "AgentID", "CurrentSprintID", "ExpectedTaskVersion", "DispatchID", "RequestID", "Purpose"}, nil, nil, MaxTaskLookupRequestBytes)
	if err != nil {
		return err
	}
	n := TaskRelaunchRequest(w)
	if err = n.Validate(); err != nil {
		return err
	}
	*v = n
	return nil
}
func (v TaskRelaunchSource) MarshalJSON() ([]byte, error) {
	type wire TaskRelaunchSource
	return checkedLimit(wire(v), v.Validate(), MaxTaskLookupRequestBytes)
}
func (v *TaskRelaunchSource) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_TASK_RELAUNCH_SOURCE")
	}
	type wire TaskRelaunchSource
	w, err := decodeFieldsLimit[wire](raw, []string{"Request", "MilestoneID", "ReferenceDigest"}, nil, nil, MaxTaskLookupRequestBytes)
	if err != nil {
		return err
	}
	n := TaskRelaunchSource(w)
	if err = n.Validate(); err != nil {
		return err
	}
	*v = n
	return nil
}
