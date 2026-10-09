package contract

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type TaskTransitionCommand struct{}
type TaskTransitionCommandID = f.ID[TaskTransitionCommand]
type TaskTransitionCommandName string

const TaskTransitionTransfer TaskTransitionCommandName = "work.task.transfer"

const (
	MaxTaskTransitionNameBytes         = 64
	MaxTaskTransferBytes               = 512 << 10
	MaxTaskTransitionResultBytes       = 512 << 10
	MaxTaskTransitionLookupBytes       = 512 << 10
	MaxTaskTransitionActorBytes        = 1 << 10
	MaxTaskTransitionCommentBytes      = 32768
	MaxTaskCommentPayloadBytes         = 256 << 10
	MaxTaskTransitionEventBytes        = 16 << 10
	MaxTaskTransitionCommentEventBytes = 272 << 10
	MaxTaskTransitionedBytes           = 16 << 10
)

func (v TaskTransitionCommandName) Validate() error {
	if v != TaskTransitionTransfer {
		return invalid("/command", "UNKNOWN_COMMAND")
	}
	return nil
}
func (v TaskTransitionCommandName) MarshalJSON() ([]byte, error) {
	return checkedLimit(string(v), v.Validate(), MaxTaskTransitionNameBytes)
}
func (v *TaskTransitionCommandName) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	s, err := taskBlockerString(raw, MaxTaskTransitionNameBytes)
	if err != nil {
		return err
	}
	next := TaskTransitionCommandName(s)
	if err := next.Validate(); err != nil {
		return err
	}
	*v = next
	return nil
}

// TaskTransfer contains requested changes, never authorization or current facts.
// A nil assignee/comment means omitted; the two slices are unordered sets.
type TaskTransfer struct {
	TargetState       TaskState           `json:"target_state"`
	AssigneeAgentID   *i.AgentID          `json:"assignee_agent_id,omitempty"`
	Comment           *string             `json:"comment,omitempty"`
	AddBlockers       []TaskBlockerCreate `json:"add_blockers"`
	ResolveBlockerIDs []TaskBlockerID     `json:"resolve_blocker_ids"`
}

