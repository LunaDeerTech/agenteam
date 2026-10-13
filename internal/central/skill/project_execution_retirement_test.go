package skill

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// These controls exercise the original decoder, private plan and synchronous
// InTx calls. They are not PostgreSQL rollback, a real Project claim or an
// Execution canonical deletion proof. The cross-owner transaction is PG's job.
type executionRetirementSkillWitness struct{}
type executionRetirementSkillOwner struct {
	tx            f.Tx
	request       ec.ProjectExecutionRetirementRequest
	commit        ec.ProjectExecutionRetirementCommit
	applied, live bool
	calls         int
}

func (o *executionRetirementSkillOwner) check(ctx context.Context, tx f.Tx, r ec.ProjectExecutionRetirementRequest, applied bool) error {
	o.calls++
	if !o.live || tx != o.tx || ctx.Value(executionRetirementSkillWitness{}) != o || applied != o.applied || r.Binding != o.request.Binding || r.Cause != o.request.Cause || !r.Actor.Equal(o.request.Actor) {
		return fault(f.Forbidden)
	}
	return nil
}
func (o *executionRetirementSkillOwner) RequireProjectExecutionRetirementPreparedInTx(ctx context.Context, tx f.Tx, r ec.ProjectExecutionRetirementRequest) error {
	return o.check(ctx, tx, r, false)
}
func (o *executionRetirementSkillOwner) RequireProjectExecutionRetirementAppliedInTx(ctx context.Context, tx f.Tx, r ec.ProjectExecutionRetirementRequest) error {
	return o.check(ctx, tx, r, true)
}
func (o *executionRetirementSkillOwner) ProjectExecutionRetirementProofInTx(ctx context.Context, tx f.Tx, r ec.ProjectExecutionRetirementRequest) (ec.ProjectExecutionRetirementCommit, error) {
	if err := o.check(ctx, tx, r, true); err != nil {
		return ec.ProjectExecutionRetirementCommit{}, err
	}
	return o.commit, nil
}

type executionRetirementSkillStore struct {
	Store
	t                *testing.T
	owner            *executionRetirementSkillOwner
	command          *assignmentCommand
	live, pending    bool
	held             map[string]f.LockMode
	proof            []any
	extraProof       int64
	reads, writes    int
	operations       []string
	fail             string
	failure          error
	wrongCount       bool
	hold             string
	entered, release chan struct{}
	once             sync.Once
}

