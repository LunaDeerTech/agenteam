package projectvariable

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// These controls use the original Variables entry points and private ownership
// protocol. Store, Project and process proofs are controlled ports; no case
// establishes a real database lock, flock, or foreign process death.
// The before hook supplies a physical result without running business SQL;
// the record map is not a PostgreSQL transaction/rollback implementation.
type ownershipControlStore struct {
	mu     sync.Mutex
	live   map[f.Tx][]f.LockRequest
	causes []f.TransactionCause
	before func(context.Context, f.TransactionCause) *f.CommitResult
	after  func(context.Context, f.TransactionCause, f.CommitResult) f.CommitResult
	row    func(context.Context, f.Tx, string, []any) postgres.Row
	exec   func(context.Context, f.Tx, string, []any) (pgconn.CommandTag, error)
}

func (s *ownershipControlStore) WithinTx(ctx context.Context, cause f.TransactionCause, callback func(context.Context, f.Tx) error) f.CommitResult {
	s.mu.Lock()
	s.causes = append(s.causes, cause)
	s.mu.Unlock()
	if s.before != nil {
		if result := s.before(ctx, cause); result != nil {
			return *result
		}
	}
	tx := f.NewTx()
	s.mu.Lock()
	s.live[tx] = nil
	s.mu.Unlock()
	err := callback(ctx, tx)
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
		result = s.after(ctx, cause, result)
	}
	return result
}

func (s *ownershipControlStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	s.mu.Lock()
	_, exists := s.live[tx]
	s.mu.Unlock()
	if !tx.Valid() || !exists {
		return nil, fault(f.Forbidden)
	}
	return ownershipControlExecutor{s, tx}, nil
}

func (s *ownershipControlStore) AcquireAll(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	if _, err := s.InTx(tx); err != nil {
		return err
	}
	s.mu.Lock()
	s.live[tx] = append([]f.LockRequest(nil), locks...)
	s.mu.Unlock()
	return ctx.Err()
}

func (s *ownershipControlStore) RequireHeldLocks(ctx context.Context, tx f.Tx, want []f.LockRequest) error {
	if _, err := s.InTx(tx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, needed := range want {
		found := false
		for _, actual := range s.live[tx] {
			if f.CompareLockKeys(actual.Key, needed.Key) == 0 && (actual.Mode == f.Exclusive || actual.Mode == needed.Mode) {
				found = true
				break
			}
		}
		if !found {
			return fault(f.Forbidden)
		}
	}
	return ctx.Err()
}

func (*ownershipControlStore) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	panic("SQL outside original transaction")
}
func (*ownershipControlStore) Query(context.Context, string, ...any) (*postgres.Rows, error) {
	panic("SQL outside original transaction")
}
func (*ownershipControlStore) QueryRow(context.Context, string, ...any) postgres.Row {
	panic("SQL outside original transaction")
}

type ownershipControlExecutor struct {
	store *ownershipControlStore
	tx    f.Tx
}

func (x ownershipControlExecutor) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	if _, err := x.store.InTx(x.tx); err != nil {
		return ownershipControlRow{err: err}
	}
	if x.store.row == nil {
		return ownershipControlRow{err: pgx.ErrNoRows}
	}
	return x.store.row(ctx, x.tx, query, args)
}
func (x ownershipControlExecutor) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if _, err := x.store.InTx(x.tx); err != nil {
		return pgconn.CommandTag{}, err
	}
	if x.store.exec == nil {
		return pgconn.CommandTag{}, errors.New("unexpected mutation")
	}
	return x.store.exec(ctx, x.tx, query, args)
}
func (x ownershipControlExecutor) Query(context.Context, string, ...any) (*postgres.Rows, error) {
	return nil, errors.New("actual Rows belong to integration; pure page controls use the private decoder")
}

type ownershipControlProcess struct {
	store   *ownershipControlStore
	process oc.ProcessID
	check   func(context.Context, f.Tx, oc.ProcessID) error
	death   func(context.Context, oc.ProcessID) error
}

func (p *ownershipControlProcess) RequireCurrentProcessInTx(ctx context.Context, tx f.Tx, process oc.ProcessID) error {
	if process != p.process {
		return fault(f.Forbidden)
	}
	if _, err := p.store.InTx(tx); err != nil {
		return err
	}
	if p.check != nil {
		return p.check(ctx, tx, process)
	}
	return ctx.Err()
}
func (p *ownershipControlProcess) ConfirmStopped(ctx context.Context, process oc.ProcessID) error {
	if p.death == nil {
		return fault(f.ResourceBusy)
	}
	return p.death(ctx, process)
}

type ownershipControlProjects struct {
	pc.ProjectAuthority
	store   *ownershipControlStore
	actor   i.Actor
	project i.ProjectID
	grant   pc.ProjectAccess
	check   func(context.Context, f.Tx, i.Actor, pc.LifecycleCause, pc.ParticipantName, pc.OperationPhase) error
}

func (p *ownershipControlProjects) RequireOwnerInTx(ctx context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, intent i.AccessIntent) (pc.ProjectAccess, error) {
	if !actor.Equal(p.actor) || project != p.project {
		return pc.ProjectAccess{}, fault(f.Forbidden)
	}
	if err := p.store.RequireHeldLocks(ctx, tx, []f.LockRequest{userLock(actor.Details().UserID, f.Shared), projectLock(project, f.Shared)}); err != nil {
		return pc.ProjectAccess{}, err
	}
	return p.grant, ctx.Err()
}
func (p *ownershipControlProjects) ValidateLifecycleInTx(ctx context.Context, tx f.Tx, actor i.Actor, cause pc.LifecycleCause, participant pc.ParticipantName, phase pc.OperationPhase) error {
	if p.check == nil {
		return fault(f.Forbidden)
	}
	return p.check(ctx, tx, actor, cause, participant, phase)
}

