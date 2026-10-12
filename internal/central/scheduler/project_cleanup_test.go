package scheduler

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

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/scheduler/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

// These controls retain the actual owned pair, live transactions and original
// Store returns. Explicit Scheduler rows do not prove PostgreSQL commits,
// Project permission, Execution completion or foreign-process death.
type schedulerCleanupStore struct {
	*schedulerCallStore
	dataMu        sync.Mutex
	tables        map[string]map[i.ProjectID]map[string]json.RawMessage
	deletes       []string
	batches       [][]string
	deleteFailure error
	wrongCount    bool
	holdDelete    func(context.Context)
}

func (s *schedulerCleanupStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.schedulerCallStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}

type cleanupControlledRow struct {
	Key    string          `json:"key"`
	Record json.RawMessage `json:"record"`
}

var cleanupControlledTables = []string{"task_runtimes", "relaunch_visits", "dispatches", "project_calls"}

func (s *schedulerCleanupStore) rows(table string, p i.ProjectID) []cleanupControlledRow {
	if table == "project_calls" {
		s.schedulerCallStore.mu.Lock()
		defer s.schedulerCallStore.mu.Unlock()
		rows := []cleanupControlledRow{}
		for _, r := range s.schedulerCallStore.rows {
			if r.Project == p {
				raw, _ := json.Marshal(r)
				rows = append(rows, cleanupControlledRow{r.ID.String(), raw})
			}
		}
		slices.SortFunc(rows, func(a, b cleanupControlledRow) int { return strings.Compare(a.Key, b.Key) })
		return rows
	}
	s.dataMu.Lock()
	defer s.dataMu.Unlock()
	rows := []cleanupControlledRow{}
	for key, raw := range s.tables[table][p] {
		rows = append(rows, cleanupControlledRow{key, append(json.RawMessage(nil), raw...)})
	}
	slices.SortFunc(rows, func(a, b cleanupControlledRow) int { return strings.Compare(a.Key, b.Key) })
	return rows
}
func (s *schedulerCleanupStore) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	if strings.HasPrefix(query, "SELECT EXISTS(SELECT 1 FROM agenteam_scheduler.task_runtimes") {
		if len(args) != 1 {
			return dispatchTestRow{err: errors.New("invalid cleanup existence arguments")}
		}
		p, err := f.ParseID[i.Project](args[0].(string))
		if err != nil {
			return dispatchTestRow{err: err}
		}
		values := []any{}
		for _, table := range cleanupControlledTables {
			values = append(values, len(s.rows(table, p)) != 0)
		}
		return dispatchTestRow{values: values}
	}
	if strings.Contains(query, "jsonb_build_object('key',selected.key,'record',selected.record)") {
		if len(args) < 1 || len(args) > 2 || !strings.Contains(query, "LIMIT 100") {
			return dispatchTestRow{err: errors.New("unbounded cleanup read")}
		}
		p, err := f.ParseID[i.Project](args[0].(string))
		if err != nil {
			return dispatchTestRow{err: err}
		}
		table := ""
		for _, name := range cleanupControlledTables {
			if strings.Contains(query, "FROM agenteam_scheduler."+name+" t WHERE") {
				table = name
			}
		}
		if table == "" {
			return dispatchTestRow{err: errors.New("cross-owner cleanup read")}
		}
		rows := s.rows(table, p)
		if len(args) == 2 {
			keys, ok := args[1].([]string)
			if !ok {
				return dispatchTestRow{err: errors.New("invalid exact keys")}
			}
			selected := []cleanupControlledRow{}
			for _, row := range rows {
				if slices.Contains(keys, row.Key) {
					selected = append(selected, row)
				}
			}
			rows = selected
		}
		if len(rows) > 100 {
			rows = rows[:100]
		}
		raw, err := json.Marshal(rows)
		return dispatchTestRow{values: []any{raw}, err: err}
	}
	return s.schedulerCallStore.QueryRow(ctx, query, args...)
}
func (s *schedulerCleanupStore) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if !strings.HasPrefix(query, "DELETE FROM agenteam_scheduler.") {
		return s.schedulerCallStore.Exec(ctx, query, args...)
	}
	if len(args) != 2 || !strings.Contains(query, "jsonb_to_recordset($2::jsonb)") || !strings.Contains(query, "to_jsonb(t)=selected.record") {
		return pgconn.CommandTag{}, errors.New("cleanup lost exact original row guard")
	}
	table := ""
	for _, name := range cleanupControlledTables {
		if strings.HasPrefix(query, "DELETE FROM agenteam_scheduler."+name+" t ") {
			table = name
		}
	}
	if table == "" {
		return pgconn.CommandTag{}, errors.New("cross-owner cleanup delete")
	}
	p, err := f.ParseID[i.Project](args[0].(string))
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	var selected []cleanupControlledRow
	if err = json.Unmarshal([]byte(args[1].(string)), &selected); err != nil || len(selected) < 1 || len(selected) > 100 {
		return pgconn.CommandTag{}, errors.New("invalid bounded original batch")
	}
	s.dataMu.Lock()
	failure, wrong, hold := s.deleteFailure, s.wrongCount, s.holdDelete
	s.dataMu.Unlock()
	if failure != nil {
		return pgconn.CommandTag{}, failure
	}
	if wrong {
		return pgconn.NewCommandTag("DELETE 0"), nil
	}
	current := s.rows(table, p)
	for _, want := range selected {
		found := false
		for _, actual := range current {
			if want.Key == actual.Key && string(want.Record) == string(actual.Record) {
				found = true
			}
		}
		if !found {
			return pgconn.NewCommandTag("DELETE 0"), nil
		}
	}
	keys := []string{}
	if table == "project_calls" {
		if !strings.Contains(query, "t.phase='business_closed'") {
			return pgconn.CommandTag{}, errors.New("cleanup lost closed-call predicate")
		}
		s.schedulerCallStore.mu.Lock()
		for _, row := range selected {
			if s.schedulerCallStore.rows[row.Key].Phase != "business_closed" {
				s.schedulerCallStore.mu.Unlock()
				return pgconn.NewCommandTag("DELETE 0"), nil
			}
		}
		for _, row := range selected {
			delete(s.schedulerCallStore.rows, row.Key)
			keys = append(keys, row.Key)
		}
		s.schedulerCallStore.mu.Unlock()
	} else {
		s.dataMu.Lock()
		for _, row := range selected {
			delete(s.tables[table][p], row.Key)
			keys = append(keys, row.Key)
		}
		s.dataMu.Unlock()
	}
	s.dataMu.Lock()
	s.deletes = append(s.deletes, table)
	s.batches = append(s.batches, keys)
	s.dataMu.Unlock()
	if hold != nil {
		hold(ctx)
	}
	return pgconn.NewCommandTag(fmt.Sprintf("DELETE %d", len(selected))), nil
}
func (s *schedulerCleanupStore) writes() int {
	s.dataMu.Lock()
	defer s.dataMu.Unlock()
	return len(s.deletes)
}
func (s *schedulerCleanupStore) seed(t *testing.T, table string, p i.ProjectID, n int) {
	t.Helper()
	for k := 0; k < n; k++ {
		key := dispatchTestID[struct{}](t, 4000+k).String()
		field := "id"
		if table == "task_runtimes" {
			field = "task_id"
		}
		raw, err := json.Marshal(map[string]any{field: key, "project_id": p.String(), "version": 1, "control": "original-row"})
		if err != nil {
			t.Fatal(err)
		}
		s.dataMu.Lock()
		if s.tables[table] == nil {
			s.tables[table] = map[i.ProjectID]map[string]json.RawMessage{}
		}
		if s.tables[table][p] == nil {
			s.tables[table][p] = map[string]json.RawMessage{}
		}
		s.tables[table][p][key] = raw
		s.dataMu.Unlock()
	}
}

