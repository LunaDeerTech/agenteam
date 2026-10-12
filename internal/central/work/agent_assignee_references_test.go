package work

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

// These ports model one controlled live transaction and one borrowed executor.
// They establish no PostgreSQL binding, Session authentication, Agent existence
// or Task state transition. Terminal-state SQL semantics belong to the PG fixture;
// these controls neither parse nor mirror the repository's SQL text.
type assigneeRefStore struct {
	mu           sync.Mutex
	tx           f.Tx
	live         bool
	ctx          context.Context
	wanted, held map[string]f.LockMode
	steps        []string
	forbidden    int
	loan         *assigneeRefLoan
	nilLoan      int
	hold         string
	entered      chan struct{}
	release      chan struct{}
}

func (s *assigneeRefStore) step(name string) {
	s.mu.Lock()
	s.steps = append(s.steps, name)
	s.mu.Unlock()
}
func (s *assigneeRefStore) snapshot() ([]string, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.steps...), s.forbidden
}
func (s *assigneeRefStore) block(stage string) {
	if s.hold == stage {
		close(s.entered)
		<-s.release
	}
}
func (s *assigneeRefStore) reject() error {
	s.mu.Lock()
	s.forbidden++
	s.mu.Unlock()
	return errors.New("unexpected ownership or ambient SQL operation")
}
func (s *assigneeRefStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	s.step("borrow")
	s.block("borrow")
	if !s.live || tx != s.tx {
		return nil, fault(f.Forbidden)
	}
	if s.nilLoan == 1 {
		return nil, nil
	}
	if s.nilLoan == 2 {
		var empty *assigneeRefLoan
		return empty, nil
	}
	return s.loan, nil
}
func (s *assigneeRefStore) RequireHeldLocks(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	s.step("locks")
	if ctx != s.ctx || tx != s.tx || !s.live {
		return fault(f.Forbidden)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(locks) != len(s.wanted) {
		return fault(f.Forbidden)
	}
	seen := make(map[string]bool)
	for _, lock := range locks {
		key := lock.Key.Canonical()
		want, ok := s.wanted[key]
		actual, held := s.held[key]
		if !ok || seen[key] || want != lock.Mode || !held || (actual != lock.Mode && !(actual == f.Exclusive && lock.Mode == f.Shared)) {
			return fault(f.Forbidden)
		}
		seen[key] = true
	}
	return nil
}
func (s *assigneeRefStore) WithinTx(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult {
	return f.NotCommittedResult(fault(f.Forbidden).WithCause(s.reject()))
}
func (s *assigneeRefStore) AcquireAll(context.Context, f.Tx, []f.LockRequest) error {
	return s.reject()
}
func (s *assigneeRefStore) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, s.reject()
}
func (s *assigneeRefStore) Query(context.Context, string, ...any) (*postgres.Rows, error) {
	return nil, s.reject()
}
func (s *assigneeRefStore) QueryRow(context.Context, string, ...any) postgres.Row {
	return assigneeRefErrorRow{s.reject()}
}

type assigneeRefErrorRow struct{ err error }

func (r assigneeRefErrorRow) Scan(...any) error { return r.err }

type assigneeRefLoan struct {
	store    *assigneeRefStore
	project  i.ProjectID
	agent    i.AgentID
	assigned bool
	scanErr  error
}

func (x *assigneeRefLoan) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, x.store.reject()
}
func (x *assigneeRefLoan) Query(context.Context, string, ...any) (*postgres.Rows, error) {
	return nil, x.store.reject()
}
func (x *assigneeRefLoan) QueryRow(ctx context.Context, _ string, args ...any) postgres.Row {
	x.store.step("query")
	if ctx != x.store.ctx || len(args) != 2 || args[0] != x.project.String() || args[1] != x.agent.String() {
		return assigneeRefErrorRow{errors.New("changed original query scope")}
	}
	return x
}
func (x *assigneeRefLoan) Scan(dest ...any) error {
	x.store.step("scan")
	x.store.block("scan") // Actual return is independent of caller cancellation.
	if x.scanErr != nil {
		return x.scanErr
	}
	if len(dest) != 1 {
		return errors.New("unexpected result shape")
	}
	value, ok := dest[0].(*bool)
	if !ok {
		return errors.New("existence result must be boolean")
	}
	*value = x.assigned
	return nil
}

