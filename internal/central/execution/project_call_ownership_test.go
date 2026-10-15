package execution

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

// These are controlled technical rows and upstream grants, not PostgreSQL or
// real process death. Original callbacks, live Tx/lock checks and Store returns
// are exercised. The existing Launch fixture supplies its original business
// SQL; the asynchronous physical-call seam never invokes a Model.
type executionCallStore struct {
	Store
	mu                   sync.Mutex
	live                 map[f.Tx][]f.LockRequest
	rows                 map[string]projectCallRecord
	calls, reads, writes int
	after                func(context.Context, f.TransactionCause, f.CommitResult) f.CommitResult
}

func (s *executionCallStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	d := cause.Details()
	technical := d.Kind == f.RecoveryCause && (strings.HasPrefix(d.Owner, "execution-call-") || d.Owner == "execution-project-stop")
	var result f.CommitResult
	if !technical {
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
	s.mu.Lock()
	after := s.after
	s.mu.Unlock()
	if after != nil {
		return after(ctx, cause, result)
	}
	return result
}
func (s *executionCallStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	s.mu.Lock()
	_, live := s.live[tx]
	s.mu.Unlock()
	if live {
		return s, nil
	}
	x, err := s.Store.InTx(tx)
	if err != nil {
		return nil, err
	}
	return executionCallSQL{s, x}, nil
}
func (s *executionCallStore) AcquireAll(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	s.mu.Lock()
	_, live := s.live[tx]
	if live {
		s.live[tx] = append([]f.LockRequest(nil), locks...)
	}
	s.mu.Unlock()
	if live {
		return ctx.Err()
	}
	return s.Store.AcquireAll(ctx, tx, locks)
}
func (s *executionCallStore) RequireHeldLocks(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	s.mu.Lock()
	held, live := s.live[tx]
	s.mu.Unlock()
	if !live {
		return s.Store.RequireHeldLocks(ctx, tx, locks)
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

type executionCallSQL struct {
	owner    *executionCallStore
	original postgres.SQLExecutor
}

func (x executionCallSQL) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(query, "agenteam_execution.project_calls") {
		return x.owner.Exec(ctx, query, args...)
	}
	return x.original.Exec(ctx, query, args...)
}
func (x executionCallSQL) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	if strings.Contains(query, "agenteam_execution.project_calls") {
		return x.owner.QueryRow(ctx, query, args...)
	}
	return x.original.QueryRow(ctx, query, args...)
}
func (x executionCallSQL) Query(ctx context.Context, query string, args ...any) (*postgres.Rows, error) {
	return x.original.Query(ctx, query, args...)
}
func (s *executionCallStore) Query(context.Context, string, ...any) (*postgres.Rows, error) {
	return nil, fault(f.Forbidden)
}
func (s *executionCallStore) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	if !strings.Contains(query, "agenteam_execution.project_calls") {
		return s.Store.QueryRow(ctx, query, args...)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads++
	var value any
	switch {
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
		r, ok := s.rows[args[0].(string)]
		if !ok {
			return launchScan{err: pgx.ErrNoRows}
		}
		value = r
	default:
		return launchScan{err: errors.New("unexpected technical read")}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return launchScan{err: err}
	}
	return launchScan{values: []any{raw}}
}
func (s *executionCallStore) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.HasPrefix(query, "INSERT INTO agenteam_execution.project_calls") && len(args) == 7 {
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
	if !strings.HasPrefix(query, "UPDATE agenteam_execution.project_calls SET phase='business_closed'") || len(args) != 7 {
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
func (s *executionCallStore) counts() (int, int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls, s.reads, s.writes
}
func (s *executionCallStore) setAfter(fn func(context.Context, f.TransactionCause, f.CommitResult) f.CommitResult) {
	s.mu.Lock()
	s.after = fn
	s.mu.Unlock()
}

type executionCallProof struct {
	store   *executionCallStore
	confirm func(context.Context, oc.ProcessID) error
}

func (p *executionCallProof) RequireCurrentProcessInTx(ctx context.Context, tx f.Tx, _ oc.ProcessID) error {
	_, err := p.store.InTx(tx)
	if err != nil {
		return err
	}
	return ctx.Err()
}
func (p *executionCallProof) ConfirmStopped(ctx context.Context, id oc.ProcessID) error {
	p.store.mu.Lock()
	live := len(p.store.live)
	p.store.mu.Unlock()
	if live != 0 {
		return fault(f.Forbidden)
	}
	if p.confirm != nil {
		return p.confirm(ctx, id)
	}
	return fault(f.ResourceBusy)
}

type executionCallLoanKey struct{}
type executionCallAuthority struct{ store *executionCallStore }

func (a *executionCallAuthority) WithProjectStop(ctx context.Context, _ i.Actor, _ pc.LifecycleCause, _ pc.ScopeRef, fn func(context.Context) error) error {
	return fn(context.WithValue(ctx, executionCallLoanKey{}, a))
}
func (a *executionCallAuthority) RequireProjectStopInTx(ctx context.Context, tx f.Tx, _ i.Actor, _ pc.LifecycleCause, scope pc.ScopeRef) error {
	if ctx.Value(executionCallLoanKey{}) != a {
		return fault(f.Forbidden)
	}
	return a.store.RequireHeldLocks(ctx, tx, projectCallLocks(scope.ProjectID))
}

type executionCallProjects struct {
	pc.ProjectAuthority
	store *executionCallStore
}

func (p *executionCallProjects) RequireOwnerInTx(ctx context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, _ i.AccessIntent) (pc.ProjectAccess, error) {
	if err := p.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(project)}); err != nil {
		return pc.ProjectAccess{}, err
	}
	at, _ := f.NewInstant(time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC))
	user, err := f.ParseID[i.User](actor.Details().UserID)
	if err != nil {
		return pc.ProjectAccess{}, err
	}
	return pc.NewProjectAccess(actor, pc.ProjectRef{ID: project, OwnerUserID: user, Name: "Execution-Control", NormalizedName: "execution-control", Lifecycle: pc.Active, Version: 1, CreatedAt: at, UpdatedAt: at}, at)
}
func (p *executionCallProjects) RequirePreparingProjectInTx(ctx context.Context, tx f.Tx, project i.ProjectID) (pc.ProjectRef, error) {
	return pc.ProjectRef{ID: project, Lifecycle: pc.Active}, p.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(project)})
}

