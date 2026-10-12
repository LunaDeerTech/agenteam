package artifact

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

// These controls reuse the original live Tx/Project EX fixture. They establish
// call ordering and ownership, not real ProcessGuard death, SQL rollback, a
// Project Delete grant, or the physical Object cleanup preceding this tail.
type cleanupTechnicalStore struct {
	*cleanupOwnerStore
	v *cleanupTechnicalFixture
}

func (s *cleanupTechnicalStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.cleanupOwnerStore.InTx(tx); err != nil {
		return nil, err
	}
	return cleanupTechnicalExecutor{s, tx}, nil
}

type cleanupTechnicalExecutor struct {
	store *cleanupTechnicalStore
	tx    f.Tx
}

func (e cleanupTechnicalExecutor) Query(context.Context, string, ...any) (*postgres.Rows, error) {
	return nil, errors.New("business Rows are outside the technical-tail controls")
}
func (e cleanupTechnicalExecutor) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	if _, err := e.store.InTx(e.tx); err != nil {
		return cleanupOwnerRow{err: err}
	}
	if !strings.HasPrefix(query, "SELECT jsonb_build_object(\n 'Work'") {
		return e.store.v.cleanupOwnerFixture.row(ctx, e.tx, query, args)
	}
	v := e.store.v
	v.mu.Lock()
	_, err := v.fact(e.tx, args)
	if err != nil {
		v.mu.Unlock()
		return cleanupOwnerRow{err: err}
	}
	p := v.gate[e.tx]
	facts := v.technical[p]
	raw, err := json.Marshal(facts)
	v.reads++
	v.mu.Unlock()
	if err != nil {
		return cleanupOwnerRow{err: err}
	}
	return cleanupOwnerRow{values: []any{raw}}
}
func (e cleanupTechnicalExecutor) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	v := e.store.v
	if _, err := e.store.InTx(e.tx); err != nil {
		return pgconn.CommandTag{}, err
	}
	if !strings.HasPrefix(query, "DELETE FROM agenteam_artifact.project_") {
		return v.cleanupOwnerFixture.exec(ctx, e.tx, query, args)
	}
	v.mu.Lock()
	_, err := v.fact(e.tx, args)
	v.mu.Unlock()
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	if v.beforeExec != nil {
		if err := v.beforeExec(ctx, query); err != nil {
			return pgconn.CommandTag{}, err
		}
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	p := v.gate[e.tx]
	facts := v.technical[p]
	if len(args) < 2 {
		return pgconn.CommandTag{}, errors.New("missing exact row selection")
	}
	ids, ok := args[1].([]string)
	if !ok || len(ids) == 0 {
		return pgconn.CommandTag{}, errors.New("invalid exact row selection")
	}
	want := make([]string, 0)
	kind := "work"
	if strings.HasPrefix(query, "DELETE FROM agenteam_artifact.project_work ") {
		if len(args) != 2 || !strings.Contains(query, "joined_at IS NOT NULL AND source_project_id IS NULL AND source_lease_id IS NULL AND source_revoked_by IS NULL AND source_joined_at IS NULL") {
			return pgconn.CommandTag{}, errors.New("work retirement lost its closed predicates")
		}
		for _, row := range facts.Work[:min(100, len(facts.Work))] {
			want = append(want, row.ID)
		}
	} else if strings.HasPrefix(query, "DELETE FROM agenteam_artifact.project_stops ") {
		kind = "stops"
		if len(args) != 3 || args[2] != int64(v.facts[p].cause.Details().Version) || !strings.Contains(query, "action='archive' AND state='stopped' AND project_version<$3") {
			return pgconn.CommandTag{}, errors.New("archive retirement lost its closed predicates")
		}
		for _, row := range facts.Stops {
			if row.Operation != v.facts[p].cause.Details().OperationID.String() {
				want = append(want, row.Operation)
			}
		}
	} else {
		return pgconn.CommandTag{}, errors.New("unexpected technical mutation")
	}
	if !reflect.DeepEqual(ids, want) {
		return pgconn.CommandTag{}, errors.New("retirement selected a different row set")
	}
	v.deletes = append(v.deletes, kind)
	if v.shortCount == kind {
		return pgconn.NewCommandTag(fmt.Sprintf("DELETE %d", len(ids)-1)), nil
	}
	if kind == "work" {
		facts.Work = facts.Work[len(ids):]
	} else {
		for _, stop := range facts.Stops {
			if stop.Operation == v.facts[p].cause.Details().OperationID.String() {
				facts.Stops = []artifactCleanupStop{stop}
				break
			}
		}
	}
	v.technical[p] = facts
	return pgconn.NewCommandTag(fmt.Sprintf("DELETE %d", len(ids))), nil
}

