package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strings"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const (
	TaskTransitionedName        event.StableName = "work.task_transitioned"
	TaskTransitionSchemaVersion uint32           = 1
)

// TaskTransitioned is a small typed envelope. Its shape does not establish that
// the referenced history, Task, positions or current authority exist.
type TaskTransitioned struct {
	CommandID      TaskTransitionCommandID     `json:"command_id"`
	Actor          TaskTransitionActor         `json:"actor"`
	TaskEventIDs   []TaskEventID               `json:"task_event_ids"`
	MilestoneID    MilestoneID                 `json:"milestone_id"`
	SprintID       SprintID                    `json:"sprint_id"`
	FromState      TaskState                   `json:"from_state"`
	ToState        TaskState                   `json:"to_state"`
	AssigneeChange *TaskAssigneeChangedPayload `json:"assignee_change"`
	SourcePosition TaskTransitionPosition      `json:"source_position"`
	TargetPosition TaskTransitionPosition      `json:"target_position"`
}

func (v TaskTransitioned) Validate() error {
	if v.CommandID.Validate() != nil || v.Actor.Validate() != nil ||
		v.MilestoneID.Validate() != nil || v.SprintID.Validate() != nil ||
		(TaskStateChangedPayload{FromState: v.FromState, ToState: v.ToState}).Validate() != nil {
		return invalid("", "INVALID_TASK_TRANSITIONED")
	}
	if err := validateTaskTransitionEventIDs(v.TaskEventIDs); err != nil {
		return err
	}
	if v.AssigneeChange != nil && v.AssigneeChange.Validate() != nil {
		return invalid("/assignee_change", "INVALID_ASSIGNEE_CHANGE")
	}
	if v.SourcePosition.Validate() != nil || v.TargetPosition.Validate() != nil ||
		v.SourcePosition.SprintID != v.SprintID || v.TargetPosition.SprintID != v.SprintID ||
		v.SourcePosition.State != v.FromState || v.TargetPosition.State != v.ToState ||
		v.SourcePosition.Priority != v.TargetPosition.Priority || v.TargetPosition.NextID != nil {
		return invalid("/position", "INVALID_POSITION")
	}
	return nil
}

func (v TaskTransitioned) Clone() TaskTransitioned {
	v.TaskEventIDs = slices.Clone(v.TaskEventIDs)
	if v.AssigneeChange != nil {
		change := v.AssigneeChange.Clone()
		v.AssigneeChange = &change
	}
	v.SourcePosition = v.SourcePosition.Clone()
	v.TargetPosition = v.TargetPosition.Clone()
	return v
}

func (v TaskTransitioned) MarshalJSON() ([]byte, error) {
	type wire TaskTransitioned
	return checkedLimit(wire(v), v.Validate(), MaxTaskTransitionedBytes)
}

func (v *TaskTransitioned) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	fields, err := decodeFieldsLimit[map[string]json.RawMessage](raw,
		[]string{"command_id", "actor", "task_event_ids", "milestone_id", "sprint_id", "from_state", "to_state", "assignee_change", "source_position", "target_position"},
		nil, []string{"assignee_change"}, MaxTaskTransitionedBytes)
	if err != nil {
		return err
	}
	var next TaskTransitioned
	// Keep each nested original token intact until its own bounded decoder has
	// checked it. A typed outer json.Unmarshal would hide nested raw failures.
	if err = next.Actor.UnmarshalJSON(fields["actor"]); err != nil {
		return err
	}
	if err = next.SourcePosition.UnmarshalJSON(fields["source_position"]); err != nil {
		return err
	}
	if err = next.TargetPosition.UnmarshalJSON(fields["target_position"]); err != nil {
		return err
	}
	if !bytes.Equal(bytes.TrimSpace(fields["assignee_change"]), []byte("null")) {
		next.AssigneeChange = new(TaskAssigneeChangedPayload)
		if err = next.AssigneeChange.UnmarshalJSON(fields["assignee_change"]); err != nil {
			return err
		}
	}
	for _, field := range []struct {
		key   string
		value any
	}{
		{"command_id", &next.CommandID}, {"task_event_ids", &next.TaskEventIDs},
		{"milestone_id", &next.MilestoneID}, {"sprint_id", &next.SprintID},
		{"from_state", &next.FromState}, {"to_state", &next.ToState},
	} {
		if json.Unmarshal(fields[field.key], field.value) != nil {
			return invalid("", "INVALID_ENCODING")
		}
	}
	if err = next.Validate(); err != nil {
		return err
	}
	*v = next
	return nil
}

