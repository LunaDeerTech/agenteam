package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// These controls use the original Agent entry points and private ownership
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
		return executionConfigurationRow{err}
	}
	if x.store.row == nil {
		return executionConfigurationRow{pgx.ErrNoRows}
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
	if !actor.Equal(p.actor) || project != p.project || intent != i.Read {
		return pc.ProjectAccess{}, fault(f.Forbidden)
	}
	if err := p.store.RequireHeldLocks(ctx, tx, []f.LockRequest{userLock(actor, f.Shared), projectLock(project, f.Shared)}); err != nil {
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

func ownershipControlDependencies(t *testing.T, store *ownershipControlStore, projects *ownershipControlProjects) Dependencies {
	t.Helper()
	authority, err := NewAuthority(store, projects)
	if err != nil {
		t.Fatal(err)
	}
	events, err := c.RegisterAgentEvents(ec.NewCatalog())
	if err != nil {
		t.Fatal(err)
	}
	eventAuthority, err := NewEventAuthority(authority, events)
	if err != nil {
		t.Fatal(err)
	}
	return Dependencies{
		Authority: authority, EventAuthority: eventAuthority,
		Events: struct{ ob.Appender }{}, Audit: struct{ ac.Appender }{}, Activity: struct{ ActivityAuthority }{},
		Models: struct{ mc.AgentReferences }{}, Tools: struct{ c.ToolConfigurationDirectory }{}, ToolReferences: struct{ c.AgentToolReferences }{},
		Mounts: agentStopMounts{}, Secrets: struct{ vc.SecretDirectory }{}, SecretReferences: struct{ vc.SecretReferences }{},
		Skills: struct{ c.AgentSkillsInitializer }{}, RunnerCandidates: agentStopRunnerDirectory{}, RunnerCursorKeys: directoryKeys(t),
	}
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
	in                    commandInput
	deps                  Dependencies
	mu                    sync.Mutex
	records               map[string]projectCallRecord
	inserts, joins, reads int
}

func newOwnershipControl(t *testing.T) *ownershipControl {
	t.Helper()
	in, _ := planFixture(t)
	_, _, grant := executionConfigurationFixture(t)
	store := &ownershipControlStore{live: make(map[f.Tx][]f.LockRequest)}
	projects := &ownershipControlProjects{store: store, actor: in.actor, project: in.project, grant: grant}
	proof := &ownershipControlProcess{store: store, process: commandID[oc.Process](t, "01900000-0000-7000-8000-000000000071")}
	h := &ownershipControl{t: t, store: store, proof: proof, projects: projects, in: in, records: make(map[string]projectCallRecord)}
	h.deps = ownershipControlDependencies(t, store, projects)
	var err error
	h.pair, err = newProjectCallOwnership(store, h.deps, directoryKeys(t), proof, proof.process)
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
	if strings.Contains(query, "FROM agenteam_agent.project_calls WHERE id=$1") {
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
	insert := strings.HasPrefix(query, "INSERT INTO agenteam_agent.project_calls")
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
	if !strings.HasPrefix(query, "UPDATE agenteam_agent.project_calls SET phase='joined'") {
		return pgconn.CommandTag{}, errors.New("unexpected mutation")
	}
	old, exists := h.records[id.String()]
	if !exists || old != r {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	r.Phase = "joined"
	h.records[id.String()] = r
	h.joins++
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (h *ownershipControl) counts() (int, int, int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.inserts, h.joins, h.reads
}

func (h *ownershipControl) lookup(ctx context.Context) (c.AgentCommandLookup, error) {
	return h.pair.Service().LookupAgentCommand(ctx, h.in.actor, h.in.project, h.in.name, h.in.meta.IdempotencyKey, h.in.semantic)
}

func ownershipCause(cause f.TransactionCause) string { return cause.Details().Owner }

func TestAgentCallOwnershipIsExplicitAndKeepsDefaultEntries(t *testing.T) {
	h := newOwnershipControl(t)
	if h.pair.Service().state.ownership != h.pair.state || h.pair.DirectoryReader().state.ownership != h.pair.state || h.pair.LocalStopper() == nil {
		t.Fatal("factory did not publish one fixed pair")
	}
	if len(h.store.causes) != 0 {
		t.Fatal("factory performed I/O")
	}
	plain, err := New(h.store, h.deps)
	if err != nil {
		t.Fatal(err)
	}
	directory, err := NewDirectoryReader(h.store, h.deps.Authority, directoryKeys(t))
	if err != nil {
		t.Fatal(err)
	}
	if plain.state.ownership != nil || directory.state.ownership != nil {
		t.Fatal("default constructors opted into ownership")
	}
	out, err := plain.LookupAgentCommand(context.Background(), h.in.actor, h.in.project, h.in.name, h.in.meta.IdempotencyKey, h.in.semantic)
	if err != nil || out.Status() != c.AgentLookupNotObserved {
		t.Fatal("default read required ownership", err)
	}
	_, _ = directory.GetAgent(context.Background(), h.in.actor, h.in.project, h.in.target)
	if inserted, joined, reads := h.counts(); inserted != 0 || joined != 0 || reads != 2 {
		t.Fatal("default entries touched ledger", inserted, joined, reads)
	}
	plain.Stop()
	directory.Stop()
	if !plain.Joined() || !directory.Joined() {
		t.Fatal("default owner no longer joins")
	}
	if pair, e := NewProjectCallOwnership(h.store, h.deps, directoryKeys(t), nil, h.proof.process); e == nil || pair != nil {
		t.Fatal("public factory accepted absent concrete guard")
	}
	bound, e := NewProjectCallOwnership(h.store, h.deps, directoryKeys(t), &object.ProcessGuard{}, h.proof.process)
	if e != nil {
		t.Fatal("pre-binding construction performed readiness check", e)
	}
	_, e = bound.Service().LookupAgentCommand(context.Background(), h.in.actor, h.in.project, h.in.name, h.in.meta.IdempotencyKey, h.in.semantic)
	requireCode(t, e, f.DependencyUnbound)
	bound.Stop()
	if e = bound.Drain(context.Background()); e != nil || !bound.Joined() {
		t.Fatal("failed unbound admission leaked", e)
	}
	if inserted, joined, reads := h.counts(); inserted != 0 || joined != 0 || reads != 2 {
		t.Fatal("unbound real guard reached ledger/business")
	}
}

func TestAgentCallOwnershipRegistrationRequiresActualCommit(t *testing.T) {
	for _, mode := range []string{"rollback", "unknown", "guard-refusal", "held-committed-return"} {
		t.Run(mode, func(t *testing.T) {
			h := newOwnershipControl(t)
			attempt := commandID[f.TransactionAttempt](t, "01900000-0000-7000-8000-000000000072")
			if mode == "rollback" {
				h.store.before = func(_ context.Context, cause f.TransactionCause) *f.CommitResult {
					if ownershipCause(cause) == "agent-call-register" {
						r := f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted))
						return &r
					}
					return nil
				}
			}
			if mode == "guard-refusal" {
				h.proof.check = func(context.Context, f.Tx, oc.ProcessID) error { return fault(f.Forbidden) }
			}
			if mode == "unknown" {
				h.store.after = func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
					return f.UnknownResult(attempt, cause)
				}
			}
			if mode != "held-committed-return" {
				_, err := h.lookup(context.Background())
				if err == nil {
					t.Fatal("unconfirmed registration entered business")
				}
				if _, _, reads := h.counts(); reads != 0 {
					t.Fatal("unconfirmed registration ran business SQL")
				}
				if mode == "unknown" {
					r, ok := UnknownAttempt(err)
					if !ok || r.AttemptID() != attempt || ownershipCause(r.Cause()) != "agent-call-register" {
						t.Fatal("checkpoint replaced original registration Unknown", err)
					}
					h.pair.Stop()
					if h.pair.Joined() {
						t.Fatal("unconfirmed accounting joined")
					}
				}
				h.store.before = nil
				h.store.after = nil
				h.proof.check = nil
				return
			}
			entered, release := make(chan struct{}), make(chan struct{})
			unblock := agentStopRelease(release)
			h.store.after = func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
				if ownershipCause(cause) == "agent-call-register" {
					close(entered)
					<-release
				}
				return result
			}
			done := make(chan error, 1)
			go func() { _, err := h.lookup(context.Background()); done <- err }()
			returned := false
			t.Cleanup(func() {
				unblock()
				if !returned {
					agentStopWait(t, done)
				}
			})
			agentStopWait(t, entered)
			if inserted, _, reads := h.counts(); inserted != 1 || reads != 0 {
				t.Fatal("callback acted as committed return")
			}
			h.pair.Stop()
			short, cancel := context.WithCancel(context.Background())
			cancel()
			if !errors.Is(h.pair.Drain(short), context.Canceled) || h.pair.Joined() {
				t.Fatal("Stop/Drain overtook original registration")
			}
			unblock()
			err := agentStopWait(t, done)
			returned = true
			if err == nil {
				t.Fatal("canceled admission published business")
			}
			if _, _, reads := h.counts(); reads != 0 {
				t.Fatal("canceled delivery entered business")
			}
		})
	}
}

func TestAgentCallOwnershipRetirementUnknownPreservesBusinessAndDrain(t *testing.T) {
	h := newOwnershipControl(t)
	attempt := commandID[f.TransactionAttempt](t, "01900000-0000-7000-8000-000000000073")
	h.store.after = func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
		if ownershipCause(cause) == "agent-call-return" && result.State() == f.Committed {
			return f.UnknownResult(attempt, cause)
		}
		return result
	}
	out, err := h.lookup(context.Background())
	if err != nil || out.Status() != c.AgentLookupNotObserved {
		t.Fatal("retirement rewrote actual known business result", err)
	}
	if inserted, joined, reads := h.counts(); inserted != 1 || joined != 1 || reads != 1 {
		t.Fatal("wrong original flow", inserted, joined, reads)
	}
	h.pair.Stop()
	if h.pair.Joined() || h.pair.Service().Joined() {
		t.Fatal("lost retirement accounting")
	}
	_, err = h.lookup(context.Background())
	requireCode(t, err, f.ShuttingDown)
	if err = h.pair.Drain(context.Background()); err == nil {
		t.Fatal("unconfirmed checkpoint falsely joined")
	}
	if h.pair.Joined() {
		t.Fatal("failed Drain dropped original owner")
	}
	h.store.after = nil
	if err = h.pair.Drain(context.Background()); err != nil || !h.pair.Joined() {
		t.Fatal("retained-only post-Stop settlement failed", err)
	}
	if inserted, joined, reads := h.counts(); inserted != 1 || joined != 1 || reads != 1 {
		t.Fatal("settlement recreated admission/business", inserted, joined, reads)
	}
	t.Run("known-registration-missing-is-not-joined", func(t *testing.T) {
		x := newOwnershipControl(t)
		var original projectCallRecord
		x.store.after = func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
			if cause.Details().Kind == f.CommandsCause && result.State() == f.Committed {
				x.mu.Lock()
				for key, r := range x.records {
					original = r
					delete(x.records, key)
				}
				x.mu.Unlock()
			}
			return result
		}
		out, err := x.lookup(context.Background())
		if err != nil || out.Status() != c.AgentLookupNotObserved {
			t.Fatal("missing retirement row replaced known business result", err)
		}
		x.pair.Stop()
		if x.pair.Joined() {
			t.Fatal("missing known registration was retired")
		}
		err = x.pair.Drain(context.Background())
		requireCode(t, err, f.InvalidState)
		x.store.after = nil
		x.mu.Lock()
		x.records[original.ID.String()] = original
		x.mu.Unlock()
	})
	t.Run("unknown-registration-absence-needs-terminal-cut", func(t *testing.T) {
		x := newOwnershipControl(t)
		x.store.after = func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
			if ownershipCause(cause) == "agent-call-register" {
				x.mu.Lock()
				clear(x.records)
				x.mu.Unlock()
				return f.UnknownResult(attempt, cause)
			}
			return result
		}
		cuts := 0
		x.proof.check = func(ctx context.Context, tx f.Tx, _ oc.ProcessID) error {
			if x.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(x.in.project, f.Exclusive)}) == nil {
				cuts++
			}
			return nil
		}
		_, err := x.lookup(context.Background())
		if _, ok := UnknownAttempt(err); !ok {
			t.Fatal("registration outcome replaced", err)
		}
		x.pair.Stop()
		if cuts != 1 || !x.pair.Joined() {
			t.Fatal("absence did not use original-return Project EX cut", cuts)
		}
		if _, _, reads := x.counts(); reads != 0 {
			t.Fatal("unknown registration performed business")
		}
	})
	t.Run("held-checkpoint-return-remains-owned", func(t *testing.T) {
		x := newOwnershipControl(t)
		entered, release := make(chan struct{}), make(chan struct{})
		unblock := agentStopRelease(release)
		var held sync.Once
		x.store.after = func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
			if ownershipCause(cause) == "agent-call-return" {
				held.Do(func() {
					close(entered)
					<-release
				})
			}
			return result
		}
		done := make(chan error, 1)
		go func() {
			out, err := x.lookup(context.Background())
			if err == nil && out.Status() != c.AgentLookupNotObserved {
				err = errors.New("known result changed")
			}
			done <- err
		}()
		returned := false
		t.Cleanup(func() {
			unblock()
			if !returned {
				agentStopWait(t, done)
			}
		})
		agentStopWait(t, entered)
		x.pair.Stop()
		short, cancel := context.WithCancel(context.Background())
		cancel()
		if !errors.Is(x.pair.Drain(short), context.Canceled) || x.pair.Joined() {
			t.Fatal("physical checkpoint update was mistaken for original return")
		}
		unblock()
		err := agentStopWait(t, done)
		returned = true
		if err != nil {
			t.Fatal("delivery cancellation replaced known business", err)
		}
	})
}

