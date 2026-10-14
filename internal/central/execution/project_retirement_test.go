package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

// The original four-owner factory and Service.Launch produce the canonical
// Execution and its returned ledger. Only the retirement SQL, Project grant
// and four reference stores are controlled here. This is not a PostgreSQL
// rollback, a real Project claim, or proof of foreign process death.
type retirementControlStore struct {
	*executionCallStore
	t           *testing.T
	mu          sync.Mutex
	txs         map[f.Tx]bool
	full        map[string][]byte
	origins     map[string][]byte
	history     map[string][]byte
	unsupported map[string]bool
	events      []string
	writes      int
	after       func(context.Context, f.TransactionCause, f.CommitResult) f.CommitResult
	rollback    bool
	exec        func(context.Context, string) error
	short       string
}

func (s *retirementControlStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	if !strings.HasPrefix(cause.Details().Owner, "execution-retirement-control") && cause.Details().Owner != "execution-project-retirement-discover" {
		return s.executionCallStore.WithinTx(ctx, cause, fn)
	}
	tx := f.NewTx()
	s.executionCallStore.mu.Lock()
	s.live[tx] = nil
	beforeCalls := maps.Clone(s.rows)
	s.executionCallStore.mu.Unlock()
	s.mu.Lock()
	s.txs[tx] = true
	beforeFull, beforeHistory, beforeOrigins := maps.Clone(s.full), maps.Clone(s.history), maps.Clone(s.origins)
	s.mu.Unlock()
	base := s.executionCallStore.Store.(*launchStore)
	beforeRows := maps.Clone(base.rows)
	err := fn(ctx, tx)
	s.mu.Lock()
	rollback, after := s.rollback, s.after
	if err != nil || rollback {
		s.full, s.history, s.origins = beforeFull, beforeHistory, beforeOrigins
		base.rows = beforeRows
	}
	delete(s.txs, tx)
	s.mu.Unlock()
	s.executionCallStore.mu.Lock()
	delete(s.live, tx)
	if err != nil || rollback {
		s.rows = beforeCalls
	}
	s.executionCallStore.mu.Unlock()
	result := f.CommittedResult()
	if err != nil {
		var known *f.Fault
		if !errors.As(err, &known) {
			known = f.NewFault(f.DependencyUnavailable, f.NotCommitted).WithCause(err)
		}
		result = f.NotCommittedResult(known)
	}
	if after != nil {
		return after(ctx, cause, result)
	}
	return result
}
func (s *retirementControlStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	x, err := s.executionCallStore.InTx(tx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	retirement := s.txs[tx]
	s.mu.Unlock()
	if retirement {
		return retirementControlSQL{s}, nil
	}
	return retirementBusinessSQL{s, x}, nil
}

// Only the original business INSERT can reach this new immutable-column
// write. Replay uses the old SELECT/receipt path and never calls this hook.
type retirementBusinessSQL struct {
	s        *retirementControlStore
	original postgres.SQLExecutor
}

func (x retirementBusinessSQL) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if !strings.HasPrefix(query, "UPDATE agenteam_execution.project_calls SET original_result=") {
		return x.original.Exec(ctx, query, args...)
	}
	if len(args) != 8 {
		return pgconn.CommandTag{}, errors.New("original-result tuple")
	}
	s := x.s
	s.executionCallStore.mu.Lock()
	defer s.executionCallStore.mu.Unlock()
	r, ok := s.rows[args[0].(string)]
	if !ok || r.Phase != "running" || args[1] != r.Project.String() || args[2] != r.Process.String() || args[3] != r.Instance.String() || args[4] != string(r.Lane) || args[5] != r.Identity || args[6] != r.Digest.String() {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.origins[r.ID.String()] != nil {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	var origin executionRetirementOrigin
	if err := json.Unmarshal(args[7].([]byte), &origin); err != nil || !origin.valid(r.Lane) {
		return pgconn.CommandTag{}, errors.New("invalid original result")
	}
	if s.executionCallStore.Store.(*launchStore).rows[origin.Execution] == nil {
		return pgconn.CommandTag{}, errors.New("original-result preceded original INSERT")
	}
	s.origins[r.ID.String()] = bytes.Clone(args[7].([]byte))
	return pgconn.NewCommandTag("UPDATE 1"), ctx.Err()
}
func (x retirementBusinessSQL) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	return x.original.QueryRow(ctx, query, args...)
}
func (x retirementBusinessSQL) Query(ctx context.Context, query string, args ...any) (*postgres.Rows, error) {
	return x.original.Query(ctx, query, args...)
}
func (s *retirementControlStore) event(value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, value)
}
func (s *retirementControlStore) snapshot() (int, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writes, slices.Clone(s.events)
}
func (s *retirementControlStore) setAfter(after func(context.Context, f.TransactionCause, f.CommitResult) f.CommitResult, rollback bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.after, s.rollback = after, rollback
}

type retirementControlSQL struct{ s *retirementControlStore }