type cleanupTechnicalFixture struct {
	*cleanupOwnerFixture
	store      *cleanupTechnicalStore
	pair       *ProjectCallOwnership
	process    oc.ProcessID
	technical  map[i.ProjectID]artifactCleanupCalls
	deletes    []string
	reads      int
	proofs     int
	foreignErr error
	shortCount string
	beforeExec func(context.Context, string) error
}

func newCleanupTechnicalFixture(t *testing.T) (*cleanupTechnicalFixture, i.ProjectID) {
	t.Helper()
	base, p := newCleanupOwnerFixture(t)
	v := &cleanupTechnicalFixture{cleanupOwnerFixture: base, process: newTestID[oc.Process](t), technical: map[i.ProjectID]artifactCleanupCalls{}}
	v.store = &cleanupTechnicalStore{base.store, v}
	owners, err := NewOwnerProvider(v.store, v)
	if err != nil {
		t.Fatal(err)
	}
	v.state.store, v.state.owners = v.store, owners
	v.pair, err = newProjectCallOwnership(v.service, v, v.process, v)
	if err != nil {
		t.Fatal(err)
	}
	v.completed(p)
	t.Cleanup(func() {
		// Subtest cleanups receive original goroutines before this callback.
		v.store.before, v.store.after, v.beforeExec, v.foreignErr = nil, nil, nil, nil
		v.deny = false
		for p := range v.facts {
			v.completed(p)
		}
		v.state.cleanupMu.Lock()
		var pending []i.ProjectID
		for p, call := range v.state.cleanupCalls {
			if call.retained && !call.active {
				pending = append(pending, p)
			}
		}
		v.state.cleanupMu.Unlock()
		for _, p := range pending {
			if _, err := v.run(context.Background(), v.service, p); err != nil {
				t.Error("original technical cleanup did not confirm", err)
			}
		}
		v.pair.Stop()
		if !v.pair.Joined() {
			t.Error("original technical cleanup control did not return")
		}
	})
	return v, p
}
func (v *cleanupTechnicalFixture) completed(p i.ProjectID) {
	v.facts[p].state = "completed"
	d := v.facts[p].cause.Details()
	v.technical[p] = artifactCleanupCalls{Work: []artifactCleanupWork{}, Stops: []artifactCleanupStop{{Operation: d.OperationID.String(), Action: "delete", State: "stopped", Process: v.process.String(), Instance: v.pair.state.instance.String(), Version: int64(d.Version)}}}
}
func (v *cleanupTechnicalFixture) addWork(t *testing.T, p i.ProjectID, n int) {
	t.Helper()
	facts := v.technical[p]
	for range n {
		facts.Work = append(facts.Work, artifactCleanupWork{ID: newTestID[artifactCallID](t).String(), Project: p.String(), Process: v.process.String(), Instance: v.pair.state.instance.String(), Joined: true})
	}
	sort.Slice(facts.Work, func(a, b int) bool { return facts.Work[a].ID < facts.Work[b].ID })
	v.technical[p] = facts
}
func (v *cleanupTechnicalFixture) addArchive(t *testing.T, p i.ProjectID) {
	t.Helper()
	facts := v.technical[p]
	facts.Stops = append(facts.Stops, artifactCleanupStop{Operation: newTestID[oc.ProjectStopOperation](t).String(), Action: "archive", State: "stopped", Process: v.process.String(), Instance: v.pair.state.instance.String(), Version: 6})
	sort.Slice(facts.Stops, func(a, b int) bool { return facts.Stops[a].Operation < facts.Stops[b].Operation })
	v.technical[p] = facts
}
func (v *cleanupTechnicalFixture) RequireCurrentProcessInTx(ctx context.Context, tx f.Tx, process oc.ProcessID) error {
	if process != v.process {
		return failure(f.Forbidden, nil)
	}
	if _, err := v.store.InTx(tx); err != nil {
		return err
	}
	return ctx.Err()
}
func (v *cleanupTechnicalFixture) ConfirmStopped(ctx context.Context, process oc.ProcessID) error {
	v.store.mu.Lock()
	live := len(v.store.live)
	v.store.mu.Unlock()
	if live != 0 || process == v.process {
		return errors.New("foreign proof ran inside a transaction or for the current process")
	}
	v.mu.Lock()
	v.proofs++
	v.mu.Unlock()
	if v.foreignErr != nil {
		return v.foreignErr
	}
	return ctx.Err()
}
func (*cleanupTechnicalFixture) DiscoverProjectStop(context.Context, oc.ProjectStopRequest) (oc.AccessDependencies, error) {
	return oc.AccessDependencies{}, failure(f.DependencyUnbound, nil)
}
func (*cleanupTechnicalFixture) ValidateProjectStopInTx(context.Context, f.Tx, oc.ProjectStopRequest, oc.AccessDependencies) (oc.ProjectStopAuthorization, error) {
	return oc.ProjectStopAuthorization{}, failure(f.DependencyUnbound, nil)
}

