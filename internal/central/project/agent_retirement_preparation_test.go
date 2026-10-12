package project

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// This executor is only a controlled transaction boundary. It never publishes
// a lifecycle row or successful COMMIT. It can return a controlled claim, but
// the subsequent canonical lifecycle read always returns a sentinel.
// Complete persisted lifecycle and Agent publication facts belong to PG.
type retirementPrepStore struct {
	mu           sync.Mutex
	tx           f.Tx
	live         bool
	held         map[string]f.LockMode
	calls        []string
	readErr      error
	readEntered  chan struct{}
	readRelease  chan struct{}
	required     []f.LockRequest
	claim        *lifecycleStopClaim
	claimMissing bool
	nilExecutor  bool
	onRead       func()
	rows         int
}

func (s *retirementPrepStore) record(name string) {
	s.mu.Lock()
	s.calls = append(s.calls, name)
	s.mu.Unlock()
}
func (s *retirementPrepStore) seen() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}
func (s *retirementPrepStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	s.record("borrow")
	if tx != s.tx || !s.live {
		return nil, fault(f.Forbidden)
	}
	if s.nilExecutor {
		return nil, nil
	}
	return s, nil
}
func (s *retirementPrepStore) RequireHeldLocks(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	s.record("locks")
	if tx != s.tx || !s.live {
		return fault(f.Forbidden)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(locks) != len(s.required) {
		return fault(f.Forbidden)
	}
	for n := range locks {
		if locks[n].Key.Canonical() != s.required[n].Key.Canonical() || locks[n].Mode != s.required[n].Mode {
			return fault(f.Forbidden)
		}
	}
	for _, lock := range locks {
		mode, ok := s.held[lock.Key.Canonical()]
		if !ok || (mode != lock.Mode && !(mode == f.Exclusive && lock.Mode == f.Shared)) {
			return fault(f.Forbidden)
		}
	}
	return nil
}
func (s *retirementPrepStore) WithinTx(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult {
	s.record("forbidden-tx")
	return f.NotCommittedResult(fault(f.Forbidden))
}
func (s *retirementPrepStore) AcquireAll(context.Context, f.Tx, []f.LockRequest) error {
	s.record("forbidden-acquire")
	return fault(f.Forbidden)
}
func (s *retirementPrepStore) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	s.record("forbidden-write")
	return pgconn.CommandTag{}, fault(f.Forbidden)
}
func (s *retirementPrepStore) Query(context.Context, string, ...any) (*postgres.Rows, error) {
	s.record("query")
	return nil, s.readErr
}
func (s *retirementPrepStore) QueryRow(context.Context, string, ...any) postgres.Row {
	s.record("row")
	s.rows++
	first := s.rows == 1
	return rowFunc(func(dest ...any) error {
		if s.onRead != nil {
			s.onRead()
		}
		if s.readEntered != nil {
			close(s.readEntered)
			<-s.readRelease
		}
		if first && s.claimMissing {
			return pgx.ErrNoRows
		}
		if first && s.claim != nil {
			r := s.claim
			return valuesRow(r.project.String(), r.process.String(), r.attempt, r.fence, r.phase).Scan(dest...)
		}
		return s.readErr
	})
}

// This private claim is a controlled input to invoke, not a persisted phase or
// COMMIT proof. The real start/SQL/Agent publication chain is covered by PG.
func retirementPrepClaim(t *testing.T, d *LifecycleStopDriver, r c.AgentRetirementPreparationRequest) lifecycleStopClaim {
	t.Helper()
	return lifecycleStopClaim{project: r.ProjectID, operation: r.Cause.OperationID, process: d.state.process, attempt: testID[struct{}](t).String(), fence: 1, phase: "running"}
}
func retirementPrepScope(t *testing.T, d *LifecycleStopDriver, r c.AgentRetirementPreparationRequest) (context.Context, *agentPreparationScope) {
	t.Helper()
	claim := retirementPrepClaim(t, d, r)
	ctx, end, err := d.state.agentPreparationStep(agentPreparationClaimContext(context.Background(), d.state, claim, r.Cause), r.Actor, r.Cause, r.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	scope, ok := ctx.Value(agentPreparationScopeKey{}).(*agentPreparationScope)
	if !ok || scope == nil {
		t.Fatal("original step did not issue a scope")
	}
	t.Cleanup(end)
	return ctx, scope
}
func retirementPrepHeld(t *testing.T, s *retirementPrepStore, r c.AgentRetirementPreparationRequest) {
	t.Helper()
	p, err := f.ProjectLock(r.ProjectID.String())
	if err != nil {
		t.Fatal(err)
	}
	schedule, err := f.ProjectScheduleLock(r.ProjectID.String())
	if err != nil {
		t.Fatal(err)
	}
	a, err := f.AgentLock(r.AgentID.String())
	if err != nil {
		t.Fatal(err)
	}
	s.required = []f.LockRequest{{Key: p, Mode: f.Shared}, {Key: schedule, Mode: f.Exclusive}, {Key: a, Mode: f.Exclusive}}
	for _, l := range s.required {
		s.held[l.Key.Canonical()] = l.Mode
	}
}
func retirementPrepAwait(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("original return barrier not reached")
	}
}

func retirementPrepRequest(t *testing.T) c.AgentRetirementPreparationRequest {
	t.Helper()
	actor, project, cause := lifecycleTestActor(t)
	cause.Action = c.Delete
	return c.AgentRetirementPreparationRequest{Actor: actor, ProjectID: project, Cause: cause, AgentID: testID[i.Agent](t), ExpectedConfigVersion: 1}
}
func retirementPrepFixture(t *testing.T, step LifecycleLocalStopStep) (*LifecycleStopDriver, *AgentRetirementPreparation, *retirementPrepStore) {
	t.Helper()
	s := &retirementPrepStore{tx: f.NewTx(), live: true, held: make(map[string]f.LockMode), readErr: errors.New("controlled original SQL")}
	if step == nil {
		step = func(context.Context, i.Actor, c.LifecycleCause, c.ScopeRef) error { return nil }
	}
	d := stopRoundTestDriver(t, s, step)
	owner, err := NewAgentRetirementPreparation(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.seen()) != 0 {
		t.Fatal("construction used the Store")
	}
	return d, owner, s
}

func TestAgentRetirementPreparationRequiresConcreteScope(t *testing.T) {
	for _, d := range []*LifecycleStopDriver{nil, {}} {
		if _, err := NewAgentRetirementPreparation(d); err == nil {
			t.Fatal("unbound original driver accepted")
		}
	}
	_, owner, store := retirementPrepFixture(t, nil)
	request := retirementPrepRequest(t)
	called := false
	err := owner.WithAgentRetirementPreparationInTx(context.Background(), store.tx, request, func(context.Context) error { called = true; return nil })
	if err == nil || called || len(store.seen()) != 0 {
		t.Fatal("public Actor/request manufactured a private scope", err)
	}
	for _, name := range []string{"legacy-invoke", "foreign-driver", "terminal-claim", "other-operation", "other-project"} {
		t.Run(name, func(t *testing.T) {
			r := retirementPrepRequest(t)
			entered := false
			var original *AgentRetirementPreparation
			var originalTx f.Tx
			d, owner, s := retirementPrepFixture(t, func(ctx context.Context, actor i.Actor, cause c.LifecycleCause, _ c.ScopeRef) error {
				entered = true
				r.Actor = actor
				if err := original.WithAgentRetirementPreparationInTx(ctx, originalTx, r, func(context.Context) error { t.Error("unissued scope admitted callback"); return nil }); err == nil {
					t.Error("unissued scope admitted")
				}
				return nil
			})
			original = owner
			originalTx = s.tx
			ctx := context.Background()
			claim := retirementPrepClaim(t, d, r)
			switch name {
			case "foreign-driver":
				_, foreign, _ := retirementPrepFixture(t, nil)
				original = foreign
			case "terminal-claim":
				claim.phase = "terminal"
			case "other-operation":
				claim.operation = testID[c.Operation](t)
			case "other-project":
				claim.project = testID[i.Project](t)
			}
			if name != "legacy-invoke" {
				ctx = agentPreparationClaimContext(ctx, d.state, claim, r.Cause)
			}
			err := d.state.invoke(ctx, r.ProjectID, r.Cause.OperationID, r.Cause)
			allowedStep := name == "legacy-invoke" || name == "foreign-driver"
			if entered != allowedStep || (allowedStep && err != nil) || (!allowedStep && err == nil) || len(s.seen()) != 0 {
				t.Fatal("issuer boundary or original invoke changed", err, s.seen())
			}
		})
	}
}

func TestAgentRetirementPreparationRequiresOriginalTxLocksAndClaim(t *testing.T) {
	for _, name := range []string{"original", "foreign-tx", "closed-tx", "nil-executor", "project-lock", "schedule-lock", "agent-lock", "schedule-shared", "agent-shared", "missing-claim", "claim-project", "claim-process", "claim-attempt", "claim-fence", "claim-terminal"} {
		t.Run(name, func(t *testing.T) {
			d, owner, s := retirementPrepFixture(t, nil)
			r := retirementPrepRequest(t)
			if err := r.Validate(); err != nil {
				t.Fatal("invalid controlled request", err)
			}
			ctx, scope := retirementPrepScope(t, d, r)
			retirementPrepHeld(t, s, r)
			actual := scope.claim
			s.claim = &actual
			tx := s.tx
			want := []string{"borrow", "locks", "row"}
			switch name {
			case "original":
				want = append(want, "row")
			case "foreign-tx":
				tx = f.NewTx()
				want = want[:1]
			case "closed-tx":
				s.live = false
				want = want[:1]
			case "nil-executor":
				s.nilExecutor = true
				want = want[:1]
			case "project-lock":
				delete(s.held, s.required[0].Key.Canonical())
				want = want[:2]
			case "schedule-lock":
				delete(s.held, s.required[1].Key.Canonical())
				want = want[:2]
			case "agent-lock":
				delete(s.held, s.required[2].Key.Canonical())
				want = want[:2]
			case "schedule-shared":
				s.held[s.required[1].Key.Canonical()] = f.Shared
				want = want[:2]
			case "agent-shared":
				s.held[s.required[2].Key.Canonical()] = f.Shared
				want = want[:2]
			case "missing-claim":
				s.claimMissing = true
			case "claim-project":
				actual.project = testID[i.Project](t)
			case "claim-process":
				actual.process = testID[oc.Process](t)
			case "claim-attempt":
				actual.attempt = testID[struct{}](t).String()
			case "claim-fence":
				actual.fence++
			case "claim-terminal":
				actual.phase = "terminal"
			}
			err := owner.WithAgentRetirementPreparationInTx(ctx, tx, r, func(context.Context) error { t.Error("controlled canonical sentinel was bypassed"); return nil })
			if err == nil || !slices.Equal(s.seen(), want) {
				t.Fatal("original borrow/held/claim order changed", err, s.seen(), want)
			}
			if name == "original" && !errors.Is(err, s.readErr) {
				t.Fatal("original canonical SQL error lost", err)
			}
			scope.mu.Lock()
			remaining := len(scope.loans)
			scope.mu.Unlock()
			if remaining != 0 {
				t.Fatal("rejected read retained a loan")
			}
		})
	}
}

func TestAgentRetirementPreparationRejectsInvalidBorrowBeforeStore(t *testing.T) {
	for _, name := range []string{"nil-context", "cancelled", "invalid-tx", "nil-callback", "archive", "human", "missing-agent", "zero-version"} {
		t.Run(name, func(t *testing.T) {
			d, owner, s := retirementPrepFixture(t, nil)
			r := retirementPrepRequest(t)
			ctx, _ := retirementPrepScope(t, d, r)
			tx := s.tx
			fn := func(context.Context) error { t.Error("invalid request reached callback"); return nil }
			switch name {
			case "nil-context":
				ctx = nil
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "invalid-tx":
				tx = f.Tx{}
			case "nil-callback":
				fn = nil
			case "archive":
				r.Cause.Action = c.Archive
			case "human":
				r.Actor = testActor(t)
			case "missing-agent":
				r.AgentID = i.AgentID{}
			case "zero-version":
				r.ExpectedConfigVersion = 0
			}
			err := owner.WithAgentRetirementPreparationInTx(ctx, tx, r, fn)
			if err == nil || len(s.seen()) != 0 {
				t.Fatal("invalid borrow reached original Store", err, s.seen())
			}
			if name == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatal("original cancellation lost", err)
			}
		})
	}
}

