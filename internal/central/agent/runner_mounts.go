package agent

import (
	"context"
	"encoding/json"
	"slices"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
)

func runnerCandidateBinding(project i.ProjectID, owner string) (cursor.Binding, error) {
	raw, err := json.Marshal(struct {
		Format  int         `json:"format"`
		Kind    string      `json:"kind"`
		Project i.ProjectID `json:"project_id"`
		Owner   string      `json:"owner_user_id"`
	}{1, "agent.available-runners", project, owner})
	if err != nil {
		return cursor.Binding{}, unavailable(err)
	}
	digest, err := cursor.Digest(raw)
	if err != nil {
		return cursor.Binding{}, unavailable(err)
	}
	scope, err := i.InProject(project)
	if err != nil {
		return cursor.Binding{}, invalid()
	}
	return cursor.Binding{Scope: scope, QueryDigest: digest, Order: "id:asc"}, nil
}

func (s *Service) ListAvailableRunners(ctx context.Context, actor i.Actor, project i.ProjectID, page f.PageRequest) (f.Page[rc.ConfigurationCandidate], error) {
	empty := f.Page[rc.ConfigurationCandidate]{}
	ctx, _, done, err := s.beginProject(ctx, project, readCall)
	if err != nil {
		return empty, err
	}
	defer done()
	if err = currentActor(actor); err != nil {
		return empty, err
	}
	if project.Validate() != nil || page.Validate() != nil || page.Limit > 50 || len(page.Cursor) > cursor.MaxTokenBytes {
		return empty, invalid()
	}
	directory := s.state.deps.RunnerCandidates
	keys := s.state.deps.RunnerCursorKeys
	if nilPort(directory) || keys.Validate() != nil {
		return empty, fault(f.DependencyUnbound)
	}
	locks, err := directory.ConfigurationCandidateLocks([]rc.RunnerID{})
	if err != nil {
		return empty, portError(err)
	}
	if len(locks) > 510 {
		return empty, fault(f.ResourceBusy)
	}
	locks = append(locks, userLock(actor, f.Shared), projectLock(project, f.Shared))
	locks, err = oc.NormalizeLocks(locks)
	if err != nil {
		return empty, portError(err)
	}
	cause, err := readCause("available-runners")
	if err != nil {
		return empty, err
	}
	out := f.Page[rc.ConfigurationCandidate]{Items: []rc.ConfigurationCandidate{}}
	result := s.state.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.state.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		grant, err := s.state.deps.Authority.state.projects.RequireOwnerInTx(ctx, tx, actor, project, i.Read)
		if err != nil {
			return portError(err)
		}
		if !grant.Matches(actor, project) {
			return fault(f.Forbidden)
		}
		binding, err := runnerCandidateBinding(project, actor.Details().UserID)
		if err != nil {
			return err
		}
		var after *rc.RunnerID
		if page.Cursor != "" {
			position, err := keys.Verify(page.Cursor, binding)
			if err != nil {
				return err
			}
			if position.OrderGeneration != nil || len(position.Scalars) != 1 || position.Scalars[0].Kind() != "uuid" {
				return fault(f.CursorInvalid)
			}
			id, err := f.ParseID[rc.Runner](position.Scalars[0].Value())
			if err != nil {
				return fault(f.CursorInvalid)
			}
			after = &id
		}
		items, err := directory.ListConfigurationCandidatesInTx(ctx, tx, actor, project, after, page.Limit+1)
		if err != nil {
			return portError(err)
		}
		if items == nil || len(items) > page.Limit+1 {
			return unavailable(nil)
		}
		for n, item := range items {
			if item.Validate() != nil || after != nil && item.ID.String() <= after.String() || n > 0 && items[n-1].ID.String() >= item.ID.String() {
				return unavailable(nil)
			}
		}
		out.Items = slices.Clone(items)
		if len(items) > page.Limit {
			out.Items = out.Items[:page.Limit]
			last, err := cursor.UUID(out.Items[len(out.Items)-1].ID.String())
			if err != nil {
				return unavailable(err)
			}
			out.NextCursor, err = keys.Sign(binding, cursor.Position{Scalars: []cursor.Scalar{last}})
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err = commitError(result); err != nil {
		return empty, err
	}
	if err = ctx.Err(); err != nil {
		return empty, err
	}
	return out, nil
}

func (s *Service) GetRunnerMounts(ctx context.Context, actor i.Actor, project i.ProjectID, agent i.AgentID) (c.AgentRunnerMounts, error) {
	empty := c.AgentRunnerMounts{}
	ctx, _, done, err := s.beginProject(ctx, project, readCall)
	if err != nil {
		return empty, err
	}
	defer done()
	if err = currentActor(actor); err != nil {
		return empty, err
	}
	if project.Validate() != nil || agent.Validate() != nil {
		return empty, invalid()
	}
	reader, ok := s.state.deps.Mounts.(c.RunnerMountBindingsReader)
	if !ok || nilPort(reader) {
		return empty, fault(f.DependencyUnbound)
	}
	cause, err := readCause("runner-mounts")
	if err != nil {
		return empty, err
	}
	var out c.AgentRunnerMounts
	result := s.state.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.state.store.AcquireAll(ctx, tx, []f.LockRequest{userLock(actor, f.Shared), projectLock(project, f.Shared), agentLock(agent, f.Shared)}); err != nil {
			return portError(err)
		}
		grant, err := s.state.deps.Authority.state.projects.RequireOwnerInTx(ctx, tx, actor, project, i.Read)
		if err != nil {
			return portError(err)
		}
		if !grant.Matches(actor, project) {
			return fault(f.Forbidden)
		}
		x, err := s.state.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		current, err := initializedAgent(ctx, x, project, agent)
		if err != nil {
			return err
		}
		fields := current.Fields()
		items, err := reader.RunnerMountBindingsInTx(ctx, tx, actor, project, agent, fields.Core.Version, fields.AllowedMountIDs)
		if err != nil {
			return portError(err)
		}
		if len(items) != len(fields.AllowedMountIDs) {
			return unavailable(nil)
		}
		for n, item := range items {
			if item.MountID != fields.AllowedMountIDs[n] {
				return unavailable(nil)
			}
		}
		out = c.AgentRunnerMounts{ProjectID: project, AgentID: agent, AgentVersion: fields.Core.Version, Items: slices.Clone(items)}
		if out.Validate() != nil {
			return unavailable(nil)
		}
		return nil
	})
	if err = commitError(result); err != nil {
		return empty, err
	}
	if err = ctx.Err(); err != nil {
		return empty, err
	}
	return out.Clone(), nil
}