type executionCallsFixture struct {
	pair   *ProjectCallOwnership
	store  *executionCallStore
	proof  *executionCallProof
	launch *launchControl
	actor  i.Actor
	cause  pc.LifecycleCause
	p, b   i.ProjectID
}

func newExecutionCalls(t *testing.T) *executionCallsFixture {
	t.Helper()
	seed, _ := newExecutionEntryPair(t)
	x := newLaunchControl(t)
	store := &executionCallStore{Store: x.store, live: map[f.Tx][]f.LockRequest{}, rows: map[string]projectCallRecord{}}
	projects := &executionCallProjects{store: store}
	authority, err := NewAuthority(store, projects, nil)
	if err != nil {
		t.Fatal(err)
	}
	x.authority = authority
	direct := seed.Direct().state.deps
	direct.Projects, direct.Agents = projects, x
	deps := ProjectCallDependencies{Service: Dependencies{Authority: authority, Agents: x, Task: x},
		Preparation: PreparationDependencies{Agents: x, Projects: projects, Processes: direct.Processes},
		Direct:      direct, Associated: AssociatedExecutorOptions{MaxOwned: 4, RecoveryInterval: time.Hour}}
	proof := &executionCallProof{store: store}
	pair, err := newProjectCallOwnership(store, deps, proof, seed.state.process, &executionCallAuthority{store})
	if err != nil {
		t.Fatal(err)
	}
	v := &executionCallsFixture{pair: pair, store: store, proof: proof, launch: x, p: x.request.ProjectID, b: newTestID[i.Project](t), cause: pc.LifecycleCause{OperationID: newTestID[pc.Operation](t), Action: pc.Delete, ProjectVersion: 2}}
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
	t.Cleanup(func() {
		store.setAfter(nil)
		pair.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := pair.Drain(ctx); err != nil || !pair.Joined() {
			t.Error("controlled original ownership remains", err)
		}
	})
	return v
}
func (v *executionCallsFixture) stop(ctx context.Context, request bool) (pc.StopReport, error) {
	scope := pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: v.p}
	if request {
		return v.pair.RequestProjectStop(ctx, v.actor, v.cause, scope)
	}
	return v.pair.InspectProjectStop(ctx, v.actor, v.cause, scope)
}
func (v *executionCallsFixture) lookup(ctx context.Context, p i.ProjectID, key string) (ec.LaunchLookup, error) {
	r := v.launch.request.Clone()
	r.ProjectID, r.Meta.IdempotencyKey = p, f.IdempotencyKey(key)
	digest, err := r.Digest()
	if err != nil {
		return ec.LaunchLookup{}, err
	}
	return v.pair.Service().LookupLaunch(ctx, v.launch.actor, ec.LaunchLookupKey{ProjectID: p, AgentID: r.AgentID, IdempotencyKey: r.Meta.IdempotencyKey}, digest)
}
func requireExecutionStopState(t *testing.T, report pc.StopReport, err error, state pc.StopState) {
	t.Helper()
	if err != nil || report.Details().State != state {
		t.Fatal("unexpected original stop result", report.Details().State, err)
	}
}

