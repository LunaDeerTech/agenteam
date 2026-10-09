//go:build integration

package work_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
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

func timelineReject(t *testing.T, b *blockerFixture, actor i.Actor, project pc.ProjectID, task c.TaskID, request f.PageRequest, code f.Code) {
	t.Helper()
	before := b.blockerSnapshot(t, actor)
	page, err := b.taskReader.ListTaskEvents(ctxFor(t), actor, project, task, c.TaskTimelineFilter{}, request)
	requireCode(t, err, code)
	if page.Items != nil || page.NextCursor != "" || before != b.blockerSnapshot(t, actor) {
		t.Fatal("rejected timeline read returned partial data or changed facts")
	}
}

// Cancellation observes the original query and original transaction. No SQL,
// context, result or row is replaced. holdRows pauses only after actual Query
// has returned its actual Rows, before handing those Rows to the real Reader.
type timelineReadStore struct {
	*hookStore
	probeMu  sync.Mutex
	query    chan int32
	rows     chan *postgres.Rows
	holdRows bool
	returns  int
	last     f.CommitResult
}
type timelineReadExecutor struct {
	postgres.SQLExecutor
	owner *timelineReadStore
}

func (s *timelineReadStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	x, err := s.hookStore.InTx(tx)
	if err != nil {
		return nil, err
	}
	return &timelineReadExecutor{SQLExecutor: x, owner: s}, nil
}
func (s *timelineReadStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	result := s.hookStore.WithinTx(ctx, cause, fn)
	s.probeMu.Lock()
	s.returns++
	s.last = result
	s.probeMu.Unlock()
	return result
}
func (x *timelineReadExecutor) Query(ctx context.Context, sql string, args ...any) (*postgres.Rows, error) {
	x.owner.probeMu.Lock()
	query, rowsSeen, hold := x.owner.query, x.owner.rows, x.owner.holdRows
	x.owner.probeMu.Unlock()
	observe := query != nil && strings.HasPrefix(sql, "SELECT id::text,project_id::text,task_id::text,task_version,type,actor,") && strings.Contains(sql, " FROM agenteam_work.task_events WHERE ")
	if observe {
		var pid int32
		if err := x.SQLExecutor.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
			return nil, err
		}
		query <- pid
	}
	rows, err := x.SQLExecutor.Query(ctx, sql, args...)
	if observe && rows != nil && rowsSeen != nil {
		rowsSeen <- rows
		if hold {
			<-ctx.Done()
		}
	}
	return rows, err
}
func (s *timelineReadStore) arm(hold bool) (<-chan int32, <-chan *postgres.Rows, int) {
	s.probeMu.Lock()
	defer s.probeMu.Unlock()
	s.query, s.rows, s.holdRows = make(chan int32, 1), make(chan *postgres.Rows, 1), hold
	return s.query, s.rows, s.returns
}
func (s *timelineReadStore) disarm(t *testing.T, previous int) {
	t.Helper()
	s.probeMu.Lock()
	defer s.probeMu.Unlock()
	if s.returns != previous+1 || s.last.State() != f.NotCommitted {
		t.Fatal("cancelled original WithinTx did not actually return once")
	}
	s.query, s.rows, s.holdRows = nil, nil, false
}
func timelinePID(t *testing.T, ch <-chan int32) int32 {
	t.Helper()
	select {
	case pid := <-ch:
		if pid <= 0 {
			t.Fatal("invalid query backend")
		}
		return pid
	case <-time.After(5 * time.Second):
		t.Fatal("original timeline SQL not reached")
	}
	return 0
}

