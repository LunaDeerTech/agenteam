package project

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
)

// These controls use explicit SQL-boundary doubles. They do not establish
// physical COMMIT, guard death, or a successful canonical lifecycle transition.
type stopRecoveryStore struct {
	Store
	query func(context.Context, string, ...any) (*postgres.Rows, error)
	claim func(context.Context, string, ...any) postgres.Row
	tx    func(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult
}

func (s *stopRecoveryStore) Query(ctx context.Context, sql string, args ...any) (*postgres.Rows, error) {
	return s.query(ctx, sql, args...)
}
func (s *stopRecoveryStore) QueryRow(ctx context.Context, sql string, args ...any) postgres.Row {
	if s.claim != nil {
		return s.claim(ctx, sql, args...)
	}
	return rowFunc(func(...any) error { return pgx.ErrNoRows })
}
func (s *stopRecoveryStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	return s.tx(ctx, cause, fn)
}
func stopRecoveryForTest(t *testing.T, store Store, process *testProcess) *LifecycleStopRecovery {
	t.Helper()
	authority, err := NewLifecycleAuthority(store, registryTestManifest(t, registryTestEntries()), nil)
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewLifecycleStopRecovery(store, authority, process, func(context.Context, identity.Actor, c.LifecycleCause, c.ScopeRef) error {
		t.Error("unconfirmed SQL-boundary control called provider")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func stopRecoveryWork(t *testing.T, count int) []lifecycleStopCandidate {
	t.Helper()
	work := make([]lifecycleStopCandidate, count)
	for i := range work {
		work[i] = lifecycleStopCandidate{operation: testID[c.Operation](t), project: testID[identity.Project](t)}
	}
	slices.SortFunc(work, func(a, b lifecycleStopCandidate) int {
		return strings.Compare(a.operation.String(), b.operation.String())
	})
	return work
}

func TestLifecycleStopRecoveryConstructionAndBounds(t *testing.T) {
	store := &stopRecoveryStore{}
	process := &testProcess{id: testID[oc.Process](t)}
	r := stopRecoveryForTest(t, store, process)
	original := process.id
	process.id = testID[oc.Process](t)
	if r.state.driver.state.process != original {
		t.Fatal("constructor did not retain original process identity")
	}
	var nilStore *stopRecoveryStore
	for _, other := range []Store{nil, nilStore, &stopRecoveryStore{}} {
		_, err := NewLifecycleStopRecovery(other, r.state.driver.state.authority, process, r.state.driver.state.step)
		hasCode(t, err, f.DependencyUnbound)
	}
	_, err := NewLifecycleStopRecovery(store, r.state.driver.state.authority, process, nil)
	hasCode(t, err, f.DependencyUnbound)
	var zero c.OperationID
	for _, input := range []struct {
		ctx   context.Context
		after *c.OperationID
		limit int
	}{{nil, nil, 1}, {context.Background(), nil, 0}, {context.Background(), nil, 5}, {context.Background(), &zero, 1}} {
		_, err = r.RunBatch(input.ctx, input.after, input.limit)
		hasCode(t, err, f.InvalidArgument)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = r.RunBatch(canceled, nil, 1); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled batch entered SQL")
	}
	var empty *LifecycleStopRecovery
	_, err = empty.RunBatch(context.Background(), nil, 1)
	hasCode(t, err, f.DependencyUnbound)
	empty.Stop()
	if err = empty.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	hasCode(t, r.Drain(nil), f.InvalidArgument)
}

type stopRecoveryRows struct {
	values         [][2]string
	position       int
	scanErr, final error
	closed         int
}

func (r *stopRecoveryRows) Next() bool { r.position++; return r.position <= len(r.values) }
func (r *stopRecoveryRows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	return valuesRow(r.values[r.position-1][0], r.values[r.position-1][1]).Scan(dest...)
}
func (r *stopRecoveryRows) Err() error { return r.final }
func (r *stopRecoveryRows) Close()     { r.closed++ }
func stopRecoveryRowValues(work []lifecycleStopCandidate) [][2]string {
	rows := make([][2]string, len(work))
	for i, item := range work {
		rows[i] = [2]string{item.operation.String(), item.project.String()}
	}
	return rows
}

func TestLifecycleStopRecoveryPageBoundaries(t *testing.T) {
	work := stopRecoveryWork(t, 4)
	for _, tc := range []struct {
		name  string
		rows  []lifecycleStopCandidate
		after *c.OperationID
		limit int
		want  int
		more  bool
	}{{"empty", nil, nil, 1, 0, false}, {"end", work[:2], nil, 2, 2, false}, {"lookahead", work[1:], &work[0].operation, 2, 2, true}} {
		t.Run(tc.name, func(t *testing.T) {
			rows := &stopRecoveryRows{values: stopRecoveryRowValues(tc.rows)}
			got, more, err := readLifecycleStopBatch(rows, tc.after, tc.limit)
			if err != nil || len(got) != tc.want || more != tc.more || rows.closed != 1 || !slices.Equal(got, tc.rows[:tc.want]) {
				t.Fatal("bounded page or actual rows close changed", err)
			}
		})
	}
}

func TestLifecycleStopRecoveryRejectsCorruptDiscovery(t *testing.T) {
	work := stopRecoveryWork(t, 4)
	valid := stopRecoveryRowValues(work)
	private := errors.New("private-discovery-material")
	for _, tc := range []struct {
		name  string
		rows  *stopRecoveryRows
		after *c.OperationID
	}{{"operation", &stopRecoveryRows{values: [][2]string{{"invalid", valid[0][1]}}}, nil},
		{"project", &stopRecoveryRows{values: [][2]string{{valid[0][0], "invalid"}}}, nil},
		{"duplicate-operation", &stopRecoveryRows{values: [][2]string{valid[0], valid[0]}}, nil},
		{"reverse", &stopRecoveryRows{values: [][2]string{valid[1], valid[0]}}, nil},
		{"cursor", &stopRecoveryRows{values: valid[:1]}, &work[0].operation},
		{"duplicate-project", &stopRecoveryRows{values: [][2]string{valid[0], {valid[1][0], valid[0][1]}}}, nil},
		{"over-bound", &stopRecoveryRows{values: valid}, nil},
		{"scan-error", &stopRecoveryRows{values: valid[:1], scanErr: private}, nil},
		{"rows-error", &stopRecoveryRows{values: valid[:1], final: private}, nil}} {
		t.Run(tc.name, func(t *testing.T) {
			got, more, err := readLifecycleStopBatch(tc.rows, tc.after, 2)
			if err == nil || len(got) != 0 || more || tc.rows.closed != 1 || strings.Contains(err.Error(), private.Error()) {
				t.Fatal("invalid discovery became visitable or exposed dependency material")
			}
			if (tc.rows.scanErr != nil || tc.rows.final != nil) && !errors.Is(err, private) {
				t.Fatal("dependency cause lost")
			}
		})
	}
}

func TestLifecycleStopRecoveryDiscoveryAdmissionAndActualReturn(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	private := errors.New("private-held-discovery-material")
	var observed context.Context
	cursor := testID[c.Operation](t)
	store := &stopRecoveryStore{query: func(ctx context.Context, _ string, args ...any) (*postgres.Rows, error) {
		observed = ctx
		if len(args) != 2 || args[0] != cursor.String() || args[1] != 5 {
			t.Error("discovery cursor/lookahead bound changed")
		}
		close(entered)
		<-release
		return nil, private
	}}
	r := stopRecoveryForTest(t, store, &testProcess{id: testID[oc.Process](t)})
	returned := make(chan error, 1)
	go func() { _, err := r.RunBatch(context.Background(), &cursor, 4); returned <- err }()
	<-entered
	_, err := r.RunBatch(context.Background(), nil, 1)
	hasCode(t, err, f.ResourceBusy)
	r.Stop()
	if observed.Err() != context.Canceled {
		t.Error("Stop did not cancel original discovery")
	}
	_, err = r.RunBatch(context.Background(), nil, 1)
	hasCode(t, err, f.ShuttingDown)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if r.Drain(ctx) == nil {
		t.Error("canceled SQL replaced its actual return")
	}
	close(release)
	err = <-returned
	if !errors.Is(err, private) || strings.Contains(err.Error(), private.Error()) {
		t.Fatal("discovery error lost safe provenance")
	}
	if err = r.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestLifecycleStopRecoveryBusyDoesNotStarveUnknownOrLaterWork(t *testing.T) {
	work := stopRecoveryWork(t, 3)
	process := &testProcess{id: testID[oc.Process](t)}
	attempt := testID[struct{}](t).String()
	job, _ := f.NewJobCause("project-lifecycle", work[1].operation.String(), testID[struct{}](t).String())
	physical := f.UnknownResult(testID[f.TransactionAttempt](t), job)
	private := errors.New("private-later-round-material")
	reads, writes := 0, 0
	store := &stopRecoveryStore{}
	store.claim = func(_ context.Context, _ string, args ...any) postgres.Row {
		reads++
		if args[0] == work[0].operation.String() {
			return valuesRow(work[0].project.String(), process.id.String(), attempt, int64(1), "running")
		}
		return rowFunc(func(...any) error { return pgx.ErrNoRows })
	}
	store.tx = func(_ context.Context, cause f.TransactionCause, _ func(context.Context, f.Tx) error) f.CommitResult {
		writes++
		if cause.Details().JobID == work[1].operation.String() {
			return physical
		}
		return f.NotCommittedResult(fault(f.DependencyUnavailable).WithCause(private))
	}
	r := stopRecoveryForTest(t, store, process)
	page, err := r.state.visit(context.Background(), work, true)
	saved, ok := UnknownAttempt(err)
	if page.Visited != 3 || page.Pending != 3 || page.Next == nil || *page.Next != work[2].operation || reads != 3 || writes != 2 || !ok || saved.AttemptID() != physical.AttemptID() || !errors.Is(err, private) || strings.Contains(err.Error(), private.Error()) {
		t.Fatal("Busy starved a later item, cursor advanced wrongly, or physical failure was lost")
	}
	hasCode(t, err, f.CommitUnknown)
	if err = r.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	// A completed scan still reports pending operations, not completion. Its
	// nil cursor tells the caller to start a later cycle at nil, including Busy.
	store.claim = func(_ context.Context, _ string, args ...any) postgres.Row {
		for _, item := range work {
			if args[0] == item.operation.String() {
				return valuesRow(item.project.String(), process.id.String(), attempt, int64(9), "running")
			}
		}
		return rowFunc(func(...any) error { return errors.New("unexpected operation") })
	}
	page, err = r.state.visit(context.Background(), work, false)
	if err != nil || page.Visited != 3 || page.Pending != 3 || page.Next != nil || writes != 2 {
		t.Fatal("Busy-only cycle changed completion semantics", err)
	}
}

func TestLifecycleStopRecoveryCancellationJoinsOriginalRun(t *testing.T) {
	work := stopRecoveryWork(t, 2)
	entered, release := make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writes := 0
	store := &stopRecoveryStore{tx: func(original context.Context, _ f.TransactionCause, _ func(context.Context, f.Tx) error) f.CommitResult {
		writes++
		close(entered)
		<-original.Done()
		<-release
		return f.NotCommittedResult(fault(f.InvalidState))
	}}
	r := stopRecoveryForTest(t, store, &testProcess{id: testID[oc.Process](t)})
	type outcome struct {
		page LifecycleStopBatch
		err  error
	}
	returned := make(chan outcome, 1)
	go func() { page, err := r.state.visit(ctx, work, false); returned <- outcome{page, err} }()
	<-entered
	cancel()
	r.Stop()
	drainCtx, end := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer end()
	if r.Drain(drainCtx) == nil {
		t.Error("batch Drain skipped its original driver call")
	}
	select {
	case <-returned:
		t.Error("cancellation fabricated original transaction return")
	default:
	}
	close(release)
	got := <-returned
	if !errors.Is(got.err, context.Canceled) || got.page.Visited != 1 || got.page.Pending != 1 || got.page.Next == nil || *got.page.Next != work[0].operation || writes != 1 {
		t.Fatal("cancellation started later work or lost visited boundary")
	}
	if err := r.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestLifecycleStopRecoveryKeepsFirstPhysicalUnknown(t *testing.T) {
	job, _ := f.NewJobCause("project-lifecycle", testID[c.Operation](t).String(), testID[struct{}](t).String())
	first := f.UnknownResult(testID[f.TransactionAttempt](t), job)
	second := f.UnknownResult(testID[f.TransactionAttempt](t), job)
	private := errors.New("private-batch-failure-material")
	err := lifecycleStopBatchError(unavailable(private), commitError(first))
	err = lifecycleStopBatchError(err, commitError(second))
	err = lifecycleStopBatchError(err, portError(context.Canceled))
	saved, ok := UnknownAttempt(err)
	hasCode(t, err, f.CommitUnknown)
	if !ok || saved.AttemptID() != first.AttemptID() || !errors.Is(err, private) || !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), private.Error()) {
		t.Fatal("batch changed first physical attempt or exposed a raw dependency error")
	}
}
