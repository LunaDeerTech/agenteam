package skill

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// The stores and creation witness below are explicit contract doubles. They
// exercise the real Skill provider, not a real Agent canonical writer or PG.
type agentInitializationStore struct {
	*initializationReadStore
	head           []any
	assignments    int64
	insertions     int
	failAssignment error
}

func (s *agentInitializationStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.initializationReadStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *agentInitializationStore) RequireHeldLocks(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	if !s.live || s.tx != tx {
		return fault(f.InvalidState)
	}
	for _, l := range locks {
		mode := s.held[l.Key.Canonical()]
		if mode != f.Exclusive && mode != l.Mode {
			return fault(f.ResourceBusy)
		}
	}
	return nil
}
func (s *agentInitializationStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	head, count := slices.Clone(s.head), s.assignments
	result := s.initializationReadStore.WithinTx(ctx, cause, fn)
	if result.State() != f.Committed {
		s.head, s.assignments = head, count
	}
	return result
}
func (s *agentInitializationStore) QueryRow(ctx context.Context, q string, args ...any) postgres.Row {
	switch {
	case strings.HasPrefix(q, "INSERT INTO agenteam_skill.agent_assignment_heads"):
		if !s.live || s.head != nil {
			return skillRowValues{err: errors.New("invalid initial insert")}
		}
		assignment := ""
		if args[8] != nil {
			assignment = args[8].(string)
		}
		created := time.Unix(1700000000, 0).UTC()
		s.head = []any{args[0], args[1], args[2], args[3], args[4], args[5], int64(1), args[6], args[7], assignment, created}
		s.insertions++
		return skillRowValues{values: []any{created}}
	case strings.Contains(q, "FROM agenteam_skill.agent_assignment_heads h"):
		if s.head == nil {
			return skillRowValues{err: pgx.ErrNoRows}
		}
		return skillRowValues{values: append(slices.Clone(s.head), s.assignments, s.assignments)}
	default:
		return s.initializationReadStore.QueryRow(ctx, q, args...)
	}
}
func (s *agentInitializationStore) Exec(_ context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	if !s.live || !strings.HasPrefix(q, "INSERT INTO agenteam_skill.agent_assignments") || s.head == nil {
		return pgconn.CommandTag{}, errors.New("unexpected write")
	}
	if s.failAssignment != nil {
		return pgconn.CommandTag{}, s.failAssignment
	}
	if args[0] != s.head[9] || args[1] != s.head[0] || args[2] != s.head[1] || args[3] != s.head[7] || args[4] != s.head[10] {
		return pgconn.CommandTag{}, errors.New("initial assignment identity changed")
	}
	s.assignments++
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

type agentInitializationProjects struct {
	ProjectPorts
	store   *agentInitializationStore
	owner   id.Actor
	project pc.ProjectRef
	err     error
	intents []id.AccessIntent
}

func (p *agentInitializationProjects) RequireOwnerInTx(ctx context.Context, tx f.Tx, actor id.Actor, project id.ProjectID, intent id.AccessIntent) (pc.ProjectAccess, error) {
	p.intents = append(p.intents, intent)
	if _, err := p.store.InTx(tx); err != nil {
		return pc.ProjectAccess{}, err
	}
	if err := p.store.RequireHeldLocks(ctx, tx, []f.LockRequest{userLock(actor.Details().UserID, f.Shared), projectLock(project, f.Shared)}); err != nil {
		return pc.ProjectAccess{}, err
	}
	if p.err != nil {
		return pc.ProjectAccess{}, p.err
	}
	if !actor.Equal(p.owner) || project != p.project.ID {
		return pc.ProjectAccess{}, fault(f.Forbidden)
	}
	if err := pc.CheckOwnerGate(p.project.Lifecycle, pc.Initialized, intent); err != nil {
		return pc.ProjectAccess{}, err
	}
	return pc.NewProjectAccess(actor, p.project, p.project.UpdatedAt)
}

type agentCreationPlanDouble struct{ locks []f.LockRequest }

func (p *agentCreationPlanDouble) RequiredLocks() []f.LockRequest { return slices.Clone(p.locks) }

type agentCreationDouble struct {
	store                   *agentInitializationStore
	plan                    *agentCreationPlanDouble
	witnessTx               f.Tx
	discoverErr, appliedErr error
	checks                  int
}

func (a *agentCreationDouble) DiscoverNewAgentCreation(context.Context, ac.SkillsInitializationRequest) (ac.NewAgentCreationPlan, error) {
	if a.discoverErr != nil {
		return nil, a.discoverErr
	}
	return a.plan, nil
}
func (a *agentCreationDouble) CheckNewAgentCreationAppliedInTx(_ context.Context, tx f.Tx, _ ac.SkillsInitializationRequest, plan ac.NewAgentCreationPlan) error {
	a.checks++
	if a.appliedErr != nil {
		return a.appliedErr
	}
	if !a.store.live || a.store.tx != tx || a.witnessTx != tx || plan != a.plan {
		return fault(f.Forbidden)
	}
	return nil
}

type forgedAgentInitializationPlan struct{ locks []f.LockRequest }

func (p forgedAgentInitializationPlan) RequiredLocks() []f.LockRequest { return slices.Clone(p.locks) }

func agentInitializerFixture(t *testing.T) (*AgentInitializer, *agentInitializationStore, *agentInitializationProjects, *agentCreationDouble, ac.SkillsInitializationRequest) {
	t.Helper()
	_, base, p, actor, r := readerFixture(t)
	store := &agentInitializationStore{initializationReadStore: base}
	projects := &agentInitializationProjects{store: store, owner: actor, project: p.project}
	authority, err := NewAuthority(store, projects)
	if err != nil {
		t.Fatal(err)
	}
	creation := &agentCreationDouble{store: store, plan: &agentCreationPlanDouble{}}
	initializer, err := NewAgentInitializer(authority, creation)
	if err != nil {
		t.Fatal(err)
	}
	command, err := f.NewCommandIdentity("project", []string{r.request.ProjectID.String()}, "agent.create", "initial-agent")
	if err != nil {
		t.Fatal(err)
	}
	request := ac.SkillsInitializationRequest{Actor: actor, ProjectID: r.request.ProjectID, AgentID: stateID[id.Agent](220), Command: command, PlanRevision: 1, AddSkillsEnabled: true}
	return initializer, store, projects, creation, request
}

func TestNewAgentInitializerAtomicSetAndSameTxReentry(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		name := "disabled"
		if enabled {
			name = "enabled"
		}
		t.Run(name, func(t *testing.T) {
			a, s, projects, owner, r := agentInitializerFixture(t)
			r.AddSkillsEnabled = enabled
			plan, err := a.DiscoverNewAgentInitialization(context.Background(), r)
			if err != nil {
				t.Fatal(err)
			}
			copy := plan.RequiredLocks()
			copy[0].Mode = f.Shared
			if plan.RequiredLocks()[0].Mode != f.Exclusive {
				t.Fatal("lock projection aliases plan")
			}
			cause, _ := f.NewCommandsCause(r.Command)
			before := s.txs
			result := s.WithinTx(context.Background(), cause, func(ctx context.Context, tx f.Tx) error {
				if err := s.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
					return err
				}
				owner.witnessTx = tx
				if err := a.InitializeNewAgentInTx(ctx, tx, r, plan); err != nil {
					return err
				}
				first := slices.Clone(s.head)
				if err := a.InitializeNewAgentInTx(ctx, tx, r, plan); err != nil {
					return err
				}
				if first[9] != s.head[9] || s.insertions != 1 {
					t.Fatal("same-Tx reentry allocated a new identity")
				}
				return nil
			})
			if result.State() != f.Committed || s.txs != before+1 || owner.checks != 2 || s.head == nil {
				t.Fatal("not one original final transaction")
			}
			want := int64(0)
			if enabled {
				want = 1
			}
			if s.assignments != want || !slices.Equal(projects.intents, []id.AccessIntent{id.Read, id.Read, id.Mutate, id.Read, id.Mutate}) {
				t.Fatal("false bypassed provider or owner gates")
			}
			if err = a.InitializeNewAgentInTx(context.Background(), s.tx, r, plan); err == nil {
				t.Fatal("ended transaction accepted")
			}
			// Same durable head alone is not authority in a later transaction.
			result = s.WithinTx(context.Background(), cause, func(ctx context.Context, tx f.Tx) error {
				if err := s.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
					return err
				}
				return a.InitializeNewAgentInTx(ctx, tx, r, plan)
			})
			if result.State() != f.NotCommitted || s.insertions != 1 {
				t.Fatal("stored receipt manufactured new-create authority")
			}
		})
	}
}

