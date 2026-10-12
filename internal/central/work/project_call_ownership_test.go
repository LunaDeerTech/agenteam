package work

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
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// These are controlled technical rows and an explicitly controlled stop loan.
// They exercise the original owner/Store call and actual live-Tx lock checks,
// not real Project permission, PostgreSQL commits, or foreign-process death.
type callOwnerStore struct {
	Store
	mu                   sync.Mutex
	live                 map[f.Tx][]f.LockRequest
	rows                 map[string]projectCallRecord
	calls, reads, writes int
	registration         func(context.Context, f.TransactionCause, *projectCallOwner) f.CommitResult
	after                func(context.Context, f.TransactionCause, int, f.CommitResult) f.CommitResult
}

func (s *callOwnerStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.mu.Lock()
	s.calls++
	n := s.calls
	s.mu.Unlock()
	if cause.Details().Owner == "work-call-register" && s.registration != nil {
		w, ok := ctx.Value(projectCallContextKey{}).(projectCallWitness)
		if !ok || w.owner == nil {
			return f.NotCommittedResult(fault(f.Forbidden))
		}
		return s.registration(ctx, cause, w.owner)
	}
	tx := f.NewTx()
	s.mu.Lock()
	s.live[tx] = nil
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
	if s.after != nil {
		return s.after(ctx, cause, n, result)
	}
	return result
}
func (s *callOwnerStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.live[tx]; !ok {
		return nil, fault(f.Forbidden)
	}
	return s, nil
}
func (s *callOwnerStore) AcquireAll(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.live[tx]; !ok {
		return fault(f.Forbidden)
	}
	s.live[tx] = append([]f.LockRequest(nil), locks...)
	return ctx.Err()
}
func (s *callOwnerStore) RequireHeldLocks(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	held, ok := s.live[tx]
	if !ok {
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
func (s *callOwnerStore) Query(context.Context, string, ...any) (*postgres.Rows, error) {
	return nil, fault(f.Forbidden)
}

type callOwnerRow struct {
	raw []byte
	err error
}

func (r callOwnerRow) Scan(out ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(out) != 1 {
		return fault(f.InternalError)
	}
	p, ok := out[0].(*[]byte)
	if !ok {
		return fault(f.InternalError)
	}
	*p = append([]byte(nil), r.raw...)
	return nil
}
func (s *callOwnerStore) QueryRow(_ context.Context, query string, args ...any) postgres.Row {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads++
	var value any
	if strings.Contains(query, "jsonb_agg") && len(args) == 2 {
		p, ok := args[0].(string)
		reads, ok2 := args[1].(bool)
		if !ok || !ok2 {
			return callOwnerRow{err: fault(f.InternalError)}
		}
		rows := []projectCallRecord{}
		for _, row := range s.rows {
			if row.Project.String() == p && (reads || row.Class == projectCallMutation) {
				rows = append(rows, row)
			}
		}
		slices.SortFunc(rows, func(a, b projectCallRecord) int { return strings.Compare(a.ID.String(), b.ID.String()) })
		value = rows
	} else if strings.Contains(query, "WHERE id=$1") && len(args) == 1 {
		id, ok := args[0].(string)
		if !ok {
			return callOwnerRow{err: fault(f.InternalError)}
		}
		row, ok := s.rows[id]
		if !ok {
			return callOwnerRow{err: pgx.ErrNoRows}
		}
		value = row
	} else {
		return callOwnerRow{err: fault(f.Forbidden)}
	}
	raw, err := json.Marshal(value)
	return callOwnerRow{raw: raw, err: err}
}
func (s *callOwnerStore) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.HasPrefix(query, "INSERT INTO agenteam_work.project_calls") && len(args) == 6 {
		id, e1 := f.ParseID[projectCallID](args[0].(string))
		p, e2 := f.ParseID[i.Project](args[1].(string))
		process, e3 := f.ParseID[oc.Process](args[2].(string))
		instance, e4 := f.ParseID[projectCallInstance](args[3].(string))
		r := projectCallRecord{ID: id, Project: p, Process: process, Instance: instance, Lane: projectCallLane(args[4].(string)), Class: projectCallClass(args[5].(string)), Phase: "running"}
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
	if !strings.HasPrefix(query, "UPDATE agenteam_work.project_calls SET phase='business_closed'") || len(args) != 6 {
		return pgconn.CommandTag{}, fault(f.Forbidden)
	}
	id, ok := args[0].(string)
	if !ok {
		return pgconn.CommandTag{}, fault(f.Forbidden)
	}
	r, ok := s.rows[id]
	if !ok || r.Phase != "running" || args[1] != r.Project.String() || args[2] != r.Process.String() || args[3] != r.Instance.String() || args[4] != string(r.Lane) || args[5] != string(r.Class) {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	r.Phase = "business_closed"
	s.rows[id] = r
	s.writes++
	return pgconn.NewCommandTag("UPDATE 1"), nil
}
func (s *callOwnerStore) counts() (int, int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls, s.reads, s.writes
}
func (s *callOwnerStore) put(r projectCallRecord) {
	s.mu.Lock()
	s.rows[r.ID.String()] = r
	s.mu.Unlock()
}

type callOwnerProof struct {
	store    *callOwnerStore
	confirms int
	confirm  func(context.Context, oc.ProcessID) error
}

func (p *callOwnerProof) RequireCurrentProcessInTx(ctx context.Context, tx f.Tx, _ oc.ProcessID) error {
	_, err := p.store.InTx(tx)
	if err != nil {
		return err
	}
	return ctx.Err()
}
func (p *callOwnerProof) ConfirmStopped(ctx context.Context, id oc.ProcessID) error {
	p.store.mu.Lock()
	live := len(p.store.live)
	p.store.mu.Unlock()
	if live != 0 {
		return fault(f.Forbidden)
	}
	p.confirms++
	if p.confirm != nil {
		return p.confirm(ctx, id)
	}
	return ctx.Err()
}

type callOwnerLoanKey struct{}
type callOwnerAuthority struct{ store *callOwnerStore }

func (a *callOwnerAuthority) WithProjectStop(ctx context.Context, _ i.Actor, _ pc.LifecycleCause, _ pc.ScopeRef, fn func(context.Context) error) error {
	return fn(context.WithValue(ctx, callOwnerLoanKey{}, a))
}
func (a *callOwnerAuthority) RequireProjectStopInTx(ctx context.Context, tx f.Tx, _ i.Actor, _ pc.LifecycleCause, scope pc.ScopeRef) error {
	if ctx.Value(callOwnerLoanKey{}) != a {
		return fault(f.Forbidden)
	}
	return a.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(scope.ProjectID, f.Exclusive)})
}

type callOwnerFixture struct {
	pair  *ProjectCallOwnership
	store *callOwnerStore
	proof *callOwnerProof
	actor i.Actor
	p, b  i.ProjectID
	cause pc.LifecycleCause
}

func newCallOwnerFixture(t *testing.T) *callOwnerFixture {
	t.Helper()
	pair, _ := newEntryOwnership(t)
	s := &callOwnerStore{live: map[f.Tx][]f.LockRequest{}, rows: map[string]projectCallRecord{}}
	proof := &callOwnerProof{store: s}
	// Business ports remain the original denial ports. Only the technical
	// owner uses these controlled rows; no business permission is fabricated.
	pair.state.store, pair.state.proof, pair.state.authority = s, proof, &callOwnerAuthority{store: s}
	v := &callOwnerFixture{pair: pair, store: s, proof: proof, actor: pureActor(t, 2), p: pureID[i.Project](t, 5), b: pureID[i.Project](t, 6), cause: pc.LifecycleCause{OperationID: pureID[pc.Operation](t, 70), Action: pc.Delete, ProjectVersion: 2}}
	if err := v.cause.Validate(); err != nil {
		t.Fatal(err)
	}
	return v
}
func (v *callOwnerFixture) stop(ctx context.Context, p i.ProjectID, request bool) (pc.StopReport, error) {
	scope := pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: p}
	if request {
		return v.pair.RequestProjectStop(ctx, v.actor, v.cause, scope)
	}
	return v.pair.InspectProjectStop(ctx, v.actor, v.cause, scope)
}
func (v *callOwnerFixture) row(t *testing.T, n int, p i.ProjectID) projectCallRecord {
	t.Helper()
	id, err := f.ParseID[projectCallID](fmt.Sprintf("01900000-0000-7000-8000-%012x", n))
	if err != nil {
		t.Fatal(err)
	}
	return projectCallRecord{ID: id, Project: p, Process: v.pair.state.process, Instance: v.pair.state.instance, Lane: projectCallTask, Class: projectCallMutation, Phase: "running"}
}
func (v *callOwnerFixture) local(t *testing.T, n int, p i.ProjectID, class projectCallClass) (context.Context, *projectCallOwner, func()) {
	t.Helper()
	ctx, entry, done, err := v.pair.TaskService().begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	r := v.row(t, n, p)
	r.Class = class
	o := &projectCallOwner{record: r, entry: entry, ctx: ctx, registration: f.CommittedResult(), registrationKnown: true, classified: true, confirmations: map[*confirmation]struct{}{}}
	entry.project, entry.class, entry.owner = p, class, o
	v.pair.state.mu.Lock()
	v.pair.state.owners[r.ID] = o
	v.pair.state.mu.Unlock()
	v.store.put(r)
	return ctx, o, done
}
func requireCallStop(t *testing.T, r pc.StopReport, err error, want pc.StopState) {
	t.Helper()
	if err != nil || r.Details().State != want {
		t.Fatalf("stop state %s, error %v", r.Details().State, err)
	}
}
func requireCallUnknown(t *testing.T, err error, original f.CommitResult) {
	t.Helper()
	var failure commitFailure
	if !errors.As(err, &failure) || failure.result.AttemptID() != original.AttemptID() {
		t.Fatal("original technical Unknown lost", err)
	}
	a, b := failure.result.Cause().Details(), original.Cause().Details()
	if a.Kind != f.RecoveryCause || a.Kind != b.Kind || a.Owner != b.Owner || a.RecoveryRunID != b.RecoveryRunID || a.CheckpointRef != b.CheckpointRef {
		t.Fatal("original technical cause changed")
	}
}
func waitCall(t *testing.T, ch <-chan struct{}, why string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal(why)
	}
}