type schedulerCleanupLoanKey struct{}
type schedulerCleanupAuthority struct {
	store *schedulerCleanupStore
	actor i.Actor
	cause pc.LifecycleCause
	scope pc.ScopeRef
}

func (a *schedulerCleanupAuthority) RequireProjectCleanupInTx(ctx context.Context, tx f.Tx, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) error {
	if ctx == nil || ctx.Value(schedulerCleanupLoanKey{}) != a || !a.actor.Equal(actor) || a.cause != cause || !a.scope.Equal(scope) {
		return fault(f.Forbidden)
	}
	x, err := a.store.InTx(tx)
	if err != nil || x != a.store {
		return fault(f.Forbidden)
	}
	return a.store.RequireHeldLocks(ctx, tx, projectCallLocks(scope.ProjectID))
}

type schedulerCleanupFixture struct {
	base        *schedulerCallsFixture
	store       *schedulerCleanupStore
	authority   *schedulerCleanupAuthority
	participant pc.ProjectLifecycleParticipant
	ctx         context.Context
	scope       pc.ScopeRef
}

func newSchedulerCleanup(t *testing.T) *schedulerCleanupFixture {
	t.Helper()
	base := newSchedulerCalls(t)
	store := &schedulerCleanupStore{schedulerCallStore: base.store, tables: map[string]map[i.ProjectID]map[string]json.RawMessage{}}
	base.pair.state.store, base.pair.state.pending.store = store, store
	scope := pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: base.p}
	authority := &schedulerCleanupAuthority{store: store, actor: base.actor, cause: base.cause, scope: scope}
	participant, err := NewProjectCleanupParticipant(base.pair, authority)
	if err != nil {
		t.Fatal(err)
	}
	return &schedulerCleanupFixture{base: base, store: store, authority: authority, participant: participant, ctx: context.WithValue(context.Background(), schedulerCleanupLoanKey{}, authority), scope: scope}
}
func (v *schedulerCleanupFixture) cleanup(ctx context.Context, cp *pc.CleanupCheckpoint) (pc.CleanupReport, error) {
	return v.participant.Cleanup(ctx, v.base.actor, v.base.cause, v.scope, cp)
}

