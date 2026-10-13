package model

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	wire "github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/usage"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
	"github.com/jackc/pgx/v5"
)

// These fixtures exercise the real Runtime witnesses and original Store
// returns. They do not initialize a ProcessGuard, invoke a Model, or supply a
// Project SQL grant. The controlled canonical row is decoded by the real
// runtime reader when testing the original read-only confirmation path.
type usageRuntimeStore struct {
	Store
	mu     sync.Mutex
	active f.Tx
	calls  int
	record *runtimeRecord
	result func(int, f.TransactionCause) f.CommitResult
	after  func(int)
}

func (s *usageRuntimeStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.mu.Lock()
	s.calls++
	n := s.calls
	s.active = f.NewTx()
	tx := s.active
	s.mu.Unlock()
	err := fn(ctx, tx)
	s.mu.Lock()
	s.active = f.Tx{}
	s.mu.Unlock()
	result := f.CommittedResult()
	if err != nil {
		result = f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted).WithCause(err))
	} else if s.result != nil {
		result = s.result(n, cause)
	}
	if s.after != nil {
		s.after(n)
	}
	return result
}
func (s *usageRuntimeStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !tx.Valid() || tx != s.active {
		return nil, fault(f.Forbidden)
	}
	return s, nil
}
func (s *usageRuntimeStore) AcquireAll(_ context.Context, tx f.Tx, _ []f.LockRequest) error {
	_, err := s.InTx(tx)
	return err
}
func (s *usageRuntimeStore) RequireHeldLocks(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	return s.AcquireAll(ctx, tx, locks)
}
func (s *usageRuntimeStore) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	if !strings.Contains(query, "FROM agenteam_model.calls c JOIN agenteam_model.runtime_attempts") {
		panic("unexpected SQL in read-only confirmation")
	}
	return usageRuntimeRow{s.record}
}

type usageRuntimeRow struct{ record *runtimeRecord }

func (r usageRuntimeRow) Scan(out ...any) error {
	if r.record == nil {
		return pgx.ErrNoRows
	}
	x := r.record
	binding, err := encoded(x.binding)
	if err != nil {
		return err
	}
	attempt, err := encoded(runtimeAttemptDTO{1, x.sequence, x.value})
	if err != nil {
		return err
	}
	var finished *time.Time
	if x.value.Final != nil {
		v := x.value.Final.FinishedAt.Time()
		finished = &v
	}
	values := []any{x.binding.Consumer.ProjectID.String(), x.binding.SnapshotID.String(), x.binding.PreparationID, x.binding.LeaseID.String(), x.value.ID.String(), x.value.ProcessID.String(), int64(x.value.Fence), x.digest, binding, x.phase, x.retired, x.version, x.value.StartedAt.Time(), finished, int64(x.value.AttemptIndex), x.value.Dispatch, attempt}
	if len(out) != len(values) {
		return errors.New("unexpected runtime row projection")
	}
	for i, v := range values {
		destination, source := reflect.ValueOf(out[i]).Elem(), reflect.ValueOf(v)
		if destination.Type() != source.Type() {
			return errors.New("unexpected runtime row type")
		}
		destination.Set(source)
	}
	return nil
}

type usageRuntimeWriter struct {
	uc.Writer
	authority *RuntimeAuthority
	store     *usageRuntimeStore
	acquire   func(*usageRuntimeToken) error
	returned  func(*usageRuntimeToken) error
	returns   int
	lookup    func(context.Context, uc.InvocationRequest) (uc.InvocationLookup, error)
}

func (w *usageRuntimeWriter) ProjectInvocationBorrower() uc.ProjectInvocationBorrower { return w }
func (w *usageRuntimeWriter) AcquireProjectInvocation(ctx context.Context, store any, request mc.ModelRequest) (uc.ProjectInvocationCall, error) {
	if err := w.authority.RequireOriginalInvocationCall(ctx, store, request, nil); err != nil {
		return nil, err
	}
	token := &usageRuntimeToken{writer: w, store: store, request: request.Clone(), ctx: ctx}
	if w.acquire != nil {
		return token, w.acquire(token)
	}
	return token, nil
}
func (w *usageRuntimeWriter) LookupInvocation(ctx context.Context, request uc.InvocationRequest) (uc.InvocationLookup, error) {
	return w.lookup(ctx, request)
}

