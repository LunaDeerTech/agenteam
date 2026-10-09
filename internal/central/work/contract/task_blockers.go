package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"unicode"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// TaskBlockerIdentity is the sole Blocker ID marker, not a persisted record.
type TaskBlockerIdentity struct{}
type TaskBlockerID = f.ID[TaskBlockerIdentity]

type TaskBlockerType string

const (
	TaskBlockerRelyOn                    TaskBlockerType = "rely_on"
	TaskBlockerWaitingForHuman           TaskBlockerType = "waiting_for_human"
	TaskBlockerWaitingForMeetingApproval TaskBlockerType = "waiting_for_meeting_approval"
	TaskBlockerTechnical                 TaskBlockerType = "technical"
	TaskBlockerUserCancelledExecution    TaskBlockerType = "user_cancelled_execution"

	MaxTaskBlockerTypeBytes              = 64
	MaxTaskBlockerMetadataBytes          = 1 << 10
	MaxTaskBlockerDescriptionBytes       = 1024
	MaxTaskBlockerResolutionCommentBytes = 1024
	MaxTaskBlockerCreateBytes            = 8 << 10
	MaxTaskBlockerPayloadBytes           = MaxTaskHistoryPayloadBytes
)

// Validate recognizes the domain enum; it does not establish metadata support.
func (v TaskBlockerType) Validate() error {
	switch v {
	case TaskBlockerRelyOn, TaskBlockerWaitingForHuman, TaskBlockerWaitingForMeetingApproval,
		TaskBlockerTechnical, TaskBlockerUserCancelledExecution:
		return nil
	}
	return invalid("/type", "INVALID_BLOCKER_TYPE")
}

func taskBlockerSupported(v TaskBlockerType) error {
	if err := v.Validate(); err != nil {
		return err
	}
	if v != TaskBlockerRelyOn && v != TaskBlockerWaitingForHuman {
		return f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return nil
}

func (v TaskBlockerType) MarshalJSON() ([]byte, error) {
	return checkedLimit(string(v), v.Validate(), MaxTaskBlockerTypeBytes)
}
func (v *TaskBlockerType) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	s, err := taskBlockerString(raw, MaxTaskBlockerTypeBytes)
	if err != nil {
		return err
	}
	next := TaskBlockerType(s)
	if err := next.Validate(); err != nil {
		return err
	}
	*v = next
	return nil
}

type TaskBlockerRelyOnMetadata struct {
	RelatedTaskID TaskID `json:"related_task_id"`
}

func (v TaskBlockerRelyOnMetadata) Validate() error {
	if v.RelatedTaskID.Validate() != nil {
		return invalid("/related_task_id", "INVALID_RELATED_TASK_ID")
	}
	return nil
}
func (v TaskBlockerRelyOnMetadata) Clone() TaskBlockerRelyOnMetadata { return v }
func (v TaskBlockerRelyOnMetadata) MarshalJSON() ([]byte, error) {
	type wire TaskBlockerRelyOnMetadata
	return checkedLimit(wire(v), v.Validate(), MaxTaskBlockerMetadataBytes)
}
func (v *TaskBlockerRelyOnMetadata) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	fields, err := taskBlockerFields(raw, MaxTaskBlockerMetadataBytes, []string{"related_task_id"}, nil)
	if err != nil {
		return err
	}
	s, err := taskBlockerString(fields["related_task_id"], MaxTaskBlockerMetadataBytes)
	if err != nil {
		return err
	}
	id, err := f.ParseID[Task](s)
	if err != nil {
		return invalid("/related_task_id", "INVALID_RELATED_TASK_ID")
	}
	*v = TaskBlockerRelyOnMetadata{RelatedTaskID: id}
	return nil
}

// TaskBlockerWaitingForHumanMetadata supports only a human wait without an
// external reference. It must not be used as a fallback for an unbound type.
type TaskBlockerWaitingForHumanMetadata struct{}

func (TaskBlockerWaitingForHumanMetadata) Validate() error { return nil }
func (v TaskBlockerWaitingForHumanMetadata) Clone() TaskBlockerWaitingForHumanMetadata {
	return v
}
func (v TaskBlockerWaitingForHumanMetadata) MarshalJSON() ([]byte, error) {
	return checkedLimit(struct{}{}, v.Validate(), MaxTaskBlockerMetadataBytes)
}
func (v *TaskBlockerWaitingForHumanMetadata) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	if _, err := taskBlockerFields(raw, MaxTaskBlockerMetadataBytes, nil, nil); err != nil {
		return err
	}
	*v = TaskBlockerWaitingForHumanMetadata{}
	return nil
}

