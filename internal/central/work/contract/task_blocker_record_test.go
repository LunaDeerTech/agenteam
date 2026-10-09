package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func blockerRecordFixture(t *testing.T, resolved bool) TaskBlocker {
	task := taskFixture(t)
	create := blockerFixture(t, TaskBlockerRelyOn)
	v := TaskBlocker{ID: create.BlockerID, ProjectID: task.ProjectID, TaskID: task.ID, Type: create.Type, Description: create.Description, Metadata: create.Metadata.Clone(), CreatedAt: task.CreatedAt, CreatedBy: TaskEventActor{Type: i.Human, UserID: testID[i.User](t, 1), Source: "task_domain"}}
	if resolved {
		at, _ := f.NewInstant(task.CreatedAt.Time().Add(time.Second))
		actor := v.CreatedBy
		comment := " resolved <sensitive> 界 "
		v.ResolvedAt = &at
		v.ResolvedBy = &actor
		v.ResolutionComment = &comment
	}
	return v
}
func TestTaskBlockerRecordStrictCodec(t *testing.T) {
	for _, resolved := range []bool{false, true} {
		v := blockerRecordFixture(t, resolved)
		raw := taskRaw(t, v)
		got, e := DecodeTaskBlocker(raw)
		if e != nil || !reflect.DeepEqual(v, got) {
			t.Fatal("record round trip", e)
		}
		for _, key := range strings.Fields("id project_id task_id type description metadata created_at created_by resolved_at resolved_by resolution_comment") {
			for _, bad := range [][]byte{taskJSONChange(t, raw, key, nil), bytes.Replace(raw, []byte(`"`+key+`":`), []byte(`"`+strings.ToUpper(key)+`":`), 1)} {
				next := v.Clone()
				if next.UnmarshalJSON(bad) == nil || !reflect.DeepEqual(next, v) {
					t.Fatalf("missing/alias accepted or receiver mutated %s", key)
				}
			}
		}
		for _, bad := range [][]byte{append(append([]byte{}, raw...), []byte(` {}`)...), bytes.Replace(raw, []byte(`"description":`), []byte(`"description":"x","description":`), 1), taskJSONChange(t, raw, "created_by", []byte(`{"type":"human","user_id":"01900000-0000-7000-8000-000000000001","SOURCE":"task_domain"}`)), taskJSONChange(t, raw, "metadata", []byte(`{"related_task_id":"01900000-0000-7000-8000-000000000202","extra":1}`)), taskJSONChange(t, raw, "description", []byte(`"\ud800"`)), taskJSONChange(t, raw, "description", []byte{34, 255, 34})} {
			if _, e = DecodeTaskBlocker(bad); e == nil {
				t.Fatal("bad record accepted")
			}
		}
	}
	for _, modify := range []func(*TaskBlocker){func(v *TaskBlocker) { v.ResolvedBy = nil }, func(v *TaskBlocker) { v.ResolvedAt = nil }, func(v *TaskBlocker) { x := " \t\r\n"; v.ResolutionComment = &x }, func(v *TaskBlocker) { at, _ := f.NewInstant(v.CreatedAt.Time().Add(-time.Second)); v.ResolvedAt = &at }, func(v *TaskBlocker) { v.Type = TaskBlockerTechnical }, func(v *TaskBlocker) { v.Metadata.WaitingForHuman = &TaskBlockerWaitingForHumanMetadata{} }} {
		v := blockerRecordFixture(t, true)
		modify(&v)
		if v.Validate() == nil {
			t.Fatal("bad resolution/union accepted")
		}
	}
	if (*TaskBlocker)(nil).UnmarshalJSON([]byte(`{}`)) == nil || (*TaskBlockerResolve)(nil).UnmarshalJSON([]byte(`{}`)) == nil {
		t.Fatal("nil receiver accepted")
	}
	for _, status := range []TaskBlockerStatus{TaskBlockersAll, TaskBlockersResolved, TaskBlockersUnresolved} {
		var got TaskBlockerStatus
		if got.UnmarshalJSON(taskRaw(t, status)) != nil || got != status {
			t.Fatal("status")
		}
	}
	for _, status := range []TaskBlockerStatus{"", "ALL", "waiting"} {
		if status.Validate() == nil {
			t.Fatal("status admitted")
		}
	}
}
func TestTaskBlockerRecordBoundsCloneAndLogs(t *testing.T) {
	v := blockerRecordFixture(t, true)
	v.Description = strings.Repeat("<", 1024)
	comment := strings.Repeat(">", 1024)
	v.ResolutionComment = &comment
	raw := taskRaw(t, v)
	if len(raw) > MaxTaskBlockerRecordBytes {
		t.Fatal("escaping overflow")
	}
	if _, e := DecodeTaskBlocker(raw); e != nil {
		t.Fatal(e)
	}
	nested := bytes.Replace(raw, []byte(`"metadata":{"related_task_id":`), []byte(`"metadata":{`+strings.Repeat(" ", MaxTaskBlockerMetadataBytes)+`"related_task_id":`), 1)
	if _, e := DecodeTaskBlocker(nested); e == nil {
		t.Fatal("nested raw cap")
	}
	if _, e := DecodeTaskBlocker(append(raw, bytes.Repeat([]byte(" "), MaxTaskBlockerRecordBytes)...)); e == nil {
		t.Fatal("complete raw cap")
	}
	clone := v.Clone()
	clone.Metadata.RelyOn.RelatedTaskID = testID[Task](t, 99)
	*clone.ResolutionComment = "changed"
	clone.ResolvedBy.UserID = testID[i.User](t, 99)
	*clone.ResolvedAt = v.CreatedAt
	if v.Metadata.RelyOn.RelatedTaskID == clone.Metadata.RelyOn.RelatedTaskID || *v.ResolutionComment == "changed" || v.ResolvedBy.UserID == clone.ResolvedBy.UserID || v.ResolvedAt.Time().Equal(v.CreatedAt.Time()) {
		t.Fatal("clone alias")
	}
	r := TaskBlockerResolve{BlockerID: v.ID, ResolutionComment: &comment}
	rr := taskRaw(t, r)
	if _, e := DecodeTaskBlockerResolve(rr); e != nil {
		t.Fatal(e)
	}
	for _, bad := range [][]byte{taskJSONChange(t, rr, "resolution_comment", nil), taskJSONChange(t, rr, "resolution_comment", []byte(`""`)), taskJSONChange(t, rr, "resolution_comment", taskRaw(t, strings.Repeat("x", 1025)))} {
		if _, e := DecodeTaskBlockerResolve(bad); e == nil {
			t.Fatal("bad comment accepted")
		}
	}
	for _, value := range []any{v, r} {
		for _, format := range []string{"%v", "%+v", "%#v"} {
			if got := fmt.Sprintf(format, value); got != "work_task_blocker" {
				t.Fatal("unsafe fmt")
			}
		}
		var b bytes.Buffer
		slog.New(slog.NewJSONHandler(&b, nil)).Info("safe", "value", value)
		if !strings.Contains(b.String(), "work_task_blocker") || strings.Contains(b.String(), comment) {
			t.Fatal("unsafe slog")
		}
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 11 {
		t.Fatal("record key closure")
	}
}