func ownershipControlDependencies(t *testing.T, store *ownershipControlStore, projects *ownershipControlProjects) (Dependencies, SecretDependencies) {
	t.Helper()
	authority, err := NewAuthority(store)
	if err != nil {
		t.Fatal(err)
	}
	writes, err := NewSecretWriteAuthority(store, projects)
	if err != nil {
		t.Fatal(err)
	}
	events, err := c.RegisterVariableEvents(ec.NewCatalog())
	if err != nil {
		t.Fatal(err)
	}
	secretEvents, err := c.RegisterSecretVariableEvents(ec.NewCatalog())
	if err != nil {
		t.Fatal(err)
	}
	ordinary := Dependencies{Authority: authority, Projects: projects, Events: struct{ ob.Appender }{}, Audit: struct{ ac.Appender }{}, Activity: struct{ ActivityAuthority }{}, VariableEvents: events, Cursors: testKeys(t)}
	secrets := SecretDependencies{Authority: authority, Writes: writes, Secrets: struct{ sc.ProjectVariableWrites }{}, Projects: projects, Events: ordinary.Events, Audit: ordinary.Audit, Activity: ordinary.Activity, VariableEvents: secretEvents, Cursors: ordinary.Cursors}
	return ordinary, secrets
}

type ownershipControlRow struct {
	raw []byte
	err error
}

func (r ownershipControlRow) Scan(out ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(out) != 1 {
		return errors.New("unexpected projection")
	}
	p, ok := out[0].(*[]byte)
	if !ok {
		return errors.New("unexpected projection type")
	}
	*p = append([]byte(nil), r.raw...)
	return nil
}

type ownershipControl struct {
	t                     *testing.T
	pair                  *ProjectCallOwnership
	store                 *ownershipControlStore
	proof                 *ownershipControlProcess
	projects              *ownershipControlProjects
	record                *commandRecord
	actor                 i.Actor
	deps                  Dependencies
	secretDeps            SecretDependencies
	mu                    sync.Mutex
	records               map[string]projectCallRecord
	inserts, joins, reads int
}

func newOwnershipControl(t *testing.T) *ownershipControl {
	t.Helper()
	record, actor := runtimeRecord(t)
	at, _ := f.ParseInstant("2026-10-09T10:00:00Z")
	grant, err := pc.NewProjectAccess(actor, pc.ProjectRef{ID: record.Project, OwnerUserID: record.User, Name: "Project", NormalizedName: "project", Lifecycle: pc.Active, Version: 1, CreatedAt: at, UpdatedAt: at}, at)
	if err != nil {
		t.Fatal(err)
	}
	store := &ownershipControlStore{live: make(map[f.Tx][]f.LockRequest)}
	projects := &ownershipControlProjects{store: store, actor: actor, project: record.Project, grant: grant}
	proof := &ownershipControlProcess{store: store, process: testID[oc.Process](71)}
	h := &ownershipControl{t: t, store: store, proof: proof, projects: projects, record: record, actor: actor, records: make(map[string]projectCallRecord)}
	h.deps, h.secretDeps = ownershipControlDependencies(t, store, projects)
	h.pair, err = newProjectCallOwnership(store, h.deps, h.secretDeps, proof, proof.process)
	if err != nil {
		t.Fatal(err)
	}
	store.row, store.exec = h.row, h.exec
	t.Cleanup(func() {
		h.pair.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := h.pair.Drain(ctx); err != nil {
			t.Error("ownership cleanup did not join", err)
		}
	})
	return h
}

func (h *ownershipControl) row(ctx context.Context, tx f.Tx, query string, args []any) postgres.Row {
	h.mu.Lock()
	defer h.mu.Unlock()
	if strings.Contains(query, "FROM agenteam_projectvariable.project_calls WHERE id=$1") {
		r, ok := h.records[args[0].(string)]
		if !ok {
			return ownershipControlRow{err: pgx.ErrNoRows}
		}
		raw, err := json.Marshal(r)
		return ownershipControlRow{raw, err}
	}
	if strings.Contains(query, "jsonb_agg") && strings.Contains(query, "LIMIT 101") {
		rows := []projectCallRecord{}
		for _, r := range h.records {
			if r.Project.String() == args[0] && r.Phase == "running" && (r.Class == "mutation" || args[1] == true) {
				rows = append(rows, r)
			}
		}
		sort.Slice(rows, func(a, b int) bool { return rows[a].ID.String() < rows[b].ID.String() })
		if len(rows) > 101 {
			rows = rows[:101]
		}
		raw, err := json.Marshal(rows)
		return ownershipControlRow{raw, err}
	}
	h.reads++
	return ownershipControlRow{err: pgx.ErrNoRows}
}

