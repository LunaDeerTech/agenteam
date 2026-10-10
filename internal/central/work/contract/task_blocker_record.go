package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

const (
	MaxTaskBlockerRecordBytes   = 16 << 10
	MaxTaskBlockerResolveBytes  = 8 << 10
	MaxTaskBlockerMutationBytes = 512 << 10
	MaxTaskBlockerLookupBytes   = 16 << 10
)

type TaskBlockerStatus string

const (
	TaskBlockersUnresolved TaskBlockerStatus = "unresolved"
	TaskBlockersResolved   TaskBlockerStatus = "resolved"
	TaskBlockersAll        TaskBlockerStatus = "all"
)

func (v TaskBlockerStatus) Validate() error {
	if v != TaskBlockersUnresolved && v != TaskBlockersResolved && v != TaskBlockersAll {
		return invalid("/status", "INVALID_BLOCKER_STATUS")
	}
	return nil
}
func (v TaskBlockerStatus) MarshalJSON() ([]byte, error) {
	return checkedLimit(string(v), v.Validate(), 64)
}
func (v *TaskBlockerStatus) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	s, e := taskBlockerString(raw, 64)
	if e != nil {
		return e
	}
	n := TaskBlockerStatus(s)
	if e = n.Validate(); e != nil {
		return e
	}
	*v = n
	return nil
}

type TaskBlocker struct {
	ID          TaskBlockerID       `json:"id"`
	ProjectID   ProjectID           `json:"project_id"`
	TaskID      TaskID              `json:"task_id"`
	Type        TaskBlockerType     `json:"type"`
	Description string              `json:"description"`
	Metadata    TaskBlockerMetadata `json:"-"`
	CreatedAt   f.Instant           `json:"created_at"`
	CreatedBy   TaskEventActor      `json:"created_by"`
	// SchedulerCreatedBy and Technical are the read-only technical arm. They
	// never enter TaskBlockerCreate or the Human actor codec.
	SchedulerCreatedBy *SchedulerTaskActor           `json:"-"`
	Technical          *TaskBlockerTechnicalMetadata `json:"-"`
	ResolvedAt         *f.Instant                    `json:"resolved_at"`
	ResolvedBy         *TaskEventActor               `json:"resolved_by"`
	ResolutionComment  *string                       `json:"resolution_comment"`
}

func (v TaskBlocker) Validate() error {
	if v.Type == TaskBlockerTechnical {
		return v.validateTechnicalRecord()
	}
	if v.Technical != nil || v.SchedulerCreatedBy != nil {
		return invalid("", "INVALID_BLOCKER")
	}
	if err := (TaskBlockerCreate{BlockerID: v.ID, Type: v.Type, Description: v.Description, Metadata: v.Metadata}).Validate(); err != nil {
		return err
	}
	if v.ProjectID.Validate() != nil || v.TaskID.Validate() != nil || v.CreatedAt.Validate() != nil || v.CreatedBy.Validate() != nil {
		return invalid("", "INVALID_BLOCKER")
	}
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
func (v TaskBlocker) Clone() TaskBlocker {
	v.Metadata = v.Metadata.Clone()
	v.Technical = taskClonePtr(v.Technical)
	v.SchedulerCreatedBy = taskClonePtr(v.SchedulerCreatedBy)
	v.ResolvedAt = taskClonePtr(v.ResolvedAt)
	v.ResolvedBy = taskClonePtr(v.ResolvedBy)
	v.ResolutionComment = taskClonePtr(v.ResolutionComment)
	return v
}
func (v TaskBlocker) MarshalJSON() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	var metadata any = v.Metadata.WaitingForHuman
	if v.Type == TaskBlockerRelyOn {
		metadata = v.Metadata.RelyOn
	}
	type wire TaskBlocker
	if v.Type == TaskBlockerTechnical {
		return checkedLimit(struct {
			wire
			Metadata  TaskBlockerTechnicalMetadata `json:"metadata"`
			CreatedBy SchedulerTaskActor           `json:"created_by"`
		}{wire(v), *v.Technical, *v.SchedulerCreatedBy}, nil, MaxTaskBlockerRecordBytes)
	}
	return checkedLimit(struct {
		wire
		Metadata any `json:"metadata"`
	}{wire(v), metadata}, nil, MaxTaskBlockerRecordBytes)
}
func (v *TaskBlocker) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	fields, err := taskBlockerFields(raw, MaxTaskBlockerRecordBytes, []string{"id", "project_id", "task_id", "type", "description", "metadata", "created_at", "created_by", "resolved_at", "resolved_by", "resolution_comment"}, []string{"resolved_at", "resolved_by", "resolution_comment"})
	if err != nil {
		return err
	}
	var next TaskBlocker
	for _, p := range []struct {
		k string
		v any
	}{{"id", &next.ID}, {"project_id", &next.ProjectID}, {"task_id", &next.TaskID}, {"type", &next.Type}, {"description", &next.Description}, {"created_at", &next.CreatedAt}, {"resolved_at", &next.ResolvedAt}, {"resolution_comment", &next.ResolutionComment}} {
		if json.Unmarshal(fields[p.k], p.v) != nil {
			return invalid("", "INVALID_ENCODING")
		}
	}
	if next.Type == TaskBlockerTechnical {
		next.SchedulerCreatedBy = new(SchedulerTaskActor)
		err = next.SchedulerCreatedBy.UnmarshalJSON(fields["created_by"])
	} else {
		err = next.CreatedBy.UnmarshalJSON(fields["created_by"])
	}
	if err != nil {
		return err
	}
	if !blockerNull(fields["resolved_by"]) {
		next.ResolvedBy = new(TaskEventActor)
		if err = next.ResolvedBy.UnmarshalJSON(fields["resolved_by"]); err != nil {
			return err
		}
	}
	if err = taskBlockerObject(fields["metadata"], MaxTaskBlockerMetadataBytes); err != nil {
		return err
	}
	if next.Type != TaskBlockerTechnical {
		if err = taskBlockerSupported(next.Type); err != nil {
			return err
		}
	}
	if next.Type == TaskBlockerTechnical {
		next.Technical = new(TaskBlockerTechnicalMetadata)
		err = next.Technical.UnmarshalJSON(fields["metadata"])
	} else if next.Type == TaskBlockerRelyOn {
		next.Metadata.RelyOn = new(TaskBlockerRelyOnMetadata)
		err = next.Metadata.RelyOn.UnmarshalJSON(fields["metadata"])
	} else {
		next.Metadata.WaitingForHuman = new(TaskBlockerWaitingForHumanMetadata)
		err = next.Metadata.WaitingForHuman.UnmarshalJSON(fields["metadata"])
	}
	if err != nil {
		return err
	}
	if err = next.Validate(); err != nil {
		return err
	}
	*v = next
	return nil
}

