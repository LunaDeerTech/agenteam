package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

// This controlled original Store/Prepared seam checks the provider protocol.
// It does not prove PostgreSQL predicates, claims, owner death or Runner return.
type executionRetirementWitness struct{}
type executionRetirementOwner struct {
	tx      f.Tx
	request ec.ProjectExecutionRetirementRequest
	live    bool
	applied bool
	calls   int
}

func (o *executionRetirementOwner) check(ctx context.Context, tx f.Tx, r ec.ProjectExecutionRetirementRequest, applied bool) error {
	o.calls++
	if !o.live || o.applied != applied || tx != o.tx || ctx.Value(executionRetirementWitness{}) != o || r.Binding != o.request.Binding || r.Cause != o.request.Cause || !r.Actor.Equal(o.request.Actor) {
		return fail(f.Forbidden)
	}
	return nil
}
func (o *executionRetirementOwner) RequireProjectExecutionRetirementPreparedInTx(ctx context.Context, tx f.Tx, r ec.ProjectExecutionRetirementRequest) error {
	return o.check(ctx, tx, r, false)
}
func (o *executionRetirementOwner) RequireProjectExecutionRetirementAppliedInTx(ctx context.Context, tx f.Tx, r ec.ProjectExecutionRetirementRequest) error {
	return o.check(ctx, tx, r, true)
}
func (*executionRetirementOwner) ProjectExecutionRetirementProofInTx(context.Context, f.Tx, ec.ProjectExecutionRetirementRequest) (ec.ProjectExecutionRetirementCommit, error) {
	return ec.ProjectExecutionRetirementCommit{}, fail(f.Forbidden)
}

type executionRetirementWrite struct {
	table string
	args  []any
}
type executionRetirementStore struct {
	Store         // No self-owned transaction or late lock acquisition is implemented.
	t             *testing.T
	owner         *executionRetirementOwner
	live          bool
	held          map[string]f.LockMode
	snapshot      []byte
	busy          bool
	reads, writes int
	expected      []executionRetirementWrite
	affected      int64
	cancel        context.CancelFunc
}

