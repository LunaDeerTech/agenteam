package usage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Only technical rows, method returns and explicit port decisions are controlled.
// This fixture executes the original Usage entry points and live-Tx checks; it
// does not issue real Project permission, model Invoke, or process-death proof.
type usageOwnerStore struct {
	mu                   sync.Mutex
	live                 map[f.Tx][]f.LockRequest
	rows                 map[string]projectCallRecord
	calls, reads, writes int
	readErr              error
	before               func(context.Context, f.TransactionCause, int) *f.CommitResult
	after                func(context.Context, f.TransactionCause, int, f.CommitResult) f.CommitResult
	extraRow             func(string, ...any) postgres.Row
	extraExec            func(string, ...any) (pgconn.CommandTag, error, bool)
}

func usageID[T any](t *testing.T, n int) f.ID[T] {
	t.Helper()
	id, err := f.ParseID[T](fmt.Sprintf("01900000-0000-7000-8000-%012x", n))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

type usageOwnerRow func(...any) error

func (r usageOwnerRow) Scan(out ...any) error { return r(out...) }
func usageJSONRow(v any) postgres.Row {
	return usageOwnerRow(func(out ...any) error {
		if len(out) != 1 {
			return errors.New("unexpected JSON projection")
		}
		p, ok := out[0].(*[]byte)
		if !ok {
			return errors.New("unexpected JSON destination")
		}
		raw, err := json.Marshal(v)
		*p = raw
		return err
	})
}
func usageErrorRow(err error) postgres.Row { return usageOwnerRow(func(...any) error { return err }) }
func (s *usageOwnerStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.mu.Lock()
	s.calls++
	n := s.calls
	s.mu.Unlock()
	if s.before != nil {
		if r := s.before(ctx, cause, n); r != nil {
			return *r
		}
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
		result = f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted).WithCause(err))
	}
	if s.after != nil {
		return s.after(ctx, cause, n, result)
	}
	return result
}
func (s *usageOwnerStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.live[tx]; !ok {
		return nil, fault(f.Forbidden)
	}
	return s, nil
}
func (s *usageOwnerStore) AcquireAll(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.live[tx]; !ok {
		return fault(f.Forbidden)
	}
	s.live[tx] = slices.Clone(locks)
	return ctx.Err()
}
func (s *usageOwnerStore) RequireHeldLocks(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	held, ok := s.live[tx]
	if !ok {
		return fault(f.Forbidden)
	}
	for _, want := range locks {
		if !slices.ContainsFunc(held, func(have f.LockRequest) bool {
			return have.Key.Canonical() == want.Key.Canonical() && (have.Mode == f.Exclusive || have.Mode == want.Mode)
		}) {
			return fault(f.Forbidden)
		}
	}
	return ctx.Err()
}
func (s *usageOwnerStore) Query(context.Context, string, ...any) (*postgres.Rows, error) {
	return nil, s.readErr
}
func (s *usageOwnerStore) QueryRow(_ context.Context, query string, args ...any) postgres.Row {
	if s.extraRow != nil {
		if row := s.extraRow(query, args...); row != nil {
			return row
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads++
	switch {
	case strings.HasPrefix(query, "INSERT INTO agenteam_model.usage_project_calls("):
		if len(args) != 6 || !strings.Contains(query, "'running',clock_timestamp()) RETURNING ") {
			return usageErrorRow(errors.New("changed registration protocol"))
		}
		id, e1 := f.ParseID[projectCallID](args[0].(string))
		p, e2 := f.ParseID[i.Project](args[1].(string))
		process, e3 := f.ParseID[oc.Process](args[2].(string))
		instance, e4 := f.ParseID[projectCallInstance](args[3].(string))
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
			return usageErrorRow(errors.New("invalid registration identity"))
		}
		r := projectCallRecord{ID: id, Project: p, Process: process, Instance: instance, Lane: projectCallLane(args[4].(string)), Phase: "running", Registered: "2026-10-13T00:00:00Z"}
		if args[5] != nil {
			r.CallID = args[5].(string)
		}
		if _, found := s.rows[id.String()]; found {
			return usageErrorRow(errors.New("duplicate registration"))
		}
		s.rows[id.String()] = r
		s.writes++
		return usageJSONRow(r)
	case strings.Contains(query, "FROM agenteam_model.usage_project_calls WHERE id=$1"):
		if len(args) != 1 {
			return usageErrorRow(errors.New("changed ledger read"))
		}
		r, ok := s.rows[args[0].(string)]
		if !ok {
			return usageErrorRow(pgx.ErrNoRows)
		}
		return usageJSONRow(r)
	case strings.Contains(query, "jsonb_agg") && strings.Contains(query, "FROM agenteam_model.usage_project_calls WHERE project_id=$1"):
		if len(args) != 1 {
			return usageErrorRow(errors.New("changed ledger set read"))
		}
		rows := []projectCallRecord{}
		for _, r := range s.rows {
			if r.Project.String() == args[0] {
				rows = append(rows, r)
			}
		}
		slices.SortFunc(rows, func(a, b projectCallRecord) int { return strings.Compare(a.ID.String(), b.ID.String()) })
		return usageJSONRow(rows)
	case query == "SELECT clock_timestamp()":
		return usageOwnerRow(func(out ...any) error {
			if len(out) != 1 {
				return errors.New("clock projection")
			}
			*out[0].(*time.Time) = time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
			return nil
		})
	default:
		return usageErrorRow(s.readErr)
	}
}
func (s *usageOwnerStore) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if s.extraExec != nil {
		if tag, err, handled := s.extraExec(query, args...); handled {
			return tag, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !strings.HasPrefix(query, "UPDATE agenteam_model.usage_project_calls SET phase='business_closed'") || len(args) != 7 || !strings.Contains(query, "registered_at=$7::timestamptz AND phase='running'") {
		return pgconn.CommandTag{}, errors.New("unexpected technical write")
	}
	r, ok := s.rows[args[0].(string)]
	if !ok || r.Phase != "running" || args[1] != r.Project.String() || args[2] != r.Process.String() || args[3] != r.Instance.String() || args[4] != string(r.Lane) || args[5] != nullString(r.CallID) || args[6] != r.Registered {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	r.Phase, r.Closed = "business_closed", "2026-10-13T00:00:01Z"
	s.rows[r.ID.String()] = r
	s.writes++
	return pgconn.NewCommandTag("UPDATE 1"), nil
}
func (s *usageOwnerStore) counts() (int, int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls, s.reads, s.writes
}

type usageOwnerProof struct {
	store   *usageOwnerStore
	process oc.ProcessID
	confirm func(context.Context, oc.ProcessID) error
}

func (p *usageOwnerProof) RequireCurrentProcessInTx(ctx context.Context, tx f.Tx, process oc.ProcessID) error {
	if process != p.process {
		return fault(f.Forbidden)
	}
	_, err := p.store.InTx(tx)
	return err
}
func (p *usageOwnerProof) ConfirmStopped(ctx context.Context, process oc.ProcessID) error {
	p.store.mu.Lock()
	live := len(p.store.live)
	p.store.mu.Unlock()
	if live != 0 || p.confirm == nil {
		return fault(f.ResourceBusy)
	}
	return p.confirm(ctx, process)
}

type usageOwnerGate struct{ store *usageOwnerStore }
type usageOwnerLoanKey struct{}

func (g *usageOwnerGate) WithProjectStop(ctx context.Context, _ i.Actor, _ pc.LifecycleCause, _ pc.ScopeRef, fn func(context.Context) error) error {
	return fn(context.WithValue(ctx, usageOwnerLoanKey{}, g))
}
func (g *usageOwnerGate) RequireProjectStopInTx(ctx context.Context, tx f.Tx, _ i.Actor, _ pc.LifecycleCause, scope pc.ScopeRef) error {
	if ctx.Value(usageOwnerLoanKey{}) != g {
		return fault(f.Forbidden)
	}
	return g.store.RequireHeldLocks(ctx, tx, []f.LockRequest{usageProjectLock(scope.ProjectID, f.Exclusive)})
}
func (g *usageOwnerGate) RequireProjectInvocationAdmissionInTx(ctx context.Context, tx f.Tx, p i.ProjectID) error {
	return g.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(p)})
}
func (g *usageOwnerGate) RequireProjectCleanupInTx(ctx context.Context, tx f.Tx, _ i.Actor, _ pc.LifecycleCause, scope pc.ScopeRef) error {
	if ctx.Value(usageOwnerLoanKey{}) != g {
		return fault(f.Forbidden)
	}
	return g.store.RequireHeldLocks(ctx, tx, []f.LockRequest{usageProjectLock(scope.ProjectID, f.Exclusive)})
}

type usageOwnerSessions struct {
	i.SessionAuthority
	store *usageOwnerStore
}

func (s usageOwnerSessions) RequireCurrentSession(ctx context.Context, tx f.Tx, actor i.Actor) error {
	_, err := s.store.InTx(tx)
	return err
}

type usageOwnerProjects struct{ store *usageOwnerStore }

func (p usageOwnerProjects) AuthorizeProject(ctx context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, intent i.AccessIntent) (i.AccessGrant, error) {
	if err := p.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(project)}); err != nil {
		return i.AccessGrant{}, err
	}
	scope, _ := i.InProject(project)
	now, _ := f.NewInstant(time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC))
	return i.NewAccessGrant(actor, scope, intent, now, 1)
}