// DecodeTaskTransitioned bounds the complete raw argument before any generic
// event canonicalization or encoding/json whitespace removal can occur.
func DecodeTaskTransitioned(raw []byte) (TaskTransitioned, error) {
	var v TaskTransitioned
	if err := v.UnmarshalJSON(raw); err != nil {
		return TaskTransitioned{}, err
	}
	return v, nil
}

type TaskTransitionEvents struct {
	catalog *event.Catalog
	task    event.EventType[TaskTransitioned]
}

func RegisterTaskTransitionEvents(catalog *event.Catalog) (TaskTransitionEvents, error) {
	if !catalog.Valid() {
		return TaskTransitionEvents{}, invalid("", "INVALID_CATALOG")
	}
	t, err := event.DefineEvent(catalog, event.Definition[TaskTransitioned]{
		Schema: event.Schema{Producer: WorkProducer, EventType: TaskTransitionedName, AggregateType: TaskAggregate, Version: TaskTransitionSchemaVersion},
		Codec:  event.JSONCodec[TaskTransitioned]{}, Validate: TaskTransitioned.Validate,
	})
	if err != nil {
		return TaskTransitionEvents{}, err
	}
	return TaskTransitionEvents{catalog: catalog, task: t}, nil
}

func (v TaskTransitionEvents) Valid() bool {
	return v.catalog.Valid() && v.task.Schema() == (event.Schema{
		Producer: WorkProducer, EventType: TaskTransitionedName,
		AggregateType: TaskAggregate, Version: TaskTransitionSchemaVersion,
	})
}

func validTaskTransitionHeader(h event.Header) error {
	if h.Validate() != nil || h.EventType != TaskTransitionedName ||
		h.AggregateType != TaskAggregate || h.SchemaVersion != TaskTransitionSchemaVersion ||
		h.Scope.Kind != event.ProjectScope || h.AggregateVersion == nil ||
		*h.AggregateVersion < 2 || h.AggregateSequence != nil {
		return invalid("", "INVALID_EVENT_HEADER")
	}
	return nil
}

func (v TaskTransitionEvents) NewTaskTransitioned(h event.Header, p TaskTransitioned) (event.Event, error) {
	if !v.Valid() {
		return event.Event{}, invalid("", "INVALID_CATALOG")
	}
	if err := validTaskTransitionHeader(h); err != nil {
		return event.Event{}, err
	}
	if err := p.Validate(); err != nil {
		return event.Event{}, err
	}
	id, err := f.ParseID[Task](h.AggregateID.String())
	if err != nil {
		return event.Event{}, invalid("", "INVALID_EVENT_HEADER")
	}
	if err = p.SourcePosition.ValidateTarget(id); err != nil {
		return event.Event{}, err
	}
	if err = p.TargetPosition.ValidateTarget(id); err != nil {
		return event.Event{}, err
	}
	return event.NewEvent(v.task, h, p.Clone())
}

func (v TaskTransitionEvents) Restore(h event.Header, raw []byte) (event.Event, error) {
	if !v.Valid() {
		return event.Event{}, invalid("", "INVALID_CATALOG")
	}
	if h.EventType != TaskTransitionedName || h.AggregateType != TaskAggregate || h.SchemaVersion != TaskTransitionSchemaVersion {
		return event.Event{}, f.NewFault(f.SchemaUnsupported, f.NotStarted)
	}
	p, err := DecodeTaskTransitioned(raw)
	if err != nil {
		return event.Event{}, err
	}
	return v.NewTaskTransitioned(h, p)
}