func requireSchedulerCleanup(t *testing.T, v *schedulerCleanupFixture, report pc.CleanupReport, err error, state pc.CleanupState, stage string) *pc.CleanupCheckpoint {
	t.Helper()
	if err != nil || !report.Matches(pc.SchedulerParticipant, v.base.cause, v.scope) || report.Details().State != state {
		t.Fatal("unexpected original cleanup report", err, report.Details().State)
	}
	cp := report.Details().Checkpoint
	if cp == nil || cp.Schema() != 1 || !cp.Matches(pc.SchedulerParticipant, v.base.cause, v.scope) {
		t.Fatal("cleanup omitted exact checkpoint")
	}
	var data struct {
		Project   string `json:"project_id"`
		Operation string `json:"operation_id"`
		Version   int64  `json:"project_version"`
		Stage     string `json:"stage"`
	}
	if json.Unmarshal(cp.Bytes(), &data) != nil || data.Project != v.base.p.String() || data.Operation != v.base.cause.OperationID.String() || data.Version != int64(v.base.cause.ProjectVersion) || data.Stage != stage {
		t.Fatal("checkpoint no longer describes the actual bounded progress")
	}
	return cp
}

func TestSchedulerProjectCleanupAuthorityAndInput(t *testing.T) {
	for _, name := range []string{"nil-pair", "nil-authority", "stop-family", "cancelled", "archive", "wrong-checkpoint"} {
		t.Run(name, func(t *testing.T) {
			v := newSchedulerCleanup(t)
			v.store.seed(t, "dispatches", v.base.p, 1)
			var err error
			switch name {
			case "nil-pair":
				_, err = NewProjectCleanupParticipant(nil, v.authority)
			case "nil-authority":
				var a *schedulerCleanupAuthority
				_, err = NewProjectCleanupParticipant(v.base.pair, a)
			case "stop-family":
				_, err = v.cleanup(context.WithValue(context.Background(), schedulerCallLoanKey{}, v.base.pair.state.authority), nil)
			case "cancelled":
				ctx, cancel := context.WithCancel(v.ctx)
				cancel()
				_, err = v.cleanup(ctx, nil)
			case "archive":
				cause := v.base.cause
				cause.Action = pc.Archive
				_, err = v.participant.Cleanup(v.ctx, v.base.actor, cause, v.scope, nil)
			case "wrong-checkpoint":
				cause := v.base.cause
				cause.OperationID = dispatchTestID[pc.Operation](t, 999)
				cp, e := sc.NewProjectCleanupCheckpoint(sc.ProjectCleanupProgress{ProjectID: v.base.p, OperationID: cause.OperationID, ProjectVersion: cause.ProjectVersion, ProcessID: v.base.pair.state.process, InstanceID: v.base.pair.state.instance.String(), Stage: "completed"}, cause, v.scope)
				if e != nil {
					t.Fatal(e)
				}
				_, err = v.cleanup(v.ctx, &cp)
			}
			if err == nil || v.store.writes() != 0 || len(v.store.rows("dispatches", v.base.p)) != 1 {
				t.Fatal("unbound or wrong-family cleanup changed rows", err)
			}
			v.base.pair.Stop()
		})
	}
}