func sameExecutionRecoveryCause(a, b f.TransactionCause) bool {
	x, y := a.Details(), b.Details()
	return x.Kind == f.RecoveryCause && y.Kind == f.RecoveryCause && x.Owner == y.Owner && x.RecoveryRunID == y.RecoveryRunID && x.CheckpointRef == y.CheckpointRef
}

func TestExecutionProjectCallOriginalRegistrationAndReturn(t *testing.T) {
	for _, stage := range []string{"execution-call-register", "execution-call-return"} {
		t.Run(stage, func(t *testing.T) {
			v := newExecutionCalls(t)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			defer once.Do(func() { close(release) })
			v.store.setAfter(func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
				if d := cause.Details(); d.Owner == stage && d.CheckpointRef == v.p.String() {
					close(entered)
					<-release
				}
				return result
			})
			done := make(chan error, 1)
			go func() { _, err := v.lookup(context.Background(), v.p, "held-return"); done <- err }()
			associatedTestReceive(t, entered)
			beforeCalls, _, beforeWrites := v.store.counts()
			r, err := v.stop(context.Background(), false)
			requireExecutionStopState(t, r, err, pc.StopPending)
			if calls, _, writes := v.store.counts(); calls != beforeCalls || writes != beforeWrites {
				t.Fatal("Inspect queried or closed the original held call")
			}
			if _, err = v.lookup(context.Background(), v.b, "other-project"); err != nil {
				t.Fatal("held A prevented original B lookup", err)
			}
			select {
			case <-done:
				t.Fatal("public call escaped original Store return")
			default:
			}
			once.Do(func() { close(release) })
			if err = associatedTestReceive(t, done); err != nil {
				t.Fatal(err)
			}
			v.store.setAfter(nil)
			r, err = v.stop(context.Background(), false)
			requireExecutionStopState(t, r, err, pc.Stopped)
		})
	}
}

func TestExecutionProjectCallTechnicalUnknownKeepsOriginal(t *testing.T) {
	for _, stage := range []string{"execution-call-register", "execution-call-return"} {
		for _, applied := range []bool{false, true} {
			name := stage + "/rollback"
			if applied {
				name = stage + "/applied"
			}
			t.Run(name, func(t *testing.T) {
				v := newExecutionCalls(t)
				var original f.CommitResult
				masked := false
				v.store.setAfter(func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
					if cause.Details().Owner != stage || masked {
						return result
					}
					masked = true
					if result.State() != f.Committed {
						t.Fatal("fixture did not reach original callback", result.State())
					}
					if !applied {
						v.store.mu.Lock()
						id := cause.Details().RecoveryRunID
						if stage == "execution-call-register" {
							delete(v.store.rows, id)
						} else {
							r := v.store.rows[id]
							r.Phase = "running"
							v.store.rows[id] = r
						}
						v.store.mu.Unlock()
					}
					original = f.UnknownResult(newTestID[f.TransactionAttempt](t), cause)
					return original
				})
				_, err := v.lookup(context.Background(), v.p, "technical-unknown")
				got, ok := UnknownAttempt(err)
				if !ok || got.AttemptID() != original.AttemptID() {
					t.Fatal("first technical outcome lost", err)
				}
				_, _, writes := v.store.counts()
				second := f.UnknownResult(newTestID[f.TransactionAttempt](t), original.Cause())
				v.store.setAfter(func(_ context.Context, cause f.TransactionCause, _ f.CommitResult) f.CommitResult {
					if !sameExecutionRecoveryCause(cause, original.Cause()) {
						t.Fatal("confirmation changed original cause")
					}
					return second
				})
				_, err = v.stop(context.Background(), false)
				got, ok = UnknownAttempt(err)
				if !ok || got.AttemptID() != original.AttemptID() {
					t.Fatal("confirmation replaced first physical attempt", err)
				}
				confirmCtx, cancelConfirm := context.WithCancel(context.Background())
				v.store.setAfter(func(_ context.Context, _ f.TransactionCause, result f.CommitResult) f.CommitResult {
					cancelConfirm()
					return result
				})
				_, err = v.stop(confirmCtx, false)
				got, ok = UnknownAttempt(err)
				if !ok || got.AttemptID() != original.AttemptID() {
					t.Fatal("cancelled confirmation erased original outcome", err)
				}
				entered, release := make(chan struct{}), make(chan struct{})
				var once sync.Once
				defer once.Do(func() { close(release) })
				v.store.setAfter(func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
					if !sameExecutionRecoveryCause(cause, original.Cause()) {
						t.Fatal("confirmation did not consume original cause")
					}
					close(entered)
					<-release
					return result
				})
				done := make(chan error, 1)
				go func() { _, err := v.stop(context.Background(), false); done <- err }()
				associatedTestReceive(t, entered)
				v.pair.state.mu.Lock()
				var retained *projectCallEntry
				for _, entry := range v.pair.state.owners {
					retained = entry
				}
				stillOriginal := retained != nil && retained.unknown != nil && retained.unknown.AttemptID() == original.AttemptID()
				v.pair.state.mu.Unlock()
				if !stillOriginal {
					t.Fatal("physical confirmation commit replaced original return")
				}
				once.Do(func() { close(release) })
				if err = associatedTestReceive(t, done); err != nil {
					t.Fatal(err)
				}
				v.store.setAfter(nil)
				if _, _, after := v.store.counts(); after != writes {
					t.Fatal("Unknown confirmation wrote another close")
				}
				needsClose := stage == "execution-call-register" && applied || stage == "execution-call-return" && !applied
				if needsClose {
					beforeCalls, _, _ := v.store.counts()
					r, err := v.stop(context.Background(), false)
					requireExecutionStopState(t, r, err, pc.StopPending)
					if calls, _, after := v.store.counts(); calls != beforeCalls || after != writes {
						t.Fatal("public Inspect became a close after Unknown cleared")
					}
					r, err = v.stop(context.Background(), true)
					requireExecutionStopState(t, r, err, pc.StopPending)
				}
				r, err := v.stop(context.Background(), false)
				requireExecutionStopState(t, r, err, pc.Stopped)
			})
		}
	}
}