func TestWorkProjectCallOriginalReturnAndSelection(t *testing.T) {
	v := newCallOwnerFixture(t)
	mut, a, doneA := v.local(t, 1, v.p, projectCallMutation)
	read, _, doneRead := v.local(t, 2, v.p, projectCallRead)
	b, _, doneB := v.local(t, 3, v.b, projectCallMutation)
	t.Cleanup(doneA)
	t.Cleanup(doneRead)
	t.Cleanup(doneB)
	v.cause.Action = pc.Archive
	r, e := v.stop(context.Background(), v.p, false)
	requireCallStop(t, r, e, pc.StopPending)
	if mut.Err() != nil || read.Err() != nil || b.Err() != nil {
		t.Fatal("Inspect cancelled a caller")
	}
	r, e = v.stop(context.Background(), v.p, true)
	requireCallStop(t, r, e, pc.StopPending)
	if mut.Err() != context.Canceled || read.Err() != nil || b.Err() != nil {
		t.Fatal("Archive crossed read/Project boundary")
	}
	entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	joined := false
	t.Cleanup(func() {
		unblock()
		if !joined {
			waitCall(t, returned, "original checkpoint did not return")
		}
	})
	v.store.after = func(_ context.Context, cause f.TransactionCause, _ int, result f.CommitResult) f.CommitResult {
		if cause.Details().Owner == "work-call-return" && cause.Details().RecoveryRunID == a.record.ID.String() {
			close(entered)
			<-release
		}
		return result
	}
	go func() { doneA(); close(returned) }()
	waitCall(t, entered, "checkpoint not reached")
	r, e = v.stop(context.Background(), v.p, false)
	requireCallStop(t, r, e, pc.StopPending)
	if a.returned || len(v.pair.state.tasks.state().calls) != 3 {
		t.Fatal("physical closed row substituted original done")
	}
	v.cause.Action = pc.Delete
	r, e = v.stop(context.Background(), v.p, true)
	requireCallStop(t, r, e, pc.StopPending)
	if read.Err() != context.Canceled || b.Err() != nil {
		t.Fatal("Delete did not isolate original A calls")
	}
	unblock()
	waitCall(t, returned, "checkpoint original return")
	joined = true
	v.store.after = nil
	doneRead()
	r, e = v.stop(context.Background(), v.p, false)
	requireCallStop(t, r, e, pc.Stopped)
	if b.Err() != nil {
		t.Fatal("A stop cancelled B")
	}
	doneB()
	v.pair.Stop()
	if !v.pair.Joined() {
		t.Fatal("returned controls failed to join")
	}
}