func TestSchedulerProjectCleanupBoundedOrderAndProjectIsolation(t *testing.T) {
	v := newSchedulerCleanup(t)
	v.store.seed(t, "task_runtimes", v.base.p, 101)
	v.store.seed(t, "relaunch_visits", v.base.p, 1)
	v.store.seed(t, "dispatches", v.base.p, 1)
	for _, table := range cleanupControlledTables[:3] {
		v.store.seed(t, table, v.base.b, 1)
	}
	_, entry := v.base.admit(t, v.base.p, "visit_next")
	v.base.close(t, entry)
	_, b := v.base.admit(t, v.base.b, "visit_next")
	defer func() { v.base.close(t, b); v.base.pair.Stop() }()
	var cp *pc.CleanupCheckpoint
	stages := []string{"runtimes", "relaunch_visits", "dispatches", "calls", "completed"}
	for n, stage := range stages {
		report, err := v.cleanup(v.ctx, cp)
		state := pc.CleanupPending
		if n == len(stages)-1 {
			state = pc.CleanupCompleted
		}
		cp = requireSchedulerCleanup(t, v, report, err, state, stage)
		if v.store.writes() != n+1 {
			t.Fatal("one explicit call executed multiple batches")
		}
		for _, table := range cleanupControlledTables[:3] {
			if len(v.store.rows(table, v.base.b)) != 1 {
				t.Fatal("A cleanup changed B", table)
			}
		}
	}
	v.store.dataMu.Lock()
	tables := slices.Clone(v.store.deletes)
	batches := slices.Clone(v.store.batches)
	v.store.dataMu.Unlock()
	if !slices.Equal(tables, []string{"task_runtimes", "task_runtimes", "relaunch_visits", "dispatches", "project_calls"}) {
		t.Fatal("cleanup skipped a domain dependency", tables)
	}
	for n, batch := range batches {
		want := 1
		if n == 0 {
			want = 100
		}
		if len(batch) != want {
			t.Fatal("wrong exact bound", n, len(batch))
		}
	}
	if len(v.store.rows("project_calls", v.base.b)) != 1 {
		t.Fatal("A cleanup retired B's original running call")
	}
	// Unsupported SQL to Work/Execution is rejected by this fixture; this
	// narrow control does not claim their real canonical data was queried.
}