type usageOwnerFixture struct {
	pair      *ProjectCallOwnership
	store     *usageOwnerStore
	proof     *usageOwnerProof
	gate      *usageOwnerGate
	actor     i.Actor
	p, b      i.ProjectID
	execution i.ExecutionID
	cause     pc.LifecycleCause
}

func newUsageOwner(t *testing.T, facts uc.InvocationFacts) *usageOwnerFixture {
	t.Helper()
	s := &usageOwnerStore{live: map[f.Tx][]f.LockRequest{}, rows: map[string]projectCallRecord{}, readErr: errors.New("controlled original Usage business read")}
	proof := &usageOwnerProof{store: s, process: usageID[oc.Process](t, 1)}
	gate := &usageOwnerGate{store: s}
	a, err := NewAuthority(s, Authorizations{Sessions: usageOwnerSessions{store: s}, Projects: usageOwnerProjects{s}, Invocations: facts})
	if err != nil {
		t.Fatal(err)
	}
	pair, err := newProjectCallOwnership(s, a, Dependencies{testCursor(t)}, proof, proof.process, gate)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := i.NewHuman(usageID[i.User](t, 2), usageID[i.Session](t, 3))
	if err != nil {
		t.Fatal(err)
	}
	v := &usageOwnerFixture{pair: pair, store: s, proof: proof, gate: gate, actor: actor, p: usageID[i.Project](t, 4), b: usageID[i.Project](t, 5), execution: usageID[i.Execution](t, 6), cause: pc.LifecycleCause{OperationID: usageID[pc.Operation](t, 7), Action: pc.Delete, ProjectVersion: 1}}
	if calls, _, _ := s.counts(); calls != 0 {
		t.Fatal("constructor performed I/O")
	}
	return v
}
func (v *usageOwnerFixture) read(ctx context.Context, lane projectCallLane, p i.ProjectID) error {
	s := v.pair.Service()
	switch lane {
	case projectCallList:
		_, err := s.List(ctx, v.actor, uc.Query{Filter: uc.Filter{ProjectID: p}, Limit: 1})
		return err
	case projectCallAggregate:
		_, err := s.Aggregate(ctx, v.actor, uc.AggregateQuery{Filter: uc.Filter{ProjectID: p}, GroupBy: uc.ByConsumer, Limit: 1})
		return err
	case projectCallSummary:
		_, err := s.GetExecutionSummary(ctx, v.actor, p, v.execution)
		return err
	default:
		_, err := s.RebuildExecutionSummary(ctx, v.actor, p, v.execution)
		return err
	}
}
func (v *usageOwnerFixture) stop(ctx context.Context, request bool) (pc.StopReport, error) {
	scope := pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: v.p}
	if request {
		return v.pair.RequestProjectStop(ctx, v.actor, v.cause, scope)
	}
	return v.pair.InspectProjectStop(ctx, v.actor, v.cause, scope)
}
func usageAwait(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("original call barrier not reached")
	}
}
func usageUnknown(t *testing.T, err error, original f.CommitResult) {
	t.Helper()
	var unknown *UnknownError
	if !errors.As(err, &unknown) || unknown.AttemptID() != original.AttemptID() || !reflect.DeepEqual(unknown.Cause().Details(), original.Cause().Details()) {
		t.Fatal("first raw Unknown provenance changed", err)
	}
}

