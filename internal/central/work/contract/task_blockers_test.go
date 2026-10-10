package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func blockerFixture(t *testing.T, kind TaskBlockerType) TaskBlockerCreate {
	t.Helper()
	v := TaskBlockerCreate{BlockerID: testID[TaskBlockerIdentity](t, 201), Type: kind, Description: " original <blocker> 界\t\n\r "}
	if kind == TaskBlockerRelyOn {
		v.Metadata.RelyOn = &TaskBlockerRelyOnMetadata{RelatedTaskID: testID[Task](t, 202)}
	} else if kind == TaskBlockerWaitingForHuman {
		v.Metadata.WaitingForHuman = &TaskBlockerWaitingForHumanMetadata{}
	}
	return v
}

func blockerUnboundTypes() []TaskBlockerType {
	return []TaskBlockerType{TaskBlockerWaitingForMeetingApproval, TaskBlockerTechnical, TaskBlockerUserCancelledExecution}
}

func blockerWire(t *testing.T, kind TaskBlockerType, description, metadata string) []byte {
	t.Helper()
	return []byte(fmt.Sprintf(`{"blocker_id":%s,"type":%s,"description":%s,"metadata":%s}`,
		taskRaw(t, testID[TaskBlockerIdentity](t, 201)), taskRaw(t, string(kind)), taskRaw(t, description), metadata))
}

func blockerField(t *testing.T, err error, path, code string) {
	t.Helper()
	fault := transitionRequireFault(t, err, f.InvalidArgument)
	if !reflect.DeepEqual(fault.FieldErrors, []f.FieldError{{Path: path, Code: code}}) {
		t.Fatalf("wrong safe field projection: %#v", fault.FieldErrors)
	}
}

func blockerRejectCreate(t *testing.T, raw []byte, code f.Code) {
	t.Helper()
	got := blockerFixture(t, TaskBlockerRelyOn)
	before := got.Clone()
	transitionRequireFault(t, got.UnmarshalJSON(raw), code)
	if !reflect.DeepEqual(got, before) {
		t.Fatal("failed create decode changed receiver")
	}
	decoded, err := DecodeTaskBlockerCreate(raw)
	transitionRequireFault(t, err, code)
	if !reflect.DeepEqual(decoded, TaskBlockerCreate{}) {
		t.Fatal("failed create Decode returned partial result")
	}
}

func blockerRejectAdded(t *testing.T, raw []byte, code f.Code) {
	t.Helper()
	got := TaskBlockerAddedPayload{BlockerID: testID[TaskBlockerIdentity](t, 201), BlockerType: TaskBlockerRelyOn}
	before := got.Clone()
	transitionRequireFault(t, got.UnmarshalJSON(raw), code)
	if !reflect.DeepEqual(got, before) {
		t.Fatal("failed added decode changed receiver")
	}
	decoded, err := DecodeTaskBlockerAddedPayload(raw)
	transitionRequireFault(t, err, code)
	if !reflect.DeepEqual(decoded, TaskBlockerAddedPayload{}) {
		t.Fatal("failed added Decode returned partial result")
	}
}

func blockerRejectResolved(t *testing.T, raw []byte, code f.Code) {
	t.Helper()
	comment := "original resolution"
	got := TaskBlockerResolvedPayload{BlockerID: testID[TaskBlockerIdentity](t, 201), BlockerType: TaskBlockerRelyOn, ResolutionComment: &comment}
	before := got.Clone()
	transitionRequireFault(t, got.UnmarshalJSON(raw), code)
	if !reflect.DeepEqual(got, before) {
		t.Fatal("failed resolved decode changed receiver")
	}
	decoded, err := DecodeTaskBlockerResolvedPayload(raw)
	transitionRequireFault(t, err, code)
	if !reflect.DeepEqual(decoded, TaskBlockerResolvedPayload{}) {
		t.Fatal("failed resolved Decode returned partial result")
	}
}

