package skill

import (
	"context"
	"errors"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
)

type initializationReadStore struct {
	Store
	tx                   f.Tx
	live                 bool
	held                 map[string]f.LockMode
	row, published       skillRowValues
	unknown              bool
	txs, acquires, facts int
}

func (s *initializationReadStore) QueryRow(_ context.Context, query string, _ ...any) postgres.Row {
	if s.live {
		s.facts++
	}
	if strings.Contains(query, "FROM agenteam_skill.initializations") {
		return s.row
	}
	if strings.Contains(query, "FROM agenteam_skill.installations") {
		return skillRowValues{err: pgx.ErrNoRows}
	}
	return s.published
}
func (s *initializationReadStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if !s.live || tx != s.tx {
		return nil, fault(f.InvalidState)
	}
	return s, nil
}
func (s *initializationReadStore) AcquireAll(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	s.acquires++
	if !s.live || tx != s.tx {
		return fault(f.InvalidState)
	}
	for n, lock := range locks {
		if n > 0 && f.CompareLockKeys(locks[n-1].Key, lock.Key) >= 0 {
			return invalid()
		}
		s.held[lock.Key.Canonical()] = lock.Mode
	}
	return nil
}
func (s *initializationReadStore) RequireHeldLocks(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	if !s.live || tx != s.tx {
		return fault(f.InvalidState)
	}
	for _, lock := range locks {
		if s.held[lock.Key.Canonical()] != lock.Mode {
			return fault(f.ResourceBusy)
		}
	}
	return nil
}
func (s *initializationReadStore) WithinTx(ctx context.Context, cause f.TransactionCause, work func(context.Context, f.Tx) error) f.CommitResult {
	s.txs++
	s.tx = f.NewTx()
	s.live = true
	s.held = map[string]f.LockMode{}
	defer func() { s.live = false }()
	if e := work(ctx, s.tx); e != nil {
		var known *f.Fault
		if errors.As(e, &known) {
			return f.NotCommittedResult(known)
		}
		return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotStarted).WithCause(e))
	}
	if s.unknown {
		return f.UnknownResult(stateID[f.TransactionAttempt](70), cause)
	}
	return f.CommittedResult()
}

type initializationReadProjects struct {
	ProjectPorts
	store                  *initializationReadStore
	gate                   error
	converges, initializes int
	ctx                    context.Context
	tx                     f.Tx
}