func TestAgentCallOwnershipFencesOriginalTransactionAndTuple(t *testing.T) {
	for _, mode := range []string{"running", "missing-row", "joined", "foreign-process", "foreign-instance", "missing-project-lock", "foreign-tx", "foreign-witness"} {
		t.Run(mode, func(t *testing.T) {
			h := newOwnershipControl(t)
			ctx, call, done, err := h.pair.Service().beginProject(context.Background(), h.in.project, mutationCall)
			if err != nil {
				t.Fatal(err)
			}
			ctx, err = h.pair.state.admit(ctx, call, h.in.actor, projectCallService)
			if err != nil {
				done()
				t.Fatal(err)
			}
			w := ctx.Value(projectCallContextKey{}).(projectCallWitness)
			original := w.owner.record
			t.Cleanup(func() {
				h.mu.Lock()
				h.records[original.ID.String()] = original
				h.mu.Unlock()
				h.pair.state.finish(ctx)
				done()
			})
			h.mu.Lock()
			r := original
			switch mode {
			case "missing-row":
				delete(h.records, r.ID.String())
			case "joined":
				r.Phase = "joined"
				h.records[r.ID.String()] = r
			case "foreign-process":
				r.Process = commandID[oc.Process](t, "01900000-0000-7000-8000-000000000074")
				h.records[r.ID.String()] = r
			case "foreign-instance":
				r.Instance = commandID[projectCallInstance](t, "01900000-0000-7000-8000-000000000074")
				h.records[r.ID.String()] = r
			}
			h.mu.Unlock()
			cause, _ := f.NewRecoveryCause("ownership-control", h.in.project.String(), "")
			reached := false
			result := h.store.WithinTx(ctx, cause, func(inner context.Context, tx f.Tx) error {
				locks := []f.LockRequest{projectLock(h.in.project, f.Shared)}
				if mode == "missing-project-lock" {
					locks = nil
				}
				if err := h.store.AcquireAll(inner, tx, locks); err != nil {
					return err
				}
				if mode == "foreign-tx" {
					tx = f.NewTx()
				}
				if mode == "foreign-witness" {
					inner = context.WithValue(inner, projectCallContextKey{}, projectCallWitness{&projectCallOwnershipState{}, w.owner})
				}
				if err := h.pair.state.requireRunning(inner, tx); err != nil {
					return err
				}
				reached = true
				return nil
			})
			if (result.State() == f.Committed) != (mode == "running") || reached != (mode == "running") {
				t.Fatal("old transaction bypassed exact running tuple", mode, result.State())
			}
		})
	}
}

