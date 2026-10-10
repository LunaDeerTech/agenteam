package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

func dispatchTestID[K any](t *testing.T, n int) f.ID[K] {
	t.Helper()
	id, err := f.ParseID[K](fmt.Sprintf("018f0000-0000-7000-8000-%012x", n))
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func dispatchTestRecord(t *testing.T, n int, status Status) *dispatchRecord {
	t.Helper()
	id := dispatchTestID[DispatchIdentity](t, n)
	p := dispatchTestID[i.Project](t, 100)
	a := dispatchTestID[i.Agent](t, 101)
	r := &dispatchRecord{id: id, project: p, agent: a, sprint: dispatchTestID[struct{}](t, 102).String(), task: dispatchTestID[struct{}](t, n+200).String(), status: status, outcome: NotSent, version: 1}
	r.launch = ec.LaunchRequest{ProjectID: p, AgentID: a, Trigger: ec.Trigger{Kind: "task", TaskID: r.task}, Purpose: "task/work", Policy: ec.Policy{SchemaVersion: 1, DeniedToolIDs: []i.ToolID{}, AllowedResourceConstraints: []json.RawMessage{}}, Lineage: ec.Lineage{DispatchID: id.String()}, Meta: f.CommandMeta{RequestID: dispatchTestID[f.Request](t, n+300), IdempotencyKey: launchKey(id)}}
	r.digest, _ = r.launch.Digest()
	r.createdAt, _ = f.ParseInstant("2026-10-10T00:00:00.000001Z")
	r.updatedAt = r.createdAt
	switch status {
	case Pending:
		r.outcome = Unknown
		r.attempts = 1
	case Launched:
		r.outcome = Created
		r.attempts = 1
		e := dispatchTestID[i.Execution](t, n+400)
		r.execution = &e
	case Failed, Skipped:
		r.outcome = KnownNotCreated
		r.attempts = 1
	}
	return r
}
func recordValues(t *testing.T, r *dispatchRecord) []any {
	t.Helper()
	raw, err := encodeLaunch(r.launch)
	if err != nil {
		t.Fatal(err)
	}
	var guard []byte
	var gs, gst, gp, execution *string
	if r.guard != nil {
		guard, err = json.Marshal(r.guard)
		if err != nil {
			t.Fatal(err)
		}
		gs, gst, gp = &r.guard.SourceSprintID, &r.guard.SourceState, &r.guard.SourcePriority
	}
	if r.execution != nil {
		s := r.execution.String()
		execution = &s
	}
	var retry *time.Time
	if r.nextRetry != nil {
		v := r.nextRetry.Time()
		retry = &v
	}
	var busy *int64
	var reason *string
	var skipped *time.Time
	if r.busyAttempt > 0 {
		v := r.busyAttempt
		busy = &v
	}
	if r.skipReason != "" {
		reason = &r.skipReason
	}
	if r.skippedAt != nil {
		v := r.skippedAt.Time()
		skipped = &v
	}
	return []any{r.id.String(), r.project.String(), r.sprint, r.task, r.agent.String(), raw, string(r.digest), string(r.launch.Meta.IdempotencyKey), r.launch.Meta.RequestID.String(), string(r.status), string(r.outcome), int64(r.version), guard, gs, gst, gp, execution, r.attempts, retry, r.createdAt.Time(), r.updatedAt.Time(), busy, reason, skipped}
}

type dispatchTestRow struct {
	values []any
	err    error
}

func (r dispatchTestRow) Scan(dst ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dst) != len(r.values) {
		return errors.New("test row columns")
	}
	for n, v := range r.values {
		d := reflect.ValueOf(dst[n]).Elem()
		if v == nil {
			d.SetZero()
		} else {
			d.Set(reflect.ValueOf(v))
		}
	}
	return nil
}

type dispatchTestRows struct {
	rows                 []dispatchTestRow
	at                   int
	closed               bool
	closeErr, errorAfter error
	afterScan            func()
}

func (r *dispatchTestRows) Next() bool { return r.at < len(r.rows) }
func (r *dispatchTestRows) Scan(dst ...any) error {
	v := r.rows[r.at]
	r.at++
	err := v.Scan(dst...)
	if r.afterScan != nil {
		r.afterScan()
	}
	return err
}
func (r *dispatchTestRows) Err() error {
	if r.closed && r.closeErr != nil {
		return r.closeErr
	}
	return r.errorAfter
}
func (r *dispatchTestRows) Close() { r.closed = true }