func TestTaskBlockerIdentityAndType(t *testing.T) {
	// Assignment proves this public ID uses the single Foundation marker.
	var id TaskBlockerID = testID[TaskBlockerIdentity](t, 201)
	var foundationID f.ID[TaskBlockerIdentity] = id
	if foundationID != id || id.Validate() != nil || (TaskBlockerID{}).Validate() == nil {
		t.Fatal("blocker identity marker or zero semantics")
	}
	for _, bad := range []string{`null`, `0`, `""`, `"00000000-0000-0000-0000-000000000000"`, `"01900000-0000-4000-8000-0000000000c9"`, `"01900000-0000-7000-c000-0000000000c9"`, `"01900000-0000-7000-8000-0000000000C9"`} {
		got := id
		if got.UnmarshalJSON([]byte(bad)) == nil || got != id {
			t.Fatal("invalid ID accepted or changed receiver", bad)
		}
	}
	for _, tc := range []struct {
		kind TaskBlockerType
		wire string
	}{
		{TaskBlockerRelyOn, `"rely_on"`},
		{TaskBlockerWaitingForHuman, `"waiting_for_human"`},
		{TaskBlockerWaitingForMeetingApproval, `"waiting_for_meeting_approval"`},
		{TaskBlockerTechnical, `"technical"`},
		{TaskBlockerUserCancelledExecution, `"user_cancelled_execution"`},
	} {
		if err := tc.kind.Validate(); err != nil {
			t.Fatal("known type rejected", err)
		}
		raw, err := tc.kind.MarshalJSON()
		if err != nil || string(raw) != tc.wire {
			t.Fatal("type wire changed", err)
		}
		var got TaskBlockerType
		if got.UnmarshalJSON(raw) != nil || got != tc.kind {
			t.Fatal("type roundtrip failed")
		}
	}
	for _, bad := range []string{`""`, `"RELY_ON"`, `"unknown"`, `" rely_on"`, `null`, `1`, `true`, `[]`, `{}`, `"rely_on" "technical"`} {
		got := TaskBlockerRelyOn
		transitionRequireFault(t, got.UnmarshalJSON([]byte(bad)), f.InvalidArgument)
		if got != TaskBlockerRelyOn {
			t.Fatal("bad type decode changed receiver")
		}
	}
	for _, bad := range []TaskBlockerType{"", "RELY_ON", "unknown"} {
		blockerField(t, bad.Validate(), "/type", "INVALID_BLOCKER_TYPE")
		raw, err := bad.MarshalJSON()
		transitionRequireFault(t, err, f.InvalidArgument)
		if raw != nil {
			t.Fatal("invalid type marshaled")
		}
	}
	for _, kind := range blockerUnboundTypes() {
		v := blockerFixture(t, kind)
		transitionRequireFault(t, v.Validate(), f.DependencyUnbound)
		transitionRequireFault(t, (TaskBlockerAddedPayload{BlockerID: id, BlockerType: kind}).Validate(), f.DependencyUnbound)
		if kind != TaskBlockerTechnical {
			transitionRequireFault(t, (TaskBlockerResolvedPayload{BlockerID: id, BlockerType: kind}).Validate(), f.DependencyUnbound)
		} else if (TaskBlockerResolvedPayload{BlockerID: id, BlockerType: kind}).Validate() != nil {
			t.Fatal("technical transition resolution shape")
		}
	}
}

func TestTaskBlockerMetadataUnion(t *testing.T) {
	rely := *blockerFixture(t, TaskBlockerRelyOn).Metadata.RelyOn
	waiting := TaskBlockerWaitingForHumanMetadata{}
	if rely.Validate() != nil || waiting.Validate() != nil || rely.Clone() != rely || waiting.Clone() != waiting {
		t.Fatal("concrete metadata validation/clone")
	}
	relyRaw := taskRaw(t, rely)
	if len(relyRaw) != 58 || string(taskRaw(t, waiting)) != `{}` {
		t.Fatal("unexpected metadata wire")
	}
	for _, kind := range []TaskBlockerType{TaskBlockerRelyOn, TaskBlockerWaitingForHuman} {
		if err := blockerFixture(t, kind).Metadata.ValidateFor(kind); err != nil {
			t.Fatal("matching union rejected", err)
		}
		for _, union := range []TaskBlockerMetadata{{}, {RelyOn: &rely, WaitingForHuman: &waiting}, {RelyOn: new(TaskBlockerRelyOnMetadata)}} {
			blockerField(t, union.ValidateFor(kind), "/metadata", "INVALID_BLOCKER_METADATA")
			v := blockerFixture(t, kind)
			v.Metadata = union
			blockerField(t, v.Validate(), "/metadata", "INVALID_BLOCKER_METADATA")
			encoded, err := v.MarshalJSON()
			transitionRequireFault(t, err, f.InvalidArgument)
			if encoded != nil {
				t.Fatal("invalid union emitted create bytes")
			}
		}
	}
	for _, tc := range []struct {
		kind  TaskBlockerType
		union TaskBlockerMetadata
	}{{TaskBlockerRelyOn, TaskBlockerMetadata{WaitingForHuman: &waiting}}, {TaskBlockerWaitingForHuman, TaskBlockerMetadata{RelyOn: &rely}}} {
		blockerField(t, tc.union.ValidateFor(tc.kind), "/metadata", "INVALID_BLOCKER_METADATA")
	}
	for _, kind := range blockerUnboundTypes() {
		for _, union := range []TaskBlockerMetadata{{}, {RelyOn: &rely}, {RelyOn: &rely, WaitingForHuman: &waiting}} {
			transitionRequireFault(t, union.ValidateFor(kind), f.DependencyUnbound)
		}
	}
	transitionRequireFault(t, (TaskBlockerMetadata{}).ValidateFor("unknown"), f.InvalidArgument)
	blockerField(t, (TaskBlockerRelyOnMetadata{}).Validate(), "/related_task_id", "INVALID_RELATED_TASK_ID")
	for _, raw := range [][]byte{
		[]byte(`{}`), []byte(`null`), []byte(`[]`),
		taskJSONChange(t, relyRaw, "related_task_id", json.RawMessage(`null`)),
		taskJSONChange(t, relyRaw, "related_task_id", json.RawMessage(`1`)),
		taskJSONChange(t, relyRaw, "related_task_id", json.RawMessage(`"01900000-0000-7000-8000-0000000000CA"`)),
		taskJSONChange(t, relyRaw, "RELATED_TASK_ID", json.RawMessage(`"x"`)),
		[]byte(string(relyRaw[:len(relyRaw)-1]) + `,"related_task_id":null}`),
		taskJSONChange(t, relyRaw, "unknown", json.RawMessage(`true`)),
		[]byte(`{"RELATED_TASK_ID":"` + rely.RelatedTaskID.String() + `"}`),
	} {
		got := rely
		transitionRequireFault(t, got.UnmarshalJSON(raw), f.InvalidArgument)
		if got != rely {
			t.Fatal("bad rely metadata changed receiver")
		}
		blockerRejectCreate(t, blockerWire(t, TaskBlockerRelyOn, "", string(raw)), f.InvalidArgument)
	}
	for _, raw := range []string{`null`, `[]`, `1`, `""`, `{"reference_type":"meeting"}`, `{"reference_id":"x"}`, `{"related_task_id":null}`, `{"unknown":1}`, `{"Reference_ID":null}`} {
		transitionRequireFault(t, new(TaskBlockerWaitingForHumanMetadata).UnmarshalJSON([]byte(raw)), f.InvalidArgument)
		blockerRejectCreate(t, blockerWire(t, TaskBlockerWaitingForHuman, "", raw), f.InvalidArgument)
	}
	escaped := bytes.Replace(relyRaw, []byte(`related_task_id`), []byte(`related_t\u0061sk_id`), 1)
	var decoded TaskBlockerRelyOnMetadata
	if decoded.UnmarshalJSON(escaped) != nil || decoded != rely {
		t.Fatal("escaped valid key rejected")
	}
	if _, err := DecodeTaskBlockerCreate(blockerWire(t, TaskBlockerRelyOn, "", string(escaped))); err != nil {
		t.Fatal("nested escaped valid key rejected", err)
	}
	// Public metadata has concrete typed branches, without an untyped escape hatch.
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
			t.Fatalf("untyped public metadata escape hatch: %v", typ)
		}
	}
	check(reflect.TypeFor[TaskBlockerCreate]())
}