func TestAgentCallOwnershipTracksEveryReadAndDistinctActualCall(t *testing.T) {
	for _, name := range []string{"configuration", "command-lookup", "available-runners", "runner-mounts", "directory-get", "directory-list"} {
		t.Run(name, func(t *testing.T) {
			h := newOwnershipControl(t)
			business := 0
			h.store.before = func(_ context.Context, cause f.TransactionCause) *f.CommitResult {
				if strings.HasPrefix(ownershipCause(cause), "agent-call-") {
					return nil
				}
				business++
				result := f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted))
				return &result
			}
			for range 2 {
				var err error
				s := h.pair.Service()
				ctx := context.Background()
				switch name {
				case "configuration":
					_, err = s.GetAgent(ctx, h.in.actor, h.in.project, h.in.target)
				case "command-lookup":
					_, err = h.lookup(ctx)
				case "available-runners":
					_, err = s.ListAvailableRunners(ctx, h.in.actor, h.in.project, f.PageRequest{Limit: 1})
				case "runner-mounts":
					_, err = s.GetRunnerMounts(ctx, h.in.actor, h.in.project, h.in.target)
				case "directory-get":
					_, err = h.pair.DirectoryReader().GetAgent(ctx, h.in.actor, h.in.project, h.in.target)
				case "directory-list":
					_, err = h.pair.DirectoryReader().ListAgents(ctx, h.in.actor, h.in.project, f.PageRequest{Limit: 1})
				}
				if err == nil {
					t.Fatal("controlled business failure lost")
				}
			}
			if inserted, joined, _ := h.counts(); inserted != 2 || joined != 2 || business != 2 {
				t.Fatal("actual entries coalesced or omitted", inserted, joined, business)
			}
			lane := projectCallService
			if strings.HasPrefix(name, "directory-") {
				lane = projectCallDirectory
			}
			for _, r := range h.records {
				if r.Class != "read" || r.Lane != lane || r.Project != h.in.project || r.Process != h.proof.process || r.Phase != "joined" {
					t.Fatal("read mapped to wrong ownership")
				}
			}
		})
	}
}

