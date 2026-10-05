package audit

import (
	"context"
	"testing"

	c "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type projectAppendPort struct {
	c.ProjectAuthority
	calls int
	err   error
}

func (p *projectAppendPort) CheckAppendInTx(_ context.Context, tx foundation.Tx, entry c.Entry, key c.AppendKey) error {
	p.calls++
	if !tx.Valid() {
		return foundation.NewFault(foundation.InvalidArgument, foundation.NotStarted)
	}
	return p.err
}
func TestProjectAuditUsesOnlyDedicatedAuthoritativeDispatch(t *testing.T) {
	raw := "01900000-0000-7000-8000-000000000001"
	project, _ := foundation.ParseID[identity.Project](raw)
	scope, _ := identity.InProject(project)
	actor := testEntry(t, "01900000-0000-7000-8000-000000000002", "", "").Fields().Actor
	v := foundation.Version(1)
	metadata, e := c.ProjectMetadata(c.ProjectCreateAccepted, c.ProjectMetadataFields{ProjectID: raw, InitiatorID: actor.Details().UserID, ProjectVersion: 1, CreationID: raw, CreationVersion: &v})
	if e != nil {
		t.Fatal(e)
	}
	resource, _ := c.NewResource(c.ProjectCreationResource, raw)
	entry, e := c.NewEntry(c.EntryFields{Scope: scope, Actor: actor, Action: c.ProjectCreateAccepted, Outcome: c.Success, Resource: resource, Metadata: metadata})
	if e != nil {
		t.Fatal(e)
	}
	key, _ := c.NewAppendKey(c.ProjectProducer, raw, 0)
	port := &projectAppendPort{}
	s := &Service{auth: Authorizations{Projects: port, Sessions: sessionPort(func(context.Context, foundation.Tx, identity.Actor) error {
		t.Fatal("generic Session/Mutate path used for unavailable creation")
		return nil
	})}}
	if e = s.authorizeAppend(context.Background(), foundation.NewTx(), entry, key); e != nil || port.calls != 1 {
		t.Fatal("dedicated Project gate missing", e)
	}
	port.err = foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	if s.authorizeAppend(context.Background(), foundation.NewTx(), entry, key) == nil {
		t.Fatal("provider error ignored")
	}
	s.auth.Projects = nil
	if s.authorizeAppend(context.Background(), foundation.NewTx(), entry, key) == nil {
		t.Fatal("unbound provider succeeded")
	}
	reg, _ := identity.RegisterService(identity.ProjectInitialization)
	service, _ := reg.Actor(raw, scope)
	secretKey, _ := c.NewAppendKey(c.SecretProducer, raw, 0)
	if serviceOwns(service, scope, secretKey) {
		t.Fatal("new role widened old Secret service ownership")
	}
}