func (v TaskTransfer) Validate() error {
	if v.TargetState.Validate() != nil {
		return invalid("/target_state", "INVALID_TASK_STATE")
	}
	if v.AssigneeAgentID != nil && v.AssigneeAgentID.Validate() != nil {
		return invalid("/assignee_agent_id", "INVALID_ASSIGNEE")
	}
	if v.Comment != nil {
		if err := validateTaskTransitionComment(*v.Comment, "/comment"); err != nil {
			return err
		}
	}
	if len(v.AddBlockers) > 16 || len(v.ResolveBlockerIDs) > 16 || len(v.AddBlockers)+len(v.ResolveBlockerIDs) > 32 {
		return invalid("", "BLOCKER_BATCH_LIMIT")
	}
	seen := make(map[TaskBlockerID]bool, len(v.AddBlockers)+len(v.ResolveBlockerIDs))
	var unbound error
	for _, b := range v.AddBlockers {
		if err := b.Validate(); err != nil {
			if !taskTransitionUnbound(err) {
				return err
			}
			if unbound == nil {
				unbound = err
			}
		}
		if seen[b.BlockerID] {
			return invalid("/add_blockers", "DUPLICATE_BLOCKER")
		}
		seen[b.BlockerID] = true
	}
	for _, id := range v.ResolveBlockerIDs {
		if id.Validate() != nil || seen[id] {
			return invalid("/resolve_blocker_ids", "INVALID_BLOCKER_SET")
		}
		seen[id] = true
	}
	return unbound
}
func (v TaskTransfer) Clone() TaskTransfer {
	v.AssigneeAgentID = taskClonePtr(v.AssigneeAgentID)
	v.Comment = taskClonePtr(v.Comment)
	v.AddBlockers = slices.Clone(v.AddBlockers)
	for n := range v.AddBlockers {
		v.AddBlockers[n] = v.AddBlockers[n].Clone()
	}
	v.ResolveBlockerIDs = slices.Clone(v.ResolveBlockerIDs)
	return v
}
func (v TaskTransfer) normalized() TaskTransfer {
	v = v.Clone()
	if v.AddBlockers == nil {
		v.AddBlockers = []TaskBlockerCreate{}
	}
	if v.ResolveBlockerIDs == nil {
		v.ResolveBlockerIDs = []TaskBlockerID{}
	}
	slices.SortFunc(v.AddBlockers, func(a, b TaskBlockerCreate) int { return strings.Compare(a.BlockerID.String(), b.BlockerID.String()) })
	slices.SortFunc(v.ResolveBlockerIDs, func(a, b TaskBlockerID) int { return strings.Compare(a.String(), b.String()) })
	return v
}
func (v TaskTransfer) MarshalJSON() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	type wire TaskTransfer
	return checkedLimit(wire(v.normalized()), nil, MaxTaskTransferBytes)
}
func (v *TaskTransfer) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	fields, err := decodeFieldsLimit[map[string]json.RawMessage](raw, []string{"target_state"},
		[]string{"assignee_agent_id", "comment", "add_blockers", "resolve_blocker_ids"}, nil, MaxTaskTransferBytes)
	if err != nil {
		return err
	}
	var next TaskTransfer
	if err := next.TargetState.UnmarshalJSON(fields["target_state"]); err != nil {
		return err
	}
	if raw, ok := fields["assignee_agent_id"]; ok {
		var id i.AgentID
		if json.Unmarshal(raw, &id) != nil {
			return invalid("/assignee_agent_id", "INVALID_ASSIGNEE")
		}
		next.AssigneeAgentID = &id
	}
	if raw, ok := fields["comment"]; ok {
		s, err := taskBlockerString(raw, MaxTaskTransferBytes)
		if err != nil {
			return err
		}
		next.Comment = &s
	}
	var adds, resolves []json.RawMessage
	if raw, ok := fields["add_blockers"]; ok && json.Unmarshal(raw, &adds) != nil {
		return invalid("/add_blockers", "INVALID_ENCODING")
	}
	if raw, ok := fields["resolve_blocker_ids"]; ok && json.Unmarshal(raw, &resolves) != nil {
		return invalid("/resolve_blocker_ids", "INVALID_ENCODING")
	}
	if len(adds) > 16 || len(resolves) > 16 || len(adds)+len(resolves) > 32 {
		return invalid("", "BLOCKER_BATCH_LIMIT")
	}
	for _, raw := range adds {
		b, err := DecodeTaskBlockerCreate(raw)
		if err != nil {
			if !taskTransitionUnbound(err) {
				return err
			}
			// B0-C already checked this original raw's envelope, common values
			// and nested metadata cap. Retain common values only to finish all
			// shape/set checks before returning UNBOUND. This never escapes as
			// a successful DTO or substitutes metadata for an unsupported type.
			var common struct {
				BlockerID   TaskBlockerID   `json:"blocker_id"`
				Type        TaskBlockerType `json:"type"`
				Description string          `json:"description"`
			}
			if json.Unmarshal(raw, &common) != nil {
				return invalid("/add_blockers", "INVALID_ENCODING")
			}
			b = TaskBlockerCreate{BlockerID: common.BlockerID, Type: common.Type, Description: common.Description}
		}
		next.AddBlockers = append(next.AddBlockers, b)
	}
	for _, raw := range resolves {
		var id TaskBlockerID
		if json.Unmarshal(raw, &id) != nil {
			return invalid("/resolve_blocker_ids", "INVALID_BLOCKER_SET")
		}
		next.ResolveBlockerIDs = append(next.ResolveBlockerIDs, id)
	}
	if err := next.Validate(); err != nil {
		return err
	}
	*v = next.normalized()
	return nil
}
func taskTransitionUnbound(err error) bool {
	var fault *f.Fault
	return errors.As(err, &fault) && fault.Code == f.DependencyUnbound
}

func validateTaskTransitionComment(body, path string) error {
	if len(body) < 1 || len(body) > MaxTaskTransitionCommentBytes || !utf8.ValidString(body) {
		return invalid(path, "INVALID_COMMENT")
	}
	hasText := false
	for _, r := range body {
		if unicode.Is(unicode.Cc, r) && r != '\t' && r != '\n' && r != '\r' {
			return invalid(path, "INVALID_COMMENT")
		}
		hasText = hasText || !unicode.IsSpace(r)
	}
	if !hasText {
		return invalid(path, "INVALID_COMMENT")
	}
	return nil
}
func validateTaskTransitionEventIDs(ids []TaskEventID) error {
	if len(ids) < 1 || len(ids) > 35 {
		return invalid("/task_event_ids", "INVALID_HISTORY_IDS")
	}
	for n, id := range ids {
		if id.Validate() != nil || n > 0 && ids[n-1].String() >= id.String() {
			return invalid("/task_event_ids", "INVALID_HISTORY_IDS")
		}
	}
	return nil
}

type TaskTransitionMutation struct {
	Task         Task            `json:"task"`
	TaskEventIDs []TaskEventID   `json:"task_event_ids"`
	EventIDs     []event.EventID `json:"event_ids"`
}