func TestAgentCallOwnershipKeepsConfirmationAndCheckpointUnderParent(t *testing.T) {
	for _, order := range []string{"stop-before-confirmation", "confirmation-before-stop"} {
		t.Run(order, func(t *testing.T) {
			h := newOwnershipControl(t)
			cause, _ := f.NewCommandsCause(h.in.identity)
			attempt := commandID[f.TransactionAttempt](t, "01900000-0000-7000-8000-000000000075")
			original := f.UnknownResult(attempt, cause)
			first, confirm := make(chan struct{}), make(chan struct{})
			firstRelease, confirmRelease := make(chan struct{}), make(chan struct{})
			freeFirst, freeConfirm := agentStopRelease(firstRelease), agentStopRelease(confirmRelease)
			var business atomic.Int32
			h.store.before = func(ctx context.Context, cause f.TransactionCause) *f.CommitResult {
				if strings.HasPrefix(ownershipCause(cause), "agent-call-") {
					return nil
				}
				if business.Add(1) == 1 {
					close(first)
					<-firstRelease
					return &original
				}
				close(confirm)
				<-confirmRelease
				result := f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted).WithCause(ctx.Err()))
				return &result
			}
			done := make(chan error, 1)
			go func() {
				_, err := h.pair.Service().CreateAgent(context.Background(), h.in.actor, h.in.meta, h.in.project, *h.in.create)
				done <- err
			}()
			returned := false
			t.Cleanup(func() {
				freeFirst()
				freeConfirm()
				if !returned {
					agentStopWait(t, done)
				}
			})
			agentStopWait(t, first)
			if order == "confirmation-before-stop" {
				freeFirst()
				agentStopWait(t, confirm)
			}
			h.pair.Stop()
			short, cancel := context.WithCancel(context.Background())
			cancel()
			if !errors.Is(h.pair.Drain(short), context.Canceled) || h.pair.Joined() {
				t.Fatal("stop detached original mutation/confirmation")
			}
			if inserted, joined, _ := h.counts(); inserted != 1 || joined != 0 {
				t.Fatal("confirmation split ownership or retired early")
			}
			freeFirst()
			freeConfirm()
			err := agentStopWait(t, done)
			returned = true
			physical, ok := UnknownAttempt(err)
			if !ok || physical.AttemptID() != attempt || physical.Cause().Details().Primary.Canonical() != h.in.identity.Canonical() {
				t.Fatal("retirement changed actual business Unknown", err)
			}
			want := int32(1)
			if order == "confirmation-before-stop" {
				want = 2
			}
			if business.Load() != want {
				t.Fatal("extra business writer/confirmation")
			}
			if err = h.pair.Drain(context.Background()); err != nil || !h.pair.Joined() {
				t.Fatal("actual parent return did not join", err)
			}
			if inserted, joined, _ := h.counts(); inserted != 1 || joined != 1 {
				t.Fatal("original parent was not the only registration")
			}
		})
	}
}