func TestWorkProjectCallRegistrationUnknownConfirmation(t *testing.T) {
	v := newCallOwnerFixture(t)
	ctx, entry, done, err := v.pair.TaskService().begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	entry.project, entry.class = v.p, projectCallMutation
	entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	joined := false
	t.Cleanup(func() {
		unblock()
		if !joined {
			select {
			case <-returned:
			case <-time.After(5 * time.Second):
				t.Error("registration did not return")
			}
		}
		done()
	})
	var original f.CommitResult
	v.store.registration = func(_ context.Context, cause f.TransactionCause, o *projectCallOwner) f.CommitResult {
		original = f.UnknownResult(pureID[f.TransactionAttempt](t, 91), cause)
		v.store.put(o.record)
		close(entered)
		<-release
		return original
	}
	go func() { _, e := v.pair.state.admit(ctx, entry, v.actor, projectCallTask); done(); returned <- e }()
	waitCall(t, entered, "registration not entered")
	r, e := v.stop(context.Background(), v.p, true)
	requireCallStop(t, r, e, pc.StopPending)
	if entry.owner.returned || entry.owner.unknown != nil {
		t.Fatal("unreturned raw Store result was consumed")
	}
	unblock()
	select {
	case e = <-returned:
		joined = true
	case <-time.After(5 * time.Second):
		t.Fatal("registration still held")
	}
	requireCallUnknown(t, e, original)
	before, _, writes := v.store.counts()
	r, e = v.stop(context.Background(), v.p, true)
	requireCallStop(t, r, e, pc.StopPending)
	after, _, nowWrites := v.store.counts()
	if after != before+1 || nowWrites != writes || entry.owner.unknown != nil || entry.owner.closed {
		t.Fatal("Unknown confirmation performed closure")
	}
	r, e = v.stop(context.Background(), v.p, true)
	requireCallStop(t, r, e, pc.StopPending)
	_, _, nowWrites = v.store.counts()
	if nowWrites != writes+1 {
		t.Fatal("next explicit request did not close original call")
	}
	r, e = v.stop(context.Background(), v.p, false)
	requireCallStop(t, r, e, pc.Stopped)
	v.pair.Stop()
	if !v.pair.Joined() {
		t.Fatal("confirmed original call remains retained")
	}
}