func (s *executionRetirementSkillStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if !s.live || tx != s.owner.tx {
		return nil, fault(f.Forbidden)
	}
	return s, nil
}
func (s *executionRetirementSkillStore) RequireHeldLocks(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	if !s.live || tx != s.owner.tx {
		return fault(f.Forbidden)
	}
	for _, l := range locks {
		m := s.held[l.Key.Canonical()]
		if m != f.Exclusive && m != l.Mode {
			return fault(f.Forbidden)
		}
	}
	return nil
}
func (s *executionRetirementSkillStore) row(name string, values []any, err error) postgres.Row {
	return retirementRefScan(func(dst ...any) error {
		if s.hold == name {
			s.once.Do(func() { close(s.entered); <-s.release })
		}
		if s.fail == name {
			return s.failure
		}
		if err != nil {
			return err
		}
		if len(dst) != len(values) {
			return fmt.Errorf("%s row width %d/%d", name, len(dst), len(values))
		}
		for n, v := range values {
			reflect.ValueOf(dst[n]).Elem().Set(reflect.ValueOf(v))
		}
		return nil
	})
}
func (s *executionRetirementSkillStore) QueryRow(ctx context.Context, q string, args ...any) postgres.Row {
	s.reads++
	if s.owner.calls == 0 || ctx.Value(executionRetirementSkillWitness{}) != s.owner {
		return s.row("unauthorized", nil, fault(f.Forbidden))
	}
	c := s.command
	switch {
	case strings.HasPrefix(q, "SELECT record FROM agenteam_skill.execution_binding_heads"):
		return s.row("capture", nil, pgx.ErrNoRows)
	case strings.HasPrefix(q, "SELECT count(*) FROM agenteam_skill.execution_bindings"):
		return s.row("bindings", []any{int64(0)}, nil)
	case strings.HasPrefix(q, "SELECT EXISTS(SELECT 1 FROM agenteam_skill.execution_binding_heads"):
		return s.row("remaining", []any{s.pending}, nil)
	case strings.Contains(q, "array_agg(change_id::text") && strings.Contains(q, "assignment_pending_refs"):
		ids := []string{}
		if s.pending {
			ids = append(ids, c.ID.String())
		}
		return s.row("pending-ids", []any{ids}, nil)
	case strings.Contains(q, "FROM agenteam_skill.assignment_pending_refs"):
		if !s.pending {
			return s.row("routing", nil, pgx.ErrNoRows)
		}
		b := c.Candidate.Runtime.Binding
		return s.row("routing", []any{s.owner.request.Binding.ExecutionID.String(), c.Candidate.After.Project.String(), c.Candidate.After.Agent.String(), b.AssignmentID.String(), int64(b.AssignmentSequence), b.SkillID.String(), b.RevisionID.String(), int64(b.Revision), c.Candidate.Revision.ObjectID.String()}, nil)
	case strings.Contains(q, "array_agg(id::text") && strings.Contains(q, "assignment_commands"):
		return s.row("command-ids", []any{[]string{c.ID.String()}}, nil)
	case strings.HasPrefix(q, "SELECT idempotency_key,input,candidate"):
		_, candidate, e := captureDigest(c.Candidate)
		return s.row("by-id", []any{string(c.Input.Meta.IdempotencyKey), c.Input.Raw, candidate}, e)
	case strings.HasPrefix(q, "SELECT id::text,actor_user_id"):
		_, candidate, e := captureDigest(c.Candidate)
		return s.row("command", []any{c.ID.String(), c.Input.Actor.Details().UserID, string(c.Digest), c.Input.Raw, candidate, c.State, c.Receipt, c.RuntimeReceipt, c.Candidate.Created.Time()}, e)
	case strings.HasPrefix(q, "SELECT project_id::text,agent_id::text,assignment_sequence,agent_version"):
		digest, state, e := captureDigest(c.Candidate.After)
		_, runtime, e2 := captureDigest(c.Candidate.Runtime)
		if e == nil {
			e = e2
		}
		return s.row("change", []any{c.Candidate.After.Project.String(), c.Candidate.After.Agent.String(), int64(c.Candidate.After.Sequence), int64(c.Candidate.Version), string(c.Candidate.BeforeDigest), state, string(digest), runtime, c.Candidate.Created.Time()}, e)
	case strings.HasPrefix(q, "SELECT EXISTS(SELECT 1 FROM agenteam_skill.assignment_execution_retirement_proofs"):
		return s.row("has-proof", []any{s.proof != nil || s.extraProof > 0}, nil)
	case strings.HasPrefix(q, "SELECT count(*) FROM agenteam_skill.assignment_execution_retirement_proofs"):
		n := s.extraProof
		if s.proof != nil {
			n++
		}
		return s.row("proof-count", []any{n}, nil)
	case strings.Contains(q, "FROM agenteam_skill.assignment_execution_retirement_proofs WHERE change_id"):
		if s.proof == nil {
			return s.row("proof", nil, pgx.ErrNoRows)
		}
		return s.row("proof", s.proof, nil)
	default:
		return s.row("unexpected", nil, fmt.Errorf("unexpected controlled query %s", q))
	}
}
func (s *executionRetirementSkillStore) Exec(ctx context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	s.writes++
	name := ""
	switch {
	case strings.HasPrefix(q, "DELETE FROM agenteam_skill.assignment_pending_refs"):
		name = "pending-delete"
		data, _, _, e := retirementProofData(s.command)
		if e != nil {
			return pgconn.CommandTag{}, e
		}
		v := data.Pending
		want := []any{v.ChangeID.String(), v.ProjectID.String(), v.AgentID.String(), v.ExecutionID.String(), v.AssignmentID.String(), int64(v.AssignmentSequence), v.SkillID.String(), v.RevisionID.String(), int64(v.Revision), v.ObjectID.String()}
		if !reflect.DeepEqual(args, want) {
			return pgconn.CommandTag{}, errors.New("wrong complete pending deletion tuple")
		}
	case strings.HasPrefix(q, "INSERT INTO agenteam_skill.assignment_execution_retirement_proofs"):
		name = "proof-insert"
		if !s.owner.applied || len(args) != 14 || args[0] != s.command.ID.String() {
			return pgconn.CommandTag{}, fault(f.Forbidden)
		}
	case strings.HasPrefix(q, "DELETE FROM agenteam_skill.assignment_execution_retirement_proofs"):
		name = "proof-delete"
		want := append([]any{s.command.ID.String()}, s.proof...)
		if s.proof == nil || !reflect.DeepEqual(args, want) {
			return pgconn.CommandTag{}, errors.New("wrong complete original proof deletion tuple")
		}
	default:
		return pgconn.CommandTag{}, fmt.Errorf("unexpected controlled write %s", q)
	}
	if ctx.Value(executionRetirementSkillWitness{}) != s.owner {
		return pgconn.CommandTag{}, fault(f.Forbidden)
	}
	s.operations = append(s.operations, name)
	if s.hold == name {
		s.once.Do(func() { close(s.entered); <-s.release })
	}
	if s.fail == name {
		return pgconn.CommandTag{}, s.failure
	}
	if s.wrongCount {
		return pgconn.NewCommandTag("DELETE 2"), nil
	}
	switch name {
	case "pending-delete":
		s.pending = false
	case "proof-insert":
		s.proof = append([]any(nil), args[1:]...)
		s.proof[11] = bytes.Clone(args[12].([]byte))
	case "proof-delete":
		s.proof = nil
	}
	return pgconn.NewCommandTag("DELETE 1"), nil
}