func (v TaskTransitionMutation) Validate() error {
	if err := v.Task.Validate(); err != nil {
		return err
	}
	if v.Task.Version < 2 || len(v.EventIDs) != 1 || v.EventIDs[0].Validate() != nil {
		return invalid("", "INVALID_TRANSITION_RESULT")
	}
	return validateTaskTransitionEventIDs(v.TaskEventIDs)
}
func (v TaskTransitionMutation) Clone() TaskTransitionMutation {
	v.Task = v.Task.Clone()
	v.TaskEventIDs = slices.Clone(v.TaskEventIDs)
	v.EventIDs = slices.Clone(v.EventIDs)
	return v
}
func (v TaskTransitionMutation) MarshalJSON() ([]byte, error) {
	type wire TaskTransitionMutation
	return checkedLimit(wire(v), v.Validate(), MaxTaskTransitionResultBytes)
}
func (v *TaskTransitionMutation) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	type wire TaskTransitionMutation
	w, err := decodeFieldsLimit[wire](raw, []string{"task", "task_event_ids", "event_ids"}, nil, nil, MaxTaskTransitionResultBytes)
	if err != nil {
		return err
	}
	next := TaskTransitionMutation(w)
	if err := next.Validate(); err != nil {
		return err
	}
	*v = next
	return nil
}

type TaskTransitionLookupRequest struct {
	ProjectID      ProjectID                 `json:"project_id"`
	Command        TaskTransitionCommandName `json:"command"`
	IdempotencyKey f.IdempotencyKey          `json:"idempotency_key"`
	SemanticDigest f.Digest                  `json:"semantic_digest"`
}

func (v TaskTransitionLookupRequest) Validate() error {
	if v.ProjectID.Validate() != nil || v.Command.Validate() != nil || v.IdempotencyKey.Validate() != nil || v.SemanticDigest.Validate() != nil {
		return invalid("", "INVALID_LOOKUP")
	}
	return nil
}
func (v TaskTransitionLookupRequest) Clone() TaskTransitionLookupRequest { return v }
func (v TaskTransitionLookupRequest) MarshalJSON() ([]byte, error) {
	type wire TaskTransitionLookupRequest
	return checkedLimit(wire(v), v.Validate(), MaxTaskLookupRequestBytes)
}
func (v *TaskTransitionLookupRequest) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	type wire TaskTransitionLookupRequest
	w, err := decodeFieldsLimit[wire](raw, []string{"project_id", "command", "idempotency_key", "semantic_digest"}, nil, nil, MaxTaskLookupRequestBytes)
	if err != nil {
		return err
	}
	next := TaskTransitionLookupRequest(w)
	if err := next.Validate(); err != nil {
		return err
	}
	*v = next
	return nil
}

type TaskTransitionLookup struct {
	Status  LookupState             `json:"status"`
	Receipt *TaskTransitionMutation `json:"receipt"`
}

func (v TaskTransitionLookup) Validate() error {
	switch v.Status {
	case LookupCommitted:
		if v.Receipt != nil {
			return v.Receipt.Validate()
		}
	case LookupInProgress, LookupNotObserved:
		if v.Receipt == nil {
			return nil
		}
	}
	return invalid("", "INVALID_LOOKUP_RESULT")
}
func (v TaskTransitionLookup) Clone() TaskTransitionLookup {
	if v.Receipt != nil {
		next := v.Receipt.Clone()
		v.Receipt = &next
	}
	return v
}
func (v TaskTransitionLookup) MarshalJSON() ([]byte, error) {
	type wire TaskTransitionLookup
	return checkedLimit(wire(v), v.Validate(), MaxTaskTransitionLookupBytes)
}
func (v *TaskTransitionLookup) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	fields, err := decodeFieldsLimit[map[string]json.RawMessage](raw, []string{"status", "receipt"}, nil, []string{"receipt"}, MaxTaskTransitionLookupBytes)
	if err != nil {
		return err
	}
	status, err := taskBlockerString(fields["status"], MaxTaskTransitionLookupBytes)
	if err != nil {
		return err
	}
	next := TaskTransitionLookup{Status: LookupState(status)}
	if strings.TrimSpace(string(fields["receipt"])) != "null" {
		receipt, err := DecodeTaskTransitionMutation(fields["receipt"])
		if err != nil {
			return err
		}
		next.Receipt = &receipt
	}
	if err := next.Validate(); err != nil {
		return err
	}
	*v = next
	return nil
}

