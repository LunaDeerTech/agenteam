package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// These controls verify the read port's output and authority boundaries. Real
// same-Store ownership and SQL execution require the caller's PostgreSQL suite.
type metadataStore struct {
	tx         f.Tx
	executor   postgres.SQLExecutor
	held       []f.LockRequest
	requested  []f.LockRequest
	trace      []string
	closed     bool
	authorized bool
}

func (*metadataStore) WithinTx(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult {
	panic("execution metadata opened a transaction")
}
func (*metadataStore) AcquireAll(context.Context, f.Tx, []f.LockRequest) error {
	panic("execution metadata acquired locks")
}
func (s *metadataStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	s.trace = append(s.trace, "transaction")
	if !tx.Valid() || tx != s.tx || s.closed {
		return nil, fault(f.InvalidArgument)
	}
	return s.executor, nil
}
func (s *metadataStore) RequireHeldLocks(_ context.Context, tx f.Tx, requests []f.LockRequest) error {
	s.trace = append(s.trace, "locks")
	s.requested = slices.Clone(requests)
	if tx != s.tx || s.closed {
		return fault(f.InvalidArgument)
	}
	for _, request := range requests {
		found := false
		for _, held := range s.held {
			if f.CompareLockKeys(request.Key, held.Key) == 0 && (held.Mode == f.Exclusive || held.Mode == request.Mode) {
				found = true
			}
		}
		if !found {
			return fault(f.InvalidArgument)
		}
	}
	return nil
}

type metadataRow struct {
	values []any
	err    error
	after  func()
}

func (r *metadataRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return errors.New("unexpected metadata projection")
	}
	for n, value := range r.values {
		reflect.ValueOf(dest[n]).Elem().Set(reflect.ValueOf(value))
	}
	if r.after != nil {
		r.after()
	}
	return nil
}

type metadataExecutor struct {
	store *metadataStore
	rows  map[string]postgres.Row
	reads []string
}

func (*metadataExecutor) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	panic("execution metadata mutated data")
}
func (*metadataExecutor) Query(context.Context, string, ...any) (*postgres.Rows, error) {
	panic("execution metadata issued an unbounded query")
}
func (x *metadataExecutor) QueryRow(_ context.Context, query string, args ...any) postgres.Row {
	if !x.store.authorized || len(x.store.requested) == 0 {
		panic("execution metadata read before authorization")
	}
	if query != `SELECT id::text,name,description,version FROM agenteam_runner.runners WHERE id=$1` || len(args) != 1 {
		panic("execution metadata queried outside its safe projection")
	}
	id, ok := args[0].(string)
	if !ok {
		panic("execution metadata query has an untyped identity")
	}
	x.store.trace = append(x.store.trace, "read")
	x.reads = append(x.reads, id)
	row, ok := x.rows[id]
	if !ok {
		return &metadataRow{err: pgx.ErrNoRows}
	}
	return row
}

type metadataAuthority struct {
	store      *metadataStore
	request    c.ExecutionMetadataRequest
	err        error
	after      func(c.ExecutionMetadataRequest)
	panicValue any
}

func (a *metadataAuthority) RequireExecutionMetadataInTx(_ context.Context, tx f.Tx, request c.ExecutionMetadataRequest) error {
	a.store.trace = append(a.store.trace, "authority")
	if tx != a.store.tx || len(a.store.requested) == 0 || request.ProjectID != a.request.ProjectID || request.AgentID != a.request.AgentID || request.ExecutionID != a.request.ExecutionID || !slices.Equal(request.RunnerIDs, a.request.RunnerIDs) {
		return fault(f.Forbidden)
	}
	if a.panicValue != nil {
		panic(a.panicValue)
	}
	if a.err != nil {
		return a.err
	}
	if a.after != nil {
		a.after(request)
	}
	a.store.authorized = true
	return nil
}