func TestUsageProjectCallPublicReadersActualReturn(t *testing.T) {
	for _, lane := range []projectCallLane{projectCallList, projectCallAggregate, projectCallSummary, projectCallRebuild} {
		t.Run(string(lane), func(t *testing.T) {
			v := newUsageOwner(t, nil)
			entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var once sync.Once
			free := func() { once.Do(func() { close(release) }) }
			v.store.after = func(ctx context.Context, cause f.TransactionCause, _ int, result f.CommitResult) f.CommitResult {
				w, _ := ctx.Value(projectCallContextKey{}).(projectCallWitness)
				if strings.HasPrefix(cause.Details().Owner, "model.usage.") && w.owner != nil && w.owner.record.Project == v.p {
					close(entered)
					<-release
				}
				return result
			}
			var original error
			go func() { defer close(returned); original = v.read(context.Background(), lane, v.p) }()
			t.Cleanup(func() { free(); usageAwait(t, returned) })
			usageAwait(t, entered)
			v.pair.state.mu.Lock()
			var held *projectCallOwner
			for _, o := range v.pair.state.owners {
				held = o
			}
			valid := held != nil && held.record.Lane == lane && held.registrationKnown && !held.bodyReturned && !held.returned
			v.pair.state.mu.Unlock()
			if !valid {
				t.Fatal("public reader lost its original registration before Store return")
			}
			report, err := v.stop(context.Background(), false)
			if err != nil || report.Details().State != pc.StopPending {
				t.Fatal("Inspect invented reader return", err)
			}
			if err = v.read(context.Background(), lane, v.b); !errors.Is(err, v.store.readErr) {
				t.Fatal("B did not reach its own business read", err)
			}
			if err = v.read(context.Background(), lane, v.p); err == nil || errors.Is(err, v.store.readErr) {
				t.Fatal("same-P late admission reached business SQL", err)
			}
			select {
			case <-returned:
				t.Fatal("Store tail detached")
			default:
			}
			free()
			usageAwait(t, returned)
			if !errors.Is(original, v.store.readErr) {
				t.Fatal("original business error changed", original)
			}
			v.store.mu.Lock()
			closed := true
			for _, r := range v.store.rows {
				closed = closed && r.Phase == "business_closed"
			}
			v.store.mu.Unlock()
			v.pair.Stop()
			if !closed || !v.pair.Joined() {
				t.Fatal("actual reader return did not close technical ownership")
			}
		})
	}
}

