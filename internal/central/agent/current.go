package agent

import (
	"context"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// A canonical row becomes observable as initialized only with its completed
// create receipt in the same commit. A row by itself is not publication proof.
func initializedAgent(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID, agent i.AgentID) (c.AgentConfig, error) {
	v, err := loadAgent(ctx, x, project, agent)
	if err != nil {
		return c.AgentConfig{}, err
	}
	if v == nil {
		return c.AgentConfig{}, fault(f.NotFound)
	}
	if v.Fields().Core.Lifecycle != c.AgentActive {
		return c.AgentConfig{}, fault(f.InvalidState)
	}
	rows, err := x.Query(ctx, `SELECT receipt FROM agenteam_agent.commands WHERE project_id=$1 AND target_id=$2 AND command_name='agent.create' AND state='completed' LIMIT 2`, project.String(), agent.String())
	if err != nil {
		return c.AgentConfig{}, unavailable(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return c.AgentConfig{}, unavailable(err)
		}
		receipt, err := c.DecodeAgentMutation(raw)
		if err != nil {
			return c.AgentConfig{}, unavailable(err)
		}
		initial := receipt.Fields()
		core := initial.Agent.Fields().Core
		if !initial.Changed || core.ID != agent || core.ProjectID != project || core.Version != 1 || core.Lifecycle != c.AgentActive || count != 0 {
			return c.AgentConfig{}, unavailable(nil)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return c.AgentConfig{}, unavailable(err)
	}
	if count != 1 {
		return c.AgentConfig{}, fault(f.InvalidState)
	}
	return v.Clone(), nil
}

func (a *Authority) RequireCurrentInTx(ctx context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, agent i.AgentID) (c.AgentRef, error) {
	if a == nil || a.state == nil {
		return c.AgentRef{}, fault(f.DependencyUnbound)
	}
	if ctx == nil {
		return c.AgentRef{}, invalid()
	}
	if err := ctx.Err(); err != nil {
		return c.AgentRef{}, err
	}
	if err := currentActor(actor); err != nil {
		return c.AgentRef{}, err
	}
	if project.Validate() != nil || agent.Validate() != nil {
		return c.AgentRef{}, invalid()
	}
	x, err := a.state.store.InTx(tx)
	if err != nil {
		return c.AgentRef{}, portError(err)
	}
	schedule, _ := f.ProjectScheduleLock(project.String())
	locks := []f.LockRequest{userLock(actor, f.Shared), projectLock(project, f.Shared), {Key: schedule, Mode: f.Exclusive}, agentLock(agent, f.Shared)}
	if err = a.state.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return c.AgentRef{}, portError(err)
	}
	grant, err := a.state.projects.RequireOwnerInTx(ctx, tx, actor, project, i.Read)
	if err != nil {
		return c.AgentRef{}, portError(err)
	}
	if !grant.Matches(actor, project) {
		return c.AgentRef{}, fault(f.Forbidden)
	}
	value, err := initializedAgent(ctx, x, project, agent)
	if err != nil {
		return c.AgentRef{}, err
	}
	return c.AgentRef{ProjectID: project, AgentID: agent, ConfigVersion: value.Fields().Core.Version}, nil
}

var _ c.WorkReferences = (*Authority)(nil)
