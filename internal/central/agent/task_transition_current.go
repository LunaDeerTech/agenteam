package agent

import (
	"context"
	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type TaskTransitionCurrent struct {
	agents *Authority
	tools  c.TaskTransitionAuthority
}

func NewTaskTransitionCurrent(agents *Authority, tools c.TaskTransitionAuthority) (*TaskTransitionCurrent, error) {
	if agents == nil || agents.state == nil || nilPort(tools) {
		return nil, fault(f.DependencyUnbound)
	}
	return &TaskTransitionCurrent{agents, tools}, nil
}

func (a *TaskTransitionCurrent) RequireTaskTransitionCurrentInTx(ctx context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, task string, target i.AgentID) (c.AgentRef, error) {
	if a == nil || a.agents == nil || nilPort(a.tools) {
		return c.AgentRef{}, fault(f.DependencyUnbound)
	}
	if ctx == nil || project.Validate() != nil || target.Validate() != nil {
		return c.AgentRef{}, invalid()
	}
	if actor.Validate() != nil || actor.Details().Kind != i.AgentRun || actor.Details().ProjectID != project.String() {
		return c.AgentRef{}, fault(f.Forbidden)
	}
	st := a.agents.state
	x, err := st.store.InTx(tx)
	if err != nil {
		return c.AgentRef{}, portError(err)
	}
	if err = st.store.RequireHeldLocks(ctx, tx, schedulerCurrentLocks(project, target)); err != nil {
		return c.AgentRef{}, portError(err)
	}
	if err = a.tools.RequireTaskToolInTx(ctx, tx, actor, project, task, "transfer"); err != nil {
		return c.AgentRef{}, portError(err)
	}
	v, err := initializedAgent(ctx, x, project, target)
	if err != nil {
		return c.AgentRef{}, err
	}
	return c.AgentRef{ProjectID: project, AgentID: target, ConfigVersion: v.Fields().Core.Version}, nil
}

var _ c.TaskTransitionReferences = (*TaskTransitionCurrent)(nil)
