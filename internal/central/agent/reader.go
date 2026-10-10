package agent

import (
	"context"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func (s *Service) GetAgent(ctx context.Context, actor i.Actor, project i.ProjectID, agent i.AgentID) (c.AgentConfig, error) {
	ctx, done, err := s.begin(ctx)
	if err != nil {
		return c.AgentConfig{}, err
	}
	defer done()
	if err = currentActor(actor); err != nil {
		return c.AgentConfig{}, err
	}
	if project.Validate() != nil || agent.Validate() != nil {
		return c.AgentConfig{}, invalid()
	}
	cause, err := readCause("get")
	if err != nil {
		return c.AgentConfig{}, err
	}
	var out c.AgentConfig
	st := s.state
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := st.store.AcquireAll(ctx, tx, []f.LockRequest{userLock(actor, f.Shared), projectLock(project, f.Shared), agentLock(agent, f.Shared)}); err != nil {
			return portError(err)
		}
		grant, err := st.deps.Authority.state.projects.RequireOwnerInTx(ctx, tx, actor, project, i.Read)
		if err != nil {
			return portError(err)
		}
		if !grant.Matches(actor, project) {
			return fault(f.Forbidden)
		}
		x, err := st.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		out, err = initializedAgent(ctx, x, project, agent)
		return err
	})
	if err := commitError(result); err != nil {
		return c.AgentConfig{}, err
	}
	return out.Clone(), nil
}

func (s *Service) LookupAgentCommand(ctx context.Context, actor i.Actor, project i.ProjectID, command c.CommandName, key f.IdempotencyKey, semantic f.Digest) (c.AgentCommandLookup, error) {
	ctx, done, err := s.begin(ctx)
	if err != nil {
		return c.AgentCommandLookup{}, err
	}
	defer done()
	if err = currentActor(actor); err != nil {
		return c.AgentCommandLookup{}, err
	}
	if semantic.Validate() != nil {
		return c.AgentCommandLookup{}, invalid()
	}
	identity, err := c.AgentCommandIdentity(project, command, key)
	if err != nil {
		return c.AgentCommandLookup{}, err
	}
	cause, err := f.NewCommandsCause(identity)
	if err != nil {
		return c.AgentCommandLookup{}, invalid()
	}
	var out c.AgentCommandLookup
	st := s.state
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := st.store.AcquireAll(ctx, tx, []f.LockRequest{commandLock(identity), userLock(actor, f.Shared), projectLock(project, f.Shared)}); err != nil {
			return portError(err)
		}
		grant, err := st.deps.Authority.state.projects.RequireOwnerInTx(ctx, tx, actor, project, i.Read)
		if err != nil {
			return portError(err)
		}
		if !grant.Matches(actor, project) {
			return fault(f.Forbidden)
		}
		x, err := st.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		r, err := loadCommand(ctx, x, project, command, key)
		if err != nil {
			return err
		}
		if r == nil {
			out, err = c.NewAgentCommandLookup(c.AgentLookupNotObserved, nil)
			return err
		}
		if r.User.String() != actor.Details().UserID {
			return fault(f.NotFound)
		}
		if r.Semantic != semantic {
			return fault(f.IdempotencyKeyReused)
		}
		if r.State == "planned" {
			out, err = c.NewAgentCommandLookup(c.AgentLookupInProgress, nil)
			return err
		}
		receipt, err := c.DecodeAgentMutation(r.Receipt)
		if err != nil {
			return unavailable(err)
		}
		if !sameValue(receipt.Fields().Agent, r.After) || receipt.Fields().Changed != (len(r.ChangedFields) > 0) {
			return unavailable(nil)
		}
		out, err = c.NewAgentCommandLookup(c.AgentLookupCommitted, &receipt)
		return err
	})
	if err := commitError(result); err != nil {
		return c.AgentCommandLookup{}, err
	}
	return out.Clone(), nil
}