func (h *ownershipControl) lifecycle(action pc.LifecycleAction) (i.Actor, pc.LifecycleCause, pc.ScopeRef) {
	h.t.Helper()
	cause := pc.LifecycleCause{OperationID: commandID[pc.Operation](h.t, "01900000-0000-7000-8000-000000000079"), Action: action, ProjectVersion: 7}
	scope := pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: h.in.project}
	registration, err := i.RegisterService(i.ProjectLifecycle)
	if err != nil {
		h.t.Fatal(err)
	}
	actorScope, err := i.InProject(h.in.project)
	if err != nil {
		h.t.Fatal(err)
	}
	actor, err := registration.Actor(cause.OperationID.String(), actorScope)
	if err != nil {
		h.t.Fatal(err)
	}
	h.projects.check = func(ctx context.Context, tx f.Tx, actual i.Actor, current pc.LifecycleCause, participant pc.ParticipantName, phase pc.OperationPhase) error {
		if !actual.Equal(actor) || current != cause || participant != pc.SkillsParticipant || phase != pc.StopPhase {
			return fault(f.Forbidden)
		}
		return h.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(scope.ProjectID, f.Shared)})
	}
	return actor, cause, scope
}

func (h *ownershipControl) foreignRecord(n int) projectCallRecord {
	r := projectCallRecord{
		ID:      commandID[projectCallIdentity](h.t, fmt.Sprintf("01900000-0000-7000-8000-%012x", n)),
		Project: h.in.project, Process: commandID[oc.Process](h.t, "01900000-0000-7000-8000-000000000077"),
		Instance: commandID[projectCallInstance](h.t, "01900000-0000-7000-8000-000000000078"),
		Lane:     projectCallService, Class: "mutation", Phase: "running",
	}
	h.mu.Lock()
	h.records[r.ID.String()] = r
	h.mu.Unlock()
	return r
}

