package secret

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

// Each original transaction borrows the existing opaque-Tx/Scan control. Its
// live lifetime ends before the optional Store-return barrier. These are SQL
// and Project-authority controls, not PostgreSQL rollback or persisted claims.
type cleanupUnknownStore struct {
	Store
	mu     sync.Mutex
	live   map[f.Tx]*projectVariableMaintenanceStore
	causes []f.TransactionCause
	before func(context.Context, f.TransactionCause) *f.CommitResult
	after  func(context.Context, f.TransactionCause, f.CommitResult) f.CommitResult
	read   func(f.Tx, string, []any) postgres.Row
	exec   func(f.Tx, string, []any) (pgconn.CommandTag, error)
}

func (s *cleanupUnknownStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.mu.Lock()
	s.causes = append(s.causes, cause)
	before, after := s.before, s.after
	s.mu.Unlock()
	if before != nil {
		if result := before(ctx, cause); result != nil {
			return *result
		}
	}
	tx := f.NewTx()
	inner := &projectVariableMaintenanceStore{projectAuditUnitStore: &projectAuditUnitStore{tx: tx}}
	inner.read = func(query string, args []any) postgres.Row { return s.read(tx, query, args) }
	inner.exec = func(query string, args []any) (pgconn.CommandTag, error) { return s.exec(tx, query, args) }
	s.mu.Lock()
	s.live[tx] = inner
	s.mu.Unlock()
	err := fn(ctx, tx)
	s.mu.Lock()
	delete(s.live, tx)
	s.mu.Unlock()
	result := f.CommittedResult()
	if err != nil {
		var problem *f.Fault
		if !errors.As(err, &problem) {
			problem = f.NewFault(f.DependencyUnavailable, f.NotCommitted).WithCause(err)
		}
		result = f.NotCommittedResult(problem)
	}
	if after != nil {
		return after(ctx, cause, result)
	}
	return result
}
func (s *cleanupUnknownStore) session(tx f.Tx) (*projectVariableMaintenanceStore, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inner := s.live[tx]
	if !tx.Valid() || inner == nil {
		return nil, f.NewFault(f.Forbidden, f.NotCommitted)
	}
	return inner, nil
}
func (s *cleanupUnknownStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	inner, err := s.session(tx)
	if err != nil {
		return nil, err
	}
	return inner.InTx(tx)
}
func (s *cleanupUnknownStore) Acquire(ctx context.Context, tx f.Tx, key f.LockKey, mode f.LockMode) error {
	return s.AcquireAll(ctx, tx, []f.LockRequest{{Key: key, Mode: mode}})
}
func (s *cleanupUnknownStore) AcquireAll(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	inner, err := s.session(tx)
	if err != nil {
		return err
	}
	return inner.AcquireAll(ctx, tx, locks)
}
func (s *cleanupUnknownStore) RequireHeldLocks(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	inner, err := s.session(tx)
	if err != nil {
		return err
	}
	return inner.RequireHeldLocks(ctx, tx, locks)
}

type cleanupUnknownProject struct {
	actor i.Actor
	cause sc.LifecycleCause
	// Only the released lease is mutable in this fixture. Other SQL controls
	// report an empty, well-formed set; no encrypted material is constructed.
	released bool
	checks   int
	reads    int
	writes   int
}
type cleanupUnknownFixture struct {
	sc.ProjectAuthority
	store      *cleanupUnknownStore
	state      *serviceState
	service    *Service
	mu         sync.Mutex
	projects   map[i.ProjectID]*cleanupUnknownProject
	authorized map[f.Tx]i.ProjectID
	deny       bool
}