func TestWorkProjectCallCloseUnknownConfirmation(t *testing.T) {
	for _, applied := range []bool{false, true} {
		name := "not-applied"
		if applied {
			name = "applied"
		}
		t.Run(name, func(t *testing.T) {
			v := newCallOwnerFixture(t)
			_, o, done := v.local(t, 1, v.p, projectCallMutation)
			t.Cleanup(done)
			var original f.CommitResult
			v.store.after = func(_ context.Context, cause f.TransactionCause, _ int, result f.CommitResult) f.CommitResult {
				if result.State() != f.Committed {
					t.Fatal("controlled close did not reach callback")
				}
				original = f.UnknownResult(pureID[f.TransactionAttempt](t, 92), cause)
				if !applied {
					v.store.put(o.record)
				}
				return original
			}
			done()
			v.store.after = nil
			if o.unknown == nil || !o.returned || v.pair.state.laneJoined(projectCallTask) {
				t.Fatal("Unknown lost original owner")
			}
			v.pair.Stop()
			if v.pair.Joined() {
				t.Fatal("Unknown counted as join")
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, err := v.pair.state.settle(ctx, o, nil, false)
			requireCallUnknown(t, err, original)
			entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan error, 1)
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			joined := false
			t.Cleanup(func() {
				unblock()
				if !joined {
					select {
					case <-returned:
					case <-time.After(5 * time.Second):
						t.Error("confirmation did not return")
					}
				}
			})
			v.store.after = func(_ context.Context, _ f.TransactionCause, _ int, result f.CommitResult) f.CommitResult {
				close(entered)
				<-release
				return result
			}
			_, _, writes := v.store.counts()
			go func() { _, e := v.pair.state.settle(context.Background(), o, nil, false); returned <- e }()
			waitCall(t, entered, "confirmation not entered")
			if o.unknown == nil || v.pair.Joined() {
				t.Fatal("confirmation callback substituted actual Store return")
			}
			unblock()
			select {
			case err = <-returned:
				joined = true
			case <-time.After(5 * time.Second):
				t.Fatal("confirmation held")
			}
			if err != nil {
				t.Fatal(err)
			}
			v.store.after = nil
			_, _, after := v.store.counts()
			if after != writes {
				t.Fatal("confirmation wrote")
			}
			if !applied {
				if v.pair.Joined() {
					t.Fatal("running call counted joined")
				}
				if _, err = v.pair.state.settle(context.Background(), o, nil, false); err != nil {
					t.Fatal(err)
				}
			}
			if !v.pair.Joined() {
				t.Fatal("original closed call did not join")
			}
		})
	}
}

