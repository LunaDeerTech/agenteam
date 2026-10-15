package project

import (
	"context"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"testing"
)

type mcpRuntimeProofControl struct {
	auditFactFunc
	require func(context.Context, f.Tx, id.Actor, id.ProjectID) error
}

func (p *mcpRuntimeProofControl) RequireMCPRuntimeInTx(ctx context.Context, tx f.Tx, actor id.Actor, project id.ProjectID) error {
	return p.require(ctx, tx, actor, project)
}

func TestMCPRuntimeProjectGateKeepsOriginalProofMandatory(t *testing.T) {
	fixture := newAuditGateFixture(t, nil)
	scope, _ := id.InProject(fixture.project)
	request := testID[struct{}](t).String()
	reg, _ := id.RegisterService(id.MCPRuntime)
	actor, _ := reg.Actor(request, scope)
	calls := 0
	ctx := context.Background()
	proof := &mcpRuntimeProofControl{require: func(got context.Context, tx f.Tx, a id.Actor, p id.ProjectID) error {
		calls++
		if got != ctx || tx != fixture.store.tx || !a.Equal(actor) || p != fixture.project {
			t.Fatal("changed original source tuple")
		}
		return fault(f.Forbidden)
	}}
	fixture.a.state().auditFacts[ac.MCPProducer] = proof
	hasCode(t, fixture.a.RequireMCPRuntimeProjectInTx(ctx, fixture.store.tx, actor, fixture.project), f.Forbidden)
	if calls != 1 {
		t.Fatal("private domain proof not required")
	}
	for _, phase := range []c.Lifecycle{c.Archiving, c.Archived, c.Deleting} {
		fixture.lifecycle = phase
		hasCode(t, fixture.a.RequireMCPRuntimeProjectInTx(ctx, fixture.store.tx, actor, fixture.project), f.ProjectNotActive)
	}
	fixture.lifecycle = c.Active
	fixture.initialized = false
	hasCode(t, fixture.a.RequireMCPRuntimeProjectInTx(ctx, fixture.store.tx, actor, fixture.project), f.ProjectNotActive)
	fixture.initialized = true
	hasCode(t, fixture.a.RequireMCPRuntimeProjectInTx(ctx, fixture.store.tx, fixture.actor, fixture.project), f.Forbidden)
	if calls != 1 {
		t.Fatal("closed Project or Human reached runtime provider")
	}
	delete(fixture.a.state().auditFacts, ac.MCPProducer)
	hasCode(t, fixture.a.RequireMCPRuntimeProjectInTx(ctx, fixture.store.tx, actor, fixture.project), f.DependencyUnbound)
}

func TestMCPRuntimeAuditRetainsCurrentProjectAndExactRefreshFact(t *testing.T) {
	fixture := newAuditGateFixture(t, nil)
	scope, _ := id.InProject(fixture.project)
	request := testID[struct{}](t).String()
	reg, _ := id.RegisterService(id.MCPRuntime)
	actor, _ := reg.Actor(request, scope)
	metadata, _ := ac.MCPMetadata(ac.MCPDiscoveryRefresh, ac.MCPMetadataFields{ConfigID: request, ConnectionID: request, Version: 1})
	resource, _ := ac.NewResource(ac.MCPConnectionResource, request)
	entry, err := ac.NewEntry(ac.EntryFields{Actor: actor, Scope: scope, Action: ac.MCPDiscoveryRefresh, Resource: resource, Outcome: ac.Success, Metadata: metadata})
	if err != nil {
		t.Fatal(err)
	}
	key, _ := ac.NewAppendKey(ac.MCPProducer, request, 0)
	calls := 0
	fixture.a.state().auditFacts[ac.MCPProducer] = auditFactFunc(func(_ context.Context, tx f.Tx, e ac.Entry, k ac.AppendKey) error {
		calls++
		if tx != fixture.store.tx || !e.Fields().Actor.Equal(actor) || k.Details() != key.Details() {
			t.Fatal("exact fact changed")
		}
		return fault(f.Forbidden)
	})
	hasCode(t, fixture.a.CheckAppendInTx(context.Background(), fixture.store.tx, entry, key), f.Forbidden)
	fixture.lifecycle = c.Archived
	hasCode(t, fixture.a.CheckAppendInTx(context.Background(), fixture.store.tx, entry, key), f.ProjectNotActive)
	if calls != 1 {
		t.Fatal("current gate bypassed")
	}
}
