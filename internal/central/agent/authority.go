package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"slices"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

type Authority struct{ state *authorityState }
type authorityState struct {
	store        Store
	projects     pc.ProjectAuthority
	secretIssuer vc.SecretPlanIssuer
}

func NewAuthority(store Store, projects pc.ProjectAuthority) (*Authority, error) {
	if nilPort(store) || nilPort(projects) {
		return nil, fault(f.DependencyUnbound)
	}
	if !reflect.TypeOf(store).Comparable() {
		return nil, invalid()
	}
	return &Authority{&authorityState{store: store, projects: projects, secretIssuer: vc.NewSecretPlanIssuer()}}, nil
}

// witness is never an exported request/DTO or a generic context boolean. Only
// writeCanonical can create it, after this Store's actual canonical SQL and
// complete preimage check. All consumers receive the original derived context.
type mutationWitnessKey struct{}
type mutationWitness struct {
	authority *authorityState
	tx        f.Tx
	actor     i.Actor
	command   c.AgentCommandID
	revision  f.Version
	mapping   f.Digest
	before    *c.AgentConfig
	after     c.AgentConfig
}

func recordMapping(r *commandRecord) (f.Digest, error) {
	if r == nil || r.validate() != nil {
		return "", unavailable(nil)
	}
	return hash(struct {
		ID                  c.AgentCommandID
		Identity            string
		User                i.UserID
		Target              i.AgentID
		Semantic            f.Digest
		Revision            f.Version
		Expected            *f.Version
		Before              *c.AgentConfig
		After               c.AgentConfig
		AddSkillsEnabled    *bool
		InstallSkillEnabled *bool
		Input               json.RawMessage
		ChangedFields       []string
	}{r.ID, r.identity().Canonical(), r.User, r.Target, r.Semantic, r.Revision, r.Expected, r.Before, r.After, r.AddSkillsEnabled, r.InstallSkillEnabled, r.Input, r.ChangedFields})
}

func ownerLocks(actor i.Actor, project i.ProjectID, agent i.AgentID, command f.CommandIdentity) []f.LockRequest {
	return []f.LockRequest{commandLock(command), userLock(actor, f.Exclusive), projectLock(project, f.Shared), agentLock(agent, f.Exclusive)}
}

func (a *Authority) inTx(ctx context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, agent i.AgentID, command f.CommandIdentity) (postgres.SQLExecutor, error) {
	if a == nil || a.state == nil {
		return nil, fault(f.DependencyUnbound)
	}
	if ctx == nil {
		return nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := currentActor(actor); err != nil {
		return nil, err
	}
	if project.Validate() != nil || agent.Validate() != nil || command.Validate() != nil || command.Namespace() != "project" || !slices.Equal(command.OwnerIDs(), []string{project.String()}) || command.Command() != "agent.create" && command.Command() != "agent.update" {
		return nil, invalid()
	}
	x, err := a.state.store.InTx(tx)
	if err != nil {
		return nil, portError(err)
	}
	if err = a.state.store.RequireHeldLocks(ctx, tx, ownerLocks(actor, project, agent, command)); err != nil {
		return nil, portError(err)
	}
	grant, err := a.state.projects.RequireOwnerInTx(ctx, tx, actor, project, i.Read)
	if err != nil {
		return nil, portError(err)
	}
	if !grant.Matches(actor, project) {
		return nil, unavailable(nil)
	}
	return x, nil
}

func (a *Authority) writeCanonical(ctx context.Context, tx f.Tx, actor i.Actor, planned *commandRecord) (context.Context, error) {
	if planned == nil || planned.validate() != nil || planned.State != "planned" {
		return nil, invalid()
	}
	x, err := a.inTx(ctx, tx, actor, planned.Project, planned.Target, planned.identity())
	if err != nil {
		return nil, err
	}
	if planned.User.String() != actor.Details().UserID {
		return nil, fault(f.NotFound)
	}
	grant, err := a.state.projects.RequireOwnerInTx(ctx, tx, actor, planned.Project, i.Mutate)
	if err != nil {
		return nil, portError(err)
	}
	if !grant.Matches(actor, planned.Project) {
		return nil, fault(f.Forbidden)
	}
	actual, err := loadCommand(ctx, x, planned.Project, planned.Name, planned.Key)
	if err != nil {
		return nil, err
	}
	mapping, err := recordMapping(planned)
	if err != nil {
		return nil, err
	}
	currentMapping, err := recordMapping(actual)
	if err != nil || actual.State != "planned" || currentMapping != mapping {
		return nil, fault(f.VersionConflict)
	}
	before, err := loadAgent(ctx, x, planned.Project, planned.Target)
	if err != nil {
		return nil, err
	}
	if !sameValue(before, planned.Before) {
		return nil, fault(f.VersionConflict)
	}
	if before == nil || before.Fields().Core.NormalizedName != planned.After.Fields().Core.NormalizedName {
		if err = a.state.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(planned.Project, f.Exclusive)}); err != nil {
			return nil, portError(err)
		}
	}
	if before == nil {
		var count int64
		if err = x.QueryRow(ctx, `SELECT count(*) FROM agenteam_agent.agents WHERE project_id=$1`, planned.Project.String()).Scan(&count); err != nil {
			return nil, unavailable(err)
		}
		if count >= 4096 {
			return nil, fault(f.ResourceBusy)
		}
	}
	changed := before == nil || !sameValue(*before, planned.After)
	if changed {
		if err = writeAgentRow(ctx, x, before, planned.After); err != nil {
			return nil, err
		}
	}
	// Re-read the whole canonical after all three local reference sets were
	// written. A half-applied collection must never yield an owner witness.
	postimage, err := loadAgent(ctx, x, planned.Project, planned.Target)
	if err != nil {
		return nil, err
	}
	if postimage == nil || !sameValue(*postimage, planned.After) {
		return nil, unavailable(nil)
	}
	w := mutationWitness{a.state, tx, actor, planned.ID, planned.Revision, mapping, before, postimage.Clone()}
	return context.WithValue(ctx, mutationWitnessKey{}, w), nil
}