func (h *ownershipControl) exec(ctx context.Context, tx f.Tx, query string, args []any) (pgconn.CommandTag, error) {
	if len(args) != 6 {
		return pgconn.CommandTag{}, errors.New("unexpected ownership tuple")
	}
	id, e1 := f.ParseID[projectCallIdentity](args[0].(string))
	project, e2 := f.ParseID[i.Project](args[1].(string))
	process, e3 := f.ParseID[oc.Process](args[2].(string))
	instance, e4 := f.ParseID[projectCallInstance](args[3].(string))
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
		return pgconn.CommandTag{}, errors.New("invalid ownership identity")
	}
	r := projectCallRecord{ID: id, Project: project, Process: process, Instance: instance, Lane: projectCallLane(args[4].(string)), Class: args[5].(string), Phase: "running"}
	mode := f.Shared
	insert := strings.HasPrefix(query, "INSERT INTO agenteam_projectvariable.project_calls")
	if !insert {
		mode = f.Exclusive
	}
	if err := h.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(project, mode)}); err != nil {
		return pgconn.CommandTag{}, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if insert {
		if _, exists := h.records[id.String()]; exists {
			return pgconn.CommandTag{}, errors.New("duplicate actual call")
		}
		h.records[id.String()] = r
		h.inserts++
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	}
	if !strings.HasPrefix(query, "UPDATE agenteam_projectvariable.project_calls SET phase='business_closed'") {
		return pgconn.CommandTag{}, errors.New("unexpected mutation")
	}
	old, exists := h.records[id.String()]
	if !exists || old != r {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	r.Phase = "business_closed"
	h.records[id.String()] = r
	h.joins++
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (h *ownershipControl) counts() (int, int, int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.inserts, h.joins, h.reads
}

func (h *ownershipControl) lookup(ctx context.Context, lane projectCallLane) error {
	if lane == projectCallSecret {
		q, err := c.NewSecretVariableCommandLookupRequest(c.SecretVariableCommandLookupFields{ProjectID: h.record.Project, Command: c.SecretCreateCommand, TargetID: h.record.Target, IdempotencyKey: "owned-key"})
		if err != nil {
			return err
		}
		out, err := h.pair.SecretService().LookupSecretVariableCommand(ctx, h.actor, q)
		if err == nil && out.Status() != c.SecretLookupNotObserved {
			return errors.New("unexpected secret observation")
		}
		return err
	}
	q, err := c.NewVariableCommandLookupRequest(c.VariableCommandLookupFields{ProjectID: h.record.Project, Command: h.record.Command, IdempotencyKey: h.record.Key, SemanticDigest: h.record.Semantic})
	if err != nil {
		return err
	}
	out, err := h.pair.Service().LookupVariableCommand(ctx, h.actor, q)
	if err == nil && out.Status() != c.LookupNotObserved {
		return errors.New("unexpected ordinary observation")
	}
	return err
}
func ownershipCause(cause f.TransactionCause) string { return cause.Details().Owner }
func ownershipRelease(ch chan struct{}) func() {
	var once sync.Once
	return func() { once.Do(func() { close(ch) }) }
}
func ownershipWait[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("original controlled call did not return")
		var zero T
		return zero
	}
}
func (h *ownershipControl) begin(t *testing.T, lane projectCallLane, kind callKind) (context.Context, *projectCallOwner, func()) {
	t.Helper()
	begin := h.pair.Service().beginProject
	if lane == projectCallSecret {
		begin = h.pair.SecretService().beginProject
	}
	ctx, call, done, err := begin(context.Background(), h.record.Project, kind)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err = h.pair.state.admit(ctx, call, h.actor, lane)
	if err != nil {
		done()
		t.Fatal(err)
	}
	w := ctx.Value(projectCallContextKey{}).(projectCallWitness)
	var once sync.Once
	finish := func() { once.Do(func() { h.pair.state.finish(ctx); done() }) }
	t.Cleanup(finish)
	return ctx, w.owner, finish
}
func (h *ownershipControl) lifecycle(t *testing.T, action pc.LifecycleAction) (i.Actor, pc.LifecycleCause, pc.ScopeRef) {
	t.Helper()
	cause := pc.LifecycleCause{OperationID: testID[pc.Operation](90), Action: action, ProjectVersion: 7}
	scope := pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: h.record.Project}
	reg, _ := i.RegisterService(i.ProjectLifecycle)
	actorScope, _ := i.InProject(scope.ProjectID)
	actor, err := reg.Actor(cause.OperationID.String(), actorScope)
	if err != nil {
		t.Fatal(err)
	}
	h.projects.check = func(ctx context.Context, tx f.Tx, a i.Actor, c pc.LifecycleCause, participant pc.ParticipantName, phase pc.OperationPhase) error {
		if !a.Equal(actor) || c != cause || participant != pc.SkillsParticipant || phase != pc.StopPhase {
			return fault(f.Forbidden)
		}
		return h.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(scope.ProjectID, f.Shared)})
	}
	return actor, cause, scope
}

func TestVariableCallOwnershipExplicitPairAndLegacy(t *testing.T) {
	h := newOwnershipControl(t)
	if h.pair.Service().state().ownership != h.pair.state || h.pair.SecretService().state().ownership != h.pair.state || h.pair.LocalStopper() == nil || len(h.store.causes) != 0 {
		t.Fatal("constructor did not publish one pure fixed pair")
	}
	ordinary, err := New(h.store, h.deps)
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := NewSecret(h.store, h.secretDeps)
	if err != nil {
		t.Fatal(err)
	}
	if ordinary.state().ownership != nil || secrets.state().ownership != nil {
		t.Fatal("default constructors opted into the new protocol")
	}
	q, _ := c.NewVariableCommandLookupRequest(c.VariableCommandLookupFields{ProjectID: h.record.Project, Command: h.record.Command, IdempotencyKey: h.record.Key, SemanticDigest: h.record.Semantic})
	if out, err := ordinary.LookupVariableCommand(context.Background(), h.actor, q); err != nil || out.Status() != c.LookupNotObserved {
		t.Fatal("legacy ordinary lookup", err)
	}
	sq, _ := c.NewSecretVariableCommandLookupRequest(c.SecretVariableCommandLookupFields{ProjectID: h.record.Project, Command: c.SecretCreateCommand, TargetID: h.record.Target, IdempotencyKey: "owned-key"})
	if out, err := secrets.LookupSecretVariableCommand(context.Background(), h.actor, sq); err != nil || out.Status() != c.SecretLookupNotObserved {
		t.Fatal("legacy secret lookup", err)
	}
	if inserts, closed, reads := h.counts(); inserts != 0 || closed != 0 || reads != 2 {
		t.Fatal("legacy path acquired new ledger dependency")
	}
	ordinary.Stop()
	secrets.Stop()
	if !ordinary.Joined() || !secrets.Joined() {
		t.Fatal("legacy lifecycle changed")
	}
	if pair, err := NewProjectCallOwnership(h.store, h.deps, h.secretDeps, nil, h.proof.process); err == nil || pair != nil {
		t.Fatal("nil public guard")
	}
	real, err := NewProjectCallOwnership(h.store, h.deps, h.secretDeps, &object.ProcessGuard{}, h.proof.process)
	if err != nil {
		t.Fatal("construction required bound guard", err)
	}
	_, err = real.Service().LookupVariableCommand(context.Background(), h.actor, q)
	code(t, err, f.DependencyUnbound)
	real.Stop()
	if err = real.Drain(context.Background()); err != nil || !real.Joined() {
		t.Fatal("unbound rejection leaked owner", err)
	}
	if inserts, closed, reads := h.counts(); inserts != 0 || closed != 0 || reads != 2 {
		t.Fatal("real unbound guard reached business")
	}
}

