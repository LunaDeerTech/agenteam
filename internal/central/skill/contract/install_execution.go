package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"unicode"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// InstallExecutionRequest describes Skill's actual input, not a permission.
// Runtime must obtain Operation, current Attempt and selected immutable spec
// from its private active dispatch handoff, never from a caller supplied DTO.
// Read is only an authorized original-operation lookup/replay; it does not
// resurrect a handoff whose synchronous Service call has already returned.
type InstallExecutionRequest struct {
	Actor                         id.Actor
	ProjectID                     id.ProjectID
	Command                       f.CommandIdentity
	RequestID                     f.ID[f.Request]
	Intent                        id.AccessIntent
	SkillID                       SkillID
	NormalizedName                string
	PackageSHA256, ManifestSHA256 f.Digest
	ByteSize                      f.Progress
}

func (r InstallExecutionRequest) Validate() error {
	if r.Actor.Validate() != nil || r.ProjectID.Validate() != nil || r.Command.Validate() != nil ||
		r.RequestID.Validate() != nil || r.SkillID.Validate() != nil || r.PackageSHA256.Validate() != nil ||
		r.ManifestSHA256.Validate() != nil || r.ByteSize <= 0 || r.ByteSize > MaxArchiveBytes ||
		(r.Intent != id.Read && r.Intent != id.Mutate) {
		return invalid()
	}
	a := r.Actor.Details()
	owners := r.Command.OwnerIDs()
	if a.Kind != id.AgentRun || a.ProjectID != r.ProjectID.String() || r.Command.Namespace() != "project" ||
		r.Command.Command() != "skill.install" || len(owners) != 1 || owners[0] != r.ProjectID.String() ||
		!utf8.ValidString(r.NormalizedName) || len(r.NormalizedName) == 0 || len(r.NormalizedName) > MaxNameBytes*3 ||
		r.NormalizedName == "add-skills" {
		return invalid()
	}
	for _, ch := range r.NormalizedName {
		if unicode.IsControl(ch) {
			return invalid()
		}
	}
	return nil
}

func (r InstallExecutionRequest) Equal(other InstallExecutionRequest) bool {
	return r.Validate() == nil && other.Validate() == nil && r.Actor.Equal(other.Actor) &&
		r.ProjectID == other.ProjectID && r.Command.Canonical() == other.Command.Canonical() &&
		r.RequestID == other.RequestID && r.Intent == other.Intent && r.SkillID == other.SkillID &&
		r.NormalizedName == other.NormalizedName && r.PackageSHA256 == other.PackageSHA256 &&
		r.ManifestSHA256 == other.ManifestSHA256 && r.ByteSize == other.ByteSize
}

// InstallExecutionBinding is immutable provenance projected by the registered
// producer, not a grant or a replacement for RequireInstallExecutionInTx.
// UUID strings refer to Tool-owned identities without introducing another ID
// owner or a reverse dependency from Skill to Tool's implementation/contract.
// AttemptID and RequestID are physical-call facts, not Operation semantics.
type InstallExecutionBinding struct {
	OperationID, AttemptID string
	ToolID                 id.ToolID
	SpecRevision           f.Version
	HandlerID              string
	ContractRevision       f.Version
	Fingerprint            f.Digest
}

func (b InstallExecutionBinding) Validate() error {
	operation, e1 := f.ParseID[struct{}](b.OperationID)
	attempt, e2 := f.ParseID[struct{}](b.AttemptID)
	if e1 != nil || e2 != nil || operation.String() != b.OperationID || attempt.String() != b.AttemptID ||
		b.ToolID.Validate() != nil || b.SpecRevision.Validate() != nil || b.HandlerID != "skill.install" ||
		b.ContractRevision != 1 || b.Fingerprint.Validate() != nil {
		return invalid()
	}
	return nil
}

func (b InstallExecutionBinding) Matches(r InstallExecutionRequest) bool {
	return b.Validate() == nil && r.Validate() == nil &&
		r.Command.Key().String() == "tool.skill.install:"+b.OperationID
}

// A plan's concrete issuer belongs to the Runtime provider. The provider must
// reject foreign plans, even if these public projections happen to match.
// RequiredLocks returns an independent complete list, including Runtime's own
// command and current Execution/Policy facts. Skill adds its distinct command,
// catalogue, Skill and Object locks, then acquires the entire union once.
type InstallExecutionPlan interface {
	RequiredLocks() []f.LockRequest
	Binding() InstallExecutionBinding
}

// Discovery is outside Skill's caller transaction; the final requirement uses
// the same Store and original Tx and performs no I/O, Begin or lock extension.
// It checks the exact request, original P/A/E/Operation/Attempt/spec/binding,
// current Execution and Policy, and the still-active private dispatch handoff.
// The producer retains that handoff until the original synchronous Service
// call, including its actual physical cleanup, has returned. Cancellation alone
// cannot revoke the producer's responsibility to join that call.
type InstallExecutionAuthority interface {
	DiscoverInstallExecution(context.Context, InstallExecutionRequest) (InstallExecutionPlan, error)
	RequireInstallExecutionInTx(context.Context, f.Tx, InstallExecutionRequest, InstallExecutionPlan) error
}

func (InstallExecutionRequest) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "skill_install_execution")
}
func (InstallExecutionRequest) LogValue() slog.Value {
	return slog.StringValue("skill_install_execution")
}
func (InstallExecutionRequest) MarshalJSON() ([]byte, error) {
	return []byte(`"skill_install_execution"`), nil
}
func (*InstallExecutionRequest) UnmarshalJSON([]byte) error { return invalid() }
func (InstallExecutionBinding) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "skill_install_binding")
}
func (InstallExecutionBinding) LogValue() slog.Value {
	return slog.StringValue("skill_install_binding")
}
func (InstallExecutionBinding) MarshalJSON() ([]byte, error) {
	return []byte(`"skill_install_binding"`), nil
}
func (*InstallExecutionBinding) UnmarshalJSON([]byte) error { return invalid() }
