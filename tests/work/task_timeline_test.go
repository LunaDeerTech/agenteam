//go:build integration

package work_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// One comparable Store pointer is shared by every real dependency. The sole
// override is the result of the original successful, no-argument clock query;
// transactions, locks, SQL, Rows and commit/rollback remain the actual Store.
type timelineClockStore struct {
	fixtureStore
	mu      sync.Mutex
	at      time.Time
	samples int
}
type timelineClockExecutor struct {
	postgres.SQLExecutor
	owner *timelineClockStore
}
type timelineClockRow struct {
	postgres.Row
	owner *timelineClockStore
}

func (s *timelineClockStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	x, err := s.fixtureStore.InTx(tx)
	if err != nil {
		return nil, err
	}
	return &timelineClockExecutor{SQLExecutor: x, owner: s}, nil
}
func (x *timelineClockExecutor) QueryRow(ctx context.Context, sql string, args ...any) postgres.Row {
	row := x.SQLExecutor.QueryRow(ctx, sql, args...)
	if sql == "SELECT clock_timestamp()" && len(args) == 0 {
		return &timelineClockRow{Row: row, owner: x.owner}
	}
	return row
}
func (r *timelineClockRow) Scan(dest ...any) error {
	if err := r.Row.Scan(dest...); err != nil {
		return err
	}
	r.owner.mu.Lock()
	defer r.owner.mu.Unlock()
	if r.owner.at.IsZero() {
		return nil
	}
	if len(dest) != 1 {
		return fmt.Errorf("TIMELINE_CLOCK_SHAPE")
	}
	value, ok := dest[0].(*time.Time)
	if !ok {
		return fmt.Errorf("TIMELINE_CLOCK_TYPE")
	}
	*value = r.owner.at
	r.owner.samples++
	return nil
}
func (s *timelineClockStore) hold(at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.at = at.UTC().Truncate(time.Microsecond)
}
func timelineOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal("timeline operation failed", err)
	}
}
func timelineJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	timelineOK(t, err)
	var normalized any
	timelineOK(t, json.Unmarshal(raw, &normalized))
	raw, err = json.Marshal(normalized)
	timelineOK(t, err)
	return string(raw)
}
func timelineID(v c.TaskTimelineEvent) string {
	if v.Planning != nil {
		return v.Planning.ID.String()
	}
	return v.Blocker.ID.String()
}
func timelineType(v c.TaskTimelineEvent) c.TaskTimelineEventType {
	if v.Planning != nil {
		return c.TaskTimelineEventType(v.Planning.Type)
	}
	return c.TaskTimelineEventType(v.Blocker.Type)
}
func timelineAt(v c.TaskTimelineEvent) time.Time {
	if v.Planning != nil {
		return v.Planning.CreatedAt.Time()
	}
	return v.Blocker.CreatedAt.Time()
}