func TestVariableCallOwnershipMapsEveryEntry(t *testing.T) {
	for _, name := range []string{"ordinary-create", "ordinary-update", "ordinary-delete", "ordinary-get", "ordinary-list", "ordinary-lookup", "secret-create", "secret-update", "secret-delete", "secret-get", "secret-list", "secret-lookup"} {
		t.Run(name, func(t *testing.T) {
			h := newOwnershipControl(t)
			expectedLane := projectCallOrdinary
			if strings.HasPrefix(name, "secret-") {
				expectedLane = projectCallSecret
			}
			class := "read"
			if strings.HasSuffix(name, "create") || strings.HasSuffix(name, "update") || strings.HasSuffix(name, "delete") {
				class = "mutation"
			}
			var registration projectCallRecord
			h.store.before = func(ctx context.Context, cause f.TransactionCause) *f.CommitResult {
				if ownershipCause(cause) != "projectvariable-call-register" {
					t.Fatal("entry reached business before registration")
				}
				w, ok := ctx.Value(projectCallContextKey{}).(projectCallWitness)
				if !ok || w.state != h.pair.state || w.owner == nil || w.owner.returned {
					t.Fatal("missing original owner")
				}
				registration = w.owner.record
				r := f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted))
				return &r
			}
			v := f.Version(1)
			meta := f.CommandMeta{RequestID: testID[f.Request](73), IdempotencyKey: "entry-key"}
			updateMeta := meta
			updateMeta.ExpectedVersion = &v
			value := "owned-value-canary"
			update, _ := c.NewVariableUpdate(c.VariableUpdateFields{Value: &value})
			secretUpdate, _ := c.NewSecretVariableUpdate(c.SecretVariableUpdateFields{Description: &value})
			defer secretUpdate.Destroy()
			var err error
			switch name {
			case "ordinary-create":
				if err := c.ValidateCommandMeta(c.CreateCommand, meta); err != nil {
					t.Fatal("invalid ordinary-create test metadata", err)
				}
				_, err = h.pair.Service().CreateVariable(context.Background(), h.actor, meta, h.record.Project, *h.record.Input.Create)
			case "ordinary-update":
				if err := c.ValidateCommandMeta(c.UpdateCommand, updateMeta); err != nil {
					t.Fatal("invalid ordinary-update test metadata", err)
				}
				_, err = h.pair.Service().UpdateVariable(context.Background(), h.actor, updateMeta, h.record.Project, h.record.Target, update)
			case "ordinary-delete":
				if err := c.ValidateCommandMeta(c.DeleteCommand, updateMeta); err != nil {
					t.Fatal("invalid ordinary-delete test metadata", err)
				}
				_, err = h.pair.Service().DeleteVariable(context.Background(), h.actor, updateMeta, h.record.Project, h.record.Target)
			case "ordinary-get":
				_, err = h.pair.Service().GetVariable(context.Background(), h.actor, h.record.Project, h.record.Target)
			case "ordinary-list":
				_, err = h.pair.Service().ListVariables(context.Background(), h.actor, h.record.Project, f.PageRequest{Limit: 1})
			case "ordinary-lookup":
				err = h.lookup(context.Background(), projectCallOrdinary)
			case "secret-create":
				if err := c.ValidateSecretCommandMeta(c.SecretCreateCommand, meta); err != nil {
					t.Fatal("invalid secret-create test metadata", err)
				}
				material, e := sc.NewSecretMaterial([]byte(value))
				if e != nil {
					t.Fatal(e)
				}
				defer material.Destroy()
				create, e := c.NewSecretVariableCreate(c.SecretVariableCreateFields{ID: h.record.Target, Name: "OWNED", Value: material})
				if e != nil {
					t.Fatal(e)
				}
				defer create.Destroy()
				_, err = h.pair.SecretService().CreateSecretVariable(context.Background(), h.actor, meta, h.record.Project, create)
			case "secret-update":
				if err := c.ValidateSecretCommandMeta(c.SecretUpdateCommand, updateMeta); err != nil {
					t.Fatal("invalid secret-update test metadata", err)
				}
				_, err = h.pair.SecretService().UpdateSecretVariable(context.Background(), h.actor, updateMeta, h.record.Project, h.record.Target, secretUpdate)
			case "secret-delete":
				if err := c.ValidateSecretCommandMeta(c.SecretDeleteCommand, updateMeta); err != nil {
					t.Fatal("invalid secret-delete test metadata", err)
				}
				_, err = h.pair.SecretService().DeleteSecretVariable(context.Background(), h.actor, updateMeta, h.record.Project, h.record.Target)
			case "secret-get":
				_, err = h.pair.SecretService().GetSecretVariable(context.Background(), h.actor, h.record.Project, h.record.Target)
			case "secret-list":
				_, err = h.pair.SecretService().ListSecretVariables(context.Background(), h.actor, h.record.Project, f.PageRequest{Limit: 1})
			case "secret-lookup":
				err = h.lookup(context.Background(), projectCallSecret)
			}
			code(t, err, f.DependencyUnavailable)
			if !registration.valid() || registration.Lane != expectedLane || registration.Class != class || registration.Project != h.record.Project || registration.Process != h.proof.process {
				t.Fatal("wrong entry ownership", name)
			}
			encoded, _ := json.Marshal(registration)
			if strings.Contains(string(encoded), "canary") || strings.Contains(string(encoded), "entry-key") {
				t.Fatal("ledger contains business/Secret material")
			}
			if inserts, closed, reads := h.counts(); inserts != 0 || closed != 0 || reads != 0 || !h.pair.Joined() {
				t.Fatal("NotCommitted admission leaked work")
			}
			h.store.before = nil
		})
	}
}