func (x retirementControlSQL) Query(context.Context, string, ...any) (*postgres.Rows, error) {
	return nil, fault(f.Forbidden)
}
func (x retirementControlSQL) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	s := x.s
	if strings.Contains(query, "'original_result',original_result") {
		s.executionCallStore.mu.Lock()
		defer s.executionCallStore.mu.Unlock()
		s.mu.Lock()
		defer s.mu.Unlock()
		rows := []struct {
			ID     string          `json:"id"`
			Result json.RawMessage `json:"original_result"`
		}{}
		for id, row := range s.rows {
			if row.Project.String() == args[0] {
				result := s.origins[id]
				if result == nil {
					result = []byte("null")
				}
				rows = append(rows, struct {
					ID     string          `json:"id"`
					Result json.RawMessage `json:"original_result"`
				}{id, bytes.Clone(result)})
			}
		}
		slices.SortFunc(rows, func(a, b struct {
			ID     string          `json:"id"`
			Result json.RawMessage `json:"original_result"`
		}) int {
			return strings.Compare(a.ID, b.ID)
		})
		raw, err := json.Marshal(rows)
		return launchScan{values: []any{raw}, err: err}
	}
	if strings.Contains(query, "agenteam_execution.project_calls") {
		return s.executionCallStore.QueryRow(ctx, query, args...)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	base := s.executionCallStore.Store.(*launchStore)
	if strings.HasPrefix(query, "SELECT "+executionColumns) {
		return launchScan{values: base.rows[args[0].(string)]}
	}
	if strings.HasPrefix(query, "SELECT to_jsonb(e)") {
		return launchScan{values: []any{bytes.Clone(s.full[args[0].(string)])}}
	}
	if strings.HasPrefix(query, "SELECT id::text") {
		ids := []string{}
		for id, row := range base.rows {
			if row[1] == args[0] {
				ids = append(ids, id)
			}
		}
		slices.Sort(ids)
		if len(ids) == 0 {
			return launchScan{}
		}
		return launchScan{values: []any{ids[0]}}
	}
	if strings.HasPrefix(query, "SELECT agent_id::text") {
		row := base.rows[args[0].(string)]
		if row == nil || row[1] != args[1] {
			return launchScan{}
		}
		return launchScan{values: []any{row[2]}}
	}
	for _, table := range retirementHistoryTables {
		if !strings.Contains(query, "agenteam_execution."+table+" ") {
			continue
		}
		raw := s.history[table]
		if raw == nil {
			raw = []byte("[]")
		}
		if strings.Contains(query, "jsonb_agg") {
			return launchScan{values: []any{bytes.Clone(raw)}}
		}
		if strings.HasPrefix(query, "SELECT EXISTS") {
			return launchScan{values: []any{string(raw) != "[]"}}
		}
		return launchScan{err: fmt.Errorf("unexpected history projection: %s", table)}
	}
	for _, table := range retirementUnsupportedTables {
		if strings.Contains(query, "agenteam_execution."+table+" ") {
			return launchScan{values: []any{s.unsupported[table]}}
		}
	}
	if strings.HasPrefix(query, "SELECT EXISTS") && strings.Contains(query, "agenteam_execution.executions ") {
		found := false
		for id, row := range base.rows {
			if strings.Contains(query, "WHERE project_id") && row[1] == args[0] || strings.Contains(query, "WHERE id=") && id == args[0] {
				found = true
			}
		}
		return launchScan{values: []any{found}}
	}
	return launchScan{err: errors.New("unexpected retirement query: " + query)}
}
func (x retirementControlSQL) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	s := x.s
	table := ""
	for _, candidate := range append(slices.Clone(retirementHistoryTables[:]), "executions", "project_calls") {
		if strings.HasPrefix(query, "DELETE FROM agenteam_execution."+candidate+" ") {
			table = candidate
			break
		}
	}
	if table == "" {
		return pgconn.CommandTag{}, errors.New("unexpected retirement write")
	}
	s.mu.Lock()
	hook, short := s.exec, s.short
	s.mu.Unlock()
	if hook != nil {
		if err := hook(ctx, table); err != nil {
			return pgconn.CommandTag{}, err
		}
	}
	if short == table {
		return pgconn.NewCommandTag("DELETE 0"), nil
	}
	if table == "project_calls" {
		s.executionCallStore.mu.Lock()
		defer s.executionCallStore.mu.Unlock()
		r, ok := s.rows[args[0].(string)]
		if len(args) != 7 || !ok || r.Phase != "business_closed" || args[1] != r.Project.String() || args[2] != r.Process.String() || args[3] != r.Instance.String() || args[4] != string(r.Lane) || args[5] != r.Identity || args[6] != r.Digest.String() {
			return pgconn.NewCommandTag("DELETE 0"), nil
		}
		delete(s.rows, r.ID.String())
		s.mu.Lock()
		delete(s.origins, r.ID.String())
		s.writes++
		s.events = append(s.events, "delete:"+table)
		s.mu.Unlock()
		return pgconn.NewCommandTag("DELETE 1"), nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(args) != 2 {
		return pgconn.CommandTag{}, errors.New("retirement delete tuple")
	}
	var count int
	if table == "executions" {
		if !bytes.Equal(s.full[args[0].(string)], args[1].([]byte)) {
			return pgconn.NewCommandTag("DELETE 0"), nil
		}
		delete(s.full, args[0].(string))
		delete(s.executionCallStore.Store.(*launchStore).rows, args[0].(string))
		count = 1
	} else {
		if s.executionCallStore.Store.(*launchStore).rows[args[0].(string)] == nil {
			return pgconn.NewCommandTag("DELETE 0"), nil
		}
		if !bytes.Equal(s.history[table], args[1].([]byte)) {
			return pgconn.NewCommandTag("DELETE 0"), nil
		}
		var rows []json.RawMessage
		if err := json.Unmarshal(s.history[table], &rows); err != nil {
			return pgconn.CommandTag{}, err
		}
		count = len(rows)
		s.history[table] = []byte("[]")
	}
	s.writes++
	s.events = append(s.events, "delete:"+table)
	return pgconn.NewCommandTag(fmt.Sprintf("DELETE %d", count)), nil
}

type retirementControlGate struct {
	store  *retirementControlStore
	actor  i.Actor
	cause  pc.LifecycleCause
	scope  pc.ScopeRef
	commit ec.ProjectExecutionRetirementCommit
}

func (g *retirementControlGate) RequireProjectExecutionCleanupInTx(ctx context.Context, tx f.Tx, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) error {
	if !actor.Equal(g.actor) || cause != g.cause || !scope.Equal(g.scope) {
		return fault(f.Forbidden)
	}
	return g.store.RequireHeldLocks(ctx, tx, projectCallLocks(scope.ProjectID))
}
func (g *retirementControlGate) ProjectExecutionRetirementCommitInTx(ctx context.Context, tx f.Tx, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) (ec.ProjectExecutionRetirementCommit, error) {
	return g.commit, g.RequireProjectExecutionCleanupInTx(ctx, tx, actor, cause, scope)
}

type retirementControlRefPlan struct {
	owner  *retirementControlRef
	digest f.Digest
	locks  []f.LockRequest
}

func (p *retirementControlRefPlan) RequiredLocks() []f.LockRequest { return slices.Clone(p.locks) }
func (p *retirementControlRefPlan) ProjectExecutionRetirementReferenceDigest() f.Digest {
	return p.digest
}

type retirementControlRef struct {
	kind      ec.ProjectExecutionRetirementReferenceKind
	authority *Authority
	store     *retirementControlStore
	plan      *retirementControlRefPlan
	captured  context.Context
	tx        f.Tx
	request   ec.ProjectExecutionRetirementRequest
	err       error
	nilPlan   bool
	commit    ec.ProjectExecutionRetirementCommit
}