type usageRuntimeToken struct {
	writer  *usageRuntimeWriter
	store   any
	request mc.ModelRequest
	ctx     context.Context
}

func (x *usageRuntimeToken) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	if err := x.writer.authority.RequireOriginalInvocationCall(ctx, x.store, x.request, x); err != nil {
		return f.NotCommittedResult(f.NewFault(f.Forbidden, f.NotCommitted).WithCause(err))
	}
	return x.writer.store.WithinTx(ctx, cause, fn)
}
func (x *usageRuntimeToken) Returned(ctx context.Context) error {
	if err := x.writer.authority.RequireOriginalInvocationReturned(ctx, x.store, x.request, x); err != nil {
		return err
	}
	x.writer.returns++
	if x.writer.returned != nil {
		return x.writer.returned(x)
	}
	return nil
}

type usageRuntimeFixture struct {
	runtime   *Runtime
	store     *usageRuntimeStore
	writer    *usageRuntimeWriter
	request   mc.ModelRequest
	ctx       context.Context
	owner     *runtimeUsageCall
	admission *runtimeAdmission
}

func newUsageRuntimeFixture(t *testing.T) *usageRuntimeFixture {
	t.Helper()
	s := &usageRuntimeStore{}
	a := runtimePureAuthority(t, s, runtimeConsumerFunc(func(context.Context, mc.ConsumerRequest) (mc.ConsumerDependencies, error) {
		panic("pure ownership cannot grant an Invoke")
	}))
	r := runtimePureService(a)
	a.state().runtime = r.state()
	w := &usageRuntimeWriter{authority: a, store: s}
	r.state().deps.Usage = w
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ctx, owner := r.usageOuter(ctx)
	admission := &runtimeAdmission{cancel: cancel, done: make(chan struct{})}
	r.state().admissions[admission] = struct{}{}
	return &usageRuntimeFixture{r, s, w, runtimePureRequest(t), ctx, owner, admission}
}
func (v *usageRuntimeFixture) acquire(t *testing.T) uc.ProjectInvocationCall {
	t.Helper()
	o, err := acquireRuntimeUsage(v.ctx, v.runtime.state(), v.admission, v.request)
	if err != nil || o != v.owner || nilPort(o.token) {
		t.Fatal("original admission did not acquire its token", err)
	}
	return o.token
}
func (v *usageRuntimeFixture) admissionDone() {
	v.runtime.state().mu.Lock()
	delete(v.runtime.state().admissions, v.admission)
	close(v.admission.done)
	v.runtime.state().mu.Unlock()
	v.owner.admissionDone()
}
func (v *usageRuntimeFixture) call(t *testing.T) *runtimeCall {
	t.Helper()
	v.acquire(t)
	c := &runtimeCall{runtime: v.runtime.state(), request: v.request, ctx: v.ctx, cancel: v.admission.cancel, usage: v.owner, gate: make(chan struct{}, 1)}
	v.runtime.state().calls[v.request.CallID] = c
	v.owner.transfer(c)
	v.admissionDone()
	return c
}

type usageRuntimeProjects struct{ usage.ProjectAuthority }

func TestRuntimeUsageOwnershipPreservesLegacyService(t *testing.T) {
	v := newUsageRuntimeFixture(t)
	a, err := usage.NewAuthority(v.store, usage.Authorizations{Sessions: sessionFunc(func(context.Context, f.Tx, id.Actor) error { panic("unexpected authority call") }), Projects: &usageRuntimeProjects{}, Invocations: v.writer.authority})
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := usage.New(v.store, a, usage.Dependencies{Cursors: testKeys(t)})
	if err != nil {
		t.Fatal(err)
	}
	v.runtime.state().deps.Usage = legacy
	ctx, o := v.runtime.usageOuter(context.Background())
	if o != nil || ctx == nil || runtimeUsageBorrower(v.runtime.state()) != nil {
		t.Fatal("legacy concrete Usage service implicitly opted into ownership")
	}
	if o, err = acquireRuntimeUsage(ctx, v.runtime.state(), v.admission, v.request); o != nil || err != nil || v.store.calls != 0 {
		t.Fatal("legacy path gained registration or I/O", err)
	}
}