func TestVariableCallOwnershipRegistrationActualReturn(t *testing.T) {
	for _, lane := range []projectCallLane{projectCallOrdinary, projectCallSecret} {
		t.Run(string(lane), func(t *testing.T) {
			h := newOwnershipControl(t)
			entered, release := make(chan struct{}), make(chan struct{})
			unblock := ownershipRelease(release)
			attempt := testID[f.TransactionAttempt](72)
			h.store.after = func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
				if ownershipCause(cause) == "projectvariable-call-register" {
					close(entered)
					<-release
					return f.UnknownResult(attempt, cause)
				}
				return f.UnknownResult(testID[f.TransactionAttempt](73), cause)
			}
			done := make(chan error, 1)
			go func() { done <- h.lookup(context.Background(), lane) }()
			joined := false
			t.Cleanup(func() {
				unblock()
				if !joined {
					ownershipWait(t, done)
				}
				h.store.after = nil
			})
			ownershipWait(t, entered)
			if inserts, _, reads := h.counts(); inserts != 1 || reads != 0 || h.pair.Joined() {
				t.Fatal("registration callback was published as a return")
			}
			h.pair.Stop()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := h.pair.Drain(ctx); !errors.Is(err, context.Canceled) || h.pair.Joined() {
				t.Fatal("Stop overtook registration original return")
			}
			unblock()
			err := ownershipWait(t, done)
			joined = true
			var saved commitFailure
			if !errors.As(err, &saved) || saved.result.AttemptID() != attempt || ownershipCause(saved.result.Cause()) != "projectvariable-call-register" {
				t.Fatal("checkpoint replaced original registration Unknown", err)
			}
			if _, _, reads := h.counts(); reads != 0 || h.pair.Joined() {
				t.Fatal("Unknown started business or forgot accounting")
			}
			h.store.after = nil
			if err := h.pair.Drain(context.Background()); err != nil || !h.pair.Joined() {
				t.Fatal("original ID did not settle after Stop", err)
			}
			if inserts, _, _ := h.counts(); inserts != 1 {
				t.Fatal("settlement registered a replacement call")
			}
		})
	}
}

func TestVariableCallOwnershipCheckpointPreservesResult(t *testing.T) {
	for _, lane := range []projectCallLane{projectCallOrdinary, projectCallSecret} {
		t.Run(string(lane), func(t *testing.T) {
			h := newOwnershipControl(t)
			h.store.after = func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
				if ownershipCause(cause) == "projectvariable-call-return" && result.State() == f.Committed {
					return f.UnknownResult(testID[f.TransactionAttempt](75), cause)
				}
				return result
			}
			if err := h.lookup(context.Background(), lane); err != nil {
				t.Fatal("checkpoint overwrote known lookup", err)
			}
			if h.pair.Joined() {
				t.Fatal("checkpoint Unknown dropped retained owner")
			}
			h.pair.state.mu.Lock()
			var original projectCallRecord
			for _, o := range h.pair.state.owners {
				original = o.record
				if !o.returned || o.checkpoint.State() != f.Unknown {
					t.Error("retained proof")
				}
			}
			h.pair.state.mu.Unlock()
			h.pair.Stop()
			h.store.after = nil
			if err := h.pair.Drain(context.Background()); err != nil || !h.pair.Joined() {
				t.Fatal("Stop lost private continuation", err)
			}
			if n, closed, reads := h.counts(); n != 1 || closed != 1 || reads != 1 {
				t.Fatal("settlement repeated business", n, closed, reads)
			}
			h.mu.Lock()
			closed := h.records[original.ID.String()]
			h.mu.Unlock()
			if closed.Phase != "business_closed" {
				t.Fatal("original metadata not retained")
			}
			actor, cause, scope := h.lifecycle(t, pc.Delete)
			if _, err := h.pair.ObserveStop(context.Background(), actor, cause, scope); err == nil {
				t.Fatal("public observation reopened stopped services")
			}
		})
	}
}

