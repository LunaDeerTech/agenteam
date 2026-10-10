package project

import (
	"context"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// SchedulerExecutionAccess is composed with the independent Scheduler intent
// authority before the Execution service/driver. It never calls Execution.
type SchedulerExecutionAccess struct {
	projects *Authority
	intents  c.SchedulerIntentAuthority
}

func NewSchedulerExecutionAccess(projects *Authority, intents c.SchedulerIntentAuthority) (*SchedulerExecutionAccess, error) {
	if projects.state() == nil || nilPort(intents) {
		return nil, fault(f.DependencyUnbound)
	}
	return &SchedulerExecutionAccess{projects: projects, intents: intents}, nil
}

func (a *SchedulerExecutionAccess) RequireExecutionProjectInTx(ctx context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, agent i.AgentID, intent i.AccessIntent) error {
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
	if d.Kind != i.Service || d.ServiceName != i.Scheduler || d.ProjectID != project.String() || (intent != i.Read && intent != i.Launch) {
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
	if err = store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(project, f.Shared), schedulerLock(project), {Key: agentKey, Mode: f.Shared}}); err != nil {
		return preparationProjectError(err)
	}
	witness, err := a.intents.RequireSchedulerIntentInTx(ctx, tx, actor, project, agent, intent)
	if cancelled := ctx.Err(); cancelled != nil {
		return cancelled
	}
	if err != nil {
		return preparationProjectError(err)
	}
	if witness.ProjectID != project || witness.AgentID != agent || witness.DispatchID != d.CauseRef || witness.SprintID.Validate() != nil {
		return fault(f.Forbidden)
	}
	p, err := loadProject(ctx, x, project)
	if cancelled := ctx.Err(); cancelled != nil {
		return cancelled
	}
	if err != nil {
		return preparationProjectError(err)
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
	if err = c.CheckOwnerGate(p.ref.Lifecycle, initialized, intent); err != nil {
		return err
	}
	if intent == i.Read {
		// Original outcome lookup is allowed while paused/archived or after a
		// Sprint switch; it does not authorize launch or a Task mutation.
		return nil
	}
	config, err := loadSchedulerConfig(ctx, x, project)
	if err != nil {
		return err
	}
	if !config.Enabled || p.ref.CurrentSprintID == nil || *p.ref.CurrentSprintID != witness.SprintID {
		return fault(f.InvalidState)
	}
	return nil
}

var _ ec.ServiceProjectAccess = (*SchedulerExecutionAccess)(nil)