func TestWorkProjectCallInspectAfterConcurrentConfirmation(t *testing.T) {
	v := newCallOwnerFixture(t)
	_, o, done := v.local(t, 1, v.p, projectCallMutation)
	t.Cleanup(done)
	v.store.after = func(_ context.Context, cause f.TransactionCause, _ int, _ f.CommitResult) f.CommitResult {
		v.store.put(o.record)
		return f.UnknownResult(pureID[f.TransactionAttempt](t, 94), cause)
	}
	done()
	v.store.after = nil
	if o.unknown == nil || !o.returned {
		t.Fatal("original close Unknown not retained")
	}
	if _, err := v.pair.state.settle(context.Background(), o, nil, false); err != nil {
		t.Fatal(err)
	}
	// Represent the exact post-confirmation interleaving: an earlier Inspect
	// observed Unknown, then another caller confirmed it, leaving running.
	if o.unknown != nil || o.closed {
		t.Fatal("confirmation did not leave original running owner")
	}
	before, _, writes := v.store.counts()
	r, err := v.stop(context.Background(), v.p, false)
	requireCallStop(t, r, err, pc.StopPending)
	afterInspect, _, inspectWrites := v.store.counts()
	if afterInspect != before || inspectWrites != writes || o.closed {
		t.Fatal("public Inspect closed confirmed-but-running original owner")
	}
	if _, err := v.pair.state.settle(context.Background(), o, nil, true); err != nil {
		t.Fatal(err)
	}
	after, _, nowWrites := v.store.counts()
	if after != before || nowWrites != writes || o.closed {
		t.Fatal("stale Inspect snapshot gained write permission")
	}
	if _, err := v.pair.state.settle(context.Background(), o, nil, false); err != nil {
		t.Fatal(err)
	}
	v.pair.Stop()
	if !v.pair.Joined() {
		t.Fatal("test original owner did not return")
	}
}