func TestVariableCallOwnershipSecretFenceBindsOriginalTx(t *testing.T) {
	for _, mode := range []string{"live", "terminal", "wrong-lane", "wrong-store", "foreign-tx", "missing-lock", "forged-marker"} {
		t.Run(mode, func(t *testing.T) {
			h := newOwnershipControl(t)
			ctx, owner, finish := h.begin(t, projectCallSecret, mutationCall)
			defer finish()
			request, err := sc.NewProjectVariableWriteRequest(sc.ProjectVariableWriteFields{Actor: h.actor, ProjectID: h.record.Project, VariableID: h.record.Target, Identity: mustOwnershipSecretIdentity(t, h), Kind: sc.Create})
			if err != nil {
				t.Fatal(err)
			}
			authority := h.secretDeps.Writes
			if mode == "wrong-store" {
				authority, err = NewSecretWriteAuthority(&ownershipControlStore{live: make(map[f.Tx][]f.LockRequest)}, h.projects)
				if err != nil {
					t.Fatal(err)
				}
			}
			if mode == "terminal" {
				h.mu.Lock()
				r := h.records[owner.record.ID.String()]
				r.Phase = "business_closed"
				h.records[r.ID.String()] = r
				h.mu.Unlock()
			}
			if mode == "wrong-lane" {
				h.pair.state.mu.Lock()
				owner.record.Lane = projectCallOrdinary
				h.pair.state.mu.Unlock()
				defer func() { owner.record.Lane = projectCallSecret }()
			}
			if mode == "forged-marker" {
				ctx = context.WithValue(ctx, projectCallContextKey{}, true)
			}
			cause, _ := f.NewRecoveryCause("owned-fence-control", "one", h.record.Project.String())
			result := h.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
				if mode != "missing-lock" {
					if err := h.store.AcquireAll(ctx, tx, secretRequestBaseLocks(request)); err != nil {
						return err
					}
				}
				if mode == "foreign-tx" {
					tx = f.NewTx()
				}
				_, err := authority.current(ctx, tx, request, i.Read)
				return err
			})
			if (result.State() == f.Committed) != (mode == "live") {
				t.Fatal("private original transaction gate", mode, result.State())
			}
			// A caller with no private marker remains the existing authority protocol;
			// it does not receive or synthesize a registered-call capability.
			if mode == "live" {
				result = h.store.WithinTx(context.Background(), cause, func(ctx context.Context, tx f.Tx) error {
					if err := h.store.AcquireAll(ctx, tx, secretRequestBaseLocks(request)); err != nil {
						return err
					}
					_, err := authority.current(ctx, tx, request, i.Read)
					return err
				})
				if result.State() != f.Committed {
					t.Fatal("legacy authority acquired new service dependency")
				}
			}
		})
	}
}
func mustOwnershipSecretIdentity(t *testing.T, h *ownershipControl) f.CommandIdentity {
	t.Helper()
	id, err := c.SecretVariableCommandIdentity(h.record.Project, c.SecretCreateCommand, "owned-key")
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestVariableCallOwnershipConfirmationKeepsParent(t *testing.T) {
	for _, lane := range []projectCallLane{projectCallOrdinary, projectCallSecret} {
		t.Run(string(lane), func(t *testing.T) {
			h := newOwnershipControl(t)
			ctx, owner, finish := h.begin(t, lane, mutationCall)
			_, _, preparedD04, request, original := secretConfirmationFixture(t)
			if lane == projectCallOrdinary {
				identity, _ := c.VariableCommandIdentity(h.record.Project, h.record.Command, h.record.Key)
				original = f.UnknownResult(testID[f.TransactionAttempt](80), commandCause(identity))
			}
			entered, release := make(chan context.Context, 1), make(chan struct{})
			unblock := ownershipRelease(release)
			h.store.before = func(ctx context.Context, cause f.TransactionCause) *f.CommitResult {
				if cause.Kind() != f.CommandsCause {
					return nil
				}
				entered <- ctx
				<-release
				r := f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted))
				return &r
			}
			returned := make(chan error, 1)
			go func() {
				var err error
				if lane == projectCallSecret {
					_, err = h.pair.SecretService().confirmSecretUnknown(ctx, owner.call, request, preparedD04.prepared, original)
				} else {
					q, _ := c.NewVariableCommandLookupRequest(c.VariableCommandLookupFields{ProjectID: h.record.Project, Command: h.record.Command, IdempotencyKey: h.record.Key, SemanticDigest: h.record.Semantic})
					_, err = h.pair.Service().confirmUnknown(ctx, owner.call, h.actor, q, original)
				}
				finish()
				returned <- err
			}()
			joined := false
			t.Cleanup(func() {
				unblock()
				if !joined {
					ownershipWait(t, returned)
				}
				h.store.before = nil
			})
			confirmation := ownershipWait(t, entered)
			w, ok := confirmation.Value(projectCallContextKey{}).(projectCallWitness)
			if !ok || w.owner != owner || w.state != h.pair.state {
				t.Fatal("confirmation lost original private call")
			}
			h.pair.state.mu.Lock()
			early := owner.returned
			h.pair.state.mu.Unlock()
			if early {
				t.Fatal("confirmation manufactured returned proof")
			}
			h.pair.Stop()
			if confirmation.Err() != context.Canceled || ctx.Err() != context.Canceled || h.pair.Joined() {
				t.Fatal("confirmation escaped original Stop")
			}
			bounded, cancel := context.WithCancel(context.Background())
			cancel()
			if err := h.pair.Drain(bounded); !errors.Is(err, context.Canceled) {
				t.Fatal("Drain overtook confirmation")
			}
			unblock()
			err := ownershipWait(t, returned)
			joined = true
			requireOriginalSecretUnknown(t, err, original)
			if n, closed, _ := h.counts(); n != 1 || closed != 1 || !h.pair.Joined() {
				t.Fatal("confirmation got a second owner or skipped retirement")
			}
			h.store.before = nil
		})
	}
}

type ownershipPrepared struct {
	sc.PreparedProjectVariableWrite
	destroy func()
}

func (*ownershipPrepared) Preparation() (sc.ProjectVariablePreparation, error) {
	return sc.ProjectVariablePreparation{}, fault(f.Forbidden)
}
func (p *ownershipPrepared) Destroy() { p.destroy() }

type ownershipD04 struct {
	sc.ProjectVariableWrites
	prepare func(context.Context, sc.ProjectVariableIntent, sc.ProjectVariableWritePlan) (sc.PreparedProjectVariableWrite, error)
}

func (p *ownershipD04) PrepareProjectVariableWrite(ctx context.Context, in sc.ProjectVariableIntent, plan sc.ProjectVariableWritePlan) (sc.PreparedProjectVariableWrite, error) {
	return p.prepare(ctx, in, plan)
}

