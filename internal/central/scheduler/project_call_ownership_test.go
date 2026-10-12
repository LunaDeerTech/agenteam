package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Technical SQL rows below are explicit controls. Original callbacks, live
// transactions, complete lock requests and physical Store returns are real
// calls; these controls do not prove PostgreSQL commits or foreign death.
type schedulerCallStore struct {
	Store
	mu                   sync.Mutex
	live                 map[f.Tx][]f.LockRequest
	rows                 map[string]projectCallRecord
	calls, reads, writes int
	after                func(context.Context, f.TransactionCause, f.CommitResult) f.CommitResult
}

func (s *schedulerCallStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	var result f.CommitResult
	if s.Store != nil && cause.Details().Kind != f.RecoveryCause {
		result = s.Store.WithinTx(ctx, cause, fn)
	} else {
		tx := f.NewTx()
		s.mu.Lock()
		s.live[tx] = nil
		s.mu.Unlock()
		err := fn(ctx, tx)
		s.mu.Lock()
		delete(s.live, tx)
		s.mu.Unlock()
		result = f.CommittedResult()
		if err != nil {
			var known *f.Fault
			if !errors.As(err, &known) {
				known = f.NewFault(f.DependencyUnavailable, f.NotCommitted).WithCause(err)
			}
			result = f.NotCommittedResult(known)
		}
	}
	if s.after != nil {
		return s.after(ctx, cause, result)
	}
	return result
}
func (s *schedulerCallStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	s.mu.Lock()
	_, live := s.live[tx]
	s.mu.Unlock()
	if live {
		return s, nil
	}
	if s.Store == nil {
		return nil, fault(f.Forbidden)
	}
	x, err := s.Store.InTx(tx)
	if err != nil {
		return nil, err
	}
	return schedulerCallSQL{owner: s, original: x}, nil
}
func (s *schedulerCallStore) AcquireAll(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	s.mu.Lock()
	_, live := s.live[tx]
	if live {
		s.live[tx] = append([]f.LockRequest(nil), locks...)
	}
	s.mu.Unlock()
	if live {
		return ctx.Err()
	}
	if s.Store != nil {
		return s.Store.AcquireAll(ctx, tx, locks)
	}
	return fault(f.Forbidden)
}
func (s *schedulerCallStore) RequireHeldLocks(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	s.mu.Lock()
	held, live := s.live[tx]
	s.mu.Unlock()
	if !live {
		if s.Store != nil {
			return s.Store.RequireHeldLocks(ctx, tx, locks)
		}
		return fault(f.Forbidden)
	}
	for _, want := range locks {
		found := false
		for _, have := range held {
			if have.Key.Canonical() == want.Key.Canonical() && (have.Mode == f.Exclusive || have.Mode == want.Mode) {
				found = true
			}
		}
		if !found {
			return fault(f.Forbidden)
		}
	}
	return ctx.Err()
}

type schedulerCallSQL struct {
	owner    *schedulerCallStore
	original postgres.SQLExecutor
}

func (x schedulerCallSQL) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(query, "agenteam_scheduler.project_calls") {
		return x.owner.Exec(ctx, query, args...)
	}
	return x.original.Exec(ctx, query, args...)
}
func (x schedulerCallSQL) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	if strings.Contains(query, "agenteam_scheduler.project_calls") || query == pendingVisitSQL {
		return x.owner.QueryRow(ctx, query, args...)
	}
	return x.original.QueryRow(ctx, query, args...)
}
func (x schedulerCallSQL) Query(ctx context.Context, query string, args ...any) (*postgres.Rows, error) {
	return x.original.Query(ctx, query, args...)
}
func (s *schedulerCallStore) Query(context.Context, string, ...any) (*postgres.Rows, error) {
	return nil, fault(f.Forbidden)
}
func (s *schedulerCallStore) QueryRow(_ context.Context, query string, args ...any) postgres.Row {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads++
	var value any
	switch {
	case query == pendingVisitSQL:
		return dispatchTestRow{err: pgx.ErrNoRows}
	case strings.Contains(query, "jsonb_agg") && len(args) == 1:
		rows := []projectCallRecord{}
		for _, r := range s.rows {
			if r.Project.String() == args[0] {
				rows = append(rows, r)
			}
		}
		slices.SortFunc(rows, func(a, b projectCallRecord) int { return strings.Compare(a.ID.String(), b.ID.String()) })
		value = rows
	case strings.Contains(query, "WHERE id=$1") && len(args) == 1:
		row, ok := s.rows[args[0].(string)]
		if !ok {
			return dispatchTestRow{err: pgx.ErrNoRows}
		}
		value = row
	default:
		return dispatchTestRow{err: errors.New("unexpected technical read")}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return dispatchTestRow{err: err}
	}
	return dispatchTestRow{values: []any{raw}}
}
func (s *schedulerCallStore) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.HasPrefix(query, "INSERT INTO agenteam_scheduler.project_calls") && len(args) == 7 {
		id, e1 := f.ParseID[projectCallID](args[0].(string))
		p, e2 := f.ParseID[i.Project](args[1].(string))
		process, e3 := f.ParseID[oc.Process](args[2].(string))
		instance, e4 := f.ParseID[projectCallInstance](args[3].(string))
		r := projectCallRecord{ID: id, Project: p, Process: process, Instance: instance, Lane: projectCallLane(args[4].(string)), Identity: args[5].(string), Digest: f.Digest(args[6].(string)), Phase: "running"}
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil || !r.valid() {
			return pgconn.CommandTag{}, fault(f.InvalidArgument)
		}
		if _, exists := s.rows[id.String()]; exists {
			return pgconn.CommandTag{}, fault(f.ResourceBusy)
		}
		s.rows[id.String()] = r
		s.writes++
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	}
	if !strings.HasPrefix(query, "UPDATE agenteam_scheduler.project_calls SET phase='business_closed'") || len(args) != 7 {
		return pgconn.CommandTag{}, errors.New("unexpected technical write")
	}
	r, ok := s.rows[args[0].(string)]
	if !ok || r.Phase != "running" || args[1] != r.Project.String() || args[2] != r.Process.String() || args[3] != r.Instance.String() || args[4] != string(r.Lane) || args[5] != r.Identity || args[6] != string(r.Digest) {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	r.Phase = "business_closed"
	s.rows[r.ID.String()] = r
	s.writes++
	return pgconn.NewCommandTag("UPDATE 1"), nil
}
func (s *schedulerCallStore) counts() (int, int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls, s.reads, s.writes
}

