package model

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

// These controls exercise actual Model SQL decoding and final participant
// ordering. The owner callback below is deliberately controlled, not an Agent
// canonical writer or proof that real PostgreSQL committed an Agent.
type agentReferenceFixture struct {
	selection *selectionFixture
	store     *agentReferenceStore
	service   *AgentReferenceService
	change    mc.AgentReferenceChange
	issuer    mc.PlanIssuer
	ownerErr  error
	checked   int
}
type agentReferenceWitnessKey struct{}
type agentReferenceStore struct {
	*projectScopeStore
	fixture           *agentReferenceFixture
	references        []referenceRecord
	writes            int
	readErr, writeErr error
	disabled          map[string]bool
}

func (s *agentReferenceStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.projectScopeStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *agentReferenceStore) QueryRow(ctx context.Context, q string, args ...any) postgres.Row {
	if strings.Contains(q, "array_agg(role") {
		if len(args) != 1 || args[0] != s.fixture.change.AgentID.String() || strings.Contains(q, "project_id=$") {
			panic("reference query must read complete Agent index")
		}
		return scanFunc(func(out ...any) error {
			if s.readErr != nil {
				return s.readErr
			}
			var roles, projects, models, efforts []string
			var versions []int64
			for _, r := range s.references {
				roles, projects, models, efforts = append(roles, r.Role), append(projects, r.Project), append(models, r.Model), append(efforts, r.Effort)
				versions = append(versions, int64(r.Version))
			}
			*out[0].(*[]string), *out[1].(*[]string), *out[2].(*[]string), *out[3].(*[]int64), *out[4].(*[]string) = roles, projects, models, versions, efforts
			return nil
		})
	}
	// The existing controlled adapter supplies real Model/Provider row shapes.
	// Each requested ID gets its own decode; only enablement is varied here.
	return scanFunc(func(out ...any) error {
		x := s.fixture.selection
		original, enabled := x.request.ModelID, x.modelEnabled
		defer func() { x.request.ModelID, x.modelEnabled = original, enabled }()
		if strings.Contains(q, "agenteam_model.models m") {
			x.request.ModelID, _ = f.ParseID[mc.Model](args[0].(string))
			x.modelEnabled = enabled && !s.disabled[args[0].(string)]
		}
		return s.projectScopeStore.QueryRow(ctx, q, args...).Scan(out...)
	})
}
func (s *agentReferenceStore) Exec(ctx context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	if s.fixture.checked == 0 || ctx.Value(agentReferenceWitnessKey{}) != s.fixture {
		panic("Model index written before original owner callback")
	}
	if s.writeErr != nil {
		return pgconn.CommandTag{}, s.writeErr
	}
	s.writes++
	if strings.HasPrefix(q, "DELETE FROM agenteam_model.references") {
		if len(args) != 1 || args[0] != s.fixture.change.AgentID.String() {
			panic("wrong owner deletion")
		}
		count := len(s.references)
		s.references = nil
		return pgconn.NewCommandTag("DELETE " + strconv.Itoa(count)), nil
	}
	if strings.HasPrefix(q, "INSERT INTO agenteam_model.references") && len(args) == 6 {
		s.references = append(s.references, referenceRecord{Kind: "agent", Owner: args[0].(string), Role: args[1].(string), Project: args[2].(string), Model: args[3].(string), Version: f.Version(args[4].(int64)), Effort: args[5].(string)})
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	}
	panic("unexpected non-Model SQL")
}
func (x *agentReferenceFixture) DiscoverAgentReferenceOwner(_ context.Context, r mc.AgentReferenceChange) (mc.AgentReferenceOwnerPlan, error) {
	locks, err := mc.AgentReferenceBaseLocks(r)
	if err != nil {
		return mc.AgentReferenceOwnerPlan{}, err
	}
	return mc.NewAgentReferenceOwnerPlan(x.issuer, r, hash([]byte("controlled-owner-canonical-mapping")), locks)
}
func (x *agentReferenceFixture) CheckAgentReferenceOwnerAppliedInTx(ctx context.Context, tx f.Tx, r mc.AgentReferenceChange, p mc.AgentReferenceOwnerPlan) error {
	if x.ownerErr != nil {
		return x.ownerErr
	}
	b, _ := mc.AgentReferenceBinding(r)
	if tx != x.store.tx || ctx.Value(agentReferenceWitnessKey{}) != x || !p.Matches(x.issuer, b, hash([]byte("controlled-owner-canonical-mapping"))) {
		return fault(f.Forbidden)
	}
	x.checked++
	return nil
}
func newAgentReferenceFixture(t *testing.T) *agentReferenceFixture {
	t.Helper()
	x := &agentReferenceFixture{selection: newSelectionFixture(t, false), issuer: mc.NewPlanIssuer()}
	x.store = &agentReferenceStore{projectScopeStore: x.selection.store, fixture: x, disabled: map[string]bool{}}
	authority, err := NewAuthority(x.store, x.selection.service.state().authority.state().auth)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := NewAgentConfiguration(x.store, authority)
	if err != nil {
		t.Fatal(err)
	}
	x.service, err = NewAgentReferences(x.store, selection, x)
	if err != nil {
		t.Fatal(err)
	}
	r := x.selection.request
	x.change = mc.AgentReferenceChange{Actor: r.Actor, ProjectID: r.ProjectID, AgentID: mustID[id.Agent](t), Command: r.Command, PlanRevision: 1, ResultOwnerVersion: 1, After: mc.AgentModelSelection{ModelID: r.ModelID}}
	return x
}
func (x *agentReferenceFixture) update(t *testing.T, approval bool) {
	t.Helper()
	if approval {
		mid := mustID[mc.Model](t)
		x.change.After.ApprovalModelID = &mid
	}
	before, version := x.change.After.Clone(), f.Version(3)
	x.change.Before, x.change.ExpectedOwnerVersion, x.change.ResultOwnerVersion = &before, &version, 4
	x.change.Command, _ = f.NewCommandIdentity("project", []string{x.change.ProjectID.String()}, "agent.update", "reference-update")
	x.store.references = agentReferenceRecords(x.change, &before, version)
}
func (x *agentReferenceFixture) discover(t *testing.T) mc.AgentReferencePlan {
	t.Helper()
	p, err := x.service.DiscoverAgentReferences(context.Background(), x.change)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func (x *agentReferenceFixture) final(p mc.AgentReferencePlan) (context.Context, f.Tx) {
	x.store.tx, x.store.locks = f.NewTx(), p.RequiredLocks()
	return context.WithValue(context.Background(), agentReferenceWitnessKey{}, x), x.store.tx
}

func TestAgentReferencesWholePairAndRetainedOwnerVersion(t *testing.T) {
	for _, name := range []string{"create", "owner-version", "remove-approval", "replace-disabled-primary", "no-op"} {
		t.Run(name, func(t *testing.T) {
			x := newAgentReferenceFixture(t)
			if name == "create" {
				approval := x.change.After.ModelID // One model may fill two distinct roles.
				x.change.After.ApprovalModelID = &approval
			} else {
				x.update(t, true)
			}
			switch name {
			case "remove-approval":
				x.change.After.ApprovalModelID = nil
			case "replace-disabled-primary":
				x.store.disabled[x.change.Before.ModelID.String()] = true
				x.change.After.ModelID = mustID[mc.Model](t)
			case "no-op":
				x.change.ResultOwnerVersion = *x.change.ExpectedOwnerVersion
			}
			p := x.discover(t)
			ctx, tx := x.final(p)
			transactions, acquisitions := x.store.transactions, x.store.acquisitions
			if err := x.service.ApplyAgentReferencesInTx(ctx, tx, x.change, p); err != nil {
				t.Fatal(err)
			}
			want := agentReferenceRecords(x.change, &x.change.After, x.change.ResultOwnerVersion)
			if !slices.Equal(x.store.references, want) || x.checked != 1 || x.store.transactions != transactions || x.store.acquisitions != acquisitions {
				t.Fatal("lost full pair, current owner proof or original caller Tx")
			}
			if name == "no-op" {
				if x.store.writes != 0 {
					t.Fatal("unchanged command rewrote references")
				}
			} else {
				if x.store.writes != 1+len(want) {
					t.Fatal("partial role update")
				}
				requireCode(t, x.service.ApplyAgentReferencesInTx(ctx, tx, x.change, p), f.ResourceBusy)
			}
		})
	}
}

func TestAgentReferencesRejectInvalidFinalOwnerAndMapping(t *testing.T) {
	for _, name := range []string{"foreign-tx", "foreign-issuer", "missing-witness", "owner-reject", "session-revoked", "changed-revision", "missing-lock", "missing-role", "foreign-project", "stale-owner-version", "stale-model", "stale-provider", "cancelled"} {
		t.Run(name, func(t *testing.T) {
			x := newAgentReferenceFixture(t)
			x.update(t, true)
			p := x.discover(t)
			ctx, tx := x.final(p)
			r, service, want := x.change.Clone(), x.service, f.Forbidden
			switch name {
			case "foreign-tx":
				tx, want = f.NewTx(), f.InvalidArgument
			case "foreign-issuer":
				service, _ = NewAgentReferences(x.store, x.service.state().selection, x)
			case "missing-witness":
				ctx = context.Background()
			case "owner-reject":
				x.ownerErr = fault(f.Forbidden)
			case "session-revoked":
				x.selection.sessionError, want = fault(f.Unauthenticated), f.Unauthenticated
			case "changed-revision":
				r.PlanRevision++
			case "missing-lock":
				x.store.locks, want = nil, f.InvalidState
			case "missing-role":
				x.store.references, want = x.store.references[:1], f.ResourceBusy
			case "foreign-project":
				x.store.references[0].Project, want = mustID[id.Project](t).String(), f.ResourceBusy
			case "stale-owner-version":
				x.store.references[1].Version++
				want = f.ResourceBusy
			case "stale-model":
				x.selection.modelVersion++
				want = f.ResourceBusy
			case "stale-provider":
				x.selection.providerVersion++
				want = f.ResourceBusy
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				want = f.DependencyUnavailable
			}
			transactions, acquisitions := x.store.transactions, x.store.acquisitions
			requireCode(t, service.ApplyAgentReferencesInTx(ctx, tx, r, p), want)
			if x.store.writes != 0 || x.store.transactions != transactions || x.store.acquisitions != acquisitions {
				t.Fatal("refusal wrote index or acquired late resources")
			}
		})
	}
}

func TestAgentReferencesMissingBindingAndSQLFailure(t *testing.T) {
	x := newAgentReferenceFixture(t)
	var absent *agentReferenceFixture
	for _, owner := range []mc.AgentReferenceOwnerAuthority{nil, absent} {
		_, err := NewAgentReferences(x.store, x.service.state().selection, owner)
		requireCode(t, err, f.DependencyUnbound)
	}
	_, err := NewAgentReferences(&projectScopeStore{}, x.service.state().selection, x)
	requireCode(t, err, f.DependencyUnbound)
	var zero *AgentReferenceService
	_, err = zero.DiscoverAgentReferences(context.Background(), mc.AgentReferenceChange{})
	requireCode(t, err, f.DependencyUnbound)
	// A create with an existing foreign/current index is never treated as empty.
	x.store.references = agentReferenceRecords(x.change, &x.change.After, 1)
	_, err = x.service.DiscoverAgentReferences(context.Background(), x.change)
	requireCode(t, err, f.ResourceBusy)
	x.store.references = nil
	p := x.discover(t)
	ctx, tx := x.final(p)
	failure := errors.New("controlled SQL failure")
	x.store.writeErr = failure
	err = x.service.ApplyAgentReferencesInTx(ctx, tx, x.change, p)
	requireCode(t, err, f.DependencyUnavailable)
	if !errors.Is(err, failure) || len(x.store.references) != 0 {
		t.Fatal("lost original SQL failure or accepted failed participant")
	}
}