type assigneeRefProject struct {
	pc.ProjectAuthority
	store   *assigneeRefStore
	actor   i.Actor
	project i.ProjectID
	access  pc.ProjectAccess
	err     error
	cancel  context.CancelFunc
}

func (p *assigneeRefProject) RequireOwnerInTx(ctx context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, intent i.AccessIntent) (pc.ProjectAccess, error) {
	before, forbidden := p.store.snapshot()
	p.store.step("owner")
	if !slices.Equal(before, []string{"borrow", "locks"}) || forbidden != 0 || ctx != p.store.ctx || tx != p.store.tx || !actor.Equal(p.actor) || project != p.project || intent != i.Read {
		return pc.ProjectAccess{}, errors.New("changed original Owner Read loan")
	}
	if p.cancel != nil {
		p.cancel()
	}
	return p.access, p.err
}

type assigneeRefFixture struct {
	authority *Authority
	store     *assigneeRefStore
	projects  *assigneeRefProject
	actor     i.Actor
	project   i.ProjectID
	agent     i.AgentID
	keys      []string
}

func assigneeRefAccess(t *testing.T, actor i.Actor, project i.ProjectID) pc.ProjectAccess {
	t.Helper()
	at, err := f.NewInstant(time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	user, err := f.ParseID[i.User](actor.Details().UserID)
	if err != nil {
		t.Fatal(err)
	}
	ref := pc.ProjectRef{ID: project, OwnerUserID: user, Name: "assignee-reference", NormalizedName: "assignee-reference", Lifecycle: pc.Active, Version: 1, CreatedAt: at, UpdatedAt: at}
	access, err := pc.NewProjectAccess(actor, ref, at)
	if err != nil {
		t.Fatal(err)
	}
	return access
}
func newAssigneeRefFixture(t *testing.T) *assigneeRefFixture {
	t.Helper()
	actor, project, agent := pureActor(t, 2), pureID[i.Project](t, 3), pureID[i.Agent](t, 4)
	userKey, err := f.UserLock(actor.Details().UserID)
	if err != nil {
		t.Fatal(err)
	}
	projectKey, err := f.ProjectLock(project.String())
	if err != nil {
		t.Fatal(err)
	}
	scheduleKey, err := f.ProjectScheduleLock(project.String())
	if err != nil {
		t.Fatal(err)
	}
	agentKey, err := f.AgentLock(agent.String())
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{userKey.Canonical(), projectKey.Canonical(), scheduleKey.Canonical(), agentKey.Canonical()}
	wanted := map[string]f.LockMode{keys[0]: f.Shared, keys[1]: f.Shared, keys[2]: f.Exclusive, keys[3]: f.Exclusive}
	held := make(map[string]f.LockMode)
	for key, mode := range wanted {
		held[key] = mode
	}
	s := &assigneeRefStore{tx: f.NewTx(), live: true, ctx: context.Background(), wanted: wanted, held: held}
	s.loan = &assigneeRefLoan{store: s, project: project, agent: agent}
	p := &assigneeRefProject{store: s, actor: actor, project: project, access: assigneeRefAccess(t, actor, project)}
	a, err := NewAuthority(s, p)
	if err != nil {
		t.Fatal(err)
	}
	if steps, forbidden := s.snapshot(); len(steps) != 0 || forbidden != 0 {
		t.Fatal("pure authority construction performed I/O")
	}
	return &assigneeRefFixture{a, s, p, actor, project, agent, keys}
}
func (v *assigneeRefFixture) call() error {
	return v.authority.RequireAgentUnassignedInTx(v.store.ctx, v.store.tx, v.actor, v.project, v.agent)
}
func assigneeRefSteps(t *testing.T, s *assigneeRefStore, expected ...string) {
	t.Helper()
	got, forbidden := s.snapshot()
	if !slices.Equal(got, expected) || forbidden != 0 {
		t.Fatalf("loan steps=%v want=%v forbidden=%d", got, expected, forbidden)
	}
}

func TestWorkAgentAssigneeReferenceInputs(t *testing.T) {
	for _, name := range []string{"nil-authority", "unbound-authority", "nil-context", "cancelled-context", "invalid-transaction", "invalid-actor", "agent-actor", "service-actor", "invalid-project", "invalid-agent"} {
		t.Run(name, func(t *testing.T) {
			v := newAssigneeRefFixture(t)
			a, ctx, tx, actor, project, agent := v.authority, v.store.ctx, v.store.tx, v.actor, v.project, v.agent
			code := f.InvalidArgument
			switch name {
			case "nil-authority":
				a = nil
				code = f.DependencyUnbound
			case "unbound-authority":
				a = &Authority{}
				code = f.DependencyUnbound
			case "nil-context":
				ctx = nil
			case "cancelled-context":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				code = f.DependencyUnavailable
			case "invalid-transaction":
				tx = f.Tx{}
			case "invalid-actor":
				actor = i.Actor{}
				code = f.Unauthenticated
			case "agent-actor":
				var err error
				actor, err = i.NewAgentRun(project, agent, pureID[i.Execution](t, 5))
				if err != nil {
					t.Fatal(err)
				}
				code = f.DependencyUnbound
			case "service-actor":
				reg, err := i.RegisterService(i.ProjectLifecycle)
				if err != nil {
					t.Fatal(err)
				}
				scope, err := i.InProject(project)
				if err != nil {
					t.Fatal(err)
				}
				actor, err = reg.Actor(pureID[struct{}](t, 6).String(), scope)
				if err != nil {
					t.Fatal(err)
				}
				code = f.Forbidden
			case "invalid-project":
				project = i.ProjectID{}
			case "invalid-agent":
				agent = i.AgentID{}
			}
			err := a.RequireAgentUnassignedInTx(ctx, tx, actor, project, agent)
			pureCode(t, err, code)
			if name == "cancelled-context" && !errors.Is(err, context.Canceled) {
				t.Fatal("lost original cancellation", err)
			}
			assigneeRefSteps(t, v.store)
		})
	}
}

func TestWorkAgentAssigneeReferenceOriginalTransactionAndLocks(t *testing.T) {
	for _, name := range []string{"foreign-transaction", "expired-transaction", "missing-user", "missing-project", "missing-schedule", "missing-agent", "shared-schedule", "shared-agent", "nil-executor", "typed-nil-executor"} {
		t.Run(name, func(t *testing.T) {
			v := newAssigneeRefFixture(t)
			tx := v.store.tx
			code, locks := f.Forbidden, false
			switch name {
			case "foreign-transaction":
				other := newAssigneeRefFixture(t)
				tx = other.store.tx
			case "expired-transaction":
				v.store.live = false
			case "missing-user":
				delete(v.store.held, v.keys[0])
				locks = true
			case "missing-project":
				delete(v.store.held, v.keys[1])
				locks = true
			case "missing-schedule":
				delete(v.store.held, v.keys[2])
				locks = true
			case "missing-agent":
				delete(v.store.held, v.keys[3])
				locks = true
			case "shared-schedule":
				v.store.held[v.keys[2]] = f.Shared
				locks = true
			case "shared-agent":
				v.store.held[v.keys[3]] = f.Shared
				locks = true
			case "nil-executor":
				v.store.nilLoan = 1
				code = f.DependencyUnbound
			case "typed-nil-executor":
				v.store.nilLoan = 2
				code = f.DependencyUnbound
			}
			pureCode(t, v.authority.RequireAgentUnassignedInTx(v.store.ctx, tx, v.actor, v.project, v.agent), code)
			if locks {
				assigneeRefSteps(t, v.store, "borrow", "locks")
			} else {
				assigneeRefSteps(t, v.store, "borrow")
			}
		})
	}
	// Existing exclusive locks can satisfy either requested shared lock. The
	// method still asks for the full exact required union rather than adding it.
	v := newAssigneeRefFixture(t)
	for key := range v.store.held {
		v.store.held[key] = f.Exclusive
	}
	if err := v.call(); err != nil {
		t.Fatal("EX did not satisfy SH", err)
	}
	assigneeRefSteps(t, v.store, "borrow", "locks", "owner", "query", "scan")
}

func TestWorkAgentAssigneeReferenceOwnerReadBeforeTask(t *testing.T) {
	for _, name := range []string{"owner-denied", "session-revoked", "project-denied", "wrong-project-grant", "wrong-session-grant", "empty-grant", "cancelled-after-owner"} {
		t.Run(name, func(t *testing.T) {
			v := newAssigneeRefFixture(t)
			code := f.Forbidden
			switch name {
			case "owner-denied":
				v.projects.err = fault(f.Forbidden)
			case "session-revoked":
				code = f.SessionRevoked
				v.projects.err = fault(code)
			case "project-denied":
				code = f.ProjectNotActive
				v.projects.err = fault(code)
			case "wrong-project-grant":
				v.projects.access = assigneeRefAccess(t, v.actor, pureID[i.Project](t, 10))
			case "wrong-session-grant":
				v.projects.access = assigneeRefAccess(t, pureActor(t, 11), v.project)
			case "empty-grant":
				v.projects.access = pc.ProjectAccess{}
			case "cancelled-after-owner":
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				v.store.ctx, v.projects.cancel = ctx, cancel
				code = f.DependencyUnavailable
			}
			err := v.call()
			pureCode(t, err, code)
			if v.projects.err != nil && err != v.projects.err {
				t.Fatal("Owner/Session/Project fault was replaced")
			}
			if name == "cancelled-after-owner" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancelled grant was accepted")
			}
			assigneeRefSteps(t, v.store, "borrow", "locks", "owner")
		})
	}
}

