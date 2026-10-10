package contract

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// This consumer-owned port depends only on layer-one identities. Agent never
// imports ToolSpec, tool schemas, backend bindings, or model-visible names.
type ToolConfigurationRequest struct {
	Actor            i.Actor
	ProjectID        i.ProjectID
	AgentID          i.AgentID
	Command          f.CommandIdentity
	RequestedToolIDs []i.ToolID
	// Present only for create; omission has already expanded to true.
	InstallSkillEnabled *bool
}

func (r ToolConfigurationRequest) Validate() error {
	if !validAgentCommand(r.Actor, r.ProjectID, r.Command) || r.AgentID.Validate() != nil || !validReferences(r.RequestedToolIDs) || (r.Command.Command() == "agent.create") != (r.InstallSkillEnabled != nil) {
		return invalid("", "INVALID_TOOL_CONFIGURATION")
	}
	return nil
}
func (r ToolConfigurationRequest) Clone() ToolConfigurationRequest {
	r.RequestedToolIDs = slices.Clone(r.RequestedToolIDs)
	r.InstallSkillEnabled = clonePtr(r.InstallSkillEnabled)
	return r
}

type ToolConfigurationPlan interface {
	RequiredLocks() []f.LockRequest
	// The provider resolves the default's real stable ID during discovery.
	// These sorted IDs are frozen into the Agent planned postimage before
	// reference discovery. Apply must never silently enlarge that postimage.
	ResolvedToolIDs() []i.ToolID
}

type ToolConfigurationDirectory interface {
	DiscoverConfigurationTools(context.Context, ToolConfigurationRequest) (ToolConfigurationPlan, error)
	RequireConfigurationToolsInTx(context.Context, f.Tx, ToolConfigurationRequest, ToolConfigurationPlan) error
}

type ToolReferenceChange struct {
	Actor                i.Actor
	ProjectID            i.ProjectID
	AgentID              i.AgentID
	Command              f.CommandIdentity
	PlanRevision         f.Version
	ExpectedOwnerVersion *f.Version
	ResultOwnerVersion   f.Version
	Before, After        []i.ToolID
}

func (r ToolReferenceChange) Validate() error {
	if !validAgentCommand(r.Actor, r.ProjectID, r.Command) || r.AgentID.Validate() != nil || r.PlanRevision.Validate() != nil || r.ResultOwnerVersion.Validate() != nil || !validReferences(r.Before) || !validReferences(r.After) {
		return invalid("", "INVALID_TOOL_REFERENCES")
	}
	if r.Command.Command() == "agent.create" {
		if r.ExpectedOwnerVersion != nil || r.ResultOwnerVersion != 1 || len(r.Before) != 0 {
			return invalid("", "INVALID_TOOL_REFERENCES")
		}
	} else if r.ExpectedOwnerVersion == nil || r.ExpectedOwnerVersion.Validate() != nil || r.ResultOwnerVersion < *r.ExpectedOwnerVersion || r.ResultOwnerVersion-*r.ExpectedOwnerVersion > 1 || !slices.Equal(r.Before, r.After) && r.ResultOwnerVersion == *r.ExpectedOwnerVersion {
		return invalid("", "INVALID_TOOL_REFERENCES")
	}
	return nil
}
func (r ToolReferenceChange) Clone() ToolReferenceChange {
	r.ExpectedOwnerVersion = clonePtr(r.ExpectedOwnerVersion)
	r.Before, r.After = slices.Clone(r.Before), slices.Clone(r.After)
	return r
}

type ToolReferencePlan interface{ RequiredLocks() []f.LockRequest }
type ToolReferenceOwnerPlan interface{ RequiredLocks() []f.LockRequest }

type AgentToolReferences interface {
	DiscoverToolReferences(context.Context, ToolReferenceChange) (ToolReferencePlan, error)
	ApplyToolReferencesInTx(context.Context, f.Tx, ToolReferenceChange, ToolReferencePlan) error
}

// Agent's adapter validates both the original planned command and its private
// canonical writer witness. Even an empty set needs this actual authority.
type ToolReferenceOwnerAuthority interface {
	DiscoverToolReferenceOwner(context.Context, ToolReferenceChange) (ToolReferenceOwnerPlan, error)
	CheckToolReferenceOwnerAppliedInTx(context.Context, f.Tx, ToolReferenceChange, ToolReferenceOwnerPlan) error
}

func (ToolConfigurationRequest) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("agent_tool_configuration"))
}
func (ToolConfigurationRequest) LogValue() slog.Value {
	return slog.StringValue("agent_tool_configuration")
}
func (ToolReferenceChange) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("agent_tool_references"))
}
func (ToolReferenceChange) LogValue() slog.Value {
	return slog.StringValue("agent_tool_references")
}