func TestAgentRetirementPreparationNestedLoansKeepOriginalLifetime(t *testing.T) {
	for _, name := range []string{"active-exact", "other-tx", "other-agent", "other-version", "released-parent"} {
		t.Run(name, func(t *testing.T) {
			d, _, s := retirementPrepFixture(t, nil)
			r := retirementPrepRequest(t)
			ctx, scope := retirementPrepScope(t, d, r)
			parent, err := scope.borrow(ctx, s.tx, r)
			if err != nil {
				t.Fatal(err)
			}
			var releaseOnce sync.Once
			releaseParent := func() { releaseOnce.Do(func() { scope.release(parent) }) }
			t.Cleanup(releaseParent)
			parentCtx := context.WithValue(ctx, agentPreparationLoanKey{}, parent)
			tx := s.tx
			next := r
			switch name {
			case "other-tx":
				tx = f.NewTx()
			case "other-agent":
				next.AgentID = testID[i.Agent](t)
			case "other-version":
				next.ExpectedConfigVersion++
			case "released-parent":
				releaseParent()
			}
			child, err := scope.borrow(parentCtx, tx, next)
			if name != "active-exact" {
				if err == nil {
					scope.release(child)
					t.Fatal("nested mismatch or closed loan revived")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var childOnce sync.Once
			releaseChild := func() { childOnce.Do(func() { scope.release(child) }) }
			t.Cleanup(releaseChild)
			childCtx := context.WithValue(parentCtx, agentPreparationLoanKey{}, child)
			releaseParent() // Must not wait on this still-live child or on itself.
			if extra, err := scope.borrow(childCtx, s.tx, r); err == nil {
				scope.release(extra)
				t.Fatal("inactive ancestor revived through its child")
			}
			// Releasing a loan does not seal the original step's admission.
			fresh, err := scope.borrow(ctx, s.tx, r)
			if err != nil {
				t.Fatal("original active step lost permission", err)
			}
			scope.release(fresh)
			closed := make(chan struct{})
			go func() { scope.close(); close(closed) }()
			t.Cleanup(func() { releaseChild(); retirementPrepAwait(t, closed) })
			retirementPrepAwait(t, scope.ctx.Done())
			select {
			case <-closed:
				t.Fatal("scope joined before original nested return")
			default:
			}
			if extra, err := scope.borrow(ctx, s.tx, r); err == nil {
				scope.release(extra)
				t.Fatal("closing scope admitted new work")
			}
			releaseChild()
			retirementPrepAwait(t, closed)
		})
	}
}

func TestAgentRetirementPreparationInvokeWaitsForOriginalRead(t *testing.T) {
	for _, name := range []string{"step-return", "caller-cancel"} {
		t.Run(name, func(t *testing.T) {
			r := retirementPrepRequest(t)
			stepErr := errors.New("original step result")
			stepRelease := make(chan struct{})
			readRelease := make(chan struct{})
			readDone := make(chan struct{})
			invokeDone := make(chan struct{})
			scopeReady := make(chan struct{})
			var stepOnce, readOnce sync.Once
			freeStep := func() { stepOnce.Do(func() { close(stepRelease) }) }
			freeRead := func() { readOnce.Do(func() { close(readRelease) }) }
			var owner *AgentRetirementPreparation
			var s *retirementPrepStore
			var originalScope *agentPreparationScope
			var scopeCtx context.Context
			var readErr, invokeErr error
			d, prepared, store := retirementPrepFixture(t, func(ctx context.Context, actor i.Actor, _ c.LifecycleCause, _ c.ScopeRef) error {
				r.Actor = actor
				scopeCtx = ctx
				originalScope = ctx.Value(agentPreparationScopeKey{}).(*agentPreparationScope)
				close(scopeReady)
				go func() {
					defer close(readDone)
					readErr = owner.WithAgentRetirementPreparationInTx(ctx, s.tx, r, func(context.Context) error { t.Error("controlled read bypassed SQL failure"); return nil })
				}()
				<-stepRelease
				return stepErr
			})
			owner = prepared
			s = store
			retirementPrepHeld(t, s, r)
			s.readEntered = make(chan struct{})
			s.readRelease = readRelease
			s.onRead = func() {
				if !originalScope.mu.TryLock() {
					t.Error("scope mutex held across SQL port")
					return
				}
				originalScope.mu.Unlock()
			}
			ctx, cancel := context.WithCancel(context.Background())
			claim := retirementPrepClaim(t, d, r)
			go func() {
				defer close(invokeDone)
				invokeErr = d.state.invoke(agentPreparationClaimContext(ctx, d.state, claim, r.Cause), r.ProjectID, r.Cause.OperationID, r.Cause)
			}()
			t.Cleanup(func() {
				cancel()
				freeStep()
				freeRead()
				retirementPrepAwait(t, invokeDone)
				retirementPrepAwait(t, readDone)
			})
			retirementPrepAwait(t, scopeReady)
			retirementPrepAwait(t, s.readEntered)
			if name == "caller-cancel" {
				cancel()
			}
			freeStep()
			retirementPrepAwait(t, originalScope.ctx.Done())
			select {
			case <-invokeDone:
				t.Fatal("invoke returned while original Scan was held")
			default:
			}
			select {
			case <-readDone:
				t.Fatal("read returned before original Scan")
			default:
			}
			before := s.seen()
			err := owner.WithAgentRetirementPreparationInTx(scopeCtx, s.tx, r, func(context.Context) error { t.Error("closed scope callback invoked"); return nil })
			if err == nil || !slices.Equal(before, s.seen()) {
				t.Fatal("closing scope readmitted a borrow", err)
			}
			freeRead()
			retirementPrepAwait(t, readDone)
			retirementPrepAwait(t, invokeDone)
			if !errors.Is(readErr, s.readErr) || !errors.Is(invokeErr, stepErr) {
				t.Fatal("original return error was replaced", readErr, invokeErr)
			}
			if !slices.Equal(s.seen(), []string{"borrow", "locks", "row"}) {
				t.Fatal("unexpected transaction/write or second read", s.seen())
			}
			originalScope.mu.Lock()
			remaining := len(originalScope.loans)
			originalScope.mu.Unlock()
			if remaining != 0 {
				t.Fatal("actual read return did not retire original loan")
			}
		})
	}
}