func TestAgentCallOwnershipForeignObservationRequiresProofAndTerminalCut(t *testing.T) {
	for _, mode := range []string{"live", "dead", "same-process-unowned", "proof-error", "tuple-changed", "unknown-final", "late-call"} {
		t.Run(mode, func(t *testing.T) {
			h := newOwnershipControl(t)
			actor, cause, scope := h.lifecycle(pc.Delete)
			r := h.foreignRecord(1000)
			if mode == "same-process-unowned" {
				r.Process = h.proof.process
				h.records[r.ID.String()] = r
			}
			proofCalls := 0
			h.proof.death = func(_ context.Context, p oc.ProcessID) error {
				proofCalls++
				h.store.mu.Lock()
				active := len(h.store.live)
				h.store.mu.Unlock()
				if active != 0 || p != r.Process {
					return errors.New("death checked in transaction or for wrong process")
				}
				if mode == "live" {
					return fault(f.ResourceBusy)
				}
				if mode == "proof-error" {
					return fault(f.DependencyUnavailable)
				}
				if mode == "tuple-changed" {
					h.mu.Lock()
					changed := r
					changed.Instance = h.pair.state.instance
					h.records[r.ID.String()] = changed
					h.mu.Unlock()
				}
				if mode == "late-call" {
					h.foreignRecord(1001)
				}
				return nil
			}
			if mode == "unknown-final" {
				h.store.after = func(_ context.Context, current f.TransactionCause, result f.CommitResult) f.CommitResult {
					_, joined, _ := h.counts()
					if ownershipCause(current) == "agent-call-observe" && joined > 0 {
						return f.UnknownResult(commandID[f.TransactionAttempt](t, "01900000-0000-7000-8000-000000000076"), current)
					}
					return result
				}
			}
			report, err := h.pair.ObserveStop(context.Background(), actor, cause, scope)
			if mode == "tuple-changed" || mode == "unknown-final" {
				if err == nil || report.Valid() {
					t.Fatal("unconfirmed or rebound terminal published report")
				}
			} else {
				if err != nil || !report.Matches(cause, scope) {
					t.Fatal("actual observation failed", err)
				}
				if report.Details().RegisteredJoined != (mode == "dead") {
					t.Fatal("foreign absence/liveness was mistaken for proof")
				}
				if mode == "late-call" && report.Details().PendingCalls != 1 {
					t.Fatal("final Project EX snapshot missed new call")
				}
			}
			_, joined, _ := h.counts()
			wantJoined := 0
			if mode == "dead" || mode == "unknown-final" || mode == "late-call" {
				wantJoined = 1
			}
			if joined != wantJoined {
				t.Fatal("terminal mutation lacked exact proof/cut", joined)
			}
			if (proofCalls == 0) != (mode == "same-process-unowned") {
				t.Fatal("same process was treated as foreign death")
			}
			h.store.after = nil
		})
	}
}