func (r *retirementControlRef) ProjectExecutionRetirementReferenceKind() ec.ProjectExecutionRetirementReferenceKind {
	return r.kind
}
func (r *retirementControlRef) DiscoverProjectExecutionReferencesInTx(ctx context.Context, tx f.Tx, req ec.ProjectExecutionRetirementRequest) (ec.ProjectExecutionRetirementReferencePlan, error) {
	if err := r.authority.RequireProjectExecutionRetirementPreparedInTx(ctx, tx, req); err != nil {
		return nil, err
	}
	if r.err != nil {
		return nil, r.err
	}
	if r.nilPlan {
		return nil, nil
	}
	return r.plan, nil
}
func (r *retirementControlRef) RetireProjectExecutionReferencesInTx(ctx context.Context, tx f.Tx, req ec.ProjectExecutionRetirementRequest, plan ec.ProjectExecutionRetirementReferencePlan) error {
	if plan != r.plan {
		return fault(f.Forbidden)
	}
	if err := r.authority.RequireProjectExecutionRetirementPreparedInTx(ctx, tx, req); err != nil {
		return err
	}
	if err := r.store.RequireHeldLocks(ctx, tx, plan.RequiredLocks()); err != nil {
		return err
	}
	if r.authority.RequireProjectExecutionRetirementAppliedInTx(ctx, tx, req) == nil {
		return errors.New("Prepared was Applied")
	}
	r.captured, r.tx, r.request = ctx, tx, req
	r.store.event("retire:" + string(r.kind))
	return r.err
}
func (r *retirementControlRef) FinalizeProjectExecutionRetirementInTx(ctx context.Context, tx f.Tx, req ec.ProjectExecutionRetirementRequest, plan ec.ProjectExecutionRetirementReferencePlan) error {
	if plan != r.plan || tx != r.tx {
		return fault(f.Forbidden)
	}
	commit, err := r.authority.ProjectExecutionRetirementProofInTx(ctx, tx, req)
	if err != nil {
		return err
	}
	if r.authority.RequireProjectExecutionRetirementPreparedInTx(ctx, tx, req) == nil {
		return errors.New("Applied was Prepared")
	}
	if _, ok := r.store.executionCallStore.Store.(*launchStore).rows[req.Binding.ExecutionID.String()]; ok {
		return errors.New("Finalize preceded canonical deletion")
	}
	r.commit = commit
	r.store.event("finalize:skill")
	return r.err
}
func (r *retirementControlRef) RequireProjectExecutionReferencesRetiredInTx(ctx context.Context, tx f.Tx, req ec.ProjectExecutionRetirementRequest, plan ec.ProjectExecutionRetirementReferencePlan) error {
	if plan != r.plan {
		return fault(f.Forbidden)
	}
	if err := r.authority.RequireProjectExecutionRetirementAppliedInTx(ctx, tx, req); err != nil {
		return err
	}
	r.store.event("require:" + string(r.kind))
	return r.err
}

type retirementFixture struct {
	base      *executionCallsFixture
	store     *retirementControlStore
	pair      *ProjectCallOwnership
	gate      *retirementControlGate
	consumer  *ProjectExecutionRetirement
	refs      [4]*retirementControlRef
	id        i.ExecutionID
	plan      ec.ProjectExecutionRetirementPlan
	confirmed bool
}

