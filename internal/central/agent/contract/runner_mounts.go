package contract

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
)

// RunnerMountDeclaration is the caller's authorization declaration. It never
// contains server allocations or a physical Runner path.
type RunnerMountDeclaration struct {
	RunnerID  rc.RunnerID `json:"runner_id"`
	Workspace string      `json:"workspace"`
}

func (v RunnerMountDeclaration) Validate() error {
	if v.RunnerID.Validate() != nil || !mc.ValidWorkspaceName(v.Workspace) {
		return invalid("/runner_mounts", "INVALID_RUNNER_MOUNT")
	}
	return nil
}
func (v *RunnerMountDeclaration) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	if _, err := decodeFields(raw, MaxAgentCoreBytes, []string{"runner_id", "workspace"}, nil, nil); err != nil {
		return err
	}
	type wire RunnerMountDeclaration
	var next wire
	if json.Unmarshal(raw, &next) != nil {
		return invalid("", "INVALID_ENCODING")
	}
	if err := RunnerMountDeclaration(next).Validate(); err != nil {
		return err
	}
	*v = RunnerMountDeclaration(next)
	return nil
}
func ValidateRunnerMountDeclarations(v []RunnerMountDeclaration) error {
	if v == nil || len(v) > MaxCapabilityReferences {
		return invalid("/runner_mounts", "INVALID_RUNNER_MOUNTS")
	}
	seen := map[RunnerMountDeclaration]bool{}
	for _, item := range v {
		if item.Validate() != nil || seen[item] {
			return invalid("/runner_mounts", "INVALID_RUNNER_MOUNTS")
		}
		seen[item] = true
	}
	return nil
}

// RunnerMountBinding is frozen in the original Agent command plan and is
// returned only by the declaration profile, including an explicit empty list.
type RunnerMountBinding struct {
	MountID     i.MountID      `json:"mount_id"`
	WorkspaceID mc.WorkspaceID `json:"workspace_id"`
	RunnerID    rc.RunnerID    `json:"runner_id"`
	Workspace   string         `json:"workspace"`
}

func (v RunnerMountBinding) Validate() error {
	if v.MountID.Validate() != nil || v.WorkspaceID.Validate() != nil {
		return invalid("/runner_mounts", "INVALID_RUNNER_MOUNT_BINDING")
	}
	return (RunnerMountDeclaration{v.RunnerID, v.Workspace}).Validate()
}
func (v *RunnerMountBinding) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	if _, err := decodeFields(raw, MaxAgentCoreBytes, []string{"mount_id", "workspace_id", "runner_id", "workspace"}, nil, nil); err != nil {
		return err
	}
	type wire RunnerMountBinding
	var next wire
	if json.Unmarshal(raw, &next) != nil {
		return invalid("", "INVALID_ENCODING")
	}
	if err := RunnerMountBinding(next).Validate(); err != nil {
		return err
	}
	*v = RunnerMountBinding(next)
	return nil
}
func RunnerMountIDs(bindings []RunnerMountBinding) ([]i.MountID, error) {
	if bindings == nil || len(bindings) > MaxCapabilityReferences {
		return nil, invalid("/runner_mounts", "INVALID_RUNNER_MOUNT_BINDINGS")
	}
	ids := make([]i.MountID, 0, len(bindings))
	workspaces := map[mc.WorkspaceID]bool{}
	pairs := make([]RunnerMountDeclaration, 0, len(bindings))
	for _, b := range bindings {
		if b.Validate() != nil || workspaces[b.WorkspaceID] {
			return nil, invalid("/runner_mounts", "INVALID_RUNNER_MOUNT_BINDINGS")
		}
		workspaces[b.WorkspaceID] = true
		ids = append(ids, b.MountID)
		pairs = append(pairs, RunnerMountDeclaration{b.RunnerID, b.Workspace})
	}
	slices.SortFunc(ids, func(a, b i.MountID) int {
		if a.String() < b.String() {
			return -1
		}
		if a == b {
			return 0
		}
		return 1
	})
	if !validReferences(ids) || ValidateRunnerMountDeclarations(pairs) != nil {
		return nil, invalid("/runner_mounts", "INVALID_RUNNER_MOUNT_BINDINGS")
	}
	return ids, nil
}
func MatchRunnerMountBindings(declarations []RunnerMountDeclaration, bindings []RunnerMountBinding, mounts []i.MountID) error {
	ids, err := RunnerMountIDs(bindings)
	if err != nil || ValidateRunnerMountDeclarations(declarations) != nil || len(declarations) != len(bindings) || !slices.Equal(ids, mounts) {
		return invalid("/runner_mounts", "RUNNER_MOUNT_BINDING_MISMATCH")
	}
	for n, d := range declarations {
		if d.RunnerID != bindings[n].RunnerID || d.Workspace != bindings[n].Workspace {
			return invalid("/runner_mounts", "RUNNER_MOUNT_BINDING_MISMATCH")
		}
	}
	return nil
}