// TaskBlockerMetadata is selected by its enclosing Create.Type, not a wire DTO.
// Valid metadata proves only shape, never a current Task/reference or graph fact.
type TaskBlockerMetadata struct {
	RelyOn          *TaskBlockerRelyOnMetadata
	WaitingForHuman *TaskBlockerWaitingForHumanMetadata
}

func (v TaskBlockerMetadata) ValidateFor(typ TaskBlockerType) error {
	if err := taskBlockerSupported(typ); err != nil {
		return err
	}
	if typ == TaskBlockerRelyOn {
		if v.RelyOn == nil || v.WaitingForHuman != nil || v.RelyOn.Validate() != nil {
			return invalid("/metadata", "INVALID_BLOCKER_METADATA")
		}
	} else if v.WaitingForHuman == nil || v.RelyOn != nil {
		return invalid("/metadata", "INVALID_BLOCKER_METADATA")
	}
	return nil
}
func (v TaskBlockerMetadata) Clone() TaskBlockerMetadata {
	v.RelyOn = taskClonePtr(v.RelyOn)
	if v.WaitingForHuman != nil {
		// A nonzero allocation avoids aliasing Go's shared zero-size address.
		copy := new(struct {
			value TaskBlockerWaitingForHumanMetadata
			pad   byte
		})
		copy.value = *v.WaitingForHuman
		v.WaitingForHuman = &copy.value
	}
	return v
}

type TaskBlockerCreate struct {
	BlockerID   TaskBlockerID       `json:"blocker_id"`
	Type        TaskBlockerType     `json:"type"`
	Description string              `json:"description"`
	Metadata    TaskBlockerMetadata `json:"-"`
}

func (v TaskBlockerCreate) validateCommon() error {
	if v.BlockerID.Validate() != nil {
		return invalid("/blocker_id", "INVALID_BLOCKER_ID")
	}
	if err := v.Type.Validate(); err != nil {
		return err
	}
	if !taskBlockerText(v.Description, MaxTaskBlockerDescriptionBytes, false) {
		return invalid("/description", "INVALID_BLOCKER_DESCRIPTION")
	}
	return nil
}
func (v TaskBlockerCreate) Validate() error {
	if err := v.validateCommon(); err != nil {
		return err
	}
	return v.Metadata.ValidateFor(v.Type)
}
func (v TaskBlockerCreate) Clone() TaskBlockerCreate {
	v.Metadata = v.Metadata.Clone()
	return v
}
func (v TaskBlockerCreate) MarshalJSON() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	// The selected metadata remains a concrete typed value; raw bytes are never
	// a public or successful representation of an unsupported branch.
	if v.Type == TaskBlockerRelyOn {
		return checkedLimit(struct {
			BlockerID   TaskBlockerID             `json:"blocker_id"`
			Type        TaskBlockerType           `json:"type"`
			Description string                    `json:"description"`
			Metadata    TaskBlockerRelyOnMetadata `json:"metadata"`
		}{v.BlockerID, v.Type, v.Description, *v.Metadata.RelyOn}, nil, MaxTaskBlockerCreateBytes)
	}
	return checkedLimit(struct {
		BlockerID   TaskBlockerID                      `json:"blocker_id"`
		Type        TaskBlockerType                    `json:"type"`
		Description string                             `json:"description"`
		Metadata    TaskBlockerWaitingForHumanMetadata `json:"metadata"`
	}{v.BlockerID, v.Type, v.Description, *v.Metadata.WaitingForHuman}, nil, MaxTaskBlockerCreateBytes)
}
func (v *TaskBlockerCreate) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	fields, err := taskBlockerFields(raw, MaxTaskBlockerCreateBytes,
		[]string{"blocker_id", "type", "description", "metadata"}, nil)
	if err != nil {
		return err
	}
	idText, err := taskBlockerString(fields["blocker_id"], MaxTaskBlockerCreateBytes)
	if err != nil {
		return err
	}
	typeText, err := taskBlockerString(fields["type"], MaxTaskBlockerTypeBytes)
	if err != nil {
		return err
	}
	description, err := taskBlockerString(fields["description"], MaxTaskBlockerCreateBytes)
	if err != nil {
		return err
	}
	// fields preserves the original metadata token span, including all object
	// interior whitespace. Check it before decoding or choosing a branch.
	metadata := fields["metadata"]
	if err := taskBlockerObject(metadata, MaxTaskBlockerMetadataBytes); err != nil {
		return err
	}
	id, err := f.ParseID[TaskBlockerIdentity](idText)
	if err != nil {
		return invalid("/blocker_id", "INVALID_BLOCKER_ID")
	}
	next := TaskBlockerCreate{BlockerID: id, Type: TaskBlockerType(typeText), Description: description}
	if err := next.validateCommon(); err != nil {
		return err
	}
	if err := taskBlockerSupported(next.Type); err != nil {
		return err
	}
	if next.Type == TaskBlockerRelyOn {
		next.Metadata.RelyOn = new(TaskBlockerRelyOnMetadata)
		err = next.Metadata.RelyOn.UnmarshalJSON(metadata)
	} else {
		next.Metadata.WaitingForHuman = new(TaskBlockerWaitingForHumanMetadata)
		err = next.Metadata.WaitingForHuman.UnmarshalJSON(metadata)
	}
	if err != nil {
		return err
	}
	*v = next
	return nil
}