func TestRuntimeUsageOwnershipRequiresOriginalScope(t *testing.T) {
	for _, name := range []string{"missing-context", "other-store", "other-authority", "other-call", "other-project", "changed-input", "changed-actor", "other-token", "returned-admission"} {
		t.Run(name, func(t *testing.T) {
			v := newUsageRuntimeFixture(t)
			token := v.acquire(t)
			ctx, store, request, a := v.ctx, any(v.store), v.request.Clone(), v.writer.authority
			switch name {
			case "missing-context":
				ctx = context.Background()
			case "other-store":
				store = &usageRuntimeStore{}
			case "other-authority":
				a = runtimePureAuthority(t, v.store, runtimeConsumerFunc(func(context.Context, mc.ConsumerRequest) (mc.ConsumerDependencies, error) {
					panic("unexpected authority")
				}))
			case "other-call":
				request.CallID = mustID[mc.Call](t)
			case "other-project":
				request.Consumer.ProjectID = mustID[id.Project](t)
				request.Model.Consumer = request.Consumer.Clone()
			case "changed-input":
				request.Messages[0].Parts[0].Text.Text = "changed-private-input"
			case "changed-actor":
				request.Actor = testActor(t)
			case "other-token":
				token = &usageRuntimeToken{writer: v.writer}
			case "returned-admission":
				v.admissionDone()
				if err := v.owner.leave(nil); err != nil {
					t.Fatal(err)
				}
			}
			if err := a.RequireOriginalInvocationCall(ctx, store, request, token); err == nil || v.store.calls != 0 {
				t.Fatal("foreign or expired original-call witness accepted")
			}
		})
	}
}

