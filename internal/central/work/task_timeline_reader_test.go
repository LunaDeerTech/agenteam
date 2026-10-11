package work

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func TestTaskTimelineInputDoesNotTouchStore(t *testing.T) {
	store, deps := pureBlockerPorts(t)
	reader, err := NewTaskReader(store, deps.Authority, deps.Structure, pureKeys(t))
	if err != nil {
		t.Fatal(err)
	}
	actor, project, task := pureActor(t, 2), pureID[i.Project](t, 3), pureID[c.Task](t, 4)
	for _, page := range []f.PageRequest{{Limit: 0}, {Limit: 201}, {Limit: -1}} {
		out, e := reader.ListTaskEvents(context.Background(), actor, project, task, c.TaskTimelineFilter{}, page)
		pureCode(t, e, f.InvalidArgument)
		if out.Items != nil || out.NextCursor != "" {
			t.Fatal("invalid input returned partial page")
		}
	}
	_, err = reader.ListTaskEvents(context.Background(), actor, project, task, c.TaskTimelineFilter{Types: []c.TaskTimelineEventType{}}, f.DefaultPageRequest())
	pureCode(t, err, f.InvalidArgument)
	_, err = reader.ListTaskEvents(context.Background(), actor, project, task, c.TaskTimelineFilter{}, f.PageRequest{Limit: 50, Cursor: strings.Repeat("x", cursor.MaxTokenBytes+1)})
	pureCode(t, err, f.InvalidArgument)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = reader.ListTaskEvents(ctx, actor, project, task, c.TaskTimelineFilter{}, f.DefaultPageRequest())
	if !errors.Is(err, context.Canceled) {
		t.Fatal("lost cancellation cause")
	}
	var missing *TaskReader
	_, err = missing.ListTaskEvents(context.Background(), actor, project, task, c.TaskTimelineFilter{}, f.DefaultPageRequest())
	pureCode(t, err, f.DependencyUnbound)
	if store.touches.Load() != 0 {
		t.Fatal("invalid input touched database")
	}
}