type TaskBlockerResolve struct {
	BlockerID         TaskBlockerID `json:"blocker_id"`
	ResolutionComment *string       `json:"resolution_comment"`
}

func (v TaskBlockerResolve) Validate() error {
	if v.BlockerID.Validate() != nil {
		return invalid("/blocker_id", "INVALID_BLOCKER_ID")
	}
	if v.ResolutionComment != nil && !taskBlockerText(*v.ResolutionComment, MaxTaskBlockerResolutionCommentBytes, true) {
		return invalid("/resolution_comment", "INVALID_RESOLUTION_COMMENT")
	}
	return nil
}
func (v TaskBlockerResolve) Clone() TaskBlockerResolve {
	v.ResolutionComment = taskClonePtr(v.ResolutionComment)
	return v
}
func (v TaskBlockerResolve) MarshalJSON() ([]byte, error) {
	type wire TaskBlockerResolve
	return checkedLimit(wire(v), v.Validate(), MaxTaskBlockerResolveBytes)
}
func (v *TaskBlockerResolve) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	type wire TaskBlockerResolve
	n, err := decodeFieldsLimit[wire](raw, []string{"blocker_id", "resolution_comment"}, nil, []string{"resolution_comment"}, MaxTaskBlockerResolveBytes)
	if err != nil {
		return err
	}
	next := TaskBlockerResolve(n)
	if err = next.Validate(); err != nil {
		return err
	}
	*v = next
	return nil
}

func DecodeTaskBlocker(raw []byte) (TaskBlocker, error) {
	var v TaskBlocker
	err := v.UnmarshalJSON(raw)
	return v, err
}
func DecodeTaskBlockerResolve(raw []byte) (TaskBlockerResolve, error) {
	var v TaskBlockerResolve
	err := v.UnmarshalJSON(raw)
	return v, err
}
func blockerNull(raw []byte) bool                     { return bytes.Equal(bytes.TrimSpace(raw), []byte("null")) }
func blockerSafeFormat(w fmt.State, _ rune)           { _, _ = io.WriteString(w, "work_task_blocker") }
func blockerSafeLog() slog.Value                      { return slog.StringValue("work_task_blocker") }
func (TaskBlocker) Format(w fmt.State, r rune)        { blockerSafeFormat(w, r) }
func (TaskBlocker) LogValue() slog.Value              { return blockerSafeLog() }
func (TaskBlockerResolve) Format(w fmt.State, r rune) { blockerSafeFormat(w, r) }
func (TaskBlockerResolve) LogValue() slog.Value       { return blockerSafeLog() }
func (TaskBlockerStatus) Format(w fmt.State, r rune)  { blockerSafeFormat(w, r) }
func (TaskBlockerStatus) LogValue() slog.Value        { return blockerSafeLog() }