type RunnerMountPlanRequest struct {
	Actor                i.Actor
	ProjectID            i.ProjectID
	AgentID              i.AgentID
	Command              f.CommandIdentity
	ExpectedOwnerVersion *f.Version
	Before               *AgentConfig
	Declarations         []RunnerMountDeclaration
}

func (r RunnerMountPlanRequest) Validate() error {
	if !validAgentCommand(r.Actor, r.ProjectID, r.Command) || r.AgentID.Validate() != nil || ValidateRunnerMountDeclarations(r.Declarations) != nil {
		return invalid("", "INVALID_RUNNER_MOUNT_PLAN")
	}
	if r.Command.Command() == "agent.create" {
		if r.ExpectedOwnerVersion != nil || r.Before != nil {
			return invalid("", "INVALID_RUNNER_MOUNT_PLAN")
		}
	} else if r.ExpectedOwnerVersion == nil || r.ExpectedOwnerVersion.Validate() != nil || r.Before == nil || r.Before.Validate() != nil || r.Before.Fields().Core.ID != r.AgentID || r.Before.Fields().Core.ProjectID != r.ProjectID || r.Before.Fields().Core.Version != *r.ExpectedOwnerVersion || r.Before.Fields().Core.Lifecycle != AgentActive {
		return invalid("", "INVALID_RUNNER_MOUNT_PLAN")
	}
	return nil
}
func (r RunnerMountPlanRequest) Clone() RunnerMountPlanRequest {
	r.ExpectedOwnerVersion = clonePtr(r.ExpectedOwnerVersion)
	r.Declarations = slices.Clone(r.Declarations)
	if r.Before != nil {
		v := r.Before.Clone()
		r.Before = &v
	}
	return r
}

type RunnerMountPlanner interface {
	RunnerMountPlanningLocks(RunnerMountPlanRequest) ([]f.LockRequest, error)
	PlanRunnerMountsInTx(context.Context, f.Tx, RunnerMountPlanRequest) ([]RunnerMountBinding, error)
}
type RunnerMountConfigurationItem struct {
	MountID   i.MountID   `json:"mount_id"`
	RunnerID  rc.RunnerID `json:"runner_id"`
	Workspace string      `json:"workspace"`
	Name      string      `json:"name"`
}
type RunnerMountBindingsReader interface {
	RunnerMountBindingsInTx(context.Context, f.Tx, i.Actor, i.ProjectID, i.AgentID, f.Version, []i.MountID) ([]RunnerMountConfigurationItem, error)
}

type RunnerCandidateDirectory interface {
	rc.ConfigurationCandidates
	ListConfigurationCandidatesInTx(context.Context, f.Tx, i.Actor, i.ProjectID, *rc.RunnerID, int) ([]rc.ConfigurationCandidate, error)
}
type AgentRunnerMounts struct {
	ProjectID    i.ProjectID                    `json:"project_id"`
	AgentID      i.AgentID                      `json:"agent_id"`
	AgentVersion f.Version                      `json:"agent_version"`
	Items        []RunnerMountConfigurationItem `json:"items"`
}

func (v AgentRunnerMounts) Validate() error {
	if v.ProjectID.Validate() != nil || v.AgentID.Validate() != nil || v.AgentVersion.Validate() != nil || v.Items == nil || len(v.Items) > MaxCapabilityReferences {
		return invalid("", "INVALID_RUNNER_MOUNT_CONFIGURATION")
	}
	for n, item := range v.Items {
		if item.MountID.Validate() != nil || item.RunnerID.Validate() != nil || !mc.ValidWorkspaceName(item.Workspace) || len(item.Name) < 1 || len(item.Name) > 128 || !utf8.ValidString(item.Name) || strings.ContainsRune(item.Name, 0) || strings.IndexFunc(item.Name, unicode.IsControl) >= 0 || n > 0 && v.Items[n-1].MountID.String() >= item.MountID.String() {
			return invalid("", "INVALID_RUNNER_MOUNT_CONFIGURATION")
		}
	}
	return nil
}
func (v AgentRunnerMounts) Clone() AgentRunnerMounts { v.Items = slices.Clone(v.Items); return v }
func (v AgentRunnerMounts) MarshalJSON() ([]byte, error) {
	type wire AgentRunnerMounts
	return marshalChecked(wire(v), v.Validate(), MaxAgentCoreBytes)
}