type TaskBlockerAddedPayload struct {
	BlockerID   TaskBlockerID   `json:"blocker_id"`
	BlockerType TaskBlockerType `json:"blocker_type"`
}

func taskBlockerPayloadCommon(id TaskBlockerID, typ TaskBlockerType) error {
	if id.Validate() != nil {
		return invalid("/blocker_id", "INVALID_BLOCKER_ID")
	}
	if typ.Validate() != nil {
		return invalid("/blocker_type", "INVALID_BLOCKER_TYPE")
	}
	return nil
}
func (v TaskBlockerAddedPayload) Validate() error {
	if err := taskBlockerPayloadCommon(v.BlockerID, v.BlockerType); err != nil {
		return err
	}
	return taskBlockerSupported(v.BlockerType)
}
func (v TaskBlockerAddedPayload) Clone() TaskBlockerAddedPayload { return v }
func (v TaskBlockerAddedPayload) MarshalJSON() ([]byte, error) {
	type wire TaskBlockerAddedPayload
	return checkedLimit(wire(v), v.Validate(), MaxTaskBlockerPayloadBytes)
}
func (v *TaskBlockerAddedPayload) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	fields, err := taskBlockerFields(raw, MaxTaskBlockerPayloadBytes, []string{"blocker_id", "blocker_type"}, nil)
	if err != nil {
		return err
	}
	id, typ, err := taskBlockerPayloadIdentity(fields)
	if err != nil {
		return err
	}
	next := TaskBlockerAddedPayload{BlockerID: id, BlockerType: typ}
	if err := next.Validate(); err != nil {
		return err
	}
	*v = next
	return nil
}

type TaskBlockerResolvedPayload struct {
	BlockerID         TaskBlockerID   `json:"blocker_id"`
	BlockerType       TaskBlockerType `json:"blocker_type"`
	ResolutionComment *string         `json:"resolution_comment"`
}

func (v TaskBlockerResolvedPayload) Validate() error {
	if err := taskBlockerPayloadCommon(v.BlockerID, v.BlockerType); err != nil {
		return err
	}
	if v.ResolutionComment != nil && !taskBlockerText(*v.ResolutionComment, MaxTaskBlockerResolutionCommentBytes, true) {
		return invalid("/resolution_comment", "INVALID_RESOLUTION_COMMENT")
	}
	return taskBlockerSupported(v.BlockerType)
}
func (v TaskBlockerResolvedPayload) Clone() TaskBlockerResolvedPayload {
	v.ResolutionComment = taskClonePtr(v.ResolutionComment)
	return v
}
func (v TaskBlockerResolvedPayload) MarshalJSON() ([]byte, error) {
	type wire TaskBlockerResolvedPayload
	return checkedLimit(wire(v), v.Validate(), MaxTaskBlockerPayloadBytes)
}
func (v *TaskBlockerResolvedPayload) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	fields, err := taskBlockerFields(raw, MaxTaskBlockerPayloadBytes,
		[]string{"blocker_id", "blocker_type", "resolution_comment"}, []string{"resolution_comment"})
	if err != nil {
		return err
	}
	var comment *string
	if !bytes.Equal(bytes.TrimSpace(fields["resolution_comment"]), []byte("null")) {
		s, err := taskBlockerString(fields["resolution_comment"], MaxTaskBlockerPayloadBytes)
		if err != nil {
			return err
		}
		comment = &s
	}
	id, typ, err := taskBlockerPayloadIdentity(fields)
	if err != nil {
		return err
	}
	next := TaskBlockerResolvedPayload{BlockerID: id, BlockerType: typ, ResolutionComment: comment}
	if err := next.Validate(); err != nil {
		return err
	}
	*v = next
	return nil
}