func TestWorkAgentAssigneeReferencePredicateAndFailure(t *testing.T) {
	for _, name := range []string{"unassigned", "assigned", "scan-error"} {
		t.Run(name, func(t *testing.T) {
			v := newAssigneeRefFixture(t)
			canary := errors.New("private scan failure")
			v.store.loan.assigned = name == "assigned"
			if name == "scan-error" {
				v.store.loan.scanErr = canary
			}
			err := v.call()
			switch name {
			case "unassigned":
				if err != nil {
					t.Fatal("no-assignee predicate failed", err)
				}
			case "assigned":
				pureCode(t, err, f.ResourceBusy)
			case "scan-error":
				pureCode(t, err, f.DependencyUnavailable)
				if !errors.Is(err, canary) {
					t.Fatal("original scan error lost", err)
				}
			}
			assigneeRefSteps(t, v.store, "borrow", "locks", "owner", "query", "scan")
		})
	}
	// A false predicate does not establish a canonical Agent. No Agent provider
	// exists in this fixture, and the one executor accepts only the requested P/A.
}

func TestWorkAgentAssigneeReferenceWaitsForOriginalReturn(t *testing.T) {
	for _, stage := range []string{"borrow", "scan"} {
		t.Run(stage, func(t *testing.T) {
			v := newAssigneeRefFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			v.store.ctx = ctx
			v.store.hold, v.store.entered, v.store.release = stage, make(chan struct{}), make(chan struct{})
			var once sync.Once
			release := func() { once.Do(func() { close(v.store.release) }) }
			result, returned := make(chan error, 1), make(chan struct{})
			go func() { defer close(returned); result <- v.call() }()
			t.Cleanup(func() {
				release()
				select {
				case <-returned:
				case <-time.After(time.Second):
					t.Error("original controlled call did not return after release")
				}
			})
			select {
			case <-v.store.entered:
			case <-time.After(time.Second):
				t.Fatal("original boundary not reached")
			}
			cancel()
			select {
			case <-returned:
				t.Fatal("cancellation abandoned original borrowed call")
			default:
			}
			release()
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) {
					t.Fatal("late false result became success after cancellation", err)
				}
			case <-time.After(time.Second):
				t.Fatal("released original call failed to return")
			}
			if stage == "borrow" {
				assigneeRefSteps(t, v.store, "borrow", "locks")
			} else {
				assigneeRefSteps(t, v.store, "borrow", "locks", "owner", "query", "scan")
			}
		})
	}
}
