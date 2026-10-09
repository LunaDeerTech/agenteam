// Independent public-API probes for the accepted B0-C contract.
// This file is overlaid into contract_test; no author test helpers are used.
package contract_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strconv"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	w "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

const independentBlockerUUID = "019ca123-45ab-7cde-9234-56789abcdef0"
const independentRelatedUUID = "019ca123-45ab-7cde-9234-56789abcdef1"

func independentBlockerID[T any](t *testing.T, raw string) f.ID[T] {
	t.Helper()
	id, err := f.ParseID[T](raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func independentBlockerBase(t *testing.T, kind w.TaskBlockerType) w.TaskBlockerCreate {
	t.Helper()
	v := w.TaskBlockerCreate{BlockerID: independentBlockerID[w.TaskBlockerIdentity](t, independentBlockerUUID), Type: kind, Description: "independent-sensitive-text"}
	if kind == w.TaskBlockerRelyOn {
		v.Metadata.RelyOn = &w.TaskBlockerRelyOnMetadata{RelatedTaskID: independentBlockerID[w.Task](t, independentRelatedUUID)}
	} else if kind == w.TaskBlockerWaitingForHuman {
		v.Metadata.WaitingForHuman = &w.TaskBlockerWaitingForHumanMetadata{}
	}
	return v
}

// Deliberately noncanonical field order and hand-built raw metadata preserve
// the caller's lexical input independently of the product's marshaler.
func independentBlockerWire(kind, description, metadata string) []byte {
	return []byte(`{"metadata":` + metadata + `,"description":` + strconv.Quote(description) + `,"type":` + strconv.Quote(kind) + `,"blocker_id":"` + independentBlockerUUID + `"}`)
}

func independentBlockerFault(t *testing.T, err error, code f.Code) *f.Fault {
	t.Helper()
	var fault *f.Fault
	if !errors.As(err, &fault) || fault == nil || fault.Code != code || fault.CommitState != f.NotStarted || !fault.Code.Known() || fault.Code.Safe() != code {
		t.Fatalf("wanted safe %s/not_started, got %v", code, err)
	}
	encoded, marshalErr := json.Marshal(fault)
	if marshalErr != nil || bytes.Contains(encoded, []byte("independent-sensitive")) || strings.Contains(fmt.Sprintf("%#v", err), "independent-sensitive") {
		t.Fatal("unsafe fault projection")
	}
	return fault
}

func independentBlockerReject(t *testing.T, raw []byte, code f.Code) {
	t.Helper()
	receiver := independentBlockerBase(t, w.TaskBlockerRelyOn)
	before := receiver.Clone()
	independentBlockerFault(t, receiver.UnmarshalJSON(raw), code)
	if !reflect.DeepEqual(receiver, before) {
		t.Fatal("failed decode changed existing receiver")
	}
	zero, err := w.DecodeTaskBlockerCreate(raw)
	independentBlockerFault(t, err, code)
	if !reflect.DeepEqual(zero, w.TaskBlockerCreate{}) {
		t.Fatal("Decode returned partial value")
	}
}

func TestIndependentBlockerWireAndPrecedence(t *testing.T) {
	kinds := []struct {
		text      string
		kind      w.TaskBlockerType
		metadata  string
		available bool
	}{
		{"rely_on", w.TaskBlockerRelyOn, `{"related_task_id":"` + independentRelatedUUID + `"}`, true},
		{"waiting_for_human", w.TaskBlockerWaitingForHuman, `{}`, true},
		{"waiting_for_meeting_approval", w.TaskBlockerWaitingForMeetingApproval, `{"future_schema":{"opaque":[1,true,null]}}`, false},
		{"technical", w.TaskBlockerTechnical, `{"arbitrary":"shape only"}`, false},
		{"user_cancelled_execution", w.TaskBlockerUserCancelledExecution, `{}`, false},
	}
	for _, tc := range kinds {
		if string(tc.kind) != tc.text || tc.kind.Validate() != nil {
			t.Fatal("enum declaration mismatch")
		}
		var enum w.TaskBlockerType
		if enum.UnmarshalJSON([]byte(strconv.Quote(tc.text))) != nil || enum != tc.kind {
			t.Fatal("recognized enum failed")
		}
		encoded, err := enum.MarshalJSON()
		if err != nil || string(encoded) != strconv.Quote(tc.text) {
			t.Fatal("enum wire mismatch")
		}
		raw := independentBlockerWire(tc.text, "", tc.metadata)
		if tc.available {
			v, err := w.DecodeTaskBlockerCreate(raw)
			if err != nil || v.Type != tc.kind || v.Description != "" || v.Validate() != nil {
				t.Fatal("supported kind rejected", err)
			}
			if tc.kind == w.TaskBlockerRelyOn && v.Metadata.RelyOn.RelatedTaskID.String() != independentRelatedUUID {
				t.Fatal("related ID lost")
			}
		} else {
			independentBlockerReject(t, raw, f.DependencyUnbound)
			v := independentBlockerBase(t, tc.kind)
			v.Metadata = independentBlockerBase(t, w.TaskBlockerRelyOn).Metadata
			v.Metadata.WaitingForHuman = &w.TaskBlockerWaitingForHumanMetadata{}
			independentBlockerFault(t, v.Validate(), f.DependencyUnbound)
			out, err := v.MarshalJSON()
			independentBlockerFault(t, err, f.DependencyUnbound)
			if out != nil {
				t.Fatal("unbound kind emitted bytes")
			}
			for _, metadata := range []string{`null`, `[]`, `1`, `"independent-sensitive"`, `{"x":1,"\u0078":2}`, `{"x":"\udfff"}`, `{` + strings.Repeat("\t", 1023) + `}`} {
				independentBlockerReject(t, independentBlockerWire(tc.text, "", metadata), f.InvalidArgument)
			}
			independentBlockerReject(t, independentBlockerWire(tc.text, "\x00", `{}`), f.InvalidArgument)
			independentBlockerReject(t, bytes.Replace(raw, []byte(independentBlockerUUID), []byte("bad-id"), 1), f.InvalidArgument)
		}
		for _, key := range []string{"metadata", "description", "type", "blocker_id"} {
			var fields map[string]json.RawMessage
			if json.Unmarshal(raw, &fields) != nil {
				t.Fatal("probe input")
			}
			original := fields[key]
			delete(fields, key)
			missing, _ := json.Marshal(fields)
			independentBlockerReject(t, missing, f.InvalidArgument)
			fields[strings.ToUpper(key)] = original
			aliased, _ := json.Marshal(fields)
			independentBlockerReject(t, aliased, f.InvalidArgument)
			delete(fields, strings.ToUpper(key))
			fields[key] = json.RawMessage(`null`)
			null, _ := json.Marshal(fields)
			independentBlockerReject(t, null, f.InvalidArgument)
			duplicate := append(bytes.Clone(raw[:len(raw)-1]), []byte(`,"`+key+`":null}`)...)
			independentBlockerReject(t, duplicate, f.InvalidArgument)
		}
	}
	for _, bad := range []string{"", "Rely_On", "technical ", "waiting_for_human\x00", "not_registered"} {
		independentBlockerReject(t, independentBlockerWire(bad, "", `{}`), f.InvalidArgument)
	}
	for _, metadata := range []string{`{"reference_type":"meeting"}`, `{"reference_id":"` + independentRelatedUUID + `"}`, `{"related_task_id":"` + independentRelatedUUID + `"}`} {
		independentBlockerReject(t, independentBlockerWire("waiting_for_human", "", metadata), f.InvalidArgument)
	}
	for _, metadata := range []string{`{}`, `{"related_task_id":null}`, `{"Related_Task_Id":"` + independentRelatedUUID + `"}`, `{"related_task_id":"` + independentRelatedUUID + `","related_t\u0061sk_id":"` + independentRelatedUUID + `"}`, `{"related_task_id":"` + independentRelatedUUID + `","extra":false}`} {
		independentBlockerReject(t, independentBlockerWire("rely_on", "", metadata), f.InvalidArgument)
	}
}

func TestIndependentBlockerLexicalLimits(t *testing.T) {
	if w.MaxTaskBlockerTypeBytes != 64 || w.MaxTaskBlockerMetadataBytes != 1024 || w.MaxTaskBlockerCreateBytes != 8192 || w.MaxTaskBlockerPayloadBytes != 8192 {
		t.Fatal("contract cap changed")
	}
	if w.MaxTaskBlockerPayloadBytes != w.MaxTaskHistoryPayloadBytes {
		t.Fatal("blocker payload no longer shares the existing history cap")
	}
	for _, tc := range []struct{ kind, metadata string }{{"waiting_for_human", `{}`}, {"rely_on", `{"related_task_id":"` + independentRelatedUUID + `"}`}, {"technical", `{"future":[true]}`}} {
		for _, n := range []int{1023, 1024, 1025} {
			// Whitespace before the closing brace is in the original metadata
			// token, and must survive all intermediate field parsing.
			meta := tc.metadata[:len(tc.metadata)-1] + strings.Repeat("\n", n-len(tc.metadata)) + "}"
			raw := independentBlockerWire(tc.kind, "", meta)
			_, err := w.DecodeTaskBlockerCreate(raw)
			if n > 1024 {
				independentBlockerFault(t, err, f.InvalidArgument)
			} else if tc.kind == "technical" {
				independentBlockerFault(t, err, f.DependencyUnbound)
			} else if err != nil {
				t.Fatal("legal nested boundary", err)
			}
		}
		outside := independentBlockerWire(tc.kind, "", strings.Repeat("\r\n", 700)+tc.metadata)
		_, err := w.DecodeTaskBlockerCreate(outside)
		if tc.kind == "technical" {
			independentBlockerFault(t, err, f.DependencyUnbound)
		} else if err != nil {
			t.Fatal("outer separator charged to metadata", err)
		}
	}
	create := independentBlockerWire("waiting_for_human", "", `{}`)
	added := []byte(`{"blocker_type":"rely_on","blocker_id":"` + independentBlockerUUID + `"}`)
	resolved := []byte(`{"resolution_comment":null,"blocker_type":"rely_on","blocker_id":"` + independentBlockerUUID + `"}`)
	for _, tc := range []struct {
		raw    []byte
		cap    int
		decode func([]byte) error
	}{
		{[]byte(`"technical"`), 64, new(w.TaskBlockerType).UnmarshalJSON},
		{[]byte(`{}`), 1024, new(w.TaskBlockerWaitingForHumanMetadata).UnmarshalJSON},
		{[]byte(`{"related_task_id":"` + independentRelatedUUID + `"}`), 1024, new(w.TaskBlockerRelyOnMetadata).UnmarshalJSON},
		{create, 8192, func(raw []byte) error { _, err := w.DecodeTaskBlockerCreate(raw); return err }},
		{added, 8192, func(raw []byte) error { _, err := w.DecodeTaskBlockerAddedPayload(raw); return err }},
		{resolved, 8192, func(raw []byte) error { _, err := w.DecodeTaskBlockerResolvedPayload(raw); return err }},
	} {
		padded := append([]byte(strings.Repeat("\t", tc.cap-len(tc.raw)-1)), tc.raw...)
		padded = append(padded, '\r')
		if len(padded) != tc.cap || tc.decode(padded) != nil {
			t.Fatal("exact full raw cap rejected")
		}
		for _, bad := range [][]byte{append(bytes.Clone(padded), ' '), append(bytes.Clone(tc.raw), []byte(` null`)...), append([]byte("\xef\xbb\xbf"), tc.raw...), {0xff}, []byte(`null`)} {
			independentBlockerFault(t, tc.decode(bad), f.InvalidArgument)
		}
	}
	// Alternate escaped key spellings are semantic keys, not aliases to reject.
	escaped := bytes.Replace(create, []byte(`"description"`), []byte(`"descripti\u006fn"`), 1)
	if _, err := w.DecodeTaskBlockerCreate(escaped); err != nil {
		t.Fatal("legal escaped field rejected", err)
	}
	duplicate := append(bytes.Clone(escaped[:len(escaped)-1]), []byte(`,"description":""}`)...)
	independentBlockerReject(t, duplicate, f.InvalidArgument)
	for _, body := range []string{`"\ud800"`, `"\udfff"`, `"\ud800\ud800"`, `"\udc00\ud800"`, `"\ud800x"`} {
		raw := bytes.Replace(create, []byte(`"description":""`), []byte(`"description":`+body), 1)
		independentBlockerReject(t, raw, f.InvalidArgument)
	}
	pair := bytes.Replace(create, []byte(`"description":""`), []byte(`"description":"\ud83d\ude80"`), 1)
	if got, err := w.DecodeTaskBlockerCreate(pair); err != nil || got.Description != "🚀" {
		t.Fatal("valid surrogate pair damaged", err)
	}
}

func TestIndependentBlockerTextAndEncoding(t *testing.T) {
	for _, kind := range []w.TaskBlockerType{w.TaskBlockerRelyOn, w.TaskBlockerWaitingForHuman} {
		base := independentBlockerBase(t, kind)
		// Mixed HTML characters each require six bytes under encoding/json.
		maxText := strings.Repeat("<&>", 341) + "&"
		base.Description = maxText
		raw, err := base.MarshalJSON()
		expected := 6301
		if kind == w.TaskBlockerWaitingForHuman {
			expected = 6255
		}
		if err != nil || len(raw) != expected || bytes.Count(raw, []byte(`\u00`)) != 1024 {
			t.Fatal("maximum create escaping", len(raw), err)
		}
		var object map[string]json.RawMessage
		if json.Unmarshal(raw, &object) != nil || len(object) != 4 {
			t.Fatal("create schema grew")
		}
		var decodedText string
		if json.Unmarshal(object["description"], &decodedText) != nil || decodedText != maxText {
			t.Fatal("business text not preserved")
		}
		got, err := w.DecodeTaskBlockerCreate(raw)
		if err != nil || !reflect.DeepEqual(got, base) {
			t.Fatal("maximum create not consumable", err)
		}
		ap := w.TaskBlockerAddedPayload{BlockerID: base.BlockerID, BlockerType: kind}
		araw, err := ap.MarshalJSON()
		expectedAdded := 78
		if kind == w.TaskBlockerWaitingForHuman {
			expectedAdded = 88
		}
		if err != nil || len(araw) != expectedAdded {
			t.Fatal("added wire length", len(araw), err)
		}
		rp := w.TaskBlockerResolvedPayload{BlockerID: base.BlockerID, BlockerType: kind, ResolutionComment: &maxText}
		rraw, err := rp.MarshalJSON()
		expectedResolved := 6246
		if kind == w.TaskBlockerWaitingForHuman {
			expectedResolved = 6256
		}
		if err != nil || len(rraw) != expectedResolved {
			t.Fatal("resolved maximum escaping", len(rraw), err)
		}
		decoded, err := w.DecodeTaskBlockerResolvedPayload(rraw)
		if err != nil || !reflect.DeepEqual(decoded, rp) {
			t.Fatal("maximum resolved not consumable", err)
		}
		for _, s := range []string{"", "\t\r\n\u1680\u202f", " e\u0301 / é ", "\u200b\ufeff", strings.Repeat("🚀", 255) + "🌐"} {
			base.Description = s
			if base.Validate() != nil {
				t.Fatal("legal description rejected")
			}
			wire := independentBlockerWire(string(kind), s, `{}`)
			if kind == w.TaskBlockerRelyOn {
				wire = independentBlockerWire(string(kind), s, `{"related_task_id":"`+independentRelatedUUID+`"}`)
			}
			value, err := w.DecodeTaskBlockerCreate(wire)
			if err != nil || value.Description != s {
				t.Fatal("text normalization/loss", err)
			}
		}
		for r := rune(0); r <= 0x9f; r++ {
			if r >= 32 && r < 127 || r == '\t' || r == '\n' || r == '\r' {
				continue
			}
			base.Description = "prefix" + string(r) + "suffix"
			fault := independentBlockerFault(t, base.Validate(), f.InvalidArgument)
			if !reflect.DeepEqual(fault.FieldErrors, []f.FieldError{{Path: "/description", Code: "INVALID_BLOCKER_DESCRIPTION"}}) {
				t.Fatal("bad text safe path")
			}
		}
		for _, s := range []string{maxText + "x", strings.Repeat("🚀", 256) + "x", string([]byte{0xc0, 0xaf})} {
			base.Description = s
			independentBlockerFault(t, base.Validate(), f.InvalidArgument)
			out, err := base.MarshalJSON()
			independentBlockerFault(t, err, f.InvalidArgument)
			if out != nil {
				t.Fatal("invalid text escaped to wire")
			}
		}
		for _, s := range []string{"", " \t\r\n", "\u1680\u2000\u202f\u3000", "x\u0085"} {
			rp.ResolutionComment = &s
			independentBlockerFault(t, rp.Validate(), f.InvalidArgument)
		}
		for _, s := range []string{"\u200b", "\ufeff", " a\u0301 "} {
			rp.ResolutionComment = &s
			if rp.Validate() != nil {
				t.Fatal("nonblank non-Cc comment rejected")
			}
		}
		rp.ResolutionComment = nil
		rraw, err = rp.MarshalJSON()
		if err != nil || !bytes.Contains(rraw, []byte(`"resolution_comment":null`)) {
			t.Fatal("required nullable comment omitted")
		}
	}
}

func TestIndependentBlockerValueSemantics(t *testing.T) {
	original := independentBlockerBase(t, w.TaskBlockerRelyOn)
	clone := original.Clone()
	clone.Metadata.RelyOn.RelatedTaskID = independentBlockerID[w.Task](t, independentBlockerUUID)
	if original.Metadata.RelyOn.RelatedTaskID.String() != independentRelatedUUID {
		t.Fatal("Create.Clone shares metadata")
	}
	comment := "independent-sensitive-comment"
	resolved := w.TaskBlockerResolvedPayload{BlockerID: original.BlockerID, BlockerType: original.Type, ResolutionComment: &comment}
	rcopy := resolved.Clone()
	*rcopy.ResolutionComment = "changed"
	if *resolved.ResolutionComment != comment {
		t.Fatal("resolved Clone shares comment")
	}
	wait := independentBlockerBase(t, w.TaskBlockerWaitingForHuman).Metadata
	if wait.Clone().WaitingForHuman == nil {
		t.Fatal("clone drops selected empty branch")
	}
	for _, raw := range []string{`{"blocker_id":"` + independentBlockerUUID + `","blocker_type":"technical","resolution_comment":" "}`, `{"blocker_id":"` + independentBlockerUUID + `","blocker_type":"technical","resolution_comment":false}`, `{"blocker_id":"bad","blocker_type":"technical","resolution_comment":null}`} {
		before := resolved.Clone()
		independentBlockerFault(t, resolved.UnmarshalJSON([]byte(raw)), f.InvalidArgument)
		if !reflect.DeepEqual(before, resolved) {
			t.Fatal("failed resolved changed receiver")
		}
		zero, err := w.DecodeTaskBlockerResolvedPayload([]byte(raw))
		independentBlockerFault(t, err, f.InvalidArgument)
		if !reflect.DeepEqual(zero, w.TaskBlockerResolvedPayload{}) {
			t.Fatal("partial resolved Decode result")
		}
	}
	for _, kind := range []string{"technical", "waiting_for_meeting_approval", "user_cancelled_execution"} {
		raw := []byte(`{"blocker_id":"` + independentBlockerUUID + `","blocker_type":"` + kind + `","resolution_comment":null}`)
		_, err := w.DecodeTaskBlockerResolvedPayload(raw)
		independentBlockerFault(t, err, f.DependencyUnbound)
		added := w.TaskBlockerAddedPayload{BlockerID: original.BlockerID, BlockerType: original.Type}
		before := added
		a := []byte(`{"blocker_id":"` + independentBlockerUUID + `","blocker_type":"` + kind + `"}`)
		independentBlockerFault(t, added.UnmarshalJSON(a), f.DependencyUnbound)
		if added != before {
			t.Fatal("unbound added changed receiver")
		}
		zero, err := w.DecodeTaskBlockerAddedPayload(a)
		independentBlockerFault(t, err, f.DependencyUnbound)
		if zero != (w.TaskBlockerAddedPayload{}) {
			t.Fatal("partial added Decode result")
		}
	}
	for _, receiver := range []interface{ UnmarshalJSON([]byte) error }{(*w.TaskBlockerType)(nil), (*w.TaskBlockerRelyOnMetadata)(nil), (*w.TaskBlockerWaitingForHumanMetadata)(nil), (*w.TaskBlockerCreate)(nil), (*w.TaskBlockerAddedPayload)(nil), (*w.TaskBlockerResolvedPayload)(nil)} {
		independentBlockerFault(t, receiver.UnmarshalJSON([]byte(`{}`)), f.InvalidArgument)
	}
	for n := 0; n < 4; n++ {
		t.Run(fmt.Sprintf("shared-%d", n), func(t *testing.T) {
			t.Parallel()
			for j := 0; j < 40; j++ {
				copied := original.Clone()
				copied.Metadata.RelyOn.RelatedTaskID = independentBlockerID[w.Task](t, independentBlockerUUID)
				if original.Validate() != nil || original.Metadata.RelyOn.RelatedTaskID.String() != independentRelatedUUID {
					t.Fatal("shared source changed")
				}
				raw, err := original.MarshalJSON()
				if err != nil {
					t.Fatal(err)
				}
				decoded, err := w.DecodeTaskBlockerCreate(raw)
				if err != nil || !reflect.DeepEqual(decoded, original) {
					t.Fatal("concurrent roundtrip mismatch", err)
				}
			}
		})
	}
}

func TestIndependentBlockerLegacyAndLog(t *testing.T) {
	v := independentBlockerBase(t, w.TaskBlockerRelyOn)
	comment := "independent-sensitive-comment"
	added := w.TaskBlockerAddedPayload{BlockerID: v.BlockerID, BlockerType: v.Type}
	resolved := w.TaskBlockerResolvedPayload{BlockerID: v.BlockerID, BlockerType: v.Type, ResolutionComment: &comment}
	waiting := w.TaskBlockerWaitingForHumanMetadata{}
	kind := w.TaskBlockerType("independent-sensitive-enum")
	values := []any{v, &v, v.Metadata, &v.Metadata, *v.Metadata.RelyOn, v.Metadata.RelyOn, waiting, &waiting, added, &added, resolved, &resolved, kind, &kind}
	for _, value := range values {
		for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%20.2v"} {
			if fmt.Sprintf(verb, value) != "work_task_transition" {
				t.Fatal("direct Formatter is not fixed marker")
			}
		}
		var log bytes.Buffer
		slog.New(slog.NewJSONHandler(&log, nil)).Info("probe", slog.Any("value", value))
		var record map[string]any
		if json.Unmarshal(log.Bytes(), &record) != nil || record["value"] != "work_task_transition" {
			t.Fatal("direct LogValuer projection")
		}
	}
	for _, wrapped := range []any{struct{ Value w.TaskBlockerCreate }{v}, []w.TaskBlockerCreate{v}, map[string]w.TaskBlockerCreate{"value": v}} {
		out := fmt.Sprintf("%+v", wrapped)
		if strings.Contains(out, "independent-sensitive") || !strings.Contains(out, "work_task_transition") {
			t.Fatal("ordinary fmt container bypass")
		}
	}
	// Business JSON intentionally retains the text. No arbitrary enclosing
	// reflection/slog JSON fallback confidentiality guarantee is asserted.
	raw, err := v.MarshalJSON()
	if err != nil || !bytes.Contains(raw, []byte(v.Description)) {
		t.Fatal("safe logging damaged business wire")
	}
	for _, typ := range []string{"blocker_added", "blocker_resolved", "rely_on", "waiting_for_human"} {
		independentBlockerFault(t, w.TaskEventType(typ).Validate(), f.InvalidArgument)
	}
	if w.TaskEventCreated.Validate() != nil || w.TaskEventFieldsUpdated.Validate() != nil {
		t.Fatal("legacy task event enum regressed")
	}
	for _, change := range []string{"blocker_added", "blocker_resolved"} {
		independentBlockerFault(t, w.TaskChange(change).Validate(), f.InvalidArgument)
	}
	for _, payload := range []json.Marshaler{added, resolved} {
		body, err := payload.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		for _, old := range []interface{ UnmarshalJSON([]byte) error }{new(w.TaskEvent), new(w.TaskChanged), new(w.TaskMutation)} {
			independentBlockerFault(t, old.UnmarshalJSON(body), f.InvalidArgument)
		}
	}
}
