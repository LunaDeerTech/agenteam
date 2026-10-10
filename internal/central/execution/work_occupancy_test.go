package execution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// Controlled Store admission and row iteration verify this adapter's boundary
// and strict decoder. They do not simulate successful PostgreSQL execution of
// workOccupancySQL, a real Project grant, Task assignment or pending dispatch.
type occupancyStoreControl struct {
	*launchStore
	queryErr error
	query    string
	args     []any
	calls    int
}

func (s *occupancyStoreControl) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.launchStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *occupancyStoreControl) Query(_ context.Context, query string, args ...any) (*postgres.Rows, error) {
	s.calls++
	s.query, s.args = query, args
	return nil, s.queryErr
}

func occupancyBoundary(t *testing.T) (*WorkOccupancy, *occupancyStoreControl, i.ProjectID, []string) {
	t.Helper()
	project := newTestID[i.Project](t)
	store := &occupancyStoreControl{launchStore: &launchStore{t: t, tx: f.NewTx(), live: true}}
	store.locks = occupancyLocks(project)
	reader, err := NewWorkOccupancy(store)
	if err != nil {
		t.Fatal(err)
	}
	return reader, store, project, []string{newTestID[struct{}](t).String()}
}

func requireNoOccupancy(t *testing.T, value c.ExecutionOccupancy, err error) {
	t.Helper()
	if err == nil || value.Active != nil || value.HistoryTaskIDs != nil {
		t.Fatal("rejection returned partial or successful occupancy", err)
	}
}

func TestExecutionWorkOccupancyRequiresOriginalTransactionAndGates(t *testing.T) {
	for _, name := range []string{"foreign-tx", "closed-tx", "missing-project", "schedule-sh", "missing-schedule"} {
		t.Run(name, func(t *testing.T) {
			r, s, project, _ := occupancyBoundary(t)
			tx := s.tx
			switch name {
			case "foreign-tx":
				tx = f.NewTx()
			case "closed-tx":
				s.live = false
			case "missing-project":
				s.locks = s.locks[1:]
			case "schedule-sh":
				s.locks[1].Mode = f.Shared
			case "missing-schedule":
				s.locks = s.locks[:1]
			}
			got, err := r.ReadInTx(context.Background(), tx, project, []string{})
			requireNoOccupancy(t, got, err)
			requireCode(t, err, f.Forbidden)
			if s.calls != 0 {
				t.Fatal("read preceded the original transaction/lock gates")
			}
		})
	}
	r, s, project, _ := occupancyBoundary(t)
	got, err := r.ReadInTx(context.Background(), s.tx, project, []string{})
	if err != nil || got.Active == nil || got.HistoryTaskIDs == nil || len(got.Active)+len(got.HistoryTaskIDs) != 0 || s.calls != 0 {
		t.Fatal("already authorized empty Task set lost its explicit result", err)
	}
	var unbound *WorkOccupancy
	got, err = unbound.ReadInTx(context.Background(), s.tx, project, []string{})
	requireNoOccupancy(t, got, err)
	requireCode(t, err, f.DependencyUnbound)
	var nilStore *occupancyStoreControl
	if _, err = NewWorkOccupancy(nilStore); err == nil {
		t.Fatal("typed-nil store accepted")
	}
}

