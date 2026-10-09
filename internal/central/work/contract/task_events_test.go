package contract

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func taskEventFixture(t *testing.T) (TaskEvents, event.Header, TaskChanged) {
	v := taskFixture(t)
	catalog := event.NewCatalog()
	if _, err := RegisterWorkEvents(catalog); err != nil {
		t.Fatal(err)
	}
	typed, err := RegisterTaskEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	version := f.Version(1)
	h := event.Header{EventID: testID[event.EventIdentity](t, 22), EventType: TaskChangedName, SchemaVersion: TaskSchemaVersion, OccurredAt: v.UpdatedAt, Scope: event.Scope{Kind: event.ProjectScope, ProjectID: testID[event.Project](t, 5)}, AggregateType: TaskAggregate, AggregateID: testID[event.Aggregate](t, 20), AggregateVersion: &version}
	p := TaskChanged{CommandID: testID[TaskCommand](t, 23), ActorUserID: testID[i.User](t, 1), TaskEventID: testID[TaskEvent](t, 21), MilestoneID: v.MilestoneID, SprintID: v.SprintID, Change: TaskCreatedChange, ChangedFields: []TaskChangedField{TaskDescriptionChanged, TaskRankChanged, TaskPlanChanged, TaskPriorityChanged, TaskTitleChanged, TaskTypeChanged}, Position: &TaskPosition{SprintID: v.SprintID, State: TaskStateBacklog, Priority: v.Priority, OrderGeneration: 2}}
	return typed, h, p
}
func taskHistoryFixture(t *testing.T) TaskEvent {
	v := taskFixture(t)
	created := TaskCreatedPayload{InitialState: v.State, MilestoneID: v.MilestoneID, SprintID: v.SprintID, Type: v.Type, Priority: v.Priority}
	id := testID[TaskCommand](t, 23)
	return TaskEvent{ID: testID[TaskEvent](t, 21), ProjectID: v.ProjectID, TaskID: v.ID, TaskVersion: 1, Type: TaskEventCreated, Actor: TaskEventActor{Type: i.Human, UserID: testID[i.User](t, 1), Source: "task_domain"}, OperationID: id, CorrelationID: id, Payload: taskRaw(t, created), CreatedAt: v.UpdatedAt}
}
func TestTaskTypedEventSchemaHeaderAndCatalog(t *testing.T) {
	typed, h, p := taskEventFixture(t)
	ev, err := typed.NewTaskChanged(h, p)
	if err != nil {
		t.Fatal(err)
	}
	p.ChangedFields[0] = TaskTitleChanged
	p.Position.OrderGeneration = 99
	decoded, err := typed.DecodeTaskChanged(ev)
	if err != nil || decoded.Position.OrderGeneration != 2 || decoded.ChangedFields[0] != TaskDescriptionChanged {
		t.Fatal("event borrowed payload", err)
	}
	restored, err := typed.Restore(ev.Header(), ev.PayloadBytes())
	if err != nil || restored.Summary().PayloadDigest != ev.Summary().PayloadDigest {
		t.Fatal("typed restore", err)
	}
	other, _, _ := taskEventFixture(t)
	if _, err = other.DecodeTaskChanged(ev); err == nil {
		t.Fatal("foreign catalog")
	}
	for _, alter := range []func(*event.Header){func(v *event.Header) { v.EventType = MilestoneChangedName }, func(v *event.Header) { v.AggregateType = SprintAggregate }, func(v *event.Header) { v.SchemaVersion = 2 }, func(v *event.Header) { v.Scope = event.Scope{Kind: event.SystemScope} }, func(v *event.Header) { v.AggregateVersion = nil }, func(v *event.Header) { n := f.Version(2); v.AggregateVersion = &n }, func(v *event.Header) { n := f.Sequence(1); v.AggregateSequence = &n }} {
		head := ev.Header()
		alter(&head)
		if _, err = typed.Restore(head, ev.PayloadBytes()); err == nil {
			t.Fatal("invalid typed header accepted")
		}
	}
	self := taskFixture(t).ID
	decoded.Position.PreviousID = &self
	if _, err = typed.NewTaskChanged(h, decoded); err == nil {
		t.Fatal("self neighbor")
	}
	var zero TaskEvents
	if zero.Valid() {
		t.Fatal("zero TaskEvents valid")
	}
	if _, err = zero.Restore(h, ev.PayloadBytes()); err == nil {
		t.Fatal("zero registry restores")
	}
	if _, err = RegisterTaskEvents(nil); err == nil {
		t.Fatal("nil catalog accepted")
	}
	catalog := event.NewCatalog()
	if _, err = RegisterTaskEvents(catalog); err != nil {
		t.Fatal(err)
	}
	if _, err = RegisterTaskEvents(catalog); err == nil {
		t.Fatal("duplicate schema accepted")
	}
	sealed := event.NewCatalog()
	if err = sealed.Seal(); err != nil {
		t.Fatal(err)
	}
	if _, err = RegisterTaskEvents(sealed); err == nil {
		t.Fatal("sealed catalog accepted")
	}
}
func TestTaskTypedChangesAndHistoryPosition(t *testing.T) {
	typed, h, created := taskEventFixture(t)
	n := f.Version(2)
	h.AggregateVersion = &n
	updated := created.Clone()
	updated.Change = TaskUpdatedChange
	updated.ChangedFields = []TaskChangedField{TaskPlanChanged, TaskTitleChanged}
	updated.Position = nil
	if _, err := typed.NewTaskChanged(h, updated); err != nil {
		t.Fatal(err)
	}
	priority := updated.Clone()
	priority.ChangedFields = []TaskChangedField{TaskPriorityChanged}
	priority.Position = created.Position
	if _, err := typed.NewTaskChanged(h, priority); err != nil {
		t.Fatal("priority update", err)
	}
	reorder := priority.Clone()
	reorder.Change = TaskReorderedChange
	reorder.ChangedFields = []TaskChangedField{TaskRankChanged}
	next := testID[Task](t, 42)
	reorder.Position.NextID = &next
	if _, err := typed.NewTaskChanged(h, reorder); err != nil {
		t.Fatal("reorder", err)
	}
	for _, alter := range []func(*TaskChanged){func(v *TaskChanged) { v.Change = "deleted" }, func(v *TaskChanged) { v.ChangedFields = nil }, func(v *TaskChanged) { v.ChangedFields = []TaskChangedField{TaskTitleChanged, TaskPlanChanged} }, func(v *TaskChanged) { v.ChangedFields = []TaskChangedField{TaskTitleChanged, TaskTitleChanged} }, func(v *TaskChanged) { v.ChangedFields = []TaskChangedField{"state"} }, func(v *TaskChanged) { v.ChangedFields = []TaskChangedField{TaskRankChanged} }, func(v *TaskChanged) { v.Position = created.Position }, func(v *TaskChanged) { v.CommandID = TaskCommandID{} }, func(v *TaskChanged) { v.TaskEventID = TaskEventID{} }, func(v *TaskChanged) { v.ActorUserID = i.UserID{} }, func(v *TaskChanged) { v.MilestoneID = MilestoneID{} }} {
		bad := updated.Clone()
		alter(&bad)
		if _, err := typed.NewTaskChanged(h, bad); err == nil {
			t.Fatal("invalid changed payload")
		}
	}
	p := TaskFieldsUpdatedPayload{ChangedFields: []TaskChangedField{TaskPlanChanged, TaskPriorityChanged, TaskTypeChanged}, TypeChange: &TaskTypeChange{From: TaskTypeFeature, To: TaskTypeBug}, PriorityChange: &TaskPriorityChange{From: TaskPriorityLow, To: TaskPriorityHigh}, Position: created.Position.ClonePtrForTest()}
	p.Position.NextID = nil
	if p.Validate() != nil {
		t.Fatal("valid combined history")
	}
	for _, alter := range []func(*TaskFieldsUpdatedPayload){func(v *TaskFieldsUpdatedPayload) { v.TypeChange = nil }, func(v *TaskFieldsUpdatedPayload) { v.TypeChange.To = v.TypeChange.From }, func(v *TaskFieldsUpdatedPayload) { v.PriorityChange = nil }, func(v *TaskFieldsUpdatedPayload) { v.PriorityChange.To = v.PriorityChange.From }, func(v *TaskFieldsUpdatedPayload) { v.Position = nil }, func(v *TaskFieldsUpdatedPayload) { v.Position.Priority = TaskPriorityLow }, func(v *TaskFieldsUpdatedPayload) { v.Position.NextID = &next }, func(v *TaskFieldsUpdatedPayload) { v.Position.State = TaskStateTodo }, func(v *TaskFieldsUpdatedPayload) {
		v.ChangedFields = []TaskChangedField{TaskRankChanged, TaskTypeChanged}
	}} {
		bad := p.Clone()
		alter(&bad)
		if bad.Validate() == nil {
			t.Fatal("invalid history field/change relationship")
		}
	}
	history := taskHistoryFixture(t)
	history.TaskVersion = 2
	history.Type = TaskEventFieldsUpdated
	history.Payload = taskRaw(t, p)
	if history.Validate() != nil {
		t.Fatal("fields_updated record")
	}
	rank := TaskFieldsUpdatedPayload{ChangedFields: []TaskChangedField{TaskRankChanged}, Position: created.Position.ClonePtrForTest()}
	rank.Position.NextID = &next
	history.Payload = taskRaw(t, rank)
	if history.Validate() != nil {
		t.Fatal("reorder history")
	}
	rank.Position.PreviousID = &history.TaskID
	history.Payload = taskRaw(t, rank)
	if history.Validate() == nil {
		t.Fatal("history self neighbor")
	}
	for _, secret := range []string{`"title":`, `"description":`, `"plan":`, `"manual_rank":`, `"session_id":`, `"idempotency_key":`} {
		if bytes.Contains(taskRaw(t, reorder), []byte(secret)) {
			t.Fatal("event includes body/key/rank")
		}
	}
}
func (v TaskPosition) ClonePtrForTest() *TaskPosition { next := v.Clone(); return &next }
func TestTaskHistoryStrictRecordAndCaps(t *testing.T) {
	good := taskHistoryFixture(t)
	raw := taskRaw(t, good)
	var restored TaskEvent
	if json.Unmarshal(raw, &restored) != nil || !reflect.DeepEqual(good, restored) {
		t.Fatal("history roundtrip")
	}
	copy := restored.Clone()
	copy.Payload[0] = '['
	if bytes.Equal(copy.Payload, restored.Payload) {
		t.Fatal("history clone aliases")
	}
	for _, key := range strings.Fields("id project_id task_id task_version type actor operation_id correlation_id payload created_at") {
		for _, replacement := range []json.RawMessage{nil, json.RawMessage("null")} {
			if json.Unmarshal(taskJSONChange(t, raw, key, replacement), new(TaskEvent)) == nil {
				t.Fatal("missing/null history field", key)
			}
		}
	}
	for _, replacement := range []struct{ key, value string }{{"task_version", "2"}, {"task_version", `"01"`}, {"task_version", `"2"`}, {"type", `"plan_updated"`}, {"correlation_id", `"` + testID[TaskCommand](t, 77).String() + `"`}, {"ACTOR", `{}`}, {"body", `"secret"`}} {
		if json.Unmarshal(taskJSONChange(t, raw, replacement.key, json.RawMessage(replacement.value)), new(TaskEvent)) == nil {
			t.Fatal("invalid history record")
		}
	}
	for _, bad := range []string{`{"type":"human","user_id":"` + good.Actor.UserID.String() + `","source":"other"}`, `{"type":"service","user_id":"` + good.Actor.UserID.String() + `","source":"task_domain"}`, `{"type":"human","user_id":"` + good.Actor.UserID.String() + `","source":"task_domain","session_id":"secret"}`} {
		if json.Unmarshal(taskJSONChange(t, raw, "actor", json.RawMessage(bad)), new(TaskEvent)) == nil {
			t.Fatal("invalid history actor")
		}
	}
	_, _, changed := taskEventFixture(t)
	payload := TaskFieldsUpdatedPayload{ChangedFields: []TaskChangedField{TaskTitleChanged}}
	created := TaskCreatedPayload{InitialState: TaskStateBacklog, MilestoneID: changed.MilestoneID, SprintID: changed.SprintID, Type: TaskTypeFeature, Priority: TaskPriorityHigh}
	for _, tc := range []struct {
		v      any
		cap    int
		decode func([]byte) error
	}{{good, MaxTaskEventBytes, new(TaskEvent).UnmarshalJSON}, {created, MaxTaskHistoryPayloadBytes, new(TaskCreatedPayload).UnmarshalJSON}, {payload, MaxTaskHistoryPayloadBytes, new(TaskFieldsUpdatedPayload).UnmarshalJSON}, {changed, MaxTaskChangedBytes, new(TaskChanged).UnmarshalJSON}} {
		wire := taskRaw(t, tc.v)
		at := append([]byte(strings.Repeat(" ", tc.cap-len(wire))), wire...)
		if tc.decode(at) != nil || tc.decode(append([]byte(" "), at...)) == nil {
			t.Fatal("event raw cap")
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(wire, &fields); err != nil {
			t.Fatal(err)
		}
		for key := range fields {
			if tc.decode(taskJSONChange(t, wire, key, nil)) == nil {
				t.Fatal("missing event field", key)
			}
		}
		for _, bad := range [][]byte{append(bytes.Clone(wire), []byte(" {}")...), append(bytes.Clone(wire[:len(wire)-1]), []byte(`,"unknown":"\ud800"}`)...), taskJSONChange(t, wire, "unknown", json.RawMessage(`"x"`))} {
			if tc.decode(bad) == nil {
				t.Fatal("strict event schema")
			}
		}
	}
	position := changed.Position
	wire := taskRaw(t, position)
	for _, bad := range []string{`1`, `"0"`, `"01"`, `"9223372036854775808"`, `null`} {
		if json.Unmarshal(taskJSONChange(t, wire, "order_generation", json.RawMessage(bad)), new(TaskPosition)) == nil {
			t.Fatal("position generation accepted")
		}
	}
	for _, key := range strings.Fields("sprint_id state priority previous_id next_id order_generation") {
		if json.Unmarshal(taskJSONChange(t, wire, key, nil), new(TaskPosition)) == nil {
			t.Fatal("missing position field")
		}
	}
}
