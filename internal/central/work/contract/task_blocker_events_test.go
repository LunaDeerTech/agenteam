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

func blockerHistoryFixture(t *testing.T, resolved bool) TaskBlockerEvent {
	m := blockerMutationFixture(t)
	v := TaskBlockerEvent{ID: m.TaskEventID, ProjectID: m.Task.ProjectID, TaskID: m.Task.ID, TaskVersion: m.Task.Version, Type: TaskBlockerEventAdded, Actor: m.Blocker.CreatedBy, OperationID: testID[TaskBlockerCommand](t, 50), CorrelationID: testID[TaskBlockerCommand](t, 50), CreatedAt: m.Task.UpdatedAt, Payload: TaskBlockerEventPayload{Added: &TaskBlockerAddedPayload{BlockerID: m.Blocker.ID, BlockerType: m.Blocker.Type}}}
	if resolved {
		v.Type = TaskBlockerEventResolved
		v.Payload = TaskBlockerEventPayload{Resolved: &TaskBlockerResolvedPayload{BlockerID: m.Blocker.ID, BlockerType: m.Blocker.Type, ResolutionComment: m.Blocker.ResolutionComment}}
	}
	return v
}
func TestTaskBlockerHistoryStrictAndLegacy(t *testing.T) {
	for _, resolved := range []bool{false, true} {
		v := blockerHistoryFixture(t, resolved)
		raw := taskRaw(t, v)
		decoded, e := DecodeTaskBlockerEvent(raw)
		if e != nil || !reflect.DeepEqual(v, decoded) {
			t.Fatal("history", e)
		}
		if json.Unmarshal(raw, new(TaskEvent)) == nil {
			t.Fatal("planning decoder broadened")
		}
		for _, key := range strings.Fields("id project_id task_id task_version type actor operation_id correlation_id payload created_at") {
			for _, bad := range [][]byte{taskJSONChange(t, raw, key, nil), bytes.Replace(raw, []byte(`"`+key+`":`), []byte(`"`+strings.ToUpper(key)+`":`), 1), taskJSONChange(t, raw, key, []byte(`null`))} {
				next := v.Clone()
				if next.UnmarshalJSON(bad) == nil || !reflect.DeepEqual(v, next) {
					t.Fatal("strict history or atomicity", key)
				}
			}
		}
		for _, bad := range [][]byte{taskJSONChange(t, raw, "type", []byte(`"task_created"`)), taskJSONChange(t, raw, "actor", []byte(`{"type":"service","source":"task_domain","user_id":"01900000-0000-7000-8000-000000000001"}`)), taskJSONChange(t, raw, "correlation_id", taskRaw(t, testID[TaskBlockerCommand](t, 51))), bytes.Replace(raw, []byte(`"blocker_type":`), []byte(`"BLOCKER_TYPE":`), 1)} {
			if _, e = DecodeTaskBlockerEvent(bad); e == nil {
				t.Fatal("invalid history")
			}
		}
	}
	v := blockerHistoryFixture(t, true)
	comment := strings.Repeat("<", 1024)
	v.Payload.Resolved.ResolutionComment = &comment
	if _, e := DecodeTaskBlockerEvent(taskRaw(t, v)); e != nil {
		t.Fatal("maximum escaped comment", e)
	}
	clone := v.Clone()
	*clone.Payload.Resolved.ResolutionComment = "changed"
	if *v.Payload.Resolved.ResolutionComment == "changed" {
		t.Fatal("history clone")
	}
	if (*TaskBlockerEvent)(nil).UnmarshalJSON([]byte(`{}`)) == nil {
		t.Fatal("nil receiver")
	}
}
func TestTaskBlockerTypedEnvelopeFactory(t *testing.T) {
	catalog := event.NewCatalog()
	factory, e := RegisterTaskBlockerEvents(catalog)
	if e != nil || !factory.Valid() {
		t.Fatal(e)
	}
	m := blockerMutationFixture(t)
	p := TaskBlockersChanged{OperationID: testID[TaskBlockerCommand](t, 50), ActorUserID: testID[i.User](t, 1), TaskEventID: m.TaskEventID, BlockerID: m.Blocker.ID, Change: TaskBlockerAddedChange}
	version := m.Task.Version
	h := event.Header{EventID: m.EventIDs[0], EventType: TaskBlockersChangedName, SchemaVersion: TaskBlockerSchemaVersion, OccurredAt: m.Task.UpdatedAt, Scope: event.Scope{Kind: event.ProjectScope, ProjectID: testID[event.Project](t, 5)}, AggregateType: TaskAggregate, AggregateID: testID[event.Aggregate](t, 20), AggregateVersion: &version}
	ev, e := factory.NewTaskBlockersChanged(h, p)
	if e != nil {
		t.Fatal(e)
	}
	if got, e := factory.DecodeTaskBlockersChanged(ev); e != nil || got != p {
		t.Fatal("typed decode", e)
	}
	raw := taskRaw(t, p)
	if _, e = factory.Restore(h, raw); e != nil {
		t.Fatal(e)
	}
	other, _ := RegisterTaskBlockerEvents(event.NewCatalog())
	if _, e = other.DecodeTaskBlockersChanged(ev); e == nil {
		t.Fatal("foreign catalog")
	}
	if _, e = factory.Restore(h, append(raw, bytes.Repeat([]byte(" "), MaxTaskBlockersChangedBytes)...)); e == nil {
		t.Fatal("raw envelope cap")
	}
	for _, modify := range []func(*event.Header){func(h *event.Header) { v := f.Version(1); h.AggregateVersion = &v }, func(h *event.Header) { h.AggregateVersion = nil }, func(h *event.Header) { h.EventType = TaskTransitionedName }, func(h *event.Header) { h.SchemaVersion = 2 }, func(h *event.Header) { h.Scope = event.Scope{} }, func(h *event.Header) { seq := f.Sequence(1); h.AggregateSequence = &seq }} {
		bad := h
		modify(&bad)
		if _, e = factory.NewTaskBlockersChanged(bad, p); e == nil {
			t.Fatal("header admitted")
		}
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	if len(fields) != 5 {
		t.Fatal("payload closure")
	}
	for _, key := range strings.Fields("operation_id actor_user_id task_event_id blocker_id change") {
		if _, e = DecodeTaskBlockersChanged(taskJSONChange(t, raw, key, nil)); e == nil {
			t.Fatal("missing", key)
		}
	}
	if _, e = DecodeTaskBlockersChanged(taskJSONChange(t, raw, "description", []byte(`"secret"`))); e == nil {
		t.Fatal("body admitted")
	}
	if (TaskBlockerEvents{}).Valid() {
		t.Fatal("zero factory")
	}
}