type executionRetirementSkillFixture struct {
	service *ProjectExecutionRetirementReferences
	store   *executionRetirementSkillStore
	ctx     context.Context
	request ec.ProjectExecutionRetirementRequest
}

func newExecutionRetirementSkillFixture(t *testing.T) *executionRetirementSkillFixture {
	t.Helper()
	actor, e := i.NewHuman(stateID[i.User](111), stateID[i.Session](112))
	if e != nil {
		t.Fatal(e)
	}
	command := assignmentChangedControl(t, actor, true)
	// The first enabled assignment follows an actually empty disabled head;
	// it is not a replacement of a fictitious disabled seq-1 grant.
	command.Candidate.Removed = nil
	command.Candidate.BeforeDigest, _, e = captureDigest(assignmentSet{Project: command.Candidate.After.Project, Agent: command.Candidate.After.Agent, Sequence: 1, Items: []sc.AgentSkillAssignment{}})
	if e != nil || command.validate() != nil {
		t.Fatal("invalid first assignment", e)
	}
	completeAssignmentControl(t, command, true)
	route, e := command.runtimeReceipt()
	if e != nil {
		t.Fatal(e)
	}
	reg, e := i.RegisterService(i.ProjectLifecycle)
	if e != nil {
		t.Fatal(e)
	}
	scope, e := i.InProject(command.Candidate.After.Project)
	if e != nil {
		t.Fatal(e)
	}
	operation := stateID[pc.Operation](601)
	caller, e := reg.Actor(operation.String(), scope)
	if e != nil {
		t.Fatal(e)
	}
	r := ec.ProjectExecutionRetirementRequest{Actor: caller, Cause: pc.LifecycleCause{OperationID: operation, Action: pc.Delete, ProjectVersion: 9}, Binding: ec.ProjectExecutionRetirementBinding{ProjectID: command.Candidate.After.Project, AgentID: command.Candidate.After.Agent, ExecutionID: *route.ExecutionID, Version: 1, CanonicalDigest: sum([]byte("full original Execution canonical control"))}}
	if r.Validate() != nil {
		t.Fatal("invalid fixture request")
	}
	o := &executionRetirementSkillOwner{tx: f.NewTx(), request: r, live: true, commit: ec.ProjectExecutionRetirementCommit{ProjectID: r.Binding.ProjectID, OperationID: operation, ProjectVersion: 9, ProcessID: stateID[struct{}](602).String(), AttemptID: stateID[struct{}](603).String(), Fence: 3, TransactionID: "501"}}
	if o.commit.Validate() != nil {
		t.Fatal("invalid original commit")
	}
	s := &executionRetirementSkillStore{t: t, owner: o, command: command, live: true, pending: true, held: map[string]f.LockMode{}, failure: errors.New("original Skill SQL return")}
	for _, l := range skillExecutionRetirementLocks(r) {
		s.held[l.Key.Canonical()] = l.Mode
	}
	l := skillLock(command.Candidate.Skill, f.Shared)
	s.held[l.Key.Canonical()] = l.Mode
	service, e := NewProjectExecutionRetirementReferences(s, o)
	if e != nil {
		t.Fatal(e)
	}
	return &executionRetirementSkillFixture{service, s, context.WithValue(context.Background(), executionRetirementSkillWitness{}, o), r}
}
func (v *executionRetirementSkillFixture) discover(t *testing.T) ec.ProjectExecutionRetirementReferencePlan {
	t.Helper()
	p, e := v.service.DiscoverProjectExecutionReferencesInTx(v.ctx, v.store.owner.tx, v.request)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func (v *executionRetirementSkillFixture) retire(t *testing.T) ec.ProjectExecutionRetirementReferencePlan {
	t.Helper()
	p := v.discover(t)
	if e := v.service.RetireProjectExecutionReferencesInTx(v.ctx, v.store.owner.tx, v.request, p); e != nil {
		t.Fatal(e)
	}
	v.store.owner.applied = true
	if e := v.service.FinalizeProjectExecutionRetirementInTx(v.ctx, v.store.owner.tx, v.request, p); e != nil {
		t.Fatal(e)
	}
	return p
}
func (v *executionRetirementSkillFixture) agentChange() ac.ProjectRetirementChange {
	return ac.ProjectRetirementChange{Request: ac.ProjectRetirementRequest{Actor: v.request.Actor, Cause: v.request.Cause, ProjectID: v.request.Binding.ProjectID, AgentID: v.request.Binding.AgentID, ExpectedVersion: v.store.command.Candidate.Version}}
}

func TestSkillExecutionRetirementQueuedProofAndHistory(t *testing.T) {
	v := newExecutionRetirementSkillFixture(t)
	original := bytes.Clone(v.store.command.RuntimeReceipt)
	p := v.retire(t)
	if !reflect.DeepEqual(v.store.operations, []string{"pending-delete", "proof-insert"}) || v.store.pending || !bytes.Equal(original, v.store.command.RuntimeReceipt) {
		t.Fatal("original queued decision or mutation order changed")
	}
	before := v.store.writes
	if e := v.service.RequireProjectExecutionReferencesRetiredInTx(v.ctx, v.store.owner.tx, v.request, p); e != nil || v.store.writes != before {
		t.Fatal("Applied confirmation must be read-only", e)
	}
	if _, e := loadAssignmentByID(v.ctx, v.store, v.request.Binding.ProjectID, v.request.Binding.AgentID, v.store.command.ID); e == nil {
		t.Fatal("ordinary routing accepted retired pending reference")
	}
	c, proof, e := loadAssignmentRetirementCommand(v.ctx, v.store, v.agentChange(), v.store.command.ID)
	if e != nil || proof == nil || !bytes.Equal(c.RuntimeReceipt, original) {
		t.Fatal("private retirement reader lost original queued receipt", e)
	}
	v.store.owner.applied = false
	if e = v.service.RequireProjectExecutionReferencesRetiredInTx(v.ctx, v.store.owner.tx, v.request, p); !retirementRefCode(e, f.Forbidden) {
		t.Fatal("Prepared cannot confirm Applied proof", e)
	}
}

func TestSkillExecutionRetirementOwnerAndPlan(t *testing.T) {
	for _, mode := range []string{"foreign-tx", "expired", "missing-witness", "agent-sh", "other-request", "foreign-plan", "prepared-finalize", "applied-retire"} {
		t.Run(mode, func(t *testing.T) {
			v := newExecutionRetirementSkillFixture(t)
			p := v.discover(t)
			tx := v.store.owner.tx
			r := v.request
			ctx := v.ctx
			final := false
			switch mode {
			case "foreign-tx":
				tx = f.NewTx()
			case "expired":
				v.store.live = false
			case "missing-witness":
				ctx = context.Background()
			case "agent-sh":
				k, _ := f.AgentLock(r.Binding.AgentID.String())
				v.store.held[k.Canonical()] = f.Shared
			case "other-request":
				r.Binding.Version++
			case "foreign-plan":
				p = newExecutionRetirementSkillFixture(t).discover(t)
			case "prepared-finalize":
				final = true
			case "applied-retire":
				v.store.owner.applied = true
			}
			reads := v.store.reads
			var e error
			if final {
				e = v.service.FinalizeProjectExecutionRetirementInTx(ctx, tx, r, p)
			} else {
				e = v.service.RetireProjectExecutionReferencesInTx(ctx, tx, r, p)
			}
			if e == nil || v.store.writes != 0 || v.store.reads != reads {
				t.Fatal("invalid original owner/plan reached SQL", e)
			}
		})
	}
}

func TestSkillExecutionRetirementExactProof(t *testing.T) {
	for _, mode := range []string{"missing", "extra", "project", "execution", "before-digest", "operation", "attempt", "transaction", "payload", "command"} {
		t.Run(mode, func(t *testing.T) {
			v := newExecutionRetirementSkillFixture(t)
			p := v.retire(t)
			switch mode {
			case "missing":
				v.store.proof = nil
			case "extra":
				v.store.extraProof = 1
			case "project":
				v.store.proof[0] = stateID[i.Project](610).String()
			case "execution":
				v.store.proof[2] = stateID[i.Execution](611).String()
			case "before-digest":
				v.store.proof[4] = string(sum([]byte("other before")))
			case "operation":
				v.store.proof[5] = stateID[pc.Operation](612).String()
			case "attempt":
				v.store.proof[8] = stateID[struct{}](613).String()
			case "transaction":
				v.store.proof[10] = "502"
			case "payload":
				raw := v.store.proof[11].([]byte)
				v.store.proof[11] = append([]byte(`{"extra":true,`), raw[1:]...)
				v.store.proof[12] = string(sum(v.store.proof[11].([]byte)))
			case "command":
				v.store.command.Candidate.Created, _ = f.NewInstant(v.store.command.Candidate.Created.Time().Add(time.Microsecond))
			}
			writes := v.store.writes
			if e := v.service.RequireProjectExecutionReferencesRetiredInTx(v.ctx, v.store.owner.tx, v.request, p); e == nil || v.store.writes != writes {
				t.Fatal("drift or missing/extra proof accepted", e)
			}
		})
	}
	v := newExecutionRetirementSkillFixture(t)
	v.store.pending = false
	if _, e := v.service.DiscoverProjectExecutionReferencesInTx(v.ctx, v.store.owner.tx, v.request); e == nil || v.store.writes != 0 {
		t.Fatal("missing pending erased the complete original queued set", e)
	}
}

func TestSkillExecutionRetirementOriginalReturns(t *testing.T) {
	for _, mode := range []string{"read-error", "delete-error", "delete-count", "proof-error", "proof-count"} {
		t.Run(mode, func(t *testing.T) {
			v := newExecutionRetirementSkillFixture(t)
			p := v.discover(t)
			if strings.HasPrefix(mode, "proof") {
				if e := v.service.RetireProjectExecutionReferencesInTx(v.ctx, v.store.owner.tx, v.request, p); e != nil {
					t.Fatal(e)
				}
				v.store.owner.applied = true
				v.store.fail = "proof-insert"
			} else if mode == "read-error" {
				v.store.fail = "command"
			} else {
				v.store.fail = "pending-delete"
			}
			if strings.HasSuffix(mode, "count") {
				v.store.fail = ""
				v.store.wrongCount = true
			}
			var e error
			if v.store.owner.applied {
				e = v.service.FinalizeProjectExecutionRetirementInTx(v.ctx, v.store.owner.tx, v.request, p)
			} else {
				e = v.service.RetireProjectExecutionReferencesInTx(v.ctx, v.store.owner.tx, v.request, p)
			}
			if strings.HasSuffix(mode, "count") {
				if !retirementRefCode(e, f.ResourceBusy) {
					t.Fatal("exact RowsAffected ignored", e)
				}
			} else if !errors.Is(e, v.store.failure) {
				t.Fatal("original SQL error lost", e)
			}
			if v.store.proof != nil {
				t.Fatal("failed original write fabricated proof")
			}
		})
	}
	for _, mode := range []string{"delete", "proof"} {
		t.Run("held-"+mode, func(t *testing.T) {
			v := newExecutionRetirementSkillFixture(t)
			p := v.discover(t)
			if mode == "proof" {
				if e := v.service.RetireProjectExecutionReferencesInTx(v.ctx, v.store.owner.tx, v.request, p); e != nil {
					t.Fatal(e)
				}
				v.store.owner.applied = true
				v.store.hold = "proof-insert"
			} else {
				v.store.hold = "pending-delete"
			}
			v.store.entered = make(chan struct{})
			v.store.release = make(chan struct{})
			ctx, cancel := context.WithCancel(v.ctx)
			defer cancel()
			done := make(chan error, 1)
			var release sync.Once
			joined := false
			t.Cleanup(func() {
				release.Do(func() { close(v.store.release) })
				if !joined {
					<-done
				}
			})
			go func() {
				if mode == "proof" {
					done <- v.service.FinalizeProjectExecutionRetirementInTx(ctx, v.store.owner.tx, v.request, p)
				} else {
					done <- v.service.RetireProjectExecutionReferencesInTx(ctx, v.store.owner.tx, v.request, p)
				}
			}()
			<-v.store.entered
			cancel()
			select {
			case <-done:
				joined = true
				t.Fatal("cancel replaced original Exec return")
			default:
			}
			release.Do(func() { close(v.store.release) })
			e := <-done
			joined = true
			if !errors.Is(e, context.Canceled) {
				t.Fatal("original cancellation tail missing", e)
			}
		})
	}
}

type executionRetirementCaptureStore struct{ *captureBindingStore }

func (s *executionRetirementCaptureStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, e := s.captureBindingStore.InTx(tx); e != nil {
		return nil, e
	}
	return s, nil
}
func (s *executionRetirementCaptureStore) QueryRow(ctx context.Context, q string, args ...any) postgres.Row {
	switch {
	case strings.HasPrefix(q, "SELECT record FROM agenteam_skill.execution_binding_heads"):
		if s.captured == nil {
			return skillRowValues{err: pgx.ErrNoRows}
		}
		return skillRowValues{values: []any{s.captured[6]}}
	case strings.Contains(q, "array_agg(change_id::text") && strings.Contains(q, "assignment_pending_refs"), strings.Contains(q, "array_agg(id::text") && strings.Contains(q, "assignment_commands"):
		return skillRowValues{values: []any{[]string{}}}
	case strings.HasPrefix(q, "SELECT EXISTS(SELECT 1 FROM agenteam_skill.assignment_execution_retirement_proofs"):
		return skillRowValues{values: []any{false}}
	default:
		return s.captureBindingStore.QueryRow(ctx, q, args...)
	}
}
func TestSkillExecutionRetirementEmptyCaptureLocks(t *testing.T) {
	_, base, captured := initialRoundFixture(t, false)
	s := &executionRetirementCaptureStore{base}
	v := newExecutionRetirementSkillFixture(t)
	r := v.request
	r.Binding.ProjectID = captured.Initial.Request.ProjectID
	r.Binding.AgentID = captured.Initial.Request.AgentID
	r.Binding.ExecutionID = captured.Initial.Request.ExecutionID
	registration, e := i.RegisterService(i.ProjectLifecycle)
	if e != nil {
		t.Fatal(e)
	}
	scope, e := i.InProject(r.Binding.ProjectID)
	if e != nil {
		t.Fatal(e)
	}
	r.Actor, e = registration.Actor(r.Cause.OperationID.String(), scope)
	if e != nil {
		t.Fatal(e)
	}
	v.store.owner.request = r
	provider, e := NewProjectExecutionRetirementReferences(s, v.store.owner)
	if e != nil {
		t.Fatal(e)
	}
	var plan ec.ProjectExecutionRetirementReferencePlan
	writes := base.writes
	result := initialRoundTx(t, base, skillExecutionRetirementLocks(r), func(ctx context.Context, tx f.Tx) error {
		v.store.owner.tx = tx
		ctx = context.WithValue(ctx, executionRetirementSkillWitness{}, v.store.owner)
		var e error
		plan, e = provider.DiscoverProjectExecutionReferencesInTx(ctx, tx, r)
		return e
	})
	if result.State() != f.Committed || plan == nil || base.writes != writes {
		t.Fatal("original empty capture discovery", result.Fault())
	}
	p := plan.(*executionRetirementSkillPlan)
	key := skillLock(p.capture.Source.InitialSkillID, f.Shared).Key.Canonical()
	found := false
	for _, l := range plan.RequiredLocks() {
		if l.Key.Canonical() == key && l.Mode == f.Shared {
			found = true
		}
	}
	if !found || len(p.capture.Source.Result.Bindings) != 0 || plan.ProjectExecutionRetirementReferenceDigest().Validate() != nil {
		t.Fatal("empty initialized capture lost original Skill gate/digest")
	}
	locks := plan.RequiredLocks()
	locks[0].Mode = ""
	if plan.RequiredLocks()[0].Mode == "" {
		t.Fatal("caller mutated original lock plan")
	}
}

type executionRetirementHistoryDelete struct {
	*executionRetirementSkillStore
	h     *assignmentRetirementHistory
	order []string
}

func (s *executionRetirementHistoryDelete) Exec(ctx context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	if strings.HasPrefix(q, "DELETE FROM agenteam_skill.assignment_execution_retirement_proofs") {
		s.order = append(s.order, "proof")
		return s.executionRetirementSkillStore.Exec(ctx, q, args...)
	}
	tables := []string{"assignment_current", "agent_assignments", "assignment_changes", "assignment_commands", "agent_assignment_heads"}
	n := len(s.order) - 1
	if n < 0 || n >= len(tables) || !strings.HasPrefix(q, "DELETE FROM agenteam_skill."+tables[n]+" WHERE ") {
		return pgconn.CommandTag{}, errors.New("command/history deleted before exact proof")
	}
	s.order = append(s.order, tables[n])
	return pgconn.NewCommandTag("DELETE 1"), nil
}
func TestSkillExecutionRetirementAgentProofConsumption(t *testing.T) {
	for _, mode := range []string{"exact", "foreign-operation", "delete-error", "delete-count"} {
		t.Run(mode, func(t *testing.T) {
			v := newExecutionRetirementSkillFixture(t)
			v.retire(t)
			change := v.agentChange()
			if mode == "foreign-operation" {
				change.Request.Cause.OperationID = stateID[pc.Operation](622)
			}
			c, proof, e := loadAssignmentRetirementCommand(v.ctx, v.store, change, v.store.command.ID)
			if mode == "foreign-operation" {
				if e == nil {
					t.Fatal("foreign operation consumed original proof")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			h := &assignmentRetirementHistory{head: &agentInitializationRecord{project: c.Candidate.After.Project, agent: c.Candidate.After.Agent, skill: c.Candidate.Skill, sequence: 1, created: c.Candidate.Created}, commands: []*assignmentCommand{c}, changes: []*assignmentCommand{c}, grants: map[string]retirementGrant{}, current: c.Candidate.After, proofs: []*executionRetirementProof{proof}}
			if e = h.verifyChain(); e != nil {
				t.Fatal("complete first grant history", e)
			}
			s := &executionRetirementHistoryDelete{executionRetirementSkillStore: v.store, h: h}
			if mode == "delete-error" {
				v.store.fail = "proof-delete"
			}
			if mode == "delete-count" {
				v.store.wrongCount = true
			}
			e = deleteAssignmentRetirementHistory(v.ctx, s, change, h)
			switch mode {
			case "exact":
				if e != nil || len(s.order) != 6 || s.order[0] != "proof" || v.store.proof != nil {
					t.Fatal("proof must retire before original history in same caller", e)
				}
			case "delete-error":
				if !errors.Is(e, v.store.failure) || len(s.order) != 1 {
					t.Fatal("original proof deletion error bypassed", e)
				}
			case "delete-count":
				if !retirementRefCode(e, f.ResourceBusy) || len(s.order) != 1 {
					t.Fatal("proof exact count bypassed", e)
				}
			}
		})
	}
}
