package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// Mounts are logical same-Project/Agent identities, never host paths or Runner
// liveness. The real provider must validate the complete set, preserve valid
// offline references and supply any required new-Agent workspace initialization.
// No implementation or success-on-empty fallback is supplied by Agent.
type MountConfigurationChange struct {
	Actor                i.Actor
	ProjectID            i.ProjectID
	AgentID              i.AgentID
	Command              f.CommandIdentity
	PlanRevision         f.Version
	ExpectedOwnerVersion *f.Version
	ResultOwnerVersion   f.Version
	Before, After        []i.MountID
}

func (r MountConfigurationChange) Validate() error {
	if !validAgentCommand(r.Actor, r.ProjectID, r.Command) || r.AgentID.Validate() != nil || r.PlanRevision.Validate() != nil || r.ResultOwnerVersion.Validate() != nil || !validReferences(r.Before) || !validReferences(r.After) {
		return invalid("", "INVALID_MOUNT_CONFIGURATION")
	}
	if r.Command.Command() == "agent.create" {
		if r.ExpectedOwnerVersion != nil || r.ResultOwnerVersion != 1 || len(r.Before) != 0 {
			return invalid("", "INVALID_MOUNT_CONFIGURATION")
		}
	} else if r.ExpectedOwnerVersion == nil || r.ExpectedOwnerVersion.Validate() != nil || r.ResultOwnerVersion < *r.ExpectedOwnerVersion || r.ResultOwnerVersion-*r.ExpectedOwnerVersion > 1 || !slices.Equal(r.Before, r.After) && r.ResultOwnerVersion == *r.ExpectedOwnerVersion {
		return invalid("", "INVALID_MOUNT_CONFIGURATION")
	}
	return nil
}
func (r MountConfigurationChange) Clone() MountConfigurationChange {
	r.ExpectedOwnerVersion = clonePtr(r.ExpectedOwnerVersion)
	r.Before, r.After = slices.Clone(r.Before), slices.Clone(r.After)
	return r
}

type MountConfigurationPlan interface{ RequiredLocks() []f.LockRequest }
type MountReferenceOwnerPlan interface{ RequiredLocks() []f.LockRequest }
type MountConfiguration interface {
	DiscoverMountConfiguration(context.Context, MountConfigurationChange) (MountConfigurationPlan, error)
	RequireMountConfigurationInTx(context.Context, f.Tx, MountConfigurationChange, MountConfigurationPlan) error
	// This is after Agent's real canonical writer and uses its original ctx.
	// It must check the private creation/preimage/postimage owner witness even
	// for empty sets, then atomically apply initialization/reference facts.
	ApplyMountConfigurationInTx(context.Context, f.Tx, MountConfigurationChange, MountConfigurationPlan) error
}
type MountReferenceOwnerAuthority interface {
	DiscoverMountReferenceOwner(context.Context, MountConfigurationChange) (MountReferenceOwnerPlan, error)
	CheckMountReferenceOwnerAppliedInTx(context.Context, f.Tx, MountConfigurationChange, MountReferenceOwnerPlan) error
}

func (MountConfigurationChange) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "agent_mount_configuration")
}
func (MountConfigurationChange) LogValue() slog.Value {
	return slog.StringValue("agent_mount_configuration")
}