func (p *initializationReadProjects) check(ctx context.Context, tx f.Tx) error {
	p.ctx = ctx
	p.tx = tx
	if !p.store.live || p.store.tx != tx {
		return invalid()
	}
	return p.gate
}
func (p *initializationReadProjects) ValidateInitializationInTx(ctx context.Context, tx f.Tx, _ id.Actor, _ pc.CreationID, _ pc.ProjectID, _ f.IdempotencyKey) error {
	p.initializes++
	return p.check(ctx, tx)
}
func (p *initializationReadProjects) ValidateInitializationConvergenceInTx(ctx context.Context, tx f.Tx, _ id.Actor, _ pc.InitializationRequest) error {
	p.converges++
	return p.check(ctx, tx)
}
func readFixture(t *testing.T) (*Service, *initializationReadStore, *initializationReadProjects, id.Actor, pc.InitializationRequest) {
	t.Helper()
	r := stateRow(t)
	r.phase = initializationPublished
	r.object = stateID[oc.StoredObject](7)
	r.upload = stateID[oc.Upload](8)
	r.attempt = stateID[oc.Attempt](6)
	v := stateValues(t)
	v[14] = string(r.phase)
	v[16] = r.object.String()
	v[17] = r.upload.String()
	v[18] = r.attempt.String()
	store := &initializationReadStore{row: skillRowValues{values: v}, published: skillRowValues{values: []any{r.request.ProjectID.String(), r.skill.String(), r.revision.String(), r.bundle.name, AddSkillsNormalizedName, r.bundle.description, true, int64(1), int64(1), true, int64(1), r.object.String(), int64(1), r.created.Time(), r.updated.Time()}}}
	p := &initializationReadProjects{store: store}
	a, e := NewAuthority(store, p)
	if e != nil {
		t.Fatal(e)
	}
	b, e := AddSkills(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	s, e := New(Dependencies{Authority: a, Objects: &constructorObjects{}, Processes: &constructorProcesses{}, ProcessID: stateID[oc.Process](50), Bundle: b})
	if e != nil {
		t.Fatal(e)
	}
	role, _ := id.RegisterService(id.ProjectInitialization)
	scope, _ := id.InProject(r.request.ProjectID)
	actor, _ := role.Actor(r.request.CreationID.String(), scope)
	return s, store, p, actor, r.request
}
func TestInitializationInspectionKeepsOriginalOutcomeAndGate(t *testing.T) {
	for _, name := range []string{"published", "absent", "reserved", "failed", "denied", "unknown_gate", "unknown_commit", "corrupt"} {
		t.Run(name, func(t *testing.T) {
			s, store, p, actor, request := readFixture(t)
			sentinel := fault(f.Forbidden)
			switch name {
			case "absent":
				store.row = skillRowValues{err: pgx.ErrNoRows}
			case "reserved":
				store.row.values[14] = "reserved"
			case "failed":
				store.row.values[14] = "failed"
				store.row.values[19] = "operation_failed"
			case "denied":
				p.gate = sentinel
			case "unknown_gate":
				sentinel = f.NewFault(f.CommitUnknown, f.Unknown)
				p.gate = sentinel
			case "unknown_commit":
				store.unknown = true
			case "corrupt":
				store.published.values[6] = false
			}
			got, e := s.InspectProjectSkills(context.Background(), actor, request)
			if p.converges != 1 || p.initializes != 0 {
				t.Fatal("wrong current gate")
			}
			switch name {
			case "denied", "unknown_gate":
				if e != sentinel || store.facts != 0 {
					t.Fatal("gate replaced or facts read before permission")
				}
			case "unknown_commit":
				if got.State != "" {
					t.Fatal("uncommitted observation escaped")
				}
				r, ok := UnknownAttempt(e)
				if !ok || r.AttemptID() != stateID[f.TransactionAttempt](70) {
					t.Fatal("original unknown lost")
				}
			case "corrupt":
				if e == nil || got.State != "" {
					t.Fatal("corrupt publication accepted")
				}
			default:
				if e != nil || !got.Matches(request) {
					t.Fatal(e)
				}
				want := pc.InitializationResultPending
				if name == "published" {
					want = pc.InitializationCompleted
				}
				if name == "failed" {
					want = pc.InitializationFailed
				}
				if got.State != want {
					t.Fatal("wrong observation state")
				}
			}
		})
	}
}
func TestInitializationConfirmationRequiresSameProviderLiveTxAndFullFacts(t *testing.T) {
	for _, name := range []string{"valid", "other_issuer", "other_actor", "foreign_tx", "ended_tx", "weak_lock", "gate_changed", "row_missing", "not_published", "protection_changed", "object_changed", "tombstone"} {
		t.Run(name, func(t *testing.T) {
			s, store, p, actor, request := readFixture(t)
			ctx := context.WithValue(context.Background(), struct{}{}, "original")
			plan, e := s.DiscoverConfirmation(ctx, actor, request)
			if e != nil || len(plan.RequiredLocks()) != 4 {
				t.Fatal("discovery", e)
			}
			store.live = true
			store.tx = f.NewTx()
			store.held = map[string]f.LockMode{}
			for _, l := range plan.RequiredLocks() {
				store.held[l.Key.Canonical()] = l.Mode
			}
			tx := store.tx
			sentinel := fault(f.Forbidden)
			switch name {
			case "other_issuer":
				plan, e = pc.NewInitializationPlanIssuer().Plan(actor, request, plan.ProposedReceipt(), plan.RequiredLocks())
				if e != nil {
					t.Fatal(e)
				}
			case "other_actor":
				role, _ := id.RegisterService(id.ProjectInitialization)
				scope, _ := id.InProject(request.ProjectID)
				actor, _ = role.Actor(stateID[pc.Creation](80).String(), scope)
			case "foreign_tx":
				tx = f.NewTx()
			case "ended_tx":
				store.live = false
			case "weak_lock":
				store.held[plan.RequiredLocks()[2].Key.Canonical()] = f.Shared
			case "gate_changed":
				p.gate = sentinel
			case "row_missing":
				store.row = skillRowValues{err: pgx.ErrNoRows}
			case "not_published":
				store.row.values[14] = "reserved"
			case "protection_changed":
				store.published.values[6] = false
			case "object_changed":
				store.published.values[11] = stateID[oc.StoredObject](81).String()
			case "tombstone":
				store.published.values[9] = false
			}
			txs, acquires := store.txs, store.acquires
			got, e := s.ConfirmInitializedInTx(ctx, tx, actor, request, plan)
			if txs != store.txs || acquires != store.acquires {
				t.Fatal("Confirm opened transaction or acquired missing lock")
			}
			if name == "valid" {
				if e != nil || got != plan.ProposedReceipt() || p.tx != tx || p.ctx != ctx {
					t.Fatal("original Tx/context/facts changed", e)
				}
			} else if e == nil || got.Validate() == nil {
				t.Fatal("invalid confirmation accepted")
			}
			if name == "gate_changed" && e != sentinel {
				t.Fatal("Project error replaced")
			}
		})
	}
}