func newRetirementFixture(t *testing.T) *retirementFixture {
	return newRetirementFixtureWithLegacy(t, false)
}
func newRetirementFixtureWithLegacy(t *testing.T, legacy bool) *retirementFixture {
	t.Helper()
	b := newExecutionCalls(t)
	s := &retirementControlStore{executionCallStore: b.store, t: t, txs: map[f.Tx]bool{}, full: map[string][]byte{}, origins: map[string][]byte{}, history: map[string][]byte{}, unsupported: map[string]bool{}}
	projects := &executionCallProjects{store: b.store}
	a, err := NewAuthority(s, projects, nil)
	if err != nil {
		t.Fatal(err)
	}
	b.launch.authority = a
	direct := b.pair.Direct().state.deps
	direct.Projects = projects
	deps := ProjectCallDependencies{Service: Dependencies{Authority: a, Agents: b.launch, Task: b.launch}, Preparation: PreparationDependencies{Agents: b.launch, Projects: projects, Processes: direct.Processes}, Direct: direct, Associated: AssociatedExecutorOptions{MaxOwned: 4, RecoveryInterval: time.Hour}}
	if legacy {
		original, createErr := New(s, deps.Service)
		if createErr != nil {
			t.Fatal(createErr)
		}
		t.Cleanup(func() {
			original.Stop()
			if err := original.Drain(context.Background()); err != nil || !original.Joined() {
				t.Error("legacy original did not return", err)
			}
		})
		if _, createErr = original.Launch(context.Background(), b.launch.actor, b.launch.request); createErr != nil {
			t.Fatal("original legacy creation", createErr)
		}
	}
	pair, err := newProjectCallOwnership(s, deps, b.proof, b.pair.state.process, &executionCallAuthority{b.store})
	if err != nil {
		t.Fatal(err)
	}
	v := &retirementFixture{base: b, store: s, pair: pair}
	v.gate = &retirementControlGate{store: s, actor: b.actor, cause: b.cause, scope: pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: b.p}}
	v.gate.commit = ec.ProjectExecutionRetirementCommit{ProjectID: b.p, OperationID: b.cause.OperationID, ProjectVersion: b.cause.ProjectVersion, ProcessID: pair.state.process.String(), AttemptID: newTestID[f.TransactionAttempt](t).String(), Fence: 2, TransactionID: "1001"}
	kinds := []ec.ProjectExecutionRetirementReferenceKind{ec.ProjectExecutionToolReferences, ec.ProjectExecutionVariablesReferences, ec.ProjectExecutionSkillReferences, ec.ProjectExecutionMountReferences}
	for n, kind := range kinds {
		r := &retirementControlRef{kind: kind, authority: a, store: s}
		key, lockErr := f.AggregateLock(f.SkillAggregate, newTestID[struct{}](t).String())
		if lockErr != nil {
			t.Fatal(lockErr)
		}
		r.plan = &retirementControlRefPlan{owner: r, digest: projectInvocationDigest(string(kind)), locks: []f.LockRequest{{Key: key, Mode: f.Shared}}}
		v.refs[n] = r
	}
	v.consumer, err = NewProjectExecutionRetirement(pair, v.gate, v.refs[0], v.refs[1], v.refs[2], v.refs[3])
	if err != nil {
		t.Fatal(err)
	}
	result, err := pair.Service().Launch(context.Background(), b.launch.actor, b.launch.request)
	if err != nil {
		t.Fatal("original owned Launch", err)
	}
	if result.Replayed != legacy {
		t.Fatal("fixture did not use the requested original create/replay path")
	}
	v.id = result.Execution.ID
	row := b.launch.store.rows[v.id.String()]
	raw, err := json.Marshal(map[string]any{"id": row[0], "project_id": row[1], "agent_id": row[2], "status": row[3], "version": row[4], "cancel_requested_at": row[5], "snapshot_id": row[6], "created_at": row[7], "updated_at": row[7], "started_at": row[8], "completed_at": row[9], "launch_request": json.RawMessage(row[10].([]byte)), "idempotency_key": row[11], "request_id": row[12], "request_digest": row[13], "trigger_reference_digest": row[13], "initiator": b.launch.actor.Details(), "waiting_reason": nil, "waiting_reference": nil, "waiting_at": nil})
	if err != nil {
		t.Fatal(err)
	}
	s.full[v.id.String()] = raw
	t.Cleanup(func() {
		s.setAfter(nil, false)
		pair.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := pair.Drain(ctx); err != nil || !pair.Joined() {
			t.Error("retirement original owner remains", err)
		}
	})
	return v
}
func (v *retirementFixture) with(ctx context.Context, fn ec.ProjectExecutionRetirementCall) (f.CommitResult, error) {
	return v.consumer.WithProjectExecutionRetirement(ctx, v.base.actor, v.base.cause, v.gate.scope, fn)
}
func (v *retirementFixture) discover(ctx context.Context) error {
	var err error
	v.plan, err = v.consumer.DiscoverProjectExecutionRetirement(ctx, v.base.actor, v.base.cause, v.gate.scope)
	return err
}
func (v *retirementFixture) transaction(ctx context.Context, locks []f.LockRequest, fn func(context.Context, f.Tx) error) (f.CommitResult, error) {
	cause, _ := f.NewRecoveryCause("execution-retirement-control", v.base.cause.OperationID.String(), v.base.p.String())
	result := v.store.WithinTx(ctx, cause, func(run context.Context, tx f.Tx) error {
		if err := v.store.AcquireAll(run, tx, locks); err != nil {
			return err
		}
		return fn(run, tx)
	})
	return result, commitError(result)
}
func (v *retirementFixture) apply(ctx context.Context) (f.CommitResult, error) {
	return v.with(ctx, func(run context.Context, confirmation bool) (f.CommitResult, error) {
		if confirmation {
			return f.NotCommittedResult(nil), errors.New("unexpected confirmation")
		}
		if err := v.discover(run); err != nil {
			return f.NotCommittedResult(nil), err
		}
		locks := projectCallLocks(v.base.p)
		if v.plan != nil {
			locks = v.plan.RequiredLocks()
		}
		return v.transaction(run, locks, func(in context.Context, tx f.Tx) error {
			if v.plan != nil {
				if err := v.consumer.RetireProjectExecutionInTx(in, tx, v.plan); err != nil {
					return err
				}
			} else {
				if err := v.consumer.RetireProjectExecutionCallsInTx(in, tx, v.base.actor, v.base.cause, v.gate.scope); err != nil {
					return err
				}
				if err := v.consumer.RequireProjectExecutionsEmptyInTx(in, tx, v.base.actor, v.base.cause, v.gate.scope); err != nil {
					return err
				}
			}
			v.store.event("checkpoint")
			return nil
		})
	})
}
func (v *retirementFixture) confirm(ctx context.Context) (f.CommitResult, error) {
	return v.with(ctx, func(run context.Context, confirmation bool) (f.CommitResult, error) {
		if !confirmation {
			return f.NotCommittedResult(nil), errors.New("missing confirmation")
		}
		locks := projectCallLocks(v.base.p)
		if v.plan != nil {
			locks = v.plan.RequiredLocks()
		}
		return v.transaction(run, locks, func(in context.Context, tx f.Tx) error {
			var err error
			v.confirmed, err = v.consumer.ConfirmProjectExecutionRetirementInTx(in, tx, v.plan)
			return err
		})
	})
}

func TestExecutionRetirementOriginalPlanAndStages(t *testing.T) {
	v := newRetirementFixture(t)
	if len(v.store.origins) != 1 {
		t.Fatal("original owned INSERT did not mint its origin")
	}
	result, err := v.apply(context.Background())
	if err != nil || result.State() != f.Committed || v.plan == nil {
		t.Fatal(result.State(), err)
	}
	want := []string{"retire:tool", "retire:variables", "retire:skill", "retire:mount", "delete:executions", "finalize:skill", "require:tool", "require:variables", "require:skill", "require:mount", "checkpoint"}
	_, events := v.store.snapshot()
	if !slices.Equal(events, want) {
		t.Fatal("original phase order", events)
	}
	if v.refs[2].commit != v.gate.commit {
		t.Fatal("Skill received another claim/transaction")
	}
	for _, r := range v.refs {
		if r.tx != v.refs[0].tx {
			t.Fatal("reference retirement split transactions")
		}
		if r.authority.RequireProjectExecutionRetirementPreparedInTx(r.captured, r.tx, r.request) == nil || r.authority.RequireProjectExecutionRetirementAppliedInTx(r.captured, r.tx, r.request) == nil {
			t.Fatal("escaped witness remained active")
		}
	}
	// A subsequent explicit call retires only this P's closed technical tail.
	if _, err = v.apply(context.Background()); err != nil {
		t.Fatal("empty tail", err)
	}
	if v.plan != nil {
		t.Fatal("empty P fabricated a canonical plan")
	}
	if _, err = v.with(context.Background(), func(context.Context, bool) (f.CommitResult, error) { return f.CommittedResult(), nil }); err == nil {
		t.Fatal("fabricated callback granted success")
	}
}