func TestRuntimeUsageOwnershipRegistrationUnknownKeepsOriginalReturn(t *testing.T) {
	v := newUsageRuntimeFixture(t)
	cause, _ := f.NewRecoveryCause("usage-register-controlled", mustID[struct{}](t).String(), v.request.Consumer.ProjectID.String())
	raw := f.UnknownResult(mustID[f.TransactionAttempt](t), cause)
	first := commitError(raw)
	entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var once sync.Once
	open := func() { once.Do(func() { close(release) }) }
	t.Cleanup(open)
	v.writer.acquire = func(token *usageRuntimeToken) error {
		if v.writer.authority.RequireOriginalInvocationCall(v.ctx, v.store, v.request, token) == nil || v.writer.authority.RequireOriginalInvocationReturned(v.ctx, v.store, v.request, token) == nil {
			return errors.New("unpublished token granted mutation or return")
		}
		// Stop may cancel the original admission before its Acquire Store has
		// returned the token, but cannot use that unpublished token for SQL.
		if err := v.writer.authority.CancelOriginalInvocation(v.ctx, v.store, v.request, token); err != nil {
			return err
		}
		return first
	}
	v.writer.returned = func(*usageRuntimeToken) error { close(entered); <-release; return first }
	finished := make(chan struct{})
	t.Cleanup(func() { open(); usageRuntimeJoin(t, finished) })
	go func() {
		defer close(finished)
		o, err := acquireRuntimeUsage(v.ctx, v.runtime.state(), v.admission, v.request)
		if o != v.owner || !errors.Is(err, first) || nilPort(o.token) {
			returned <- errors.New("registration lost original token/error")
			return
		}
		v.admissionDone()
		returned <- o.leave(err)
	}()
	select {
	case <-entered:
	case err := <-returned:
		t.Fatal("registration did not reach original return", err)
	case <-time.After(5 * time.Second):
		t.Fatal("registration return did not start")
	}
	v.runtime.StopAdmission()
	if v.ctx.Err() == nil || v.runtime.Joined() {
		t.Fatal("cancellation or original technical return was lost")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if v.runtime.Drain(ctx) == nil {
		t.Fatal("Drain bypassed the original held return")
	}
	open()
	select {
	case err := <-returned:
		if !errors.Is(err, first) || v.runtime.Joined() {
			t.Fatal("technical Unknown lost the first error or owner", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("original return did not finish")
	}
	v.writer.returned = nil // the next explicit controlled confirmation succeeds
	if err := v.runtime.Drain(context.Background()); err != nil || !v.runtime.Joined() || v.writer.returns != 2 {
		t.Fatal("same retained token did not close explicitly", err)
	}
}

func TestRuntimeUsageOwnershipWaitsForWholeCall(t *testing.T) {
	v := newUsageRuntimeFixture(t)
	c := v.call(t)
	if err := v.owner.leave(nil); err != nil || v.writer.returns != 0 {
		t.Fatal("between-mutation lifetime retired before future Finalize", err)
	}
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var once sync.Once
	open := func() { once.Do(func() { close(release) }) }
	t.Cleanup(open)
	v.writer.returned = func(token *usageRuntimeToken) error {
		if token.request.CallID == v.request.CallID {
			close(entered)
			<-release
		}
		return nil
	}
	h := &runtimeJSONCall{call: c, started: true, returned: true}
	finished := make(chan struct{})
	var businessOnce sync.Once
	releaseBusiness := func() { businessOnce.Do(func() { c.releaseLocal(); c.leave() }) }
	t.Cleanup(func() { releaseBusiness(); open(); usageRuntimeJoin(t, finished) })
	go func() { defer close(finished); done <- h.Close(context.Background()) }()
	select {
	case <-c.ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not cancel its original call")
	}
	if h.Joined() || v.writer.returns != 0 {
		t.Fatal("cancellation retired Usage before original work returned")
	}
	// Complete only the controlled original business tail, then let the real
	// Close finish its original gate and its Usage technical Store return.
	releaseBusiness()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not reach the technical return")
	}
	if h.Joined() {
		t.Fatal("internal releaseLocal was mistaken for the outer return")
	}
	// A separate P on the same original Runtime/Usage pair can return while
	// A's original technical return is still held.
	otherCtx, otherCancel := context.WithCancel(context.Background())
	t.Cleanup(otherCancel)
	otherCtx, otherOwner := v.runtime.usageOuter(otherCtx)
	otherAdmission := &runtimeAdmission{cancel: otherCancel, done: make(chan struct{})}
	v.runtime.state().mu.Lock()
	v.runtime.state().admissions[otherAdmission] = struct{}{}
	v.runtime.state().mu.Unlock()
	other := &usageRuntimeFixture{v.runtime, v.store, v.writer, runtimePureRequest(t), otherCtx, otherOwner, otherAdmission}
	other.acquire(t)
	other.admissionDone()
	if err := other.owner.leave(nil); err != nil || other.writer.returns != 2 {
		t.Fatal("held call blocked the unrelated project fixture", err)
	}
	v.runtime.StopAdmission()
	if v.runtime.Joined() {
		t.Fatal("root joined before the original Usage return")
	}
	open()
	select {
	case err := <-done:
		if err != nil || !h.Joined() || !v.runtime.Joined() {
			t.Fatal("original Close/technical return did not join", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return")
	}
}

func usageRuntimePending(t *testing.T, v *usageRuntimeFixture) (*runtimeCall, *runtimeMutation) {
	t.Helper()
	c := v.call(t)
	r := v.request
	deadline, _ := f.NewInstant(time.Now().Add(time.Minute))
	at, _ := f.NewInstant(time.Now().Add(-time.Second))
	maxAttempts := f.Sequence(1)
	snapshot, _ := encoded(r.Model.Snapshot)
	b := runtimeBinding{Format: 1, CallID: r.CallID, Consumer: r.Consumer.Clone(), Input: r.Input.Clone(), Initiator: runtimeStableActor(r.Actor), SnapshotID: r.Model.Snapshot.ID, SnapshotDigest: hash(snapshot), PreparationID: mustID[struct{}](t).String(), LeaseID: r.Model.CredentialLease.LeaseID, Owner: r.Model.LeaseOwner.Details(), Policy: mc.RetryPolicy{Class: mc.BoundedRetry, Deadline: &deadline, MaxAttempts: &maxAttempts}, Mode: wire.JSONResponse}
	rawBinding, _ := encoded(b)
	record := &runtimeRecord{binding: b, digest: hash(rawBinding), value: uc.Invocation{ID: mustID[mc.Invocation](t), CallID: r.CallID, AttemptIndex: 1, Consumer: r.Consumer.Clone(), SnapshotID: r.Model.Snapshot.ID, Identity: r.Model.Snapshot.Identity, ProcessID: mustID[oc.Process](t), Fence: 1, Dispatch: uc.Reserved, StartedAt: at, Usage: mc.Usage{Source: mc.UnknownUsage}}, sequence: 1, version: 1, phase: "accepted"}
	if err := record.validate(); err != nil {
		t.Fatal("invalid controlled canonical runtime row", err)
	}
	c.record = record
	request, err := c.invocationRequest(uc.ReserveAction, record)
	if err != nil {
		t.Fatal(err)
	}
	binding, _ := uc.InvocationBinding(request)
	locks, _ := runtimeCallLocks(r.Consumer.ProjectID, r.CallID, record.value.ID)
	mapping := hash([]byte("controlled-original-mutation"))
	facts, err := uc.NewInvocationDependencies(v.writer.authority.state().usageIssuer, uc.InvocationDependencyDetails{Binding: binding, Mapping: mapping, Locks: locks})
	if err != nil {
		t.Fatal(err)
	}
	cause, _ := runtimeCallCause(r.Consumer.ProjectID, r.CallID)
	plan, err := uc.NewInvocationPlan(uc.NewPlanIssuer(), uc.InvocationPlanDetails{Binding: binding, Mapping: mapping, Locks: locks, Facts: facts, Cause: cause})
	if err != nil {
		t.Fatal(err)
	}
	raw := f.UnknownResult(mustID[f.TransactionAttempt](t), cause)
	m := &runtimeMutation{call: c, desired: record, action: uc.ReserveAction, request: request, plan: plan, locks: locks, mapping: mapping, active: true, originalResult: raw, originalUnknown: commitError(raw)}
	c.pending = m
	v.store.record = record
	v.writer.lookup = func(ctx context.Context, request uc.InvocationRequest) (uc.InvocationLookup, error) {
		confirmedFacts, err := v.writer.authority.Discover(ctx, request)
		if err != nil {
			return uc.InvocationLookup{}, err
		}
		var receipt uc.InvocationReceipt
		result := v.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
			if request.Access != uc.ConfirmInvocation {
				return errors.New("confirmation requested a new mutation")
			}
			if _, err := v.writer.authority.ValidateInTx(ctx, tx, request, confirmedFacts); err != nil {
				return err
			}
			receipt = uc.InvocationReceipt{Action: m.action, Sequence: m.desired.sequence, Digest: mapping, Value: m.desired.value.Clone()}
			return nil
		})
		if err := commitError(result); err != nil {
			return uc.InvocationLookup{}, err
		}
		return uc.InvocationLookup{Observed: true, Receipt: &receipt}, nil
	}
	return c, m
}

func TestRuntimeUsageOwnershipConfirmsBothOriginalStores(t *testing.T) {
	v := newUsageRuntimeFixture(t)
	c, m := usageRuntimePending(t, v)
	entered := []chan struct{}{make(chan struct{}), make(chan struct{})}
	release := []chan struct{}{make(chan struct{}), make(chan struct{})}
	var opens [2]sync.Once
	open := func(i int) { opens[i].Do(func() { close(release[i]) }) }
	t.Cleanup(func() { open(0); open(1) })
	v.store.after = func(n int) { close(entered[n-1]); <-release[n-1] }
	done := make(chan error, 1)
	finished := make(chan struct{})
	t.Cleanup(func() { open(0); open(1); usageRuntimeJoin(t, finished) })
	go func() { defer close(finished); done <- c.confirmMutation(v.ctx, m) }()
	for i := range 2 {
		select {
		case <-entered[i]:
		case err := <-done:
			t.Fatal("confirmation returned before both stores", err)
		case <-time.After(5 * time.Second):
			t.Fatal("confirmation original Store did not arrive")
		}
		if c.pending != m || v.writer.returns != 0 || m.originalResult.AttemptID().Validate() != nil {
			t.Fatal("first mutation was retired before original read Store return")
		}
		if err := v.writer.authority.RequireOriginalInvocationReturned(v.ctx, v.store, v.request, v.owner.token); err == nil {
			t.Fatal("confirmation Store callback was mistaken for whole-call return")
		}
		open(i)
	}
	select {
	case err := <-done:
		if err != nil || c.pending != nil || !c.accepted || v.store.calls != 2 {
			t.Fatal("original read-only confirmation failed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("confirmation did not return")
	}
	if err := v.owner.leave(nil); err != nil || v.writer.returns != 0 {
		t.Fatal("confirming one mutation released future Finalize ownership", err)
	}
}

func TestRuntimeUsageOwnershipKeepsFirstUnknown(t *testing.T) {
	for _, stage := range []string{"model-read", "usage-read", "cancelled"} {
		t.Run(stage, func(t *testing.T) {
			v := newUsageRuntimeFixture(t)
			c, m := usageRuntimePending(t, v)
			v.store.result = func(n int, _ f.TransactionCause) f.CommitResult {
				if stage == "model-read" && n == 1 || stage == "usage-read" && n == 2 {
					cause, _ := f.NewRecoveryCause("different-read-attempt", mustID[struct{}](t).String(), "")
					return f.UnknownResult(mustID[f.TransactionAttempt](t), cause)
				}
				return f.CommittedResult()
			}
			ctx := v.ctx
			if stage == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			err := c.confirmMutation(ctx, m)
			var original *UnknownCommandError
			if err != m.originalUnknown || !errors.As(err, &original) || original.AttemptID() != m.originalResult.AttemptID() || !reflect.DeepEqual(original.Cause().Details(), m.originalResult.Cause().Details()) || c.pending != m || v.writer.returns != 0 {
				t.Fatal("failed confirmation replaced the original mutation or its owner", err)
			}
		})
	}
}

func usageRuntimeJoin(t *testing.T, finished <-chan struct{}) {
	t.Helper()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Error("controlled original call did not return during cleanup")
	}
}

func TestRuntimeUsageOwnershipOriginalStoreResult(t *testing.T) {
	for _, state := range []f.CommitState{f.Committed, f.NotCommitted, f.Unknown} {
		t.Run(string(state), func(t *testing.T) {
			v := newUsageRuntimeFixture(t)
			c := v.call(t)
			cause, _ := runtimeCallCause(v.request.Consumer.ProjectID, v.request.CallID)
			want := f.CommittedResult()
			switch state {
			case f.NotCommitted:
				want = f.NotCommittedResult(f.NewFault(f.Forbidden, f.NotCommitted))
			case f.Unknown:
				want = f.UnknownResult(mustID[f.TransactionAttempt](t), cause)
			}
			v.store.result = func(int, f.TransactionCause) f.CommitResult { return want }
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			open := func() { once.Do(func() { close(release) }) }
			finished := make(chan struct{})
			t.Cleanup(func() { open(); usageRuntimeJoin(t, finished) })
			v.store.after = func(int) { close(entered); <-release }
			done := make(chan f.CommitResult, 1)
			go func() {
				defer close(finished)
				done <- c.withinUsage(v.ctx, cause, func(ctx context.Context, tx f.Tx) error {
					_, err := v.store.InTx(tx)
					return err
				})
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("original Store callback was not reached")
			}
			if err := v.writer.authority.RequireOriginalInvocationReturned(v.ctx, v.store, v.request, v.owner.token); err == nil {
				t.Fatal("callback completion closed original caller Store")
			}
			select {
			case <-done:
				t.Fatal("result escaped before original Store returned")
			default:
			}
			open()
			select {
			case got := <-done:
				if !reflect.DeepEqual(got, want) || v.store.calls != 1 || v.writer.returns != 0 {
					t.Fatal("wrapper changed original result/cause/attempt")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("original Store did not return")
			}
		})
	}
}

func TestRuntimeUsageOwnershipUnfinishedTailRemainsOwned(t *testing.T) {
	for _, tail := range []string{"mutation", "retirement", "material", "exchange", "start-return"} {
		t.Run(tail, func(t *testing.T) {
			v := newUsageRuntimeFixture(t)
			c := v.call(t)
			c.joined = true // internal end alone is deliberately insufficient
			switch tail {
			case "mutation":
				c.pending = &runtimeMutation{}
			case "retirement":
				c.retirement = &runtimeRetirement{}
			case "material":
				c.materialOwned = true
			case "exchange":
				c.exchange = &wire.Exchange{}
			case "start-return":
				c.handoff = true
			}
			if err := v.owner.leave(nil); err != nil || v.writer.returns != 0 || c.usageJoined() {
				t.Fatal("unreturned original resource tail released Usage", err)
			}
			if err := v.writer.authority.RequireOriginalInvocationReturned(v.ctx, v.store, v.request, v.owner.token); err == nil {
				t.Fatal("private return proof ignored an unfinished tail")
			}
		})
	}
}
