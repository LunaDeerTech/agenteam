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
