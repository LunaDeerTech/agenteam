package project

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"testing"
)

type meetingIntentFunc func(context.Context, f.Tx, i.Actor, i.ProjectID, i.AgentID, i.AccessIntent) error

func (fn meetingIntentFunc) RequireMeetingIntentInTx(ctx context.Context, tx f.Tx, a i.Actor, p i.ProjectID, g i.AgentID, intent i.AccessIntent) error {
	return fn(ctx, tx, a, p, g, intent)
}
func TestMeetingExecutionAccessKeepsHistoryAndRequiresCurrentIntent(t *testing.T) {
	v := newAuditGateFixture(t, nil)
	agent := testID[i.Agent](t)
	scope, _ := i.InProject(v.project)
	registration, _ := i.RegisterService(i.MeetingRuntime)
	actor, _ := registration.Actor(testID[struct{}](t).String(), scope)
	checks := 0
	denied := false
	access, err := NewMeetingExecutionAccess(v.a, meetingIntentFunc(func(ctx context.Context, tx f.Tx, a i.Actor, p i.ProjectID, g i.AgentID, intent i.AccessIntent) error {
		checks++
		if tx != v.store.tx || !a.Equal(actor) || p != v.project || g != agent {
			t.Fatal("lost original intent tuple")
		}
		if denied {
			return fault(f.Forbidden)
		}
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	v.lifecycle = c.Archived
	if err = access.RequireExecutionProjectInTx(context.Background(), v.store.tx, actor, v.project, agent, i.Read); err != nil {
		t.Fatal("archived original outcome became inaccessible", err)
	}
	if err = access.RequireExecutionProjectInTx(context.Background(), v.store.tx, actor, v.project, agent, i.Launch); err == nil {
		t.Fatal("archived project permitted launch")
	}
	v.lifecycle = c.Active
	denied = true
	if err = access.RequireExecutionProjectInTx(context.Background(), v.store.tx, actor, v.project, agent, i.Launch); err == nil {
		t.Fatal("project gate replaced original intent")
	}
	before := checks
	if err = access.RequireExecutionProjectInTx(context.Background(), v.store.tx, v.actor, v.project, agent, i.Read); err == nil || checks != before {
		t.Fatal("Human actor reached service intent port")
	}
}