func TestSchedulerPendingReadsFourStatesAndUnknown(t *testing.T) {
	var input []dispatchTestRow
	var ids []string
	p := dispatchTestID[i.Project](t, 100)
	for n, status := range []Status{Pending, Launched, Failed, Skipped} {
		r := dispatchTestRecord(t, n+1, status)
		ids = append(ids, r.task)
		input = append(input, dispatchTestRow{values: recordValues(t, r)})
	}
	rows := &dispatchTestRows{rows: input}
	out, err := collectPending(context.Background(), rows, p, ids)
	if err != nil || !rows.closed || len(out.Pending) != 1 || len(out.HistoryTaskIDs) != 4 || out.Pending[0].TaskID != ids[0] {
		t.Fatal("original four-state history/unknown occupancy lost", err)
	}
	for n, id := range ids {
		if out.HistoryTaskIDs[n] != id {
			t.Fatal("history subset/order")
		}
	}
}

func TestSchedulerPendingRejectsPartialFactsAndJoinsRows(t *testing.T) {
	for _, mode := range []string{"foreign-project", "outside-task", "duplicate-pending", "bad-original-digest", "tail-error", "close-error", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			r := dispatchTestRecord(t, 1, Pending)
			values := recordValues(t, r)
			rows := &dispatchTestRows{rows: []dispatchTestRow{{values: values}}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "foreign-project":
				values[1] = dispatchTestID[i.Project](t, 999).String()
			case "outside-task":
				r.task = dispatchTestID[struct{}](t, 888).String()
				r.launch.Trigger.TaskID = r.task
				r.digest, _ = r.launch.Digest()
				rows.rows[0].values = recordValues(t, r)
			case "duplicate-pending":
				rows.rows = append(rows.rows, dispatchTestRow{values: recordValues(t, r)})
			case "bad-original-digest":
				values[6] = "sha256:" + strings.Repeat("0", 64)
			case "tail-error":
				rows.errorAfter = errors.New("private database canary")
			case "close-error":
				rows.closeErr = errors.New("private database canary")
			case "cancel":
				rows.afterScan = cancel
			}
			out, err := collectPending(ctx, rows, dispatchTestID[i.Project](t, 100), []string{dispatchTestID[struct{}](t, 201).String()})
			if err == nil || out.Pending != nil || out.HistoryTaskIDs != nil || !rows.closed {
				t.Fatal("partial facts accepted or rows unjoined")
			}
			if strings.Contains(err.Error(), "canary") {
				t.Fatal("private storage error leaked")
			}
		})
	}
}

type pendingTestStore struct {
	tx            f.Tx
	heldErr       error
	inErr         error
	queryErr      error
	exists        bool
	queries, held int
}

func (s *pendingTestStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if s.inErr != nil {
		return nil, s.inErr
	}
	if tx != s.tx || !tx.Valid() {
		return nil, fault(f.InvalidArgument)
	}
	return s, nil
}
func (s *pendingTestStore) WithinTx(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult {
	panic("reader must not begin")
}
func (s *pendingTestStore) AcquireAll(context.Context, f.Tx, []f.LockRequest) error {
	panic("reader must not acquire")
}
func (s *pendingTestStore) RequireHeldLocks(_ context.Context, _ f.Tx, locks []f.LockRequest) error {
	s.held++
	if len(locks) != 2 || locks[0].Mode != f.Shared || locks[1].Mode != f.Exclusive {
		return errors.New("missing original gates")
	}
	return s.heldErr
}
func (s *pendingTestStore) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	panic("reader must not write")
}
func (s *pendingTestStore) Query(context.Context, string, ...any) (*postgres.Rows, error) {
	s.queries++
	return nil, s.queryErr
}
func (s *pendingTestStore) QueryRow(context.Context, string, ...any) postgres.Row {
	s.queries++
	return dispatchTestRow{values: []any{s.exists}, err: s.queryErr}
}

