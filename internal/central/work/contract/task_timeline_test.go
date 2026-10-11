package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestTaskTimelineFilterStrictAndNormalized(t *testing.T) {
	for _, raw := range []string{`{}`, `{"order":"desc"}`, `{"types":["blocker_resolved","task_created"],"order":"asc"}`} {
		v, err := DecodeTaskTimelineFilter([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := v.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		if raw == `{}` && string(encoded) != `{"order":"desc"}` {
			t.Fatal("zero filter default")
		}
		if v.Types != nil && string(encoded) != `{"types":["task_created","blocker_resolved"],"order":"asc"}` {
			t.Fatal("set normalization")
		}
		clone := v.Clone()
		if len(clone.Types) > 0 {
			clone.Types[0] = TaskTimelineFieldsUpdated
			if v.Types[0] == clone.Types[0] {
				t.Fatal("borrowed filter")
			}
		}
	}
	original := TaskTimelineFilter{Types: []TaskTimelineEventType{TaskTimelineCreated}, Order: TaskTimelineAscending}
	for _, raw := range []string{`null`, `[]`, `{"types":null}`, `{"types":[]}`, `{"types":["task_created","task_created"]}`, `{"types":["future"]}`, `{"order":null}`, `{"order":""}`, `{"order":"DESC"}`, `{"Order":"desc"}`, `{"order":"asc","order":"desc"}`, `{"unknown":1}`, `{} {}`, `{"types":["\ud800"]}`} {
		v := original.Clone()
		if v.UnmarshalJSON([]byte(raw)) == nil || !reflect.DeepEqual(v, original) {
			t.Fatal("accepted invalid filter or changed receiver", raw)
		}
	}
	for _, v := range []TaskTimelineFilter{{Types: []TaskTimelineEventType{}}, {Types: []TaskTimelineEventType{TaskTimelineCreated, TaskTimelineCreated}}, {Order: "invalid"}} {
		if v.Validate() == nil {
			t.Fatal("invalid Go filter")
		}
		if _, err := json.Marshal(v); err == nil {
			t.Fatal("invalid filter marshal")
		}
	}
	if _, err := DecodeTaskTimelineFilter(append(bytes.Repeat([]byte(" "), MaxTaskEventBytes), []byte(`{}`)...)); err == nil {
		t.Fatal("raw cap")
	}
	if _, err := DecodeTaskTimelineFilter([]byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'}); err == nil {
		t.Fatal("invalid UTF8")
	}
	if (*TaskTimelineFilter)(nil).UnmarshalJSON([]byte(`{}`)) == nil {
		t.Fatal("nil receiver")
	}
}

func TestTaskTimelineEventsPreserveBothStrictFamilies(t *testing.T) {
	created := taskHistoryFixture(t)
	updated := created.Clone()
	updated.Type, updated.TaskVersion = TaskEventFieldsUpdated, 2
	updated.Payload = taskRaw(t, TaskFieldsUpdatedPayload{ChangedFields: []TaskChangedField{TaskTitleChanged}})
	added, resolved := blockerHistoryFixture(t, false), blockerHistoryFixture(t, true)
	values := []TaskTimelineEvent{{Planning: &created}, {Planning: &updated}, {Blocker: &added}, {Blocker: &resolved}}
	for _, v := range values {
		raw, err := v.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		var original []byte
		if v.Planning != nil {
			original = taskRaw(t, *v.Planning)
		} else {
			original = taskRaw(t, *v.Blocker)
		}
		if !bytes.Equal(raw, original) {
			t.Fatal("timeline changed event wire")
		}
		got, err := DecodeTaskTimelineEvent(raw)
		if err != nil || !reflect.DeepEqual(v, got) {
			t.Fatal("union roundtrip", err)
		}
		if v.Planning != nil {
			if _, err := DecodeTaskBlockerEvent(raw); err == nil {
				t.Fatal("old blocker decoder broadened")
			}
		} else if json.Unmarshal(raw, new(TaskEvent)) == nil {
			t.Fatal("old planning decoder broadened")
		}
		for _, key := range strings.Fields("id project_id task_id task_version type actor operation_id correlation_id payload created_at") {
			for _, bad := range [][]byte{taskJSONChange(t, raw, key, nil), taskJSONChange(t, raw, key, []byte(`null`)), bytes.Replace(raw, []byte(`"`+key+`":`), []byte(`"`+strings.ToUpper(key)+`":`), 1)} {
				next := v.Clone()
				if next.UnmarshalJSON(bad) == nil || !reflect.DeepEqual(next, v) {
					t.Fatal("strict/atomic history", key)
				}
			}
		}
		for _, bad := range [][]byte{append(raw, []byte(` {}`)...), append(bytes.Repeat([]byte(" "), MaxTaskEventBytes), raw...), taskJSONChange(t, raw, "type", []byte(`"status_changed"`)), bytes.Replace(raw, []byte(`"type":`), []byte(`"type":"task_created","type":`), 1)} {
			if _, err := DecodeTaskTimelineEvent(bad); err == nil {
				t.Fatal("invalid or unsupported history")
			}
		}
		if fmt.Sprintf("%+v", v) != "work_task_timeline" || v.LogValue().String() != "work_task_timeline" {
			t.Fatal("unsafe log")
		}
	}
	for _, v := range []TaskTimelineEvent{{}, {Planning: &created, Blocker: &added}} {
		if v.Validate() == nil {
			t.Fatal("invalid union")
		}
		if _, err := v.MarshalJSON(); err == nil {
			t.Fatal("invalid union marshal")
		}
	}
	p := values[0].Clone()
	p.Planning.Payload[0] = '!'
	if values[0].Planning.Payload[0] == '!' {
		t.Fatal("planning borrowed payload")
	}
	text := "private original"
	resolved.Payload.Resolved.ResolutionComment = &text
	b := (TaskTimelineEvent{Blocker: &resolved}).Clone()
	*b.Blocker.Payload.Resolved.ResolutionComment = "changed"
	if *resolved.Payload.Resolved.ResolutionComment != text {
		t.Fatal("blocker borrowed payload")
	}
	if (*TaskTimelineEvent)(nil).UnmarshalJSON(taskRaw(t, created)) == nil {
		t.Fatal("nil event receiver")
	}
}

func TestTaskTimelineCurrentTypedSources(t *testing.T) {
	state := transitionHistoryFixture(t, TaskTransitionStateChanged)
	assignee := transitionHistoryFixture(t, TaskTransitionAssigneeChanged)
	comment := transitionHistoryFixture(t, TaskTransitionComment)
	comment.Payload.Comment.Body = strings.Repeat("<", MaxTaskTransitionCommentBytes)
	operation := testID[SchedulerClaim](t, 901)
	claim := SchedulerTaskEvent{ID: state.ID, ProjectID: state.ProjectID, TaskID: state.TaskID, TaskVersion: 2, Type: TaskTransitionStateChanged, Actor: SchedulerTaskActor{CauseID: operation.String()}, OperationID: operation, CorrelationID: operation, CreatedAt: state.CreatedAt, Payload: SchedulerStateChanged{FromState: TaskStateTodo, ToState: TaskStateInProgress, ReasonCode: "scheduler_claim"}}
	busy := TaskBusyTaskEvent{ID: state.ID, ProjectID: state.ProjectID, TaskID: state.TaskID, TaskVersion: 3, Type: TaskTransitionStateChanged, Actor: claim.Actor, OperationID: operation, CorrelationID: operation, CreatedAt: state.CreatedAt, Payload: TaskBusyStateChanged{FromState: TaskStateInProgress, ToState: TaskStateTodo, ReasonCode: "scheduler_agent_busy_compensation"}}
	failure := TaskFailureTaskEvent{ID: state.ID, ProjectID: state.ProjectID, TaskID: state.TaskID, TaskVersion: 4, Type: "state_changed", Actor: claim.Actor, OperationID: operation, CorrelationID: operation, CreatedAt: state.CreatedAt, State: &TaskFailureStateChanged{FromState: TaskStateInReview, ToState: TaskStateBlocked, ReasonCode: TaskLaunchFailureHistoryReason}}
	blockFailure := failure.Clone()
	blockFailure.Type, blockFailure.State = "blocker_added", nil
	blockFailure.Blocker = &TaskFailureBlockerAdded{BlockerID: testID[TaskBlockerIdentity](t, 902), BlockerType: TaskBlockerTechnical, ReasonCode: TaskLaunchFailureHistoryReason}
	values := []TaskTimelineEvent{{Transition: &state}, {Transition: &assignee}, {Transition: &comment}, {Claim: &claim}, {Compensation: &busy}, {Failure: &failure}, {Failure: &blockFailure}}
	for _, v := range values {
		raw, err := v.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		got, err := DecodeTaskTimelineEvent(raw)
		if err != nil || !reflect.DeepEqual(got, v) {
			t.Fatal("current family roundtrip", err)
		}
		h, err := got.Header()
		if err != nil || h.ProjectID != state.ProjectID || h.TaskID != state.TaskID || h.ID != state.ID || h.CreatedAt != state.CreatedAt {
			t.Fatal("header diverged from original fact", err)
		}
		if h.Type == TaskTimelineComment && len(raw) <= MaxTaskEventBytes {
			t.Fatal("comment never crossed old event cap")
		}
		// Each source codec must enforce its own actor/reason/type and exact bytes.
		for _, bad := range [][]byte{taskJSONChange(t, raw, "actor", []byte(`{"type":"system","service_name":"scheduler","source":"scheduler","cause_id":"bad"}`)), taskJSONChange(t, raw, "payload", []byte(`{}`)), taskJSONChange(t, raw, "correlation_id", taskRaw(t, testID[SchedulerClaim](t, 903))), taskJSONChange(t, raw, "type", []byte(`"task_moved"`))} {
			next := v.Clone()
			if next.UnmarshalJSON(bad) == nil || !reflect.DeepEqual(next, v) {
				t.Fatal("invalid current history changed receiver")
			}
		}
		if _, err := DecodeTaskTimelineEvent(append(bytes.Repeat([]byte(" "), MaxTaskTransitionCommentEventBytes), raw...)); err == nil {
			t.Fatal("comment outer cap")
		}
	}
	// Standalone and Transfer human blocker facts intentionally share the same
	// public wire. Only persisted operation columns retain their provenance.
	for _, kind := range []TaskTransitionEventType{TaskTransitionBlockerAdded, TaskTransitionBlockerResolved} {
		e := transitionHistoryFixture(t, kind)
		original := TaskTimelineEvent{Transition: &e}
		raw := taskRaw(t, original)
		got, err := DecodeTaskTimelineEvent(raw)
		if err != nil || got.Blocker == nil || !bytes.Equal(taskRaw(t, got), raw) {
			t.Fatal("canonical human blocker wire", err)
		}
	}
	technical := transitionHistoryFixture(t, TaskTransitionBlockerResolved)
	technical.Payload.BlockerResolved.BlockerType = TaskBlockerTechnical
	raw := taskRaw(t, TaskTimelineEvent{Transition: &technical})
	got, err := DecodeTaskTimelineEvent(raw)
	if err != nil || got.Transition == nil || json.Unmarshal(raw, new(TaskBlockerEvent)) == nil {
		t.Fatal("technical resolution lost its actual family", err)
	}
	for _, other := range values {
		mixed := other.Clone()
		planning := taskHistoryFixture(t)
		mixed.Planning = &planning
		if mixed.Validate() == nil {
			t.Fatal("multiple family arms")
		}
	}
	cloned := values[2].Clone()
	cloned.Transition.Payload.Comment.Body = "changed"
	if values[2].Transition.Payload.Comment.Body == "changed" {
		t.Fatal("borrowed comment")
	}
	cloned = values[6].Clone()
	cloned.Failure.Blocker.ReasonCode = "changed"
	if values[6].Failure.Blocker.ReasonCode == "changed" {
		t.Fatal("borrowed failure payload")
	}
	if _, err := (TaskTimelineEvent{}).Header(); err == nil {
		t.Fatal("invalid union yielded header")
	}
	all := TaskTimelineFilter{Types: []TaskTimelineEventType{TaskTimelineComment, TaskTimelineStateChanged, TaskTimelineAssigneeChanged}}
	encoded, err := all.MarshalJSON()
	if err != nil || string(encoded) != `{"types":["state_changed","assignee_changed","comment"],"order":"desc"}` {
		t.Fatal("new types canonical order", err)
	}
}
