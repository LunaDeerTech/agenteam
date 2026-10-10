package work

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"slices"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// TaskTriggerRecentEvents is a bounded chronological suffix, not the complete
// history. Required Task fields and unresolved blockers are never truncated.
const TaskTriggerRecentEvents = 32

type taskTriggerWire struct {
	SchemaVersion      f.Version         `json:"schema_version"`
	Task               c.Task            `json:"task"`
	Sprint             c.Sprint          `json:"sprint"`
	Milestone          c.Milestone       `json:"milestone"`
	Purpose            string            `json:"purpose"`
	UnresolvedBlockers []c.TaskBlocker   `json:"unresolved_blockers"`
	RecentTaskEvents   []json.RawMessage `json:"recent_task_events"`
}

// TaskTriggerInput is an owned observation. Neither construction nor decoding
// establishes current authority, launch eligibility or a captured Execution.
// Material is available only through the explicit accessors/Data method.
type TaskTriggerInput struct{ data func() taskTriggerWire }

func (v taskTriggerWire) clone() taskTriggerWire {
	v.Task = v.Task.Clone()
	v.Sprint = v.Sprint.Clone()
	v.UnresolvedBlockers = slices.Clone(v.UnresolvedBlockers)
	for n := range v.UnresolvedBlockers {
		v.UnresolvedBlockers[n] = v.UnresolvedBlockers[n].Clone()
	}
	v.RecentTaskEvents = slices.Clone(v.RecentTaskEvents)
	for n := range v.RecentTaskEvents {
		v.RecentTaskEvents[n] = bytes.Clone(v.RecentTaskEvents[n])
	}
	return v
}
func (v taskTriggerWire) validate() error {
	if v.SchemaVersion != 1 || v.Task.Validate() != nil || v.Sprint.Validate() != nil || v.Milestone.Validate() != nil ||
		v.Purpose != "task/work" && v.Purpose != "task/review" || v.Task.ProjectID != v.Sprint.ProjectID || v.Task.ProjectID != v.Milestone.ProjectID ||
		v.Task.SprintID != v.Sprint.ID || v.Task.MilestoneID != v.Milestone.ID || v.Sprint.MilestoneID != v.Milestone.ID ||
		v.UnresolvedBlockers == nil || len(v.UnresolvedBlockers) > 256 || v.RecentTaskEvents == nil || len(v.RecentTaskEvents) == 0 || len(v.RecentTaskEvents) > TaskTriggerRecentEvents {
		return fault(f.InvalidArgument)
	}
	seen := map[string]bool{}
	for n, b := range v.UnresolvedBlockers {
		if b.Validate() != nil || b.ResolvedAt != nil || b.ProjectID != v.Task.ProjectID || b.TaskID != v.Task.ID || seen[b.ID.String()] {
			return fault(f.InvalidArgument)
		}
		seen[b.ID.String()] = true
		if n > 0 {
			p := v.UnresolvedBlockers[n-1]
			if b.CreatedAt.Time().Before(p.CreatedAt.Time()) || b.CreatedAt.Time().Equal(p.CreatedAt.Time()) && b.ID.String() <= p.ID.String() {
				return fault(f.InvalidArgument)
			}
		}
	}
	seen = map[string]bool{}
	var previous taskTriggerEventIdentity
	for n, raw := range v.RecentTaskEvents {
		event, err := decodeTaskTriggerEvent(raw)
		if err != nil || event.project != v.Task.ProjectID || event.task != v.Task.ID || event.version > v.Task.Version || seen[event.id] {
			return fault(f.InvalidArgument)
		}
		seen[event.id] = true
		if n > 0 && (event.at.Time().Before(previous.at.Time()) || event.at.Time().Equal(previous.at.Time()) && event.id <= previous.id) {
			return fault(f.InvalidArgument)
		}
		previous = event
	}
	return nil
}
func newTaskTriggerInput(w taskTriggerWire) (TaskTriggerInput, error) {
	if err := w.validate(); err != nil {
		return TaskTriggerInput{}, err
	}
	raw, err := json.Marshal(w)
	if err != nil {
		return TaskTriggerInput{}, internal(err)
	}
	if len(raw) > ec.MaxTriggerInputBytes {
		return TaskTriggerInput{}, fault(f.PayloadTooLarge)
	}
	owned := w.clone()
	return TaskTriggerInput{data: func() taskTriggerWire { return owned.clone() }}, nil
}
func (v TaskTriggerInput) Validate() error {
	if v.data == nil {
		return fault(f.InvalidArgument)
	}
	return v.data().validate()
}
func (v TaskTriggerInput) Task() c.Task {
	if v.data == nil {
		return c.Task{}
	}
	return v.data().Task
}
func (v TaskTriggerInput) Sprint() c.Sprint {
	if v.data == nil {
		return c.Sprint{}
	}
	return v.data().Sprint
}
func (v TaskTriggerInput) Milestone() c.Milestone {
	if v.data == nil {
		return c.Milestone{}
	}
	return v.data().Milestone
}
func (v TaskTriggerInput) Purpose() string {
	if v.data == nil {
		return ""
	}
	return v.data().Purpose
}
func (v TaskTriggerInput) UnresolvedBlockers() []c.TaskBlocker {
	if v.data == nil {
		return nil
	}
	return v.data().UnresolvedBlockers
}
func (v TaskTriggerInput) RecentTaskEvents() []json.RawMessage {
	if v.data == nil {
		return nil
	}
	return v.data().RecentTaskEvents
}
func (v TaskTriggerInput) Data() ([]byte, error) {
	if v.data == nil {
		return nil, fault(f.InvalidArgument)
	}
	return json.Marshal(v.data())
}
func DecodeTaskTriggerInput(raw []byte) (TaskTriggerInput, error) {
	if _, err := taskPrivateObject(raw, ec.MaxTriggerInputBytes, []string{"schema_version", "task", "sprint", "milestone", "purpose", "unresolved_blockers", "recent_task_events"}, nil); err != nil {
		return TaskTriggerInput{}, fault(f.InvalidArgument)
	}
	var w taskTriggerWire
	if err := json.Unmarshal(raw, &w); err != nil {
		return TaskTriggerInput{}, fault(f.InvalidArgument)
	}
	return newTaskTriggerInput(w)
}
func (TaskTriggerInput) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "task_trigger_input") }
func (TaskTriggerInput) LogValue() slog.Value         { return slog.StringValue("task_trigger_input") }
func (TaskTriggerInput) MarshalJSON() ([]byte, error) { return []byte(`"task_trigger_input"`), nil }
func (*TaskTriggerInput) UnmarshalJSON([]byte) error  { return fault(f.InvalidArgument) }