type schedulerCallProof struct {
	mu       sync.Mutex
	store    *schedulerCallStore
	confirms int
	confirm  func(context.Context, oc.ProcessID) error
}

func (p *schedulerCallProof) RequireCurrentProcessInTx(ctx context.Context, tx f.Tx, _ oc.ProcessID) error {
	_, err := p.store.InTx(tx)
	if err != nil {
		return err
	}
	return ctx.Err()
}
func (p *schedulerCallProof) ConfirmStopped(ctx context.Context, process oc.ProcessID) error {
	p.store.mu.Lock()
	live := len(p.store.live)
	p.store.mu.Unlock()
	if live != 0 {
		return fault(f.Forbidden)
	}
	p.mu.Lock()
	p.confirms++
	p.mu.Unlock()
	if p.confirm != nil {
		return p.confirm(ctx, process)
	}
	return ctx.Err()
}

type schedulerCallLoanKey struct{}
type schedulerCallAuthority struct{ store *schedulerCallStore }

func (a *schedulerCallAuthority) WithProjectStop(ctx context.Context, _ i.Actor, _ pc.LifecycleCause, _ pc.ScopeRef, fn func(context.Context) error) error {
	return fn(context.WithValue(ctx, schedulerCallLoanKey{}, a))
}
func (a *schedulerCallAuthority) RequireProjectStopInTx(ctx context.Context, tx f.Tx, _ i.Actor, _ pc.LifecycleCause, scope pc.ScopeRef) error {
	if ctx.Value(schedulerCallLoanKey{}) != a {
		return fault(f.Forbidden)
	}
	return a.store.RequireHeldLocks(ctx, tx, projectCallLocks(scope.ProjectID))
}

type schedulerCallProjects struct {
	store   *schedulerCallStore
	current func(i.ProjectID) pc.SchedulerProject
}

func (p *schedulerCallProjects) RequireSchedulerProjectInTx(ctx context.Context, tx f.Tx, project i.ProjectID) (pc.SchedulerProject, error) {
	if err := p.store.RequireHeldLocks(ctx, tx, pendingLocks(project)); err != nil {
		return pc.SchedulerProject{}, err
	}
	if p.current != nil {
		return p.current(project), ctx.Err()
	}
	return pc.SchedulerProject{Project: pc.ProjectRef{ID: project, Lifecycle: pc.Active}, Config: pc.ProjectSchedulerConfig{Enabled: true}}, ctx.Err()
}

type schedulerCallsFixture struct {
	pair  *ProjectCallOwnership
	store *schedulerCallStore
	proof *schedulerCallProof
	actor i.Actor
	cause pc.LifecycleCause
	p, b  i.ProjectID
}

func newSchedulerCalls(t *testing.T) *schedulerCallsFixture {
	t.Helper()
	pair, _ := newSchedulerEntryPair(t)
	store := &schedulerCallStore{live: map[f.Tx][]f.LockRequest{}, rows: map[string]projectCallRecord{}}
	proof := &schedulerCallProof{store: store}
	projects := &schedulerCallProjects{store: store}
	// Fix explicit technical facts on the same original private factory graph.
	// Original business ports still refuse unsupported work; no default binding
	// or production authority is fabricated by this controlled test graph.
	pair.state.store, pair.state.pending.store, pair.state.proof, pair.state.authority = store, store, proof, &schedulerCallAuthority{store: store}
	pair.Coordinator().deps.Projects, pair.Busy().deps.Projects, pair.Failure().deps.Projects, pair.Handoff().retryProjects = projects, projects, projects, projects
	v := &schedulerCallsFixture{pair: pair, store: store, proof: proof, p: dispatchTestID[i.Project](t, 100), b: dispatchTestID[i.Project](t, 200), cause: pc.LifecycleCause{OperationID: dispatchTestID[pc.Operation](t, 500), Action: pc.Delete, ProjectVersion: 2}}
	service, err := i.RegisterService(i.ProjectLifecycle)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := i.InProject(v.p)
	if err != nil {
		t.Fatal(err)
	}
	v.actor, err = service.Actor(v.cause.OperationID.String(), scope)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func (v *schedulerCallsFixture) stop(ctx context.Context, request bool) (pc.StopReport, error) {
	scope := pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: v.p}
	if request {
		return v.pair.RequestProjectStop(ctx, v.actor, v.cause, scope)
	}
	return v.pair.InspectProjectStop(ctx, v.actor, v.cause, scope)
}
func (v *schedulerCallsFixture) admit(t *testing.T, p i.ProjectID, method string) (context.Context, *projectCallEntry) {
	t.Helper()
	identity, err := projectEntryIdentity(p, method, f.IdempotencyKey(method))
	if err != nil {
		t.Fatal(err)
	}
	ctx, e, err := v.pair.state.admit(context.Background(), p, projectLaneVisitor, identity)
	if err != nil {
		t.Fatal(err)
	}
	return ctx, e
}
func (v *schedulerCallsFixture) close(t *testing.T, e *projectCallEntry) {
	t.Helper()
	if err := v.pair.state.finish(e, nil); err != nil {
		t.Error(err)
	}
	v.pair.state.returned(e)
}
func requireSchedulerStopState(t *testing.T, report pc.StopReport, err error, state pc.StopState) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	if report.Details().State != state {
		t.Fatal("unexpected original stop state", report.Details().State)
	}
}