func TestTaskBlockerCreateCodecAndText(t *testing.T) {
	for _, kind := range []TaskBlockerType{TaskBlockerRelyOn, TaskBlockerWaitingForHuman} {
		base := blockerFixture(t, kind)
		raw := taskRaw(t, base)
		got, err := DecodeTaskBlockerCreate(raw)
		if err != nil || !reflect.DeepEqual(got, base) {
			t.Fatal("create roundtrip", err)
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil || len(fields) != 4 || fields["metadata"] == nil {
			t.Fatal("create wire is not four exact fields")
		}
		for _, key := range strings.Fields("blocker_id type description metadata") {
			for _, replacement := range []json.RawMessage{nil, json.RawMessage(`null`)} {
				blockerRejectCreate(t, taskJSONChange(t, raw, key, replacement), f.InvalidArgument)
			}
			changed := taskJSONChange(t, raw, key, nil)
			blockerRejectCreate(t, taskJSONChange(t, changed, strings.ToUpper(key), fields[key]), f.InvalidArgument)
			blockerRejectCreate(t, []byte(string(raw[:len(raw)-1])+`,"`+key+`":null}`), f.InvalidArgument)
		}
		for _, key := range strings.Fields("task_id project_id actor version cause status resolved_at resolved_by resolution_comment RelyOn WaitingForHuman unknown") {
			blockerRejectCreate(t, taskJSONChange(t, raw, key, json.RawMessage(`"injected"`)), f.InvalidArgument)
		}
		for _, text := range []string{"", " \t\n\r ", "\u2003", " e\u0301 é \t\n\r ", strings.Repeat("x", 1024), strings.Repeat("😀", 256), strings.Repeat("界", 341) + "x"} {
			v := base.Clone()
			v.Description = text
			if err := v.Validate(); err != nil {
				t.Fatal("legal description rejected", len(text), err)
			}
			decoded, err := DecodeTaskBlockerCreate(taskRaw(t, v))
			if err != nil || decoded.Description != text {
				t.Fatal("description bytes not preserved", err)
			}
		}
		for _, text := range []string{strings.Repeat("x", 1025), strings.Repeat("😀", 256) + "x", strings.Repeat("界", 342), "a\x00b", "a\x7fb", "a\u0085b", string([]byte{0xff})} {
			v := base.Clone()
			v.Description = text
			blockerField(t, v.Validate(), "/description", "INVALID_BLOCKER_DESCRIPTION")
			encoded, err := v.MarshalJSON()
			transitionRequireFault(t, err, f.InvalidArgument)
			if encoded != nil {
				t.Fatal("invalid description produced bytes")
			}
			if text != string([]byte{0xff}) {
				blockerRejectCreate(t, taskJSONChange(t, raw, "description", taskRaw(t, text)), f.InvalidArgument)
			}
		}
		for _, tc := range []struct{ key, value string }{{"blocker_id", `1`}, {"type", `false`}, {"description", `1`}, {"description", `[]`}, {"metadata", `[]`}, {"metadata", `"x"`}} {
			blockerRejectCreate(t, taskJSONChange(t, raw, tc.key, json.RawMessage(tc.value)), f.InvalidArgument)
		}
	}
	v := blockerFixture(t, TaskBlockerRelyOn)
	v.BlockerID = TaskBlockerID{}
	blockerField(t, v.Validate(), "/blocker_id", "INVALID_BLOCKER_ID")
	v = blockerFixture(t, TaskBlockerRelyOn)
	v.Type = "unknown"
	blockerField(t, v.Validate(), "/type", "INVALID_BLOCKER_TYPE")
}

func TestTaskBlockerHistoryPayloads(t *testing.T) {
	for _, kind := range []TaskBlockerType{TaskBlockerRelyOn, TaskBlockerWaitingForHuman} {
		id := testID[TaskBlockerIdentity](t, 201)
		added := TaskBlockerAddedPayload{BlockerID: id, BlockerType: kind}
		resolved := TaskBlockerResolvedPayload{BlockerID: id, BlockerType: kind}
		araw, rraw := taskRaw(t, added), taskRaw(t, resolved)
		if got, err := DecodeTaskBlockerAddedPayload(araw); err != nil || got != added {
			t.Fatal("added roundtrip", err)
		}
		if got, err := DecodeTaskBlockerResolvedPayload(rraw); err != nil || !reflect.DeepEqual(got, resolved) || !bytes.Contains(rraw, []byte(`"resolution_comment":null`)) {
			t.Fatal("resolved null roundtrip", err)
		}
		for _, tc := range []struct {
			raw    []byte
			keys   []string
			reject func(*testing.T, []byte, f.Code)
		}{{araw, strings.Fields("blocker_id blocker_type"), blockerRejectAdded}, {rraw, strings.Fields("blocker_id blocker_type resolution_comment"), blockerRejectResolved}} {
			var fields map[string]json.RawMessage
			if json.Unmarshal(tc.raw, &fields) != nil || len(fields) != len(tc.keys) {
				t.Fatal("history payload field count")
			}
			for _, key := range tc.keys {
				tc.reject(t, taskJSONChange(t, tc.raw, key, nil), f.InvalidArgument)
				if key != "resolution_comment" {
					tc.reject(t, taskJSONChange(t, tc.raw, key, json.RawMessage(`null`)), f.InvalidArgument)
				}
				without := taskJSONChange(t, tc.raw, key, nil)
				tc.reject(t, taskJSONChange(t, without, strings.ToUpper(key), fields[key]), f.InvalidArgument)
				tc.reject(t, []byte(string(tc.raw[:len(tc.raw)-1])+`,"`+key+`":null}`), f.InvalidArgument)
			}
			for _, key := range strings.Fields("description metadata related_task_id type actor created_at task_version unknown") {
				tc.reject(t, taskJSONChange(t, tc.raw, key, json.RawMessage(`"injected"`)), f.InvalidArgument)
			}
		}
		for _, comment := range []string{"x", " \t\n\r e\u0301 \u2003 ", strings.Repeat("😀", 256), strings.Repeat("x", 1024)} {
			v := resolved
			v.ResolutionComment = &comment
			if err := v.Validate(); err != nil {
				t.Fatal("legal resolution comment rejected", err)
			}
			got, err := DecodeTaskBlockerResolvedPayload(taskRaw(t, v))
			if err != nil || got.ResolutionComment == nil || *got.ResolutionComment != comment {
				t.Fatal("resolution comment bytes changed", err)
			}
		}
		for _, comment := range []string{"", " \t\n\r ", "\u2003\u00a0", strings.Repeat("x", 1025), strings.Repeat("😀", 256) + "x", "a\x00b", "a\x7fb", "a\u0085b", string([]byte{0xff})} {
			v := resolved
			v.ResolutionComment = &comment
			blockerField(t, v.Validate(), "/resolution_comment", "INVALID_RESOLUTION_COMMENT")
			encoded, err := v.MarshalJSON()
			transitionRequireFault(t, err, f.InvalidArgument)
			if encoded != nil {
				t.Fatal("bad resolved payload produced bytes")
			}
			if comment != string([]byte{0xff}) {
				blockerRejectResolved(t, taskJSONChange(t, rraw, "resolution_comment", taskRaw(t, comment)), f.InvalidArgument)
			}
		}
		for _, value := range []string{`1`, `false`, `[]`, `{}`} {
			blockerRejectResolved(t, taskJSONChange(t, rraw, "resolution_comment", json.RawMessage(value)), f.InvalidArgument)
		}
		for _, unsupported := range blockerUnboundTypes() {
			blockerRejectAdded(t, taskJSONChange(t, araw, "blocker_type", taskRaw(t, string(unsupported))), f.DependencyUnbound)
			blockerRejectResolved(t, taskJSONChange(t, rraw, "blocker_type", taskRaw(t, string(unsupported))), f.DependencyUnbound)
		}
		for _, bad := range []string{"", "RELY_ON", "unknown"} {
			added.BlockerType, resolved.BlockerType = TaskBlockerType(bad), TaskBlockerType(bad)
			blockerField(t, added.Validate(), "/blocker_type", "INVALID_BLOCKER_TYPE")
			blockerField(t, resolved.Validate(), "/blocker_type", "INVALID_BLOCKER_TYPE")
		}
		added.BlockerType, resolved.BlockerType = kind, kind
		added.BlockerID, resolved.BlockerID = TaskBlockerID{}, TaskBlockerID{}
		blockerField(t, added.Validate(), "/blocker_id", "INVALID_BLOCKER_ID")
		blockerField(t, resolved.Validate(), "/blocker_id", "INVALID_BLOCKER_ID")
	}
}

func TestTaskBlockerRawCapsAndAtomicDecode(t *testing.T) {
	if MaxTaskBlockerTypeBytes != 64 || MaxTaskBlockerMetadataBytes != 1024 || MaxTaskBlockerDescriptionBytes != 1024 || MaxTaskBlockerResolutionCommentBytes != 1024 || MaxTaskBlockerCreateBytes != 8192 || MaxTaskBlockerPayloadBytes != MaxTaskHistoryPayloadBytes {
		t.Fatal("frozen blocker caps changed")
	}
	base := blockerFixture(t, TaskBlockerRelyOn)
	kind := TaskBlockerRelyOn
	rely := *base.Metadata.RelyOn
	waiting := TaskBlockerWaitingForHumanMetadata{}
	create := base.Clone()
	added := TaskBlockerAddedPayload{BlockerID: base.BlockerID, BlockerType: base.Type}
	comment := "existing comment"
	resolved := TaskBlockerResolvedPayload{BlockerID: base.BlockerID, BlockerType: base.Type, ResolutionComment: &comment}
	for _, tc := range []struct {
		name    string
		raw     []byte
		limit   int
		decode  func([]byte) error
		current func() any
	}{
		{"type", taskRaw(t, kind), MaxTaskBlockerTypeBytes, kind.UnmarshalJSON, func() any { return kind }},
		{"rely_metadata", taskRaw(t, rely), MaxTaskBlockerMetadataBytes, rely.UnmarshalJSON, func() any { return rely.Clone() }},
		{"waiting_metadata", taskRaw(t, waiting), MaxTaskBlockerMetadataBytes, waiting.UnmarshalJSON, func() any { return waiting.Clone() }},
		{"create", taskRaw(t, create), MaxTaskBlockerCreateBytes, create.UnmarshalJSON, func() any { return create.Clone() }},
		{"added", taskRaw(t, added), MaxTaskBlockerPayloadBytes, added.UnmarshalJSON, func() any { return added.Clone() }},
		{"resolved", taskRaw(t, resolved), MaxTaskBlockerPayloadBytes, resolved.UnmarshalJSON, func() any { return resolved.Clone() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Call the custom codec directly: json.Unmarshal would remove this padding.
			at := append(bytes.Repeat([]byte(" "), tc.limit-len(tc.raw)-1), tc.raw...)
			at = append(at, '\n')
			if len(at) != tc.limit || tc.decode(at) != nil {
				t.Fatal("exact complete raw cap rejected")
			}
			before := tc.current()
			for _, bad := range [][]byte{append(bytes.Clone(at), ' '), append([]byte(" "), at...), append(bytes.Clone(tc.raw), []byte(" {}")...), []byte(`null`), []byte(`[]`), []byte(`"\ud800"`), []byte(`"\udc00"`), []byte("\"\xff\"")} {
				transitionRequireFault(t, tc.decode(bad), f.InvalidArgument)
				if !reflect.DeepEqual(tc.current(), before) {
					t.Fatal("failed codec mutated receiver")
				}
			}
		})
	}
	for _, tc := range []struct {
		raw    []byte
		decode func([]byte) error
		reject func(*testing.T, []byte, f.Code)
	}{
		{taskRaw(t, base), func(raw []byte) error { _, err := DecodeTaskBlockerCreate(raw); return err }, blockerRejectCreate},
		{taskRaw(t, added), func(raw []byte) error { _, err := DecodeTaskBlockerAddedPayload(raw); return err }, blockerRejectAdded},
		{taskRaw(t, resolved), func(raw []byte) error { _, err := DecodeTaskBlockerResolvedPayload(raw); return err }, blockerRejectResolved},
	} {
		at := append(bytes.Repeat([]byte(" "), 8192-len(tc.raw)-1), tc.raw...)
		at = append(at, '\n')
		if err := tc.decode(at); err != nil {
			t.Fatal("Decode exact complete raw cap rejected", err)
		}
		tc.reject(t, append(bytes.Clone(at), ' '), f.InvalidArgument)
		for _, bad := range [][]byte{[]byte(`null`), []byte(`[]`), []byte(`"x"`), []byte(`{}`), {}, append(bytes.Clone(tc.raw), []byte(" {}")...)} {
			tc.reject(t, bad, f.InvalidArgument)
		}
	}
	for _, supported := range []TaskBlockerType{TaskBlockerRelyOn, TaskBlockerWaitingForHuman} {
		var canonical []byte
		if supported == TaskBlockerRelyOn {
			canonical = taskRaw(t, rely)
		} else {
			canonical = []byte(`{}`)
		}
		for _, size := range []int{1024, 1025} {
			// Internal whitespace belongs to metadata's lexical range. A JSON
			// re-marshal here would compact it and invalidate the negative test.
			metadata := "{" + strings.Repeat(" ", size-len(canonical)) + string(canonical[1:])
			raw := blockerWire(t, supported, "", metadata)
			if len(raw) >= MaxTaskBlockerCreateBytes || len(metadata) != size {
				t.Fatal("nested cap fixture did not isolate metadata")
			}
			if size == 1024 {
				if _, err := DecodeTaskBlockerCreate(raw); err != nil {
					t.Fatal("exact nested metadata cap rejected", err)
				}
			} else {
				blockerRejectCreate(t, raw, f.InvalidArgument)
			}
		}
		// Whitespace after the colon is outside the metadata token range.
		outerSpace := blockerWire(t, supported, "", strings.Repeat(" ", 1025)+string(canonical))
		if _, err := DecodeTaskBlockerCreate(outerSpace); err != nil {
			t.Fatal("outer field whitespace incorrectly counted as metadata", err)
		}
	}
	for _, tc := range []struct {
		kind                                   TaskBlockerType
		createBytes, addedBytes, resolvedBytes int
	}{{TaskBlockerRelyOn, 6301, 78, 6246}, {TaskBlockerWaitingForHuman, 6255, 88, 6256}} {
		for _, unit := range []string{"<", ">", "&"} {
			v := blockerFixture(t, tc.kind)
			v.Description = strings.Repeat(unit, 1024)
			raw, err := v.MarshalJSON()
			if err != nil || len(raw) != tc.createBytes {
				t.Fatal("max escaped create does not fit frozen arithmetic", len(raw), err)
			}
			if got, err := DecodeTaskBlockerCreate(raw); err != nil || !reflect.DeepEqual(got, v) || !reflect.DeepEqual(got.Clone(), v) {
				t.Fatal("maximum create cannot roundtrip/clone", err)
			}
			ap := TaskBlockerAddedPayload{BlockerID: v.BlockerID, BlockerType: tc.kind}
			if raw := taskRaw(t, ap); len(raw) != tc.addedBytes {
				t.Fatal("added payload frozen arithmetic", len(raw))
			}
			rp := TaskBlockerResolvedPayload{BlockerID: v.BlockerID, BlockerType: tc.kind, ResolutionComment: &v.Description}
			raw, err = rp.MarshalJSON()
			if err != nil || len(raw) != tc.resolvedBytes {
				t.Fatal("max escaped resolved does not fit frozen arithmetic", len(raw), err)
			}
			if got, err := DecodeTaskBlockerResolvedPayload(raw); err != nil || !reflect.DeepEqual(got, rp) || !reflect.DeepEqual(got.Clone(), rp) {
				t.Fatal("maximum resolved cannot roundtrip/clone", err)
			}
		}
	}
	raw := taskRaw(t, base)
	for _, malformed := range []string{`"\ud800"`, `"\udc00"`, `"\ud800\u0041"`} {
		blockerRejectCreate(t, taskJSONChange(t, raw, "description", json.RawMessage(malformed)), f.InvalidArgument)
		blockerRejectResolved(t, taskJSONChange(t, taskRaw(t, resolved), "resolution_comment", json.RawMessage(malformed)), f.InvalidArgument)
	}
	blockerRejectCreate(t, bytes.Replace(raw, []byte("original"), []byte{0xff}, 1), f.InvalidArgument)
	blockerRejectResolved(t, bytes.Replace(taskRaw(t, resolved), []byte("existing"), []byte{0xff}, 1), f.InvalidArgument)
	blockerRejectCreate(t, []byte(string(raw[:len(raw)-1])+`,"descrip\u0074ion":"duplicate"}`), f.InvalidArgument)
	addedRaw, resolvedRaw := taskRaw(t, added), taskRaw(t, resolved)
	blockerRejectAdded(t, []byte(string(addedRaw[:len(addedRaw)-1])+`,"blocker_t\u0079pe":"rely_on"}`), f.InvalidArgument)
	blockerRejectResolved(t, []byte(string(resolvedRaw[:len(resolvedRaw)-1])+`,"resolution_c\u006fmment":null}`), f.InvalidArgument)
	relyRaw := taskRaw(t, rely)
	duplicate := string(relyRaw[:len(relyRaw)-1]) + `,"related_t\u0061sk_id":"` + rely.RelatedTaskID.String() + `"}`
	blockerRejectCreate(t, blockerWire(t, TaskBlockerRelyOn, "", duplicate), f.InvalidArgument)
	beforeRely := rely
	transitionRequireFault(t, rely.UnmarshalJSON([]byte(duplicate)), f.InvalidArgument)
	if rely != beforeRely {
		t.Fatal("escaped duplicate metadata changed receiver")
	}
	for _, unsupported := range blockerUnboundTypes() {
		for _, metadata := range []string{`{}`, `{"not_a_frozen_schema":true}`} {
			blockerRejectCreate(t, blockerWire(t, unsupported, "", metadata), f.DependencyUnbound)
		}
		for _, metadata := range []string{`null`, `[]`, `1`, `"x"`, "{" + strings.Repeat(" ", 1024) + "}", `{"x":1,"x":2}`, `{"x":"\ud800"}`} {
			blockerRejectCreate(t, blockerWire(t, unsupported, "", metadata), f.InvalidArgument)
		}
		unboundRaw := blockerWire(t, unsupported, "", `{}`)
		for _, tc := range []struct{ key, value string }{{"blocker_id", `""`}, {"description", `1`}, {"description", `"\u0000"`}, {"unknown", `true`}} {
			blockerRejectCreate(t, taskJSONChange(t, unboundRaw, tc.key, json.RawMessage(tc.value)), f.InvalidArgument)
		}
		blockerRejectCreate(t, blockerWire(t, unsupported, strings.Repeat("x", 1025), `{}`), f.InvalidArgument)
		v := blockerFixture(t, unsupported)
		v.Description = strings.Repeat("x", 1025)
		blockerField(t, v.Validate(), "/description", "INVALID_BLOCKER_DESCRIPTION")
		v.Description = ""
		v.BlockerID = TaskBlockerID{}
		blockerField(t, v.Validate(), "/blocker_id", "INVALID_BLOCKER_ID")
		ap := TaskBlockerAddedPayload{BlockerID: base.BlockerID, BlockerType: unsupported}
		rp := TaskBlockerResolvedPayload{BlockerID: base.BlockerID, BlockerType: unsupported}
		marshals := []func() ([]byte, error){ap.MarshalJSON, blockerFixture(t, unsupported).MarshalJSON}
		if unsupported != TaskBlockerTechnical {
			marshals = append(marshals, rp.MarshalJSON)
		}
		for _, marshal := range marshals {
			encoded, err := marshal()
			transitionRequireFault(t, err, f.DependencyUnbound)
			if encoded != nil {
				t.Fatal("unbound type emitted bytes")
			}
		}
		// Build raw forms without asking an unbound value to marshal successfully.
		apRaw := taskJSONChange(t, taskRaw(t, added), "blocker_type", taskRaw(t, string(unsupported)))
		rpRaw := taskJSONChange(t, taskRaw(t, resolved), "blocker_type", taskRaw(t, string(unsupported)))
		blockerRejectAdded(t, taskJSONChange(t, apRaw, "blocker_id", json.RawMessage(`1`)), f.InvalidArgument)
		blockerRejectAdded(t, taskJSONChange(t, apRaw, "unknown", json.RawMessage(`1`)), f.InvalidArgument)
		blockerRejectResolved(t, taskJSONChange(t, rpRaw, "resolution_comment", nil), f.InvalidArgument)
		blockerRejectResolved(t, taskJSONChange(t, rpRaw, "resolution_comment", json.RawMessage(`" "`)), f.InvalidArgument)
		blockerRejectResolved(t, taskJSONChange(t, rpRaw, "blocker_id", json.RawMessage(`null`)), f.InvalidArgument)
		blank := " "
		rp.ResolutionComment = &blank
		blockerField(t, rp.Validate(), "/resolution_comment", "INVALID_RESOLUTION_COMMENT")
	}
	var nilType *TaskBlockerType
	var nilRely *TaskBlockerRelyOnMetadata
	var nilWaiting *TaskBlockerWaitingForHumanMetadata
	var nilCreate *TaskBlockerCreate
	var nilAdded *TaskBlockerAddedPayload
	var nilResolved *TaskBlockerResolvedPayload
	for _, decode := range []func([]byte) error{nilType.UnmarshalJSON, nilRely.UnmarshalJSON, nilWaiting.UnmarshalJSON, nilCreate.UnmarshalJSON, nilAdded.UnmarshalJSON, nilResolved.UnmarshalJSON} {
		transitionRequireFault(t, decode(nil), f.InvalidArgument)
		transitionRequireFault(t, decode(raw), f.InvalidArgument)
	}
}

func TestTaskBlockerCloneAndSafeLog(t *testing.T) {
	base := blockerFixture(t, TaskBlockerRelyOn)
	clone := base.Clone()
	if !reflect.DeepEqual(clone, base) || clone.Metadata.RelyOn == base.Metadata.RelyOn {
		t.Fatal("create clone borrows rely metadata")
	}
	clone.Metadata.RelyOn.RelatedTaskID = testID[Task](t, 203)
	if base.Metadata.RelyOn.RelatedTaskID != testID[Task](t, 202) {
		t.Fatal("clone mutation changed original rely metadata")
	}
	waiting := blockerFixture(t, TaskBlockerWaitingForHuman)
	waitingClone := waiting.Clone()
	if !reflect.DeepEqual(waitingClone, waiting) || waitingClone.Metadata.RelyOn != nil || waitingClone.Metadata.WaitingForHuman == nil || waitingClone.Metadata.WaitingForHuman == waiting.Metadata.WaitingForHuman {
		t.Fatal("waiting clone changed branch or nil")
	}
	// The frozen Clone API requires independent branch pointers even for the
	// empty concrete metadata; the implementation must avoid shared zero storage.
	waitingClone.Metadata.WaitingForHuman = nil
	if waiting.Metadata.WaitingForHuman == nil {
		t.Fatal("changing cloned waiting branch changed original")
	}
	both := TaskBlockerMetadata{RelyOn: base.Metadata.RelyOn, WaitingForHuman: waiting.Metadata.WaitingForHuman}
	bothClone := both.Clone()
	if !reflect.DeepEqual(bothClone, both) || bothClone.RelyOn == both.RelyOn || bothClone.WaitingForHuman == nil || bothClone.WaitingForHuman == both.WaitingForHuman {
		t.Fatal("union clone lost branch or borrows mutable pointer")
	}
	bothClone.RelyOn.RelatedTaskID = testID[Task](t, 204)
	if both.RelyOn.RelatedTaskID != testID[Task](t, 202) || !reflect.DeepEqual((TaskBlockerMetadata{}).Clone(), TaskBlockerMetadata{}) {
		t.Fatal("union clone modified original or changed zero")
	}
	comment := "private resolution <body>"
	resolved := TaskBlockerResolvedPayload{BlockerID: base.BlockerID, BlockerType: base.Type, ResolutionComment: &comment}
	resolvedClone := resolved.Clone()
	if !reflect.DeepEqual(resolvedClone, resolved) || resolvedClone.ResolutionComment == resolved.ResolutionComment {
		t.Fatal("resolved clone borrows comment")
	}
	*resolvedClone.ResolutionComment = "changed"
	if *resolved.ResolutionComment != comment || (TaskBlockerResolvedPayload{}).Clone().ResolutionComment != nil {
		t.Fatal("resolved clone changed original or nil")
	}
	added := TaskBlockerAddedPayload{BlockerID: base.BlockerID, BlockerType: base.Type}
	if added.Clone() != added {
		t.Fatal("added clone changed value")
	}
	kind := base.Type
	rely := *base.Metadata.RelyOn
	empty := *waiting.Metadata.WaitingForHuman
	metadata := base.Metadata
	for _, value := range []any{kind, &kind, rely, &rely, empty, &empty, metadata, &metadata, base, &base, added, &added, resolved, &resolved} {
		for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x"} {
			if got := fmt.Sprintf(format, value); got != "work_task_transition" {
				t.Fatalf("unsafe direct blocker format: %q", got)
			}
		}
		var output bytes.Buffer
		slog.New(slog.NewJSONHandler(&output, nil)).Info("safe", "value", value)
		var record map[string]any
		if err := json.Unmarshal(output.Bytes(), &record); err != nil || record["value"] != "work_task_transition" {
			t.Fatal("unsafe direct blocker slog", err)
		}
	}
	raw := taskRaw(t, base)
	if !bytes.Contains(raw, []byte(base.BlockerID.String())) || !bytes.Contains(taskRaw(t, resolved), []byte(`private resolution \u003cbody\u003e`)) {
		t.Fatal("safe logging projection damaged business JSON")
	}
	bad := base.Clone()
	bad.Description = "private invalid description\x00"
	for _, err := range []error{bad.Validate(), blockerFixture(t, TaskBlockerTechnical).Validate(), (TaskBlockerRelyOnMetadata{}).Validate()} {
		code := f.InvalidArgument
		if err == nil {
			t.Fatal("missing expected safe fault")
		}
		if err.Error() == string(f.DependencyUnbound) {
			code = f.DependencyUnbound
		}
		fault := transitionRequireFault(t, err, code)
		encoded := taskRaw(t, fault)
		for _, needle := range []string{"private", "original", comment, base.BlockerID.String(), base.Metadata.RelyOn.RelatedTaskID.String()} {
			if bytes.Contains(encoded, []byte(needle)) || strings.Contains(fmt.Sprintf("%#v", fault), needle) {
				t.Fatal("fault exposed input")
			}
		}
	}
	for n := range 8 {
		t.Run(fmt.Sprintf("parallel_%d", n), func(t *testing.T) {
			t.Parallel()
			for range 32 {
				if err := base.Validate(); err != nil {
					t.Fatal(err)
				}
				copy := base.Clone()
				copy.Metadata.RelyOn.RelatedTaskID = testID[Task](t, 203)
				got, err := DecodeTaskBlockerCreate(raw)
				if err != nil || !reflect.DeepEqual(got, base) || !bytes.Equal(taskRaw(t, base), raw) {
					t.Fatal("parallel read changed shared input", err)
				}
				rp := resolved.Clone()
				*rp.ResolutionComment = "clone only"
				if err := resolved.Validate(); err != nil || *resolved.ResolutionComment != comment {
					t.Fatal("parallel comment clone affected original", err)
				}
			}
		})
	}
}