func TestExecutionRetirementOriginalCreationNotReplay(t *testing.T) {
	v := newRetirementFixtureWithLegacy(t, true)
	if len(v.store.origins) != 0 {
		t.Fatal("owned replay minted original creation provenance")
	}
	// The real Project callback has not begun its mutation Store when discovery
	// refuses. Its zero result must not turn this known refusal into Unknown.
	var original error
	result, err := v.with(context.Background(), func(ctx context.Context, _ bool) (f.CommitResult, error) {
		original = v.discover(ctx)
		return f.CommitResult{}, original
	})
	var refused *f.Fault
	if result.State() != f.NotCommitted || err != original || !errors.As(err, &refused) || refused.Code != f.ResourceBusy {
		t.Fatal("later replay substituted for original creation", result.State(), err)
	}
	if writes, events := v.store.snapshot(); writes != 0 || len(events) != 0 {
		t.Fatal("legacy origin reached reference/canonical mutation", events)
	}
	v.pair.state.mu.Lock()
	retained := v.pair.state.retirements[v.base.p] != nil
	v.pair.state.mu.Unlock()
	if retained {
		t.Fatal("known discovery refusal retained an Unknown control")
	}
	entered := false
	result, err = v.with(context.Background(), func(context.Context, bool) (f.CommitResult, error) { entered = true; return f.CommitResult{}, original })
	if !entered || result.State() != f.NotCommitted || err != original {
		t.Fatal("known refusal blocked the next explicit call", err)
	}
}

func TestExecutionRetirementCanonicalAndClosedOwners(t *testing.T) {
	for _, field := range []string{"id", "project_id", "agent_id", "status", "version", "idempotency_key", "request_id", "request_digest", "trigger_reference_digest", "launch_request", "initiator", "cancel_requested_at", "snapshot_id", "created_at", "updated_at", "started_at", "completed_at", "waiting_reason", "waiting_reference", "waiting_at"} {
		t.Run(field, func(t *testing.T) {
			v := newRetirementFixture(t)
			_, err := v.with(context.Background(), func(ctx context.Context, _ bool) (f.CommitResult, error) {
				if err := v.discover(ctx); err != nil {
					return f.NotCommittedResult(nil), err
				}
				var raw map[string]json.RawMessage
				if err := json.Unmarshal(v.store.full[v.id.String()], &raw); err != nil {
					t.Fatal(err)
				}
				// A valid JSON row with the complete original projection except
				// one lost field must fail before any reference mutation.
				delete(raw, field)
				changed, err := json.Marshal(raw)
				if err != nil {
					t.Fatal(err)
				}
				v.store.full[v.id.String()] = changed
				return v.transaction(ctx, v.plan.RequiredLocks(), func(run context.Context, tx f.Tx) error {
					return v.consumer.RetireProjectExecutionInTx(run, tx, v.plan)
				})
			})
			writes, events := v.store.snapshot()
			if err == nil || writes != 0 || len(events) != 0 {
				t.Fatal("partial canonical authorized retirement", err, events)
			}
		})
	}
	for _, which := range []string{"original-launch-identity", "same-process-other-instance", "running-call", "extra-canonical-field", "stale-waiting-fact", "nil-reference-plan"} {
		t.Run(which, func(t *testing.T) {
			v := newRetirementFixture(t)
			switch which {
			case "original-launch-identity":
				v.base.store.mu.Lock()
				for id, row := range v.base.store.rows {
					row.Identity = strings.Replace(row.Identity, `"command":"launch"`, `"command":"lookup_launch"`, 1)
					row.Digest = projectInvocationDigest(row.Identity)
					v.base.store.rows[id] = row
				}
				v.base.store.mu.Unlock()
			case "same-process-other-instance":
				v.base.store.mu.Lock()
				for id, row := range v.base.store.rows {
					row.Instance = newTestID[projectCallInstance](t)
					v.base.store.rows[id] = row
				}
				v.base.store.mu.Unlock()
			case "running-call":
				v.base.store.mu.Lock()
				for id, row := range v.base.store.rows {
					row.Phase = "running"
					v.base.store.rows[id] = row
				}
				v.base.store.mu.Unlock()
			case "extra-canonical-field", "stale-waiting-fact":
				var raw map[string]json.RawMessage
				if err := json.Unmarshal(v.store.full[v.id.String()], &raw); err != nil {
					t.Fatal(err)
				}
				if which == "extra-canonical-field" {
					raw["future_owner"] = json.RawMessage(`true`)
				} else {
					raw["waiting_reason"] = json.RawMessage(`"approval"`)
				}
				changed, err := json.Marshal(raw)
				if err != nil {
					t.Fatal(err)
				}
				v.store.full[v.id.String()] = changed
			case "nil-reference-plan":
				v.refs[0].nilPlan = true
			}
			_, err := v.apply(context.Background())
			writes, _ := v.store.snapshot()
			if err == nil || writes != 0 {
				t.Fatal("unowned facts were retired", err, writes)
			}
		})
	}
}

