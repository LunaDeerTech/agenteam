package skill

import (
	"context"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

type AssignmentDirectory struct {
	Sequence f.Version
	Items    []sc.AgentSkillAssignment
}

func (s *AssignmentCommands) ListAssignments(ctx context.Context, actor i.Actor, project i.ProjectID, agent i.AgentID) (AssignmentDirectory, error) {
	var zero AssignmentDirectory
	if s == nil {
		return zero, fault(f.DependencyUnbound)
	}
	if actor.Validate() != nil || actor.Details().Kind != i.Human || project.Validate() != nil || agent.Validate() != nil {
		return zero, invalid()
	}
	call, err := s.deps.Service.begin(ctx, false)
	if err != nil {
		return zero, err
	}
	defer s.deps.Service.end(call)
	key, err := f.NewID[f.TransactionAttempt]()
	if err != nil {
		return zero, unavailable(err)
	}
	cause, _ := f.NewJobCause("skill-assignments-read", agent.String(), key.String())
	a, _ := f.AgentLock(agent.String())
	schedule, _ := f.ProjectScheduleLock(project.String())
	locks, err := ob.NormalizeLocks([]f.LockRequest{userLock(actor.Details().UserID, f.Shared), projectLock(project, f.Shared), {Key: schedule, Mode: f.Exclusive}, {Key: a, Mode: f.Shared}})
	if err != nil {
		return zero, err
	}
	var out AssignmentDirectory
	result := s.store().WithinTx(call.ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store().AcquireAll(ctx, tx, locks); err != nil {
			return captureError(err)
		}
		grant, err := s.deps.Service.state().authority.state().projects.RequireOwnerInTx(ctx, tx, actor, project, i.Read)
		if err != nil {
			return captureError(err)
		}
		if !grant.Matches(actor, project) {
			return fault(f.Forbidden)
		}
		if _, err = s.deps.CurrentAgents.RequireCurrentInTx(ctx, tx, actor, project, agent); err != nil {
			return captureError(err)
		}
		x, err := s.store().InTx(tx)
		if err != nil {
			return captureError(err)
		}
		state, err := loadAssignmentSet(ctx, x, project, agent)
		if err != nil {
			return err
		}
		out = AssignmentDirectory{Sequence: state.Sequence, Items: slices.Clone(state.Items)}
		return ctx.Err()
	})
	if err = commitError(result); err != nil {
		return zero, err
	}
	return out, nil
}