func TestTaskTimelineCursorBindingAndFixedWatermark(t *testing.T) {
	keys := pureKeys(t)
	project, task, owner := pureID[i.Project](t, 1), pureID[c.Task](t, 2), pureID[i.User](t, 3).String()
	binding, err := taskTimelineBinding(project, task, owner, c.TaskTimelineFilter{})
	if err != nil {
		t.Fatal(err)
	}
	at, _ := f.ParseInstant("2026-10-09T10:11:12.123456Z")
	event := c.TaskTimelineEvent{Planning: &c.TaskEvent{
		ID: pureID[c.TaskEvent](t, 4), ProjectID: project, TaskID: task, TaskVersion: 1, Type: c.TaskEventCreated,
		Actor: c.TaskEventActor{Type: i.Human, UserID: pureID[i.User](t, 3), Source: "task_domain"}, OperationID: pureID[c.TaskCommand](t, 5), CorrelationID: pureID[c.TaskCommand](t, 5),
		Payload: nil, CreatedAt: at}}
	event.Planning.Payload, err = json.Marshal(c.TaskCreatedPayload{InitialState: c.TaskStateBacklog, MilestoneID: pureID[c.Milestone](t, 6), SprintID: pureID[pc.Sprint](t, 7), Type: c.TaskTypeTask, Priority: c.TaskPriorityMedium})
	if err != nil {
		t.Fatal(err)
	}
	token, err := taskTimelineToken(keys, binding, 17, event)
	if err != nil {
		t.Fatal(err)
	}
	for _, current := range []f.Version{17, 18, 1000} {
		after, e := taskTimelineAfter(keys, token, binding, current)
		if e != nil || after == nil || after.watermark != 17 || after.at != at || after.id != event.Planning.ID.String() {
			t.Fatal("append changed original snapshot", e)
		}
	}
	_, err = taskTimelineAfter(keys, token, binding, 16)
	pureCode(t, err, f.CursorStale)
	if after, e := taskTimelineAfter(keys, "", binding, 17); e != nil || after != nil {
		t.Fatal("first page position")
	}
	for _, filter := range []c.TaskTimelineFilter{{Order: c.TaskTimelineDescending}, {Types: []c.TaskTimelineEventType{c.TaskTimelineBlockerResolved, c.TaskTimelineCreated, c.TaskTimelineBlockerAdded, c.TaskTimelineFieldsUpdated, c.TaskTimelineStateChanged, c.TaskTimelineAssigneeChanged, c.TaskTimelineComment}}} {
		same, e := taskTimelineBinding(project, task, owner, filter)
		if e != nil || !reflect.DeepEqual(same.Scope.Details(), binding.Scope.Details()) || same.QueryDigest != binding.QueryDigest || same.Order != binding.Order {
			t.Fatal("same set/default order changed binding", e)
		}
	}
	for _, changed := range []struct {
		p      c.ProjectID
		t      c.TaskID
		owner  string
		filter c.TaskTimelineFilter
	}{
		{pureID[i.Project](t, 7), task, owner, c.TaskTimelineFilter{}},
		{project, pureID[c.Task](t, 7), owner, c.TaskTimelineFilter{}},
		{project, task, pureID[i.User](t, 7).String(), c.TaskTimelineFilter{}},
		{project, task, owner, c.TaskTimelineFilter{Order: c.TaskTimelineAscending}},
		{project, task, owner, c.TaskTimelineFilter{Types: []c.TaskTimelineEventType{c.TaskTimelineCreated}}},
	} {
		next, e := taskTimelineBinding(changed.p, changed.t, changed.owner, changed.filter)
		if e != nil {
			t.Fatal(e)
		}
		_, e = taskTimelineAfter(keys, token, next, 17)
		pureCode(t, e, f.CursorInvalid)
	}
	instant, _ := cursor.Instant(at)
	id, _ := cursor.UUID(event.Planning.ID.String())
	text, _ := cursor.Text(at.String())
	gen := int64(17)
	for _, position := range []cursor.Position{
		{Scalars: []cursor.Scalar{instant, id}},
		{Scalars: []cursor.Scalar{instant, id, cursor.Integer(0)}},
		{Scalars: []cursor.Scalar{instant, id, cursor.Integer(-1)}},
		{Scalars: []cursor.Scalar{text, id, cursor.Integer(17)}},
		{Scalars: []cursor.Scalar{id, instant, cursor.Integer(17)}},
		{Scalars: []cursor.Scalar{instant, id, cursor.Integer(17), id}},
		{Scalars: []cursor.Scalar{instant, id, cursor.Integer(17)}, OrderGeneration: &gen},
	} {
		bad, e := keys.Sign(binding, position)
		if e != nil {
			t.Fatal(e)
		}
		_, e = taskTimelineAfter(keys, bad, binding, 16)
		pureCode(t, e, f.CursorInvalid)
	}
}

type timelineScanRow struct {
	values  []any
	failure error
}