func TestExecutionRetirementUnsupportedAndHistory(t *testing.T) {
	for _, table := range retirementUnsupportedTables {
		t.Run(table, func(t *testing.T) {
			v := newRetirementFixture(t)
			v.store.unsupported[table] = true
			_, err := v.apply(context.Background())
			writes, _ := v.store.snapshot()
			if err == nil || writes != 0 {
				t.Fatal("unsupported owner crossed the fixed four refs", err)
			}
		})
	}
	for _, table := range []string{"snapshots", "preparation_claims", "preparation_attempts", "preparation_inputs"} {
		t.Run(table, func(t *testing.T) {
			// preparation_inputs deliberately reaches the original strict source
			// loader, whose unbound full projection refuses here. It does not
			// claim to reach origin matching with a complete captured input.
			v := newRetirementFixture(t)
			raw, err := json.Marshal([]map[string]any{{"project_id": v.base.p.String(), "agent_id": v.base.launch.request.AgentID.String(), "execution_id": v.id.String(), "process_id": v.pair.state.process.String(), "attempt_id": newTestID[struct{}](t).String(), "fence": int64(1), "id": newTestID[struct{}](t).String(), "start_id": newTestID[struct{}](t).String(), "started_version": int64(2), "digest": v.base.launch.store.rows[v.id.String()][13]}})
			if err != nil {
				t.Fatal(err)
			}
			v.store.history[table] = raw
			_, err = v.apply(context.Background())
			writes, _ := v.store.snapshot()
			if err == nil || writes != 0 {
				t.Fatal("Launch did not own Preparation/Direct history", err)
			}
		})
	}
	for _, raw := range []string{`[{"schema_version":2,"tool_pending":false,"transcript_through":1}]`, `[{"schema_version":1,"tool_pending":true,"transcript_through":1}]`, `[{"schema_version":1,"tool_pending":false,"transcript_through":3}]`} {
		t.Run("round-closed-shape", func(t *testing.T) {
			v := newRetirementFixture(t)
			v.store.history["rounds"] = []byte(raw)
			_, err := v.apply(context.Background())
			writes, _ := v.store.snapshot()
			if err == nil || writes != 0 {
				t.Fatal("later round escaped", err)
			}
		})
	}
	// The repository's exact-row batch algorithm covers all seven fixed
	// tables. These opaque records are not claimed to be full Preparation or
	// Direct business receipts; discovery/owner validation is covered above.
	v := newRetirementFixture(t)
	b := &executionRetirementBefore{record: &executionRecord{summary: ec.Summary{ID: v.id}}, raw: bytes.Clone(v.store.full[v.id.String()])}
	for n, table := range retirementHistoryTables {
		raw, err := json.Marshal([]map[string]string{{"execution_id": v.id.String(), "controlled_row": table}})
		if err != nil {
			t.Fatal(err)
		}
		v.store.history[table] = raw
		b.history[n] = bytes.Clone(raw)
		b.counts[n] = 1
	}
	_, err := v.transaction(context.Background(), projectCallLocks(v.base.p), func(ctx context.Context, tx f.Tx) error {
		x, err := v.store.InTx(tx)
		if err != nil {
			return err
		}
		return deleteRetirementBefore(ctx, x, b)
	})
	_, events := v.store.snapshot()
	want := []string{}
	for _, table := range retirementHistoryTables {
		want = append(want, "delete:"+table)
	}
	want = append(want, "delete:executions")
	if err != nil || !slices.Equal(events, want) {
		t.Fatal("seven-history exact delete order", events, err)
	}
}

