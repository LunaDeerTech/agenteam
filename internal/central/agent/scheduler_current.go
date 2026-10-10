package agent

import (
	"context"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// SchedulerCurrent fixes the original Agent/Project owner pair and a real
// Scheduler intent provider. Construction performs no I/O and issues no grant.
type SchedulerCurrent struct {
	agents   *Authority
	projects c.SchedulerProjectGate
	intents  c.SchedulerCurrentAuthority
}

func NewSchedulerCurrent(agents *Authority, intents c.SchedulerCurrentAuthority) (*SchedulerCurrent, error) {
	if agents == nil || agents.state == nil || nilPort(intents) {
		return nil, fault(f.DependencyUnbound)
	}
	projects, ok := agents.state.projects.(c.SchedulerProjectGate)
	if !ok || nilPort(projects) {
		return nil, fault(f.DependencyUnbound)
	}
	return &SchedulerCurrent{agents: agents, projects: projects, intents: intents}, nil
}

func schedulerCurrentLocks(project i.ProjectID, agent i.AgentID) []f.LockRequest {
	schedule, _ := f.ProjectScheduleLock(project.String())
	return []f.LockRequest{projectLock(project, f.Shared), {Key: schedule, Mode: f.Exclusive}, agentLock(agent, f.Shared)}
}

func (a *SchedulerCurrent) RequireSchedulerCurrentInTx(ctx context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, agent i.AgentID) (c.AgentRef, error) {
	if a == nil || a.agents == nil || a.agents.state == nil || nilPort(a.projects) || nilPort(a.intents) {
		return c.AgentRef{}, fault(f.DependencyUnbound)
	}
	if ctx == nil {
		return c.AgentRef{}, invalid()
	}
	if err := ctx.Err(); err != nil {
		return c.AgentRef{}, err
	}
	if !tx.Valid() || project.Validate() != nil || agent.Validate() != nil {
		return c.AgentRef{}, invalid()
	}
	if actor.Validate() != nil {
		return c.AgentRef{}, fault(f.Unauthenticated)
	}
	details := actor.Details()
	if details.Kind != i.Service || details.ServiceName != i.Scheduler || details.ProjectID != project.String() {
		return c.AgentRef{}, fault(f.Forbidden)
	}
	if _, err := f.ParseID[struct{}](details.CauseRef); err != nil {
		return c.AgentRef{}, fault(f.Forbidden)
	}
	state := a.agents.state
	x, err := state.store.InTx(tx)
	if err != nil {
		return c.AgentRef{}, executionConfigurationError(err)
	}
	if nilPort(x) {
		return c.AgentRef{}, fault(f.DependencyUnbound)
	}
	if err = state.store.RequireHeldLocks(ctx, tx, schedulerCurrentLocks(project, agent)); err != nil {
		return c.AgentRef{}, executionConfigurationError(err)
	}
	witness, err := a.intents.RequireSchedulerCurrentIntentInTx(ctx, tx, actor, project, agent)
	if cancelled := ctx.Err(); cancelled != nil {
		return c.AgentRef{}, cancelled
	}
	if err != nil {
		return c.AgentRef{}, executionConfigurationError(err)
	}
	if witness.ProjectID != project || witness.AgentID != agent || witness.DispatchID != details.CauseRef || witness.SprintID.Validate() != nil {
		return c.AgentRef{}, fault(f.Forbidden)
	}
	current, err := a.projects.RequireSchedulerProjectInTx(ctx, tx, project)
	if cancelled := ctx.Err(); cancelled != nil {
		return c.AgentRef{}, cancelled
	}
	if err != nil {
		return c.AgentRef{}, executionConfigurationError(err)
	}
	if current.Project.Validate() != nil || current.Project.ID != project || current.Config.Validate() != nil {
		return c.AgentRef{}, unavailable(nil)
	}
	if current.Project.Lifecycle != pc.Active || !current.Config.Enabled || current.Project.CurrentSprintID == nil || *current.Project.CurrentSprintID != witness.SprintID {
		return c.AgentRef{}, fault(f.InvalidState)
	}
	value, err := initializedAgent(ctx, x, project, agent)
	// Cancellation cannot detach the canonical/receipt reads or publish facts
	// after they return. Their actual synchronous tail precedes this result.
	if cancelled := ctx.Err(); cancelled != nil {
		return c.AgentRef{}, cancelled
	}
	if err != nil {
		return c.AgentRef{}, executionConfigurationError(err)
	}
	ref := c.AgentRef{ProjectID: project, AgentID: agent, ConfigVersion: value.Fields().Core.Version}
	return ref, nil
}

var _ c.SchedulerCurrentReferences = (*SchedulerCurrent)(nil)