func TestSchedulerProjectCallOriginalBusinessReturn(t *testing.T) {
	v := newSchedulerCalls(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	v.store.after = func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
		d := cause.Details()
		if d.Kind == f.CommandsCause && d.Primary.Command() == "visit_pending" && d.Primary.OwnerIDs()[0] == v.p.String() {
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
				t.Error("original business Store did not return")
			}
		}
		v.pair.Stop()
	})
	go func() { _, err := v.pair.Visitor().VisitNext(context.Background(), v.p, nil); done <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("original business Store not reached")
	}
	report, err := v.stop(context.Background(), true)
	requireSchedulerStopState(t, report, err, pc.StopPending)
	if _, err = v.pair.Visitor().VisitNext(context.Background(), v.b, nil); err != nil {
		t.Fatal("A stop blocked B", err)
	}
	select {
	case <-done:
		collected = true
		t.Fatal("stop substituted original Store return")
	default:
	}
	unblock()
	select {
	case err = <-done:
		collected = true
		if !errors.Is(err, context.Canceled) {
			t.Fatal("original cancellation changed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("original business did not return")
	}
	report, err = v.stop(context.Background(), true)
	requireSchedulerStopState(t, report, err, pc.Stopped)
	v.pair.Stop()
	if !v.pair.Joined() {
		t.Fatal("known original return did not retire")
	}
}

func TestSchedulerProjectCallTechnicalUnknown(t *testing.T) {
	for _, phase := range []string{"registration", "return"} {
		t.Run(phase, func(t *testing.T) {
			v := newSchedulerCalls(t)
			attempt := dispatchTestID[f.TransactionAttempt](t, 990)
			owner := "scheduler-call-register"
			if phase == "return" {
				owner = "scheduler-call-return"
			}
			masked := false
			var first f.CommitResult
			v.store.after = func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
				if cause.Details().Owner == owner && !masked && result.State() == f.Committed {
					masked = true
					first = f.UnknownResult(attempt, cause)
					return first
				}
				return result
			}
			_, err := v.pair.Visitor().VisitNext(context.Background(), v.p, nil)
			raw, ok := UnknownAttempt(err)
			if !ok || raw.AttemptID() != attempt || !schedulerSameRecoveryCause(raw.Cause(), first.Cause()) {
				t.Fatal("original technical Unknown lost", err)
			}
			v.pair.state.mu.Lock()
			var entry *projectCallEntry
			for _, e := range v.pair.state.owners {
				entry = e
			}
			v.pair.state.mu.Unlock()
			if entry == nil || !entry.returned || entry.unknown == nil {
				t.Fatal("original returned owner not retained")
			}
			_, _, writes := v.store.counts()
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			v.store.after = func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
				if schedulerSameRecoveryCause(cause, first.Cause()) {
					close(entered)
					<-release
				}
				return result
			}
			type answer struct {
				report pc.StopReport
				err    error
			}
			done := make(chan answer, 1)
			confirmation, cancel := context.WithCancel(context.Background())
			defer cancel()
			collected := false
			t.Cleanup(func() {
				unblock()
				if !collected {
					select {
					case <-done:
					case <-time.After(5 * time.Second):
						t.Error("original confirmation did not return")
					}
				}
				v.store.after = nil
				v.pair.Stop()
			})
			go func() { r, e := v.stop(confirmation, true); done <- answer{r, e} }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("original readonly confirmation not reached")
			}
			v.pair.state.mu.Lock()
			retained := entry.unknown != nil && entry.unknown.AttemptID() == attempt
			v.pair.state.mu.Unlock()
			cancel() // Cancellation cannot replace the original confirmation return.
			if !retained {
				t.Fatal("physical confirmation replaced original Store return")
			}
			if _, _, n := v.store.counts(); n != writes {
				t.Fatal("confirmation wrote another row")
			}
			select {
			case <-done:
				collected = true
				t.Fatal("confirmation returned before original Store")
			default:
			}
			unblock()
			var response answer
			select {
			case response = <-done:
				collected = true
			case <-time.After(5 * time.Second):
				t.Fatal("confirmation did not return")
			}
			v.store.after = nil
			raw, ok = UnknownAttempt(response.err)
			if !ok || raw.AttemptID() != attempt || !schedulerSameRecoveryCause(raw.Cause(), first.Cause()) {
				t.Fatal("cancelled confirmation replaced first Unknown", response.err)
			}
			v.pair.state.mu.Lock()
			retained = entry.unknown != nil && entry.unknown.AttemptID() == attempt
			v.pair.state.mu.Unlock()
			if !retained {
				t.Fatal("cancelled confirmation released old custody")
			}
			response.report, response.err = v.stop(context.Background(), true)
			if _, _, n := v.store.counts(); n != writes {
				t.Fatal("retry confirmation wrote technical history")
			}
			if phase == "registration" {
				requireSchedulerStopState(t, response.report, response.err, pc.StopPending)
				before, _, _ := v.store.counts()
				// Both the public inspection and the mutex-protected private branch
				// must remain readonly after the old Unknown has already been cleared.
				report, e := v.stop(context.Background(), false)
				requireSchedulerStopState(t, report, e, pc.StopPending)
				if _, _, n := v.store.counts(); n != writes {
					t.Fatal("Inspect closed confirmed-running original call")
				}
				after, _, _ := v.store.counts()
				if after < before {
					t.Fatal("invalid counter")
				}
				before, _, _ = v.store.counts()
				if _, e = v.pair.state.settle(context.Background(), entry, nil, true); e != nil {
					t.Fatal(e)
				}
				after, _, _ = v.store.counts()
				if after != before {
					t.Fatal("readonly decision escaped owner lock")
				}
				report, e = v.stop(context.Background(), true)
				requireSchedulerStopState(t, report, e, pc.StopPending)
			} else {
				requireSchedulerStopState(t, response.report, response.err, pc.StopPending)
			}
			report, e := v.stop(context.Background(), false)
			requireSchedulerStopState(t, report, e, pc.Stopped)
			v.pair.Stop()
			if !v.pair.Joined() {
				t.Fatal("confirmed technical owner not retired")
			}
		})
	}
}