func metadataFixture(t *testing.T, count int) (*ExecutionMetadataDirectory, *metadataStore, *metadataExecutor, *metadataAuthority, c.ExecutionMetadataRequest) {
	t.Helper()
	request := c.ExecutionMetadataRequest{
		ProjectID:   sid[i.Project](t),
		AgentID:     sid[i.Agent](t),
		ExecutionID: sid[i.Execution](t),
		RunnerIDs:   make([]c.RunnerID, count),
	}
	global, _ := f.SystemConfigLock("runner-management")
	store := &metadataStore{tx: f.NewTx(), held: []f.LockRequest{{Key: global, Mode: f.Shared}}}
	x := &metadataExecutor{store: store, rows: map[string]postgres.Row{}}
	for n := range request.RunnerIDs {
		id, err := f.ParseID[c.Runner](fmt.Sprintf("01900000-0000-7000-8000-%012x", n+1))
		if err != nil {
			t.Fatal(err)
		}
		request.RunnerIDs[n] = id
		gate, _ := f.SystemConfigLock("runner-control-" + id.String())
		store.held = append(store.held, f.LockRequest{Key: gate, Mode: f.Shared})
		x.rows[id.String()] = &metadataRow{values: []any{id.String(), fmt.Sprintf("runner-%d", n), "safe description", int64(n + 1)}}
	}
	slices.SortFunc(store.held, func(a, b f.LockRequest) int { return f.CompareLockKeys(a.Key, b.Key) })
	store.executor = x
	authority := &metadataAuthority{store: store, request: request}
	directory, err := NewExecutionMetadataDirectory(store, authority)
	if err != nil {
		t.Fatal(err)
	}
	return directory, store, x, authority, request
}

func equalMetadataLocks(a, b []f.LockRequest) bool {
	return slices.EqualFunc(a, b, func(left, right f.LockRequest) bool {
		return f.CompareLockKeys(left.Key, right.Key) == 0 && left.Mode == right.Mode
	})
}

func TestExecutionMetadataReadsCompleteSafeProjectionAfterAuthority(t *testing.T) {
	directory, store, x, _, request := metadataFixture(t, 2)
	result, err := directory.ReadExecutionMetadataInTx(context.Background(), store.tx, request)
	if err != nil || len(result) != 2 {
		t.Fatal("complete metadata read failed", err)
	}
	wantTrace := []string{"transaction", "locks", "authority", "read", "read"}
	if !slices.Equal(store.trace, wantTrace) || !equalMetadataLocks(store.requested, store.held) {
		t.Fatal("read bypassed its original transaction, lock set or authority")
	}
	for n, metadata := range result {
		if metadata.ID != request.RunnerIDs[n] || metadata.Name != fmt.Sprintf("runner-%d", n) || metadata.Description != "safe description" || metadata.Version != f.Version(n+1) || x.reads[n] != metadata.ID.String() {
			t.Fatal("metadata lost request order or a safe field")
		}
	}
}

func TestExecutionMetadataEmptySetStillRequiresAuthority(t *testing.T) {
	directory, store, x, authority, request := metadataFixture(t, 0)
	result, err := directory.ReadExecutionMetadataInTx(context.Background(), store.tx, request)
	if err != nil || result == nil || len(result) != 0 || len(x.reads) != 0 || !slices.Equal(store.trace, []string{"transaction", "locks", "authority"}) {
		t.Fatal("empty set skipped authorization or produced metadata", err)
	}
	authority.err = fault(f.Forbidden)
	result, err = directory.ReadExecutionMetadataInTx(context.Background(), store.tx, request)
	if result != nil || err == nil {
		t.Fatal("empty set became an authorization bypass")
	}
}

