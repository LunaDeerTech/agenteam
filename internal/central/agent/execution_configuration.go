package agent

import (
	"context"
	"errors"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// ExecutionConfigurationAuthority is immutable composition of the real Agent
// owner and the real Execution identity owner. It adds no public Get behavior,
// worker, transaction, mutable binding, or cross-domain SQL.
type ExecutionConfigurationAuthority struct {
	agents     *Authority
	executions c.ExecutionIdentityAuthority
}

func NewExecutionConfiguration(agents *Authority, executions c.ExecutionIdentityAuthority) (*ExecutionConfigurationAuthority, error) {
	if agents == nil || agents.state == nil || nilPort(executions) {
		return nil, fault(f.DependencyUnbound)
	}
	return &ExecutionConfigurationAuthority{agents: agents, executions: executions}, nil
}

func executionConfigurationError(err error) error {
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return portError(err)
}

func (a *ExecutionConfigurationAuthority) ReadExecutionConfigurationInTx(ctx context.Context, tx f.Tx, request c.ExecutionConfigurationRequest) (c.AgentConfig, error) {
	if a == nil || a.agents == nil || a.agents.state == nil || nilPort(a.executions) {
		return c.AgentConfig{}, fault(f.DependencyUnbound)
	}
	if ctx == nil {
		return c.AgentConfig{}, invalid()
	}
	if err := ctx.Err(); err != nil {
		return c.AgentConfig{}, err
	}
	if err := request.Validate(); err != nil {
		return c.AgentConfig{}, err
	}
	state := a.agents.state
	x, err := state.store.InTx(tx)
	if err != nil {
		return c.AgentConfig{}, executionConfigurationError(err)
	}
	if err = state.store.RequireHeldLocks(ctx, tx, request.RequiredLocks()); err != nil {
		return c.AgentConfig{}, executionConfigurationError(err)
	}
	if request.Actor.Details().Kind == i.Human {
		grant, err := state.projects.RequireOwnerInTx(ctx, tx, request.Actor, request.ProjectID, i.Read)
		if err != nil {
			return c.AgentConfig{}, executionConfigurationError(err)
		}
		if !grant.Matches(request.Actor, request.ProjectID) {
			return c.AgentConfig{}, fault(f.Forbidden)
		}
	}
	// Every stage needs the original Execution owner. An AgentRun constructor
	// alone cannot pass; preparing does not borrow the running/Snapshot gate.
	if err = a.executions.RequireExecutionConfigurationInTx(ctx, tx, request.Clone()); err != nil {
		return c.AgentConfig{}, executionConfigurationError(err)
	}
	if err = ctx.Err(); err != nil {
		return c.AgentConfig{}, err
	}
	value, err := initializedAgent(ctx, x, request.ProjectID, request.AgentID)
	// The repository safely hides raw storage causes. Check the original
	// context only after that call has actually returned, preserving cancellation
	// without publishing a value or detaching the in-flight SQL call.
	if canceled := ctx.Err(); canceled != nil {
		return c.AgentConfig{}, canceled
	}
	if err != nil {
		return c.AgentConfig{}, executionConfigurationError(err)
	}
	return value.Clone(), nil
}

var _ c.ExecutionConfiguration = (*ExecutionConfigurationAuthority)(nil)