func TestSchedulerProjectCleanupWaitsForSevenOriginalLanes(t *testing.T) {
	for _, lane := range []projectCallLane{projectLaneCoordinator, projectLaneVisitor, projectLaneHandoff, projectLaneBusy, projectLaneFailure, projectLaneRelaunch, projectLaneRunner} {
		t.Run(string(lane), func(t *testing.T) {
			v := newSchedulerCleanup(t)
			v.store.seed(t, "dispatches", v.base.p, 1)
			identity, err := projectEntryIdentity(v.base.p, "controlled_original", "controlled-lane")
			if err != nil {
				t.Fatal(err)
			}
			_, entry, err := v.base.pair.state.admit(context.Background(), v.base.p, lane, identity)
			if err != nil {
				t.Fatal(err)
			}
			report, err := v.cleanup(v.ctx, nil)
			requireSchedulerCleanup(t, v, report, err, pc.CleanupPending, "runtimes")
			if v.store.writes() != 0 {
				t.Fatal("cleanup passed a running original lane")
			}
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			v.base.store.after = func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
				if cause.Details().Owner == "scheduler-call-return" && cause.Details().CheckpointRef == v.base.p.String() {
					close(entered)
					<-release
				}
				return result
			}
			done := make(chan error, 1)
			collected := false
			t.Cleanup(func() {
				unblock()
				if !collected {
					select {
					case <-done:
					case <-time.After(5 * time.Second):
						t.Error("original lane return did not finish")
					}
				}
				v.base.pair.Stop()
			})
			go func() { err := v.base.pair.state.finish(entry, nil); v.base.pair.state.returned(entry); done <- err }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("original lane checkpoint not reached")
			}
			report, err = v.cleanup(v.ctx, nil)
			requireSchedulerCleanup(t, v, report, err, pc.CleanupPending, "runtimes")
			if v.store.writes() != 0 {
				t.Fatal("physical closed row substituted original return")
			}
			if _, err = v.base.pair.Visitor().VisitNext(context.Background(), v.base.b, nil); err != nil {
				t.Fatal("A held lane blocked B", err)
			}
			unblock()
			select {
			case err = <-done:
				collected = true
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("original checkpoint did not return")
			}
			v.base.store.after = nil
			report, err = v.cleanup(v.ctx, nil)
			cp := requireSchedulerCleanup(t, v, report, err, pc.CleanupPending, "calls")
			report, err = v.cleanup(v.ctx, cp)
			requireSchedulerCleanup(t, v, report, err, pc.CleanupCompleted, "completed")
		})
	}
}

