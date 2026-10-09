package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"reflect"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func transitionHistoryFixture(t *testing.T, kind TaskTransitionEventType) TaskTransitionEvent {
	t.Helper()
	task := taskFixture(t)
	command := testID[TaskTransitionCommand](t, 302)
	v := TaskTransitionEvent{
		ID: testID[TaskEvent](t, 301), ProjectID: task.ProjectID, TaskID: task.ID, TaskVersion: 2,
		Type: kind, Actor: TaskTransitionActor{UserID: testID[identity.User](t, 1)},
		OperationID: command, CorrelationID: command, CreatedAt: task.UpdatedAt,
	}
	switch kind {
	case TaskTransitionStateChanged:
		v.Payload.StateChanged = &TaskStateChangedPayload{FromState: TaskStateInProgress, ToState: TaskStateInReview}
	case TaskTransitionAssigneeChanged:
		from := testID[identity.Agent](t, 70)
		v.Payload.AssigneeChanged = &TaskAssigneeChangedPayload{FromAgentID: &from, ToAgentID: testID[identity.Agent](t, 71)}
	case TaskTransitionBlockerAdded:
		v.Payload.BlockerAdded = &TaskBlockerAddedPayload{BlockerID: testID[TaskBlockerIdentity](t, 303), BlockerType: TaskBlockerRelyOn}
	case TaskTransitionBlockerResolved:
		v.Payload.BlockerResolved = &TaskBlockerResolvedPayload{BlockerID: testID[TaskBlockerIdentity](t, 303), BlockerType: TaskBlockerWaitingForHuman}
	case TaskTransitionComment:
		v.Payload.Comment = &TaskCommentPayload{Body: " original <review> e\u0301 界\t\n\r "}
	}
	return v
}

func transitionHistoryReject(t *testing.T, raw []byte, code f.Code) {
	t.Helper()
	got := transitionHistoryFixture(t, TaskTransitionAssigneeChanged)
	before := got.Clone()
	transitionRequireFault(t, got.UnmarshalJSON(raw), code)
	if !reflect.DeepEqual(got, before) {
		t.Fatal("failed history decode changed receiver")
	}
	decoded, err := DecodeTaskTransitionEvent(raw)
	transitionRequireFault(t, err, code)
	if !reflect.DeepEqual(decoded, TaskTransitionEvent{}) {
		t.Fatal("failed history Decode returned nonzero result")
	}
}

func transitionHistoryRawField(t *testing.T, raw []byte, key string, replacement []byte) []byte {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	original, ok := fields[key]
	if !ok {
		t.Fatal("missing fixture field", key)
	}
	needle := append(append(taskRaw(t, key), ':'), original...)
	if bytes.Count(raw, needle) != 1 {
		t.Fatal("raw replacement is not unique", key)
	}
	value := append(append(taskRaw(t, key), ':'), replacement...)
	return bytes.Replace(raw, needle, value, 1)
}

