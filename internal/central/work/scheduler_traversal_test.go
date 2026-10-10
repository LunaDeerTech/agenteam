package work

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"github.com/jackc/pgx/v5"
)

type traversalReadStore struct {
	*taskLaunchTestStore
	readErr error
	missing bool
	cancel  context.CancelFunc
	query   string
	args    []any
	nilRows bool
}

func (s *traversalReadStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx {
		return nil, fault(f.Forbidden)
	}
	return s, nil
}
func (s *traversalReadStore) QueryRow(ctx context.Context, q string, args ...any) postgres.Row {
	if s.cancel != nil {
		s.cancel()
	}
	if s.readErr != nil {
		return unblockErrorRow{s.readErr}
	}
	if s.missing && strings.Contains(q, "FROM agenteam_work.tasks ") {
		return unblockErrorRow{pgx.ErrNoRows}
	}
	return s.taskLaunchTestStore.QueryRow(ctx, q, args...)
}
func (s *traversalReadStore) Query(_ context.Context, q string, args ...any) (*postgres.Rows, error) {
	s.queries++
	s.query, s.args = q, append([]any(nil), args...)
	if s.nilRows {
		return nil, nil
	}
	return nil, errors.New("private-traversal-query-canary")
}

type traversalReadProject struct {
	*taskLaunchTestProject
	calls  int
	cancel context.CancelFunc
	err    error
}

func (p *traversalReadProject) RequireSchedulerProjectInTx(_ context.Context, _ f.Tx, _ pc.ProjectID) (pc.SchedulerProject, error) {
	p.calls++
	if p.cancel != nil {
		p.cancel()
	}
	return p.value.Clone(), p.err
}

func newTraversalReadControl(t *testing.T) (*SchedulerTaskReader, *traversalReadStore, *traversalReadProject) {
	t.Helper()
	_, base, projects, _, _, _, _ := newTaskLaunchControl(t)
	store := &traversalReadStore{taskLaunchTestStore: base}
	project := &traversalReadProject{taskLaunchTestProject: projects}
	authority, err := NewAuthority(store, project)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := NewSchedulerTaskReader(store, authority)
	if err != nil {
		t.Fatal(err)
	}
	return reader, store, project
}

func traversalReadLocks(t *testing.T, store *traversalReadStore, current bool) {
	t.Helper()
	locks := []f.LockRequest{projectLock(store.task.ProjectID, f.Shared), taskScheduleLock(store.task.ProjectID, f.Exclusive)}
	if current {
		locks = append(locks, taskLock(store.task.ID.String(), f.Shared))
	}
	var err error
	store.required, err = taskNormalize(locks)
	if err != nil {
		t.Fatal(err)
	}
}

func TestSchedulerTaskReaderScopeAndEmptySprint(t *testing.T) {
	r, s, project := newTraversalReadControl(t)
	p := s.task.ProjectID
	traversalReadLocks(t, s, false)
	project.value.Project.CurrentSprintID = nil
	project.value.Config.Enabled = false
	got, err := r.SnapshotInTx(context.Background(), s.tx, p)
	if err != nil || got.ProjectID != p || got.CurrentSprintID != nil || got.Entries == nil || len(got.Entries) != 0 || project.calls != 1 || s.queries != 0 || s.acquires != 0 || s.writes != 0 {
		t.Fatal("paused nil-current read did not return real empty snapshot", err)
	}
	for _, mutate := range []func(){
		func() { s.required = nil },
		func() {
			for n := range s.required {
				if s.required[n].Key.Canonical() == taskScheduleLock(p, f.Exclusive).Key.Canonical() {
					s.required[n].Mode = f.Shared
				}
			}
		},
	} {
		traversalReadLocks(t, s, false)
		mutate()
		prior := project.calls
		if got, err = r.SnapshotInTx(context.Background(), s.tx, p); err == nil || got.Entries != nil || project.calls != prior {
			t.Fatal("incomplete held locks reached Project or returned a snapshot")
		}
	}
	traversalReadLocks(t, s, false)
	if got, err = r.SnapshotInTx(context.Background(), f.NewTx(), p); err == nil || got.Entries != nil {
		t.Fatal("foreign Tx read")
	}
	project.value.Project.ID = pureID[i.Project](t, 150)
	if got, err = r.SnapshotInTx(context.Background(), s.tx, p); err == nil || got.Entries != nil {
		t.Fatal("mismatched current Project read")
	}
	project.value.Project.ID = p
	ctx, cancel := context.WithCancel(context.Background())
	project.cancel = cancel
	got, err = r.SnapshotInTx(ctx, s.tx, p)
	cancel()
	if !errors.Is(err, context.Canceled) || got.Entries != nil {
		t.Fatal("cancelled Project gate returned empty success")
	}
	var absent *Authority
	if _, err = NewSchedulerTaskReader(s, absent); err == nil {
		t.Fatal("missing original authority accepted")
	}
	_, _, otherAuthority, _ := purePorts(t)
	if _, err = NewSchedulerTaskReader(s, otherAuthority); err == nil {
		t.Fatal("foreign Store authority accepted")
	}
	if s.queries != 0 || s.acquires != 0 || s.writes != 0 {
		t.Fatal("trusted gate failures performed SQL or acquired/wrote")
	}
}