func newCleanupUnknownFixture(t *testing.T) (*cleanupUnknownFixture, i.ProjectID) {
	t.Helper()
	v := &cleanupUnknownFixture{projects: make(map[i.ProjectID]*cleanupUnknownProject), authorized: make(map[f.Tx]i.ProjectID)}
	v.store = &cleanupUnknownStore{live: make(map[f.Tx]*projectVariableMaintenanceStore)}
	v.state = &serviceState{store: v.store, auth: Authorizations{Projects: v}}
	v.service = &Service{data: func() *serviceState { return v.state }}
	v.store.read, v.store.exec = v.read, v.exec
	project := v.addProject(t)
	t.Cleanup(func() {
		// Tests release and Wait their original goroutines before this cleanup.
		// Resolve any retained attempt through CleanupProject, never clear maps.
		v.store.mu.Lock()
		v.store.before, v.store.after = nil, nil
		v.store.mu.Unlock()
		v.mu.Lock()
		v.deny = false
		v.mu.Unlock()
		v.state.cleanupMu.Lock()
		pending := make([]i.ProjectID, 0)
		for p, owner := range v.state.cleanupCalls {
			if owner.active {
				t.Error("original cleanup call still active")
			} else if owner.retained {
				pending = append(pending, p)
			}
		}
		v.state.cleanupMu.Unlock()
		for _, p := range pending {
			if _, err := v.run(context.Background(), v.service, p); err != nil {
				t.Error("original Unknown did not settle", err)
			}
		}
	})
	return v, project
}
func (v *cleanupUnknownFixture) addProject(t *testing.T) i.ProjectID {
	t.Helper()
	project := projectAuditID[i.Project](t)
	scope, _ := i.InProject(project)
	operation := projectAuditID[sc.LifecycleOperation](t)
	cause, _ := sc.NewLifecycleCause(operation, 7, true)
	registration, _ := i.RegisterService(i.ProjectLifecycle)
	actor, err := registration.Actor(operation.String(), scope)
	if err != nil {
		t.Fatal(err)
	}
	v.projects[project] = &cleanupUnknownProject{actor: actor, cause: cause}
	return project
}
func (v *cleanupUnknownFixture) CheckCleanupInTx(ctx context.Context, tx f.Tx, actor i.Actor, cause sc.LifecycleCause, project i.ProjectID) error {
	key, _ := f.ProjectLock(project.String())
	if err := v.store.RequireHeldLocks(ctx, tx, []f.LockRequest{{Key: key, Mode: f.Exclusive}}); err != nil {
		return err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	p := v.projects[project]
	if p == nil || !p.actor.Equal(actor) || p.cause.Details() != cause.Details() {
		return f.NewFault(f.Forbidden, f.NotCommitted)
	}
	p.checks++
	if v.deny {
		return f.NewFault(f.Forbidden, f.NotCommitted)
	}
	v.authorized[tx] = project
	return ctx.Err()
}
func (v *cleanupUnknownFixture) facts(tx f.Tx, args []any) (*cleanupUnknownProject, error) {
	if _, err := v.store.InTx(tx); err != nil {
		return nil, err
	}
	project, ok := v.authorized[tx]
	if !ok || len(args) != 1 || args[0] != project.String() {
		return nil, errors.New("SQL without the original current Project gate")
	}
	return v.projects[project], nil
}
func (v *cleanupUnknownFixture) read(tx f.Tx, query string, args []any) postgres.Row {
	v.mu.Lock()
	defer v.mu.Unlock()
	p, err := v.facts(tx, args)
	if err != nil {
		return projectAuditUnitRow{err: err}
	}
	p.reads++
	switch {
	case strings.HasPrefix(query, "SELECT EXISTS(SELECT 1 FROM agenteam_secret.secret_references"):
		return projectAuditUnitRow{values: []any{false}}
	case strings.Contains(query, "LEFT JOIN agenteam_secret.secret_payloads"):
		return projectAuditUnitRow{values: []any{false}}
	case strings.HasPrefix(query, "SELECT NOT EXISTS"):
		// The independently retained tombstone is visible only when completion
		// includes all leases, not just the already-checked active lease set.
		includesReleased := strings.Contains(query, "NOT EXISTS(SELECT 1 FROM agenteam_secret.secret_leases WHERE project_id=$1)")
		return projectAuditUnitRow{values: []any{!p.released || !includesReleased}}
	default:
		return projectAuditUnitRow{err: errors.New("unexpected cleanup projection")}
	}
}
func (v *cleanupUnknownFixture) exec(tx f.Tx, query string, args []any) (pgconn.CommandTag, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	p, err := v.facts(tx, args)
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	p.writes++
	if strings.HasPrefix(query, "DELETE FROM agenteam_secret.secret_leases") {
		if !strings.Contains(query, "AND released") {
			return pgconn.CommandTag{}, errors.New("cleanup omitted released lease guard")
		}
		p.released = false
	}
	return pgconn.NewCommandTag("DELETE 0"), nil
}
func (v *cleanupUnknownFixture) run(ctx context.Context, service *Service, project i.ProjectID) (sc.CleanupReport, error) {
	p := v.projects[project]
	return service.CleanupProject(ctx, p.actor, p.cause, project, nil)
}
func (v *cleanupUnknownFixture) retained(project i.ProjectID) bool {
	v.state.cleanupMu.Lock()
	defer v.state.cleanupMu.Unlock()
	owner := v.state.cleanupCalls[project]
	return owner != nil && owner.retained
}
func cleanupUnknownWait[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case out := <-ch:
		return out
	case <-time.After(5 * time.Second):
		t.Fatal("original cleanup call did not return")
		var zero T
		return zero
	}
}
func requireCleanupUnknown(t *testing.T, report sc.CleanupReport, err error, original f.CommitResult) {
	t.Helper()
	var fault *f.Fault
	var private *projectCleanupCommitFailure
	if report != (sc.CleanupReport{}) || !errors.As(err, &fault) || fault.Code != f.CommitUnknown || fault.CommitState != f.Unknown || fault.CauseID != original.AttemptID().String() || fault.RetryHint != "lookup" || !errors.As(err, &private) || private.result.AttemptID() != original.AttemptID() || !reflect.DeepEqual(private.result.Cause().Details(), original.Cause().Details()) {
		t.Fatal("cleanup lost the original Unknown attempt or cause", err)
	}
}
func (v *cleanupUnknownFixture) unknown(t *testing.T, project i.ProjectID) f.CommitResult {
	t.Helper()
	attempt := projectAuditID[f.TransactionAttempt](t)
	var original f.CommitResult
	v.store.after = func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
		if result.State() != f.Committed {
			return result
		}
		original = f.UnknownResult(attempt, cause)
		return original
	}
	report, err := v.run(context.Background(), v.service, project)
	v.store.after = nil
	requireCleanupUnknown(t, report, err, original)
	if !v.retained(project) {
		t.Fatal("Unknown ownership was released")
	}
	return original
}

