package execution

import (
	"context"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type preparationWitnessKey struct{}
type preparationWitness struct {
	owner          *authorityState
	driver         *preparationState
	tx             f.Tx
	request        c.PreparationRequest
	claim          preparationClaim
	locks          []f.LockRequest
	project        pc.ProjectRef
	projectChecked bool
	input          c.CapturedTriggerInput
	sourceCaptured bool
}

func clonePreparationProject(p pc.ProjectRef) pc.ProjectRef {
	if p.CurrentSprintID != nil {
		v := *p.CurrentSprintID
		p.CurrentSprintID = &v
	}
	if p.ArchivedAt != nil {
		v := *p.ArchivedAt
		p.ArchivedAt = &v
	}
	return p
}

func (w *preparationWitness) require(ctx context.Context, tx f.Tx, request c.PreparationRequest) error {
	if ctx == nil {
		return invalid()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if w == nil || w.driver == nil || w.owner == nil || w.owner != w.driver.authority.state || w.tx != tx || !w.request.Equal(request) {
		return fault(f.Forbidden)
	}
	s := w.driver
	x, err := s.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if err = s.store.RequireHeldLocks(ctx, tx, w.locks); err != nil {
		return portError(err)
	}
	if s.processes.CurrentProcess() != s.process || w.claim.process != s.process {
		return fault(f.InvalidState)
	}
	s.mu.Lock()
	run := s.calls[request.ExecutionID]
	active := run != nil && !run.returned && run.unresolved == nil && run.claim != nil && *run.claim == w.claim && run.request.Equal(request)
	s.mu.Unlock()
	if !active {
		return fault(f.Forbidden)
	}
	row, err := loadExecution(ctx, x, request.ExecutionID)
	if err != nil {
		return err
	}
	if err = preparingRecord(row, request, false); err != nil {
		return err
	}
	claim, err := loadPreparationClaim(ctx, x, request.ExecutionID)
	if err != nil {
		return err
	}
	if !samePreparationClaim(claim, &w.claim) || claim.phase != "running" {
		return fault(f.ConfirmationStale)
	}
	return ctx.Err()
}

func (a *Authority) RequireTriggerCaptureInTx(ctx context.Context, tx f.Tx, execution i.ExecutionID, launch c.LaunchRequest) (pc.ProjectRef, error) {
	if a == nil || a.state == nil {
		return pc.ProjectRef{}, fault(f.DependencyUnbound)
	}
	if ctx == nil {
		return pc.ProjectRef{}, invalid()
	}
	request := c.PreparationRequest{ExecutionID: execution, Launch: launch}
	if err := request.Validate(); err != nil {
		return pc.ProjectRef{}, err
	}
	w, ok := ctx.Value(preparationWitnessKey{}).(*preparationWitness)
	if !ok || w == nil || w.owner != a.state || !w.projectChecked {
		return pc.ProjectRef{}, fault(f.Forbidden)
	}
	if err := w.require(ctx, tx, request); err != nil {
		return pc.ProjectRef{}, err
	}
	if w.project.Validate() != nil || w.project.ID != request.Launch.ProjectID || w.project.Lifecycle != pc.Active {
		return pc.ProjectRef{}, fault(f.InvalidState)
	}
	return clonePreparationProject(w.project), nil
}

func (a *Authority) requireCaptureConfiguration(ctx context.Context, tx f.Tx, request ac.ExecutionConfigurationRequest) error {
	w, ok := ctx.Value(preparationWitnessKey{}).(*preparationWitness)
	if !ok || w == nil || w.owner != a.state || !w.projectChecked || !w.sourceCaptured || w.input.Validate() != nil {
		return fault(f.Forbidden)
	}
	actor, err := i.NewAgentRun(w.request.Launch.ProjectID, w.request.Launch.AgentID, w.request.ExecutionID)
	if err != nil || !actor.Equal(request.Actor) || request.ProjectID != w.request.Launch.ProjectID || request.AgentID != w.request.Launch.AgentID || request.ExecutionID != w.request.ExecutionID || request.Stage != ac.ExecutionConfigurationCapture {
		return fault(f.Forbidden)
	}
	if w.input.Ref().ProviderType != w.request.Launch.Trigger.Kind {
		return fault(f.InvalidState)
	}
	return w.require(ctx, tx, w.request)
}

var _ c.TriggerCaptureAuthority = (*Authority)(nil)