func TestExecutionProjectCallLaunchLookupRecoveryAfterServiceStop(t *testing.T) {
	for _, missing := range []bool{false, true} {
		name := "found"
		if missing {
			name = "missing-is-not-launch-proof"
		}
		t.Run(name, func(t *testing.T) {
			v := newExecutionCalls(t)
			v.launch.store.unknown = true
			_, err := v.pair.Service().Launch(context.Background(), v.launch.actor, v.launch.request)
			original, ok := UnknownAttempt(err)
			if !ok || original.Cause().Details().Kind != f.CommandsCause || len(v.launch.store.rows) != 1 {
				t.Fatal("actual original Launch did not retain its result", err)
			}
			v.pair.Service().Stop()
			digest, err := v.launch.request.Digest()
			if err != nil {
				t.Fatal(err)
			}
			key := ec.LaunchLookupKey{ProjectID: v.p, AgentID: v.launch.request.AgentID, IdempotencyKey: v.launch.request.Meta.IdempotencyKey}
			if missing {
				saved := v.launch.store.rows
				v.launch.store.rows = map[string][]any{}
				lookup, err := v.pair.Service().LookupLaunch(context.Background(), v.launch.actor, key, digest)
				if err != nil || lookup.Found || v.pair.Service().Joined() {
					t.Fatal("missing Launch was treated as returned original outcome", err)
				}
				_, err = v.stop(context.Background(), false)
				if got, ok := UnknownAttempt(err); !ok || got.AttemptID() != original.AttemptID() {
					t.Fatal("Stop laundered missing Launch into success", err)
				}
				v.launch.store.rows = saved
			}
			entered, release := make(chan context.Context, 1), make(chan struct{})
			var once sync.Once
			defer once.Do(func() { close(release) })
			v.store.setAfter(func(ctx context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
				if cause.Details().Kind == f.CommandsCause {
					entered <- ctx
					<-release
				}
				return result
			})
			done := make(chan error, 1)
			go func() {
				lookup, err := v.pair.Service().LookupLaunch(context.Background(), v.launch.actor, key, digest)
				if err == nil && (!lookup.Found || lookup.RequestDigest != digest) {
					err = errors.New("exact original Lookup result missing")
				}
				done <- err
			}()
			actual := associatedTestReceive(t, entered)
			v.pair.Service().Stop()
			if actual.Err() != nil || v.pair.Service().Joined() {
				t.Fatal("repeated Service.Stop cancelled recovery or joined before original Lookup return")
			}
			once.Do(func() { close(release) })
			if err = associatedTestReceive(t, done); err != nil {
				t.Fatal(err)
			}
			v.store.setAfter(nil)
			if !v.pair.Service().Joined() || v.launch.store.writes != 1 {
				t.Fatal("original successful recovery did not retire without a second Launch")
			}
		})
	}
}

