package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func TestTaskTechnicalBlockerReadDoesNotAuthorizeHumanCreate(t *testing.T) {
	v := blockerRecordFixture(t, false)
	id := testID[SchedulerClaim](t, 219).String()
	v.Type, v.Description = TaskBlockerTechnical, "Scheduler launch failed."
	v.Metadata, v.CreatedBy = TaskBlockerMetadata{}, TaskEventActor{}
	v.Technical = &TaskBlockerTechnicalMetadata{"scheduler_launch_failed", "scheduler_dispatch", id}
	v.SchedulerCreatedBy = &SchedulerTaskActor{CauseID: id}
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeTaskBlocker(raw)
	if err != nil || !reflect.DeepEqual(got, v) {
		t.Fatal("technical read roundtrip", err)
	}
	for _, mutate := range []func(*TaskBlocker){
		func(x *TaskBlocker) { x.Technical.ReferenceID = testID[SchedulerClaim](t, 220).String() },
		func(x *TaskBlocker) { x.Technical.Code = "other" },
		func(x *TaskBlocker) { x.Metadata.WaitingForHuman = &TaskBlockerWaitingForHumanMetadata{} },
		func(x *TaskBlocker) { x.CreatedBy = blockerRecordFixture(t, false).CreatedBy },
		func(x *TaskBlocker) { x.ResolvedAt = &x.CreatedAt },
	} {
		x := v.Clone()
		mutate(&x)
		if x.Validate() == nil {
			t.Fatal("mixed or unsupported technical arm")
		}
		if !reflect.DeepEqual(v, got) {
			t.Fatal("clone alias")
		}
	}
	for _, bad := range [][]byte{
		bytes.Replace(raw, []byte(`"code":"scheduler_launch_failed"`), []byte(`"code":"scheduler_launch_failed","extra":true`), 1),
		bytes.Replace(raw, []byte(`"service_name":"scheduler"`), []byte(`"service_name":"other"`), 1),
	} {
		if _, err = DecodeTaskBlocker(bad); err == nil {
			t.Fatal("open technical codec")
		}
	}
	request := []byte(fmt.Sprintf(`{"blocker_id":%q,"type":"technical","description":"x","metadata":{"code":"scheduler_launch_failed","source":"scheduler_dispatch","reference_id":%q}}`, v.ID.String(), id))
	if _, err = DecodeTaskBlockerCreate(request); err == nil {
		t.Fatal("read arm became Human create")
	}
	if bytes.Contains([]byte(fmt.Sprintf("%+v", v)), []byte(id)) {
		t.Fatal("unsafe default formatting")
	}
}