func TestUsageProjectCallTechnicalUnknownConfirmation(t *testing.T) {
	for _, registration := range []bool{true, false} {
		t.Run(fmt.Sprint("registration=", registration), func(t *testing.T) {
			v := newUsageOwner(t, nil)
			var original f.CommitResult
			if registration {
				v.store.before = func(_ context.Context, cause f.TransactionCause, n int) *f.CommitResult {
					if n != 1 {
						return nil
					}
					original = f.UnknownResult(usageID[f.TransactionAttempt](t, 30), cause)
					return &original
				}
			} else {
				v.store.after = func(_ context.Context, cause f.TransactionCause, _ int, result f.CommitResult) f.CommitResult {
					if cause.Details().Owner != "usage-call-return" {
						return result
					}
					// Explicitly model a rolled-back close with a lost rollback ACK.
					v.store.mu.Lock()
					for key, row := range v.store.rows {
						row.Phase, row.Closed = "running", ""
						v.store.rows[key] = row
					}
					v.store.mu.Unlock()
					original = f.UnknownResult(usageID[f.TransactionAttempt](t, 31), cause)
					return original
				}
			}
			err := v.read(context.Background(), projectCallList, v.p)
			if registration {
				usageUnknown(t, err, original)
			} else if !errors.Is(err, v.store.readErr) {
				t.Fatal("technical error replaced original business error", err)
			}
			v.store.before, v.store.after = nil, nil
			v.pair.state.mu.Lock()
			var owner *projectCallOwner
			for _, o := range v.pair.state.owners {
				owner = o
			}
			v.pair.state.mu.Unlock()
			if owner == nil || owner.unknown == nil || !owner.returned || owner.unknown.AttemptID() != original.AttemptID() {
				t.Fatal("first raw technical Unknown not retained")
			}
			_, _, before := v.store.counts()
			entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var once sync.Once
			free := func() { once.Do(func() { close(release) }) }
			v.store.after = func(_ context.Context, cause f.TransactionCause, _ int, result f.CommitResult) f.CommitResult {
				if !reflect.DeepEqual(cause.Details(), original.Cause().Details()) {
					t.Error("confirmation changed original cause")
				}
				close(entered)
				<-release
				return result
			}
			var report pc.StopReport
			go func() { defer close(returned); report, err = v.stop(context.Background(), true) }()
			t.Cleanup(func() { free(); usageAwait(t, returned) })
			usageAwait(t, entered)
			if owner.unknown == nil {
				t.Fatal("observation cleared Unknown before original Store returned")
			}
			select {
			case <-returned:
				t.Fatal("confirmation detached Store")
			default:
			}
			free()
			usageAwait(t, returned)
			if err != nil || report.Details().State != pc.StopPending || owner.unknown != nil {
				t.Fatal("confirmed technical outcome was not conservative Pending", err)
			}
			_, _, after := v.store.counts()
			if after != before {
				t.Fatal("confirmation performed a write")
			}
			v.store.after = nil
			if !registration {
				if _, err = v.stop(context.Background(), true); err != nil {
					t.Fatal(err)
				}
				_, _, after = v.store.counts()
				if after != before+1 {
					t.Fatal("next explicit request failed to close original technical row")
				}
			}
		})
	}
}