func TestExecutionProjectCallAssociatedOriginalWorkerBody(t *testing.T) {
	v := newExecutionCalls(t)
	_, dispatch := associatedTestIdentity(t)
	_, other := associatedTestIdentity(t)
	entered, release, otherBody := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	// This is the original private physical-call seam. Terminal observations
	// are controlled here; no real association or Model completion is claimed.
	v.pair.Associated().state.ops.inspect = func(_ context.Context, p i.ProjectID, _ associatedIdentity) (associatedFacts, error) {
		if p == v.p {
			close(entered)
			<-release
		} else {
			close(otherBody)
		}
		return associatedFacts{status: ec.Succeeded}, nil
	}
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := associatedTestRun(t, v.pair.Associated(), runCtx)
	if _, err := v.pair.Associated().Advance(context.Background(), v.p, dispatch); err != nil {
		t.Fatal(err)
	}
	associatedTestReceive(t, entered)
	r, err := v.stop(context.Background(), true)
	requireExecutionStopState(t, r, err, pc.StopPending)
	if _, err = v.pair.Associated().Advance(context.Background(), v.b, other); err != nil {
		t.Fatal("Project stop affected B", err)
	}
	associatedTestReceive(t, otherBody)
	cancel()
	select {
	case <-runDone:
		t.Fatal("lifetime escaped original inspected worker body")
	default:
	}
	once.Do(func() { close(release) })
	_ = associatedTestReceive(t, runDone)
	r, err = v.stop(context.Background(), false)
	requireExecutionStopState(t, r, err, pc.Stopped)
}

func waitExecutionCallReturns(t *testing.T, pair *ProjectCallOwnership) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for {
		returned, changed := pair.state.projectCallsReturned("")
		if returned {
			return
		}
		select {
		case <-changed:
		case <-ctx.Done():
			t.Fatal("original project call tail did not return")
		}
	}
}

func TestExecutionProjectCallAssociatedTechnicalRecoveryDoesNotRunBody(t *testing.T) {
	v := newExecutionCalls(t)
	_, dispatch := associatedTestIdentity(t)
	var original f.CommitResult
	masked := false
	v.store.setAfter(func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
		if cause.Details().Owner == "execution-call-register" && !masked {
			masked = true
			original = f.UnknownResult(newTestID[f.TransactionAttempt](t), cause)
			return original
		}
		return result
	})
	var bodyMu sync.Mutex
	inspects := 0
	v.pair.Associated().state.ops.inspect = func(context.Context, i.ProjectID, associatedIdentity) (associatedFacts, error) {
		bodyMu.Lock()
		inspects++
		bodyMu.Unlock()
		return associatedFacts{}, fault(f.Forbidden)
	}
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := associatedTestRun(t, v.pair.Associated(), runCtx)
	if _, err := v.pair.Associated().Advance(context.Background(), v.p, dispatch); err != nil {
		t.Fatal(err)
	}
	waitExecutionCallReturns(t, v.pair)
	a := v.pair.Associated().state
	a.mu.Lock()
	job := a.jobs[dispatch.ExecutionID]
	if job == nil || !job.retained {
		a.mu.Unlock()
		t.Fatal("asynchronous registration Unknown lost its original job")
	}
	first := job.err
	job.next = time.Now().Add(-time.Hour)
	a.mu.Unlock()
	if raw, ok := UnknownAttempt(first); !ok || raw.AttemptID() != original.AttemptID() {
		t.Fatal("original job did not preserve registration outcome", first)
	}
	_, _, writes := v.store.counts()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	v.store.setAfter(func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
		if !sameExecutionRecoveryCause(cause, original.Cause()) {
			t.Fatal("worker technical confirmation changed original cause")
		}
		close(entered)
		<-release
		return result
	})
	_, _ = v.pair.Associated().Advance(context.Background(), v.p, dispatch)
	associatedTestReceive(t, entered)
	v.pair.state.mu.Lock()
	stillUnknown := job.projectCall.unknown != nil && !job.projectCall.returned
	v.pair.state.mu.Unlock()
	if !stillUnknown {
		t.Fatal("confirmed callback replaced original worker return")
	}
	once.Do(func() { close(release) })
	waitExecutionCallReturns(t, v.pair)
	v.store.setAfter(nil)
	if _, _, after := v.store.counts(); after != writes {
		t.Fatal("same-job confirmation closed or re-registered its row")
	}
	a.mu.Lock()
	job = a.jobs[dispatch.ExecutionID]
	if job == nil || !job.retained {
		a.mu.Unlock()
		t.Fatal("confirmation skipped the next explicit technical closure")
	}
	job.next = time.Now().Add(-time.Hour)
	a.mu.Unlock()
	_, _ = v.pair.Associated().Advance(context.Background(), v.p, dispatch)
	waitExecutionCallReturns(t, v.pair)
	bodyMu.Lock()
	actualInspects := inspects
	bodyMu.Unlock()
	if actualInspects != 0 {
		t.Fatal("technical confirmation or closure ran original execution body")
	}
	if _, _, after := v.store.counts(); after != writes+1 {
		t.Fatal("next explicit recovery did not close exactly its original row")
	}
	cancel()
	_ = associatedTestReceive(t, runDone)
	if !v.pair.Associated().Joined() {
		t.Fatal("technical-only job remained after actual closure return")
	}
}