func TestSchedulerPendingEmptyStillRequiresOriginalGates(t *testing.T) {
	p := dispatchTestID[i.Project](t, 100)
	for _, mode := range []string{"success", "foreign-tx", "missing-held", "nil-tasks", "query-failure"} {
		t.Run(mode, func(t *testing.T) {
			s := &pendingTestStore{tx: f.NewTx()}
			a, _ := NewPendingAuthority(s)
			tx := s.tx
			ids := []string{}
			switch mode {
			case "foreign-tx":
				tx = f.NewTx()
			case "missing-held":
				s.heldErr = fault(f.Forbidden)
			case "nil-tasks":
				ids = nil
			case "query-failure":
				ids = []string{dispatchTestID[struct{}](t, 201).String()}
				s.queryErr = errors.New("private query")
			}
			out, err := a.ReadInTx(context.Background(), tx, p, ids)
			if mode == "success" {
				if err != nil || out.Pending == nil || out.HistoryTaskIDs == nil || s.held != 1 || s.queries != 0 {
					t.Fatal("empty bypassed original transaction")
				}
			} else if err == nil || out.Pending != nil || out.HistoryTaskIDs != nil {
				t.Fatal("failed read returned facts")
			}
		})
	}
}

func TestSchedulerPendingProtectsPersistedSourceGroup(t *testing.T) {
	s := &pendingTestStore{tx: f.NewTx(), exists: true}
	a, _ := NewPendingAuthority(s)
	p := dispatchTestID[i.Project](t, 100)
	g := ec.PendingClaimGroup{SprintID: dispatchTestID[struct{}](t, 102).String(), State: "todo", Priority: "medium"}
	err := a.RequireNoPendingGroupsInTx(context.Background(), s.tx, p, []ec.PendingClaimGroup{g})
	var got *f.Fault
	if !errors.As(err, &got) || got.Code != f.ResourceBusy || s.queries != 1 {
		t.Fatal("persisted position not protected", err)
	}
	s.exists = false
	if err = a.RequireNoPendingGroupsInTx(context.Background(), s.tx, p, []ec.PendingClaimGroup{g}); err != nil {
		t.Fatal(err)
	}
	if err = a.RequireNoPendingGroupsInTx(context.Background(), s.tx, p, []ec.PendingClaimGroup{g, g}); err == nil {
		t.Fatal("duplicate groups accepted")
	}
	if err = a.RequireNoPendingGroupsInTx(context.Background(), f.NewTx(), p, []ec.PendingClaimGroup{}); err == nil {
		t.Fatal("empty groups bypassed same transaction")
	}
}

func TestSchedulerDispatchStrictOriginalInputAndSafeProjection(t *testing.T) {
	r := dispatchTestRecord(t, 1, Pending)
	r.guard = &ClaimGuard{TaskID: r.task, ClaimedVersion: 2, SourceState: "todo", SourceAssigneeID: r.agent, SourcePriority: "critical", SourceSprintID: r.sprint, SourceOrderGeneration: 1}
	copy, err := scanDispatch(dispatchTestRow{values: recordValues(t, r)})
	if err != nil || copy.guard == nil || copy.guard.SourcePriority != "critical" {
		t.Fatal("original claim roundtrip", err)
	}
	values := recordValues(t, r)
	values[15] = ptrString("low")
	if _, err = scanDispatch(dispatchTestRow{values: values}); err == nil {
		t.Fatal("source index disagreed with immutable guard")
	}
	canary := "private-constraint-canary"
	r.launch.Policy.AllowedResourceConstraints = []json.RawMessage{json.RawMessage(`{"secret":"` + canary + `"}`)}
	d := snapshot(r)
	raw, _ := json.Marshal(map[string]any{"nested": []Dispatch{d}})
	if strings.Contains(string(raw), canary) || strings.Contains(fmt.Sprintf("%+v", []Dispatch{d}), canary) {
		t.Fatal("implicit launch input leak")
	}
	original, err := d.LaunchRequest()
	if err != nil || !strings.Contains(string(original.Policy.AllowedResourceConstraints[0]), canary) {
		t.Fatal("explicit original input lost")
	}
	original.Policy.AllowedResourceConstraints[0][2] = 'X'
	again, _ := d.LaunchRequest()
	if string(again.Policy.AllowedResourceConstraints[0]) == string(original.Policy.AllowedResourceConstraints[0]) {
		t.Fatal("mutable launch alias")
	}
}
func ptrString(s string) *string { return &s }