func schedulerSameRecoveryCause(a, b f.TransactionCause) bool {
	x, y := a.Details(), b.Details()
	return x.Kind == f.RecoveryCause && y.Kind == x.Kind && x.Owner == y.Owner && x.RecoveryRunID == y.RecoveryRunID && x.CheckpointRef == y.CheckpointRef
}

func (v *schedulerCallsFixture) foreignRow(t *testing.T, n int) projectCallRecord {
	t.Helper()
	identity, err := projectEntryIdentity(v.p, "visit_next", f.IdempotencyKey("foreign-visit"))
	if err != nil {
		t.Fatal(err)
	}
	r := projectCallRecord{ID: dispatchTestID[projectCallID](t, n+1000), Project: v.p, Process: dispatchTestID[oc.Process](t, 880), Instance: dispatchTestID[projectCallInstance](t, 882), Lane: projectLaneVisitor, Identity: identity.Canonical(), Digest: projectInvocationDigest(identity.Canonical()), Phase: "running"}
	if !r.valid() {
		t.Fatal("invalid controlled foreign row")
	}
	return r
}
func (v *schedulerCallsFixture) put(r projectCallRecord) {
	v.store.mu.Lock()
	v.store.rows[r.ID.String()] = r
	v.store.mu.Unlock()
}

func TestSchedulerProjectCallForeignProofAndSnapshot(t *testing.T) {
	v := newSchedulerCalls(t)
	for n := range 101 {
		row := v.foreignRow(t, n)
		row.Phase = "business_closed"
		v.put(row)
	}
	_, _, writes := v.store.counts()
	report, err := v.stop(context.Background(), false)
	requireSchedulerStopState(t, report, err, pc.Stopped)
	if _, _, n := v.store.counts(); n != writes {
		t.Fatal("Inspect wrote foreign history")
	}
	priorProofs := v.proof.count()
	entered, release := make(chan struct{}), make(chan struct{})
	var once, hold sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	v.store.after = func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
		if cause.Details().Owner == "scheduler-project-stop" {
			hold.Do(func() { close(entered); <-release })
		}
		return result
	}
	type answer struct {
		r pc.StopReport
		e error
	}
	done := make(chan answer, 1)
	collected := false
	t.Cleanup(func() {
		unblock()
		if !collected {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("foreign scan did not return")
			}
		}
		v.pair.Stop()
	})
	go func() { r, e := v.stop(context.Background(), true); done <- answer{r, e} }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("original foreign scan not reached")
	}
	if v.proof.count() != priorProofs {
		t.Fatal("foreign proof preceded original read return")
	}
	unblock()
	var result answer
	select {
	case result = <-done:
		collected = true
	case <-time.After(5 * time.Second):
		t.Fatal("foreign closure did not return")
	}
	v.store.after = nil
	requireSchedulerStopState(t, result.r, result.e, pc.Stopped)
	if _, _, n := v.store.counts(); n != writes {
		t.Fatal("foreign observation wrote technical history")
	}
	// Death does not prove the outcome of a foreign running business call.
	row := v.foreignRow(t, 101)
	v.put(row)
	report, err = v.stop(context.Background(), true)
	requireSchedulerStopState(t, report, err, pc.StopPending)
	if _, _, n := v.store.counts(); n != writes {
		t.Fatal("running foreign call was closed without its original outcome")
	}
	v.pair.Stop()
}

func TestSchedulerProjectCallForeignIdentityBoundaries(t *testing.T) {
	for _, name := range []string{"same-process-other-instance", "live-foreign", "changed-after-proof"} {
		t.Run(name, func(t *testing.T) {
			v := newSchedulerCalls(t)
			row := v.foreignRow(t, 1)
			row.Phase = "business_closed"
			if name == "same-process-other-instance" {
				row.Process = v.pair.state.process
				row.Phase = "business_closed"
			}
			v.put(row)
			if name == "live-foreign" {
				v.proof.confirm = func(context.Context, oc.ProcessID) error { return fault(f.ResourceBusy) }
			}
			if name == "changed-after-proof" {
				v.proof.confirm = func(context.Context, oc.ProcessID) error {
					changed := row
					changed.Instance = dispatchTestID[projectCallInstance](t, 889)
					v.put(changed)
					return nil
				}
			}
			report, err := v.stop(context.Background(), true)
			if name == "changed-after-proof" {
				if err == nil && report.Details().State == pc.Stopped {
					t.Fatal("changed identity reused foreign proof")
				}
			} else {
				requireSchedulerStopState(t, report, err, pc.StopPending)
			}
			if _, _, writes := v.store.counts(); writes != 0 {
				t.Fatal("unsupported identity changed technical rows")
			}
			v.pair.Stop()
		})
	}
}

func (p *schedulerCallProof) count() int { p.mu.Lock(); defer p.mu.Unlock(); return p.confirms }

