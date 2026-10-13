package mount

import (
	"bytes"
	"context"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type projectCleanupParticipant struct {
	original  *ProjectLifecycleOwnership
	authority mc.ProjectCleanupAuthority
}

func NewProjectCleanupParticipant(original *ProjectLifecycleOwnership, authority mc.ProjectCleanupAuthority) (pc.ProjectLifecycleParticipant, error) {
	if original == nil || nilPort(authority) {
		return nil, fault(f.DependencyUnbound)
	}
	return &projectCleanupParticipant{original, authority}, nil
}
func (*projectCleanupParticipant) Name() pc.ParticipantName { return pc.RunnerManagedParticipant }
func (p *projectCleanupParticipant) RequestStop(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) (pc.StopReport, error) {
	return p.original.RequestProjectStop(ctx, actor, cause, scope)
}
func (p *projectCleanupParticipant) InspectStop(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) (pc.StopReport, error) {
	return p.original.InspectProjectStop(ctx, actor, cause, scope)
}

func (p *projectCleanupParticipant) Cleanup(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef, cp *pc.CleanupCheckpoint) (pc.CleanupReport, error) {
	if p == nil || p.original == nil || nilPort(p.authority) {
		return pc.CleanupReport{}, fault(f.DependencyUnbound)
	}
	prior, err := mc.DecodeProjectCleanupCheckpoint(cp, cause, scope)
	if err != nil {
		return pc.CleanupReport{}, err
	}
	s := p.original
	run, c, err := s.borrow(ctx, actor, cause, scope, "cleanup", cp, p.authority)
	if err != nil {
		return pc.CleanupReport{}, err
	}
	defer s.returned(c)
	if c.unknown != nil {
		if err = s.confirmCleanup(run, c); err != nil {
			return pc.CleanupReport{}, err
		}
		return s.cleanupReport(c, "agents", prior)
	}
	// First discover the Agent under the current exact Project claim. The
	// complete final lock union is acquired once in the following transaction.
	c.observation = "discovery"
	c.observed = false
	c.batch = nil
	c.candidate = i.AgentID{}
	result := s.transaction(run, c, projectLifecycleCause(c), i.AgentID{}, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		if err := s.requireOriginalReturned(ctx, tx, c); err != nil {
			return err
		}
		if err := noProjectExecutionMounts(ctx, x, scope.ProjectID); err != nil {
			return err
		}
		var err error
		c.candidate, err = nextProjectMountAgent(ctx, x, scope.ProjectID)
		if err != nil {
			return err
		}
		c.observed = true
		return ctx.Err()
	})
	if result.State() == f.Unknown {
		return pc.CleanupReport{}, s.retain(c, result)
	}
	if err = commitError(result); err != nil {
		return pc.CleanupReport{}, err
	}
	if !c.observed || run.Err() != nil {
		return pc.CleanupReport{}, fault(f.ResourceBusy)
	}
	if prior.Validate() == nil && (prior.ProcessID != s.process || prior.InstanceID != s.instance.String()) {
		// A checkpoint cannot mint a new local owner. Foreign process proof is
		// taken only after the current claim transaction actually returned.
		if prior.ProcessID == s.process || s.proof.ConfirmStopped(run, prior.ProcessID) != nil {
			return s.cleanupReport(c, "agents", prior)
		}
	}
	c.observation = "mutation"
	stage := "agents"
	validated := false
	result = s.transaction(run, c, projectLifecycleCause(c), c.candidate, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		if err := s.requireOriginalReturned(ctx, tx, c); err != nil {
			return err
		}
		if err := noProjectExecutionMounts(ctx, x, scope.ProjectID); err != nil {
			return err
		}
		current, err := nextProjectMountAgent(ctx, x, scope.ProjectID)
		if err != nil {
			return err
		}
		if current != c.candidate {
			return fault(f.ConfirmationStale)
		}
		if current.Validate() != nil {
			if err = requireProjectMountsRetired(ctx, x, c); err != nil {
				return err
			}
			stage = "completed"
			validated = true
			return ctx.Err()
		}
		before, err := s.agents.ReadRunnerManagedConfigurationInTx(ctx, tx, actor, cause, scope, current)
		if err != nil {
			return portError(err)
		}
		snapshot, raw, err := readProjectMountSnapshot(ctx, x, scope.ProjectID, current)
		if err != nil {
			return err
		}
		if err = validateProjectMountSnapshot(snapshot, before); err != nil {
			return err
		}
		b := &projectMountBatch{agent: current, before: before, snapshot: snapshot, raw: raw}
		b.proof, err = newProjectAgentProof(c, s.process, b)
		if err != nil {
			return err
		}
		c.batch = b
		if err = insertProjectAgentProof(ctx, x, b.proof); err != nil {
			return err
		}
		if err = deleteProjectMountSnapshot(ctx, x, scope.ProjectID, current, snapshot); err != nil {
			return err
		}
		if err = projectProofResourcesAbsent(ctx, x, b.proof); err != nil {
			return err
		}
		validated = true
		return ctx.Err()
	})
	if result.State() == f.Unknown {
		return pc.CleanupReport{}, s.retain(c, result)
	}
	if err = commitError(result); err != nil {
		return pc.CleanupReport{}, err
	}
	if !validated || run.Err() != nil {
		return pc.CleanupReport{}, fault(f.ResourceBusy)
	}
	return s.cleanupReport(c, stage, mc.ProjectCleanupProgress{})
}
func (s *ProjectLifecycleOwnership) requireOriginalReturned(ctx context.Context, tx f.Tx, c *projectLifecycleControl) error {
	if err := s.agents.RequireProjectCallsReturnedInTx(ctx, tx, c.actor, c.cause, c.scope); err != nil {
		return portError(err)
	}
	return portError(s.executions.RequireProjectCallsReturnedInTx(ctx, tx, c.actor, c.cause, c.scope))
}
func (s *ProjectLifecycleOwnership) confirmCleanup(ctx context.Context, c *projectLifecycleControl) error {
	validated := false
	result := s.transaction(ctx, c, c.unknown.Cause(), c.candidate, func(run context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		if err := s.requireOriginalReturned(run, tx, c); err != nil {
			return err
		}
		if err := noProjectExecutionMounts(run, x, c.scope.ProjectID); err != nil {
			return err
		}
		if c.batch == nil {
			if c.observation == "discovery" {
				next, err := nextProjectMountAgent(run, x, c.scope.ProjectID)
				if err != nil {
					return err
				}
				// An interrupted original read never observed an empty Project.
				// This confirmation still performs the current gates and actual
				// read, then returns Pending without advancing any mutation.
				if c.observed && next != c.candidate {
					return fault(f.ConfirmationStale)
				}
			}
			validated = true
			return run.Err()
		}
		b := c.batch
		before, err := s.agents.ReadRunnerManagedConfigurationInTx(run, tx, c.actor, c.cause, c.scope, b.agent)
		if err != nil {
			return portError(err)
		}
		d, err := ac.ProjectRetirementBeforeDigest(before)
		if err != nil || d != b.proof.beforeDigest {
			return fault(f.ConfirmationStale)
		}
		proof, err := readProjectAgentProof(run, x, c.scope.ProjectID, b.agent)
		if err != nil {
			return err
		}
		_, raw, err := readProjectMountSnapshot(run, x, c.scope.ProjectID, b.agent)
		if err != nil {
			return err
		}
		if proof == nil {
			if !bytes.Equal(raw, b.raw) {
				return fault(f.ConfirmationStale)
			}
		} else {
			if !sameProjectAgentProof(proof, b.proof) {
				return fault(f.ConfirmationStale)
			}
			if err = projectProofResourcesAbsent(run, x, proof); err != nil {
				return err
			}
		}
		validated = true
		return run.Err()
	})
	if result.State() != f.Committed || !validated || ctx.Err() != nil {
		return c.firstError
	}
	s.clearUnknown(c)
	return nil
}
func (s *ProjectLifecycleOwnership) cleanupReport(c *projectLifecycleControl, stage string, prior mc.ProjectCleanupProgress) (pc.CleanupReport, error) {
	p := mc.ProjectCleanupProgress{ProjectID: c.scope.ProjectID, OperationID: c.cause.OperationID, ProjectVersion: c.cause.ProjectVersion, ProcessID: s.process, InstanceID: s.instance.String(), Stage: stage}
	if prior.Validate() == nil {
		p = prior
		p.Stage = stage
	}
	cp, err := mc.NewProjectCleanupCheckpoint(p, c.cause, c.scope)
	if err != nil {
		return pc.CleanupReport{}, err
	}
	d := pc.CleanupDetails{State: pc.CleanupPending, Checkpoint: &cp, SafeReason: pc.ReasonWorkPending}
	if stage == "completed" {
		d.State, d.SafeReason = pc.CleanupCompleted, ""
	}
	return pc.NewCleanupReport(pc.RunnerManagedParticipant, c.cause, c.scope, d)
}

var _ pc.ProjectLifecycleParticipant = (*projectCleanupParticipant)(nil)