func TestTaskTimelineAuthorityAndCancellation(t *testing.T) {
	db := newDatabase(t)
	raw := openStore(t, db.Config(t, nil))
	store := &timelineReadStore{hookStore: &hookStore{fixtureStore: raw}}
	b := attachBlockers(t, assembleTask(t, db, raw, store, true))
	owner := b.human(t, "timeline-current-owner", "user")
	other := b.human(t, "timeline-next-owner", "user")
	admin := b.human(t, "timeline-not-owner-admin", "admin")
	p, _, _ := b.create(t, owner, "authority")
	m := b.milestone(t, owner, p.ID, "m")
	s := b.sprint(t, owner, p.ID, m.ID, "s")
	task := b.task(t, owner, p.ID, s.ID, "first")
	title := "second"
	_, err := b.tasks.UpdateTask(ctxFor(t), owner, meta(t, "timeline-authority-update", &task.Version), p.ID, task.ID, c.TaskFieldsUpdate{Title: &title})
	timelineOK(t, err)
	first := timelinePage(t, b.taskReader, owner, p.ID, task.ID, c.TaskTimelineFilter{}, f.PageRequest{Limit: 1})
	if first.NextCursor == "" {
		t.Fatal("authority cursor premise missing")
	}
	next := f.PageRequest{Limit: 1, Cursor: first.NextCursor}
	timelineReject(t, b, other, p.ID, task.ID, next, f.NotFound)
	timelineReject(t, b, admin, p.ID, task.ID, next, f.NotFound)
	timelineReject(t, b, owner, p.ID, id[c.Task](t), next, f.TaskNotFound)
	p2, _, _ := b.create(t, owner, "other-project")
	timelineReject(t, b, owner, p2.ID, task.ID, next, f.TaskNotFound)
	m2 := b.milestone(t, owner, p2.ID, "m")
	s2 := b.sprint(t, owner, p2.ID, m2.ID, "s")
	task2 := b.task(t, owner, p2.ID, s2.ID, "other-task")
	timelineReject(t, b, owner, p2.ID, task2.ID, next, f.CursorInvalid)
	otherTask := b.task(t, owner, p.ID, s.ID, "same-project-other-task")
	timelineReject(t, b, owner, p.ID, otherTask.ID, next, f.CursorInvalid)

	current := b.renew(t, owner)
	user, _ := f.UserLock(owner.Details().UserID)
	b.tx(t, []f.LockRequest{{Key: user, Mode: f.Exclusive}}, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		_, err := x.Exec(ctx, `UPDATE agenteam_account.sessions SET revoked_at=clock_timestamp(),revoked_reason='administrative' WHERE id=$1`, owner.Details().SessionID)
		return err
	})
	timelineReject(t, b, owner, p.ID, task.ID, next, f.SessionRevoked)
	timelinePage(t, b.taskReader, current, p.ID, task.ID, c.TaskTimelineFilter{}, next)
	b.transferOwner(t, current, other, p.ID)
	timelineReject(t, b, current, p.ID, task.ID, next, f.NotFound)
	timelineReject(t, b, other, p.ID, task.ID, next, f.CursorInvalid)
	timelinePage(t, b.taskReader, other, p.ID, task.ID, c.TaskTimelineFilter{}, f.DefaultPageRequest())

	for _, mode := range []string{"lock", "sql", "rows"} {
		t.Run("cancel-"+mode, func(t *testing.T) {
			before := b.blockerSnapshot(t, other)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			key, _ := f.AggregateLock(f.TaskAggregate, task.ID.String())
			observed := observeLock(store.hookStore, key, nil)
			conn := db.Connect(t)
			hold, err := conn.Begin(ctxFor(t))
			timelineOK(t, err)
			rollback := func() {
				ctx, end := context.WithTimeout(context.Background(), 3*time.Second)
				defer end()
				_ = hold.Rollback(ctx)
			}
			defer rollback()
			if mode == "lock" {
				_, err = hold.Exec(ctxFor(t), "SELECT pg_advisory_xact_lock($1)", key.AdvisoryKey())
			}
			if mode == "sql" {
				_, err = hold.Exec(ctxFor(t), "LOCK TABLE agenteam_work.task_events IN ACCESS EXCLUSIVE MODE")
			}
			timelineOK(t, err)
			query, rows, previous := store.arm(mode == "rows")
			done := make(chan struct{})
			var page f.Page[c.TaskTimelineEvent]
			var readErr error
			go func() {
				defer close(done)
				page, readErr = b.taskReader.ListTaskEvents(ctx, other, p.ID, task.ID, c.TaskTimelineFilter{}, f.DefaultPageRequest())
			}()
			t.Cleanup(func() { cancel(); rollback(); await(t, done) })
			var backend int32
			if mode == "lock" {
				select {
				case attempt := <-observed:
					backend = attempt.BackendPID
					waitTaskExactMode(t, b.taskFixture, attempt, f.Shared, int32(conn.PgConn().PID()))
				case <-time.After(5 * time.Second):
					t.Fatal("actual Task SH waiter absent")
				}
			} else {
				backend = timelinePID(t, query)
			}
			if mode == "sql" {
				check := db.Connect(t)
				waitCtx, end := context.WithTimeout(context.Background(), 5*time.Second)
				defer end()
				for {
					var waiting bool
					err = check.QueryRow(waitCtx, `SELECT EXISTS(SELECT 1 FROM pg_locks l JOIN pg_stat_activity a ON a.pid=l.pid WHERE a.datname=$1 AND l.pid=$2 AND l.locktype='relation' AND l.relation='agenteam_work.task_events'::regclass AND l.mode='AccessShareLock' AND NOT l.granted AND $3=ANY(pg_blocking_pids(l.pid)))`, db.Name, backend, int32(conn.PgConn().PID())).Scan(&waiting)
					timelineOK(t, err)
					if waiting {
						break
					}
					select {
					case <-waitCtx.Done():
						t.Fatal("original history SQL relation waiter absent")
					case <-time.After(5 * time.Millisecond):
					}
				}
			}
			if mode == "rows" {
				select {
				case actual := <-rows:
					if actual == nil {
						t.Fatal("actual Rows missing")
					}
				case <-time.After(5 * time.Second):
					t.Fatal("actual Rows not returned")
				}
			}
			cancel()
			await(t, done)
			if !errors.Is(readErr, context.Canceled) || page.Items != nil || page.NextCursor != "" {
				t.Fatal("cancelled original read pretended completion", readErr)
			}
			store.disarm(t, previous)
			rollback()
			var active int
			timelineOK(t, raw.QueryRow(ctxFor(t), `SELECT count(*) FROM pg_stat_activity WHERE pid=$1 AND (state='active' OR xact_start IS NOT NULL)`, backend).Scan(&active))
			if active != 0 {
				t.Fatal("cancelled backend transaction still active")
			}
			b.tx(t, []f.LockRequest{{Key: key, Mode: f.Exclusive}}, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
				var one int
				return x.QueryRow(ctx, "SELECT 1").Scan(&one)
			})
			timelinePage(t, b.taskReader, other, p.ID, task.ID, c.TaskTimelineFilter{}, f.DefaultPageRequest())
			if before != b.blockerSnapshot(t, other) {
				t.Fatal("cancelled/read-only timeline changed facts or Activity")
			}
		})
	}

	for _, state := range []string{"pending", "archiving", "archived", "deleting"} {
		t.Run(state, func(t *testing.T) {
			p, _, _ := b.create(t, current, "lifecycle-"+state)
			m := b.milestone(t, current, p.ID, "m")
			s := b.sprint(t, current, p.ID, m.ID, "s")
			task := b.task(t, current, p.ID, s.ID, "history")
			switch state {
			case "pending":
				b.tx(t, fixtureLocks(current, p.ID), func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
					_, err := x.Exec(ctx, `UPDATE agenteam_project.projects SET initialized_at=NULL,version=1,current_sprint_id=NULL WHERE id=$1`, p.ID.String())
					return err
				})
			case "archiving":
				b.seedProjectTransition(t, current, p.ID, pc.Archiving)
			case "deleting":
				b.seedProjectTransition(t, current, p.ID, pc.Deleting)
			case "archived":
				b.tx(t, fixtureLocks(current, p.ID), func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
					_, err := x.Exec(ctx, `WITH moment AS MATERIALIZED(SELECT clock_timestamp() AS value) UPDATE agenteam_project.projects SET lifecycle='archived',archived_at=moment.value,updated_at=moment.value FROM moment WHERE id=$1`, p.ID.String())
					return err
				})
			}
			before := b.blockerSnapshot(t, current)
			if state == "pending" || state == "deleting" {
				timelineReject(t, b, current, p.ID, task.ID, f.DefaultPageRequest(), f.ProjectNotActive)
			} else {
				page := timelinePage(t, b.taskReader, current, p.ID, task.ID, c.TaskTimelineFilter{}, f.DefaultPageRequest())
				if len(page.Items) != 1 {
					t.Fatal("historical lifecycle read absent")
				}
			}
			if before != b.blockerSnapshot(t, current) {
				t.Fatal("lifecycle read mutated facts")
			}
		})
	}
	t.Log("lifecycle/Owner/Session changes are isolated test inputs, not implemented lifecycle or ownership commands")
}