func TestProjectCleanupUnknownWaitsForOriginalReturns(t *testing.T) {
	v, project := newCleanupUnknownFixture(t)
	otherProject := v.addProject(t)
	copyService := *v.service
	attempt := projectAuditID[f.TransactionAttempt](t)
	var original f.CommitResult
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	v.store.after = func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
		if cause.Details().RecoveryRunID == v.projects[project].cause.Details().OperationID.String() {
			original = f.UnknownResult(attempt, cause)
			close(entered)
			<-release
			return original
		}
		return result
	}
	type output struct {
		report sc.CleanupReport
		err    error
	}
	done := make(chan output, 1)
	joined := false
	t.Cleanup(func() {
		unblock()
		if !joined {
			cleanupUnknownWait(t, done)
		}
	})
	go func() { r, err := v.run(context.Background(), v.service, project); done <- output{r, err} }()
	cleanupUnknownWait(t, entered)
	if _, err := v.run(context.Background(), &copyService, project); err == nil {
		t.Fatal("copied Service borrowed the still-active original")
	} else {
		projectAuditCode(t, err, f.ResourceBusy)
	}
	if r, err := v.run(context.Background(), &copyService, otherProject); err != nil || !r.Completed {
		t.Fatal("held Project A stopped Project B cleanup", err)
	}
	select {
	case <-done:
		joined = true
		t.Fatal("cleanup returned before the original Store")
	default:
	}
	unblock()
	out := cleanupUnknownWait(t, done)
	joined = true
	v.store.after = nil
	requireCleanupUnknown(t, out.report, out.err, original)
	if !v.retained(project) {
		t.Fatal("original Store return discarded Unknown")
	}
	wrong, _ := sc.NewLifecycleCause(v.projects[project].cause.Details().OperationID, 8, true)
	before := len(v.store.causes)
	if _, err := copyService.CleanupProject(context.Background(), v.projects[project].actor, wrong, project, nil); err == nil {
		t.Fatal("another cause borrowed the retained original")
	} else {
		projectAuditCode(t, err, f.InvalidState)
	}
	if len(v.store.causes) != before {
		t.Fatal("mismatched cause opened a transaction")
	}
	// A second explicit call is read-only and keeps the old attempt through its
	// own original Store return, even though its callback has already committed.
	confirmEntered, confirmRelease := make(chan struct{}), make(chan struct{})
	var confirmOnce sync.Once
	confirmUnblock := func() { confirmOnce.Do(func() { close(confirmRelease) }) }
	confirmDone := make(chan output, 1)
	confirmJoined := false
	t.Cleanup(func() {
		confirmUnblock()
		if !confirmJoined {
			cleanupUnknownWait(t, confirmDone)
		}
	})
	p := v.projects[project]
	writes, checks := p.writes, p.checks
	v.store.after = func(_ context.Context, _ f.TransactionCause, result f.CommitResult) f.CommitResult {
		close(confirmEntered)
		<-confirmRelease
		return result
	}
	go func() { r, err := v.run(context.Background(), &copyService, project); confirmDone <- output{r, err} }()
	cleanupUnknownWait(t, confirmEntered)
	if !v.retained(project) || p.writes != writes || p.checks != checks+1 {
		t.Fatal("confirmation skipped current gate, wrote data, or released early")
	}
	confirmUnblock()
	out = cleanupUnknownWait(t, confirmDone)
	confirmJoined = true
	v.store.after = nil
	if out.err != nil || !out.report.Completed || v.retained(project) || p.writes != writes {
		t.Fatal("actual read-only confirmation return did not settle original", out.err)
	}
}

