package mcp

import (
	"context"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"testing"
)

type sourceTailContextKey struct{}
type sourceTailProof struct {
	actor id.Actor
	plan  oc.AccessDependencies
	file  string
	lease oc.LeaseID
	calls int
}

func (p *sourceTailProof) MCPContentReadActor(ctx context.Context) (id.Actor, error) {
	if ctx.Value(sourceTailContextKey{}) != p {
		return id.Actor{}, fail(f.Forbidden)
	}
	return p.actor, nil
}
func (p *sourceTailProof) DiscoverMCPContentRead(context.Context, ec.MCPContentReadRequest) (oc.AccessDependencies, error) {
	return oc.AccessDependencies{}, fail(f.Forbidden)
}
func (p *sourceTailProof) ValidateMCPContentReadInTx(context.Context, f.Tx, ec.MCPContentReadRequest, oc.AccessDependencies) error {
	return fail(f.Forbidden)
}
func (p *sourceTailProof) RequireMCPContentReadInTx(context.Context, f.Tx, ec.MCPContentReadRequest) error {
	return fail(f.Forbidden)
}
func (p *sourceTailProof) DiscoverMCPContentReadTail(ctx context.Context, r ec.MCPContentReadRequest, access oc.AccessRequest) (oc.AccessDependencies, error) {
	p.calls++
	if ctx.Value(sourceTailContextKey{}) != p || r.Validate() != nil || !r.Actor.Equal(p.actor) || r.Reference.Details().FileID != p.file || access.Details().LeaseID != p.lease {
		return oc.AccessDependencies{}, fail(f.Forbidden)
	}
	return p.plan, nil
}
func (p *sourceTailProof) ValidateMCPContentReadTailInTx(ctx context.Context, _ f.Tx, r ec.MCPContentReadRequest, access oc.AccessRequest, expected oc.AccessDependencies) error {
	plan, err := p.DiscoverMCPContentReadTail(ctx, r, access)
	if err != nil {
		return err
	}
	if !plan.Equal(expected) {
		return fail(f.ConfirmationStale)
	}
	return nil
}

type sourceTailStore struct {
	*contentTailStore
	file, config string
}

func (s *sourceTailStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx {
		return nil, fail(f.InvalidArgument)
	}
	return s, nil
}
func (s *sourceTailStore) QueryRow(context.Context, string, ...any) postgres.Row {
	s.queries++
	return sourceTailRow{s}
}

type sourceTailRow struct{ store *sourceTailStore }

func (r sourceTailRow) Scan(dest ...any) error {
	s := r.store
	if len(dest) == 1 {
		*dest[0].(*bool) = s.objectExists
		return nil
	}
	if len(dest) != 11 {
		return fail(f.InvalidState)
	}
	values := []string{s.fact.execution, s.fact.operation, s.fact.attempt, s.fact.request, s.fact.uri, s.fact.connection, s.fact.runtime, s.config}
	for n, value := range values {
		*dest[n].(*string) = value
	}
	*dest[8].(*f.Version) = 1
	*dest[9].(*string) = s.file
	*dest[10].(*int64) = 1
	return nil
}

func TestMCPContentSourceTailKeepsExactPrivateInputAndObject(t *testing.T) {
	a, w, base, resolver, _, _ := contentTailFixture(t)
	base.fact.state = "ready"
	store := &sourceTailStore{contentTailStore: base, file: contentTailID[struct{}](t, "19").String(), config: contentTailID[struct{}](t, "20").String()}
	a.direct.store = store
	plan, _ := oc.NewAccessDependencies(f.Digest(digest([]byte("original-reader-tail"))), w.locks)
	lease := contentTailID[oc.Lease](t, "21")
	proof := &sourceTailProof{actor: w.call.Actor, plan: plan, file: store.file, lease: lease}
	a.reads = proof
	access, err := oc.NewMaintenanceAccess(oc.AccessRequestDetails{Operation: oc.ReleaseReaderAccess, InstanceID: contentTailID[oc.Process](t, "11"), ObjectID: resolver.object, LeaseID: lease})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Discover(context.Background(), access); err == nil {
		t.Fatal("bare callback reached reader cleanup")
	}
	if resolver.calls != 0 || store.queries != 0 {
		t.Fatal("bare source context reached persistent mapping")
	}
	ctx := context.WithValue(context.Background(), sourceTailContextKey{}, proof)
	actual, err := a.Discover(ctx, access)
	if err != nil || !actual.Equal(plan) {
		t.Fatal("original source tail rejected", err)
	}
	if err = a.ValidateInTx(ctx, store.tx, access, actual); err != nil {
		t.Fatal("same-Tx tail rejected", err)
	}
	store.objectExists = false
	if err = a.ValidateInTx(ctx, store.tx, access, actual); err == nil {
		t.Fatal("foreign Object accepted")
	}
	store.objectExists = true
	store.file = contentTailID[struct{}](t, "22").String()
	if err = a.ValidateInTx(ctx, store.tx, access, actual); err == nil {
		t.Fatal("substituted canonical reference accepted")
	}
	store.file = proof.file
	changed := access.Details()
	changed.LeaseID = contentTailID[oc.Lease](t, "23")
	other, _ := oc.NewMaintenanceAccess(changed)
	if err = a.ValidateInTx(ctx, store.tx, other, actual); err == nil {
		t.Fatal("substituted original reader accepted")
	}
	base.denyLocks = true
	if err = a.ValidateInTx(ctx, store.tx, access, actual); err == nil {
		t.Fatal("missing original locks accepted")
	}
}