func TestExecutionMetadataLocksArePureSharedAndDetached(t *testing.T) {
	directory, store, _, _, request := metadataFixture(t, 2)
	ids := []c.RunnerID{request.RunnerIDs[1], request.RunnerIDs[0], request.RunnerIDs[1]}
	before := slices.Clone(ids)
	locks, err := directory.ExecutionMetadataLocks(ids)
	if err != nil || !equalMetadataLocks(locks, store.held) || !slices.Equal(ids, before) || len(store.trace) != 0 {
		t.Fatal("lock collection touched the store or changed the canonical shared set", err)
	}
	locks[0].Mode = f.Exclusive
	again, err := directory.ExecutionMetadataLocks(ids)
	if err != nil || !equalMetadataLocks(again, store.held) {
		t.Fatal("caller changed a later lock set", err)
	}
}

func TestExecutionMetadataRejectsBeforeAnyRunnerRead(t *testing.T) {
	for _, name := range []string{"nil-context", "canceled", "invalid-request", "zero-tx", "foreign-tx", "closed-tx", "missing-global-lock", "missing-runner-lock", "authority", "authority-project", "authority-agent", "authority-execution", "nil-executor", "typed-nil-executor"} {
		t.Run(name, func(t *testing.T) {
			directory, store, x, authority, request := metadataFixture(t, 2)
			ctx := context.Background()
			tx := store.tx
			switch name {
			case "nil-context":
				ctx = nil
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "invalid-request":
				request.RunnerIDs = []c.RunnerID{request.RunnerIDs[1], request.RunnerIDs[0]}
			case "zero-tx":
				tx = f.Tx{}
			case "foreign-tx":
				tx = f.NewTx()
			case "closed-tx":
				store.closed = true
			case "missing-global-lock":
				store.held = store.held[:len(store.held)-1]
			case "missing-runner-lock":
				store.held = store.held[1:]
			case "authority":
				authority.err = fault(f.Forbidden)
			case "authority-project":
				request.ProjectID = sid[i.Project](t)
			case "authority-agent":
				request.AgentID = sid[i.Agent](t)
			case "authority-execution":
				request.ExecutionID = sid[i.Execution](t)
			case "nil-executor":
				store.executor = nil
			case "typed-nil-executor":
				store.executor = (*metadataExecutor)(nil)
			}
			result, err := directory.ReadExecutionMetadataInTx(ctx, tx, request)
			if result != nil || err == nil || len(x.reads) != 0 {
				t.Fatal("rejected read returned metadata or queried Runner", err)
			}
			if name == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation provenance lost")
			}
		})
	}
}

func TestExecutionMetadataRejectsPartialAndCorruptRows(t *testing.T) {
	for _, name := range []string{"missing", "scan", "malformed-id", "different-id", "name", "description", "version", "nil-row", "typed-nil-row"} {
		t.Run(name, func(t *testing.T) {
			directory, store, x, _, request := metadataFixture(t, 2)
			second := request.RunnerIDs[1].String()
			row := x.rows[second].(*metadataRow)
			switch name {
			case "missing":
				delete(x.rows, second)
			case "scan":
				row.err = errors.New("private database failure")
			case "malformed-id":
				row.values[0] = "invalid"
			case "different-id":
				row.values[0] = request.RunnerIDs[0].String()
			case "name":
				row.values[1] = ""
			case "description":
				row.values[2] = "invalid\ntext"
			case "version":
				row.values[3] = int64(0)
			case "nil-row":
				x.rows[second] = nil
			case "typed-nil-row":
				x.rows[second] = (*metadataRow)(nil)
			}
			result, err := directory.ReadExecutionMetadataInTx(context.Background(), store.tx, request)
			var failure *f.Fault
			if result != nil || !errors.As(err, &failure) || len(x.reads) != 2 {
				t.Fatal("failed second row exposed a partial result", err)
			}
			wantCode := f.DependencyUnavailable
			if name == "missing" {
				wantCode = f.NotFound
			}
			if failure.Code != wantCode {
				t.Fatal("wrong safe failure classification")
			}
		})
	}
}