func TestTaskTimelineIntegrityAndCompatibility(t *testing.T) {
	b := attachBlockers(t, newTaskFixture(t))
	actor := b.human(t, "timeline-integrity", "user")
	p, _, _ := b.create(t, actor, "integrity")
	m := b.milestone(t, actor, p.ID, "m")
	s := b.sprint(t, actor, p.ID, m.ID, "s")
	task := b.task(t, actor, p.ID, s.ID, "original")
	added, err := b.blockers.AddTaskBlocker(ctxFor(t), actor, meta(t, "timeline-integrity-add", &task.Version), p.ID, task.ID, blockerWaiting(t, "blocking"))
	timelineOK(t, err)
	task = added.Task
	stored := timelineStored(t, b, p.ID, task.ID)
	if len(stored) != 2 {
		t.Fatal("old codec compatibility premise missing")
	}
	for _, event := range stored {
		raw := []byte(timelineJSON(t, event))
		var planning c.TaskEvent
		var blocker c.TaskBlockerEvent
		if event.Planning != nil {
			timelineOK(t, planning.UnmarshalJSON(raw))
			if blocker.UnmarshalJSON(raw) == nil {
				t.Fatal("old blocker decoder accepted planning")
			}
		} else {
			timelineOK(t, blocker.UnmarshalJSON(raw))
			if planning.UnmarshalJSON(raw) == nil {
				t.Fatal("old planning decoder accepted blocker")
			}
		}
	}
	before := b.blockerSnapshot(t, actor)
	got, err := b.taskReader.GetTask(ctxFor(t), actor, p.ID, task.ID)
	timelineOK(t, err)
	listed, err := b.taskReader.ListTasks(ctxFor(t), actor, p.ID, c.TaskFilter{}, f.DefaultPageRequest())
	timelineOK(t, err)
	if timelineJSON(t, got) != timelineJSON(t, task) || len(listed.Items) != 1 || timelineJSON(t, listed.Items[0]) != timelineJSON(t, task) {
		t.Fatal("old Get/List compatibility differs")
	}
	empty := timelinePage(t, b.taskReader, actor, p.ID, task.ID, c.TaskTimelineFilter{Types: []c.TaskTimelineEventType{c.TaskTimelineBlockerResolved}}, f.DefaultPageRequest())
	if len(empty.Items) != 0 || empty.NextCursor != "" {
		t.Fatal("nonmatching typed filter not an empty page")
	}
	// The signed position is built from the published contract, not the new
	// Reader's private binding helper. Same identity/scope, impossible waterline.
	bindingRaw := []byte(fmt.Sprintf(`{"format":1,"kind":"work.task-timeline","project_id":%q,"task_id":%q,"owner_user_id":%q,"types":["task_created","fields_updated","blocker_added","blocker_resolved"],"order":"desc"}`, p.ID.String(), task.ID.String(), actor.Details().UserID))
	digest, err := cursor.Digest(bindingRaw)
	timelineOK(t, err)
	scope, err := i.InProject(p.ID)
	timelineOK(t, err)
	binding := cursor.Binding{Scope: scope, QueryDigest: digest, Order: "created_at:desc,id:desc"}
	stamp, err := f.ParseInstant(stored[0].Planning.CreatedAt.String())
	timelineOK(t, err)
	ts, err := cursor.Instant(stamp)
	timelineOK(t, err)
	uuid, err := cursor.UUID(timelineID(stored[0]))
	timelineOK(t, err)
	for _, watermark := range []int64{int64(task.Version) + 1, 0} {
		token, err := b.keys.Sign(binding, cursor.Position{Scalars: []cursor.Scalar{ts, uuid, cursor.Integer(watermark)}})
		timelineOK(t, err)
		code := f.CursorStale
		if watermark == 0 {
			code = f.CursorInvalid
		}
		timelineReject(t, b, actor, p.ID, task.ID, f.PageRequest{Limit: 1, Cursor: token}, code)
	}
	if before != b.blockerSnapshot(t, actor) {
		t.Fatal("compatibility reads changed facts")
	}
	// Only this nonce-owned test database relaxes its history kind CHECK so
	// negative future/corrupt persisted inputs can reach the real decoder.
	// No positive history is inserted or manufactured by SQL.
	_, err = b.raw.Exec(ctxFor(t), `ALTER TABLE agenteam_work.task_events DROP CONSTRAINT task_events_operation_kind_check`)
	timelineOK(t, err)
	for _, kind := range []string{"unknown", "payload", "actor", "operation", "correlation", "lookahead"} {
		t.Run(kind, func(t *testing.T) {
			task := b.task(t, actor, p.ID, s.ID, "corrupt-"+kind)
			title := "updated"
			updated, err := b.tasks.UpdateTask(ctxFor(t), actor, meta(t, "timeline-corrupt-"+kind, &task.Version), p.ID, task.ID, c.TaskFieldsUpdate{Title: &title})
			timelineOK(t, err)
			event := updated.TaskEventID
			if kind == "lookahead" {
				var first string
				timelineOK(t, b.raw.QueryRow(ctxFor(t), `SELECT id::text FROM agenteam_work.task_events WHERE task_id=$1 AND type='task_created'`, task.ID.String()).Scan(&first))
				value, err := f.ParseID[c.TaskEvent](first)
				timelineOK(t, err)
				event = &value
			}
			key, _ := f.AggregateLock(f.TaskAggregate, task.ID.String())
			schedule, _ := f.ProjectScheduleLock(p.ID.String())
			b.tx(t, fixtureLocks(actor, p.ID, f.LockRequest{Key: schedule, Mode: f.Exclusive}, f.LockRequest{Key: key, Mode: f.Exclusive}), func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
				change := "payload='{}'::jsonb"
				switch kind {
				case "unknown":
					change = "type='future_task_event'"
				case "actor":
					change = "actor=actor||'{\"unexpected\":true}'::jsonb"
				case "operation":
					change = "operation_id=NULL"
				case "correlation":
					change = "correlation_id=task_id"
				}
				_, err := x.Exec(ctx, "UPDATE agenteam_work.task_events SET "+change+" WHERE project_id=$1 AND task_id=$2 AND id=$3", p.ID.String(), task.ID.String(), event.String())
				return err
			})
			timelineReject(t, b, actor, p.ID, task.ID, f.PageRequest{Limit: 1}, f.InternalError)
			if kind == "unknown" {
				page := timelinePage(t, b.taskReader, actor, p.ID, task.ID, c.TaskTimelineFilter{Types: []c.TaskTimelineEventType{c.TaskTimelineCreated}}, f.DefaultPageRequest())
				if len(page.Items) != 1 {
					t.Fatal("explicit supported filter lost its valid event")
				}
			}
		})
	}
}