func TestAgentCallOwnershipBoundedObservationProgressAndSelection(t *testing.T) {
	t.Run("more-than-one-dead-page", func(t *testing.T) {
		h := newOwnershipControl(t)
		actor, cause, scope := h.lifecycle(pc.Delete)
		for n := 0; n < 201; n++ {
			h.foreignRecord(2000 + n)
		}
		h.proof.death = func(context.Context, oc.ProcessID) error { return nil }
		for pass, want := range []int{100, 200, 201} {
			report, err := h.pair.ObserveStop(context.Background(), actor, cause, scope)
			if err != nil || !report.Valid() {
				t.Fatal("bounded observation failed", err)
			}
			_, joined, _ := h.counts()
			if joined != want || report.Details().RegisteredJoined != (pass == 2) {
				t.Fatal("overflow prevented legal batch progress", joined, pass)
			}
			if pass == 0 && !report.Details().HasMore {
				t.Fatal("101st remaining row lost overflow")
			}
		}
	})
	t.Run("archive-preserves-read-and-other-project", func(t *testing.T) {
		h := newOwnershipControl(t)
		actor, cause, scope := h.lifecycle(pc.Archive)
		h.foreignRecord(3000)
		read := h.foreignRecord(3001)
		read.Lane = projectCallDirectory
		read.Class = "read"
		h.records[read.ID.String()] = read
		other := h.foreignRecord(3002)
		other.Project = commandID[i.Project](t, "01900000-0000-7000-8000-000000000088")
		h.records[other.ID.String()] = other
		h.proof.death = func(context.Context, oc.ProcessID) error { return nil }
		report, err := h.pair.ObserveStop(context.Background(), actor, cause, scope)
		if err != nil || !report.Details().RegisteredJoined {
			t.Fatal("Archive selection failed", err)
		}
		if _, joined, _ := h.counts(); joined != 1 || h.records[read.ID.String()].Phase != "running" || h.records[other.ID.String()].Phase != "running" {
			t.Fatal("Archive crossed class/project")
		}
		actor, cause, scope = h.lifecycle(pc.Delete)
		report, err = h.pair.ObserveStop(context.Background(), actor, cause, scope)
		if err != nil || !report.Details().RegisteredJoined || h.records[read.ID.String()].Phase != "joined" || h.records[other.ID.String()].Phase != "running" {
			t.Fatal("Delete missed directory read or crossed project", err)
		}
	})
}
