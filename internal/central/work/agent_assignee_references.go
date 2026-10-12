package work

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func (a *Authority) RequireAgentUnassignedInTx(ctx context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, agent i.AgentID) error {
	st := a.state()
	if st == nil || nilPort(st.store) || nilPort(st.projects) {
		return fault(f.DependencyUnbound)
	}
	if err := readInput(ctx, actor, project); err != nil {
		return err
	}
	if agent.Validate() != nil || !tx.Valid() {
		return fault(f.InvalidArgument)
	}
	x, err := st.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if nilPort(x) {
		return fault(f.DependencyUnbound)
	}
	agentKey, _ := f.AgentLock(agent.String())
	locks := []f.LockRequest{
		userLock(actor.Details().UserID, f.Shared),
		projectLock(project, f.Shared),
		taskScheduleLock(project, f.Exclusive),
		{Key: agentKey, Mode: f.Exclusive},
	}
	if err := st.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	access, err := st.projects.RequireOwnerInTx(ctx, tx, actor, project, i.Read)
	if err != nil {
		return portError(err)
	}
	if !access.Matches(actor, project) {
		return fault(f.Forbidden)
	}
	if err := ctx.Err(); err != nil {
		return canceled(err)
	}
	var assigned bool
	if err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_work.tasks WHERE project_id=$1 AND assignee_agent_id=$2)`, project.String(), agent.String()).Scan(&assigned); err != nil {
		return taskSQL(err)
	}
	if err := ctx.Err(); err != nil {
		return canceled(err)
	}
	if assigned {
		return fault(f.ResourceBusy)
	}
	return nil
}

var _ c.AgentAssigneeReferences = (*Authority)(nil)