// Read independent persisted wire through the existing strict family decoders,
// never by calling the new Reader. Compare each byte-preserving typed record.
func timelineStored(t *testing.T, b *blockerFixture, project pc.ProjectID, task c.TaskID) []c.TaskTimelineEvent {
	t.Helper()
	rows, err := b.raw.Query(ctxFor(t), `SELECT type,jsonb_build_object('id',id,'project_id',project_id,'task_id',task_id,'task_version',task_version::text,'type',type,'actor',actor,'operation_id',coalesce(operation_id,blocker_operation_id),'correlation_id',correlation_id,'payload',payload,'created_at',to_char(created_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"')) FROM agenteam_work.task_events WHERE project_id=$1 AND task_id=$2`, project.String(), task.String())
	timelineOK(t, err)
	defer rows.Close()
	values := []c.TaskTimelineEvent{}
	for rows.Next() {
		var kind string
		var raw []byte
		timelineOK(t, rows.Scan(&kind, &raw))
		var value c.TaskTimelineEvent
		switch kind {
		case "task_created", "fields_updated":
			var event c.TaskEvent
			timelineOK(t, event.UnmarshalJSON(raw))
			value.Planning = &event
		case "blocker_added", "blocker_resolved":
			var event c.TaskBlockerEvent
			timelineOK(t, event.UnmarshalJSON(raw))
			value.Blocker = &event
		default:
			t.Fatal("unexpected positive timeline family")
		}
		values = append(values, value)
	}
	timelineOK(t, rows.Err())
	rows.Close()
	slices.SortFunc(values, func(a, b c.TaskTimelineEvent) int {
		if n := timelineAt(a).Compare(timelineAt(b)); n != 0 {
			return n
		}
		if timelineID(a) < timelineID(b) {
			return -1
		}
		if timelineID(a) > timelineID(b) {
			return 1
		}
		return 0
	})
	return values
}
func timelineWant(values []c.TaskTimelineEvent, filter c.TaskTimelineFilter) []c.TaskTimelineEvent {
	out := []c.TaskTimelineEvent{}
	for _, v := range values {
		if filter.Types == nil || slices.Contains(filter.Types, timelineType(v)) {
			out = append(out, v.Clone())
		}
	}
	if filter.Order != c.TaskTimelineAscending {
		slices.Reverse(out)
	}
	return out
}
func timelineEqual(t *testing.T, got, want []c.TaskTimelineEvent) {
	t.Helper()
	if timelineJSON(t, got) != timelineJSON(t, want) {
		t.Fatalf("timeline page contents/order differ: got=%d want=%d", len(got), len(want))
	}
}
func timelinePage(t *testing.T, reader *work.TaskReader, actor i.Actor, project pc.ProjectID, task c.TaskID, filter c.TaskTimelineFilter, page f.PageRequest) f.Page[c.TaskTimelineEvent] {
	t.Helper()
	out, err := reader.ListTaskEvents(ctxFor(t), actor, project, task, filter, page)
	timelineOK(t, err)
	if out.Items == nil || len(out.Items) > page.Limit {
		t.Fatal("invalid successful timeline page")
	}
	return out
}
func timelineRest(t *testing.T, reader *work.TaskReader, actor i.Actor, project pc.ProjectID, task c.TaskID, filter c.TaskTimelineFilter, first f.Page[c.TaskTimelineEvent], limit int) []c.TaskTimelineEvent {
	t.Helper()
	out := append([]c.TaskTimelineEvent{}, first.Items...)
	cursor := first.NextCursor
	seen := map[string]bool{}
	for cursor != "" {
		if seen[cursor] || len(seen) > 300 {
			t.Fatal("timeline cursor cycle")
		}
		seen[cursor] = true
		page := timelinePage(t, reader, actor, project, task, filter, f.PageRequest{Cursor: cursor, Limit: limit})
		out = append(out, page.Items...)
		cursor = page.NextCursor
	}
	return out
}
func timelineReceipt(t *testing.T, b *blockerFixture, actor i.Actor, project pc.ProjectID, key f.IdempotencyKey, name string, event c.TaskEventID, result any, blocker bool) {
	t.Helper()
	table := "task_commands"
	if blocker {
		table = "task_blocker_commands"
	}
	var state, user, command, eventID, operation, correlation string
	var receipt []byte
	err := b.raw.QueryRow(ctxFor(t), "SELECT c.state,c.actor_user_id::text,c.command_name,c.task_event_id::text,c.receipt,c.id::text,e.correlation_id::text FROM agenteam_work."+table+" c JOIN agenteam_work.task_events e ON e.project_id=c.project_id AND e.id=c.task_event_id WHERE c.project_id=$1 AND c.idempotency_key=$2", project.String(), string(key)).Scan(&state, &user, &command, &eventID, &receipt, &operation, &correlation)
	timelineOK(t, err)
	var value any
	timelineOK(t, json.Unmarshal(receipt, &value))
	if state != "completed" || user != actor.Details().UserID || command != name || eventID != event.String() || operation != correlation || timelineJSON(t, value) != timelineJSON(t, result) {
		t.Fatal("original timeline command receipt/history binding differs")
	}
}