// Exercise the actual terminal technical predicate under the original private
// cleanup owner and Store. Business/Object cleanup is deliberately not faked.
func (v *cleanupTechnicalFixture) retire(ctx context.Context, p i.ProjectID) (bool, bool, error) {
	fact := v.facts[p]
	call, err := v.state.borrowProjectCleanup(fact.actor, fact.cause.Details())
	if err != nil {
		return false, false, err
	}
	defer v.state.releaseProjectCleanup(call)
	ctx = context.WithValue(ctx, projectCleanupContextKey{}, projectCleanupContext{v.state, call})
	ctx, ready, err := v.service.prepareCleanupCalls(ctx, fact.actor, fact.cause, call)
	if err != nil || !ready {
		return ready, false, err
	}
	var clean bool
	result := v.service.withinCleanup(ctx, fact.actor, fact.cause, nil, nil, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, _ oc.LockedAccess) error {
		ready, clean, err = v.service.cleanupCallsInTx(ctx, tx, x, fact.cause, true)
		return err
	})
	return ready, clean, projectCleanupResultError(result)
}

func TestArtifactCleanupTechnicalEligibility(t *testing.T) {
	for _, mode := range []string{"exact-current", "missing-current", "empty-without-stop", "unjoined", "null-instance", "replacement-instance", "source-project", "source-use", "source-side", "stopping", "wrong-version", "old-delete", "unfinished-scan", "foreign-alive", "legacy-history"} {
		t.Run(mode, func(t *testing.T) {
			v, p := newCleanupTechnicalFixture(t)
			if mode != "exact-current" {
				v.addWork(t, p, 1)
			}
			facts := v.technical[p]
			switch mode {
			case "missing-current":
				facts.Stops = []artifactCleanupStop{}
			case "empty-without-stop":
				facts.Work, facts.Stops = []artifactCleanupWork{}, []artifactCleanupStop{}
			case "unjoined":
				facts.Work[0].Joined = false
			case "null-instance":
				facts.Work[0].Instance = ""
			case "replacement-instance":
				facts.Work[0].Instance = newTestID[artifactInstanceID](t).String()
			case "source-project":
				facts.Work[0].Source = p.String()
			case "source-use":
				facts.Work[0].SourceUse = true
			case "source-side":
				facts.Work[0].Source = p.String()
				facts.Work[0].Project = newTestID[i.Project](t).String()
			case "stopping":
				facts.Stops[0].State = "stopping"
			case "wrong-version":
				facts.Stops[0].Version++
			case "old-delete":
				facts.Stops[0].Operation = newTestID[oc.ProjectStopOperation](t).String()
			case "unfinished-scan":
				facts.Stops[0].Scan = 1
			case "foreign-alive":
				facts.Work[0].Process = newTestID[oc.Process](t).String()
				v.foreignErr = errors.New("original process still alive")
			case "legacy-history":
				v.state.ownership = nil
			}
			v.technical[p] = facts
			out, err := v.run(context.Background(), v.service, p)
			want := oc.CleanupPending
			if mode == "exact-current" {
				want = oc.CleanupCompleted
			}
			if err != nil || out.State != want || len(v.deletes) != 0 || v.discovery != 0 {
				t.Fatal("technical evidence was bypassed or caused business cleanup", mode, out, err)
			}
			if mode == "foreign-alive" && v.proofs != 1 {
				t.Fatal("foreign process did not reach the actual proof port")
			}
		})
	}
}

