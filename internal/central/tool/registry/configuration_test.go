package registry

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type configurationAuth struct{}

func (configurationAuth) RequireCurrentSession(context.Context, f.Tx, id.Actor) error { return nil }
func (configurationAuth) AuthorizeProject(_ context.Context, _ f.Tx, a id.Actor, p id.ProjectID, intent id.AccessIntent) (id.AccessGrant, error) {
	scope, _ := id.InProject(p)
	now, _ := f.NewInstant(time.Unix(1, 0))
	return id.NewAccessGrant(a, scope, intent, now, 1)
}

type ownerPlanFixture struct{ locks []f.LockRequest }

func (p ownerPlanFixture) RequiredLocks() []f.LockRequest { return p.locks }

type ownerFixture struct {
	discover, checked int
	err               error
	tx                f.Tx
}

func (o *ownerFixture) DiscoverToolReferenceOwner(context.Context, ac.ToolReferenceChange) (ac.ToolReferenceOwnerPlan, error) {
	o.discover++
	return ownerPlanFixture{}, nil
}
func (o *ownerFixture) CheckToolReferenceOwnerAppliedInTx(_ context.Context, tx f.Tx, _ ac.ToolReferenceChange, _ ac.ToolReferenceOwnerPlan) error {
	o.checked++
	o.tx = tx
	return o.err
}
func configurationFixture(t *testing.T) (*Configuration, *metadataStore, *ownerFixture, ac.ToolConfigurationRequest) {
	t.Helper()
	r, s, _ := metadataFixture(t)
	r.data().auth = Authorizations{Sessions: configurationAuth{}, Projects: configurationAuth{}}
	owner := &ownerFixture{}
	c, err := NewConfiguration(r, owner, "")
	if err != nil {
		t.Fatal(err)
	}
	u, err := f.ParseID[id.User]("018f0000-0000-7000-8000-000000000003")
	if err != nil {
		t.Fatal(err)
	}
	session, err := f.ParseID[id.Session]("018f0000-0000-7000-8000-000000000004")
	if err != nil {
		t.Fatal(err)
	}
	a, err := id.NewHuman(u, session)
	if err != nil {
		t.Fatal(err)
	}
	p, err := f.ParseID[id.Project]("018f0000-0000-7000-8000-000000000005")
	if err != nil {
		t.Fatal(err)
	}
	agent, err := f.ParseID[id.Agent]("018f0000-0000-7000-8000-000000000006")
	if err != nil {
		t.Fatal(err)
	}
	command, err := f.NewCommandIdentity("project", []string{p.String()}, "agent.create", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	on := true
	return c, s, owner, ac.ToolConfigurationRequest{Actor: a, ProjectID: p, AgentID: agent, Command: command, RequestedToolIDs: []id.ToolID{}, InstallSkillEnabled: &on}
}
func TestConfigurationMissingInstallSourceCannotBecomeEmptySuccess(t *testing.T) {
	c, s, _, request := configurationFixture(t)
	for _, enabled := range []bool{true, false} {
		request.InstallSkillEnabled = &enabled
		if plan, err := c.DiscoverConfigurationTools(context.Background(), request); err == nil || plan != nil {
			t.Fatal("missing default directory silently succeeded")
		}
	}
	if s.queries != 0 || len(s.writes) != 0 {
		t.Fatal("missing default source accessed tool rows")
	}
	if _, err := NewConfiguration(c.state().registry, nil, ""); err == nil {
		t.Fatal("unbound Agent owner accepted")
	}
}

type forgedConfigurationPlan struct{}

func (forgedConfigurationPlan) RequiredLocks() []f.LockRequest { return nil }
func (forgedConfigurationPlan) ResolvedToolIDs() []id.ToolID   { return []id.ToolID{} }
func TestConfigurationPlanCannotBeForgedOrCrossInstance(t *testing.T) {
	c, s, _, request := configurationFixture(t)
	if err := c.RequireConfigurationToolsInTx(context.Background(), s.tx, request, forgedConfigurationPlan{}); err == nil {
		t.Fatal("arbitrary interface authorized")
	}
	other, err := NewConfiguration(c.state().registry, c.state().owner, "")
	if err != nil {
		t.Fatal(err)
	}
	d := directoryPlanData{issuer: other.state(), request: request.Clone()}
	if err = c.RequireConfigurationToolsInTx(context.Background(), s.tx, request, directoryPlan{data: func() directoryPlanData { return d }}); err == nil || s.queries != 0 {
		t.Fatal("cross-instance plan reached SQL")
	}
	tool, err := f.ParseID[id.Tool]("018f0000-0000-7000-8000-000000000007")
	if err != nil {
		t.Fatal(err)
	}
	d.resolved = []id.ToolID{tool}
	d.locks = []f.LockRequest{RegistryLock(f.Shared)}
	p := directoryPlan{data: func() directoryPlanData { return d }}
	ids := p.ResolvedToolIDs()
	ids[0] = id.ToolID{}
	locks := p.RequiredLocks()
	locks[0].Mode = f.Exclusive
	if p.ResolvedToolIDs()[0] != tool || p.RequiredLocks()[0].Mode != f.Shared {
		t.Fatal("plan mutable through projection")
	}
}
func TestEmptyToolReferencesStillNeedAppliedOwnerWitness(t *testing.T) {
	c, s, owner, request := configurationFixture(t)
	change := ac.ToolReferenceChange{Actor: request.Actor, ProjectID: request.ProjectID, AgentID: request.AgentID, Command: request.Command, PlanRevision: 1, ResultOwnerVersion: 1, Before: []id.ToolID{}, After: []id.ToolID{}}
	plan, err := c.DiscoverToolReferences(context.Background(), change)
	if err != nil {
		t.Fatal(err)
	}
	if owner.discover != 1 {
		t.Fatal("empty refs skipped owner discovery")
	}
	s.held = plan.RequiredLocks()
	command, _ := f.CommandLock(change.Command)
	user, _ := f.UserLock(change.Actor.Details().UserID)
	if err = s.RequireHeldLocks(context.Background(), s.tx, []f.LockRequest{{Key: command, Mode: f.Exclusive}, {Key: user, Mode: f.Exclusive}}); err != nil {
		t.Fatal("reference write weakened original command/user gates")
	}
	private := errors.New("private-owner-witness-canary")
	owner.err = private
	err = c.ApplyToolReferencesInTx(context.Background(), s.tx, change, plan)
	if err == nil || !errors.Is(err, private) || strings.Contains(err.Error(), "canary") || owner.checked != 1 || owner.tx != s.tx || s.queries != 0 || len(s.writes) != 0 {
		t.Fatal("empty references bypassed witness or lost original same Tx")
	}
	change.PlanRevision = 2
	if err = c.ApplyToolReferencesInTx(context.Background(), s.tx, change, plan); err == nil || owner.checked != 1 {
		t.Fatal("changed planned revision reused witness")
	}
}
func TestRegistryLockUnionNeverDowngradesRequiredExclusive(t *testing.T) {
	shared := RegistryLock(f.Shared)
	exclusive := RegistryLock(f.Exclusive)
	got, err := normalizedLocks([]f.LockRequest{shared, exclusive, shared})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Mode != f.Exclusive {
		t.Fatal("merged lock set weakened exclusive")
	}
}