func (row timelineScanRow) Scan(dest ...any) error {
	if row.failure != nil {
		return row.failure
	}
	for n, value := range row.values {
		v := reflect.ValueOf(dest[n]).Elem()
		if value == nil {
			v.SetZero()
		} else {
			v.Set(reflect.ValueOf(value))
		}
	}
	return nil
}
func TestTaskTimelineScanRejectsCorruptAndUnsupportedRows(t *testing.T) {
	project, task := pureID[i.Project](t, 1), pureID[c.Task](t, 2)
	operation := pureID[c.TaskCommand](t, 3).String()
	at := time.Date(2026, 10, 9, 10, 11, 12, 123456000, time.UTC)
	raw := func(value any) []byte {
		b, e := json.Marshal(value)
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	actor := raw(c.TaskEventActor{Type: i.Human, UserID: pureID[i.User](t, 4), Source: "task_domain"})
	payload := raw(c.TaskCreatedPayload{InitialState: c.TaskStateBacklog, MilestoneID: pureID[c.Milestone](t, 5), SprintID: pureID[pc.Sprint](t, 6), Type: c.TaskTypeTask, Priority: c.TaskPriorityMedium})
	base := []any{pureID[c.TaskEvent](t, 7).String(), project.String(), task.String(), int64(1), "task_created", actor, &operation, nil, nil, nil, nil, nil, operation, payload, at}
	v, e := scanTaskTimeline(timelineScanRow{values: base}, project, task, 20)
	if e != nil || v.Planning == nil || v.Blocker != nil {
		t.Fatal("valid planning", e)
	}
	for _, kind := range []string{"blocker_added", "blocker_resolved"} {
		values := append([]any(nil), base...)
		values[3] = int64(2)
		values[4] = kind
		values[6] = nil
		values[7] = &operation
		if kind == "blocker_added" {
			values[13] = raw(c.TaskBlockerAddedPayload{BlockerID: pureID[c.TaskBlockerIdentity](t, 8), BlockerType: c.TaskBlockerWaitingForHuman})
		} else {
			values[13] = raw(c.TaskBlockerResolvedPayload{BlockerID: pureID[c.TaskBlockerIdentity](t, 8), BlockerType: c.TaskBlockerWaitingForHuman})
		}
		got, err := scanTaskTimeline(timelineScanRow{values: values}, project, task, 20)
		if err != nil || got.Blocker == nil || got.Planning != nil {
			t.Fatal("valid blocker", err)
		}
	}
	for _, bad := range []struct {
		column int
		value  any
	}{
		{0, "invalid"}, {1, pureID[i.Project](t, 8).String()}, {2, pureID[c.Task](t, 8).String()},
		{3, int64(21)}, {3, int64(0)}, {4, "future_history"}, {6, nil}, {7, &operation}, {8, &operation}, {9, &operation}, {10, &operation}, {11, &operation}, {12, pureID[c.TaskCommand](t, 8).String()},
		{5, []byte(`{"type":"service"}`)}, {13, []byte(`{}`)}, {14, time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)},
	} {
		values := append([]any(nil), base...)
		values[bad.column] = bad.value
		got, err := scanTaskTimeline(timelineScanRow{values: values}, project, task, 20)
		pureCode(t, err, f.InternalError)
		if !reflect.DeepEqual(got, c.TaskTimelineEvent{}) {
			t.Fatal("corrupt row returned partial event")
		}
	}
	// Storage provenance, not overlapping type/actor wire, chooses the decoder.
	for _, source := range []struct {
		op      int
		kind    string
		payload any
		system  bool
	}{
		{8, "state_changed", c.TaskStateChangedPayload{FromState: c.TaskStateInProgress, ToState: c.TaskStateInReview}, false},
		{8, "assignee_changed", c.TaskAssigneeChangedPayload{ToAgentID: pureID[i.Agent](t, 12)}, false},
		{8, "comment", c.TaskCommentPayload{Body: "original review comment"}, false},
		{8, "blocker_resolved", c.TaskBlockerResolvedPayload{BlockerID: pureID[c.TaskBlockerIdentity](t, 8), BlockerType: c.TaskBlockerWaitingForHuman}, false},
		{8, "blocker_resolved", c.TaskBlockerResolvedPayload{BlockerID: pureID[c.TaskBlockerIdentity](t, 8), BlockerType: c.TaskBlockerTechnical}, false},
		{9, "state_changed", c.SchedulerStateChanged{FromState: c.TaskStateTodo, ToState: c.TaskStateInProgress, ReasonCode: "scheduler_claim"}, true},
		{10, "state_changed", c.TaskBusyStateChanged{FromState: c.TaskStateInProgress, ToState: c.TaskStateTodo, ReasonCode: "scheduler_agent_busy_compensation"}, true},
		{11, "state_changed", c.TaskFailureStateChanged{FromState: c.TaskStateInReview, ToState: c.TaskStateBlocked, ReasonCode: c.TaskLaunchFailureHistoryReason}, true},
		{11, "blocker_added", c.TaskFailureBlockerAdded{BlockerID: pureID[c.TaskBlockerIdentity](t, 8), BlockerType: c.TaskBlockerTechnical, ReasonCode: c.TaskLaunchFailureHistoryReason}, true},
	} {
		values := append([]any(nil), base...)
		values[3], values[4], values[6], values[source.op], values[13] = int64(2), source.kind, nil, &operation, raw(source.payload)
		if source.system {
			values[5] = raw(c.SchedulerTaskActor{CauseID: operation})
		}
		got, err := scanTaskTimeline(timelineScanRow{values: values}, project, task, 20)
		if err != nil {
			t.Fatal("real operation column rejected", source.op, source.kind, err)
		}
		header, err := got.Header()
		if err != nil || header.Type != c.TaskTimelineEventType(source.kind) || (source.op == 8 && got.Transition == nil) || (source.op == 9 && got.Claim == nil) || (source.op == 10 && got.Compensation == nil) || (source.op == 11 && got.Failure == nil) {
			t.Fatal("lost operation provenance", err)
		}
		for col := 6; col < 12; col++ {
			if col == source.op {
				continue
			}
			bad := append([]any(nil), values...)
			bad[col] = &operation
			value, err := scanTaskTimeline(timelineScanRow{values: bad}, project, task, 20)
			pureCode(t, err, f.InternalError)
			if !reflect.DeepEqual(value, c.TaskTimelineEvent{}) {
				t.Fatal("mixed operation returned data")
			}
		}
		if source.system {
			values[5] = actor
			_, err = scanTaskTimeline(timelineScanRow{values: values}, project, task, 20)
			pureCode(t, err, f.InternalError)
		}
	}
	sentinel := errors.New("test driver failure")
	_, e = scanTaskTimeline(timelineScanRow{failure: sentinel}, project, task, 20)
	if !errors.Is(e, sentinel) {
		t.Fatal("driver error hidden")
	}
}