func TestNewAgentInitializerRejectsStaleAuthorityBeforeWrites(t *testing.T) {
	for _, name := range []string{"forged_plan", "foreign_issuer", "changed_default", "changed_revision", "foreign_tx", "missing_lock", "missing_witness", "published_agent", "revoked", "archived", "unpublished_skill", "changed_skill", "assignment_error", "caller_rollback"} {
		t.Run(name, func(t *testing.T) {
			a, s, projects, owner, r := agentInitializerFixture(t)
			plan, err := a.DiscoverNewAgentInitialization(context.Background(), r)
			if err != nil {
				t.Fatal(err)
			}
			private := errors.New("private-final-canary")
			switch name {
			case "forged_plan":
				plan = forgedAgentInitializationPlan{plan.RequiredLocks()}
			case "foreign_issuer":
				other, _, _, _, rr := agentInitializerFixture(t)
				plan, err = other.DiscoverNewAgentInitialization(context.Background(), rr)
				if err != nil {
					t.Fatal(err)
				}
			case "changed_default":
				r.AddSkillsEnabled = false
			case "changed_revision":
				r.PlanRevision = 2
			case "published_agent":
				owner.appliedErr = fault(f.InvalidState)
			case "revoked":
				projects.err = fault(f.SessionRevoked)
			case "archived":
				projects.project.Lifecycle = pc.Archived
				at := projects.project.UpdatedAt
				projects.project.ArchivedAt = &at
			case "unpublished_skill":
				s.row.values[14] = "reserved"
			case "changed_skill":
				s.published.values[8] = int64(2)
			case "assignment_error":
				s.failAssignment = private
			}
			cause, _ := f.NewCommandsCause(r.Command)
			var callErr error
			result := s.WithinTx(context.Background(), cause, func(ctx context.Context, tx f.Tx) error {
				if err := s.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
					return err
				}
				owner.witnessTx = tx
				if name == "missing_lock" {
					delete(s.held, plan.RequiredLocks()[0].Key.Canonical())
				}
				if name == "missing_witness" {
					owner.witnessTx = f.Tx{}
				}
				if name == "foreign_tx" {
					tx = f.NewTx()
				}
				callErr = a.InitializeNewAgentInTx(ctx, tx, r, plan)
				if name == "caller_rollback" && callErr == nil {
					return private
				}
				return callErr
			})
			if result.State() != f.NotCommitted || s.head != nil || s.assignments != 0 {
				t.Fatal("failed final transaction left an initial set")
			}
			if name != "caller_rollback" && callErr == nil {
				t.Fatal("rejection omitted")
			}
			if name == "assignment_error" && (!errors.Is(callErr, private) || strings.Contains(callErr.Error(), private.Error())) {
				t.Fatal("original store error identity/privacy lost")
			}
		})
	}
}