func TestTaskBlockerLegacyIsolation(t *testing.T) {
	history := taskHistoryFixture(t)
	historyRaw := taskRaw(t, history)
	if err := new(TaskEvent).UnmarshalJSON(historyRaw); err != nil {
		t.Fatal("legacy history positive rejected", err)
	}
	for _, name := range []string{"blocker_added", "blocker_resolved"} {
		kind := TaskEventType(name)
		transitionRequireFault(t, kind.Validate(), f.InvalidArgument)
		_, err := kind.MarshalJSON()
		transitionRequireFault(t, err, f.InvalidArgument)
		transitionRequireFault(t, new(TaskEventType).UnmarshalJSON(taskRaw(t, name)), f.InvalidArgument)
		transitionRequireFault(t, new(TaskEvent).UnmarshalJSON(taskJSONChange(t, historyRaw, "type", taskRaw(t, name))), f.InvalidArgument)
	}
	blocker := blockerFixture(t, TaskBlockerRelyOn)
	added := taskRaw(t, TaskBlockerAddedPayload{BlockerID: blocker.BlockerID, BlockerType: blocker.Type})
	resolved := taskRaw(t, TaskBlockerResolvedPayload{BlockerID: blocker.BlockerID, BlockerType: blocker.Type})
	for _, payload := range [][]byte{added, resolved} {
		transitionRequireFault(t, new(TaskEvent).UnmarshalJSON(taskJSONChange(t, historyRaw, "payload", payload)), f.InvalidArgument)
	}
	actorRaw := taskRaw(t, history.Actor)
	if err := new(TaskEventActor).UnmarshalJSON(actorRaw); err != nil {
		t.Fatal("legacy Human actor positive rejected", err)
	}
	for _, raw := range []string{
		`{"type":"agent","agent_id":"` + blocker.BlockerID.String() + `","execution_id":"` + blocker.BlockerID.String() + `","source":"task_domain"}`,
		`{"type":"system","service_name":"scheduler","cause_id":"` + blocker.BlockerID.String() + `","source":"scheduler"}`,
	} {
		transitionRequireFault(t, new(TaskEventActor).UnmarshalJSON([]byte(raw)), f.InvalidArgument)
		transitionRequireFault(t, new(TaskEvent).UnmarshalJSON(taskJSONChange(t, historyRaw, "actor", json.RawMessage(raw))), f.InvalidArgument)
	}
	mutationRaw := taskRaw(t, taskMutationFixture(t))
	_, _, changed := taskEventFixture(t)
	changedRaw := taskRaw(t, changed)
	for _, tc := range []struct {
		raw    []byte
		decode func([]byte) error
	}{{mutationRaw, new(TaskMutation).UnmarshalJSON}, {changedRaw, new(TaskChanged).UnmarshalJSON}, {historyRaw, new(TaskEvent).UnmarshalJSON}} {
		if err := tc.decode(tc.raw); err != nil {
			t.Fatal("legacy schema positive rejected", err)
		}
		for _, key := range []string{"blocker_id", "blocker_type", "metadata", "add_blockers", "resolve_blocker_ids", "task_event_ids"} {
			transitionRequireFault(t, tc.decode(taskJSONChange(t, tc.raw, key, json.RawMessage(`[]`))), f.InvalidArgument)
		}
	}
	transitionRequireFault(t, new(TaskMutation).UnmarshalJSON(taskJSONChange(t, mutationRaw, "task_event_id", json.RawMessage(`[]`))), f.InvalidArgument)
	transitionRequireFault(t, new(TaskChanged).UnmarshalJSON(taskJSONChange(t, changedRaw, "change", json.RawMessage(`"blocker_added"`))), f.InvalidArgument)
}