func TestExecutionMetadataCancellationDiscardsAllResults(t *testing.T) {
	for _, phase := range []string{"authority", "first-row", "last-row", "authority-error", "row-error"} {
		t.Run(phase, func(t *testing.T) {
			directory, store, x, authority, request := metadataFixture(t, 2)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			wantReads := 0
			switch phase {
			case "authority":
				authority.after = func(c.ExecutionMetadataRequest) { cancel() }
			case "first-row":
				x.rows[request.RunnerIDs[0].String()].(*metadataRow).after = cancel
				wantReads = 1
			case "last-row":
				x.rows[request.RunnerIDs[1].String()].(*metadataRow).after = cancel
				wantReads = 2
			case "authority-error":
				authority.err = context.Canceled
			case "row-error":
				x.rows[request.RunnerIDs[1].String()].(*metadataRow).err = context.Canceled
				wantReads = 2
			}
			result, err := directory.ReadExecutionMetadataInTx(ctx, store.tx, request)
			if result != nil || !errors.Is(err, context.Canceled) || len(x.reads) != wantReads {
				t.Fatal("cancellation leaked metadata or started another read", err)
			}
		})
	}
}

func TestExecutionMetadataDetachesAuthorityRequestIDs(t *testing.T) {
	directory, store, _, authority, request := metadataFixture(t, 2)
	original := slices.Clone(request.RunnerIDs)
	authority.after = func(received c.ExecutionMetadataRequest) { received.RunnerIDs[0] = received.RunnerIDs[1] }
	result, err := directory.ReadExecutionMetadataInTx(context.Background(), store.tx, request)
	if err != nil || len(result) != 2 || result[0].ID != original[0] || result[1].ID != original[1] || !slices.Equal(original, request.RunnerIDs) {
		t.Fatal("authority changed the read set through an aliased request", err)
	}
}

func TestExecutionMetadataPanicUnwindsWithoutOutput(t *testing.T) {
	for _, phase := range []string{"authority", "first-row"} {
		t.Run(phase, func(t *testing.T) {
			directory, store, x, authority, request := metadataFixture(t, 2)
			marker := new(int)
			wantReads := 0
			if phase == "authority" {
				authority.panicValue = marker
			} else {
				x.rows[request.RunnerIDs[0].String()].(*metadataRow).after = func() { panic(marker) }
				wantReads = 1
			}
			returned, recovered := false, false
			func() {
				defer func() { recovered = recover() == marker }()
				_, _ = directory.ReadExecutionMetadataInTx(context.Background(), store.tx, request)
				returned = true
			}()
			if returned || !recovered || len(x.reads) != wantReads {
				t.Fatal("panic became a result or allowed later SQL")
			}
		})
	}
}

func TestExecutionMetadataUnboundPortsRejectWithoutCallingDependencies(t *testing.T) {
	_, store, _, authority, request := metadataFixture(t, 1)
	for _, candidate := range []struct {
		store     Store
		authority c.ExecutionMetadataAuthority
	}{
		{nil, authority},
		{(*metadataStore)(nil), authority},
		{store, nil},
		{store, (*metadataAuthority)(nil)},
	} {
		if directory, err := NewExecutionMetadataDirectory(candidate.store, candidate.authority); directory != nil || err == nil {
			t.Fatal("unbound dependency accepted")
		}
	}
	for _, directory := range []*ExecutionMetadataDirectory{nil, {}, {store: store}, {authority: authority}} {
		result, err := directory.ReadExecutionMetadataInTx(context.Background(), store.tx, request)
		if result != nil || err == nil {
			t.Fatal("unbound directory returned metadata")
		}
		locks, err := directory.ExecutionMetadataLocks(request.RunnerIDs)
		if locks != nil || err == nil {
			t.Fatal("unbound directory provided a lock set")
		}
	}
	if len(store.trace) != 0 {
		t.Fatal("unbound dependency reached a store")
	}
}
