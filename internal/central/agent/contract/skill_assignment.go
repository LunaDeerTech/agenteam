package contract

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// SkillAssignmentRequest binds the Skill command to its current Agent owner.
// ExpectedVersion is Agent.config_version, not the Skill assignment sequence.
// A completed original command may replay before this version is checked.
type SkillAssignmentRequest struct {
	Actor           i.Actor
	ProjectID       i.ProjectID
	AgentID         i.AgentID
	Command         f.CommandIdentity
	ExpectedVersion f.Version
}

func (r SkillAssignmentRequest) Validate() error {
	if r.Actor.Validate() != nil || r.Actor.Details().Kind != i.Human || r.ProjectID.Validate() != nil || r.AgentID.Validate() != nil || r.ExpectedVersion.Validate() != nil ||
		r.Command.Validate() != nil || r.Command.Namespace() != "project" || !slices.Equal(r.Command.OwnerIDs(), []string{r.ProjectID.String(), r.AgentID.String()}) ||
		(r.Command.Command() != "skill.assign" && r.Command.Command() != "skill.remove") {
		return invalid("", "INVALID_SKILL_ASSIGNMENT_OWNER")
	}
	return nil
}

func (r SkillAssignmentRequest) RequiredLocks() []f.LockRequest {
	if r.Validate() != nil {
		return nil
	}
	command, _ := f.CommandLock(r.Command)
	user, _ := f.UserLock(r.Actor.Details().UserID)
	project, _ := f.ProjectLock(r.ProjectID.String())
	schedule, _ := f.ProjectScheduleLock(r.ProjectID.String())
	agent, _ := f.AgentLock(r.AgentID.String())
	return []f.LockRequest{{Key: command, Mode: f.Exclusive}, {Key: user, Mode: f.Exclusive}, {Key: project, Mode: f.Shared}, {Key: schedule, Mode: f.Exclusive}, {Key: agent, Mode: f.Exclusive}}
}

type SkillAssignmentPlan interface {
	RequiredLocks() []f.LockRequest
}

// Prepare observes current Owner Read and the initialized Agent before looking
// up the original Skill command. Completed replay returns false before checking
// ExpectedVersion and may observe an initialized deleting Agent. A new command
// must require current Owner Mutate, check ExpectedVersion, then require active
// Agent authority. A new no-op may record its unchanged receipt in
// Prepare, also returning false. A real change returns true with only its
// mutation plan prepared. Agent then synchronously advances its canonical
// version and all four reference owners before calling Finalize with the new
// Ref. Finalize writes the Skill assignment/change/receipt and runtime ingress
// in the same transaction. Any error must roll back the caller transaction.
// No callable version writer or private write witness escapes either callback.
type SkillAssignmentOwner interface {
	// Read checks the current Owner and initialized Agent in the caller's held
	// transaction. Historical lookup permits deleting Agents and stale expected
	// versions; it neither discovers reference locks nor changes configuration.
	ReadSkillAssignmentInTx(context.Context, f.Tx, SkillAssignmentRequest) (AgentRef, error)
	DiscoverSkillAssignment(context.Context, SkillAssignmentRequest) (SkillAssignmentPlan, error)
	WithSkillAssignmentInTx(context.Context, f.Tx, SkillAssignmentRequest, SkillAssignmentPlan,
		func(context.Context, AgentRef) (bool, error), func(context.Context, AgentRef) error) error
}

// SkillAssignmentVersionChange retains every configuration field and resource
// selection. Only the Agent version and updated instant advance. Providers
// must verify the Agent owner's private same-Tx canonical-write witness before
// reading or writing their own complete reference index. This DTO is no grant.
type SkillAssignmentVersionChange struct {
	Request       SkillAssignmentRequest
	Before        AgentConfig
	ResultVersion f.Version
}

func (r SkillAssignmentVersionChange) Validate() error {
	if r.Request.Validate() != nil || r.Before.Validate() != nil {
		return invalid("", "INVALID_SKILL_ASSIGNMENT_VERSION")
	}
	core := r.Before.Fields().Core
	if core.ProjectID != r.Request.ProjectID || core.ID != r.Request.AgentID || core.Lifecycle != AgentActive || core.Version != r.Request.ExpectedVersion || core.Version == math.MaxInt64 || r.ResultVersion != core.Version+1 {
		return invalid("", "INVALID_SKILL_ASSIGNMENT_VERSION")
	}
	return nil
}

func (r SkillAssignmentVersionChange) Clone() SkillAssignmentVersionChange {
	r.Before = r.Before.Clone()
	return r
}

type SkillAssignmentVersionOwner interface {
	RequireSkillAssignmentVersionAppliedInTx(context.Context, f.Tx, SkillAssignmentVersionChange) error
}

type SkillAssignmentReferenceKind string

const (
	SkillAssignmentToolReferences   SkillAssignmentReferenceKind = "tool"
	SkillAssignmentMountReferences  SkillAssignmentReferenceKind = "mount"
	SkillAssignmentModelReferences  SkillAssignmentReferenceKind = "model"
	SkillAssignmentSecretReferences SkillAssignmentReferenceKind = "secret"
)

// Every one of the four real providers is mandatory, including empty sets.
// RequiredLocks is pure discovery from the unchanged canonical selections;
// Apply requires that complete set already held in the original live Tx. It
// may change only its owner version after verifying all original references.
// It must neither select resources anew nor add/remove/replace a reference.
type SkillAssignmentVersionReferences interface {
	SkillAssignmentReferenceKind() SkillAssignmentReferenceKind
	SkillAssignmentVersionLocks(SkillAssignmentVersionChange) ([]f.LockRequest, error)
	AdvanceSkillAssignmentVersionInTx(context.Context, f.Tx, SkillAssignmentVersionChange) error
}

func (SkillAssignmentRequest) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("skill_assignment_owner_request"))
}
func (SkillAssignmentRequest) LogValue() slog.Value {
	return slog.StringValue("skill_assignment_owner_request")
}
func (SkillAssignmentRequest) MarshalJSON() ([]byte, error) {
	return []byte(`"skill_assignment_owner_request"`), nil
}
func (*SkillAssignmentRequest) UnmarshalJSON([]byte) error {
	return invalid("", "INVALID_SKILL_ASSIGNMENT_OWNER")
}
func (SkillAssignmentVersionChange) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("skill_assignment_version_change"))
}
func (SkillAssignmentVersionChange) LogValue() slog.Value {
	return slog.StringValue("skill_assignment_version_change")
}
func (SkillAssignmentVersionChange) MarshalJSON() ([]byte, error) {
	return []byte(`"skill_assignment_version_change"`), nil
}
func (*SkillAssignmentVersionChange) UnmarshalJSON([]byte) error {
	return invalid("", "INVALID_SKILL_ASSIGNMENT_VERSION")
}