func TestProjectCleanupUnknownConfirmationKeepsOriginal(t *testing.T) {
	for _, mode := range []string{"current-gate-denied", "unknown", "not-committed", "cancelled-before", "cancelled-after-callback", "skipped-callback"} {
		t.Run(mode, func(t *testing.T) {
			v, project := newCleanupUnknownFixture(t)
			original := v.unknown(t, project)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			secondAttempt := projectAuditID[f.TransactionAttempt](t)
			switch mode {
			case "current-gate-denied":
				v.deny = true
			case "unknown":
				v.store.after = func(_ context.Context, cause f.TransactionCause, _ f.CommitResult) f.CommitResult {
					return f.UnknownResult(secondAttempt, cause)
				}
			case "not-committed":
				v.store.after = func(context.Context, f.TransactionCause, f.CommitResult) f.CommitResult {
					return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted))
				}
			case "cancelled-before":
				cancel()
			case "cancelled-after-callback":
				v.store.after = func(_ context.Context, _ f.TransactionCause, result f.CommitResult) f.CommitResult {
					cancel()
					return result
				}
			case "skipped-callback":
				v.store.before = func(context.Context, f.TransactionCause) *f.CommitResult {
					result := f.CommittedResult()
					return &result
				}
			}
			p := v.projects[project]
			writes, reads, checks, transactions := p.writes, p.reads, p.checks, len(v.store.causes)
			report, err := v.run(ctx, v.service, project)
			requireCleanupUnknown(t, report, err, original)
			if !v.retained(project) || p.writes != writes {
				t.Fatal("failed confirmation lost or repeated original mutation")
			}
			if mode == "cancelled-before" && len(v.store.causes) != transactions {
				t.Fatal("pre-cancelled confirmation opened a transaction")
			}
			if mode == "current-gate-denied" && (p.checks != checks+1 || p.reads != reads) {
				t.Fatal("confirmation read through a rejected current gate")
			}
			v.store.before, v.store.after, v.deny = nil, nil, false
			report, err = v.run(context.Background(), v.service, project)
			if err != nil || !report.Completed || v.retained(project) || p.writes != writes {
				t.Fatal("later explicit confirmation did not recover original", err)
			}
		})
	}
}

func TestProjectCleanupUnknownRejectsFabricatedCommitted(t *testing.T) {
	v, project := newCleanupUnknownFixture(t)
	v.store.before = func(context.Context, f.TransactionCause) *f.CommitResult {
		result := f.CommittedResult()
		return &result
	}
	report, err := v.run(context.Background(), v.service, project)
	if err == nil || report != (sc.CleanupReport{}) || v.retained(project) || v.projects[project].checks != 0 || v.projects[project].writes != 0 {
		t.Fatal("unexecuted callback fabricated cleanup progress", err)
	}
	projectAuditCode(t, err, f.DependencyUnavailable)
}

func TestProjectCleanupUnknownReleasedLeaseNeedsLaterMutation(t *testing.T) {
	v, project := newCleanupUnknownFixture(t)
	p := v.projects[project]
	p.released = true
	attempt := projectAuditID[f.TransactionAttempt](t)
	var original f.CommitResult
	// The original controlled transaction never executes its callback. This
	// preserves the tombstone without claiming to simulate a physical rollback.
	v.store.before = func(_ context.Context, cause f.TransactionCause) *f.CommitResult {
		original = f.UnknownResult(attempt, cause)
		return &original
	}
	report, err := v.run(context.Background(), v.service, project)
	requireCleanupUnknown(t, report, err, original)
	v.store.before = nil
	report, err = v.run(context.Background(), v.service, project)
	if err != nil || report.Completed || !p.released || p.writes != 0 || v.retained(project) || report.Checkpoint.ProjectID != project || report.Checkpoint.OperationID != p.cause.Details().OperationID {
		t.Fatal("read-only confirmation lost or ignored released lease", err)
	}
	report, err = v.run(context.Background(), v.service, project)
	if err != nil || !report.Completed || p.released || p.writes == 0 {
		t.Fatal("later explicit mutation did not remove released lease", err)
	}
}