func TestTaskTimelinePersistenceAndPaging(t *testing.T) {
	db := newDatabase(t)
	raw := openStore(t, db.Config(t, nil))
	clock := &timelineClockStore{fixtureStore: raw}
	b := attachBlockers(t, assembleTask(t, db, raw, clock, true))
	actor := b.human(t, "timeline-owner", "user")
	project, _, _ := b.create(t, actor, "timeline")
	milestone := b.milestone(t, actor, project.ID, "milestone")
	sprint := b.sprint(t, actor, project.ID, milestone.ID, "sprint")
	createMeta := meta(t, "timeline-create", nil)
	createRequest := c.TaskCreate{TaskID: id[c.Task](t), SprintID: sprint.ID, Title: "initial", Type: c.TaskTypeTask, Priority: c.TaskPriorityMedium}
	created, err := b.tasks.CreateTask(ctxFor(t), actor, createMeta, project.ID, createRequest)
	timelineOK(t, err)
	if !created.Changed || created.TaskEventID == nil {
		t.Fatal("real Task create omitted history")
	}
	timelineReceipt(t, b, actor, project.ID, createMeta.IdempotencyKey, "work.task.create", *created.TaskEventID, created, false)
	task := created.Task
	fixed := time.Now().UTC().Truncate(time.Microsecond)
	if fixed.Before(task.UpdatedAt.Time()) {
		fixed = task.UpdatedAt.Time()
	}
	clock.hold(fixed)
	update := func(n int) {
		title := fmt.Sprintf("title-%d", n)
		m := meta(t, fmt.Sprintf("timeline-update-%d", n), &task.Version)
		result, e := b.tasks.UpdateTask(ctxFor(t), actor, m, project.ID, task.ID, c.TaskFieldsUpdate{Title: &title})
		timelineOK(t, e)
		if !result.Changed || result.TaskEventID == nil || result.Task.Version != task.Version+1 {
			t.Fatal("real update omitted the next history fact")
		}
		timelineReceipt(t, b, actor, project.ID, m.IdempotencyKey, "work.task.update", *result.TaskEventID, result, false)
		task = result.Task
	}
	for n := 0; n < 200; n++ {
		update(n)
	}
	addVersion := task.Version
	addMeta := meta(t, "timeline-add", &addVersion)
	addRequest := blockerWaiting(t, "retained blocker")
	added, err := b.blockers.AddTaskBlocker(ctxFor(t), actor, addMeta, project.ID, task.ID, addRequest)
	timelineOK(t, err)
	timelineReceipt(t, b, actor, project.ID, addMeta.IdempotencyKey, "work.task.blocker.add", added.TaskEventID, added, true)
	task = added.Task
	resolveMeta := meta(t, "timeline-resolve", &task.Version)
	comment := "resolved once"
	resolved, err := b.blockers.ResolveTaskBlocker(ctxFor(t), actor, resolveMeta, project.ID, task.ID, c.TaskBlockerResolve{BlockerID: added.Blocker.ID, ResolutionComment: &comment})
	timelineOK(t, err)
	timelineReceipt(t, b, actor, project.ID, resolveMeta.IdempotencyKey, "work.task.blocker.resolve", resolved.TaskEventID, resolved, true)
	task = resolved.Task
	values := timelineStored(t, b, project.ID, task.ID)
	if len(values) != 203 || len(values) <= f.MaxPageLimit {
		t.Fatal("real timeline did not exceed the public maximum page")
	}
	same := 0
	for _, v := range values {
		if timelineAt(v).Equal(fixed) {
			same++
		}
	}
	clock.mu.Lock()
	samples := clock.samples
	clock.mu.Unlock()
	if same < 2 || samples < 2 {
		t.Fatal("real writer did not produce the fixed microsecond collision")
	}
	rebuilt, err := work.NewTaskReader(clock, b.authority, b.reader, b.keys)
	timelineOK(t, err)
	filters := []c.TaskTimelineFilter{{}, {Order: c.TaskTimelineAscending}, {Types: []c.TaskTimelineEventType{c.TaskTimelineCreated}}, {Types: []c.TaskTimelineEventType{c.TaskTimelineFieldsUpdated}}, {Types: []c.TaskTimelineEventType{c.TaskTimelineBlockerAdded}}, {Types: []c.TaskTimelineEventType{c.TaskTimelineBlockerResolved}}, {Types: []c.TaskTimelineEventType{c.TaskTimelineBlockerAdded, c.TaskTimelineFieldsUpdated}, Order: c.TaskTimelineAscending}}
	for _, filter := range filters {
		first := timelinePage(t, rebuilt, actor, project.ID, task.ID, filter, f.DefaultPageRequest())
		want := timelineWant(values, filter)
		expectedFirst := min(len(want), 50)
		if len(first.Items) != expectedFirst || (first.NextCursor != "") != (len(want) > 50) {
			t.Fatal("default timeline page/lookahead shape")
		}
		nextActor := b.renew(t, actor)
		nextFilter := filter.Clone()
		slices.Reverse(nextFilter.Types)
		got := timelineRest(t, rebuilt, nextActor, project.ID, task.ID, nextFilter, first, 7)
		timelineEqual(t, got, want)
		max := timelinePage(t, rebuilt, actor, project.ID, task.ID, filter, f.PageRequest{Limit: 200})
		timelineEqual(t, timelineRest(t, rebuilt, actor, project.ID, task.ID, filter, max, 200), want)
	}
	// Original command replay must preserve the exact historical receipt and all
	// current facts, including Activity; it cannot manufacture another event.
	before := b.blockerSnapshot(t, actor)
	replay, err := b.tasks.CreateTask(ctxFor(t), actor, createMeta, project.ID, createRequest)
	timelineOK(t, err)
	equalTaskMutation(t, replay, created)
	replayBlocker, err := b.blockers.AddTaskBlocker(ctxFor(t), actor, addMeta, project.ID, task.ID, addRequest)
	timelineOK(t, err)
	equalBlockerMutation(t, replayBlocker, added)
	if b.blockerSnapshot(t, actor) != before {
		t.Fatal("timeline original replay created another fact")
	}
	for index, order := range []c.TaskTimelineOrder{c.TaskTimelineAscending, c.TaskTimelineDescending} {
		beforeValues := timelineStored(t, b, project.ID, task.ID)
		filter := c.TaskTimelineFilter{Order: order}
		first := timelinePage(t, rebuilt, actor, project.ID, task.ID, filter, f.PageRequest{Limit: 7})
		if first.NextCursor == "" {
			t.Fatal("watermark control missing continuation")
		}
		update(200 + index)
		timelineEqual(t, timelineRest(t, rebuilt, actor, project.ID, task.ID, filter, first, 200), timelineWant(beforeValues, filter))
		fresh := timelinePage(t, rebuilt, actor, project.ID, task.ID, filter, f.PageRequest{Limit: 200})
		timelineEqual(t, timelineRest(t, rebuilt, actor, project.ID, task.ID, filter, fresh, 200), timelineWant(timelineStored(t, b, project.ID, task.ID), filter))
		for _, other := range []c.TaskTimelineFilter{{Order: map[c.TaskTimelineOrder]c.TaskTimelineOrder{c.TaskTimelineAscending: c.TaskTimelineDescending, c.TaskTimelineDescending: c.TaskTimelineAscending}[order]}, {Types: []c.TaskTimelineEventType{c.TaskTimelineFieldsUpdated}, Order: order}} {
			page, e := rebuilt.ListTaskEvents(ctxFor(t), actor, project.ID, task.ID, other, f.PageRequest{Cursor: first.NextCursor, Limit: 7})
			requireCode(t, e, f.CursorInvalid)
			if len(page.Items) != 0 || page.NextCursor != "" {
				t.Fatal("invalid cursor returned partial history")
			}
		}
	}
	// Every read is a same-Tx pure read, including after the clock-only wrapper.
	before = b.blockerSnapshot(t, actor)
	first := timelinePage(t, rebuilt, actor, project.ID, task.ID, c.TaskTimelineFilter{}, f.PageRequest{Limit: 200})
	_ = timelineRest(t, rebuilt, actor, project.ID, task.ID, c.TaskTimelineFilter{}, first, 200)
	if b.blockerSnapshot(t, actor) != before {
		t.Fatal("timeline read touched Activity or business facts")
	}
}