func TestWorkProjectCallForeignProofAndBatch(t *testing.T) {
	v := newCallOwnerFixture(t)
	foreign := pureID[oc.Process](t, 89)
	for n := 1; n <= 101; n++ {
		r := v.row(t, n, v.p)
		r.Process = foreign
		v.store.put(r)
	}
	entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	joined := false
	t.Cleanup(func() {
		unblock()
		if !joined {
			select {
			case <-returned:
			case <-time.After(5 * time.Second):
				t.Error("foreign original read did not return")
			}
		}
	})
	v.store.after = func(_ context.Context, _ f.TransactionCause, n int, result f.CommitResult) f.CommitResult {
		if n == 1 {
			close(entered)
			<-release
		}
		return result
	}
	go func() {
		r, e := v.stop(context.Background(), v.p, false)
		if e == nil && r.Details().State != pc.StopPending {
			e = errors.New("Inspect closed rows")
		}
		returned <- e
	}()
	waitCall(t, entered, "foreign discovery not reached")
	if v.proof.confirms != 0 {
		t.Fatal("foreign proof before original read Store return")
	}
	unblock()
	select {
	case err := <-returned:
		joined = true
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("foreign read held")
	}
	v.store.after = nil
	_, _, writes := v.store.counts()
	if writes != 0 || v.proof.confirms != 1 {
		t.Fatal("Inspect mutated or skipped original proof")
	}
	r, e := v.stop(context.Background(), v.p, true)
	requireCallStop(t, r, e, pc.StopPending)
	_, _, writes = v.store.counts()
	if writes != 100 {
		t.Fatal("first batch is not bounded at 100", writes)
	}
	r, e = v.stop(context.Background(), v.p, true)
	requireCallStop(t, r, e, pc.Stopped)
	_, _, writes = v.store.counts()
	if writes != 101 {
		t.Fatal("remaining original row not closed", writes)
	}
}

func TestWorkProjectCallForeignIdentityBoundaries(t *testing.T) {
	for _, name := range []string{"same-process-other-instance", "foreign-alive", "changed-after-proof"} {
		t.Run(name, func(t *testing.T) {
			v := newCallOwnerFixture(t)
			row := v.row(t, 1, v.p)
			row.Phase = "business_closed"
			switch name {
			case "same-process-other-instance":
				row.Instance = pureID[projectCallInstance](t, 88)
			case "foreign-alive":
				row.Process = pureID[oc.Process](t, 89)
				v.proof.confirm = func(context.Context, oc.ProcessID) error { return fault(f.ResourceBusy) }
			case "changed-after-proof":
				row.Process = pureID[oc.Process](t, 89)
				v.proof.confirm = func(context.Context, oc.ProcessID) error {
					changed := row
					changed.Instance = pureID[projectCallInstance](t, 88)
					v.store.put(changed)
					return nil
				}
			}
			v.store.put(row)
			r, err := v.stop(context.Background(), v.p, true)
			if name == "changed-after-proof" {
				pureCode(t, err, f.VersionConflict)
			} else {
				requireCallStop(t, r, err, pc.StopPending)
			}
			_, _, writes := v.store.counts()
			if writes != 0 {
				t.Fatal("uncertain identity was retired")
			}
		})
	}
}

func TestWorkProjectCallForeignUnknownKeepsOriginal(t *testing.T) {
	v := newCallOwnerFixture(t)
	row := v.row(t, 1, v.p)
	row.Process = pureID[oc.Process](t, 89)
	v.store.put(row)
	var original f.CommitResult
	v.store.after = func(_ context.Context, cause f.TransactionCause, n int, result f.CommitResult) f.CommitResult {
		if n == 2 {
			original = f.UnknownResult(pureID[f.TransactionAttempt](t, 93), cause)
			return original
		}
		return result
	}
	_, err := v.stop(context.Background(), v.p, true)
	requireCallUnknown(t, err, original)
	_, _, writes := v.store.counts()
	if writes != 1 {
		t.Fatal("controlled foreign close not reached")
	}
	failed := true
	v.store.after = func(_ context.Context, _ f.TransactionCause, _ int, result f.CommitResult) f.CommitResult {
		if failed {
			return f.NotCommittedResult(fault(f.Forbidden))
		}
		return result
	}
	_, err = v.stop(context.Background(), v.p, false)
	requireCallUnknown(t, err, original)
	if len(v.pair.state.stops) != 1 {
		t.Fatal("failed confirmation released stop owner")
	}
	failed = false
	r, err := v.stop(context.Background(), v.p, false)
	requireCallStop(t, r, err, pc.StopPending)
	_, _, after := v.store.counts()
	if after != writes || len(v.pair.state.stops) != 0 {
		t.Fatal("readonly original confirmation wrote or retained")
	}
	v.store.after = nil
	r, err = v.stop(context.Background(), v.p, false)
	requireCallStop(t, r, err, pc.Stopped)
}

