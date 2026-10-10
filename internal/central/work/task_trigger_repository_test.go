package work

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type taskTriggerRow []any

func (r taskTriggerRow) Scan(out ...any) error {
	if len(out) != len(r) {
		return errors.New("test column mismatch")
	}
	for n, dst := range out {
		value := reflect.ValueOf(dst)
		if value.Kind() != reflect.Pointer || value.IsNil() {
			return errors.New("test destination")
		}
		value = value.Elem()
		if r[n] == nil {
			value.SetZero()
			continue
		}
		incoming := reflect.ValueOf(r[n])
		if !incoming.Type().AssignableTo(value.Type()) {
			return errors.New("test column type")
		}
		value.Set(incoming)
	}
	return nil
}
func taskTriggerEventRow(t *testing.T, raw []byte, blocker bool) taskTriggerRow {
	t.Helper()
	var e c.TaskEvent
	var b c.TaskBlockerEvent
	if blocker {
		if err := b.UnmarshalJSON(raw); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := e.UnmarshalJSON(raw); err != nil {
			t.Fatal(err)
		}
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if blocker {
		operation := b.OperationID.String()
		return taskTriggerRow{b.ID.String(), b.ProjectID.String(), b.TaskID.String(), b.TaskVersion, string(b.Type), []byte(fields["actor"]), (*string)(nil), &operation, b.CorrelationID.String(), []byte(fields["payload"]), b.CreatedAt.Time()}
	}
	operation := e.OperationID.String()
	return taskTriggerRow{e.ID.String(), e.ProjectID.String(), e.TaskID.String(), e.TaskVersion, string(e.Type), []byte(fields["actor"]), &operation, (*string)(nil), e.CorrelationID.String(), []byte(fields["payload"]), e.CreatedAt.Time()}
}
func TestTaskTriggerEventStorageArms(t *testing.T) {
	task, _, _ := pureTaskRecord(t)
	blocker, _, _ := pureBlockerRecord(t, false)
	blockRaw, err := json.Marshal(blocker.Plan.TaskEvent)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		raw     []byte
		blocker bool
	}{{task.Plan.TaskEvent, false}, {blockRaw, true}} {
		row := taskTriggerEventRow(t, item.raw, item.blocker)
		raw, err := scanTaskTriggerEvent(row)
		if err != nil {
			t.Fatal("formal stored arm rejected", err)
		}
		identity, err := decodeTaskTriggerEvent(raw)
		if err != nil || identity.task != task.Input.Target || identity.project != task.Project {
			t.Fatal("event scope", err)
		}
		bad := append(taskTriggerRow{}, row...)
		other := pureID[c.TaskCommand](t, 99).String()
		bad[6], bad[7] = &other, &other
		if _, err = scanTaskTriggerEvent(bad); err == nil {
			t.Fatal("dual operation arms")
		}
		bad = append(taskTriggerRow{}, row...)
		bad[6], bad[7] = (*string)(nil), (*string)(nil)
		if _, err = scanTaskTriggerEvent(bad); err == nil {
			t.Fatal("missing operation arm")
		}
		bad = append(taskTriggerRow{}, row...)
		bad[4] = "future_event"
		if _, err = scanTaskTriggerEvent(bad); err == nil {
			t.Fatal("unknown event silently skipped")
		}
	}
}
func TestTaskTriggerRepositoryFailuresDoNotReturnPartialMaterial(t *testing.T) {
	store := &taskTriggerTestStore{tx: f.NewTx()}
	w := taskTriggerFixtureInput(t)
	if result, err := loadTaskTriggerBlockers(context.Background(), store, w.Task.ProjectID, w.Task.ID); err == nil || result != nil {
		t.Fatal("blocker read failure became empty success")
	}
	if result, err := loadTaskTriggerEvents(context.Background(), store, w.Task.ProjectID, w.Task.ID); err == nil || result != nil {
		t.Fatal("history read failure became empty success")
	}
	input, err := loadTaskTriggerInput(context.Background(), store, taskTriggerTestProjectRef(t), w.Task.ID, "task/work")
	if err == nil || input.Validate() == nil || store.queries != 3 {
		t.Fatal("Task error yielded partial input")
	}
	raw, err := scanTaskTriggerEvent(denialRow{})
	if err == nil || raw != nil {
		t.Fatal("row error yielded history")
	}
	// The input decoder independently rejects foreign child scope and a future
	// event version, even if the outer object's project and Task are correct.
	b, _, _ := pureBlockerRecord(t, false)
	w.Task = b.Plan.After.Task.Clone()
	w.UnresolvedBlockers = []c.TaskBlocker{b.Plan.After.Blocker.Clone()}
	w.UnresolvedBlockers[0].ProjectID = pureID[i.Project](t, 99)
	if _, err = newTaskTriggerInput(w); err == nil {
		t.Fatal("foreign child accepted")
	}
	w = taskTriggerFixtureInput(t)
	event := b.Plan.TaskEvent.Clone()
	later, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	w.RecentTaskEvents = append(w.RecentTaskEvents, later)
	if _, err = newTaskTriggerInput(w); err == nil {
		t.Fatal("history beyond captured Task version")
	}
}