type taskTriggerEventIdentity struct {
	id      string
	project c.ProjectID
	task    c.TaskID
	version f.Version
	at      f.Instant
}

func decodeTaskTriggerEvent(raw []byte) (taskTriggerEventIdentity, error) {
	fields, err := taskPrivateObject(raw, c.MaxTaskEventBytes, []string{"id", "project_id", "task_id", "task_version", "type", "actor", "operation_id", "correlation_id", "payload", "created_at"}, nil)
	if err != nil {
		return taskTriggerEventIdentity{}, fault(f.InvalidArgument)
	}
	var kind string
	if json.Unmarshal(fields["type"], &kind) != nil {
		return taskTriggerEventIdentity{}, fault(f.InvalidArgument)
	}
	switch kind {
	case string(c.TaskEventCreated), string(c.TaskEventFieldsUpdated):
		var e c.TaskEvent
		if e.UnmarshalJSON(raw) != nil {
			return taskTriggerEventIdentity{}, fault(f.InvalidArgument)
		}
		return taskTriggerEventIdentity{e.ID.String(), e.ProjectID, e.TaskID, e.TaskVersion, e.CreatedAt}, nil
	case string(c.TaskBlockerEventAdded), string(c.TaskBlockerEventResolved):
		var e c.TaskBlockerEvent
		if e.UnmarshalJSON(raw) != nil {
			return taskTriggerEventIdentity{}, fault(f.InvalidArgument)
		}
		return taskTriggerEventIdentity{e.ID.String(), e.ProjectID, e.TaskID, e.TaskVersion, e.CreatedAt}, nil
	default:
		// Future transition history needs its real writer/storage contract before
		// it can be read here; an unknown event is not silently dropped.
		return taskTriggerEventIdentity{}, fault(f.SchemaUnsupported)
	}
}

type capturedTaskWire struct {
	SchemaVersion f.Version       `json:"schema_version"`
	ExecutionID   i.ExecutionID   `json:"execution_id"`
	LaunchDigest  f.Digest        `json:"launch_digest"`
	Input         taskTriggerWire `json:"input"`
}

// DecodeCapturedTaskInput is the Build-side input step. It only consumes the
// exact captured bytes and original request; it never reads a newer Task or
// grants execution. Persistence/commit of the reference belongs to Execution.
func DecodeCapturedTaskInput(v ec.CapturedTriggerInput, request ec.PreparationRequest) (TaskTriggerInput, error) {
	if v.Validate() != nil || request.Validate() != nil || request.Launch.Trigger.Kind != "task" || v.Ref().ProviderType != "task" || v.Ref().SchemaVersion != 1 {
		return TaskTriggerInput{}, fault(f.InvalidArgument)
	}
	raw := v.Data()
	if ec.TriggerInputDigest(raw) != v.Ref().Digest {
		return TaskTriggerInput{}, fault(f.InvalidArgument)
	}
	fields, err := taskPrivateObject(raw, ec.MaxTriggerInputBytes, []string{"schema_version", "execution_id", "launch_digest", "input"}, nil)
	if err != nil {
		return TaskTriggerInput{}, fault(f.InvalidArgument)
	}
	var wire capturedTaskWire
	if json.Unmarshal(raw, &wire) != nil {
		return TaskTriggerInput{}, fault(f.InvalidArgument)
	}
	expected, _ := request.Launch.Digest()
	if wire.SchemaVersion != 1 || wire.ExecutionID != request.ExecutionID || wire.LaunchDigest != expected {
		return TaskTriggerInput{}, fault(f.InvalidArgument)
	}
	input, err := DecodeTaskTriggerInput(fields["input"])
	if err != nil {
		return TaskTriggerInput{}, err
	}
	if err = taskCaptureEligible(input, request.Launch); err != nil {
		return TaskTriggerInput{}, err
	}
	return input, nil
}
