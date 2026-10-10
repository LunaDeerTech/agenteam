package contract

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// SkillsInitializationRequest refers to the original unpublished Agent create.
// False disables only the assignment, never the real owner/provider checks.
type SkillsInitializationRequest struct {
	Actor            i.Actor
	ProjectID        i.ProjectID
	AgentID          i.AgentID
	Command          f.CommandIdentity
	PlanRevision     f.Version
	AddSkillsEnabled bool
}

func validAgentCommand(actor i.Actor, project i.ProjectID, command f.CommandIdentity) bool {
	return actor.Validate() == nil && actor.Details().Kind == i.Human && project.Validate() == nil &&
		command.Validate() == nil && command.Namespace() == "project" &&
		slices.Equal(command.OwnerIDs(), []string{project.String()}) &&
		(command.Command() == "agent.create" || command.Command() == "agent.update")
}

func (r SkillsInitializationRequest) Validate() error {
	if !validAgentCommand(r.Actor, r.ProjectID, r.Command) || r.Command.Command() != "agent.create" || r.AgentID.Validate() != nil || r.PlanRevision.Validate() != nil {
		return invalid("", "INVALID_AGENT_INITIALIZATION")
	}
	return nil
}
func (r SkillsInitializationRequest) Clone() SkillsInitializationRequest { return r }

// Implementations own their private concrete plan and issuer. A lock list,
// arbitrary implementation of this interface, or successful Validate is not
// authority. Only the original provider may accept its plan in the original
// live Store transaction after rechecking all dependencies.
type SkillsInitializationPlan interface {
	RequiredLocks() []f.LockRequest
}

type AgentSkillsInitializer interface {
	DiscoverNewAgentInitialization(context.Context, SkillsInitializationRequest) (SkillsInitializationPlan, error)
	InitializeNewAgentInTx(context.Context, f.Tx, SkillsInitializationRequest, SkillsInitializationPlan) error
}

type NewAgentCreationPlan interface {
	RequiredLocks() []f.LockRequest
}

// Agent is the sole implementation. Discover binds its persistent planned
// command/revision and the original default. CheckAppliedInTx additionally
// needs the unique canonical writer's same-Tx create-absent/postimage witness;
// it rejects previously published Agents, public DTOs and mere row existence.
// Neither operation fabricates no_active_execution or runtime assignments.
type NewAgentCreationAuthority interface {
	DiscoverNewAgentCreation(context.Context, SkillsInitializationRequest) (NewAgentCreationPlan, error)
	CheckNewAgentCreationAppliedInTx(context.Context, f.Tx, SkillsInitializationRequest, NewAgentCreationPlan) error
}

func (SkillsInitializationRequest) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("agent_skills_initialization"))
}
func (SkillsInitializationRequest) LogValue() slog.Value {
	return slog.StringValue("agent_skills_initialization")
}
