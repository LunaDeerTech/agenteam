package contract

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func blockerMutationFixture(t *testing.T) TaskBlockerMutation {
	v := blockerRecordFixture(t, true)
	task := taskFixture(t)
	task.Version = 2
	task.UpdatedAt = *v.ResolvedAt
	return TaskBlockerMutation{Task: task, Blocker: v, TaskEventID: testID[TaskEvent](t, 21), EventIDs: []event.EventID{testID[event.EventIdentity](t, 22)}}
}
func TestTaskBlockerCommandDigestIdentityAndLookup(t *testing.T) {
	actor := testActor(t, 1, 2)
	task := taskFixture(t)
	req := blockerFixture(t, TaskBlockerRelyOn)
	expected := f.Version(1)
	meta := f.CommandMeta{RequestID: testID[f.Request](t, 50), IdempotencyKey: "blocker/key", ExpectedVersion: &expected}
	got, e := TaskBlockerAddDigest(actor, meta, task.ProjectID, task.ID, req)
	if e != nil {
		t.Fatal(e)
	}
	// Independently specified canonical golden bytes, with no production digest helper.
	raw := `{"actor_user_id":"` + actor.Details().UserID + `","command":"work.task.blocker.add","expected_version":"1","format":"work-task-blocker-v1","project_id":"` + task.ProjectID.String() + `","request":{"blocker_id":"` + req.BlockerID.String() + `","description":"golden","metadata":{"related_task_id":"` + req.Metadata.RelyOn.RelatedTaskID.String() + `"},"type":"rely_on"},"task_id":"` + task.ID.String() + `"}`
	golden := req.Clone()
	golden.Description = "golden"
	digest, e := TaskBlockerAddDigest(actor, meta, task.ProjectID, task.ID, golden)
	sum := sha256.Sum256([]byte(raw))
	if e != nil || string(digest) != "sha256:"+hex.EncodeToString(sum[:]) {
		t.Fatal("digest golden")
	}
	other := meta
	other.RequestID = testID[f.Request](t, 51)
	other.IdempotencyKey = "another"
	same, e := TaskBlockerAddDigest(actor, other, task.ProjectID, task.ID, req)
	if e != nil || same != got {
		t.Fatal("nonsemantic identity changed digest")
	}
	expected = 2
	different, e := TaskBlockerAddDigest(actor, meta, task.ProjectID, task.ID, req)
	if e != nil || different == got {
		t.Fatal("version omitted")
	}
	expected = 1
	for _, modify := range []func(*TaskBlockerCreate){func(v *TaskBlockerCreate) { v.Description += " " }, func(v *TaskBlockerCreate) { v.BlockerID = testID[TaskBlockerIdentity](t, 210) }, func(v *TaskBlockerCreate) { v.Metadata.RelyOn.RelatedTaskID = testID[Task](t, 211) }} {
		v := req.Clone()
		modify(&v)
		d, e := TaskBlockerAddDigest(actor, meta, task.ProjectID, task.ID, v)
		if e != nil || d == got {
			t.Fatal("request semantics omitted")
		}
	}
	id, e := TaskBlockerCommandIdentity(task.ProjectID, TaskBlockerCommandAdd, meta.IdempotencyKey)
	if e != nil || id.Command() != string(TaskBlockerCommandAdd) || id.OwnerIDs()[0] != task.ProjectID.String() {
		t.Fatal("identity")
	}
	meta.ExpectedVersion = nil
	if _, e = TaskBlockerAddDigest(actor, meta, task.ProjectID, task.ID, req); e == nil {
		t.Fatal("missing expected")
	}
	meta.ExpectedVersion = &expected
	if _, e = TaskBlockerAddDigest(i.Actor{}, meta, task.ProjectID, task.ID, req); e == nil {
		t.Fatal("anonymous")
	}
	q := TaskBlockerCommandLookupRequest{ProjectID: task.ProjectID, Command: TaskBlockerCommandAdd, IdempotencyKey: meta.IdempotencyKey, SemanticDigest: got}
	if decoded, e := DecodeTaskBlockerCommandLookupRequest(taskRaw(t, q)); e != nil || decoded != q {
		t.Fatal("lookup request", e)
	}
	for _, status := range []LookupState{LookupCommitted, LookupInProgress, LookupNotObserved} {
		v := TaskBlockerCommandLookup{Status: status}
		if status == LookupCommitted {
			x := blockerMutationFixture(t)
			v.Receipt = &x
		}
		decoded, e := DecodeTaskBlockerCommandLookup(taskRaw(t, v))
		if e != nil || !reflect.DeepEqual(v, decoded) {
			t.Fatal("lookup result", e)
		}
	}
}
func TestTaskBlockerMutationStrictBoundsAndClone(t *testing.T) {
	v := blockerMutationFixture(t)
	v.Task.Description = strings.Repeat("<", 32768)
	v.Task.Plan = strings.Repeat(">", 32768)
	v.Blocker.Description = strings.Repeat("&", 1024)
	comment := strings.Repeat("<", 1024)
	v.Blocker.ResolutionComment = &comment
	raw := taskRaw(t, v)
	if len(raw) > MaxTaskBlockerMutationBytes {
		t.Fatal("receipt cap")
	}
	got, e := DecodeTaskBlockerMutation(raw)
	if e != nil || !reflect.DeepEqual(v, got) {
		t.Fatal("receipt roundtrip", e)
	}
	for _, bad := range [][]byte{taskJSONChange(t, raw, "event_ids", []byte(`null`)), taskJSONChange(t, raw, "event_ids", []byte(`[]`)), taskJSONChange(t, raw, "task_event_id", []byte(`null`)), bytes.Replace(raw, []byte(`"metadata":`), []byte(`"METADATA":`), 1), bytes.Replace(raw, []byte(`"title":`), []byte(`"TITLE":`), 1)} {
		next := v.Clone()
		if next.UnmarshalJSON(bad) == nil || !reflect.DeepEqual(next, v) {
			t.Fatal("strict receipt/atomicity")
		}
	}
	clone := v.Clone()
	clone.EventIDs[0] = testID[event.EventIdentity](t, 99)
	*clone.Blocker.ResolutionComment = "changed"
	if v.EventIDs[0] == clone.EventIDs[0] || *v.Blocker.ResolutionComment == "changed" {
		t.Fatal("receipt clone")
	}
	q := TaskBlockerCommandLookup{Status: LookupCommitted, Receipt: &v}
	lookup := q.Clone()
	*lookup.Receipt.Blocker.ResolutionComment = "changed"
	if *q.Receipt.Blocker.ResolutionComment == "changed" {
		t.Fatal("lookup clone")
	}
	for _, decode := range []func([]byte) error{(*TaskBlockerMutation)(nil).UnmarshalJSON, (*TaskBlockerCommandLookupRequest)(nil).UnmarshalJSON, (*TaskBlockerCommandLookup)(nil).UnmarshalJSON} {
		if decode([]byte(`{}`)) == nil {
			t.Fatal("nil receiver")
		}
	}
	var keys map[string]json.RawMessage
	_ = json.Unmarshal(raw, &keys)
	if len(keys) != 4 {
		t.Fatal("receipt closure")
	}
}