func schedulerOwnedRunner(t *testing.T, base *runnerTestFixture, originalRunner *ProjectRunner, executor ec.AssociatedExecutor) (*schedulerCallsFixture, *ProjectRunner, *ProjectRunners) {
	t.Helper()
	a := base.coordinator.authority
	originalStore := a.store
	store := &schedulerCallStore{Store: originalStore, live: map[f.Tx][]f.LockRequest{}, rows: map[string]projectCallRecord{}}
	a.store = store
	projects := &schedulerCallProjects{store: store, current: func(p i.ProjectID) pc.SchedulerProject {
		return pc.SchedulerProject{Project: pc.ProjectRef{ID: p, Lifecycle: pc.Active, CurrentSprintID: base.project.sprint}, Config: pc.ProjectSchedulerConfig{Enabled: base.project.enabled}}
	}}
	proof := &schedulerCallProof{store: store}
	coordinatorDeps := base.coordinator.deps
	coordinatorDeps.Projects = projects
	handoffDeps := base.visitor.handoff.deps
	handoffDeps.Observations = coordinatorDeps.Executions
	busyDeps := base.visitor.busy.deps
	busyDeps.Projects = projects
	pair, err := newProjectCallOwnership(a, ProjectCallDependencies{Coordinator: coordinatorDeps, Handoff: handoffDeps, Busy: busyDeps, Failure: LaunchFailureFinalizerDependencies{Projects: projects, Work: &failureTestWork{}}, RelaunchTasks: &relaunchTestWork{}, Tasks: base.reader, Occupancy: &relaunchTestOccupancy{}, RelaunchSkipCount: 2}, proof, dispatchTestID[oc.Process](t, 881), &schedulerCallAuthority{store: store})
	if err != nil {
		t.Fatal(err)
	}
	options := base.options
	options.Relaunch = pair.Relaunch()
	runner, err := NewProjectRunnerWithExecutions(pair.Coordinator(), pair.Visitor(), options, executor, 1)
	if err != nil {
		t.Fatal(err)
	}
	// Keep the existing concrete page collector and original business Store;
	// the wrapper only adds the controlled technical table to that same Tx.
	runner.readPending = func(ctx context.Context, x postgres.SQLExecutor, p i.ProjectID) ([]traversalPending, error) {
		scope, ok := x.(schedulerCallSQL)
		if !ok || scope.owner != store {
			return nil, fault(f.Forbidden)
		}
		return originalRunner.readPending(ctx, originalStore, p)
	}
	runner.readLaunched = func(ctx context.Context, x postgres.SQLExecutor, p i.ProjectID, after, through string, limit int) ([]*dispatchRecord, error) {
		scope, ok := x.(schedulerCallSQL)
		if !ok || scope.owner != store {
			return nil, fault(f.Forbidden)
		}
		return originalRunner.readLaunched(ctx, originalStore, p, after, through, limit)
	}
	ready := make(chan struct{})
	close(ready)
	executions := schedulerCallExecutions{AssociatedExecutor: executor, ready: ready}
	manager, err := NewProjectRunners(ProjectRunnersDependencies{Projects: managedDirectory(func(context.Context, pc.SchedulerProjectPageRequest) (pc.SchedulerProjectPage, error) {
		return pc.SchedulerProjectPage{}, fault(f.Forbidden)
	}), Coordinator: pair.Coordinator(), Visitor: pair.Visitor(), Tasks: base.reader, Relaunch: pair.Relaunch(), Executions: executions}, ProjectRunnersOptions{MaxProjects: 1, ProjectPageSize: 1, ExecutionPageSize: 1, DiscoveryInterval: time.Hour, TickInterval: time.Millisecond, RelaunchSkipCount: 2, LaunchPolicy: emptyClaimPolicy()})
	if err != nil {
		t.Fatal(err)
	}
	v := &schedulerCallsFixture{pair: pair, store: store, proof: proof, p: options.ProjectID, b: dispatchTestID[i.Project](t, 200), cause: pc.LifecycleCause{OperationID: dispatchTestID[pc.Operation](t, 500), Action: pc.Delete, ProjectVersion: 2}}
	registration, err := i.RegisterService(i.ProjectLifecycle)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := i.InProject(v.p)
	if err != nil {
		t.Fatal(err)
	}
	v.actor, err = registration.Actor(v.cause.OperationID.String(), scope)
	if err != nil {
		t.Fatal(err)
	}
	return v, runner, manager
}

func TestSchedulerProjectCallManagerWaitsForOriginalAdvance(t *testing.T) {
	legacy := newRunnerExecutionControl(t, 1, time.Millisecond)
	legacy.add(t, 1, ec.Created)
	legacy.base.project.enabled, legacy.base.project.sprint = false, nil
	v, runner, manager := schedulerOwnedRunner(t, legacy.base, legacy.runner, legacy.executor)
	pair := v.pair
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	legacy.executor.onCall = func(context.Context) { close(entered); <-release }
	managed := &managedRunner{runner: runner, active: true}
	manager.projects[v.p] = managed
	done := make(chan struct{})
	collected := false
	t.Cleanup(func() {
		unblock()
		if !collected {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("original manager Advance did not return")
			}
		}
		manager.Stop()
		runner.Stop()
		pair.Stop()
	})
	go func() { manager.runOne(context.Background(), managed, nil); close(done) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("original associated Advance not reached")
	}
	report, err := v.stop(context.Background(), true)
	requireSchedulerStopState(t, report, err, pc.StopPending)
	if _, err = pair.Visitor().VisitNext(context.Background(), v.b, nil); err != nil {
		t.Fatal("A's borrowed Advance blocked B", err)
	}
	manager.mu.Lock()
	active := managed.active
	manager.mu.Unlock()
	if !active || runner.Joined() {
		t.Fatal("cancel replaced original manager/Advance return")
	}
	select {
	case <-done:
		collected = true
		t.Fatal("manager returned before original Advance")
	default:
	}
	unblock()
	select {
	case <-done:
		collected = true
	case <-time.After(5 * time.Second):
		t.Fatal("manager did not join original Advance")
	}
	manager.mu.Lock()
	active = managed.active
	originalErr := managed.err
	manager.mu.Unlock()
	if active || !errors.Is(originalErr, context.Canceled) {
		t.Fatal("manager lost original return", originalErr)
	}
	report, err = v.stop(context.Background(), true)
	requireSchedulerStopState(t, report, err, pc.Stopped)
	if len(legacy.executor.requests) != 1 || legacy.executor.stops != 0 || legacy.executor.drains != 0 || legacy.base.execution.launches != 0 {
		t.Fatal("stop started or globally stopped the borrowed executor")
	}
	manager.Stop()
	runner.Stop()
	pair.Stop()
	if !manager.Joined() || !runner.Joined() || !pair.Joined() {
		t.Fatal("original managed tails did not join")
	}
}