func (v TaskTransitionEvents) DecodeTaskTransitioned(e event.Event) (TaskTransitioned, error) {
	if !v.Valid() || !v.catalog.Owns(e) || e.Summary().Producer != WorkProducer {
		return TaskTransitioned{}, invalid("", "FOREIGN_EVENT")
	}
	p, err := event.DecodeEvent(v.task, e)
	if err != nil {
		return TaskTransitioned{}, err
	}
	if _, err = v.NewTaskTransitioned(e.Header(), p); err != nil {
		return TaskTransitioned{}, err
	}
	return p.Clone(), nil
}

// NewTaskTransitionData checks caller-supplied data only. It does not load or
// persist history, authorize a transition, validate current Agent/Blocker/graph
// facts, or issue an append plan. Catalog ownership only establishes schema.
func (v TaskTransitionEvents) NewTaskTransitionData(h event.Header, before, after Task,
	request TaskTransfer, history []TaskTransitionEvent,
	source, target TaskTransitionPosition) (TaskTransitionMutation, event.Event, error) {
	fail := func(err error) (TaskTransitionMutation, event.Event, error) {
		return TaskTransitionMutation{}, event.Event{}, err
	}
	bad := func() (TaskTransitionMutation, event.Event, error) {
		return fail(invalid("", "INVALID_TASK_TRANSITION_DATA"))
	}
	if !v.Valid() || validTaskTransitionHeader(h) != nil || before.Validate() != nil || after.Validate() != nil {
		return bad()
	}
	if err := request.Validate(); err != nil {
		return fail(err)
	}
	if before.ID != after.ID || before.ProjectID != after.ProjectID ||
		before.Version >= f.Version(math.MaxInt64) || after.Version != before.Version+1 ||
		before.MilestoneID != after.MilestoneID || before.SprintID != after.SprintID ||
		before.Title != after.Title || before.Description != after.Description ||
		before.Type != after.Type || before.Priority != after.Priority || before.Plan != after.Plan ||
		!before.CreatedAt.Time().Equal(after.CreatedAt.Time()) ||
		after.UpdatedAt.Time().Before(before.UpdatedAt.Time()) || !after.UpdatedAt.Time().Equal(h.OccurredAt.Time()) ||
		h.Scope.ProjectID.String() != after.ProjectID.String() || h.AggregateID.String() != after.ID.String() ||
		*h.AggregateVersion != after.Version || request.TargetState != after.State ||
		(TaskStateChangedPayload{FromState: before.State, ToState: after.State}).Validate() != nil {
		return bad()
	}
	assignee := before.AssigneeAgentID
	if request.AssigneeAgentID != nil {
		assignee = request.AssigneeAgentID
	}
	if !taskTransitionSameAssignee(assignee, after.AssigneeAgentID) {
		return bad()
	}
	handoff := before.State == TaskStateInProgress && after.State == TaskStateInReview ||
		before.State == TaskStateInReview && after.State == TaskStateTodo
	commentRequired := handoff || before.State == TaskStateInReview &&
		(after.State == TaskStateDone || after.State == TaskStateBlocked)
	if handoff && request.AssigneeAgentID == nil || commentRequired && request.Comment == nil {
		return bad()
	}
	if source.SprintID != before.SprintID || source.State != before.State || source.Priority != before.Priority ||
		target.SprintID != after.SprintID || target.State != after.State || target.Priority != after.Priority ||
		source.ValidateTarget(before.ID) != nil || target.ValidateTarget(after.ID) != nil || target.NextID != nil {
		return bad()
	}
	changedAssignee := !taskTransitionSameAssignee(before.AssigneeAgentID, after.AssigneeAgentID)
	expected := 1 + len(request.ResolveBlockerIDs) + len(request.AddBlockers)
	if changedAssignee {
		expected++
	}
	if request.Comment != nil {
		expected++
	}
	if len(history) != expected || len(history) > 35 {
		return bad()
	}
	ids := make([]TaskEventID, len(history))
	for n, fact := range history {
		if err := fact.Validate(); err != nil {
			return fail(err)
		}
		if fact.ProjectID != after.ProjectID || fact.TaskID != after.ID || fact.TaskVersion != after.Version ||
			fact.Actor != history[0].Actor || fact.OperationID != history[0].OperationID ||
			fact.CorrelationID != history[0].OperationID || !fact.CreatedAt.Time().Equal(h.OccurredAt.Time()) {
			return bad()
		}
		ids[n] = fact.ID
	}
	if err := validateTaskTransitionEventIDs(ids); err != nil {
		return fail(err)
	}
	if history[0].Type != TaskTransitionStateChanged ||
		history[0].Payload.StateChanged.FromState != before.State || history[0].Payload.StateChanged.ToState != after.State {
		return bad()
	}
	index := 1
	var assigneeChange *TaskAssigneeChangedPayload
	if changedAssignee {
		fact := history[index]
		if fact.Type != TaskTransitionAssigneeChanged || after.AssigneeAgentID == nil ||
			!taskTransitionSameAssignee(fact.Payload.AssigneeChanged.FromAgentID, before.AssigneeAgentID) ||
			fact.Payload.AssigneeChanged.ToAgentID != *after.AssigneeAgentID {
			return bad()
		}
		change := fact.Payload.AssigneeChanged.Clone()
		assigneeChange = &change
		index++
	}
	resolves := slices.Clone(request.ResolveBlockerIDs)
	slices.SortFunc(resolves, func(a, b TaskBlockerID) int { return strings.Compare(a.String(), b.String()) })
	for _, id := range resolves {
		fact := history[index]
		if fact.Type != TaskTransitionBlockerResolved || fact.Payload.BlockerResolved.BlockerID != id ||
			fact.Payload.BlockerResolved.ResolutionComment != nil {
			return bad()
		}
		index++
	}
	adds := slices.Clone(request.AddBlockers)
	slices.SortFunc(adds, func(a, b TaskBlockerCreate) int { return strings.Compare(a.BlockerID.String(), b.BlockerID.String()) })
	for _, item := range adds {
		fact := history[index]
		if fact.Type != TaskTransitionBlockerAdded || fact.Payload.BlockerAdded.BlockerID != item.BlockerID ||
			fact.Payload.BlockerAdded.BlockerType != item.Type {
			return bad()
		}
		index++
	}
	if request.Comment != nil {
		if history[index].Type != TaskTransitionComment || history[index].Payload.Comment.Body != *request.Comment {
			return bad()
		}
	}
	p := TaskTransitioned{
		CommandID: history[0].OperationID, Actor: history[0].Actor.Clone(), TaskEventIDs: ids,
		MilestoneID: after.MilestoneID, SprintID: after.SprintID, FromState: before.State, ToState: after.State,
		AssigneeChange: assigneeChange, SourcePosition: source.Clone(), TargetPosition: target.Clone(),
	}
	e, err := v.NewTaskTransitioned(h, p)
	if err != nil {
		return fail(err)
	}
	result := TaskTransitionMutation{Task: after.Clone(), TaskEventIDs: slices.Clone(ids), EventIDs: []event.EventID{h.EventID}}
	if err := result.Validate(); err != nil {
		return fail(err)
	}
	return result, e, nil
}

func taskTransitionSameAssignee(a, b *i.AgentID) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func (TaskTransitioned) Format(w fmt.State, _ rune)     { taskTransitionSafeFormat(w) }
func (TaskTransitioned) LogValue() slog.Value           { return slog.StringValue("work_task_transition") }
func (TaskTransitionEvents) Format(w fmt.State, _ rune) { taskTransitionSafeFormat(w) }
func (TaskTransitionEvents) LogValue() slog.Value       { return slog.StringValue("work_task_transition") }