func TestUsageProjectCallRebuildUnknownUsesOriginalBeforeImage(t *testing.T) {
	v := newUsageOwner(t, nil)
	before := []byte(`{"project_id":"` + v.p.String() + `","execution_id":"` + v.execution.String() + `","version":2,"total_sum":77}`)
	current := slices.Clone(before)
	v.store.extraRow = func(query string, args ...any) postgres.Row {
		switch {
		case strings.HasPrefix(query, "SELECT EXISTS(SELECT 1 FROM agenteam_model.invocations"):
			return usageOwnerRow(func(out ...any) error { *out[0].(*bool) = true; return nil })
		case strings.HasPrefix(query, "SELECT version,updated_at,"):
			return usageErrorRow(pgx.ErrNoRows)
		case strings.HasPrefix(query, "SELECT to_jsonb(t) FROM agenteam_model.execution_usage_summaries"):
			return usageOwnerRow(func(out ...any) error { *out[0].(*[]byte) = slices.Clone(current); return nil })
		}
		return nil
	}
	var original f.CommitResult
	v.store.after = func(_ context.Context, cause f.TransactionCause, _ int, result f.CommitResult) f.CommitResult {
		if cause.Details().Owner != "model.usage.summary" {
			return result
		}
		if result.State() != f.NotCommitted || !errors.Is(result.Fault(), v.store.readErr) {
			t.Error("Rebuild did not reach original Query sentinel")
		}
		original = f.UnknownResult(usageID[f.TransactionAttempt](t, 32), cause)
		return original
	}
	usageUnknown(t, v.read(context.Background(), projectCallRebuild, v.p), original)
	v.store.after = nil
	var owner *projectCallOwner
	for _, o := range v.pair.state.owners {
		owner = o
	}
	if owner == nil || owner.rebuild == nil || !owner.rebuild.beforeKnown || owner.rebuild.afterKnown || !reflect.DeepEqual(owner.rebuild.before, before) || owner.businessUnknown == nil {
		t.Fatal("real Rebuild before-image was not captured")
	}
	_, _, writes := v.store.counts()
	current = []byte(`{"version":3,"total_sum":77}`)
	_, err := v.stop(context.Background(), true)
	usageUnknown(t, err, original)
	current = slices.Clone(before)
	report, err := v.stop(context.Background(), true)
	if err != nil || report.Details().State != pc.StopPending || owner.businessUnknown != nil {
		t.Fatal("before-image observation did not settle original business Unknown", err)
	}
	_, _, after := v.store.counts()
	if after != writes {
		t.Fatal("Rebuild confirmation rewrote the summary or technical row")
	}
	if _, err = v.stop(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	_, _, after = v.store.counts()
	if after != writes+1 {
		t.Fatal("technical close did not wait for the next explicit request")
	}
}

func TestUsageProjectCallForeignProofRechecksFullTuple(t *testing.T) {
	for _, mode := range []string{"proved", "same-process-other-instance", "changed-registered"} {
		t.Run(mode, func(t *testing.T) {
			v := newUsageOwner(t, nil)
			r := projectCallRecord{ID: usageID[projectCallID](t, 40), Project: v.p, Process: usageID[oc.Process](t, 41), Instance: usageID[projectCallInstance](t, 42), Lane: projectCallList, Phase: "running", Registered: "2026-10-13T00:00:00Z"}
			if mode == "same-process-other-instance" {
				r.Process = v.proof.process
				r.Phase, r.Closed = "business_closed", "2026-10-13T00:00:01Z"
			}
			v.store.rows[r.ID.String()] = r
			proofs := 0
			v.proof.confirm = func(_ context.Context, process oc.ProcessID) error {
				proofs++
				if process != r.Process {
					t.Error("foreign proof identity changed")
				}
				if mode == "changed-registered" {
					v.store.mu.Lock()
					changed := v.store.rows[r.ID.String()]
					changed.Registered = "2026-10-13T00:00:00.1Z"
					v.store.rows[r.ID.String()] = changed
					v.store.mu.Unlock()
				}
				return nil
			}
			report, err := v.stop(context.Background(), true)
			_, _, writes := v.store.counts()
			switch mode {
			case "proved":
				if err != nil || report.Details().State != pc.Stopped || proofs != 1 || writes != 1 {
					t.Fatal("proved foreign technical close failed", err)
				}
			case "same-process-other-instance":
				if err != nil || report.Details().State != pc.StopPending || proofs != 0 || writes != 0 {
					t.Fatal("another instance borrowed local return", err)
				}
			default:
				if err == nil || writes != 0 {
					t.Fatal("foreign tuple drift accepted", err)
				}
			}
		})
	}
}

// Lifetime facts are explicitly controlled and cannot grant InvocationFacts.
// Discover/Validate remain a denial, so no test claims a model Invoke success.
type usageInvocationFacts struct {
	store    *usageOwnerStore
	request  mc.ModelRequest
	token    uc.ProjectInvocationCall
	returned bool
	cancels  int
}
type usageInvocationKey struct{}

func (p *usageInvocationFacts) Discover(context.Context, uc.InvocationRequest) (uc.InvocationDependencies, error) {
	return uc.InvocationDependencies{}, fault(f.Forbidden)
}
func (p *usageInvocationFacts) ValidateInTx(context.Context, f.Tx, uc.InvocationRequest, uc.InvocationDependencies) (uc.InvocationFact, error) {
	return uc.InvocationFact{}, fault(f.Forbidden)
}
func (p *usageInvocationFacts) RequireOriginalInvocationCall(ctx context.Context, store any, r mc.ModelRequest, token uc.ProjectInvocationCall) error {
	a, _ := json.Marshal(r)
	b, _ := json.Marshal(p.request)
	if ctx.Value(usageInvocationKey{}) != p || store != p.store || token != p.token || !r.Actor.Equal(p.request.Actor) || !bytes.Equal(a, b) {
		return fault(f.Forbidden)
	}
	return nil
}
func (p *usageInvocationFacts) RequireOriginalInvocationReturned(ctx context.Context, store any, r mc.ModelRequest, token uc.ProjectInvocationCall) error {
	if err := p.RequireOriginalInvocationCall(ctx, store, r, token); err != nil {
		return err
	}
	if !p.returned {
		return fault(f.ResourceBusy)
	}
	return nil
}
func (p *usageInvocationFacts) CancelOriginalInvocation(ctx context.Context, store any, r mc.ModelRequest, token uc.ProjectInvocationCall) error {
	if err := p.RequireOriginalInvocationCall(ctx, store, r, token); err != nil {
		return err
	}
	p.cancels++
	return nil
}
func usageModelRequest(t *testing.T, p i.ProjectID) mc.ModelRequest {
	t.Helper()
	agent, execution := usageID[i.Agent](t, 50), usageID[i.Execution](t, 51)
	actor, err := i.NewAgentRun(p, agent, execution)
	if err != nil {
		t.Fatal(err)
	}
	consumer := mc.Consumer{Kind: mc.AgentConsumer, ProjectID: p, Purpose: mc.AgentGeneration, AgentID: &agent, ExecutionID: &execution}
	owner, err := sc.NewCredentialLeaseOwner(sc.ExecutionOwner, execution.String())
	if err != nil {
		t.Fatal(err)
	}
	snapshot := mc.ConfigSnapshot{ID: usageID[mc.Snapshot](t, 52), Identity: mc.ModelIdentity{ProviderID: usageID[mc.Provider](t, 53), ModelID: usageID[mc.Model](t, 54), ProviderName: "provider", ModelName: "chat", ProviderModelID: "upstream", Protocol: mc.OpenAIChat, Profile: mc.OpenAIChatV1, ModelType: mc.ChatModel, AdapterRevision: "native-v1"}, Endpoint: "https://provider.example/v1", Parameters: json.RawMessage(`{}`), RequestOverwrite: json.RawMessage(`{}`), Capabilities: mc.Capabilities{InputModalities: []string{"text"}, OutputModalities: []string{"text"}}}
	r := mc.ModelRequest{Actor: actor, CallID: usageID[mc.Call](t, 55), Consumer: consumer, Model: mc.ResolvedModel{Snapshot: snapshot, Consumer: consumer, LeaseOwner: owner}, Input: mc.InputIdentity{ExecutionID: &execution, RoundID: usageID[struct{}](t, 56).String(), Digest: hash([]byte("controlled-input")), SchemaVersion: 1}, Messages: []mc.Message{{Role: "user", Parts: []mc.MessagePart{{Text: &mc.TextPart{Text: "controlled input"}}}}}, ToolChoice: mc.ToolChoice{Kind: "none"}, ResponseFormat: mc.ResponseFormat{Kind: "text"}, RetryClass: mc.AgentRetry}
	if err = r.Validate(); err != nil {
		t.Fatal(err)
	}
	return r
}
func TestUsageProjectInvocationFixedFactsAndActualTokenReturn(t *testing.T) {
	facts := &usageInvocationFacts{}
	v := newUsageOwner(t, facts)
	facts.store = v.store
	facts.request = usageModelRequest(t, v.p)
	legacy, err := New(v.store, v.pair.Service().state().authority, Dependencies{testCursor(t)})
	if err != nil {
		t.Fatal(err)
	}
	if legacy.ProjectInvocationBorrower() != nil || v.pair.Service().ProjectInvocationBorrower() != v.pair.Service() {
		t.Fatal("legacy/owned borrower selection changed")
	}
	if token, err := v.pair.Service().AcquireProjectInvocation(context.Background(), v.store, facts.request); err == nil || token != nil {
		t.Fatal("public request minted an owned invocation")
	}
	if calls, _, _ := v.store.counts(); calls != 0 {
		t.Fatal("unissued invocation reached registration")
	}
	ctx := context.WithValue(context.Background(), usageInvocationKey{}, facts)
	requestCopy := facts.request.Clone()
	token, err := v.pair.Service().AcquireProjectInvocation(ctx, v.store, requestCopy)
	if err != nil || token == nil {
		t.Fatal(err)
	}
	facts.token = token
	requestCopy.Messages[0].Parts[0].Text.Text = "changed caller input"
	if err = token.Returned(ctx); err == nil {
		t.Fatal("caller manufactured original Runtime return")
	}
	cause, _ := readCause("controlled-token")
	entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	original := f.UnknownResult(usageID[f.TransactionAttempt](t, 57), cause)
	v.store.after = func(_ context.Context, c f.TransactionCause, _ int, result f.CommitResult) f.CommitResult {
		if reflect.DeepEqual(c.Details(), cause.Details()) {
			if result.State() != f.Committed {
				t.Error("token lost original live Tx", result.Fault())
			}
			close(entered)
			<-release
			return original
		}
		return result
	}
	var result f.CommitResult
	go func() {
		defer close(returned)
		result = token.WithinTx(ctx, cause, func(run context.Context, tx f.Tx) error {
			return v.store.AcquireAll(run, tx, []f.LockRequest{projectLock(v.p)})
		})
	}()
	t.Cleanup(func() { free(); usageAwait(t, returned) })
	usageAwait(t, entered)
	facts.returned = true
	if err = token.Returned(ctx); err == nil {
		t.Fatal("Runtime return facts detached active token Store tail")
	}
	v.pair.Stop()
	if v.pair.Joined() || facts.cancels != 1 {
		t.Fatal("Stop replaced original invocation return")
	}
	free()
	usageAwait(t, returned)
	if result.AttemptID() != original.AttemptID() || !reflect.DeepEqual(result.Cause().Details(), original.Cause().Details()) {
		t.Fatal("token changed original Store raw result")
	}
	v.store.after = nil
	if err = token.Returned(ctx); err != nil || !v.pair.Joined() {
		t.Fatal("original facts failed to retire technical tail", err)
	}
	before, _, _ := v.store.counts()
	if r := token.WithinTx(ctx, cause, func(context.Context, f.Tx) error { return nil }); r.State() == f.Committed {
		t.Fatal("retired token minted a new transaction")
	}
	after, _, _ := v.store.counts()
	if after != before {
		t.Fatal("retired token reached Store")
	}
}

func TestUsageProjectInvocationFailedAdmissionKeepsOriginalToken(t *testing.T) {
	facts := &usageInvocationFacts{}
	v := newUsageOwner(t, facts)
	facts.store = v.store
	facts.request = usageModelRequest(t, v.p)
	ctx := context.WithValue(context.Background(), usageInvocationKey{}, facts)
	var original f.CommitResult
	v.store.before = func(_ context.Context, cause f.TransactionCause, n int) *f.CommitResult {
		if n != 1 {
			return nil
		}
		original = f.UnknownResult(usageID[f.TransactionAttempt](t, 58), cause)
		return &original
	}
	token, err := v.pair.Service().AcquireProjectInvocation(ctx, v.store, facts.request)
	usageUnknown(t, err, original)
	if token == nil {
		t.Fatal("failed registration lost the original Runtime tail handle")
	}
	facts.token, facts.returned = token, true
	usageUnknown(t, token.Returned(ctx), original)
	if calls, _, _ := v.store.counts(); calls != 1 {
		t.Fatal("first failed admission return confirmed its own registration")
	}
	v.store.before = nil
	if err = token.Returned(ctx); err != nil {
		t.Fatal("later absent confirmation failed", err)
	}
	if calls, _, writes := v.store.counts(); calls != 2 || writes != 0 {
		t.Fatal("absent registration confirmation wrote or retried admission")
	}
}