func (s *executionRetirementStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if !s.live || tx != s.owner.tx {
		return nil, fail(f.Forbidden)
	}
	return s, nil
}
func (s *executionRetirementStore) RequireHeldLocks(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	if !s.live || tx != s.owner.tx {
		return fail(f.Forbidden)
	}
	for _, l := range locks {
		if mode, ok := s.held[l.Key.Canonical()]; !ok || mode != f.Exclusive && mode != l.Mode {
			return fail(f.Forbidden)
		}
	}
	return nil
}
func (s *executionRetirementStore) authorized(ctx context.Context) bool {
	return s.owner.live && s.owner.calls > 0 && ctx.Value(executionRetirementWitness{}) == s.owner
}
func (s *executionRetirementStore) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	s.reads++
	if !s.authorized(ctx) || !reflect.DeepEqual(args, []any{s.owner.request.Binding.ExecutionID.String()}) {
		s.t.Fatal("SQL escaped original Prepared execution")
	}
	return retirementRefScan(func(out ...any) error {
		if len(out) != 1 {
			return fmt.Errorf("controlled row shape")
		}
		if strings.HasPrefix(query, "SELECT EXISTS") {
			for _, table := range []string{"operations", "attempts", "read_file_results", "write_file_results", "command_results"} {
				if !strings.Contains(query, "agenteam_tool."+table+" WHERE execution_id=$1") {
					s.t.Error("unowned Tool history omitted", table)
				}
			}
			*out[0].(*bool) = s.busy
		} else {
			*out[0].(*[]byte) = bytes.Clone(s.snapshot)
		}
		return nil
	})
}
func (s *executionRetirementStore) Query(context.Context, string, ...any) (*postgres.Rows, error) {
	s.t.Fatal("unexpected multirow reader")
	return nil, fail(f.InternalError)
}
func (s *executionRetirementStore) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	n := s.writes
	s.writes++
	if !s.authorized(ctx) || n >= len(s.expected) || !strings.HasPrefix(query, "DELETE FROM agenteam_tool."+s.expected[n].table+" WHERE execution_id=$1") || !reflect.DeepEqual(args, s.expected[n].args) {
		s.t.Fatal("delete escaped exact owned FK order", query, args)
	}
	if n+1 == len(s.expected) && s.affected == 1 {
		s.snapshot = []byte(`{"head":null,"references":[]}`)
	}
	if s.cancel != nil {
		s.cancel()
	}
	return pgconn.NewCommandTag(fmt.Sprintf("DELETE %d", s.affected)), nil
}
func executionRetirementFixture(t *testing.T) (*ProjectExecutionRetirementReferences, *executionRetirementStore, context.Context, ec.ProjectExecutionRetirementRequest) {
	t.Helper()
	p, a, e := retirementRefID[i.Project](t, 1), retirementRefID[i.Agent](t, 2), retirementRefID[i.Execution](t, 3)
	op := retirementRefID[pc.Operation](t, 4)
	registration, err := i.RegisterService(i.ProjectLifecycle)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := i.InProject(p)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := registration.Actor(op.String(), scope)
	if err != nil {
		t.Fatal(err)
	}
	r := ec.ProjectExecutionRetirementRequest{Actor: actor, Cause: pc.LifecycleCause{OperationID: op, Action: pc.Delete, ProjectVersion: 9}, Binding: ec.ProjectExecutionRetirementBinding{ProjectID: p, AgentID: a, ExecutionID: e, Version: 7, CanonicalDigest: f.Digest("sha256:" + strings.Repeat("a", 64))}}
	if err = r.Validate(); err != nil {
		t.Fatal(err)
	}
	o := &executionRetirementOwner{tx: f.NewTx(), request: r, live: true}
	s := &executionRetirementStore{t: t, owner: o, live: true, held: map[string]f.LockMode{}, affected: 1}
	// Independent exact expected identities, including the discovery-only extra locks.
	pk, _ := f.ProjectLock(p.String())
	sk, _ := f.ProjectScheduleLock(p.String())
	ak, _ := f.AgentLock(a.String())
	ek, _ := f.AggregateLock(f.ExecutionAggregate, e.String())
	for _, k := range []f.LockKey{pk, sk, ak, ek} {
		s.held[k.Canonical()] = f.Exclusive
	}
	tool := retirementRefID[i.Tool](t, 10)
	s.held[RegistryLock(f.Exclusive).Key.Canonical()] = f.Exclusive
	s.held[toolLock(tool, f.Shared).Key.Canonical()] = f.Shared
	head := map[string]any{"execution_id": e.String(), "project_id": p.String(), "agent_id": a.String(), "agent_version": 3, "attempt_binding": r.Binding.CanonicalDigest, "capture_digest": r.Binding.CanonicalDigest, "tool_count": 1}
	refs := []any{map[string]any{
		"execution_id": e.String(), "tool_id": tool.String(), "spec_revision": 2,
		"model_visible_name": "tool_" + strings.ReplaceAll(tool.String(), "-", ""),
		"binding_kind":       "builtin", "handler_id": "fixture.first", "contract_revision": 1,
		"scope_resolver_id": "fixture.scope", "risk_classifier_id": "fixture.risk", "class": "ordinary",
		"operation_name": nil, "operation_revision": nil, "protocol_major": nil,
	}}
	s.snapshot, err = json.Marshal(map[string]any{"head": head, "references": refs})
	if err != nil {
		t.Fatal(err)
	}
	s.expected = []executionRetirementWrite{{"execution_references", []any{e.String(), tool.String()}}, {"execution_configurations", []any{e.String(), p.String(), a.String()}}}
	provider, err := NewProjectExecutionRetirementReferences(s, o)
	if err != nil {
		t.Fatal(err)
	}
	return provider, s, context.WithValue(context.Background(), executionRetirementWitness{}, o), r
}

func TestProjectExecutionRetirementToolRequiresOriginalPrepared(t *testing.T) {
	for _, mode := range []string{"foreign-tx", "expired-store", "missing-lock", "public-request", "expired-owner", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			p, s, ctx, r := executionRetirementFixture(t)
			tx := s.owner.tx
			switch mode {
			case "foreign-tx":
				tx = f.NewTx()
			case "expired-store":
				s.live = false
			case "missing-lock":
				k, _ := f.AgentLock(r.Binding.AgentID.String())
				delete(s.held, k.Canonical())
			case "public-request":
				ctx = context.Background()
			case "expired-owner":
				s.owner.live = false
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			plan, err := p.DiscoverProjectExecutionReferencesInTx(ctx, tx, r)
			if err == nil || plan != nil || s.reads != 0 || s.writes != 0 {
				t.Fatal("fabricated preparation reached SQL", err)
			}
		})
	}
}