type schedulerCallExecutions struct {
	ec.AssociatedExecutor
	ready chan struct{}
}

func (v schedulerCallExecutions) Ready() <-chan struct{} { return v.ready }

func TestSchedulerProjectCallReborrowsOriginalAtCapacity(t *testing.T) {
	v := newSchedulerCalls(t)
	identity, err := projectEntryIdentity(v.p, "managed_project_run", "managed_project_runner")
	if err != nil {
		t.Fatal(err)
	}
	ctx, original, err := v.pair.state.admit(context.Background(), v.p, projectLaneRunner, identity)
	if err != nil {
		t.Fatal(err)
	}
	cause, err := f.NewRecoveryCause("scheduler-controlled-advance", original.record.ID.String(), v.p.String())
	if err != nil {
		t.Fatal(err)
	}
	unknown := commitError(f.UnknownResult(dispatchTestID[f.TransactionAttempt](t, 994), cause))
	if got := v.pair.state.finish(original, unknown); got != unknown {
		t.Fatal("original business Unknown replaced", got)
	}
	v.pair.state.returned(original)
	// Populate only inactive technical custody. This control proves bounded
	// admission, not successful recovery or a public execution outcome.
	v.pair.state.mu.Lock()
	for n := 1; n < maxProjectCallOwners; n++ {
		row := v.foreignRow(t, n)
		v.pair.state.owners[row.ID] = &projectCallEntry{record: row, returned: true, bodyReturned: true, cancel: func() {}}
	}
	v.pair.state.mu.Unlock()
	calls, _, writes := v.store.counts()
	reborrowed, current, err := v.pair.state.admit(context.Background(), v.p, projectLaneRunner, identity)
	if err != nil || current != original || reborrowed.Err() != nil || ctx.Err() == nil {
		t.Fatal("capacity blocked the original recovery owner", err)
	}
	v.pair.state.mu.Lock()
	count := len(v.pair.state.owners)
	v.pair.state.mu.Unlock()
	after, _, afterWrites := v.store.counts()
	if count != maxProjectCallOwners || calls != after || writes != afterWrites {
		t.Fatal("recovery allocated or registered another owner")
	}
	newIdentity, err := projectEntryIdentity(v.b, "visit_next", "new-at-capacity")
	if err != nil {
		t.Fatal(err)
	}
	if _, entry, err := v.pair.state.admit(context.Background(), v.b, projectLaneVisitor, newIdentity); !runnerHasCode(err, f.ResourceBusy) || entry != nil {
		t.Fatal("new admission escaped the same capacity", err)
	}
	_ = v.pair.state.finish(original, unknown)
	v.pair.state.returned(original)
	v.pair.Stop()
	// The synthetic inactive entries above have no real calls to join. They
	// are discarded with this isolated fixture, never reported as stopped.
}

type schedulerRecoveringExecutions struct {
	advance func(context.Context, i.ProjectID, ec.AssociatedDispatch) (ec.ExecutionAdvance, error)
}

func (e *schedulerRecoveringExecutions) Advance(ctx context.Context, p i.ProjectID, d ec.AssociatedDispatch) (ec.ExecutionAdvance, error) {
	return e.advance(ctx, p, d)
}