func TestTaskTransitionHistoryTypedFacts(t *testing.T) {
	kinds := []TaskTransitionEventType{TaskTransitionStateChanged, TaskTransitionAssigneeChanged, TaskTransitionBlockerAdded, TaskTransitionBlockerResolved, TaskTransitionComment}
	t.Run("event_type", func(t *testing.T) {
		for n, wire := range []string{"state_changed", "assignee_changed", "blocker_added", "blocker_resolved", "comment"} {
			kind := kinds[n]
			if err := kind.Validate(); err != nil {
				t.Fatal("known history type rejected", err)
			}
			raw, err := kind.MarshalJSON()
			if err != nil || string(raw) != `"`+wire+`"` {
				t.Fatal("history type wire changed", err)
			}
			var got TaskTransitionEventType
			if got.UnmarshalJSON(raw) != nil || got != kind {
				t.Fatal("history type roundtrip")
			}
		}
		for _, raw := range []string{`"task_created"`, `"fields_updated"`, `"STATE_CHANGED"`, `""`, `"unknown"`, `null`, `1`, `true`, `[]`, `{}`, `"comment" "comment"`, `"\ud800"`} {
			got := TaskTransitionComment
			transitionRequireFault(t, got.UnmarshalJSON([]byte(raw)), f.InvalidArgument)
			if got != TaskTransitionComment {
				t.Fatal("failed enum decode changed receiver")
			}
		}
		for _, kind := range []TaskTransitionEventType{"", "task_created", "fields_updated", "STATE_CHANGED"} {
			transitionRequireFault(t, kind.Validate(), f.InvalidArgument)
			raw, err := kind.MarshalJSON()
			transitionRequireFault(t, err, f.InvalidArgument)
			if raw != nil {
				t.Fatal("invalid enum emitted bytes")
			}
		}
	})
	t.Run("human_actor", func(t *testing.T) {
		actor := transitionHistoryFixture(t, TaskTransitionComment).Actor
		raw := taskRaw(t, actor)
		if string(raw) != `{"type":"human","user_id":"`+actor.UserID.String()+`","source":"task_domain"}` {
			t.Fatal("Human actor projection changed")
		}
		if got, err := DecodeTaskTransitionActor(raw); err != nil || got != actor || got.Clone() != actor {
			t.Fatal("actor roundtrip/clone", err)
		}
		for _, pair := range []struct{ key, value string }{
			{"type", `"agent"`}, {"type", `"agent_run"`}, {"type", `"system"`}, {"type", `"service"`},
			{"type", `"HUMAN"`}, {"source", `"scheduler"`}, {"source", `"executor"`},
			{"session_id", `"private"`}, {"agent_id", `"private"`}, {"execution_id", `"private"`},
			{"service_name", `"private"`}, {"cause_id", `"private"`}, {"display_name", `"private"`},
		} {
			got := actor
			bad := taskJSONChange(t, raw, pair.key, json.RawMessage(pair.value))
			transitionRequireFault(t, got.UnmarshalJSON(bad), f.InvalidArgument)
			if got != actor {
				t.Fatal("failed actor decode changed receiver")
			}
			decoded, err := DecodeTaskTransitionActor(bad)
			transitionRequireFault(t, err, f.InvalidArgument)
			if decoded != (TaskTransitionActor{}) {
				t.Fatal("failed Actor Decode returned partial value")
			}
		}
		transitionRequireFault(t, (TaskTransitionActor{}).Validate(), f.InvalidArgument)
		if raw, err := (TaskTransitionActor{}).MarshalJSON(); err == nil || raw != nil {
			t.Fatal("zero actor marshaled")
		}
	})
	t.Run("human_edges_and_reason", func(t *testing.T) {
		// Literal Human oracle; do not derive expectations from T0a or Validate.
		allowed := map[[2]TaskState]bool{
			{TaskStateBacklog, TaskStateTodo}: true, {TaskStateBacklog, TaskStateCancelled}: true,
			{TaskStateTodo, TaskStateBlocked}: true, {TaskStateTodo, TaskStateCancelled}: true,
			{TaskStateInProgress, TaskStateInReview}: true, {TaskStateInProgress, TaskStateBlocked}: true, {TaskStateInProgress, TaskStateCancelled}: true,
			{TaskStateInReview, TaskStateDone}: true, {TaskStateInReview, TaskStateTodo}: true, {TaskStateInReview, TaskStateBlocked}: true, {TaskStateInReview, TaskStateCancelled}: true,
			{TaskStateBlocked, TaskStateTodo}: true, {TaskStateBlocked, TaskStateCancelled}: true,
		}
		states := []TaskState{TaskStateBacklog, TaskStateTodo, TaskStateInProgress, TaskStateInReview, TaskStateBlocked, TaskStateDone, TaskStateCancelled}
		for _, from := range states {
			for _, to := range states {
				value := TaskStateChangedPayload{FromState: from, ToState: to}
				if allowed[[2]TaskState{from, to}] {
					if err := value.Validate(); err != nil {
						t.Fatal("Human edge rejected", string(from), string(to), err)
					}
					raw := taskRaw(t, value)
					if !bytes.Contains(raw, []byte(`"reason_code":null`)) {
						t.Fatal("Human state fact has no explicit null reason")
					}
					got, err := DecodeTaskStateChangedPayload(raw)
					if err != nil || got != value || got.Clone() != value {
						t.Fatal("state fact roundtrip", err)
					}
				} else {
					transitionRequireFault(t, value.Validate(), f.InvalidArgument)
					encoded, err := value.MarshalJSON()
					transitionRequireFault(t, err, f.InvalidArgument)
					if encoded != nil {
						t.Fatal("non-Human edge marshaled")
					}
					raw := []byte(fmt.Sprintf(`{"from_state":%q,"to_state":%q,"reason_code":null}`, string(from), string(to)))
					decoded, err := DecodeTaskStateChangedPayload(raw)
					transitionRequireFault(t, err, f.InvalidArgument)
					if decoded != (TaskStateChangedPayload{}) {
						t.Fatal("failed state Decode returned partial value")
					}
				}
			}
		}
		value := TaskStateChangedPayload{FromState: TaskStateInProgress, ToState: TaskStateInReview}
		raw := taskRaw(t, value)
		for _, reason := range []string{`""`, `"scheduler"`, `"state_inconsistency"`, `0`, `false`, `{}`, `[]`} {
			got := value
			transitionRequireFault(t, got.UnmarshalJSON(taskJSONChange(t, raw, "reason_code", json.RawMessage(reason))), f.InvalidArgument)
			if got != value {
				t.Fatal("invalid reason changed state receiver")
			}
		}
	})
	t.Run("assignee_and_comment", func(t *testing.T) {
		assignee := *transitionHistoryFixture(t, TaskTransitionAssigneeChanged).Payload.AssigneeChanged
		for _, from := range []*identity.AgentID{nil, assignee.FromAgentID} {
			v := assignee.Clone()
			v.FromAgentID = from
			got, err := DecodeTaskAssigneeChangedPayload(taskRaw(t, v))
			if err != nil || !reflect.DeepEqual(got, v) {
				t.Fatal("assignee roundtrip", err)
			}
		}
		for _, v := range []TaskAssigneeChangedPayload{{}, {FromAgentID: &assignee.ToAgentID, ToAgentID: assignee.ToAgentID}, {FromAgentID: new(identity.AgentID), ToAgentID: assignee.ToAgentID}} {
			transitionRequireFault(t, v.Validate(), f.InvalidArgument)
			if raw, err := v.MarshalJSON(); err == nil || raw != nil {
				t.Fatal("invalid assignee payload marshaled")
			}
		}
		for _, body := range []string{"x", " \t\n\r e\u0301 é 界 \u2003 ", strings.Repeat("x", 32768), strings.Repeat("😀", 8192)} {
			value := TaskCommentPayload{Body: body}
			if err := value.Validate(); err != nil {
				t.Fatal("legal comment rejected", err)
			}
			got, err := DecodeTaskCommentPayload(taskRaw(t, value))
			if err != nil || got != value || got.Clone() != value {
				t.Fatal("comment bytes changed", err)
			}
		}
		for _, body := range []string{"", " \t\n\r ", "\u2003\u00a0", strings.Repeat("x", 32769), strings.Repeat("😀", 8192) + "x", "a\x00b", "a\x7fb", "a\u0085b", string([]byte{0xff})} {
			value := TaskCommentPayload{Body: body}
			transitionRequireFault(t, value.Validate(), f.InvalidArgument)
			encoded, err := value.MarshalJSON()
			transitionRequireFault(t, err, f.InvalidArgument)
			if encoded != nil {
				t.Fatal("invalid comment emitted bytes")
			}
			if body != string([]byte{0xff}) {
				got, err := DecodeTaskCommentPayload(taskJSONChange(t, []byte(`{"body":"original"}`), "body", taskRaw(t, body)))
				transitionRequireFault(t, err, f.InvalidArgument)
				if got != (TaskCommentPayload{}) {
					t.Fatal("failed comment Decode returned partial value")
				}
			}
		}
	})
	t.Run("typed_union_and_blockers", func(t *testing.T) {
		for _, kind := range kinds {
			v := transitionHistoryFixture(t, kind)
			if err := v.Payload.ValidateFor(kind); err != nil {
				t.Fatal("matching union rejected", err)
			}
			transitionRequireFault(t, (TaskTransitionFactPayload{}).ValidateFor(kind), f.InvalidArgument)
			for _, other := range kinds {
				if other != kind {
					transitionRequireFault(t, v.Payload.ValidateFor(other), f.InvalidArgument)
				}
			}
			mixed := v.Payload.Clone()
			if kind == TaskTransitionComment {
				mixed.StateChanged = &TaskStateChangedPayload{FromState: TaskStateBacklog, ToState: TaskStateTodo}
			} else {
				mixed.Comment = &TaskCommentPayload{Body: "extra"}
			}
			transitionRequireFault(t, mixed.ValidateFor(kind), f.InvalidArgument)
			v.Payload = mixed
			encoded, err := v.MarshalJSON()
			transitionRequireFault(t, err, f.InvalidArgument)
			if encoded != nil {
				t.Fatal("multiple payload branches emitted bytes")
			}
		}
		for _, kind := range []TaskTransitionEventType{TaskTransitionBlockerAdded, TaskTransitionBlockerResolved} {
			for _, blockerKind := range []TaskBlockerType{TaskBlockerRelyOn, TaskBlockerWaitingForHuman} {
				v := transitionHistoryFixture(t, kind)
				if kind == TaskTransitionBlockerAdded {
					v.Payload.BlockerAdded.BlockerType = blockerKind
				} else {
					v.Payload.BlockerResolved.BlockerType = blockerKind
				}
				raw := taskRaw(t, v)
				got, err := DecodeTaskTransitionEvent(raw)
				if err != nil || !reflect.DeepEqual(got, v) {
					t.Fatal("supported blocker history rejected", err)
				}
				for _, unbound := range []TaskBlockerType{TaskBlockerTechnical, TaskBlockerWaitingForMeetingApproval, TaskBlockerUserCancelledExecution} {
					var fields map[string]json.RawMessage
					if err := json.Unmarshal(raw, &fields); err != nil {
						t.Fatal(err)
					}
					payload := taskJSONChange(t, fields["payload"], "blocker_type", taskRaw(t, string(unbound)))
					transitionHistoryReject(t, taskJSONChange(t, raw, "payload", payload), f.DependencyUnbound)
					copy := v.Clone()
					if copy.Payload.BlockerAdded != nil {
						copy.Payload.BlockerAdded.BlockerType = unbound
					} else {
						copy.Payload.BlockerResolved.BlockerType = unbound
					}
					transitionRequireFault(t, copy.Validate(), f.DependencyUnbound)
					if encoded, err := copy.MarshalJSON(); err == nil || encoded != nil {
						t.Fatal("unbound blocker history marshaled")
					}
				}
			}
		}
		resolved := transitionHistoryFixture(t, TaskTransitionBlockerResolved)
		raw := taskRaw(t, resolved)
		comment := "independent blocker resolution"
		resolved.Payload.BlockerResolved.ResolutionComment = &comment
		if err := resolved.Payload.BlockerResolved.Validate(); err != nil {
			t.Fatal("B0-C standalone comment fixture rejected", err)
		}
		transitionRequireFault(t, resolved.Validate(), f.InvalidArgument)
		transitionHistoryReject(t, taskJSONChange(t, raw, "payload", taskRaw(t, *resolved.Payload.BlockerResolved)), f.InvalidArgument)
		var check func(reflect.Type)
		check = func(typ reflect.Type) {
			switch typ.Kind() {
			case reflect.Pointer:
				check(typ.Elem())
			case reflect.Struct:
				for n := range typ.NumField() {
					field := typ.Field(n)
					if field.IsExported() {
						check(field.Type)
					}
				}
			case reflect.Map, reflect.Interface, reflect.Slice:
				t.Fatal("public untyped history escape hatch", typ)
			}
		}
		check(reflect.TypeFor[TaskTransitionEvent]())
	})
	t.Run("strict_concrete_codecs", func(t *testing.T) {
		actor := transitionHistoryFixture(t, TaskTransitionComment).Actor
		state := *transitionHistoryFixture(t, TaskTransitionStateChanged).Payload.StateChanged
		assignee := *transitionHistoryFixture(t, TaskTransitionAssigneeChanged).Payload.AssigneeChanged
		comment := *transitionHistoryFixture(t, TaskTransitionComment).Payload.Comment
		for _, tc := range []struct {
			name           string
			value          any
			keys, nullable []string
			decode         func([]byte) error
			current        func() any
			free           func([]byte) (any, error)
			zero           any
		}{
			{"actor", actor, strings.Fields("type user_id source"), nil, actor.UnmarshalJSON, func() any { return actor.Clone() }, func(raw []byte) (any, error) { return DecodeTaskTransitionActor(raw) }, TaskTransitionActor{}},
			{"state", state, strings.Fields("from_state to_state reason_code"), []string{"reason_code"}, state.UnmarshalJSON, func() any { return state.Clone() }, func(raw []byte) (any, error) { return DecodeTaskStateChangedPayload(raw) }, TaskStateChangedPayload{}},
			{"assignee", assignee, strings.Fields("from_agent_id to_agent_id"), []string{"from_agent_id"}, assignee.UnmarshalJSON, func() any { return assignee.Clone() }, func(raw []byte) (any, error) { return DecodeTaskAssigneeChangedPayload(raw) }, TaskAssigneeChangedPayload{}},
			{"comment", comment, []string{"body"}, nil, comment.UnmarshalJSON, func() any { return comment.Clone() }, func(raw []byte) (any, error) { return DecodeTaskCommentPayload(raw) }, TaskCommentPayload{}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				raw := taskRaw(t, tc.value)
				before := tc.current()
				reject := func(bad []byte) {
					t.Helper()
					transitionRequireFault(t, tc.decode(bad), f.InvalidArgument)
					if !reflect.DeepEqual(tc.current(), before) {
						t.Fatal("failed concrete codec changed receiver")
					}
					decoded, err := tc.free(bad)
					transitionRequireFault(t, err, f.InvalidArgument)
					if !reflect.DeepEqual(decoded, tc.zero) {
						t.Fatal("failed concrete Decode returned partial result")
					}
				}
				var fields map[string]json.RawMessage
				if json.Unmarshal(raw, &fields) != nil || len(fields) != len(tc.keys) {
					t.Fatal("concrete wire field count")
				}
				for _, key := range tc.keys {
					reject(taskJSONChange(t, raw, key, nil))
					nullable := false
					for _, allowed := range tc.nullable {
						nullable = nullable || key == allowed
					}
					if !nullable {
						reject(taskJSONChange(t, raw, key, json.RawMessage(`null`)))
					}
					reject(taskJSONChange(t, taskJSONChange(t, raw, key, nil), strings.ToUpper(key), fields[key]))
					reject([]byte(string(raw[:len(raw)-1]) + `,"` + key + `":null}`))
				}
				for _, bad := range [][]byte{[]byte(`null`), []byte(`[]`), []byte(`{}`), []byte(`1`), []byte(`"x"`), {}, append(bytes.Clone(raw), []byte(" {}")...), []byte(string(raw[:len(raw)-1]) + `,"unknown":true}`)} {
					reject(bad)
				}
				for _, bad := range []string{`"\ud800"`, `"\udc00"`, `"\ud800\u0041"`, `1`, `{}`, `[]`, `false`} {
					reject(taskJSONChange(t, raw, tc.keys[0], json.RawMessage(bad)))
				}
			})
		}
	})
	t.Run("strict_history_and_identity", func(t *testing.T) {
		for _, kind := range kinds {
			base := transitionHistoryFixture(t, kind)
			raw := taskRaw(t, base)
			got, err := DecodeTaskTransitionEvent(raw)
			if err != nil || !reflect.DeepEqual(got, base) {
				t.Fatal("history roundtrip", err)
			}
			var fields map[string]json.RawMessage
			if json.Unmarshal(raw, &fields) != nil || len(fields) != 10 {
				t.Fatal("history ten-field wire changed")
			}
			for _, key := range strings.Fields("id project_id task_id task_version type actor operation_id correlation_id payload created_at") {
				transitionHistoryReject(t, taskJSONChange(t, raw, key, nil), f.InvalidArgument)
				transitionHistoryReject(t, taskJSONChange(t, raw, key, json.RawMessage(`null`)), f.InvalidArgument)
				without := taskJSONChange(t, raw, key, nil)
				transitionHistoryReject(t, taskJSONChange(t, without, strings.ToUpper(key), fields[key]), f.InvalidArgument)
				transitionHistoryReject(t, []byte(string(raw[:len(raw)-1])+`,"`+key+`":null}`), f.InvalidArgument)
			}
			for _, key := range []string{"StateChanged", "AssigneeChanged", "BlockerAdded", "Comment", "source", "body", "description", "related_task_id", "unknown"} {
				transitionHistoryReject(t, taskJSONChange(t, raw, key, json.RawMessage(`"injected"`)), f.InvalidArgument)
			}
			for _, key := range []string{"id", "project_id", "task_id", "operation_id", "correlation_id"} {
				for _, bad := range []string{`1`, `""`, `"00000000-0000-0000-0000-000000000000"`, `"01900000-0000-4000-8000-0000000000ca"`, `"01900000-0000-7000-c000-0000000000ca"`, `"01900000-0000-7000-8000-0000000000CA"`} {
					transitionHistoryReject(t, taskJSONChange(t, raw, key, json.RawMessage(bad)), f.InvalidArgument)
				}
			}
			for _, bad := range []string{`0`, `2`, `"0"`, `"1"`, `"02"`, `"+2"`, `"-2"`, `"2.0"`, `"2e0"`, `"9223372036854775808"`} {
				transitionHistoryReject(t, taskJSONChange(t, raw, "task_version", json.RawMessage(bad)), f.InvalidArgument)
			}
			for _, version := range []f.Version{2, math.MaxInt64} {
				v := base.Clone()
				v.TaskVersion = version
				if got, err := DecodeTaskTransitionEvent(taskRaw(t, v)); err != nil || !reflect.DeepEqual(got, v) {
					t.Fatal("legal history version rejected", err)
				}
			}
			transitionHistoryReject(t, taskJSONChange(t, raw, "correlation_id", taskRaw(t, testID[TaskTransitionCommand](t, 304))), f.InvalidArgument)
			for _, bad := range []string{`"task_created"`, `"fields_updated"`, `"STATE_CHANGED"`, `"unknown"`, `1`} {
				transitionHistoryReject(t, taskJSONChange(t, raw, "type", json.RawMessage(bad)), f.InvalidArgument)
			}
			for _, bad := range []string{`"not-a-time"`, `1`, `"2026-10-09T00:00:00.0000001Z"`} {
				transitionHistoryReject(t, taskJSONChange(t, raw, "created_at", json.RawMessage(bad)), f.InvalidArgument)
			}
			for _, other := range kinds {
				if other != kind {
					transitionHistoryReject(t, taskJSONChange(t, raw, "type", taskRaw(t, string(other))), f.InvalidArgument)
				}
			}
			for _, bad := range [][]byte{[]byte(`null`), []byte(`[]`), []byte(`{}`), []byte(`1`), {}, append(bytes.Clone(raw), []byte(" {}")...), []byte(string(raw[:len(raw)-1]) + `,"task_\u0069d":null}`)} {
				transitionHistoryReject(t, bad, f.InvalidArgument)
			}
			for _, change := range []func(*TaskTransitionEvent){
				func(v *TaskTransitionEvent) { v.ID = TaskEventID{} },
				func(v *TaskTransitionEvent) { v.ProjectID = ProjectID{} },
				func(v *TaskTransitionEvent) { v.TaskID = TaskID{} },
				func(v *TaskTransitionEvent) { v.TaskVersion = 1 },
				func(v *TaskTransitionEvent) { v.Type = "unknown" },
				func(v *TaskTransitionEvent) { v.Actor = TaskTransitionActor{} },
				func(v *TaskTransitionEvent) { v.OperationID = TaskTransitionCommandID{} },
				func(v *TaskTransitionEvent) { v.CorrelationID = testID[TaskTransitionCommand](t, 304) },
				func(v *TaskTransitionEvent) { v.Payload = TaskTransitionFactPayload{} },
			} {
				value := base.Clone()
				change(&value)
				transitionRequireFault(t, value.Validate(), f.InvalidArgument)
				encoded, err := value.MarshalJSON()
				transitionRequireFault(t, err, f.InvalidArgument)
				if encoded != nil {
					t.Fatal("invalid history produced bytes")
				}
			}
		}
	})
	t.Run("raw_caps_and_nested_tokens", func(t *testing.T) {
		actor := transitionHistoryFixture(t, TaskTransitionComment).Actor
		state := *transitionHistoryFixture(t, TaskTransitionStateChanged).Payload.StateChanged
		assignee := *transitionHistoryFixture(t, TaskTransitionAssigneeChanged).Payload.AssigneeChanged
		comment := *transitionHistoryFixture(t, TaskTransitionComment).Payload.Comment
		ordinary := transitionHistoryFixture(t, TaskTransitionStateChanged)
		commentEvent := transitionHistoryFixture(t, TaskTransitionComment)
		kind := TaskTransitionComment
		for _, tc := range []struct {
			name    string
			value   any
			limit   int
			decode  func([]byte) error
			current func() any
			free    func([]byte) (any, error)
		}{
			{"type", kind, 64, kind.UnmarshalJSON, func() any { return kind }, nil},
			{"actor", actor, 1024, actor.UnmarshalJSON, func() any { return actor.Clone() }, func(raw []byte) (any, error) { return DecodeTaskTransitionActor(raw) }},
			{"state", state, 8192, state.UnmarshalJSON, func() any { return state.Clone() }, func(raw []byte) (any, error) { return DecodeTaskStateChangedPayload(raw) }},
			{"assignee", assignee, 8192, assignee.UnmarshalJSON, func() any { return assignee.Clone() }, func(raw []byte) (any, error) { return DecodeTaskAssigneeChangedPayload(raw) }},
			{"comment", comment, 256 << 10, comment.UnmarshalJSON, func() any { return comment.Clone() }, func(raw []byte) (any, error) { return DecodeTaskCommentPayload(raw) }},
			{"ordinary_record", ordinary, 16 << 10, ordinary.UnmarshalJSON, func() any { return ordinary.Clone() }, func(raw []byte) (any, error) { return DecodeTaskTransitionEvent(raw) }},
			{"comment_record", commentEvent, 272 << 10, commentEvent.UnmarshalJSON, func() any { return commentEvent.Clone() }, func(raw []byte) (any, error) { return DecodeTaskTransitionEvent(raw) }},
		} {
			t.Run(tc.name, func(t *testing.T) {
				raw := taskRaw(t, tc.value)
				at := append(bytes.Repeat([]byte(" "), tc.limit-len(raw)-1), raw...)
				at = append(at, '\n')
				if len(at) != tc.limit || tc.decode(at) != nil {
					t.Fatal("complete raw exact cap rejected")
				}
				if tc.free != nil {
					got, err := tc.free(at)
					if err != nil || !reflect.DeepEqual(got, tc.value) {
						t.Fatal("direct Decode exact raw cap", err)
					}
				}
				before := tc.current()
				for _, bad := range [][]byte{append(bytes.Clone(at), ' '), append([]byte(" "), at...)} {
					transitionRequireFault(t, tc.decode(bad), f.InvalidArgument)
					if !reflect.DeepEqual(tc.current(), before) {
						t.Fatal("raw cap failure changed receiver")
					}
					if tc.free != nil {
						got, err := tc.free(bad)
						transitionRequireFault(t, err, f.InvalidArgument)
						if !reflect.ValueOf(got).IsZero() {
							t.Fatal("raw cap failure returned nonzero Decode")
						}
					}
				}
			})
		}
		for _, kind := range kinds {
			base := transitionHistoryFixture(t, kind)
			raw := taskRaw(t, base)
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			payloadCap, outerCap := 8192, 16<<10
			if kind == TaskTransitionComment {
				payloadCap, outerCap = 256<<10, 272<<10
			}
			for _, tc := range []struct {
				key string
				cap int
			}{{"actor", 1024}, {"payload", payloadCap}} {
				original := fields[tc.key]
				for _, size := range []int{tc.cap, tc.cap + 1} {
					// Preserve padding inside the nested token. Re-marshaling the
					// outer map would erase the intended boundary probe.
					nested := []byte("{" + strings.Repeat(" ", size-len(original)) + string(original[1:]))
					wire := transitionHistoryRawField(t, raw, tc.key, nested)
					if len(wire) >= outerCap || len(nested) != size {
						t.Fatal("nested cap fixture does not isolate the child")
					}
					if size == tc.cap {
						got, err := DecodeTaskTransitionEvent(wire)
						if err != nil || !reflect.DeepEqual(got, base) {
							t.Fatal("nested exact cap rejected", tc.key, err)
						}
					} else {
						transitionHistoryReject(t, wire, f.InvalidArgument)
					}
				}
			}
		}
		for _, unit := range []string{"<", ">", "&"} {
			v := transitionHistoryFixture(t, TaskTransitionComment)
			v.Payload.Comment.Body = strings.Repeat(unit, 32768)
			payload := taskRaw(t, *v.Payload.Comment)
			if len(payload) != 196619 {
				t.Fatal("worst-case comment escaping arithmetic", len(payload))
			}
			raw := taskRaw(t, v)
			if len(raw) <= len(payload) || len(raw) >= len(payload)+4096 || len(raw) > MaxTaskTransitionCommentEventBytes {
				t.Fatal("comment record overhead/cap")
			}
			got, err := DecodeTaskTransitionEvent(raw)
			if err != nil || !reflect.DeepEqual(got, v) || !reflect.DeepEqual(got.Clone(), v) {
				t.Fatal("max comment record roundtrip/clone", err)
			}
		}
		base := transitionHistoryFixture(t, TaskTransitionComment)
		raw := taskRaw(t, base)
		for _, body := range []string{`"\ud800"`, `"\udc00"`, `"\ud800\u0041"`} {
			payload := []byte(`{"body":` + body + `}`)
			transitionHistoryReject(t, transitionHistoryRawField(t, raw, "payload", payload), f.InvalidArgument)
		}
		transitionHistoryReject(t, bytes.Replace(raw, []byte("original"), []byte{0xff}, 1), f.InvalidArgument)
		transitionHistoryReject(t, transitionHistoryRawField(t, raw, "payload", []byte(`{"body":"one","b\u006fdy":"two"}`)), f.InvalidArgument)
		escaped := transitionHistoryRawField(t, raw, "payload", []byte(`{"b\u006fdy":"\ud83d\ude00"}`))
		got, err := DecodeTaskTransitionEvent(escaped)
		if err != nil || got.Payload.Comment.Body != "😀" {
			t.Fatal("legal escaped key/surrogate pair rejected", err)
		}
		var nilActor *TaskTransitionActor
		var nilState *TaskStateChangedPayload
		var nilAssignee *TaskAssigneeChangedPayload
		var nilComment *TaskCommentPayload
		var nilEvent *TaskTransitionEvent
		var nilType *TaskTransitionEventType
		for _, decode := range []func([]byte) error{nilActor.UnmarshalJSON, nilState.UnmarshalJSON, nilAssignee.UnmarshalJSON, nilComment.UnmarshalJSON, nilEvent.UnmarshalJSON, nilType.UnmarshalJSON} {
			transitionRequireFault(t, decode(nil), f.InvalidArgument)
			transitionRequireFault(t, decode(raw), f.InvalidArgument)
		}
	})
	t.Run("clone_and_safe_log", func(t *testing.T) {
		state := transitionHistoryFixture(t, TaskTransitionStateChanged)
		assignee := transitionHistoryFixture(t, TaskTransitionAssigneeChanged)
		added := transitionHistoryFixture(t, TaskTransitionBlockerAdded)
		resolved := transitionHistoryFixture(t, TaskTransitionBlockerResolved)
		comment := transitionHistoryFixture(t, TaskTransitionComment)
		independent := "independent comment"
		resolved.Payload.BlockerResolved.ResolutionComment = &independent
		union := TaskTransitionFactPayload{
			StateChanged: state.Payload.StateChanged, AssigneeChanged: assignee.Payload.AssigneeChanged,
			BlockerAdded: added.Payload.BlockerAdded, BlockerResolved: resolved.Payload.BlockerResolved, Comment: comment.Payload.Comment,
		}
		clone := union.Clone()
		if !reflect.DeepEqual(union, clone) || clone.StateChanged == union.StateChanged || clone.AssigneeChanged == union.AssigneeChanged || clone.AssigneeChanged.FromAgentID == union.AssigneeChanged.FromAgentID || clone.BlockerAdded == union.BlockerAdded || clone.BlockerResolved == union.BlockerResolved || clone.BlockerResolved.ResolutionComment == union.BlockerResolved.ResolutionComment || clone.Comment == union.Comment {
			t.Fatal("fact union clone borrowed mutable payload")
		}
		clone.StateChanged.FromState = TaskStateBacklog
		*clone.AssigneeChanged.FromAgentID = testID[identity.Agent](t, 75)
		clone.BlockerAdded.BlockerID = testID[TaskBlockerIdentity](t, 309)
		*clone.BlockerResolved.ResolutionComment = "changed"
		clone.Comment.Body = "changed"
		if union.StateChanged.FromState != TaskStateInProgress || *union.AssigneeChanged.FromAgentID != testID[identity.Agent](t, 70) || union.BlockerAdded.BlockerID != testID[TaskBlockerIdentity](t, 303) || *union.BlockerResolved.ResolutionComment != independent || union.Comment.Body != comment.Payload.Comment.Body {
			t.Fatal("cloned payload mutation changed source")
		}
		if !reflect.DeepEqual((TaskTransitionFactPayload{}).Clone(), TaskTransitionFactPayload{}) || (TaskAssigneeChangedPayload{}).Clone().FromAgentID != nil {
			t.Fatal("clone changed zero/nil branches")
		}
		historyClone := assignee.Clone()
		*historyClone.Payload.AssigneeChanged.FromAgentID = testID[identity.Agent](t, 76)
		if *assignee.Payload.AssigneeChanged.FromAgentID != testID[identity.Agent](t, 70) {
			t.Fatal("history clone borrowed nested assignee")
		}
		kind := state.Type
		for _, value := range []any{state.Actor, &state.Actor, kind, &kind, *state.Payload.StateChanged, state.Payload.StateChanged, *assignee.Payload.AssigneeChanged, assignee.Payload.AssigneeChanged, *comment.Payload.Comment, comment.Payload.Comment, union, &union, comment, &comment} {
			for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x"} {
				if got := fmt.Sprintf(format, value); got != "work_task_transition" {
					t.Fatalf("unsafe direct history format: %q", got)
				}
			}
			var output bytes.Buffer
			slog.New(slog.NewJSONHandler(&output, nil)).Info("safe", "value", value)
			var record map[string]any
			if err := json.Unmarshal(output.Bytes(), &record); err != nil || record["value"] != "work_task_transition" {
				t.Fatal("unsafe direct history slog", err)
			}
		}
		raw := taskRaw(t, comment)
		if !bytes.Contains(raw, []byte(comment.ID.String())) || !bytes.Contains(raw, []byte(`\u003creview\u003e`)) {
			t.Fatal("logging projection damaged business history JSON")
		}
		for n := range 8 {
			t.Run(fmt.Sprintf("parallel_%d", n), func(t *testing.T) {
				t.Parallel()
				for range 16 {
					if err := comment.Validate(); err != nil {
						t.Fatal(err)
					}
					copy := comment.Clone()
					copy.Payload.Comment.Body = "clone only"
					got, err := DecodeTaskTransitionEvent(raw)
					if err != nil || !reflect.DeepEqual(got, comment) || !bytes.Equal(taskRaw(t, comment), raw) {
						t.Fatal("parallel immutable history changed", err)
					}
				}
			})
		}
	})
	t.Run("legacy_isolation", func(t *testing.T) {
		old := taskHistoryFixture(t)
		oldRaw := taskRaw(t, old)
		if err := new(TaskEvent).UnmarshalJSON(oldRaw); err != nil {
			t.Fatal("old history positive rejected", err)
		}
		for _, kind := range kinds {
			transitionRequireFault(t, new(TaskEvent).UnmarshalJSON(taskRaw(t, transitionHistoryFixture(t, kind))), f.InvalidArgument)
			transitionRequireFault(t, new(TaskEventType).UnmarshalJSON(taskRaw(t, string(kind))), f.InvalidArgument)
		}
		transitionHistoryReject(t, oldRaw, f.InvalidArgument)
		oldActorRaw := taskRaw(t, old.Actor)
		if got, err := DecodeTaskTransitionActor(oldActorRaw); err != nil || got.UserID != old.Actor.UserID {
			t.Fatal("unchanged Human wire lost compatibility", err)
		}
		for _, kind := range []string{"agent", "agent_run", "system", "service"} {
			raw := taskJSONChange(t, oldActorRaw, "type", taskRaw(t, kind))
			transitionRequireFault(t, new(TaskEventActor).UnmarshalJSON(raw), f.InvalidArgument)
			_, err := DecodeTaskTransitionActor(raw)
			transitionRequireFault(t, err, f.InvalidArgument)
		}
	})
}
