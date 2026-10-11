package mount

import (
	"context"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
)

// CurrentTargetInTx reads only safe current metadata before an Operation is
// created. The real Agent reader must validate the caller's original live
// Execution witness, same Store and full held lock union. This method neither
// invokes the Operation owner nor opens a transaction, acquires locks, writes
// facts or issues a ticket; Resolve and Claim remain the dispatch authority.
func (r *ReadFileResolver) CurrentTargetInTx(ctx context.Context, tx f.Tx, in mc.ReadFileTargetRequest) (mc.ReadFileTarget, error) {
	if r == nil || nilPort(r.store) || nilPort(r.agents) {
		return mc.ReadFileTarget{}, fault(f.DependencyUnbound)
	}
	if ctx == nil || in.Validate() != nil {
		return mc.ReadFileTarget{}, fault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return mc.ReadFileTarget{}, err
	}
	x, err := r.store.InTx(tx)
	if err != nil {
		return mc.ReadFileTarget{}, portError(err)
	}
	request := ac.ExecutionConfigurationRequest{Actor: in.Actor, ProjectID: in.ProjectID, AgentID: in.AgentID, ExecutionID: in.ExecutionID, Stage: ac.ExecutionConfigurationCurrent}
	if err = r.store.RequireHeldLocks(ctx, tx, request.RequiredLocks()); err != nil {
		return mc.ReadFileTarget{}, portError(err)
	}
	config, err := r.agents.ReadExecutionConfigurationInTx(ctx, tx, request)
	if canceled := ctx.Err(); canceled != nil {
		return mc.ReadFileTarget{}, canceled
	}
	if err != nil {
		return mc.ReadFileTarget{}, portError(err)
	}
	current, err := readCurrentMount(ctx, x, config, in.ProjectID, in.AgentID, in.MountID)
	if canceled := ctx.Err(); canceled != nil {
		return mc.ReadFileTarget{}, canceled
	}
	if err != nil {
		return mc.ReadFileTarget{}, err
	}
	return readFileTarget(current)
}

func readFileTarget(current readFileCurrent) (mc.ReadFileTarget, error) {
	out := mc.ReadFileTarget{MountID: current.definition.ID, WorkspaceID: current.mapping.binding.WorkspaceID, RunnerID: current.definition.Runner, WorkspaceName: current.definition.Workspace, AgentVersion: current.agentVersion, MountVersion: current.definition.Version}
	if out.Validate() != nil {
		return mc.ReadFileTarget{}, fault(f.InvalidState)
	}
	return out, nil
}