func TestSchedulerProjectCallManagerOriginalUnknownRecovery(t *testing.T) {
	legacy := newRunnerExecutionControl(t, 1, time.Millisecond)
	row := legacy.add(t, 1, ec.Created)
	legacy.base.project.enabled, legacy.base.project.sprint = false, nil
	cause, err := f.NewRecoveryCause("execution-controlled-advance", row.execution.String(), row.project.String())
	if err != nil {
		t.Fatal(err)
	}
	first := f.UnknownResult(dispatchTestID[f.TransactionAttempt](t, 995), cause)
	original := commitError(first)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	calls := 0
	executor := &schedulerRecoveringExecutions{}
	executor.advance = func(ctx context.Context, p i.ProjectID, d ec.AssociatedDispatch) (ec.ExecutionAdvance, error) {
		calls++
		if calls == 1 {
			return ec.ExecutionAdvance{}, original
		}
		if calls == 2 {
			close(entered)
			<-release
			return legacy.executor.Advance(ctx, p, d)
		}
		return ec.ExecutionAdvance{}, errors.New("unexpected third Advance")
	}
	v, runner, manager := schedulerOwnedRunner(t, legacy.base, legacy.runner, executor)
	managed := &managedRunner{runner: runner, active: true}
	manager.projects[v.p] = managed
	manager.runOne(context.Background(), managed, nil)
	prior := managed.err
	actual, ok := UnknownAttempt(prior)
	if !ok || actual.AttemptID() != first.AttemptID() || calls != 1 {
		t.Fatal("original manager did not retain Advance Unknown", prior)
	}
	// A Stop observation must never call Advance to guess this outcome.
	report, err := v.stop(context.Background(), false)
	if err == nil || !errors.Is(err, original) || report.Details().State == pc.Stopped || calls != 1 {
		t.Fatal("Stop fabricated recovery of the borrowed Advance", err)
	}
	v.pair.state.mu.Lock()
	var rootEntry *projectCallEntry
	for _, entry := range v.pair.state.owners {
		if entry.identity.Command() == "managed_project_run" {
			rootEntry = entry
		}
	}
	v.pair.state.mu.Unlock()
	if rootEntry == nil {
		t.Fatal("original managed custody missing")
	}
	// Recovery and the old technical close remain separate original actions.
	// If a new traversal is reached, cancel it at its real initial read.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	v.store.after = func(_ context.Context, c f.TransactionCause, result f.CommitResult) f.CommitResult {
		if calls >= 2 && c.Details().Kind == f.CommandsCause && c.Details().Primary.Command() == "capture_launched" {
			cancel()
		}
		return result
	}
	manager.mu.Lock()
	managed.active = true
	manager.mu.Unlock()
	done := make(chan struct{})
	collected := false
	t.Cleanup(func() {
		unblock()
		cancel()
		if !collected {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("original manager recovery did not return")
			}
		}
		manager.Stop()
		runner.Stop()
		v.pair.Stop()
	})
	go func() { manager.runOne(ctx, managed, prior); close(done) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("same original manager did not reach recovery Advance")
	}
	v.pair.state.mu.Lock()
	retained := v.pair.state.owners[rootEntry.record.ID] == rootEntry && !rootEntry.returned
	v.pair.state.mu.Unlock()
	if !retained || manager.Joined() || runner.Joined() {
		t.Fatal("physical result replaced original recovery return")
	}
	report, err = v.stop(context.Background(), false)
	if err == nil {
		requireSchedulerStopState(t, report, nil, pc.StopPending)
	} else if !errors.Is(err, original) {
		t.Fatal("inspection replaced the original unresolved Advance", err)
	}
	if calls != 2 {
		t.Fatal("inspection invoked borrowed Advance")
	}
	select {
	case <-done:
		collected = true
		t.Fatal("manager returned before original recovery Advance")
	default:
	}
	unblock()
	select {
	case <-done:
		collected = true
	case <-time.After(5 * time.Second):
		t.Fatal("recovery did not return")
	}
	v.store.after = nil
	if !runnerHasCode(managed.err, f.ResourceBusy) || uncertainProjectVisit(managed.err) || calls != 2 {
		t.Fatal("successful original recovery kept the old Unknown", managed.err)
	}
	if legacy.base.execution.launches != 0 || legacy.executor.stops != 0 || legacy.executor.drains != 0 {
		t.Fatal("recovery launched or globally stopped borrowed execution")
	}
	report, err = v.stop(context.Background(), true)
	requireSchedulerStopState(t, report, err, pc.Stopped)
	manager.Stop()
	runner.Stop()
	v.pair.Stop()
	if !manager.Joined() || !runner.Joined() || !v.pair.Joined() {
		t.Fatal("successful original recovery did not retire custody")
	}
}

func TestSchedulerProjectCallCrossMethodWaitsForOuterReturn(t *testing.T) {
	v := newSchedulerCalls(t)
	identity, err := projectEntryIdentity(v.p, "visit", "same-original-key")
	if err != nil {
		t.Fatal(err)
	}
	_, entry, err := v.pair.state.admit(context.Background(), v.p, projectLaneVisitor, identity)
	if err != nil {
		t.Fatal(err)
	}
	other, err := projectEntryIdentity(v.p, "visit_next", identity.Key())
	if err != nil {
		t.Fatal(err)
	}
	assertBusy := func() {
		t.Helper()
		before, _, writes := v.store.counts()
		if _, next, err := v.pair.state.admit(context.Background(), v.p, projectLaneVisitor, other); !runnerHasCode(err, f.ResourceBusy) || next != nil {
			t.Fatal("different method overtook the original snapshot/return", err)
		}
		after, _, afterWrites := v.store.counts()
		if before != after || writes != afterWrites {
			t.Fatal("cross-method refusal registered new custody")
		}
	}
	assertBusy() // Before original body/capture has returned.
	if err = v.pair.state.finish(entry, nil); err != nil {
		t.Fatal(err)
	}
	assertBusy() // Physical closure is not the original wrapper's return.
	_, b := v.admit(t, v.b, "visit_next")
	v.close(t, b)
	v.pair.state.returned(entry)
	_, next, err := v.pair.state.admit(context.Background(), v.p, projectLaneVisitor, other)
	if err != nil || next == nil {
		t.Fatal("actual original return did not release the method gate", err)
	}
	v.close(t, next)
	v.pair.Stop()
	if !v.pair.Joined() {
		t.Fatal("actual local tails not joined")
	}
}