type traversalSnapshotRows struct {
	rows       []taskTriggerRow
	index      int
	closed     bool
	closeCount int
	rowErr     error
	closeErr   error
	scanErr    error
	cancel     context.CancelFunc
	generated  int
	project    c.ProjectID
	sprint     c.SprintID
}

func (r *traversalSnapshotRows) Next() bool {
	n := len(r.rows)
	if r.generated != 0 {
		n = r.generated
	}
	if r.index >= n {
		return false
	}
	r.index++
	return true
}
func (r *traversalSnapshotRows) Scan(out ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	if r.generated != 0 {
		id := fmt.Sprintf("018f0000-0000-7000-8000-%012x", r.index)
		return (taskTriggerRow{id, r.project.String(), r.sprint.String(), c.TaskStateTodo, c.TaskPriorityMedium, fmt.Sprintf("%032x", r.index)}).Scan(out...)
	}
	return r.rows[r.index-1].Scan(out...)
}
func (r *traversalSnapshotRows) Err() error {
	if r.closed && r.closeErr != nil {
		return r.closeErr
	}
	return r.rowErr
}
func (r *traversalSnapshotRows) Close() {
	r.closed = true
	r.closeCount++
	if r.cancel != nil {
		r.cancel()
	}
}

func TestSchedulerTaskSnapshotOrderAndCompleteTail(t *testing.T) {
	p, sprint := pureID[i.Project](t, 151), pureID[pc.Sprint](t, 152)
	var rows []taskTriggerRow
	for n, pair := range [][2]string{{"todo", "critical"}, {"todo", "critical"}, {"todo", "low"}, {"in_progress", "high"}, {"in_review", "medium"}, {"blocked", "low"}} {
		rows = append(rows, taskTriggerRow{pureID[c.Task](t, 160+n).String(), p.String(), sprint.String(), c.TaskState(pair[0]), c.TaskPriority(pair[1]), fmt.Sprintf("%032x", n+1)})
	}
	input := &traversalSnapshotRows{rows: rows}
	got, err := collectSchedulerTaskSnapshot(context.Background(), input, p, sprint)
	if err != nil || !input.closed || input.closeCount != 1 || len(got.Entries) != len(rows) || got.CurrentSprintID == nil || *got.CurrentSprintID != sprint {
		t.Fatal("complete ordered identity snapshot", err)
	}
	for n, entry := range got.Entries {
		if entry.TaskID.String() != rows[n][0] || entry.State != rows[n][3] {
			t.Fatal("snapshot changed authoritative order")
		}
	}
	copy := got.Clone()
	copy.Entries[0].TaskID = pureID[c.Task](t, 180)
	*copy.CurrentSprintID = pureID[pc.Sprint](t, 181)
	if got.Entries[0].TaskID == copy.Entries[0].TaskID || *got.CurrentSprintID != sprint {
		t.Fatal("snapshot clone aliases")
	}
	for _, mutate := range []func(*traversalSnapshotRows){
		func(r *traversalSnapshotRows) { r.rows[1][0] = r.rows[0][0] },
		func(r *traversalSnapshotRows) { r.rows[1][1] = pureID[i.Project](t, 182).String() },
		func(r *traversalSnapshotRows) { r.rows[1][2] = pureID[pc.Sprint](t, 182).String() },
		func(r *traversalSnapshotRows) { r.rows[1][3] = c.TaskStateBacklog },
		func(r *traversalSnapshotRows) { r.rows[1][4] = c.TaskPriority("future") },
		func(r *traversalSnapshotRows) { r.rows[1][5] = "invalid" },
		func(r *traversalSnapshotRows) { r.rows[0], r.rows[2] = r.rows[2], r.rows[0] },
		func(r *traversalSnapshotRows) { r.scanErr = errors.New("private-row-canary") },
		func(r *traversalSnapshotRows) { r.rowErr = errors.New("private-rows-tail-canary") },
		func(r *traversalSnapshotRows) { r.closeErr = errors.New("private-close-canary") },
	} {
		bad := &traversalSnapshotRows{}
		for _, row := range rows {
			bad.rows = append(bad.rows, append(taskTriggerRow(nil), row...))
		}
		mutate(bad)
		out, err := collectSchedulerTaskSnapshot(context.Background(), bad, p, sprint)
		if err == nil || out.Entries != nil || out.CurrentSprintID != nil || !bad.closed || bad.closeCount != 1 {
			t.Fatal("bad source returned partial identities or abandoned rows")
		}
		if strings.Contains(fmt.Sprintf("%+v", err), "private-") {
			t.Fatal("source error leaked diagnostics")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	input = &traversalSnapshotRows{rows: rows, cancel: cancel}
	got, err = collectSchedulerTaskSnapshot(ctx, input, p, sprint)
	cancel()
	if !errors.Is(err, context.Canceled) || got.Entries != nil || !input.closed {
		t.Fatal("cancellation during Close returned a snapshot")
	}
	input = &traversalSnapshotRows{generated: c.MaxProjectTasks + 1, project: p, sprint: sprint}
	got, err = collectSchedulerTaskSnapshot(context.Background(), input, p, sprint)
	if err == nil || got.Entries != nil || !input.closed {
		t.Fatal("over-cap snapshot silently truncated")
	}
}

func TestSchedulerTaskReaderCurrentFacts(t *testing.T) {
	r, s, project := newTraversalReadControl(t)
	p, id := s.task.ProjectID, s.task.ID
	traversalReadLocks(t, s, true)
	project.value.Config.Enabled = false
	project.value.Project.CurrentSprintID = nil
	s.unresolved = 1
	got, err := r.CurrentTaskInTx(context.Background(), s.tx, p, id)
	if err != nil || got.TaskID != id || got.ProjectID != p || got.Version != s.task.Version || got.State != s.task.State || !got.HasUnresolvedBlockers || got.AssigneeAgentID == nil || *got.AssigneeAgentID != *s.task.AssigneeAgentID {
		t.Fatal("current facts missing on paused historical Task", err)
	}
	copy := got.Clone()
	*copy.AssigneeAgentID = pureID[i.Agent](t, 184)
	if *got.AssigneeAgentID != *s.task.AssigneeAgentID {
		t.Fatal("current facts clone aliases")
	}
	s.task.State, s.task.Version, s.unresolved = c.TaskStateDone, s.task.Version+1, 0
	current, err := r.CurrentTaskInTx(context.Background(), s.tx, p, id)
	if err != nil || current.State != c.TaskStateDone || current.Version != s.task.Version || current.HasUnresolvedBlockers {
		t.Fatal("reader used snapshot state/version instead of current Task", err)
	}
	s.missing = true
	current, err = r.CurrentTaskInTx(context.Background(), s.tx, p, id)
	pureCode(t, err, f.TaskNotFound)
	if !reflect.DeepEqual(current, c.SchedulerTaskFacts{}) {
		t.Fatal("missing Task returned facts")
	}
	s.missing = false
	s.unresolved = -1
	if current, err = r.CurrentTaskInTx(context.Background(), s.tx, p, id); err == nil || !reflect.DeepEqual(current, c.SchedulerTaskFacts{}) {
		t.Fatal("invalid blocker count returned facts")
	}
	s.unresolved = 0
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	current, err = r.CurrentTaskInTx(ctx, s.tx, p, id)
	cancel()
	if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(current, c.SchedulerTaskFacts{}) {
		t.Fatal("cancelled canonical read returned facts")
	}
	s.cancel = nil
	traversalReadLocks(t, s, false)
	if _, err = r.CurrentTaskInTx(context.Background(), s.tx, p, id); err == nil {
		t.Fatal("current read omitted Task SH")
	}
	project.value.Project.CurrentSprintID = &s.sprint.ID
	for _, nilRows := range []bool{false, true} {
		s.nilRows = nilRows
		out, err := r.SnapshotInTx(context.Background(), s.tx, p)
		if err == nil || out.Entries != nil || s.query != schedulerTaskSnapshotSQL || len(s.args) != 3 || s.args[2] != c.MaxProjectTasks+1 {
			t.Fatal("snapshot query failure became empty success")
		}
	}
	if s.acquires != 0 || s.writes != 0 {
		t.Fatal("read provider acquired locks or mutated")
	}
}