func TestSchedulerProjectCleanupOriginalReturnFencesLateCalls(t *testing.T) {
	v := newSchedulerCleanup(t)
	v.store.seed(t, "dispatches", v.base.p, 1)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	v.base.store.after = func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
		if cause.Details().Owner == "scheduler-project-cleanup" && v.store.writes() == 1 {
			close(entered)
			<-release
		}
		return result
	}
	ctx, cancel := context.WithCancel(v.ctx)
	defer cancel()
	type answer struct {
		r pc.CleanupReport
		e error
	}
	done := make(chan answer, 1)
	collected := false
	t.Cleanup(func() {
		unblock()
		cancel()
		if !collected {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("original cleanup did not return")
			}
		}
		v.base.pair.Stop()
	})
	go func() { r, e := v.cleanup(ctx, nil); done <- answer{r, e} }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("physical cleanup commit not reached")
	}
	cancel()
	copy, err := NewProjectCleanupParticipant(v.base.pair, v.authority)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = copy.Cleanup(v.ctx, v.base.actor, v.base.cause, v.scope, nil); !runnerHasCode(err, f.ResourceBusy) {
		t.Fatal("another adapter bypassed original cleanup return", err)
	}
	for _, request := range []bool{false, true} {
		if _, err = v.base.stop(context.Background(), request); !runnerHasCode(err, f.ResourceBusy) {
			t.Fatal("late Stop crossed an active cleanup", err)
		}
	}
	if _, err = v.base.pair.Visitor().VisitNext(context.Background(), v.base.p, nil); !runnerHasCode(err, f.ResourceBusy) {
		t.Fatal("late original call crossed cleanup", err)
	}
	if _, err = v.base.pair.Visitor().VisitNext(context.Background(), v.base.b, nil); err != nil {
		t.Fatal("A cleanup globally blocked B", err)
	}
	select {
	case <-done:
		collected = true
		t.Fatal("cancel substituted original cleanup return")
	default:
	}
	unblock()
	select {
	case result := <-done:
		collected = true
		if result.e == nil || result.r.Details().State == pc.CleanupCompleted {
			t.Fatal("cancelled original return published completed", result.e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cleanup did not return")
	}
	v.base.store.after = nil
	report, err := v.cleanup(v.ctx, nil)
	requireSchedulerCleanup(t, v, report, err, pc.CleanupCompleted, "completed")
}

func requireCleanupUnknown(t *testing.T, report pc.CleanupReport, err error, original f.CommitResult) {
	t.Helper()
	result, ok := UnknownAttempt(err)
	if !ok || result.AttemptID() != original.AttemptID() || !schedulerSameRecoveryCause(result.Cause(), original.Cause()) || report.Details().Checkpoint != nil {
		t.Fatal("cleanup lost the first physical attempt or published progress", err)
	}
}

func TestSchedulerProjectCleanupUnknownConfirmsOnlyOriginalBatch(t *testing.T) {
	for _, applied := range []bool{true, false} {
		name := "applied"
		if !applied {
			name = "not-applied"
		}
		t.Run(name, func(t *testing.T) {
			v := newSchedulerCleanup(t)
			v.store.seed(t, "task_runtimes", v.base.p, 2)
			v.store.seed(t, "dispatches", v.base.p, 1)
			before := v.store.rows("task_runtimes", v.base.p)
			var first f.CommitResult
			masked := false
			v.base.store.after = func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
				if cause.Details().Owner == "scheduler-project-cleanup" && v.store.writes() == 1 && !masked && result.State() == f.Committed {
					masked = true
					// This explicitly controls the two physical outcomes; the in-memory
					// executor does not pretend to implement SQL rollback.
					if !applied {
						v.store.dataMu.Lock()
						for _, row := range before {
							v.store.tables["task_runtimes"][v.base.p][row.Key] = slices.Clone(row.Record)
						}
						v.store.dataMu.Unlock()
					}
					first = f.UnknownResult(dispatchTestID[f.TransactionAttempt](t, 980), cause)
					return first
				}
				return result
			}
			report, err := v.cleanup(v.ctx, nil)
			requireCleanupUnknown(t, report, err, first)
			v.base.store.after = nil
			// The original pair, not a particular adapter, owns the retained batch.
			copy, err := NewProjectCleanupParticipant(v.base.pair, v.authority)
			if err != nil {
				t.Fatal(err)
			}
			v.participant = copy
			t.Cleanup(func() {
				v.base.store.after = nil
				// Retire through the real confirmation path after every held call
				// has returned; never delete a retained owner from the test map.
				_, _ = v.cleanup(v.ctx, nil)
				v.base.pair.Stop()
			})
			report, err = v.cleanup(context.Background(), nil)
			requireCleanupUnknown(t, report, err, first)
			if v.store.writes() != 1 {
				t.Fatal("wrong-family confirmation wrote another batch")
			}
			v.base.store.after = func(_ context.Context, cause f.TransactionCause, _ f.CommitResult) f.CommitResult {
				return f.UnknownResult(dispatchTestID[f.TransactionAttempt](t, 981), cause)
			}
			report, err = v.cleanup(v.ctx, nil)
			requireCleanupUnknown(t, report, err, first)
			v.base.store.after = nil
			// Mixed presence or changed original bytes cannot confirm an atomic
			// batch. Restore only this explicit controlled input for the next call.
			v.store.dataMu.Lock()
			if applied {
				v.store.tables["task_runtimes"][v.base.p][before[0].Key] = slices.Clone(before[0].Record)
			} else {
				v.store.tables["task_runtimes"][v.base.p][before[0].Key] = json.RawMessage(strings.Replace(string(before[0].Record), "original-row", "changed-row", 1))
			}
			v.store.dataMu.Unlock()
			report, err = v.cleanup(v.ctx, nil)
			requireCleanupUnknown(t, report, err, first)
			v.store.dataMu.Lock()
			if applied {
				delete(v.store.tables["task_runtimes"][v.base.p], before[0].Key)
			} else {
				v.store.tables["task_runtimes"][v.base.p][before[0].Key] = slices.Clone(before[0].Record)
			}
			v.store.dataMu.Unlock()
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			v.base.store.after = func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
				if !schedulerSameRecoveryCause(cause, first.Cause()) {
					t.Error("confirmation changed the original transaction cause")
				}
				close(entered)
				<-release
				return result
			}
			ctx, cancel := context.WithCancel(v.ctx)
			type answer struct {
				report pc.CleanupReport
				err    error
			}
			done := make(chan answer, 1)
			collected := false
			t.Cleanup(func() {
				unblock()
				cancel()
				if !collected {
					select {
					case <-done:
					case <-time.After(5 * time.Second):
						t.Error("original readonly confirmation did not return")
					}
				}
			})
			go func() { r, e := v.cleanup(ctx, nil); done <- answer{r, e} }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("readonly original return not reached")
			}
			cancel()
			if _, err = copy.Cleanup(v.ctx, v.base.actor, v.base.cause, v.scope, nil); !runnerHasCode(err, f.ResourceBusy) {
				t.Fatal("cancel released the retained confirmation", err)
			}
			if v.store.writes() != 1 {
				t.Fatal("readonly confirmation sent another DELETE")
			}
			select {
			case <-done:
				collected = true
				t.Fatal("cancel substituted actual confirmation return")
			default:
			}
			unblock()
			select {
			case out := <-done:
				collected = true
				requireCleanupUnknown(t, out.report, out.err, first)
			case <-time.After(5 * time.Second):
				t.Fatal("confirmation did not return")
			}
			v.base.store.after = nil
			report, err = v.cleanup(v.ctx, nil)
			cp := requireSchedulerCleanup(t, v, report, err, pc.CleanupPending, "runtimes")
			if v.store.writes() != 1 {
				t.Fatal("successful readonly confirmation applied the next batch")
			}
			report, err = v.cleanup(v.ctx, cp)
			stage, state := "dispatches", pc.CleanupPending
			if applied {
				stage, state = "completed", pc.CleanupCompleted
			}
			requireSchedulerCleanup(t, v, report, err, state, stage)
			if v.store.writes() != 2 {
				t.Fatal("next explicit call did not perform exactly one batch")
			}
		})
	}
}