func TestExecutionProjectCallForeignClosedFactsWaitForOriginalProof(t *testing.T) {
	v := newExecutionCalls(t)
	if _, err := v.lookup(context.Background(), v.p, "foreign-facts"); err != nil {
		t.Fatal(err)
	}
	v.store.mu.Lock()
	var stored projectCallRecord
	for _, row := range v.store.rows {
		stored = row
	}
	stored.Instance = newTestID[projectCallInstance](t)
	v.store.rows[stored.ID.String()] = stored
	v.store.mu.Unlock()
	r, err := v.stop(context.Background(), false)
	requireExecutionStopState(t, r, err, pc.StopPending)
	foreign := newTestID[oc.Process](t)
	v.store.mu.Lock()
	stored.Process = foreign
	v.store.rows[stored.ID.String()] = stored
	v.store.mu.Unlock()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	var proofMu sync.Mutex
	proofs := 0
	v.proof.confirm = func(context.Context, oc.ProcessID) error {
		proofMu.Lock()
		proofs++
		proofMu.Unlock()
		return nil // controlled death result; not a real ProcessGuard proof.
	}
	first := true
	v.store.setAfter(func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
		if cause.Details().Owner == "execution-project-stop" && first {
			first = false
			close(entered)
			<-release
		}
		return result
	})
	done := make(chan error, 1)
	go func() { _, err := v.stop(context.Background(), false); done <- err }()
	associatedTestReceive(t, entered)
	proofMu.Lock()
	beforeReturn := proofs
	proofMu.Unlock()
	if beforeReturn != 0 {
		t.Fatal("foreign proof ran before original snapshot Store returned")
	}
	// Full-row revalidation must reject a fact changed after the original read.
	v.store.mu.Lock()
	stored.Instance = newTestID[projectCallInstance](t)
	v.store.rows[stored.ID.String()] = stored
	v.store.mu.Unlock()
	once.Do(func() { close(release) })
	requireCode(t, associatedTestReceive(t, done), f.VersionConflict)
	v.store.setAfter(nil)
	r, err = v.stop(context.Background(), false)
	requireExecutionStopState(t, r, err, pc.Stopped)
}

func TestExecutionProjectCallWaitsForOriginalServiceDone(t *testing.T) {
	v := newExecutionCalls(t)
	checkpoint, returnCheckpoint := make(chan struct{}), make(chan struct{})
	doneEntered, returnDone := make(chan struct{}), make(chan struct{})
	var checkpointOnce, doneOnce sync.Once
	defer checkpointOnce.Do(func() { close(returnCheckpoint) })
	defer doneOnce.Do(func() { close(returnDone) })
	v.store.setAfter(func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
		if cause.Details().Owner == "execution-call-return" {
			close(checkpoint)
			<-returnCheckpoint
		}
		return result
	})
	done := make(chan error, 1)
	go func() { _, err := v.lookup(context.Background(), v.p, "original-service-done"); done <- err }()
	associatedTestReceive(t, checkpoint)
	v.pair.state.mu.Lock()
	var original *projectCallEntry
	for _, entry := range v.pair.state.owners {
		original = entry
	}
	if original == nil || original.serviceDone == nil {
		v.pair.state.mu.Unlock()
		t.Fatal("original Service admission was not retained")
	}
	originalDone := original.serviceDone
	original.serviceDone = func() {
		close(doneEntered)
		<-returnDone
		originalDone()
	}
	v.pair.state.mu.Unlock()
	checkpointOnce.Do(func() { close(returnCheckpoint) })
	associatedTestReceive(t, doneEntered)
	beforeCalls, _, beforeWrites := v.store.counts()
	r, err := v.stop(context.Background(), false)
	requireExecutionStopState(t, r, err, pc.StopPending)
	if calls, _, writes := v.store.counts(); calls != beforeCalls || writes != beforeWrites {
		t.Fatal("Inspect advanced while original Service done was held")
	}
	v.pair.state.mu.Lock()
	present := v.pair.state.owners[original.record.ID] == original && !original.returned
	v.pair.state.mu.Unlock()
	if !present {
		t.Fatal("technical close retired before original Service done returned")
	}
	doneOnce.Do(func() { close(returnDone) })
	if err = associatedTestReceive(t, done); err != nil {
		t.Fatal(err)
	}
	v.store.setAfter(nil)
	r, err = v.stop(context.Background(), false)
	requireExecutionStopState(t, r, err, pc.Stopped)
}

