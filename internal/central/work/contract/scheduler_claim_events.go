package contract

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// Scheduler claims have a separate codec. Human schema 1 and its history
// decoder never interpret this projection as an authenticated Human.
const SchedulerClaimSchemaVersion uint32 = 2

type SchedulerClaim struct{}
type SchedulerClaimID = f.ID[SchedulerClaim]

type SchedulerTaskActor struct {
	CauseID string `json:"cause_id"`
}

func (v SchedulerTaskActor) Validate() error {
	if _, err := f.ParseID[SchedulerClaim](v.CauseID); err != nil {
		return invalid("/actor", "INVALID_SCHEDULER_ACTOR")
	}
	return nil
}
func (v SchedulerTaskActor) MarshalJSON() ([]byte, error) {
	return checkedLimit(struct {
		Type        string `json:"type"`
		ServiceName string `json:"service_name"`
		Source      string `json:"source"`
		CauseID     string `json:"cause_id"`
	}{"system", string(i.Scheduler), "scheduler", v.CauseID}, v.Validate(), MaxTaskTransitionActorBytes)
}
func (v *SchedulerTaskActor) UnmarshalJSON(raw []byte) error {
	type wire struct {
		Type        string `json:"type"`
		ServiceName string `json:"service_name"`
		Source      string `json:"source"`
		CauseID     string `json:"cause_id"`
	}
	w, err := decodeFieldsLimit[wire](raw, []string{"type", "service_name", "source", "cause_id"}, nil, nil, MaxTaskTransitionActorBytes)
	if v == nil || err != nil || w.Type != "system" || w.ServiceName != string(i.Scheduler) || w.Source != "scheduler" {
		return invalid("/actor", "INVALID_SCHEDULER_ACTOR")
	}
	next := SchedulerTaskActor{w.CauseID}
	if err = next.Validate(); err != nil {
		return err
	}
	*v = next
	return nil
}

type SchedulerStateChanged struct {
	FromState  TaskState `json:"from_state"`
	ToState    TaskState `json:"to_state"`
	ReasonCode string    `json:"reason_code"`
}

func (v SchedulerStateChanged) Validate() error {
	if v.FromState != TaskStateTodo || v.ToState != TaskStateInProgress || v.ReasonCode != "scheduler_claim" {
		return invalid("/payload", "INVALID_SCHEDULER_TRANSITION")
	}
	return nil
}
func (v SchedulerStateChanged) MarshalJSON() ([]byte, error) {
	type wire SchedulerStateChanged
	return checkedLimit(wire(v), v.Validate(), MaxTaskHistoryPayloadBytes)
}
func (v *SchedulerStateChanged) UnmarshalJSON(raw []byte) error {
	type wire SchedulerStateChanged
	w, err := decodeFieldsLimit[wire](raw, []string{"from_state", "to_state", "reason_code"}, nil, nil, MaxTaskHistoryPayloadBytes)
	if v == nil || err != nil {
		return invalid("/payload", "INVALID_SCHEDULER_TRANSITION")
	}
	next := SchedulerStateChanged(w)
	if err = next.Validate(); err != nil {
		return err
	}
	*v = next
	return nil
}

type SchedulerTaskEvent struct {
	ID            TaskEventID             `json:"id"`
	ProjectID     ProjectID               `json:"project_id"`
	TaskID        TaskID                  `json:"task_id"`
	TaskVersion   f.Version               `json:"task_version"`
	Type          TaskTransitionEventType `json:"type"`
	Actor         SchedulerTaskActor      `json:"actor"`
	OperationID   SchedulerClaimID        `json:"operation_id"`
	CorrelationID SchedulerClaimID        `json:"correlation_id"`
	Payload       SchedulerStateChanged   `json:"payload"`
	CreatedAt     f.Instant               `json:"created_at"`
}

func (v SchedulerTaskEvent) Validate() error {
	if v.ID.Validate() != nil || v.ProjectID.Validate() != nil || v.TaskID.Validate() != nil || v.TaskVersion.Validate() != nil || v.TaskVersion < 2 || v.Type != TaskTransitionStateChanged || v.Actor.Validate() != nil || v.OperationID.Validate() != nil || v.CorrelationID != v.OperationID || v.Actor.CauseID != v.OperationID.String() || v.Payload.Validate() != nil || v.CreatedAt.Validate() != nil {
		return invalid("", "INVALID_SCHEDULER_HISTORY")
	}
	return nil
}
func (v SchedulerTaskEvent) MarshalJSON() ([]byte, error) {
	type wire SchedulerTaskEvent
	return checkedLimit(wire(v), v.Validate(), 16<<10)
}
func (v *SchedulerTaskEvent) UnmarshalJSON(raw []byte) error {
	type wire SchedulerTaskEvent
	w, err := decodeFieldsLimit[wire](raw, []string{"id", "project_id", "task_id", "task_version", "type", "actor", "operation_id", "correlation_id", "payload", "created_at"}, nil, nil, 16<<10)
	if v == nil || err != nil {
		return invalid("", "INVALID_SCHEDULER_HISTORY")
	}
	next := SchedulerTaskEvent(w)
	if err = next.Validate(); err != nil {
		return err
	}
	*v = next
	return nil
}

