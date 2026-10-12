package contract

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// ProjectRetirementRequest identifies a Project Delete cleanup transaction.
// These public fields are not an issuer or proof that any original call joined.
// Ordinary Human Agent deletion and the Work current-reference port keep their
// existing rules, including references retained by terminal Tasks.
type ProjectRetirementRequest struct {
	Actor           i.Actor
	ProjectID       i.ProjectID
	AgentID         i.AgentID
	Cause           pc.LifecycleCause
	ExpectedVersion f.Version
}

func (r ProjectRetirementRequest) Validate() error {
	if r.Actor.Validate() != nil || r.ProjectID.Validate() != nil || r.AgentID.Validate() != nil || r.Cause.Validate() != nil || r.ExpectedVersion.Validate() != nil {
		return invalid("", "INVALID_PROJECT_AGENT_RETIREMENT")
	}
	d := r.Actor.Details()
	if r.Cause.Action != pc.Delete || d.Kind != i.Service || d.ServiceName != i.ProjectLifecycle || d.ProjectID != r.ProjectID.String() || d.CauseRef != r.Cause.OperationID.String() {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	return nil
}

// Callers acquire the complete union, including each reference owner's locks,
// before entering the mutation. These methods only require already-held locks.
func (r ProjectRetirementRequest) RequiredLocks() []f.LockRequest {
	if r.Validate() != nil {
		return nil
	}
	project, _ := f.ProjectLock(r.ProjectID.String())
	schedule, _ := f.ProjectScheduleLock(r.ProjectID.String())
	agent, _ := f.AgentLock(r.AgentID.String())
	return []f.LockRequest{{Key: project, Mode: f.Exclusive}, {Key: schedule, Mode: f.Exclusive}, {Key: agent, Mode: f.Exclusive}}
}

// Before is the complete original configuration. No synthetic postimage or
// version increment is used for physical retirement. The original completed
// command and its immutable after_config remain available for exact recovery.
type ProjectRetirementChange struct {
	Request ProjectRetirementRequest
	Before  AgentConfig
}

func (c ProjectRetirementChange) Validate() error {
	if err := c.Request.Validate(); err != nil {
		return err
	}
	if c.Before.Validate() != nil {
		return invalid("", "INVALID_PROJECT_AGENT_RETIREMENT")
	}
	b := c.Before.Fields().Core
	if b.ProjectID != c.Request.ProjectID || b.ID != c.Request.AgentID || b.Version != c.Request.ExpectedVersion || b.Lifecycle != AgentActive {
		return invalid("", "INVALID_PROJECT_AGENT_RETIREMENT")
	}
	return nil
}

func (c ProjectRetirementChange) Clone() ProjectRetirementChange {
	c.Before = c.Before.Clone()
	return c
}

// Only the original Agent consumer creates these private, synchronous same-Tx
// witnesses. Prepared binds the live Project cleanup claim and exact Before.
// Applied additionally proves the canonical deletion in this transaction, or
// an exact Project-owned applied checkpoint plus its immutable command during
// recovery. An absent canonical row or an empty reference table is insufficient.
// Saving a context, leaving the callback, changing Tx or changing any binding
// cannot preserve or recreate either witness.
type ProjectRetirementOwner interface {
	RequireProjectRetirementPreparedInTx(context.Context, f.Tx, ProjectRetirementChange) error
	RequireProjectRetirementAppliedInTx(context.Context, f.Tx, ProjectRetirementChange) error
}

// A gate confirms only its own domain. Work preserves all Task fields/history
// under the irreversible Project Delete gate; Execution rejects any historical
// row, in any state. Neither result grants retirement outside the live caller.
type ProjectRetirementGate interface {
	RequireProjectAgentRetirementInTx(context.Context, f.Tx, ProjectRetirementChange) error
}

type ProjectRetirementReferenceKind string

const (
	ProjectRetirementModelReferences  ProjectRetirementReferenceKind = "model"
	ProjectRetirementToolReferences   ProjectRetirementReferenceKind = "tool"
	ProjectRetirementMountReferences  ProjectRetirementReferenceKind = "mount"
	ProjectRetirementSecretReferences ProjectRetirementReferenceKind = "secret"
	ProjectRetirementSkillReferences  ProjectRetirementReferenceKind = "skill"
)

// All five concrete owners are required even for empty selections: Tool and
// Mount retain initialized heads, and a disabled Skill assignment retains its
// initialization receipt. Each owner verifies its complete original index and
// expected version, mutates only its own records, and consumes Applied. The
// Project caller commits their changes together with canonical Agent deletion
// and the exact applied checkpoint; any error rolls back the whole transaction.
// Retired confirmation consumes the same scoped recovery witness before SQL.
type ProjectRetirementReferences interface {
	ProjectRetirementReferenceKind() ProjectRetirementReferenceKind
	ProjectRetirementLocks(ProjectRetirementChange) ([]f.LockRequest, error)
	RetireProjectAgentReferencesInTx(context.Context, f.Tx, ProjectRetirementChange) error
	RequireProjectAgentReferencesRetiredInTx(context.Context, f.Tx, ProjectRetirementChange) error
}

// A completed command supplies the immutable configuration fields. Nil
// BeforeUpdatedAt keeps the original exact-postimage contract. Otherwise only
// version and updated_at may differ: Agent must reconstruct from that command
// and compare the entire result with the actual current Before in the original
// transaction. Project persists the exact time with version/command/digest;
// recovery reconstructs and hashes the complete configuration, never a guess.
type ProjectRetirementPlan struct {
	Change          ProjectRetirementChange
	CommandID       AgentCommandID
	BeforeDigest    f.Digest
	BeforeUpdatedAt *f.Instant
}

func ProjectRetirementBeforeDigest(before AgentConfig) (f.Digest, error) {
	if err := before.Validate(); err != nil {
		return "", err
	}
	raw, err := json.Marshal(before)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return f.Digest("sha256:" + hex.EncodeToString(sum[:])), nil
}

func (p ProjectRetirementPlan) Validate() error {
	if err := p.Change.Validate(); err != nil {
		return err
	}
	digest, err := ProjectRetirementBeforeDigest(p.Change.Before)
	if p.CommandID.Validate() != nil || p.BeforeDigest.Validate() != nil || err != nil || p.BeforeDigest != digest {
		return invalid("", "INVALID_PROJECT_AGENT_RETIREMENT_PLAN")
	}
	if p.BeforeUpdatedAt != nil && (p.BeforeUpdatedAt.Validate() != nil || *p.BeforeUpdatedAt != p.Change.Before.Fields().Core.UpdatedAt) {
		return invalid("", "INVALID_PROJECT_AGENT_RETIREMENT_PLAN")
	}
	return nil
}

func (p ProjectRetirementPlan) Clone() ProjectRetirementPlan {
	p.Change = p.Change.Clone()
	if p.BeforeUpdatedAt != nil {
		at := *p.BeforeUpdatedAt
		p.BeforeUpdatedAt = &at
	}
	return p
}

// Discovery/restoration own only their original read transactions. Project
// owns the mutation Tx, complete lock union, exact applied checkpoint and its
// actual Commit return. A nil discovery is a current canonical observation,
// never confirmation of a missing applied checkpoint or an Unknown attempt.
type ProjectRetirementConsumer interface {
	DiscoverProjectRetirement(context.Context, i.Actor, pc.LifecycleCause, i.ProjectID) (*ProjectRetirementPlan, error)
	RequireProjectAgentsEmptyInTx(context.Context, f.Tx, i.Actor, pc.LifecycleCause, i.ProjectID) error
	ProjectRetirementLocks(ProjectRetirementPlan) ([]f.LockRequest, error)
	RetireProjectAgentInTx(context.Context, f.Tx, ProjectRetirementPlan) error
	RestoreProjectRetirement(context.Context, i.Actor, pc.LifecycleCause, i.ProjectID, i.AgentID, f.Version, AgentCommandID, f.Digest, *f.Instant) (ProjectRetirementPlan, error)
	RequireProjectAgentRetiredInTx(context.Context, f.Tx, ProjectRetirementPlan) error
}

func (ProjectRetirementPlan) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("project_agent_retirement_plan"))
}
func (ProjectRetirementPlan) LogValue() slog.Value {
	return slog.StringValue("project_agent_retirement_plan")
}
func (ProjectRetirementPlan) MarshalJSON() ([]byte, error) {
	return []byte(`"project_agent_retirement_plan"`), nil
}

func (ProjectRetirementRequest) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("project_agent_retirement"))
}
func (ProjectRetirementRequest) LogValue() slog.Value {
	return slog.StringValue("project_agent_retirement")
}
func (ProjectRetirementRequest) MarshalJSON() ([]byte, error) {
	return []byte(`"project_agent_retirement"`), nil
}
func (ProjectRetirementChange) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("project_agent_retirement_change"))
}
func (ProjectRetirementChange) LogValue() slog.Value {
	return slog.StringValue("project_agent_retirement_change")
}
func (ProjectRetirementChange) MarshalJSON() ([]byte, error) {
	return []byte(`"project_agent_retirement_change"`), nil
}
