package agent

import (
	"context"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func (s *ProjectTerminalCleanup) ProjectTerminalAnchorLocks(plan c.ProjectRetirementPlan) ([]f.LockRequest, error) {
	if s == nil || s.retirement == nil {
		return nil, fault(f.DependencyUnbound)
	}
	return s.retirement.ProjectRetirementLocks(plan)
}
func (s *ProjectTerminalCleanup) RestoreProjectTerminalAnchor(ctx context.Context, req pc.ProjectTerminalCleanupRequest, a i.AgentID, version f.Version, command c.AgentCommandID, digest f.Digest, updatedAt *f.Instant) (c.ProjectRetirementPlan, error) {
	var plan c.ProjectRetirementPlan
	if ctx == nil || req.Validate() != nil || req.Confirmation || a.Validate() != nil || version.Validate() != nil || command.Validate() != nil || digest.Validate() != nil {
		return plan, invalid()
	}
	if s == nil || s.pair == nil || s.retirement == nil {
		return plan, fault(f.DependencyUnbound)
	}
	if req.Owner != s.ProjectTerminalCleanupIdentity() {
		return plan, fault(f.Forbidden)
	}
	if updatedAt != nil {
		at := *updatedAt
		updatedAt = &at
		if at.Validate() != nil {
			return plan, invalid()
		}
	}
	cause, _ := f.NewRecoveryCause("agent-terminal-anchor", req.Cause.OperationID.String(), req.ProjectID.String())
	validated := false
	result := s.pair.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.pair.store.AcquireAll(ctx, tx, req.RequiredLocks()); err != nil {
			return portError(err)
		}
		if err := s.retirement.projects.RequireProjectTerminalCleanupInTx(ctx, tx, req); err != nil {
			return portError(err)
		}
		x, err := s.pair.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if nilPort(x) {
			return fault(f.DependencyUnbound)
		}
		record, err := loadRetirementCommand(ctx, x, req.ProjectID, command)
		if err != nil {
			return err
		}
		before, err := retirementBeforeFromCommand(record.After, version, updatedAt)
		if err != nil {
			return err
		}
		plan = c.ProjectRetirementPlan{Change: c.ProjectRetirementChange{Request: c.ProjectRetirementRequest{Actor: req.Actor, ProjectID: req.ProjectID, AgentID: a, Cause: req.Cause, ExpectedVersion: version}, Before: before}, CommandID: command, BeforeDigest: digest, BeforeUpdatedAt: updatedAt}
		if record.Target != a {
			return fault(f.Forbidden)
		}
		if err = validateRetirementCommandBefore(record.After, plan); err != nil {
			return err
		}
		validated = true
		return portError(ctx.Err())
	})
	if err := commitError(result); err != nil {
		return c.ProjectRetirementPlan{}, err
	}
	if !validated {
		return c.ProjectRetirementPlan{}, unavailable(nil)
	}
	return plan.Clone(), portError(ctx.Err())
}
func (r *ProjectRetirement) terminalOriginal(ctx context.Context, tx f.Tx, plan c.ProjectRetirementPlan) (postgres.SQLExecutor, error) {
	locks, err := r.ProjectRetirementLocks(plan)
	if err != nil {
		return nil, err
	}
	if ctx == nil || !tx.Valid() {
		return nil, invalid()
	}
	x, err := r.agents.state.store.InTx(tx)
	if err != nil {
		return nil, portError(err)
	}
	if nilPort(x) {
		return nil, fault(f.DependencyUnbound)
	}
	if err = r.agents.state.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return nil, portError(err)
	}
	if err = r.projects.RequireProjectTerminalAgentAppliedInTx(ctx, tx, plan.Clone()); err != nil {
		return nil, portError(err)
	}
	if err = checkRetirementCommand(ctx, x, plan); err != nil {
		return nil, err
	}
	return x, portError(ctx.Err())
}
func (s *ProjectTerminalCleanup) RequireProjectTerminalAnchorRetiredInTx(ctx context.Context, tx f.Tx, plan c.ProjectRetirementPlan) error {
	if s == nil || s.retirement == nil {
		return fault(f.DependencyUnbound)
	}
	plan = plan.Clone()
	r := s.retirement
	x, err := r.terminalOriginal(ctx, tx, plan)
	if err != nil {
		return err
	}
	if err = requireRetirementCanonicalAbsent(ctx, x, plan.Change); err != nil {
		return err
	}
	w := &projectRetirementWitness{open: true, applied: true, recovery: true, terminal: true, consumer: r, tx: tx, plan: plan}
	defer w.close()
	run := context.WithValue(ctx, projectRetirementWitnessKey{}, w)
	for _, owner := range r.references() {
		if err = owner.RequireProjectAgentReferencesRetiredInTx(run, tx, plan.Change.Clone()); err != nil {
			return portError(err)
		}
		if err = run.Err(); err != nil {
			return portError(err)
		}
	}
	return nil
}