func taskBlockerPayloadIdentity(fields map[string]json.RawMessage) (TaskBlockerID, TaskBlockerType, error) {
	idText, err := taskBlockerString(fields["blocker_id"], MaxTaskBlockerPayloadBytes)
	if err != nil {
		return TaskBlockerID{}, "", err
	}
	typeText, err := taskBlockerString(fields["blocker_type"], MaxTaskBlockerTypeBytes)
	if err != nil {
		return TaskBlockerID{}, "", err
	}
	id, err := f.ParseID[TaskBlockerIdentity](idText)
	if err != nil {
		return TaskBlockerID{}, "", invalid("/blocker_id", "INVALID_BLOCKER_ID")
	}
	return id, TaskBlockerType(typeText), nil
}

func taskBlockerText(s string, limit int, nonblank bool) bool {
	if len(s) > limit || !utf8.ValidString(s) {
		return false
	}
	hasText := false
	for _, r := range s {
		if unicode.Is(unicode.Cc, r) && r != '\t' && r != '\n' && r != '\r' {
			return false
		}
		hasText = hasText || !unicode.IsSpace(r)
	}
	return !nonblank || hasText
}

// These private raw readers preserve original value spans. In particular,
// RawMessage receives the exact metadata token range rather than re-encoding
// decoded values, so internal whitespace cannot evade the nested raw cap.
func taskBlockerFields(raw []byte, limit int, required, nullable []string) (map[string]json.RawMessage, error) {
	return decodeFieldsLimit[map[string]json.RawMessage](raw, required, nil, nullable, limit)
}
func taskBlockerObject(raw []byte, limit int) error {
	if err := strictRawLimit(raw, limit); err != nil {
		return err
	}
	if trimmed := bytes.TrimSpace(raw); len(trimmed) == 0 || trimmed[0] != '{' {
		return invalid("", "INVALID_ENCODING")
	}
	return nil
}
func taskBlockerString(raw []byte, limit int) (string, error) {
	if err := strictRawLimit(raw, limit); err != nil {
		return "", err
	}
	var s string
	if trimmed := bytes.TrimSpace(raw); len(trimmed) == 0 || trimmed[0] != '"' || json.Unmarshal(raw, &s) != nil {
		return "", invalid("", "INVALID_ENCODING")
	}
	return s, nil
}

func DecodeTaskBlockerCreate(raw []byte) (TaskBlockerCreate, error) {
	var v TaskBlockerCreate
	err := v.UnmarshalJSON(raw)
	return v, err
}
func DecodeTaskBlockerAddedPayload(raw []byte) (TaskBlockerAddedPayload, error) {
	var v TaskBlockerAddedPayload
	err := v.UnmarshalJSON(raw)
	return v, err
}
func DecodeTaskBlockerResolvedPayload(raw []byte) (TaskBlockerResolvedPayload, error) {
	var v TaskBlockerResolvedPayload
	err := v.UnmarshalJSON(raw)
	return v, err
}

func (TaskBlockerType) Format(w fmt.State, _ rune)                    { taskTransitionSafeFormat(w) }
func (TaskBlockerRelyOnMetadata) Format(w fmt.State, _ rune)          { taskTransitionSafeFormat(w) }
func (TaskBlockerWaitingForHumanMetadata) Format(w fmt.State, _ rune) { taskTransitionSafeFormat(w) }
func (TaskBlockerMetadata) Format(w fmt.State, _ rune)                { taskTransitionSafeFormat(w) }
func (TaskBlockerCreate) Format(w fmt.State, _ rune)                  { taskTransitionSafeFormat(w) }
func (TaskBlockerAddedPayload) Format(w fmt.State, _ rune)            { taskTransitionSafeFormat(w) }
func (TaskBlockerResolvedPayload) Format(w fmt.State, _ rune)         { taskTransitionSafeFormat(w) }
func (TaskBlockerType) LogValue() slog.Value                          { return slog.StringValue("work_task_transition") }
func (TaskBlockerRelyOnMetadata) LogValue() slog.Value {
	return slog.StringValue("work_task_transition")
}
func (TaskBlockerWaitingForHumanMetadata) LogValue() slog.Value {
	return slog.StringValue("work_task_transition")
}
func (TaskBlockerMetadata) LogValue() slog.Value     { return slog.StringValue("work_task_transition") }
func (TaskBlockerCreate) LogValue() slog.Value       { return slog.StringValue("work_task_transition") }
func (TaskBlockerAddedPayload) LogValue() slog.Value { return slog.StringValue("work_task_transition") }
func (TaskBlockerResolvedPayload) LogValue() slog.Value {
	return slog.StringValue("work_task_transition")
}