func TestVariableCallOwnershipSecretPrepareAndDestroyReturnBeforeRetirement(t *testing.T) {
	h := newOwnershipControl(t)
	prepareEntered, prepareRelease := make(chan context.Context, 1), make(chan struct{})
	destroyEntered, destroyRelease := make(chan struct{}), make(chan struct{})
	releasePrepare, releaseDestroy := ownershipRelease(prepareRelease), ownershipRelease(destroyRelease)
	prepared := &ownershipPrepared{destroy: func() { close(destroyEntered); <-destroyRelease }}
	provider := &ownershipD04{prepare: func(ctx context.Context, _ sc.ProjectVariableIntent, plan sc.ProjectVariableWritePlan) (sc.PreparedProjectVariableWrite, error) {
		if _, err := plan.Basis(); err != nil {
			return nil, err
		}
		prepareEntered <- ctx
		<-prepareRelease
		return prepared, nil
	}}
	// Replace only the declared D04 consumer port before any call. It supplies an
	// explicitly controlled invalid preparation and real synchronous Destroy tail;
	// this is not a D04 capability, nonce reservation or authenticated write.
	h.pair.SecretService().state().deps.Secrets = provider
	material, err := sc.NewSecretMaterial([]byte("not-persisted-canary"))
	if err != nil {
		t.Fatal(err)
	}
	defer material.Destroy()
	create, err := c.NewSecretVariableCreate(c.SecretVariableCreateFields{ID: h.record.Target, Name: "OWNED", Value: material})
	if err != nil {
		t.Fatal(err)
	}
	defer create.Destroy()
	meta := f.CommandMeta{RequestID: testID[f.Request](74), IdempotencyKey: "destroy-key"}
	if err := c.ValidateSecretCommandMeta(c.SecretCreateCommand, meta); err != nil {
		t.Fatal("invalid Prepare/Destroy test metadata", err)
	}
	returned := make(chan error, 1)
	go func() {
		_, err := h.pair.SecretService().CreateSecretVariable(context.Background(), h.actor, meta, h.record.Project, create)
		returned <- err
	}()
	joined := false
	t.Cleanup(func() {
		releasePrepare()
		releaseDestroy()
		if !joined {
			ownershipWait(t, returned)
		}
	})
	var original context.Context
	select {
	case original = <-prepareEntered:
	case err = <-returned:
		joined = true
		t.Fatal("original Secret create returned before Prepare entry", err)
	case <-time.After(5 * time.Second):
		t.Fatal("original Secret create did not reach Prepare entry")
	}
	w := original.Value(projectCallContextKey{}).(projectCallWitness)
	check := func() {
		h.pair.state.mu.Lock()
		early := w.owner.returned
		h.pair.state.mu.Unlock()
		if early || h.pair.Joined() {
			t.Fatal("provider tail released the owner")
		}
		if _, closed, _ := h.counts(); closed != 0 {
			t.Fatal("checkpoint ran before Destroy return")
		}
	}
	check()
	releasePrepare()
	ownershipWait(t, destroyEntered)
	check()
	h.pair.Stop()
	if original.Err() != context.Canceled {
		t.Fatal("Stop missed Prepare context")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := h.pair.Drain(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("Drain overtook Destroy")
	}
	releaseDestroy()
	err = ownershipWait(t, returned)
	joined = true
	code(t, err, f.Forbidden)
	if !h.pair.Joined() {
		t.Fatal("actual Destroy return did not allow private checkpoint")
	}
}

func TestVariableCallOwnershipObservationIncludesLocalUnknown(t *testing.T) {
	for _, action := range []pc.LifecycleAction{pc.Archive, pc.Delete} {
		t.Run(string(action), func(t *testing.T) {
			h := newOwnershipControl(t)
			actor, cause, scope := h.lifecycle(t, action)
			h.store.before = func(_ context.Context, cause f.TransactionCause) *f.CommitResult {
				if ownershipCause(cause) == "projectvariable-call-register" {
					r := f.UnknownResult(testID[f.TransactionAttempt](95), cause)
					return &r
				}
				return nil
			}
			finishes := []func(){}
			for _, lane := range []projectCallLane{projectCallOrdinary, projectCallSecret} {
				begin := h.pair.Service().beginProject
				kind := mutationCall
				if lane == projectCallSecret {
					begin = h.pair.SecretService().beginProject
					kind = readCall
				}
				ctx, entry, done, err := begin(context.Background(), h.record.Project, kind)
				if err != nil {
					t.Fatal(err)
				}
				ctx, err = h.pair.state.admit(ctx, entry, h.actor, lane)
				code(t, err, f.CommitUnknown)
				var once sync.Once
				finish := func() { once.Do(func() { h.pair.state.finish(ctx); done() }) }
				t.Cleanup(finish)
				finishes = append(finishes, finish)
			}
			// A newly admitted call before registration has no CallID yet. It still
			// prevents the same observation from publishing a joined local set.
			_, _, localDone, err := h.pair.Service().beginProject(context.Background(), h.record.Project, mutationCall)
			if err != nil {
				t.Fatal(err)
			}
			defer localDone()
			_, _, otherDone, err := h.pair.SecretService().beginProject(context.Background(), testID[i.Project](96), mutationCall)
			if err != nil {
				t.Fatal(err)
			}
			defer otherDone()
			report, err := h.pair.ObserveStop(context.Background(), actor, cause, scope)
			if err != nil {
				t.Fatal(err)
			}
			want := 2
			if action == pc.Delete {
				want = 3
			}
			if !report.Valid() || !report.Matches(cause, scope) || report.Details().PendingCalls != want || report.Details().RegisteredProjectBusinessJoined {
				t.Fatal("empty SQL set hid local registration/Unknown", report.Details())
			}
			if n, closed, reads := h.counts(); n != 0 || closed != 0 || reads != 0 {
				t.Fatal("registration Unknown ran business")
			}
			for _, finish := range finishes {
				finish()
			}
			localDone()
			h.store.before = nil
			report, err = h.pair.ObserveStop(context.Background(), actor, cause, scope)
			if err != nil || !report.Details().RegisteredProjectBusinessJoined || report.Details().PendingCalls != 0 {
				t.Fatal("confirmed original absence not settled", err)
			}
			if h.pair.Joined() {
				t.Fatal("project-business report became global join for another Project")
			}
			var zero ProjectBusinessStopReport
			if zero.Valid() || zero.Matches(cause, scope) {
				t.Fatal("zero report is authority")
			}
			raw, _ := json.Marshal(report)
			if strings.Contains(string(raw), scope.ProjectID.String()) {
				t.Fatal("default report serialization leaked scoped data")
			}
		})
	}
}

func TestVariableCallOwnershipForeignProofAndBoundedProgress(t *testing.T) {
	h := newOwnershipControl(t)
	actor, cause, scope := h.lifecycle(t, pc.Archive)
	for n := 0; n < 101; n++ {
		r := projectCallRecord{ID: testID[projectCallIdentity](1000 + n), Project: scope.ProjectID, Process: testID[oc.Process](97), Instance: testID[projectCallInstance](98), Lane: projectCallSecret, Class: "mutation", Phase: "running"}
		h.records[r.ID.String()] = r
	}
	// A controlled busy process result must not be treated as proof.
	report, err := h.pair.ObserveStop(context.Background(), actor, cause, scope)
	if err != nil || !report.Details().More || report.Details().PendingCalls != 101 || report.Details().RegisteredProjectBusinessJoined {
		t.Fatal("unproven foreign page", err)
	}
	if _, closed, _ := h.counts(); closed != 0 {
		t.Fatal("foreign Busy closed metadata")
	}
	proofs := 0
	h.proof.death = func(ctx context.Context, p oc.ProcessID) error {
		h.store.mu.Lock()
		live := len(h.store.live)
		h.store.mu.Unlock()
		if live != 0 || p != testID[oc.Process](97) {
			return fault(f.Forbidden)
		}
		proofs++
		return ctx.Err()
	}
	entered, release := make(chan struct{}), make(chan struct{})
	unblock := ownershipRelease(release)
	calls := 0
	h.store.after = func(_ context.Context, c f.TransactionCause, result f.CommitResult) f.CommitResult {
		if ownershipCause(c) == "projectvariable-call-observe" {
			calls++
			if calls == 2 {
				close(entered)
				<-release
			}
		}
		return result
	}
	type observed struct {
		report ProjectBusinessStopReport
		err    error
	}
	done := make(chan observed, 1)
	go func() { r, e := h.pair.ObserveStop(context.Background(), actor, cause, scope); done <- observed{r, e} }()
	joined := false
	t.Cleanup(func() {
		unblock()
		if !joined {
			ownershipWait(t, done)
		}
		h.store.after = nil
	})
	ownershipWait(t, entered)
	if _, closed, _ := h.counts(); closed != 100 || h.pair.Joined() {
		t.Fatal("bounded candidates failed to progress or return gate was skipped")
	}
	select {
	case <-done:
		joined = true
		t.Fatal("callback published report before original final Tx return")
	default:
	}
	unblock()
	got := ownershipWait(t, done)
	joined = true
	h.store.after = nil
	if got.err != nil || got.report.Details().PendingCalls != 1 || got.report.Details().RegisteredProjectBusinessJoined {
		t.Fatal("first 100 did not form finite progress", got.err)
	}
	report, err = h.pair.ObserveStop(context.Background(), actor, cause, scope)
	if err != nil || !report.Details().RegisteredProjectBusinessJoined || proofs != 101 {
		t.Fatal("remaining original page did not converge", err)
	}
	if _, closed, _ := h.counts(); closed != 101 {
		t.Fatal("wrong terminal count")
	}
	// This establishes only the private observer's decisions with controlled
	// process/Store ports. It proves no real process death, SQL COMMIT cut, or
	// completion of D04 nonce/provider work; real PG owns those boundaries.
}

func TestVariableCallOwnershipAbsenceAndCapacity(t *testing.T) {
	for _, mode := range []string{"known-missing", "unknown-missing", "capacity"} {
		t.Run(mode, func(t *testing.T) {
			h := newOwnershipControl(t)
			if mode == "unknown-missing" {
				h.store.before = func(_ context.Context, cause f.TransactionCause) *f.CommitResult {
					if ownershipCause(cause) == "projectvariable-call-register" {
						r := f.UnknownResult(testID[f.TransactionAttempt](99), cause)
						return &r
					}
					return nil
				}
				err := h.lookup(context.Background(), projectCallSecret)
				code(t, err, f.CommitUnknown)
				if !h.pair.Joined() {
					t.Fatal("returned Unknown absence did not settle under EX")
				}
				h.store.before = nil
				return
			}
			if mode == "known-missing" {
				_, owner, finish := h.begin(t, projectCallOrdinary, readCall)
				h.mu.Lock()
				saved := h.records[owner.record.ID.String()]
				delete(h.records, owner.record.ID.String())
				h.mu.Unlock()
				finish()
				if h.pair.Joined() {
					t.Fatal("known committed missing row was treated as rollback")
				}
				h.pair.state.mu.Lock()
				err := owner.err
				h.pair.state.mu.Unlock()
				code(t, err, f.InvalidState)
				h.mu.Lock()
				h.records[saved.ID.String()] = saved
				h.mu.Unlock()
				return
			}
			finishes := make([]func(), 0, maxProjectCallOwners)
			for n := 0; n < maxProjectCallOwners; n++ {
				lane := projectCallOrdinary
				if n%2 != 0 {
					lane = projectCallSecret
				}
				_, _, finish := h.begin(t, lane, readCall)
				finishes = append(finishes, finish)
			}
			ctx, entry, done, err := h.pair.Service().beginProject(context.Background(), h.record.Project, readCall)
			if err != nil {
				t.Fatal(err)
			}
			defer done()
			_, err = h.pair.state.admit(ctx, entry, h.actor, projectCallOrdinary)
			code(t, err, f.ResourceBusy)
			if n, _, _ := h.counts(); n != maxProjectCallOwners {
				t.Fatal("overflow was durably admitted")
			}
			done()
			for _, finish := range finishes {
				finish()
			}
			if !h.pair.Joined() {
				t.Fatal("all original accepted calls did not retire")
			}
		})
	}
}