func TestProjectExecutionRetirementToolDeletesOnlyOriginalReferences(t *testing.T) {
	p, s, ctx, r := executionRetirementFixture(t)
	plan, err := p.DiscoverProjectExecutionReferencesInTx(ctx, s.owner.tx, r)
	if err != nil {
		t.Fatal(err)
	}
	if s.writes != 0 || len(plan.RequiredLocks()) != 6 || p.ProjectExecutionRetirementReferenceKind() != ec.ProjectExecutionToolReferences {
		t.Fatal("discovery mutated or omitted lock union")
	}
	if plan.ProjectExecutionRetirementReferenceDigest().Validate() != nil {
		t.Fatal("complete reference shape has no digest")
	}
	locks := plan.RequiredLocks()
	locks[0].Mode = f.Shared
	if reflect.DeepEqual(locks, plan.RequiredLocks()) {
		t.Fatal("caller mutated private locks")
	}
	before := s.reads
	if err = p.RequireProjectExecutionReferencesRetiredInTx(ctx, s.owner.tx, r, plan); !retirementRefCode(err, f.Forbidden) || s.reads != before {
		t.Fatal("Prepared substituted for Applied", err)
	}
	if err = p.RetireProjectExecutionReferencesInTx(ctx, s.owner.tx, r, plan); err != nil {
		t.Fatal(err)
	}
	s.owner.applied = true // Controlled consumer reaches its separate Applied phase.
	if err = p.RequireProjectExecutionReferencesRetiredInTx(ctx, s.owner.tx, r, plan); err != nil || s.writes != 2 || s.owner.calls != 4 {
		t.Fatal("same-Tx retirement failed", err)
	}
	before = s.reads
	if err = p.RetireProjectExecutionReferencesInTx(ctx, s.owner.tx, r, plan); err == nil || s.reads != before || s.writes != 2 {
		t.Fatal("Applied confirmation allowed a second mutation", err)
	}
	s.owner.live = false
	before = s.reads
	if err = p.RequireProjectExecutionReferencesRetiredInTx(ctx, s.owner.tx, r, plan); err == nil || s.reads != before {
		t.Fatal("empty tables substituted for Prepared")
	}
}

func TestProjectExecutionRetirementToolRejectsChangedOrUnownedState(t *testing.T) {
	for _, mode := range []string{"unowned-history", "changed-complete-reference", "foreign-plan", "missing-final-lock"} {
		t.Run(mode, func(t *testing.T) {
			p, s, ctx, r := executionRetirementFixture(t)
			plan, err := p.DiscoverProjectExecutionReferencesInTx(ctx, s.owner.tx, r)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "unowned-history":
				s.busy = true
			case "changed-complete-reference":
				s.snapshot = bytes.Replace(s.snapshot, []byte(`"handler_id":"fixture.first"`), []byte(`"handler_id":"fixture.second"`), 1)
				current, err := p.DiscoverProjectExecutionReferencesInTx(ctx, s.owner.tx, r)
				if err != nil || current.ProjectExecutionRetirementReferenceDigest() == plan.ProjectExecutionRetirementReferenceDigest() {
					t.Fatal("shape digest omitted full immutable reference", err)
				}
			case "foreign-plan":
				p, err = NewProjectExecutionRetirementReferences(s, s.owner)
				if err != nil {
					t.Fatal(err)
				}
			case "missing-final-lock":
				delete(s.held, RegistryLock(f.Exclusive).Key.Canonical())
			}
			if err = p.RetireProjectExecutionReferencesInTx(ctx, s.owner.tx, r, plan); err == nil || s.writes != 0 {
				t.Fatal("stale/unowned source was deleted", err)
			}
		})
	}
}

func TestProjectExecutionRetirementToolStopsAfterIncompleteDelete(t *testing.T) {
	for _, mode := range []string{"zero-rows", "cancelled-after-first"} {
		t.Run(mode, func(t *testing.T) {
			p, s, ctx, r := executionRetirementFixture(t)
			plan, err := p.DiscoverProjectExecutionReferencesInTx(ctx, s.owner.tx, r)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "zero-rows" {
				s.affected = 0
			} else {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				defer cancel()
				s.cancel = cancel
			}
			if err = p.RetireProjectExecutionReferencesInTx(ctx, s.owner.tx, r, plan); err == nil || s.writes != 1 {
				t.Fatal("incomplete child deletion reached parent", err)
			}
		})
	}
}