func TestArtifactCleanupTechnicalRetirement(t *testing.T) {
	for _, mode := range []string{"complete", "bounded-work", "work-count", "stop-count", "sql-error", "identity-changed-after-proof"} {
		t.Run(mode, func(t *testing.T) {
			v, p := newCleanupTechnicalFixture(t)
			n := 2
			if mode == "bounded-work" {
				n = 101
			}
			v.addWork(t, p, n)
			v.addArchive(t, p)
			originalErr := errors.New("original technical delete failure")
			switch mode {
			case "work-count":
				v.shortCount = "work"
			case "stop-count":
				v.shortCount = "stops"
			case "sql-error":
				v.beforeExec = func(context.Context, string) error { return originalErr }
			case "identity-changed-after-proof":
				v.store.after = func(_ context.Context, _ f.TransactionCause, result f.CommitResult) f.CommitResult {
					v.mu.Lock()
					facts := v.technical[p]
					facts.Work[0].Process = newTestID[oc.Process](t).String()
					v.technical[p] = facts
					v.mu.Unlock()
					return result
				}
			}
			ready, clean, err := v.retire(context.Background(), p)
			v.store.after = nil
			switch mode {
			case "complete":
				if err != nil || !ready || !clean || !reflect.DeepEqual(v.deletes, []string{"work", "stops"}) {
					t.Fatal("joined work did not precede archive retirement", ready, clean, err)
				}
				facts := v.technical[p]
				if len(facts.Work) != 0 || len(facts.Stops) != 1 || facts.Stops[0].Operation != v.facts[p].cause.Details().OperationID.String() {
					t.Fatal("current Delete receipt was removed")
				}
			case "bounded-work":
				if err != nil || !ready || clean || len(v.technical[p].Work) != 1 || !reflect.DeepEqual(v.deletes, []string{"work"}) || len(v.technical[p].Stops) != 2 {
					t.Fatal("work batch skipped its remaining rows or retired receipts early", err)
				}
			case "identity-changed-after-proof":
				if err != nil || ready || clean || len(v.deletes) != 0 {
					t.Fatal("changed technical owner reused an earlier proof", err)
				}
			default:
				if err == nil || clean {
					t.Fatal("failed technical deletion published success")
				}
				if mode == "sql-error" && !errors.Is(err, originalErr) {
					t.Fatal("original SQL error lost", err)
				}
				if mode == "work-count" && !reflect.DeepEqual(v.deletes, []string{"work"}) {
					t.Fatal("archive deletion followed an inexact work result")
				}
			}
		})
	}
}

func TestArtifactCleanupTechnicalPreflightReturn(t *testing.T) {
	v, p := newCleanupTechnicalFixture(t)
	facts := v.technical[p]
	facts.Stops[0].Process = newTestID[oc.Process](t).String()
	v.technical[p] = facts
	b := v.add(t)
	v.completed(b)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	// Hold only the first original preflight return, not later transactions.
	var enteredOnce sync.Once
	v.store.after = func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
		if cause.Details().CheckpointRef == p.String() {
			enteredOnce.Do(func() { close(entered); <-release })
		}
		return result
	}
	type output struct {
		out oc.ProjectCleanupResult
		err error
	}
	done := make(chan output, 1)
	joined := false
	t.Cleanup(func() {
		unblock()
		if !joined {
			cleanupOwnerWait(t, done)
		}
	})
	go func() { out, err := v.run(context.Background(), v.service, p); done <- output{out, err} }()
	cleanupOwnerWait(t, entered)
	if v.proofs != 0 || len(v.deletes) != 0 {
		t.Fatal("foreign proof or mutation preceded the original preflight return")
	}
	copyService := *v.service
	if _, err := v.run(context.Background(), &copyService, p); err == nil {
		t.Fatal("a Service copy borrowed the original active cleanup")
	}
	if out, err := v.run(context.Background(), v.service, b); err != nil || out.State != oc.CleanupCompleted {
		t.Fatal("Project A blocked B", err)
	}
	unblock()
	got := cleanupOwnerWait(t, done)
	joined = true
	if got.err != nil || got.out.State != oc.CleanupCompleted || v.proofs != 1 {
		t.Fatal("original returned proof chain did not complete", got.err)
	}
}