func (a *Authority) checkApplied(ctx context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, agent i.AgentID, command f.CommandIdentity, mapping f.Digest) (*commandRecord, error) {
	x, err := a.inTx(ctx, tx, actor, project, agent, command)
	if err != nil {
		return nil, err
	}
	w, ok := ctx.Value(mutationWitnessKey{}).(mutationWitness)
	if !ok || w.authority != a.state || w.tx != tx || !w.actor.Equal(actor) || w.mapping != mapping {
		return nil, fault(f.Forbidden)
	}
	r, err := loadCommand(ctx, x, project, c.CommandName(command.Command()), command.Key())
	if err != nil {
		return nil, err
	}
	actual, err := recordMapping(r)
	if err != nil || r.State != "planned" || r.ID != w.command || r.Revision != w.revision || actual != mapping || r.User.String() != actor.Details().UserID || r.Target != agent || !sameValue(r.Before, w.before) || !sameValue(r.After, w.after) {
		return nil, fault(f.Forbidden)
	}
	current, err := loadAgent(ctx, x, project, agent)
	if err != nil {
		return nil, err
	}
	if current == nil || !sameValue(*current, w.after) {
		return nil, fault(f.Forbidden)
	}
	grant, err := a.state.projects.RequireOwnerInTx(ctx, tx, actor, project, i.Mutate)
	if err != nil {
		return nil, portError(err)
	}
	if !grant.Matches(actor, project) {
		return nil, fault(f.Forbidden)
	}
	return r, nil
}

func (a *Authority) discoverPlanned(ctx context.Context, actor i.Actor, project i.ProjectID, agent i.AgentID, command f.CommandIdentity, check func(*commandRecord) error) error {
	if a == nil || a.state == nil {
		return fault(f.DependencyUnbound)
	}
	if err := currentActor(actor); err != nil {
		return err
	}
	if ctx == nil {
		return invalid()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	locks := ownerLocks(actor, project, agent, command)
	cause, err := f.NewCommandsCause(command)
	if err != nil {
		return invalid()
	}
	result := a.state.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) (err error) {
		if err = a.state.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := a.inTx(ctx, tx, actor, project, agent, command)
		if err != nil {
			return err
		}
		r, err := loadCommand(ctx, x, project, c.CommandName(command.Command()), command.Key())
		if err != nil {
			return err
		}
		if r == nil || r.User.String() != actor.Details().UserID || r.Target != agent {
			return fault(f.NotFound)
		}
		if r.State != "planned" {
			return fault(f.InvalidState)
		}
		current, err := loadAgent(ctx, x, project, agent)
		if err != nil {
			return err
		}
		if !sameValue(current, r.Before) {
			return fault(f.VersionConflict)
		}
		return check(r)
	})
	return commitError(result)
}

func (Authority) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "agent_authority") }
func (Authority) LogValue() slog.Value       { return slog.StringValue("agent_authority") }