// TaskTransitions declares future service operations; this package supplies no
// implementation, current authority, persistence or observation of a commit.
type TaskTransitions interface {
	TransferTask(context.Context, i.Actor, f.CommandMeta, ProjectID, TaskID, TaskTransfer) (TaskTransitionMutation, error)
	LookupTaskTransition(context.Context, i.Actor, TaskTransitionLookupRequest) (TaskTransitionLookup, error)
}

func TaskTransitionIdentity(project ProjectID, key f.IdempotencyKey) (f.CommandIdentity, error) {
	if project.Validate() != nil || key.Validate() != nil {
		return f.CommandIdentity{}, invalid("", "INVALID_COMMAND")
	}
	return f.NewCommandIdentity("project", []string{project.String()}, string(TaskTransitionTransfer), key)
}

// TaskTransferDigest identifies pure request semantics. Neither a valid Actor
// nor the returned Digest proves current execution/Tool or Project authority.
func TaskTransferDigest(actor i.Actor, meta f.CommandMeta, project ProjectID, task TaskID, request TaskTransfer) (f.Digest, error) {
	if project.Validate() != nil || task.Validate() != nil || meta.Validate() != nil || meta.ExpectedVersion == nil {
		return "", invalid("", "INVALID_COMMAND")
	}
	if err := request.Validate(); err != nil {
		return "", err
	}
	if actor.Validate() != nil {
		return "", f.NewFault(f.Unauthenticated, f.NotStarted)
	}
	details := actor.Details()
	var subject map[string]string
	switch details.Kind {
	case i.Human:
		subject = map[string]string{"kind": "human", "user_id": details.UserID}
	case i.AgentRun:
		if details.ProjectID != project.String() {
			return "", f.NewFault(f.Forbidden, f.NotStarted)
		}
		subject = map[string]string{"kind": "agent_run", "project_id": details.ProjectID, "agent_id": details.AgentID, "execution_id": details.ExecutionID}
	default:
		return "", f.NewFault(f.Forbidden, f.NotStarted)
	}
	raw, err := json.Marshal(struct {
		Format          string                    `json:"format"`
		Command         TaskTransitionCommandName `json:"command"`
		ProjectID       ProjectID                 `json:"project_id"`
		TaskID          TaskID                    `json:"task_id"`
		ActorSubject    map[string]string         `json:"actor_subject"`
		ExpectedVersion f.Version                 `json:"expected_version"`
		Request         TaskTransfer              `json:"request"`
	}{"work-task-transition-v1", TaskTransitionTransfer, project, task, subject, *meta.ExpectedVersion, request})
	if err != nil {
		return "", err
	}
	raw, err = cursor.CanonicalJSON(raw)
	if err != nil {
		return "", invalid("", "INVALID_COMMAND")
	}
	sum := sha256.Sum256(raw)
	return f.Digest("sha256:" + hex.EncodeToString(sum[:])), nil
}

func DecodeTaskTransfer(raw []byte) (TaskTransfer, error) {
	var v TaskTransfer
	err := v.UnmarshalJSON(raw)
	return v, err
}
func DecodeTaskTransitionMutation(raw []byte) (TaskTransitionMutation, error) {
	var v TaskTransitionMutation
	err := v.UnmarshalJSON(raw)
	return v, err
}
func DecodeTaskTransitionLookupRequest(raw []byte) (TaskTransitionLookupRequest, error) {
	var v TaskTransitionLookupRequest
	err := v.UnmarshalJSON(raw)
	return v, err
}
func DecodeTaskTransitionLookup(raw []byte) (TaskTransitionLookup, error) {
	var v TaskTransitionLookup
	err := v.UnmarshalJSON(raw)
	return v, err
}

func (TaskTransitionCommandName) Format(w fmt.State, _ rune)   { taskTransitionSafeFormat(w) }
func (TaskTransfer) Format(w fmt.State, _ rune)                { taskTransitionSafeFormat(w) }
func (TaskTransitionMutation) Format(w fmt.State, _ rune)      { taskTransitionSafeFormat(w) }
func (TaskTransitionLookupRequest) Format(w fmt.State, _ rune) { taskTransitionSafeFormat(w) }
func (TaskTransitionLookup) Format(w fmt.State, _ rune)        { taskTransitionSafeFormat(w) }
func (TaskTransitionCommandName) LogValue() slog.Value {
	return slog.StringValue("work_task_transition")
}
func (TaskTransfer) LogValue() slog.Value           { return slog.StringValue("work_task_transition") }
func (TaskTransitionMutation) LogValue() slog.Value { return slog.StringValue("work_task_transition") }
func (TaskTransitionLookupRequest) LogValue() slog.Value {
	return slog.StringValue("work_task_transition")
}
func (TaskTransitionLookup) LogValue() slog.Value { return slog.StringValue("work_task_transition") }