func TestArtifactCleanupTechnicalUnknownConfirmation(t *testing.T) {
	for _, mode := range []string{"current-only", "joined-work", "unreturned-owner"} {
		t.Run(mode, func(t *testing.T) {
			v, p := newCleanupTechnicalFixture(t)
			attempt := newTestID[f.TransactionAttempt](t)
			var original f.CommitResult
			v.store.after = func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
				if result.State() != f.Committed {
					return result
				}
				original = f.UnknownResult(attempt, cause)
				return original
			}
			out, err := v.run(context.Background(), v.service, p)
			v.store.after = nil
			requireCleanupOwnerUnknown(t, out, err, original)
			if len(v.deletes) != 0 || v.discovery != 0 {
				t.Fatal("Unknown preflight continued")
			}
			if mode == "joined-work" {
				v.addWork(t, p, 1)
			}
			if mode == "unreturned-owner" {
				id := newTestID[artifactCallID](t)
				v.pair.state.owners[id] = &artifactCall{record: artifactCallRecord{ID: id, Project: p}}
				t.Cleanup(func() { delete(v.pair.state.owners, id) })
			}
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			v.store.after = func(_ context.Context, _ f.TransactionCause, result f.CommitResult) f.CommitResult {
				close(entered)
				<-release
				return result
			}
			type output struct {
				out oc.ProjectCleanupResult
				err error
			}
			done := make(chan output, 1)
			joined := false
			t.Cleanup(func() {
				unblock()
				if !joined {
					cleanupOwnerWait(t, done)
				}
			})
			go func() { out, err := v.run(context.Background(), v.service, p); done <- output{out, err} }()
			cleanupOwnerWait(t, entered)
			v.state.cleanupMu.Lock()
			owner := v.state.cleanupCalls[p]
			retained := owner != nil && owner.active && owner.retained && owner.original.AttemptID() == attempt
			v.state.cleanupMu.Unlock()
			if !retained || len(v.deletes) != 0 {
				t.Fatal("confirmation released the original owner or repeated deletion")
			}
			unblock()
			got := cleanupOwnerWait(t, done)
			joined = true
			v.store.after = nil
			want := oc.CleanupPending
			if mode == "current-only" {
				want = oc.CleanupCompleted
			}
			if got.err != nil || got.out.State != want || len(v.deletes) != 0 || len(v.state.cleanupCalls) != 0 {
				t.Fatal("read-only original confirmation", got.out, got.err)
			}
		})
	}
}

func TestArtifactCleanupTechnicalDeleteReturn(t *testing.T) {
	v, p := newCleanupTechnicalFixture(t)
	v.addWork(t, p, 1)
	v.addArchive(t, p)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	originalErr := errors.New("original held delete error")
	v.beforeExec = func(context.Context, string) error { close(entered); <-release; return originalErr }
	done := make(chan error, 1)
	joined := false
	t.Cleanup(func() {
		unblock()
		if !joined {
			cleanupOwnerWait(t, done)
		}
	})
	go func() { _, _, err := v.retire(ctx, p); done <- err }()
	cleanupOwnerWait(t, entered)
	cancel()
	select {
	case err := <-done:
		joined = true
		t.Fatal("cancel replaced the original DELETE return", err)
	default:
	}
	unblock()
	err := cleanupOwnerWait(t, done)
	joined = true
	if !errors.Is(err, originalErr) || len(v.deletes) != 0 {
		t.Fatal("original DELETE error was hidden or archive cleanup advanced", err)
	}
}
