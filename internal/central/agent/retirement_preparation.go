package agent

import (
	"context"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type RetirementPreparation struct {
	agents *Authority
	owner  *project.AgentRetirementPreparation
}

// Same-Store binding is proved by both real authorities consuming the original
// live Tx at read time. This constructor does not infer it from a shared DSN.
func NewRetirementPreparation(agents *Authority, owner *project.AgentRetirementPreparation) (*RetirementPreparation, error) {
	if agents == nil || agents.state == nil || owner == nil {
		return nil, fault(f.DependencyUnbound)
	}
	return &RetirementPreparation{agents: agents, owner: owner}, nil
}

func (r *RetirementPreparation) ReadRetirementPreparationInTx(ctx context.Context, tx f.Tx, request pc.AgentRetirementPreparationRequest) (c.AgentConfig, error) {
	if r == nil || r.agents == nil || r.agents.state == nil || r.owner == nil {
		return c.AgentConfig{}, fault(f.DependencyUnbound)
	}
	var value c.AgentConfig
	err := r.owner.WithAgentRetirementPreparationInTx(ctx, tx, request, func(run context.Context) error {
		state := r.agents.state
		x, err := state.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if nilPort(x) {
			return fault(f.DependencyUnbound)
		}
		locks, err := request.RequiredLocks()
		if err != nil {
			return err
		}
		if err = state.store.RequireHeldLocks(run, tx, locks); err != nil {
			return portError(err)
		}
		value, err = initializedAgentState(run, x, request.ProjectID, request.AgentID, false)
		if cancelled := run.Err(); cancelled != nil {
			return cancelled
		}
		if err != nil {
			return err
		}
		if value.Fields().Core.Version != request.ExpectedConfigVersion {
			return fault(f.VersionConflict)
		}
		value = value.Clone()
		return nil
	})
	if err != nil {
		return c.AgentConfig{}, err
	}
	return value, nil
}

var _ c.RetirementPreparation = (*RetirementPreparation)(nil)