func TestExecutionProjectCallDriverScopeAndLaneStop(t *testing.T) {
	for _, lane := range []projectCallLane{projectLanePreparation, projectLaneDirect} {
		t.Run(string(lane), func(t *testing.T) {
			v := newExecutionCalls(t)
			_, dispatch := associatedTestIdentity(t)
			childCalls := 0
			v.store.setAfter(func(ctx context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
				w, ok := ctx.Value(projectCallContextKey{}).(projectCallWitness)
				if cause.Details().Owner == "execution-call-register" && ok && w.entry.record.Lane == lane {
					childCalls++
					// Explicit controlled rollback of only this registration.
					v.store.mu.Lock()
					delete(v.store.rows, w.entry.record.ID.String())
					v.store.mu.Unlock()
					return f.NotCommittedResult(f.NewFault(f.Forbidden, f.NotCommitted))
				}
				return result
			})
			invoke := func(ctx context.Context, execution i.ExecutionID) error {
				if lane == projectLanePreparation {
					return v.pair.Preparation().Run(ctx, execution)
				}
				_, err := v.pair.Direct().Start(ctx, execution)
				return err
			}
			// The association facts are the existing controlled physical seam.
			// The actual public worker still mints the parent; these child calls
			// reject before their business callback and never reach a Model.
			v.pair.Associated().state.ops.inspect = func(ctx context.Context, _ i.ProjectID, _ associatedIdentity) (associatedFacts, error) {
				before, _, _ := v.store.counts()
				requireCode(t, invoke(ctx, dispatch.ExecutionID), f.DependencyUnbound)
				if after, _, _ := v.store.counts(); after != before {
					t.Error("uninspected original job reached child SQL")
				}
				job, ok := ctx.Value(associatedContextKey{}).(*associatedCall)
				if !ok || markAssociatedInspected(ctx, job) != nil {
					return associatedFacts{}, fault(f.InvalidState)
				}
				requireCode(t, invoke(ctx, newTestID[i.Execution](t)), f.DependencyUnbound)
				requireCode(t, invoke(ctx, dispatch.ExecutionID), f.Forbidden)
				if lane == projectLanePreparation {
					v.pair.Preparation().Stop()
				} else {
					v.pair.Direct().Stop()
				}
				before, _, _ = v.store.counts()
				requireCode(t, invoke(ctx, dispatch.ExecutionID), f.ShuttingDown)
				if after, _, _ := v.store.counts(); after != before {
					t.Error("stopped original driver registered another child")
				}
				return associatedFacts{status: ec.Succeeded}, nil
			}
			runCtx, cancel := context.WithCancel(context.Background())
			defer cancel()
			runDone := associatedTestRun(t, v.pair.Associated(), runCtx)
			if _, err := v.pair.Associated().Advance(context.Background(), v.p, dispatch); err != nil {
				t.Fatal(err)
			}
			waitExecutionCallReturns(t, v.pair)
			cancel()
			_ = associatedTestReceive(t, runDone)
			if childCalls != 1 {
				t.Fatal("actual inspected worker did not reach exactly one child registration")
			}
		})
	}
}

type executionRecoveryProjects struct {
	*executionCallProjects
	original pc.ProjectRef
}

func (p *executionRecoveryProjects) RequirePreparingProjectInTx(ctx context.Context, tx f.Tx, project i.ProjectID) (pc.ProjectRef, error) {
	if p.original.ID != project {
		return pc.ProjectRef{}, fault(f.Forbidden)
	}
	return p.original, p.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(project)})
}

