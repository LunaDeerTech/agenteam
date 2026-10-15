package project

import (
	"context"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type MeetingExecutionAccess struct {
	projects *Authority
	intents  ec.MeetingIntentAuthority
}

func NewMeetingExecutionAccess(projects *Authority, intents ec.MeetingIntentAuthority) (*MeetingExecutionAccess, error) {
	if projects.state() == nil || nilPort(intents) {
		return nil, fault(f.DependencyUnbound)
	}
	return &MeetingExecutionAccess{projects: projects, intents: intents}, nil
}
func (a *MeetingExecutionAccess) RequireExecutionProjectInTx(ctx context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, agent i.AgentID, intent i.AccessIntent) error {
	if a == nil || a.projects.state() == nil || nilPort(a.intents) {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil || !tx.Valid() || project.Validate() != nil || agent.Validate() != nil {
		return invalid()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if actor.Validate() != nil {
		return fault(f.Unauthenticated)
	}
	d := actor.Details()
	if d.Kind != i.Service || d.ServiceName != i.MeetingRuntime || d.ProjectID != project.String() || (intent != i.Read && intent != i.Launch) {
		return fault(f.Forbidden)
	}
	if _, err := f.ParseID[struct{}](d.CauseRef); err != nil {
		return fault(f.Forbidden)
	}
	store := a.projects.state().store
	x, err := store.InTx(tx)
	if err != nil {
		return preparationProjectError(err)
	}
	agentKey, _ := f.AgentLock(agent.String())
	if err = store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(project, f.Shared), {Key: agentKey, Mode: f.Shared}}); err != nil {
		return preparationProjectError(err)
	}
	if err = a.intents.RequireMeetingIntentInTx(ctx, tx, actor, project, agent, intent); err != nil {
		return preparationProjectError(err)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	p, err := loadProject(ctx, x, project)
	if err != nil {
		return preparationProjectError(err)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if p == nil {
		return fault(f.NotFound)
	}
	if p.ref.ID != project {
		return unavailable(nil)
	}
	initialized := c.InitializationPending
	if p.initialized {
		initialized = c.Initialized
	}
	return c.CheckOwnerGate(p.ref.Lifecycle, initialized, intent)
}

var _ ec.ServiceProjectAccess = (*MeetingExecutionAccess)(nil)
