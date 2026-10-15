package secret

import (
	"context"
	"errors"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

type mcpObservationProofKey struct{}
type mcpObservationProof struct{ active bool }

// The upstream private proof is controlled here. Real stopped-process/claim
// admission remains the MCP PublishedConnection integration gate.
type mcpObservationPlanner struct {
	sc.UsageAuthority
	proof     *mcpObservationProof
	request   sc.UsageRequest
	issuer    sc.PlanIssuer
	locks     []f.LockRequest
	discovery int
	validated int
}

func (p *mcpObservationPlanner) require(ctx context.Context, r sc.UsageRequest) error {
	got, err := sc.UsageBinding(r)
	want, _ := sc.UsageBinding(p.request)
	if ctx.Value(mcpObservationProofKey{}) != p.proof || !p.proof.active || err != nil || got != want {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	return nil
}
func (p *mcpObservationPlanner) DiscoverUsage(ctx context.Context, r sc.UsageRequest) (sc.UsageDependencies, error) {
	p.discovery++
	if err := p.require(ctx, r); err != nil {
		return sc.UsageDependencies{}, err
	}
	binding, _ := sc.UsageBinding(r)
	return sc.NewUsageDependencies(p.issuer, binding, binding, p.locks)
}
func (p *mcpObservationPlanner) ValidateUsageInTx(ctx context.Context, tx f.Tx, r sc.UsageRequest, plan sc.UsageDependencies) error {
	p.validated++
	if err := p.require(ctx, r); err != nil {
		return err
	}
	binding, _ := sc.UsageBinding(r)
	if !tx.Valid() || !plan.Matches(p.issuer, binding, binding) {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	return nil
}

func mcpObservationFixture(t *testing.T) (*Service, *projectAuditUnitStore, *mcpObservationPlanner, context.Context) {
	t.Helper()
	scope, _ := id.InProject(projectAuditID[id.Project](t))
	owner, _ := sc.NewCredentialLeaseOwner(sc.MCPConnectionRequestOwner, projectAuditID[f.Request](t).String())
	registration, _ := id.RegisterService(id.SecretService)
	actor, _ := registration.Actor(owner.Details().ID, scope)
	ref, _ := sc.NewCredentialRef(projectAuditID[sc.Credential](t), scope)
	r := sc.UsageRequest{Actor: actor, Ref: ref, Purpose: sc.MCP, LeaseOwner: owner, LeaseID: projectAuditID[sc.Lease](t), Action: sc.ReleaseLeaseUsage}
	key, err := f.RecordLock(f.ReferenceRecordLock, "original-mcp-request:"+owner.Details().ID)
	if err != nil || r.Validate() != nil {
		t.Fatal("invalid observation fixture", err)
	}
	p := &mcpObservationPlanner{request: r, proof: &mcpObservationProof{active: true}, issuer: sc.NewPlanIssuer(), locks: []f.LockRequest{{Key: key, Mode: f.Exclusive}}}
	store := &projectAuditUnitStore{tx: f.NewTx()}
	state := &serviceState{store: store, auth: Authorizations{Usage: p}, usageIssuer: sc.NewPlanIssuer()}
	s := &Service{data: func() *serviceState { return state }}
	return s, store, p, context.WithValue(context.Background(), mcpObservationProofKey{}, p.proof)
}

func TestMCPRequestLeaseObservationPlansBeforeOriginalAcquire(t *testing.T) {
	s, store, upstream, ctx := mcpObservationFixture(t)
	r := upstream.request
	plan, err := s.DiscoverMCPRequestLeaseObservation(ctx, r)
	if err != nil || plan.Validate() != nil || upstream.discovery != 1 || store.queries != 0 {
		t.Fatal("dependency planning read an unacquired lease or lost private proof", err)
	}
	store.locks = plan.RequiredLocks()
	refLock, _ := f.AggregateLock(f.CredentialRefAggregate, r.Ref.Details().ID.String())
	leaseLock, _ := f.RecordLock(f.ReferenceRecordLock, "secret-lease:"+r.LeaseID.String())
	projectLock, _ := f.ProjectLock(r.Ref.Details().Scope.Details().ProjectID)
	for _, want := range append(upstream.locks, f.LockRequest{Key: refLock, Mode: f.Exclusive}, f.LockRequest{Key: leaseLock, Mode: f.Exclusive}, f.LockRequest{Key: projectLock, Mode: f.Shared}) {
		found := false
		for _, got := range store.locks {
			found = found || got.Key.Canonical() == want.Key.Canonical() && got.Mode == want.Mode
		}
		if !found {
			t.Fatal("observation omitted an original provider or Secret lock")
		}
	}
	store.read = func(sql string, args []any) postgres.Row {
		if !strings.HasPrefix(sql, "SELECT credential_id::text") || len(args) != 1 || args[0] != r.LeaseID.String() {
			t.Fatal("observation did not read exactly the original lease")
		}
		return projectAuditUnitRow{err: pgx.ErrNoRows}
	}
	state, err := s.ObserveMCPRequestLeaseInTx(ctx, store.tx, r, plan)
	if err != nil || state.Exists || state.Released || store.queries != 1 || upstream.validated != 1 {
		t.Fatal("original absence was not proven by the authorized transaction", err)
	}
	// Existing leases retain their actual state; the observer does not release
	// them. This Store deliberately has no mutation implementation.
	for _, released := range []bool{false, true} {
		store.read = func(string, []any) postgres.Row {
			return projectAuditUnitRow{values: []any{r.Ref.Details().ID.String(), "project", r.Ref.Details().Scope.Details().ProjectID, string(sc.MCPConnectionRequestOwner), r.LeaseOwner.Details().ID, string(sc.MCP), released}}
		}
		state, err = s.ObserveMCPRequestLeaseInTx(ctx, store.tx, r, plan)
		if err != nil || !state.Exists || state.Released != released {
			t.Fatal("observation changed the existing lease state", err)
		}
	}
	store.read = func(string, []any) postgres.Row {
		return projectAuditUnitRow{values: []any{projectAuditID[sc.Credential](t).String(), "project", r.Ref.Details().Scope.Details().ProjectID, string(sc.MCPConnectionRequestOwner), r.LeaseOwner.Details().ID, string(sc.MCP), true}}
	}
	state, err = s.ObserveMCPRequestLeaseInTx(ctx, store.tx, r, plan)
	if err == nil || state.Exists || state.Released {
		t.Fatal("foreign canonical lease became an absent or released observation")
	}
	store.read = func(string, []any) postgres.Row { return projectAuditUnitRow{err: pgx.ErrNoRows} }
	_, err = s.DiscoverUsage(ctx, r)
	projectAuditCode(t, err, f.NotFound) // ordinary Release discovery is unchanged.
}

func TestMCPRequestLeaseObservationRequiresProofTupleAndAllLocks(t *testing.T) {
	s, store, upstream, ctx := mcpObservationFixture(t)
	r := upstream.request
	if _, err := s.DiscoverMCPRequestLeaseObservation(context.Background(), r); err == nil || store.queries != 0 {
		t.Fatal("bare request DTO acquired an observation plan")
	}
	plan, err := s.DiscoverMCPRequestLeaseObservation(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	store.locks = plan.RequiredLocks()
	for _, change := range []func(*sc.UsageRequest){
		func(v *sc.UsageRequest) { v.LeaseID = projectAuditID[sc.Lease](t) },
		func(v *sc.UsageRequest) {
			v.Ref, _ = sc.NewCredentialRef(projectAuditID[sc.Credential](t), r.Ref.Details().Scope)
		},
		func(v *sc.UsageRequest) { v.Purpose = sc.Model },
	} {
		other := r
		change(&other)
		if _, err = s.ObserveMCPRequestLeaseInTx(ctx, store.tx, other, plan); err == nil || store.queries != 0 {
			t.Fatal("changed tuple reached Secret observation")
		}
	}
	store.locks = nil
	if _, err = s.ObserveMCPRequestLeaseInTx(ctx, store.tx, r, plan); err == nil || store.queries != 0 {
		t.Fatal("missing locks reached observation")
	}
	store.locks = plan.RequiredLocks()
	upstream.proof.active = false
	if _, err = s.ObserveMCPRequestLeaseInTx(ctx, store.tx, r, plan); err == nil || store.queries != 0 {
		t.Fatal("withdrawn private proof reached observation")
	}
	upstream.proof.active = true
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = s.DiscoverMCPRequestLeaseObservation(cancelled, r); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled plan discovery continued", err)
	}
}

func TestMCPRequestLeaseObservationPlanKeepsClosedRequestShape(t *testing.T) {
	s, store, upstream, ctx := mcpObservationFixture(t)
	r := upstream.request
	for _, change := range []func(*sc.UsageRequest){
		func(v *sc.UsageRequest) { v.Action = sc.AcquireLeaseUsage },
		func(v *sc.UsageRequest) { v.Action = sc.ReadLeaseUsage },
		func(v *sc.UsageRequest) { v.Purpose = sc.Model },
		func(v *sc.UsageRequest) {
			v.LeaseOwner, _ = sc.NewCredentialLeaseOwner(sc.ExecutionOwner, r.LeaseOwner.Details().ID)
		},
		func(v *sc.UsageRequest) {
			reg, _ := id.RegisterService(id.ModelRuntime)
			v.Actor, _ = reg.Actor(r.LeaseOwner.Details().ID, r.Ref.Details().Scope)
		},
		func(v *sc.UsageRequest) {
			reg, _ := id.RegisterService(id.SecretService)
			v.Actor, _ = reg.Actor(r.LeaseOwner.Details().ID, id.SystemScope())
			v.Ref, _ = sc.NewCredentialRef(r.Ref.Details().ID, id.SystemScope())
		},
		func(v *sc.UsageRequest) {
			reg, _ := id.RegisterService(id.SecretService)
			v.Actor, _ = reg.Actor(projectAuditID[f.Request](t).String(), r.Ref.Details().Scope)
		},
		func(v *sc.UsageRequest) {
			reg, _ := id.RegisterService(id.SecretService)
			scope, _ := id.InProject(projectAuditID[id.Project](t))
			v.Actor, _ = reg.Actor(r.LeaseOwner.Details().ID, scope)
		},
		func(v *sc.UsageRequest) {
			v.Actor, _ = id.NewHuman(projectAuditID[id.User](t), projectAuditID[id.Session](t))
		},
	} {
		other := r
		change(&other)
		if _, err := s.DiscoverMCPRequestLeaseObservation(ctx, other); err == nil || upstream.discovery != 0 || store.queries != 0 {
			t.Fatal("unsupported observation shape reached the provider")
		}
	}
}