func TestExecutionProjectCallOriginalChildRecoveryRetiresParent(t *testing.T) {
	seed, _ := newExecutionEntryPair(t)
	preparation := newPreparationControl(t)
	store := &executionCallStore{Store: preparation.store, live: map[f.Tx][]f.LockRequest{}, rows: map[string]projectCallRecord{}}
	projects := &executionRecoveryProjects{executionCallProjects: &executionCallProjects{store: store}, original: preparation.project}
	authority, err := NewAuthority(store, projects, nil)
	if err != nil {
		t.Fatal(err)
	}
	preparation.launch.authority = authority
	direct := seed.Direct().state.deps
	direct.Projects, direct.Agents, direct.Processes = projects, preparation.launch, preparation.processes
	deps := ProjectCallDependencies{Service: Dependencies{Authority: authority, Agents: preparation.launch, Task: preparation.launch},
		Preparation: PreparationDependencies{Agents: preparation.launch, Projects: projects, Processes: preparation.processes, Task: preparation},
		Direct:      direct, Associated: AssociatedExecutorOptions{MaxOwned: 2, RecoveryInterval: time.Hour}}
	process, err := f.ParseID[oc.Process](preparation.processes.process.String())
	if err != nil {
		t.Fatal(err)
	}
	pair, err := newProjectCallOwnership(store, deps, &executionCallProof{store: store}, process, &executionCallAuthority{store})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		store.setAfter(nil)
		pair.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := pair.Drain(ctx); err != nil || !pair.Joined() {
			t.Error("original nested recovery did not retire", err)
		}
	})
	project := preparation.launch.request.ProjectID
	_, dispatch := associatedTestIdentity(t)
	dispatch.ExecutionID, dispatch.AgentID = preparation.execution, preparation.launch.request.AgentID
	// Only association discovery is a private physical-call control. The
	// Preparation Run, claim Unknown, returned owner and Resolve below are
	// actual existing driver code over its explicit SQL fixture. This is not
	// a known-NotCommitted retention test or a real Scheduler association.
	inspects := 0
	pair.Associated().state.ops.inspect = func(ctx context.Context, _ i.ProjectID, _ associatedIdentity) (associatedFacts, error) {
		job, _ := ctx.Value(associatedContextKey{}).(*associatedCall)
		if err := markAssociatedInspected(ctx, job); err != nil {
			return associatedFacts{}, err
		}
		inspects++
		if inspects == 1 {
			return associatedFacts{status: ec.Created}, nil
		}
		return associatedFacts{status: ec.Preparing}, nil
	}
	preparation.store.unknownAt = "claim"
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := associatedTestRun(t, pair.Associated(), runCtx)
	if _, err := pair.Associated().Advance(context.Background(), project, dispatch); err != nil {
		t.Fatal(err)
	}
	waitExecutionCallReturns(t, pair)
	a := pair.Associated().state
	a.mu.Lock()
	job := a.jobs[dispatch.ExecutionID]
	if job == nil || !job.retained {
		a.mu.Unlock()
		t.Fatal("actual child Unknown did not retain the original parent")
	}
	first := job.err
	job.next = time.Now().Add(-time.Hour)
	a.mu.Unlock()
	original, ok := UnknownAttempt(first)
	if !ok || original.Cause().Details().Kind != f.JobCause {
		t.Fatal("parent lost actual preparation attempt", first)
	}
	pair.state.mu.Lock()
	parent := job.projectCall
	parentOnly := parent.outcome != nil && parent.outcome.parentOnly && len(parent.children) == 1
	pair.state.mu.Unlock()
	if !parentOnly {
		t.Fatal("actual child outcome was not attributed to its parent")
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	store.setAfter(func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
		if cause.Details().Kind == f.JobCause {
			x, y := cause.Details(), original.Cause().Details()
			if x.JobType != y.JobType || x.JobID != y.JobID || x.JobAttemptID != y.JobAttemptID || result.State() != f.Committed {
				t.Fatal("normal Resolve changed its original attempt")
			}
			close(entered)
			<-release
		}
		return result
	})
	_, _ = pair.Associated().Advance(context.Background(), project, dispatch)
	associatedTestReceive(t, entered)
	claim := preparation.store.claims[preparation.execution.String()]
	if preparation.store.attempts[claim.attempt.String()].phase != "terminal" {
		t.Fatal("control did not hold the actual completed recovery transaction")
	}
	pair.state.mu.Lock()
	stillOriginal := parent.outcomeErr != nil && !parent.returned && parent.outcome != nil && parent.outcome.parentOnly
	pair.state.mu.Unlock()
	if !stillOriginal || pair.Preparation().Joined() {
		t.Fatal("physical child checkpoint retired its parent before original Resolve return")
	}
	oneScope := pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: project}
	lifecycle, err := i.RegisterService(i.ProjectLifecycle)
	if err != nil {
		t.Fatal(err)
	}
	identityScope, err := i.InProject(project)
	if err != nil {
		t.Fatal(err)
	}
	cause := pc.LifecycleCause{OperationID: newTestID[pc.Operation](t), Action: pc.Delete, ProjectVersion: 2}
	actor, err := lifecycle.Actor(cause.OperationID.String(), identityScope)
	if err != nil {
		t.Fatal(err)
	}
	beforeCalls, _, beforeWrites := store.counts()
	r, err := pair.InspectProjectStop(context.Background(), actor, cause, oneScope)
	requireExecutionStopState(t, r, err, pc.StopPending)
	if calls, _, writes := store.counts(); calls != beforeCalls || writes != beforeWrites {
		t.Fatal("Stop inspected or mutated a child while its original recovery was held")
	}
	once.Do(func() { close(release) })
	waitExecutionCallReturns(t, pair)
	store.setAfter(nil)
	if preparation.captures != 0 || len(preparation.store.attempts) != 1 || inspects != 2 {
		t.Fatal("normal original recovery repeated preparation or lost the next read")
	}
	pair.state.mu.Lock()
	remaining := len(pair.state.owners)
	pair.state.mu.Unlock()
	if remaining != 0 {
		t.Fatal("actual recovered child left its parent permanently retained")
	}
	r, err = pair.InspectProjectStop(context.Background(), actor, cause, oneScope)
	requireExecutionStopState(t, r, err, pc.Stopped)
	cancel()
	_ = associatedTestReceive(t, runDone)
}