func TestNewAgentInitializerMissingDependencyAndUnknownDiscovery(t *testing.T) {
	a, s, _, owner, r := agentInitializerFixture(t)
	if _, err := NewAgentInitializer(nil, owner); err == nil {
		t.Fatal("nil Authority accepted")
	}
	if _, err := NewAgentInitializer(&Authority{}, owner); err == nil {
		t.Fatal("zero Authority accepted")
	}
	var missing *agentCreationDouble
	authority, _ := NewAuthority(s, a.state().authority.projects)
	if _, err := NewAgentInitializer(authority, missing); err == nil {
		t.Fatal("typed nil creation authority accepted")
	}
	s.unknown = true
	plan, err := a.DiscoverNewAgentInitialization(context.Background(), r)
	physical, ok := UnknownAttempt(err)
	if plan != nil || !ok || physical.AttemptID() != stateID[f.TransactionAttempt](70) || s.head != nil {
		t.Fatal("Unknown discovery escaped as plan")
	}
	s.unknown = false
	owner.discoverErr = fault(f.DependencyUnbound)
	r.AddSkillsEnabled = false
	if plan, err = a.DiscoverNewAgentInitialization(context.Background(), r); plan != nil || err != owner.discoverErr {
		t.Fatal("false bypassed unbound Agent owner")
	}
}