func TestSchedulerProjectCleanupForeignFactsAndCheckpoint(t *testing.T) {
	for _, name := range []string{"same-process-other-instance", "foreign-alive", "preflight-unknown-keeps-identity", "proof-after-return-and-recheck"} {
		t.Run(name, func(t *testing.T) {
			v := newSchedulerCleanup(t)
			defer v.base.pair.Stop()
			v.store.seed(t, "dispatches", v.base.p, 1)
			progress := sc.ProjectCleanupProgress{ProjectID: v.base.p, OperationID: v.base.cause.OperationID, ProjectVersion: v.base.cause.ProjectVersion, ProcessID: v.base.pair.state.process, InstanceID: dispatchTestID[projectCallInstance](t, 919).String(), Stage: "completed"}
			if name != "same-process-other-instance" {
				progress.ProcessID = dispatchTestID[oc.Process](t, 918)
			}
			cp, err := sc.NewProjectCleanupCheckpoint(progress, v.base.cause, v.scope)
			if err != nil {
				t.Fatal(err)
			}
			if name != "proof-after-return-and-recheck" {
				v.base.proof.confirm = func(context.Context, oc.ProcessID) error { return fault(f.ResourceBusy) }
				if name == "preflight-unknown-keeps-identity" {
					var first f.CommitResult
					v.base.store.after = func(_ context.Context, cause f.TransactionCause, _ f.CommitResult) f.CommitResult {
						first = f.UnknownResult(dispatchTestID[f.TransactionAttempt](t, 916), cause)
						return first
					}
					report, err := v.cleanup(v.ctx, &cp)
					requireCleanupUnknown(t, report, err, first)
					v.base.store.after = nil
					// The first iteration below confirms only the original preflight;
					// the second must still obtain the missing foreign proof.
				}
				// Reusing the returned Pending checkpoint cannot launder the old
				// instance/process into the current pair on a second call.
				for n := 0; n < 2; n++ {
					report, err := v.cleanup(v.ctx, &cp)
					next := requireSchedulerCleanup(t, v, report, err, pc.CleanupPending, "runtimes")
					var actual sc.ProjectCleanupProgress
					if json.Unmarshal(next.Bytes(), &actual) != nil || actual.ProcessID != progress.ProcessID || actual.InstanceID != progress.InstanceID || v.store.writes() != 0 {
						t.Fatal("Pending laundered an unconfirmed owner")
					}
					cp = *next
				}
				return
			}
			_, entry := v.base.admit(t, v.base.p, "visit_next")
			v.base.close(t, entry)
			v.base.store.mu.Lock()
			row := v.base.store.rows[entry.record.ID.String()]
			row.Process = progress.ProcessID
			v.base.store.rows[row.ID.String()] = row
			v.base.store.mu.Unlock()
			entered, release := make(chan struct{}), make(chan struct{})
			var once, holdOnce sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			v.base.store.after = func(_ context.Context, cause f.TransactionCause, r f.CommitResult) f.CommitResult {
				if cause.Details().Owner == "scheduler-project-cleanup" {
					holdOnce.Do(func() { close(entered); <-release })
				}
				return r
			}
			v.base.proof.confirm = func(_ context.Context, process oc.ProcessID) error {
				if process != progress.ProcessID {
					t.Error("foreign proof changed identity")
				}
				// An identity changing after external proof must be caught by the
				// mutation's complete ledger re-read before any DELETE.
				v.base.store.mu.Lock()
				changed := row
				changed.Instance = dispatchTestID[projectCallInstance](t, 917)
				v.base.store.rows[row.ID.String()] = changed
				v.base.store.mu.Unlock()
				return nil
			}
			done := make(chan error, 1)
			collected := false
			t.Cleanup(func() {
				unblock()
				if !collected {
					select {
					case <-done:
					case <-time.After(5 * time.Second):
						t.Error("original preflight did not return")
					}
				}
			})
			go func() { _, err := v.cleanup(v.ctx, &cp); done <- err }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("preflight return not reached")
			}
			v.base.proof.mu.Lock()
			confirms := v.base.proof.confirms
			v.base.proof.mu.Unlock()
			if confirms != 0 || v.store.writes() != 0 {
				t.Fatal("foreign proof preceded original Store return")
			}
			// Limit the held hook to the original preflight using a one-shot
			// wrapper, rather than mutating it concurrently with the Store call.
			unblock()
			select {
			case err = <-done:
				collected = true
			case <-time.After(5 * time.Second):
				t.Fatal("foreign recheck did not return")
			}
			if !runnerHasCode(err, f.ResourceBusy) || v.store.writes() != 0 {
				t.Fatal("changed full snapshot passed cleanup", err)
			}
		})
	}
}

