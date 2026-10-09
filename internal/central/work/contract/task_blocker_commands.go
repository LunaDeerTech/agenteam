package contract

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type TaskBlockerCommand struct{}
type TaskBlockerCommandID = f.ID[TaskBlockerCommand]
type TaskBlockerCommandName string

const (
	TaskBlockerCommandAdd     TaskBlockerCommandName = "work.task.blocker.add"
	TaskBlockerCommandResolve TaskBlockerCommandName = "work.task.blocker.resolve"
)

func (v TaskBlockerCommandName) Validate() error {
	if v != TaskBlockerCommandAdd && v != TaskBlockerCommandResolve {
		return invalid("/command", "INVALID_COMMAND")
	}
	return nil
}
func (v TaskBlockerCommandName) MarshalJSON() ([]byte, error) {
	return checkedLimit(string(v), v.Validate(), 128)
}
func (v *TaskBlockerCommandName) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	s, e := taskBlockerString(raw, 128)
	if e != nil {
		return e
	}
	n := TaskBlockerCommandName(s)
	if e = n.Validate(); e != nil {
		return e
	}
	*v = n
	return nil
}

type TaskBlockerMutation struct {
	Task        Task            `json:"task"`
	Blocker     TaskBlocker     `json:"blocker"`
	TaskEventID TaskEventID     `json:"task_event_id"`
	EventIDs    []event.EventID `json:"event_ids"`
}

func (v TaskBlockerMutation) Validate() error {
	if v.Task.Validate() != nil || v.Blocker.Validate() != nil || v.TaskEventID.Validate() != nil || len(v.EventIDs) != 1 || v.EventIDs[0].Validate() != nil || v.Task.ProjectID != v.Blocker.ProjectID || v.Task.ID != v.Blocker.TaskID || v.Task.Version < 2 {
		return invalid("", "INVALID_BLOCKER_MUTATION")
	}
	at := v.Blocker.CreatedAt
	if v.Blocker.ResolvedAt != nil {
		at = *v.Blocker.ResolvedAt
	}
	if !v.Task.UpdatedAt.Time().Equal(at.Time()) {
		return invalid("", "INVALID_BLOCKER_MUTATION")
	}
	return nil
}
func (v TaskBlockerMutation) Clone() TaskBlockerMutation {
	v.Task = v.Task.Clone()
	v.Blocker = v.Blocker.Clone()
	v.EventIDs = slices.Clone(v.EventIDs)
	return v
}
func (v TaskBlockerMutation) MarshalJSON() ([]byte, error) {
	type wire TaskBlockerMutation
	return checkedLimit(wire(v), v.Validate(), MaxTaskBlockerMutationBytes)
}
func (v *TaskBlockerMutation) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	fields, e := taskBlockerFields(raw, MaxTaskBlockerMutationBytes, []string{"task", "blocker", "task_event_id", "event_ids"}, nil)
	if e != nil {
		return e
	}
	var n TaskBlockerMutation
	if e = n.Task.UnmarshalJSON(fields["task"]); e != nil {
		return e
	}
	if e = n.Blocker.UnmarshalJSON(fields["blocker"]); e != nil {
		return e
	}
	if json.Unmarshal(fields["task_event_id"], &n.TaskEventID) != nil || json.Unmarshal(fields["event_ids"], &n.EventIDs) != nil {
		return invalid("", "INVALID_ENCODING")
	}
	if e = n.Validate(); e != nil {
		return e
	}
	*v = n
	return nil
}

type TaskBlockerCommandLookupRequest struct {
	ProjectID      ProjectID              `json:"project_id"`
	Command        TaskBlockerCommandName `json:"command"`
	IdempotencyKey f.IdempotencyKey       `json:"idempotency_key"`
	SemanticDigest f.Digest               `json:"semantic_digest"`
}

func (v TaskBlockerCommandLookupRequest) Validate() error {
	if v.ProjectID.Validate() != nil || v.Command.Validate() != nil || v.IdempotencyKey.Validate() != nil || v.SemanticDigest.Validate() != nil {
		return invalid("", "INVALID_LOOKUP_REQUEST")
	}
	return nil
}
func (v TaskBlockerCommandLookupRequest) Clone() TaskBlockerCommandLookupRequest { return v }
func (v TaskBlockerCommandLookupRequest) MarshalJSON() ([]byte, error) {
	type wire TaskBlockerCommandLookupRequest
	return checkedLimit(wire(v), v.Validate(), MaxTaskBlockerLookupBytes)
}
func (v *TaskBlockerCommandLookupRequest) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	type wire TaskBlockerCommandLookupRequest
	n, e := decodeFieldsLimit[wire](raw, []string{"project_id", "command", "idempotency_key", "semantic_digest"}, nil, nil, MaxTaskBlockerLookupBytes)
	if e != nil {
		return e
	}
	next := TaskBlockerCommandLookupRequest(n)
	if e = next.Validate(); e != nil {
		return e
	}
	*v = next
	return nil
}

type TaskBlockerCommandLookup struct {
	Status  LookupState          `json:"status"`
	Receipt *TaskBlockerMutation `json:"receipt"`
}