// Route only technical Work rows to their controlled store; the business side
// is the existing public Task Update/receipt fixture, not a fabricated replay.
type callReplayStore struct {
	*titleUpdateStore
	technical *callOwnerStore
}

func (s *callReplayStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	if strings.HasPrefix(cause.Details().Owner, "work-call-") {
		return s.technical.WithinTx(ctx, cause, fn)
	}
	return s.titleUpdateStore.WithinTx(ctx, cause, fn)
}
func (s *callReplayStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.technical.InTx(tx); err == nil {
		return s, nil
	}
	if _, err := s.titleUpdateStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *callReplayStore) AcquireAll(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	if _, err := s.technical.InTx(tx); err == nil {
		return s.technical.AcquireAll(ctx, tx, locks)
	}
	return s.titleUpdateStore.AcquireAll(ctx, tx, locks)
}
func (s *callReplayStore) RequireHeldLocks(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	if _, err := s.technical.InTx(tx); err == nil {
		return s.technical.RequireHeldLocks(ctx, tx, locks)
	}
	return s.titleUpdateStore.RequireHeldLocks(ctx, tx, locks)
}
func (s *callReplayStore) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	if strings.Contains(query, "agenteam_work.project_calls") {
		return s.technical.QueryRow(ctx, query, args...)
	}
	return s.titleUpdateStore.QueryRow(ctx, query, args...)
}
func (s *callReplayStore) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(query, "agenteam_work.project_calls") {
		return s.technical.Exec(ctx, query, args...)
	}
	return s.titleUpdateStore.Exec(ctx, query, args...)
}

type callReplayProof struct{ store *callReplayStore }

func (p callReplayProof) RequireCurrentProcessInTx(ctx context.Context, tx f.Tx, _ oc.ProcessID) error {
	_, err := p.store.InTx(tx)
	if err != nil {
		return err
	}
	return ctx.Err()
}
func (callReplayProof) ConfirmStopped(context.Context, oc.ProcessID) error { return fault(f.Forbidden) }

type callReplayProject struct {
	pc.ProjectAuthority
	store   *callReplayStore
	ref     pc.ProjectRef
	actor   i.Actor
	intents []i.AccessIntent
}