func TestExecutionWorkOccupancyValidatesTaskSetAndPreservesReadFailures(t *testing.T) {
	r, s, project, tasks := occupancyBoundary(t)
	for _, ids := range [][]string{nil, {"bad"}, {"00000000-0000-7000-8000-00000000000A"}, {tasks[0], tasks[0]}, {"ffffffff-ffff-7fff-bfff-ffffffffffff", "00000000-0000-7000-8000-000000000001"}} {
		got, err := r.ReadInTx(context.Background(), s.tx, project, ids)
		requireNoOccupancy(t, got, err)
		requireCode(t, err, f.InvalidArgument)
	}
	got, err := r.ReadInTx(context.Background(), s.tx, project, make([]string, c.MaxWorkOccupancyTasks+1))
	requireNoOccupancy(t, got, err)
	requireCode(t, err, f.PayloadTooLarge)
	if s.calls != 0 {
		t.Fatal("invalid task set reached SQL")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err = r.ReadInTx(ctx, s.tx, project, tasks)
	requireNoOccupancy(t, got, err)
	if !errors.Is(err, context.Canceled) || s.calls != 0 {
		t.Fatal("cancelled caller reached SQL")
	}
	s.queryErr = errors.New("occupancy-private-storage-canary")
	got, err = r.ReadInTx(context.Background(), s.tx, project, tasks)
	requireNoOccupancy(t, got, err)
	requireCode(t, err, f.DependencyUnavailable)
	if s.calls != 1 || !strings.Contains(s.query, "agenteam_execution.executions") || strings.Contains(fmt.Sprintf("%+v", err), "occupancy-private") || s.args[0] != project.String() || !reflect.DeepEqual(s.args[1], tasks) {
		t.Fatal("query lost its original scope or exposed storage material")
	}
	// Query parameters own the Task slice; an external change cannot rewrite it.
	tasks[0] = "changed"
	if s.args[1].([]string)[0] == "changed" {
		t.Fatal("query retained caller-owned Task slice")
	}
}

type occupancyRowsControl struct {
	values              [][]any
	n                   int
	closed              bool
	scanErr, endErr     error
	closeErr            error
	beforeScan, onClose func()
}

func (r *occupancyRowsControl) Next() bool { return !r.closed && r.n < len(r.values) }
func (r *occupancyRowsControl) Scan(dest ...any) error {
	if r.beforeScan != nil {
		r.beforeScan()
	}
	values := r.values[r.n]
	r.n++
	return (launchScan{values: values, err: r.scanErr}).Scan(dest...)
}
func (r *occupancyRowsControl) Close() {
	r.closed = true
	if r.onClose != nil {
		r.onClose()
	}
}
func (r *occupancyRowsControl) Err() error {
	if r.closed && r.closeErr != nil {
		return r.closeErr
	}
	if r.n == len(r.values) {
		return r.endErr
	}
	return nil
}

func occupancyRow(t *testing.T, request c.LaunchRequest, state c.Status, cancelled bool) []any {
	t.Helper()
	execution := newTestID[i.Execution](t)
	digest, err := request.Digest()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	later := created.Add(time.Second)
	var started, completed, cancel *time.Time
	var snapshot *string
	if state == c.Running || state == c.Waiting || state == c.Succeeded {
		started = &later
		id := newTestID[c.Snapshot](t).String()
		snapshot = &id
	}
	if state.Terminal() {
		completed = &later
	}
	if cancelled {
		cancel = &later
	}
	return []any{execution.String(), request.ProjectID.String(), request.AgentID.String(), string(state), int64(2), cancel, snapshot, created, started, completed, raw, request.Meta.IdempotencyKey.String(), request.Meta.RequestID.String(), string(digest)}
}

func TestExecutionWorkOccupancyProjectsActiveAndHistoricalOriginalRows(t *testing.T) {
	x := newLaunchControl(t)
	request := x.request.Clone()
	tasks := []string{request.Trigger.TaskID, newTestID[struct{}](t).String(), newTestID[struct{}](t).String()}
	slices.Sort(tasks)
	request.Trigger.TaskID = tasks[0]
	var rows [][]any
	wantIDs := map[string]c.Status{}
	wantAgents := map[string]i.AgentID{}
	for _, state := range []c.Status{c.Created, c.Preparing, c.Running, c.Waiting} {
		request.AgentID = newTestID[i.Agent](t)
		row := occupancyRow(t, request, state, true)
		rows = append(rows, row)
		wantIDs[row[0].(string)] = state
		wantAgents[row[0].(string)] = request.AgentID
	}
	request.Trigger.TaskID = tasks[1]
	for _, state := range []c.Status{c.Succeeded, c.Failed, c.Cancelled} {
		rows = append(rows, occupancyRow(t, request, state, false))
	}
	slices.Reverse(rows)
	control := &occupancyRowsControl{values: rows}
	got, err := collectWorkOccupancy(context.Background(), control, request.ProjectID, tasks)
	if err != nil || !control.closed || len(got.Active) != 4 || !reflect.DeepEqual(got.HistoryTaskIDs, tasks[:2]) {
		t.Fatal("active cancellation or terminal history was lost", err)
	}
	for n, active := range got.Active {
		if active.TaskID != tasks[0] || active.AgentID != wantAgents[active.ExecutionID.String()] || wantIDs[active.ExecutionID.String()] != active.Status || n > 0 && got.Active[n-1].ExecutionID.String() >= active.ExecutionID.String() {
			t.Fatal("wrong or unordered active identity")
		}
	}
	encoded, _ := json.Marshal(map[string]any{"occupancy": got})
	if strings.Contains(string(encoded), tasks[0]) || strings.Contains(fmt.Sprintf("%+v", got), tasks[0]) {
		t.Fatal("default projection exposed occupancy material")
	}
}

func TestExecutionWorkOccupancyRejectsPartialRowsAndJoinsClose(t *testing.T) {
	for _, name := range []string{"foreign-project", "foreign-task", "meeting", "bad-digest", "bad-state", "duplicate", "scan", "end", "close", "cancel-scan", "cancel-close"} {
		t.Run(name, func(t *testing.T) {
			x := newLaunchControl(t)
			request := x.request.Clone()
			first := occupancyRow(t, request, c.Created, false)
			row := occupancyRow(t, request, c.Waiting, true)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			r := &occupancyRowsControl{values: [][]any{first, row}}
			private := errors.New("occupancy-private-row-canary")
			switch name {
			case "foreign-project":
				request.ProjectID = newTestID[i.Project](t)
				r.values[1] = occupancyRow(t, request, c.Created, false)
			case "foreign-task":
				request.Trigger.TaskID = newTestID[struct{}](t).String()
				r.values[1] = occupancyRow(t, request, c.Created, false)
			case "meeting":
				request.Trigger = c.Trigger{Kind: "meeting", MeetingID: newTestID[struct{}](t).String(), TurnID: newTestID[struct{}](t).String(), ContributionID: newTestID[struct{}](t).String(), ParticipantID: newTestID[struct{}](t).String()}
				request.Purpose = "meeting/response"
				r.values[1] = occupancyRow(t, request, c.Created, false)
			case "bad-digest":
				row[13] = "sha256:" + strings.Repeat("0", 64)
			case "bad-state":
				row[3] = "pending"
			case "duplicate":
				r.values[1] = first
			case "scan":
				r.scanErr = private
			case "end":
				r.endErr = private
			case "close":
				r.closeErr = private
			case "cancel-scan":
				r.beforeScan = cancel
			case "cancel-close":
				r.onClose = cancel
			}
			got, err := collectWorkOccupancy(ctx, r, x.request.ProjectID, []string{x.request.Trigger.TaskID})
			requireNoOccupancy(t, got, err)
			if !r.closed || strings.Contains(fmt.Sprintf("%+v", err), "occupancy-private") {
				t.Fatal("reader escaped Close or leaked row error")
			}
			if strings.HasPrefix(name, "cancel") && !errors.Is(err, context.Canceled) {
				t.Fatal("original cancellation was lost")
			}
		})
	}
}

type occupancyLaunchControl struct{ *launchControl }

func (p occupancyLaunchControl) ValidateLaunchInTx(ctx context.Context, tx f.Tx, actor i.Actor, request c.LaunchRequest, plan c.LaunchPlan) (c.LaunchPermit, error) {
	if err := p.store.RequireHeldLocks(ctx, tx, occupancyLocks(request.ProjectID)); err != nil {
		return c.LaunchPermit{}, err
	}
	return p.launchControl.ValidateLaunchInTx(ctx, tx, actor, request, plan)
}

func TestExecutionWorkOccupancyTaskLaunchKeepsScheduleExclusive(t *testing.T) {
	x := newLaunchControl(t)
	x.service.deps.Task = occupancyLaunchControl{x}
	value, err := x.service.Launch(context.Background(), x.actor, x.request)
	if err != nil || value.Execution.Status != c.Created || x.validations != 1 || x.store.writes != 1 {
		t.Fatal("actual Task Launch bypassed the same Schedule EX gate", err)
	}
}