func (v TaskBlockerCommandLookup) Validate() error {
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
func (v TaskBlockerCommandLookup) Clone() TaskBlockerCommandLookup {
	if v.Receipt != nil {
		n := v.Receipt.Clone()
		v.Receipt = &n
	}
	return v
}
func (v TaskBlockerCommandLookup) MarshalJSON() ([]byte, error) {
	type wire TaskBlockerCommandLookup
	return checkedLimit(wire(v), v.Validate(), MaxTaskBlockerMutationBytes+1024)
}
func (v *TaskBlockerCommandLookup) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	fields, e := taskBlockerFields(raw, MaxTaskBlockerMutationBytes+1024, []string{"status", "receipt"}, []string{"receipt"})
	if e != nil {
		return e
	}
	var n TaskBlockerCommandLookup
	if json.Unmarshal(fields["status"], &n.Status) != nil {
		return invalid("", "INVALID_ENCODING")
	}
	if !blockerNull(fields["receipt"]) {
		n.Receipt = new(TaskBlockerMutation)
		if e = n.Receipt.UnmarshalJSON(fields["receipt"]); e != nil {
			return e
		}
	}
	if e = n.Validate(); e != nil {
		return e
	}
	*v = n
	return nil
}

type TaskBlockerCommands interface {
	AddTaskBlocker(context.Context, i.Actor, f.CommandMeta, ProjectID, TaskID, TaskBlockerCreate) (TaskBlockerMutation, error)
	ResolveTaskBlocker(context.Context, i.Actor, f.CommandMeta, ProjectID, TaskID, TaskBlockerResolve) (TaskBlockerMutation, error)
	LookupTaskBlockerCommand(context.Context, i.Actor, TaskBlockerCommandLookupRequest) (TaskBlockerCommandLookup, error)
}

func TaskBlockerCommandIdentity(project ProjectID, name TaskBlockerCommandName, key f.IdempotencyKey) (f.CommandIdentity, error) {
	if project.Validate() != nil || name.Validate() != nil || key.Validate() != nil {
		return f.CommandIdentity{}, invalid("", "INVALID_COMMAND")
	}
	return f.NewCommandIdentity("project", []string{project.String()}, string(name), key)
}
func taskBlockerCommandDigest(a i.Actor, m f.CommandMeta, p ProjectID, t TaskID, n TaskBlockerCommandName, r any) (f.Digest, error) {
	if e := ValidateActor(a); e != nil {
		return "", e
	}
	if m.Validate() != nil || m.ExpectedVersion == nil || m.ExpectedVersion.Validate() != nil || p.Validate() != nil || t.Validate() != nil || n.Validate() != nil {
		return "", invalid("", "INVALID_COMMAND")
	}
	raw, e := json.Marshal(struct {
		Format   string                 `json:"format"`
		Command  TaskBlockerCommandName `json:"command"`
		Project  ProjectID              `json:"project_id"`
		Task     TaskID                 `json:"task_id"`
		User     string                 `json:"actor_user_id"`
		Expected f.Version              `json:"expected_version"`
		Request  any                    `json:"request"`
	}{"work-task-blocker-v1", n, p, t, a.Details().UserID, *m.ExpectedVersion, r})
	if e != nil {
		return "", e
	}
	raw, e = cursor.CanonicalJSON(raw)
	if e != nil {
		return "", invalid("", "INVALID_ENCODING")
	}
	sum := sha256.Sum256(raw)
	return f.Digest("sha256:" + hex.EncodeToString(sum[:])), nil
}
func TaskBlockerAddDigest(a i.Actor, m f.CommandMeta, p ProjectID, t TaskID, r TaskBlockerCreate) (f.Digest, error) {
	return taskBlockerCommandDigest(a, m, p, t, TaskBlockerCommandAdd, r)
}
func TaskBlockerResolveDigest(a i.Actor, m f.CommandMeta, p ProjectID, t TaskID, r TaskBlockerResolve) (f.Digest, error) {
	return taskBlockerCommandDigest(a, m, p, t, TaskBlockerCommandResolve, r)
}
func DecodeTaskBlockerMutation(raw []byte) (TaskBlockerMutation, error) {
	var v TaskBlockerMutation
	e := v.UnmarshalJSON(raw)
	return v, e
}
func DecodeTaskBlockerCommandLookupRequest(raw []byte) (TaskBlockerCommandLookupRequest, error) {
	var v TaskBlockerCommandLookupRequest
	e := v.UnmarshalJSON(raw)
	return v, e
}
func DecodeTaskBlockerCommandLookup(raw []byte) (TaskBlockerCommandLookup, error) {
	var v TaskBlockerCommandLookup
	e := v.UnmarshalJSON(raw)
	return v, e
}
func (TaskBlockerCommandName) Format(w fmt.State, r rune)          { blockerSafeFormat(w, r) }
func (TaskBlockerCommandName) LogValue() slog.Value                { return blockerSafeLog() }
func (TaskBlockerMutation) Format(w fmt.State, r rune)             { blockerSafeFormat(w, r) }
func (TaskBlockerMutation) LogValue() slog.Value                   { return blockerSafeLog() }
func (TaskBlockerCommandLookupRequest) Format(w fmt.State, r rune) { blockerSafeFormat(w, r) }
func (TaskBlockerCommandLookupRequest) LogValue() slog.Value       { return blockerSafeLog() }
func (TaskBlockerCommandLookup) Format(w fmt.State, r rune)        { blockerSafeFormat(w, r) }
func (TaskBlockerCommandLookup) LogValue() slog.Value              { return blockerSafeLog() }