func (p *callReplayProject) RequireOwnerInTx(ctx context.Context, tx f.Tx, actor i.Actor, id i.ProjectID, intent i.AccessIntent) (pc.ProjectAccess, error) {
	if err := p.store.RequireHeldLocks(ctx, tx, []f.LockRequest{userLock(actor.Details().UserID, f.Shared), projectLock(id, f.Shared)}); err != nil {
		return pc.ProjectAccess{}, err
	}
	p.intents = append(p.intents, intent)
	if !actor.Equal(p.actor) || id != p.ref.ID {
		return pc.ProjectAccess{}, fault(f.Forbidden)
	}
	if intent == i.Mutate && p.ref.Lifecycle != pc.Active {
		return pc.ProjectAccess{}, fault(f.ProjectNotActive)
	}
	return pc.NewProjectAccess(actor, p.ref, p.ref.UpdatedAt)
}
func callReplayPair(t *testing.T, store *callReplayStore, projects *callReplayProject) *ProjectCallOwnership {
	t.Helper()
	template, _ := newEntryOwnership(t)
	a, err := NewAuthority(store, projects)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := NewReader(store, a, pureKeys(t))
	if err != nil {
		t.Fatal(err)
	}
	deps := ProjectCallDependencies{Structure: template.state.structure.state().deps, Tasks: template.state.tasks.state().deps, Transitions: template.state.transitions.state().deps, Blockers: template.state.blockers.state().deps, Sprints: SprintLifecycleDependencies{Authority: a, Projects: template.state.sprints.projects, Events: template.state.sprints.base.state().deps.Events, SprintEvents: template.state.sprints.events, Activity: template.state.sprints.base.state().deps.Activity}}
	deps.Structure.Authority = a
	deps.Tasks.Authority = a
	deps.Tasks.Structure = reader
	deps.Transitions.Authority = a
	deps.Transitions.Structure = reader
	deps.Blockers.Authority = a
	deps.Blockers.Structure = reader
	pair, err := newProjectCallOwnership(store, deps, callReplayProof{store}, template.state.process, &entryStopAuthority{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pair.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := pair.Drain(ctx); err != nil {
			t.Error(err)
		}
	})
	return pair
}
func TestWorkProjectCallArchiveReplayDoesNotUpgrade(t *testing.T) {
	for _, name := range []string{"archiving-replay", "archived-replay", "archived-new-key", "restore-before-business"} {
		t.Run(name, func(t *testing.T) {
			legacy, business, oldProjects, _, actor, meta := titleUpdateFixture(t)
			title := "Original completed title"
			request := c.TaskFieldsUpdate{Title: &title}
			p, id := business.task.ProjectID, business.task.ID
			original, err := legacy.UpdateTask(context.Background(), actor, meta, p, id, request)
			if err != nil {
				t.Fatal(err)
			}
			writes, appends := business.writes, business.appends
			tech := &callOwnerStore{live: map[f.Tx][]f.LockRequest{}, rows: map[string]projectCallRecord{}}
			store := &callReplayStore{titleUpdateStore: business, technical: tech}
			projects := &callReplayProject{store: store, ref: oldProjects.ref, actor: actor}
			projects.ref.Lifecycle = pc.Archived
			projects.ref.ArchivedAt = &projects.ref.UpdatedAt
			if name == "archiving-replay" {
				projects.ref.Lifecycle = pc.Archiving
				projects.ref.ArchivedAt = nil
			}
			pair := callReplayPair(t, store, projects)
			if name == "archived-new-key" {
				meta.IdempotencyKey = "new-archived-key"
			}
			var classified *projectCallOwner
			tech.after = func(ctx context.Context, cause f.TransactionCause, _ int, result f.CommitResult) f.CommitResult {
				if cause.Details().Owner == "work-call-register" {
					w, ok := ctx.Value(projectCallContextKey{}).(projectCallWitness)
					if !ok {
						t.Fatal("registration lost original owner")
					}
					classified = w.owner
					if !classified.classified || classified.record.Class != projectCallRead || classified.replayActor == nil {
						t.Fatal("archived Execute not classified as read replay")
					}
					pair.state.cancelProjectCalls(p, false)
					if ctx.Err() != nil {
						t.Fatal("Archive cancelled permitted replay")
					}
					if name == "restore-before-business" {
						projects.ref.Lifecycle = pc.Active
						projects.ref.ArchivedAt = nil
					}
				}
				return result
			}
			got, err := pair.TaskService().UpdateTask(context.Background(), actor, meta, p, id, request)
			switch name {
			case "archived-new-key":
				pureCode(t, err, f.ProjectNotActive)
			case "restore-before-business":
				pureCode(t, err, f.InvalidState)
			default:
				if err != nil || !sameValue(got, original) {
					t.Fatal("completed Execute replay changed", err)
				}
			}
			if classified == nil || !classified.returned || business.writes != writes || business.appends != appends {
				t.Fatal("replay gained business mutation or skipped original return")
			}
			if len(projects.intents) == 0 || projects.intents[0] != i.Read {
				t.Fatal("registration bypassed original Read-first gate")
			}
			if name != "archived-new-key" && slices.Contains(projects.intents, i.Mutate) {
				t.Fatal("receipt or restored replay reached new-write gate")
			}
			if name == "archived-new-key" && !slices.Contains(projects.intents, i.Mutate) {
				t.Fatal("new key bypassed ordinary mutation check")
			}
		})
	}
}