func TestSchedulerProjectCallChildConfirmationBeforeParentCapture(t *testing.T) {
	v := newSchedulerCalls(t)
	parentIdentity, err := projectEntryIdentity(v.p, "project_run", "project_runner")
	if err != nil {
		t.Fatal(err)
	}
	ctx, parent, err := v.pair.state.admit(context.Background(), v.p, projectLaneRunner, parentIdentity)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := projectEntryIdentity(v.p, "visit_next", "nested-visitor")
	if err != nil {
		t.Fatal(err)
	}
	attempt := dispatchTestID[f.TransactionAttempt](t, 997)
	v.store.after = func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
		if cause.Details().Owner == "scheduler-call-register" && result.State() == f.Committed {
			return f.UnknownResult(attempt, cause)
		}
		return result
	}
	_, child, original := v.pair.state.admit(ctx, v.p, projectLaneVisitor, identity)
	v.store.after = nil
	if raw, ok := UnknownAttempt(original); !ok || raw.AttemptID() != attempt || child == nil {
		t.Fatal("original nested registration did not reach Unknown", original)
	}
	if got := v.pair.state.finish(child, original); got != original {
		t.Fatal("original child error replaced", got)
	}
	v.pair.state.returned(child)
	// The real child returned Unknown while its parent body is still active.
	// Confirming/closing that child must not erase the parent's original
	// return evidence. No childOutcomes or other receipt is fabricated here.
	_, _, writes := v.store.counts()
	report, err := v.stop(context.Background(), false)
	requireSchedulerStopState(t, report, err, pc.StopPending)
	if _, _, n := v.store.counts(); n != writes {
		t.Fatal("child confirmation wrote before parent capture")
	}
	report, err = v.stop(context.Background(), true)
	requireSchedulerStopState(t, report, err, pc.StopPending)
	if got := v.pair.state.finish(parent, original); got != original {
		t.Fatal("parent did not preserve its actual child result", got)
	}
	v.pair.state.returned(parent)
	// The child's immutable actual-return receipt identifies the parent
	// outcome even after the child's mutable Unknown and row custody retire.
	_, _, writes = v.store.counts()
	report, err = v.stop(context.Background(), false)
	requireSchedulerStopState(t, report, err, pc.StopPending)
	if _, _, n := v.store.counts(); n != writes {
		t.Fatal("parent confirmation wrote another outcome")
	}
	report, err = v.stop(context.Background(), true)
	requireSchedulerStopState(t, report, err, pc.StopPending)
	report, err = v.stop(context.Background(), false)
	requireSchedulerStopState(t, report, err, pc.Stopped)
	v.pair.Stop()
	if !v.pair.Joined() {
		t.Fatal("confirmed child lost its parent's original custody")
	}
}

func TestSchedulerProjectCallStoppedOriginalResolve(t *testing.T) {
	v := newSchedulerCalls(t)
	_, business, work, request := testClaimSetup(t)
	v.store.Store = business
	v.pair.Coordinator().deps.Claims = work
	attempt := dispatchTestID[f.TransactionAttempt](t, 998)
	id, err := f.ParseID[DispatchIdentity](request.DispatchID)
	if err != nil {
		t.Fatal(err)
	}
	command, err := claimCommand(request.ProjectID, id)
	if err != nil {
		t.Fatal(err)
	}
	cause, err := f.NewCommandsCause(command)
	if err != nil {
		t.Fatal(err)
	}
	phase := 0
	business.within = func(ctx context.Context, _ f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
		phase++
		// Reuse the original claim commit-boundary control. It does not
		// manufacture Work applied proof or claim successful Work SQL.
		if phase == 1 {
			return f.CommittedResult()
		}
		if phase == 2 {
			return f.UnknownResult(attempt, cause)
		}
		business.tx = f.NewTx()
		defer func() { business.tx = f.Tx{} }()
		if err := fn(ctx, business.tx); err != nil {
			return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted).WithCause(err))
		}
		return f.CommittedResult()
	}
	_, original := v.pair.Coordinator().ClaimTask(context.Background(), request, emptyClaimPolicy())
	if raw, ok := UnknownAttempt(original); !ok || raw.AttemptID() != attempt || work.called != 1 {
		t.Fatal("original claim Unknown not reached", original)
	}
	v.pair.Stop()
	if v.pair.Joined() {
		t.Fatal("global Stop discarded original claim")
	}
	v.store.mu.Lock()
	registered := len(v.store.rows)
	v.store.mu.Unlock()
	if _, err := v.pair.Coordinator().ResolveClaim(context.Background(), request); !errors.Is(err, original) || v.pair.Joined() {
		t.Fatal("missing receipt became negative proof", err)
	}
	if _, err := v.pair.Coordinator().ClaimTask(context.Background(), request, emptyClaimPolicy()); err == nil || work.called != 1 {
		t.Fatal("global Stop admitted another mutation", err)
	}
	row := dispatchTestRecord(t, 1, Pending)
	row.outcome, row.attempts = NotSent, 0
	row.guard = &ClaimGuard{TaskID: request.TaskID.String(), ClaimedVersion: 2, SourceState: "todo", SourceAssigneeID: request.AgentID, SourcePriority: "high", SourceSprintID: request.CurrentSprintID.String(), SourceOrderGeneration: 1}
	business.row = dispatchTestRow{values: recordValues(t, row)}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	var readContext context.Context
	v.store.after = func(ctx context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
		if cause.Details().Kind == f.CommandsCause && result.State() == f.Committed {
			readContext = ctx
			close(entered)
			<-release
		}
		return result
	}
	type answer struct {
		out Dispatch
		err error
	}
	done := make(chan answer, 1)
	collected := false
	t.Cleanup(func() {
		unblock()
		if !collected {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("original stopped Resolve did not return")
			}
		}
		v.pair.Stop()
	})
	go func() {
		out, err := v.pair.Coordinator().ResolveClaim(context.Background(), request)
		done <- answer{out, err}
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("old stopped owner could not reach original positive read")
	}
	v.pair.Stop() // Repeated global Stop must not cancel this permitted recovery.
	if readContext == nil || readContext.Err() != nil {
		t.Fatal("repeated Stop cancelled the original read-only recovery")
	}
	if v.pair.Joined() {
		t.Fatal("physical positive read substituted original return")
	}
	select {
	case <-done:
		collected = true
		t.Fatal("Resolve returned before its original Store")
	default:
	}
	unblock()
	select {
	case result := <-done:
		collected = true
		if result.err != nil || result.out.Summary().ID != id {
			t.Fatal("old stopped owner could not finish", result.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("original Resolve did not return")
	}
	v.store.after = nil
	v.store.mu.Lock()
	finalRows := len(v.store.rows)
	v.store.mu.Unlock()
	if finalRows != registered || work.called != 1 {
		t.Fatal("original resolution created another call or Work operation")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := v.pair.Drain(ctx); err != nil || !v.pair.Joined() {
		t.Fatal("actual original Resolve could not retire stopped custody", err)
	}
}