type SchedulerTaskClaimed struct {
	ClaimID        SchedulerClaimID       `json:"claim_id"`
	Actor          SchedulerTaskActor     `json:"actor"`
	TaskEventID    TaskEventID            `json:"task_event_id"`
	MilestoneID    MilestoneID            `json:"milestone_id"`
	SprintID       SprintID               `json:"sprint_id"`
	AgentID        i.AgentID              `json:"agent_id"`
	SourcePosition TaskTransitionPosition `json:"source_position"`
	TargetPosition TaskTransitionPosition `json:"target_position"`
}

func (v SchedulerTaskClaimed) Validate() error {
	if v.ClaimID.Validate() != nil || v.Actor.Validate() != nil || v.Actor.CauseID != v.ClaimID.String() || v.TaskEventID.Validate() != nil || v.MilestoneID.Validate() != nil || v.SprintID.Validate() != nil || v.AgentID.Validate() != nil || v.SourcePosition.Validate() != nil || v.TargetPosition.Validate() != nil || v.SourcePosition.SprintID != v.SprintID || v.TargetPosition.SprintID != v.SprintID || v.SourcePosition.State != TaskStateTodo || v.TargetPosition.State != TaskStateInProgress || v.SourcePosition.Priority != v.TargetPosition.Priority || v.TargetPosition.NextID != nil {
		return invalid("", "INVALID_SCHEDULER_CLAIMED")
	}
	return nil
}
func (v SchedulerTaskClaimed) Clone() SchedulerTaskClaimed {
	v.SourcePosition = v.SourcePosition.Clone()
	v.TargetPosition = v.TargetPosition.Clone()
	return v
}
func (v SchedulerTaskClaimed) MarshalJSON() ([]byte, error) {
	type wire SchedulerTaskClaimed
	return checkedLimit(wire(v), v.Validate(), 16<<10)
}
func (v *SchedulerTaskClaimed) UnmarshalJSON(raw []byte) error {
	type wire SchedulerTaskClaimed
	w, err := decodeFieldsLimit[wire](raw, []string{"claim_id", "actor", "task_event_id", "milestone_id", "sprint_id", "agent_id", "source_position", "target_position"}, nil, nil, 16<<10)
	if v == nil || err != nil {
		return invalid("", "INVALID_SCHEDULER_CLAIMED")
	}
	next := SchedulerTaskClaimed(w)
	if err = next.Validate(); err != nil {
		return err
	}
	*v = next
	return nil
}

type SchedulerClaimEvents struct {
	catalog *event.Catalog
	claimed event.EventType[SchedulerTaskClaimed]
}

func RegisterSchedulerClaimEvents(catalog *event.Catalog) (SchedulerClaimEvents, error) {
	if !catalog.Valid() {
		return SchedulerClaimEvents{}, invalid("", "INVALID_CATALOG")
	}
	t, err := event.DefineEvent(catalog, event.Definition[SchedulerTaskClaimed]{Schema: event.Schema{Producer: WorkProducer, EventType: TaskTransitionedName, AggregateType: TaskAggregate, Version: SchedulerClaimSchemaVersion}, Codec: event.JSONCodec[SchedulerTaskClaimed]{}, Validate: SchedulerTaskClaimed.Validate})
	if err != nil {
		return SchedulerClaimEvents{}, err
	}
	return SchedulerClaimEvents{catalog, t}, nil
}
func (v SchedulerClaimEvents) Valid() bool {
	return v.catalog.Valid() && v.claimed.Schema() == (event.Schema{Producer: WorkProducer, EventType: TaskTransitionedName, AggregateType: TaskAggregate, Version: SchedulerClaimSchemaVersion})
}
func (v SchedulerClaimEvents) NewTaskClaimed(h event.Header, p SchedulerTaskClaimed) (event.Event, error) {
	if !v.Valid() || h.Validate() != nil || h.EventType != TaskTransitionedName || h.AggregateType != TaskAggregate || h.SchemaVersion != SchedulerClaimSchemaVersion || h.Scope.Kind != event.ProjectScope || h.AggregateVersion == nil || *h.AggregateVersion < 2 || h.AggregateSequence != nil || p.Validate() != nil {
		return event.Event{}, invalid("", "INVALID_SCHEDULER_CLAIMED")
	}
	t, err := f.ParseID[Task](h.AggregateID.String())
	if err != nil || p.SourcePosition.ValidateTarget(t) != nil || p.TargetPosition.ValidateTarget(t) != nil {
		return event.Event{}, invalid("", "INVALID_SCHEDULER_CLAIMED")
	}
	return event.NewEvent(v.claimed, h, p.Clone())
}
func (v SchedulerClaimEvents) Restore(h event.Header, raw []byte) (event.Event, error) {
	var p SchedulerTaskClaimed
	if err := json.Unmarshal(raw, &p); err != nil {
		return event.Event{}, err
	}
	return v.NewTaskClaimed(h, p)
}
func (SchedulerTaskEvent) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "work_scheduler_claim")
}
func (SchedulerTaskEvent) LogValue() slog.Value { return slog.StringValue("work_scheduler_claim") }