func TestExecutionRetirementPrivateAuthority(t *testing.T) {
	v := newRetirementFixture(t)
	for _, ctx := range []context.Context{nil, context.Background()} {
		actor := v.base.actor
		if ctx != nil {
			actor = v.base.launch.actor
		}
		result, err := v.consumer.WithProjectExecutionRetirement(ctx, actor, v.base.cause, v.gate.scope, func(context.Context, bool) (f.CommitResult, error) {
			t.Error("invalid caller entered")
			return f.CommittedResult(), nil
		})
		if err == nil || result.State() != f.NotCommitted || result.AttemptID().Validate() == nil {
			t.Fatal("early refusal invented Unknown attempt", result.State(), err)
		}
	}
	_, err := v.with(context.Background(), func(ctx context.Context, _ bool) (f.CommitResult, error) {
		if err := v.discover(ctx); err != nil {
			return f.NotCommittedResult(nil), err
		}
		return v.transaction(ctx, v.plan.RequiredLocks(), func(run context.Context, tx f.Tx) error {
			request := v.plan.Request()
			a := v.pair.Service().deps.Authority
			if a.RequireProjectExecutionRetirementPreparedInTx(run, tx, request) == nil || a.RequireProjectExecutionRetirementAppliedInTx(run, tx, request) == nil {
				t.Fatal("public request minted a phase")
			}
			if err := v.consumer.RetireProjectExecutionInTx(run, tx, nil); err == nil {
				t.Fatal("nil plan accepted")
			}
			if _, err := v.consumer.ConfirmProjectExecutionRetirementInTx(run, tx, v.plan); err == nil {
				t.Fatal("normal call borrowed Unknown confirmation")
			}
			return nil
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.consumer.DiscoverProjectExecutionRetirement(context.Background(), v.base.actor, v.base.cause, v.gate.scope); err == nil {
		t.Fatal("missing call scope accepted")
	}
	if _, err := NewProjectExecutionRetirement(v.pair, v.gate, v.refs[1], v.refs[0], v.refs[2], v.refs[3]); err == nil {
		t.Fatal("wrong fixed reference family accepted")
	}
	// A public, correctly shaped binding and identity cannot restore a plan
	// into a new control after the original callback has returned.
	_, err = v.with(context.Background(), func(ctx context.Context, _ bool) (f.CommitResult, error) {
		_, restoreErr := v.consumer.RestoreProjectExecutionRetirement(ctx, v.base.actor, v.base.cause, v.gate.scope, v.plan.Request().Binding, v.gate.commit, v.consumer.ProjectExecutionRetirementIdentity())
		if restoreErr == nil {
			t.Fatal("public values restored authority")
		}
		return f.NotCommittedResult(nil), restoreErr
	})
	if err == nil {
		t.Fatal("restore unexpectedly succeeded")
	}
}

func requireRetirementUnknown(t *testing.T, result f.CommitResult, err error, original f.CommitResult) {
	t.Helper()
	retained, ok := UnknownAttempt(err)
	if err == nil || !ok || result.State() != f.Unknown || retained.State() != f.Unknown || result.AttemptID() != original.AttemptID() || retained.AttemptID() != original.AttemptID() || !sameExecutionRecoveryCause(result.Cause(), original.Cause()) || !sameExecutionRecoveryCause(retained.Cause(), original.Cause()) {
		t.Fatal("first raw CommitResult/cause was replaced", result.State(), err)
	}
}

func TestExecutionRetirementOriginalReturns(t *testing.T) {
	t.Run("original-exec-error-after-cancel", func(t *testing.T) {
		v := newRetirementFixture(t)
		entered, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		sentinel := errors.New("original retirement Exec")
		original := f.NewFault(f.Forbidden, f.NotCommitted).WithCause(sentinel)
		v.store.exec = func(context.Context, string) error { close(entered); <-release; return original }
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() { _, err := v.apply(ctx); done <- err }()
		defer func() {
			once.Do(func() { close(release) })
			if err := associatedTestReceive(t, done); !errors.Is(err, sentinel) {
				t.Error("original Exec error lost", err)
			}
		}()
		associatedTestReceive(t, entered)
		cancel()
		select {
		case <-done:
			t.Fatal("cancel replaced original Exec return")
		default:
		}
		if _, events := v.store.snapshot(); slices.Contains(events, "finalize:skill") || slices.Contains(events, "checkpoint") {
			t.Fatal("unreturned Exec advanced", events)
		}
	})
	t.Run("service-done", func(t *testing.T) {
		v := newRetirementFixture(t)
		request := v.base.launch.request.Clone()
		request.Meta.IdempotencyKey = "original-done"
		digest, err := request.Digest()
		if err != nil {
			t.Fatal(err)
		}
		key := ec.LaunchLookupKey{ProjectID: v.base.p, AgentID: request.AgentID, IdempotencyKey: request.Meta.IdempotencyKey}
		ctx, entry, err := v.pair.state.admitService(context.Background(), v.base.launch.actor, key, digest, "lookup_launch")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = v.pair.Service().lookup(ctx, v.base.launch.actor, key, digest); err != nil {
			t.Fatal(err)
		}
		if err = v.pair.state.finish(entry, nil); err != nil {
			t.Fatal(err)
		}
		calls := v.pair.Service().calls
		calls.mu.Lock()
		var unlock sync.Once
		defer unlock.Do(calls.mu.Unlock)
		done := make(chan struct{})
		go func() { v.pair.state.returned(entry); close(done) }()
		defer func() { unlock.Do(calls.mu.Unlock); associatedTestReceive(t, done) }()
		// Original callSet.done cancels this real context before taking mu.
		associatedTestReceive(t, ctx.Done())
		before, _, _ := v.base.store.counts()
		_, err = v.with(context.Background(), func(context.Context, bool) (f.CommitResult, error) {
			t.Error("entered before Service.done")
			return f.CommittedResult(), nil
		})
		after, _, _ := v.base.store.counts()
		if err == nil || before != after {
			t.Fatal("retirement crossed original Service.done", err)
		}
		select {
		case <-done:
			t.Fatal("original done was not held")
		default:
		}
	})
	t.Run("caller-store-return", func(t *testing.T) {
		v := newRetirementFixture(t)
		entered, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		v.store.setAfter(func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
			if cause.Details().Owner == "execution-retirement-control" {
				close(entered)
				<-release
			}
			return result
		}, false)
		done := make(chan error, 1)
		go func() { _, err := v.apply(context.Background()); done <- err }()
		defer func() {
			once.Do(func() { close(release) })
			if err := associatedTestReceive(t, done); err != nil {
				t.Error(err)
			}
		}()
		associatedTestReceive(t, entered)
		if len(v.store.full) != 0 {
			t.Fatal("barrier is not after physical canonical deletion")
		}
		copyAdapter, err := NewProjectExecutionRetirement(v.pair, v.gate, v.refs[0], v.refs[1], v.refs[2], v.refs[3])
		if err != nil {
			t.Fatal(err)
		}
		for _, adapter := range []*ProjectExecutionRetirement{v.consumer, copyAdapter} {
			var rejected f.CommitResult
			if rejected, err = adapter.WithProjectExecutionRetirement(context.Background(), v.base.actor, v.base.cause, v.gate.scope, func(context.Context, bool) (f.CommitResult, error) {
				t.Error("late cleanup entered")
				return f.CommittedResult(), nil
			}); err == nil || rejected.State() != f.NotCommitted || rejected.AttemptID().Validate() == nil {
				t.Fatal("late cleanup crossed original return")
			}
		}
		for _, request := range []bool{false, true} {
			if request {
				_, err = v.pair.RequestProjectStop(context.Background(), v.base.actor, v.base.cause, v.gate.scope)
			} else {
				_, err = v.pair.InspectProjectStop(context.Background(), v.base.actor, v.base.cause, v.gate.scope)
			}
			if err == nil {
				t.Fatal("late stop crossed cleanup", request)
			}
		}
		lookup := func(project i.ProjectID, key string) error {
			r := v.base.launch.request.Clone()
			r.ProjectID = project
			r.Meta.IdempotencyKey = f.IdempotencyKey(key)
			d, e := r.Digest()
			if e != nil {
				return e
			}
			_, e = v.pair.Service().LookupLaunch(context.Background(), v.base.launch.actor, ec.LaunchLookupKey{ProjectID: project, AgentID: r.AgentID, IdempotencyKey: r.Meta.IdempotencyKey}, d)
			return e
		}
		if lookup(v.base.p, "late-A") == nil {
			t.Fatal("late A admission escaped")
		}
		if err = lookup(v.base.b, "live-B"); err != nil {
			t.Fatal("A cleanup blocked B", err)
		}
		select {
		case err := <-done:
			t.Fatal("returned before original Store", err)
		default:
		}
	})
}

func TestExecutionRetirementUnknownOriginalConfirmation(t *testing.T) {
	for _, physical := range []string{"committed", "rolled-back", "empty-committed", "empty-rolled-back", "empty-before-delete"} {
		t.Run(physical, func(t *testing.T) {
			v := newRetirementFixture(t)
			empty := strings.HasPrefix(physical, "empty-")
			rolledBack := strings.HasSuffix(physical, "rolled-back") || physical == "empty-before-delete"
			if empty {
				if _, err := v.apply(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			attempt := newTestID[f.TransactionAttempt](t)
			if physical == "empty-before-delete" {
				v.store.exec = func(context.Context, string) error { return errors.New("original Store failed before first delete") }
			}
			var first f.CommitResult
			v.store.setAfter(func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
				if cause.Details().Owner == "execution-retirement-control" {
					first = f.UnknownResult(attempt, cause)
					return first
				}
				return result
			}, rolledBack)
			result, err := v.apply(context.Background())
			if err == nil || result.State() != f.Unknown || result.AttemptID() != attempt {
				t.Fatal("original Unknown lost", result.State(), err)
			}
			requireRetirementUnknown(t, result, err, first)
			originalPlan := v.plan
			v.store.exec = nil
			before, _ := v.store.snapshot()
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			v.store.setAfter(func(_ context.Context, _ f.TransactionCause, result f.CommitResult) f.CommitResult {
				close(entered)
				<-release
				return result
			}, false)
			done := make(chan error, 1)
			joined := false
			go func() { _, err := v.confirm(context.Background()); done <- err }()
			defer func() {
				once.Do(func() { close(release) })
				if !joined {
					if err := associatedTestReceive(t, done); err != nil {
						t.Error(err)
					}
				}
			}()
			associatedTestReceive(t, entered)
			v.pair.state.mu.Lock()
			control := v.pair.state.retirements[v.base.p]
			retained := control != nil && control.active && control.unknown != nil && control.unknown.AttemptID() == attempt
			v.pair.state.mu.Unlock()
			if !retained {
				t.Fatal("confirmation callback replaced original actual return")
			}
			after, _ := v.store.snapshot()
			if before != after || v.plan != originalPlan {
				t.Fatal("confirmation wrote or rebuilt original plan")
			}
			_, blocked := v.with(context.Background(), func(context.Context, bool) (f.CommitResult, error) {
				t.Error("parallel unknown callback")
				return f.CommittedResult(), nil
			})
			if blocked == nil {
				t.Fatal("retained confirmation was bypassed")
			}
			// Finish this original confirmation before asking for a later write.
			once.Do(func() { close(release) })
			err = associatedTestReceive(t, done)
			joined = true
			if err != nil {
				t.Fatal(err)
			}
			if v.confirmed == rolledBack {
				t.Fatal("wrong physical confirmation", v.confirmed)
			}
			v.store.setAfter(nil, false)
			if rolledBack {
				if _, err = v.apply(context.Background()); err != nil {
					t.Fatal("next explicit mutation", err)
				}
			}
		})
	}
	t.Run("discovery", func(t *testing.T) {
		v := newRetirementFixture(t)
		attempt := newTestID[f.TransactionAttempt](t)
		v.store.setAfter(func(_ context.Context, cause f.TransactionCause, _ f.CommitResult) f.CommitResult {
			return f.UnknownResult(attempt, cause)
		}, false)
		result, err := v.apply(context.Background())
		if err == nil || result.AttemptID() != attempt {
			t.Fatal("discovery Unknown not retained", err)
		}
		before, _ := v.store.snapshot()
		v.store.setAfter(nil, false)
		_, err = v.with(context.Background(), func(ctx context.Context, confirmation bool) (f.CommitResult, error) {
			if !confirmation {
				t.Fatal("discovery confirmation not selected")
			}
			if err := v.discover(ctx); err != nil {
				return f.NotCommittedResult(nil), err
			}
			if _, err := v.transaction(ctx, v.plan.RequiredLocks(), func(run context.Context, tx f.Tx) error {
				return v.consumer.RetireProjectExecutionInTx(run, tx, v.plan)
			}); err == nil {
				t.Fatal("discovery confirmation wrote")
			}
			return f.CommittedResult(), nil
		})
		after, _ := v.store.snapshot()
		if err != nil || before != after {
			t.Fatal("read-only discovery recovery", err)
		}
		if _, err = v.apply(context.Background()); err != nil {
			t.Fatal(err)
		}
	})
}

func TestExecutionRetirementUnknownDriftAndCancelledReturn(t *testing.T) {
	v := newRetirementFixture(t)
	attempt := newTestID[f.TransactionAttempt](t)
	v.store.setAfter(func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
		if cause.Details().Owner == "execution-retirement-control" {
			return f.UnknownResult(attempt, cause)
		}
		return result
	}, true)
	first, err := v.apply(context.Background())
	if err == nil {
		t.Fatal("expected Unknown")
	}
	v.store.setAfter(nil, false)
	old := v.refs[0].plan.digest
	v.refs[0].plan = &retirementControlRefPlan{owner: v.refs[0], digest: projectInvocationDigest("drift"), locks: v.refs[0].plan.RequiredLocks()}
	result, err := v.confirm(context.Background())
	if err == nil || result.AttemptID() != attempt || !sameExecutionRecoveryCause(result.Cause(), first.Cause()) {
		t.Fatal("drift lost original cause", err)
	}
	requireRetirementUnknown(t, result, err, first)
	v.refs[0].plan = v.plan.(*executionRetirementPlan).references[0].(*retirementControlRefPlan)
	if v.refs[0].plan.digest != old {
		t.Fatal("original plan was changed")
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	v.store.setAfter(func(_ context.Context, _ f.TransactionCause, result f.CommitResult) f.CommitResult {
		close(entered)
		<-release
		return result
	}, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type returned struct {
		result f.CommitResult
		err    error
	}
	done := make(chan returned, 1)
	go func() { r, e := v.confirm(ctx); done <- returned{r, e} }()
	defer func() {
		once.Do(func() { close(release) })
		value := associatedTestReceive(t, done)
		if value.result.AttemptID() != attempt || value.err == nil {
			t.Error("cancel replaced first Unknown", value.err)
		}
		requireRetirementUnknown(t, value.result, value.err, first)
		v.store.setAfter(nil, false)
		if _, err := v.confirm(context.Background()); err != nil {
			t.Error("cleanup original confirmation", err)
		}
	}()
	associatedTestReceive(t, entered)
	cancel()
	select {
	case <-done:
		t.Fatal("cancel replaced Store return")
	default:
	}
}

func TestExecutionRetirementDeleteFailures(t *testing.T) {
	expectedTables := []string{"cancel_commands", "loop_approval_results", "runtime_snapshots", "transcript_entries", "rounds", "snapshots", "preparation_inputs", "preparation_claims", "preparation_attempts", "skill_assignment_ingress", "executions"}
	for failed, table := range expectedTables {
		t.Run(table, func(t *testing.T) {
			v := newRetirementFixture(t)
			b := &executionRetirementBefore{record: &executionRecord{summary: ec.Summary{ID: v.id}}, raw: bytes.Clone(v.store.full[v.id.String()])}
			for n, name := range expectedTables[:len(expectedTables)-1] {
				raw := []byte(`[{"controlled":true}]`)
				v.store.history[name] = raw
				b.history[n] = bytes.Clone(raw)
				b.counts[n] = 1
			}
			v.store.short = table
			_, err := v.transaction(context.Background(), projectCallLocks(v.base.p), func(ctx context.Context, tx f.Tx) error {
				x, e := v.store.InTx(tx)
				if e != nil {
					return e
				}
				return deleteRetirementBefore(ctx, x, b)
			})
			if err == nil {
				t.Fatal("short delete accepted")
			}
			_, events := v.store.snapshot()
			want := []string{}
			for _, name := range expectedTables[:failed] {
				want = append(want, "delete:"+name)
			}
			if !slices.Equal(events, want) {
				t.Fatal("short exact count did not stop at the original table", events, want)
			}
		})
	}
}