func TestSchedulerProjectCleanupDeleteErrorsAndOriginalReturn(t *testing.T) {
	for _, name := range []string{"original-error", "wrong-count", "held-delete"} {
		t.Run(name, func(t *testing.T) {
			v := newSchedulerCleanup(t)
			v.store.seed(t, "dispatches", v.base.p, 1)
			original := errors.New("controlled original delete failure")
			if name != "held-delete" {
				if name == "original-error" {
					v.store.deleteFailure = original
				} else {
					v.store.wrongCount = true
				}
				report, err := v.cleanup(v.ctx, nil)
				if err == nil || report.Details().Checkpoint != nil || len(v.store.rows("dispatches", v.base.p)) != 1 {
					t.Fatal("failed exact delete published progress", err)
				}
				if name == "original-error" && !errors.Is(err, original) {
					t.Fatal("original SQL error lost", err)
				}
				if name == "wrong-count" && !runnerHasCode(err, f.VersionConflict) {
					t.Fatal("RowsAffected mismatch accepted", err)
				}
				v.base.pair.Stop()
				return
			}
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			v.store.holdDelete = func(context.Context) { close(entered); <-release }
			ctx, cancel := context.WithCancel(v.ctx)
			done := make(chan error, 1)
			collected := false
			t.Cleanup(func() {
				unblock()
				cancel()
				if !collected {
					select {
					case <-done:
					case <-time.After(5 * time.Second):
						t.Error("original DELETE did not return")
					}
				}
				v.base.pair.Stop()
			})
			go func() { _, err := v.cleanup(ctx, nil); done <- err }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("original DELETE not reached")
			}
			cancel()
			if _, err := v.cleanup(v.ctx, nil); !runnerHasCode(err, f.ResourceBusy) {
				t.Fatal("cancel replaced original SQL return", err)
			}
			select {
			case <-done:
				collected = true
				t.Fatal("returned before original DELETE")
			default:
			}
			unblock()
			select {
			case err := <-done:
				collected = true
				if !errors.Is(err, context.Canceled) {
					t.Fatal("original cancellation lost", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("DELETE return not observed")
			}
		})
	}
}
