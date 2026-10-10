package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// SkillCaptureRequest is an identity, never preparing authority. Attempt,
// fence, process, original Launch and cancellation belong to Execution's
// private live call, not caller-supplied fields or an AgentRun constructor.
type SkillCaptureRequest struct {
	ProjectID   id.ProjectID   `json:"project_id"`
	AgentID     id.AgentID     `json:"agent_id"`
	ExecutionID id.ExecutionID `json:"execution_id"`
}

func (r SkillCaptureRequest) Validate() error {
	if r.ProjectID.Validate() != nil || r.AgentID.Validate() != nil || r.ExecutionID.Validate() != nil {
		return invalid()
	}
	return nil
}

// Scope is a callback-local fact, not a bearer grant. AttemptBinding is made
// by the real Execution owner from the original request/claim/process/fence.
// The provider freezes it in its private plan and checks it again in final Tx.
type SkillCaptureScope struct {
	Project        pc.ProjectRef
	AttemptBinding f.Digest
}

// Both callbacks verify the original same-Store live Tx, complete preheld
// union, real preparing claim, current Project and no cancellation. Discovery
// permits only source/lock planning. Final additionally requires this call's
// actual Trigger and Agent capture to have returned successfully. Neither
// callback may recurse into Skill or require an already-complete Snapshot.
type SkillCaptureAuthority interface {
	RequireSkillCaptureDiscoveryInTx(context.Context, f.Tx, SkillCaptureRequest) (SkillCaptureScope, error)
	RequireSkillCaptureInTx(context.Context, f.Tx, SkillCaptureRequest) (SkillCaptureScope, error)
}

// SkillBinding pins the immutable revision and its catalog metadata. Stable
// Agent assignment references and initialization ObservedRevision alone are
// insufficient. Package content is read later through the fixed revision.
type SkillBinding struct {
	SkillID            SkillID      `json:"skill_id"`
	RevisionID         RevisionID   `json:"revision_id"`
	Revision           f.Revision   `json:"revision"`
	AssignmentID       AssignmentID `json:"assignment_id"`
	AssignmentSequence f.Version    `json:"assignment_sequence"`
	Name               string       `json:"name"`
	Description        string       `json:"description"`
	PackageSHA256      f.Digest     `json:"package_sha256"`
	EntryPath          string       `json:"entry_path"`
}

func (b SkillBinding) Validate() error {
	if b.SkillID.Validate() != nil || b.RevisionID.Validate() != nil || b.Revision.Validate() != nil ||
		b.AssignmentID.Validate() != nil || b.AssignmentSequence.Validate() != nil ||
		!displayText(b.Description, MaxDescriptionBytes, true) || b.PackageSHA256.Validate() != nil || b.EntryPath != EntryPath {
		return invalid()
	}
	if _, err := NormalizeName(b.Name); err != nil {
		return invalid()
	}
	return nil
}

type InitialSkillBindings struct {
	Request            SkillCaptureRequest `json:"request"`
	AssignmentSequence f.Version           `json:"assignment_sequence"`
	Bindings           []SkillBinding      `json:"bindings"`
}

func (v InitialSkillBindings) Validate() error {
	if v.Request.Validate() != nil || v.AssignmentSequence.Validate() != nil || v.Bindings == nil {
		return invalid()
	}
	seen := make(map[AssignmentID]bool, len(v.Bindings))
	for n, b := range v.Bindings {
		if b.Validate() != nil || b.AssignmentSequence > v.AssignmentSequence || seen[b.AssignmentID] ||
			n > 0 && v.Bindings[n-1].SkillID.String() >= b.SkillID.String() {
			return invalid()
		}
		seen[b.AssignmentID] = true
	}
	return nil
}
func (v InitialSkillBindings) Clone() InitialSkillBindings {
	v.Bindings = slices.Clone(v.Bindings)
	return v
}

type InitialBindingsPlan interface{ RequiredLocks() []f.LockRequest }

// Resolve writes only Skill-owned fixed-revision protection in the caller Tx.
// Execution must atomically persist the complete preparation input and all
// other owner references/leases or roll back this whole transaction. Even an
// empty result requires the real initialization head and current publication.
// Discovery writes no binding and never acquires late locks in final Tx.
type InitialBindingsProvider interface {
	DiscoverInitialBindings(context.Context, SkillCaptureRequest) (InitialBindingsPlan, error)
	ResolveInitialBindingsInTx(context.Context, f.Tx, SkillCaptureRequest, InitialBindingsPlan) (InitialSkillBindings, error)
}

func (SkillCaptureRequest) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "skill_capture_request")
}
func (SkillCaptureRequest) LogValue() slog.Value     { return slog.StringValue("skill_capture_request") }
func (SkillCaptureScope) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "skill_capture_scope") }
func (SkillCaptureScope) LogValue() slog.Value       { return slog.StringValue("skill_capture_scope") }
func (InitialSkillBindings) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "initial_skill_bindings")
}
func (InitialSkillBindings) LogValue() slog.Value { return slog.StringValue("initial_skill_bindings") }