func TestTaskTimelineByteBudgetKeepsCompleteComments(t *testing.T) {
	at, _ := f.ParseInstant("2026-10-09T10:11:12.123456Z")
	op := pureID[c.TaskTransitionCommand](t, 5)
	event := c.TaskTimelineEvent{Transition: &c.TaskTransitionEvent{ID: pureID[c.TaskEvent](t, 1), ProjectID: pureID[i.Project](t, 2), TaskID: pureID[c.Task](t, 3), TaskVersion: 2, Type: c.TaskTransitionComment, Actor: c.TaskTransitionActor{UserID: pureID[i.User](t, 4)}, OperationID: op, CorrelationID: op, CreatedAt: at, Payload: c.TaskTransitionFactPayload{Comment: &c.TaskCommentPayload{Body: strings.Repeat("<", c.MaxTaskTransitionCommentBytes)}}}}
	raw, err := event.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) <= c.MaxTaskEventBytes {
		t.Fatal("large escaped comment premise")
	}
	size := 2
	items := []c.TaskTimelineEvent{}
	for n := 0; n < 200; n++ {
		event.Transition.ID = pureID[c.TaskEvent](t, 100+n)
		if !taskTimelineAppend(&items, &size, event, len(raw)) {
			break
		}
	}
	if len(items) == 0 || len(items) >= 200 {
		t.Fatal("byte budget did not form a nonempty short page")
	}
	encoded, err := json.Marshal(items)
	if err != nil || len(encoded) != size || size > c.MaxTaskTimelineItemsBytes {
		t.Fatal("items budget differs from actual encoding", err)
	}
	last := items[len(items)-1].Clone()
	before := size
	if taskTimelineAppend(&items, &size, event, len(raw)) || size != before {
		t.Fatal("overflow consumed or cut an event")
	}
	keys := pureKeys(t)
	binding, err := taskTimelineBinding(event.Transition.ProjectID, event.Transition.TaskID, event.Transition.Actor.UserID.String(), c.TaskTimelineFilter{Order: c.TaskTimelineAscending})
	if err != nil {
		t.Fatal(err)
	}
	token, err := taskTimelineToken(keys, binding, 2, last)
	if err != nil {
		t.Fatal(err)
	}
	position, err := taskTimelineAfter(keys, token, binding, 3)
	if err != nil || position.id != last.Transition.ID.String() || position.watermark != 2 {
		t.Fatal("short page cursor advanced past returned data", err)
	}
	whole, err := json.Marshal(f.Page[c.TaskTimelineEvent]{Items: items, NextCursor: token})
	if err != nil || len(whole) > c.MaxTaskTimelineResponseBytes {
		t.Fatal("wire response exceeded total budget", err)
	}
	event.Transition.Payload.Comment.Body = "mutated caller"
	for _, v := range items {
		if v.Transition.Payload.Comment.Body != strings.Repeat("<", c.MaxTaskTransitionCommentBytes) {
			t.Fatal("comment truncated or borrowed")
		}
	}
	a, _ := last.Header()
	b := a
	b.ID = pureID[c.TaskEvent](t, 1000)
	if !taskTimelineFollows(a, b, c.TaskTimelineAscending) || !taskTimelineFollows(b, a, c.TaskTimelineDescending) || taskTimelineFollows(a, a, c.TaskTimelineAscending) {
		t.Fatal("same timestamp seek order")
	}
}
