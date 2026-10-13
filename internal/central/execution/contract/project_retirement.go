package contract

import (
	"context"
	"fmt"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// Binding identifies the complete canonical preimage. The actual row, launch
// bytes and captures remain in the concrete Execution owner's private plan.
// Neither this digest nor a persisted checkpoint is a caller capability.
type ProjectExecutionRetirementBinding struct {
	ProjectID       i.ProjectID   `json:"project_id"`
	AgentID         i.AgentID     `json:"agent_id"`
	ExecutionID     i.ExecutionID `json:"execution_id"`
	Version         f.Version     `json:"version"`
	CanonicalDigest f.Digest      `json:"canonical_digest"`
}

func (b ProjectExecutionRetirementBinding) Validate() error {
	if b.ProjectID.Validate() != nil || b.AgentID.Validate() != nil || b.ExecutionID.Validate() != nil || b.Version.Validate() != nil || b.CanonicalDigest.Validate() != nil {
		return invalid()
	}
	return nil
}

type ProjectExecutionRetirementRequest struct {
	Actor   i.Actor
	Cause   pc.LifecycleCause
	Binding ProjectExecutionRetirementBinding
}

func (r ProjectExecutionRetirementRequest) Validate() error {
	if r.Actor.Validate() != nil || r.Cause.Validate() != nil || r.Binding.Validate() != nil {
		return invalid()
	}
	d := r.Actor.Details()
	if r.Cause.Action != pc.Delete || d.Kind != i.Service || d.ServiceName != i.ProjectLifecycle || d.ProjectID != r.Binding.ProjectID.String() || d.CauseRef != r.Cause.OperationID.String() {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	return nil
}

// Identity is only the original pair's shape. Return/death evidence is always
// supplied by that concrete pair and its original ProcessGuard.
type ProjectExecutionRetirementIdentity struct {
	ProcessID  string `json:"process_id"`
	InstanceID string `json:"instance_id"`
}

func (v ProjectExecutionRetirementIdentity) Validate() error {
	for _, raw := range []string{v.ProcessID, v.InstanceID} {
		if _, err := f.ParseID[struct{}](raw); err != nil {
			return invalid()
		}
	}
	return nil
}

// Commit is the original Project claim and PostgreSQL transaction identity.
// Confirmation preserves this value; it never substitutes its own Tx ID.
type ProjectExecutionRetirementCommit struct {
	ProjectID      i.ProjectID    `json:"project_id"`
	OperationID    pc.OperationID `json:"operation_id"`
	ProjectVersion f.Version      `json:"project_version"`
	ProcessID      string         `json:"process_id"`
	AttemptID      string         `json:"attempt_id"`
	Fence          int64          `json:"fence"`
	TransactionID  string         `json:"transaction_id"`
}

func (v ProjectExecutionRetirementCommit) Validate() error {
	if v.ProjectID.Validate() != nil || v.OperationID.Validate() != nil || v.ProjectVersion.Validate() != nil || v.Fence < 1 || len(v.TransactionID) == 0 || len(v.TransactionID) > 20 || v.TransactionID[0] == '0' {
		return invalid()
	}
	for _, raw := range []string{v.ProcessID, v.AttemptID} {
		if _, err := f.ParseID[struct{}](raw); err != nil {
			return invalid()
		}
	}
	for _, ch := range v.TransactionID {
		if ch < '0' || ch > '9' {
			return invalid()
		}
	}
	return nil
}

func (v ProjectExecutionRetirementCommit) Matches(r ProjectExecutionRetirementRequest) bool {
	return v.Validate() == nil && r.Validate() == nil && v.ProjectID == r.Binding.ProjectID && v.OperationID == r.Cause.OperationID && v.ProjectVersion == r.Cause.ProjectVersion
}

type ProjectExecutionRetirementPlan interface {
	Request() ProjectExecutionRetirementRequest
	RequiredLocks() []f.LockRequest
}

type ProjectExecutionRetirementReferencePlan interface {
	RequiredLocks() []f.LockRequest
	// Digest covers the provider's complete private preimage. It is compared
	// during read-only NotApplied confirmation; it grants no authority.
	ProjectExecutionRetirementReferenceDigest() f.Digest
}

type ProjectExecutionRetirementReferenceKind string

const (
	ProjectExecutionToolReferences      ProjectExecutionRetirementReferenceKind = "tool"
	ProjectExecutionVariablesReferences ProjectExecutionRetirementReferenceKind = "variables"
	ProjectExecutionSkillReferences     ProjectExecutionRetirementReferenceKind = "skill"
	ProjectExecutionMountReferences     ProjectExecutionRetirementReferenceKind = "mount"
)

// The original Execution Authority implements these checks against an active
// concrete consumer/plan in this same Store and live Tx. Prepared cannot stand
// in for Applied. Only Applied can issue the projection used for Skill proofs.
type ProjectExecutionRetirementOwner interface {
	RequireProjectExecutionRetirementPreparedInTx(context.Context, f.Tx, ProjectExecutionRetirementRequest) error
	RequireProjectExecutionRetirementAppliedInTx(context.Context, f.Tx, ProjectExecutionRetirementRequest) error
	ProjectExecutionRetirementProofInTx(context.Context, f.Tx, ProjectExecutionRetirementRequest) (ProjectExecutionRetirementCommit, error)
}

// Discovery is read-only in the original Prepared discovery transaction. Its
// extra locks are acquired with the complete union before the mutation Tx;
// InTx methods never acquire late locks, open transactions, or do external I/O.
type ProjectExecutionRetirementReferences interface {
	ProjectExecutionRetirementReferenceKind() ProjectExecutionRetirementReferenceKind
	DiscoverProjectExecutionReferencesInTx(context.Context, f.Tx, ProjectExecutionRetirementRequest) (ProjectExecutionRetirementReferencePlan, error)
	RetireProjectExecutionReferencesInTx(context.Context, f.Tx, ProjectExecutionRetirementRequest, ProjectExecutionRetirementReferencePlan) error
	RequireProjectExecutionReferencesRetiredInTx(context.Context, f.Tx, ProjectExecutionRetirementRequest, ProjectExecutionRetirementReferencePlan) error
}

// Only the fixed Skill provider has this final phase. It records the original
// queued receipt after Execution deletion, in that same caller transaction.
// Its proof lives until the later exact Agent/Skill retirement consumes it.
type ProjectExecutionSkillRetirementReferences interface {
	ProjectExecutionRetirementReferences
	FinalizeProjectExecutionRetirementInTx(context.Context, f.Tx, ProjectExecutionRetirementRequest, ProjectExecutionRetirementReferencePlan) error
}

// Confirmation is selected by the concrete original owner, never by a public
// request flag. The callback covers the caller Store's actual return. In a
// confirmation call Retire is forbidden; success permits only a later Run to
// write again. Fake Committed callbacks lacking the owner's InTx work fail.
type ProjectExecutionRetirementCall func(context.Context, bool) (f.CommitResult, error)

type ProjectExecutionRetirementConsumer interface {
	ProjectExecutionRetirementIdentity() ProjectExecutionRetirementIdentity
	WithProjectExecutionRetirement(context.Context, i.Actor, pc.LifecycleCause, pc.ScopeRef, ProjectExecutionRetirementCall) (f.CommitResult, error)
	DiscoverProjectExecutionRetirement(context.Context, i.Actor, pc.LifecycleCause, pc.ScopeRef) (ProjectExecutionRetirementPlan, error)
	RestoreProjectExecutionRetirement(context.Context, i.Actor, pc.LifecycleCause, pc.ScopeRef, ProjectExecutionRetirementBinding, ProjectExecutionRetirementCommit, ProjectExecutionRetirementIdentity) (ProjectExecutionRetirementPlan, error)
	RetireProjectExecutionInTx(context.Context, f.Tx, ProjectExecutionRetirementPlan) error
	ConfirmProjectExecutionRetirementInTx(context.Context, f.Tx, ProjectExecutionRetirementPlan) (bool, error)
	RetireProjectExecutionCallsInTx(context.Context, f.Tx, i.Actor, pc.LifecycleCause, pc.ScopeRef) error
	RequireProjectExecutionsEmptyInTx(context.Context, f.Tx, i.Actor, pc.LifecycleCause, pc.ScopeRef) error
}

// Project owns the current cleanup claim. All methods consume the fixed
// driver's private family and preheld Project EX + ProjectSchedule EX.
type ProjectExecutionCleanupAuthority interface {
	RequireProjectExecutionCleanupInTx(context.Context, f.Tx, i.Actor, pc.LifecycleCause, pc.ScopeRef) error
	ProjectExecutionRetirementCommitInTx(context.Context, f.Tx, i.Actor, pc.LifecycleCause, pc.ScopeRef) (ProjectExecutionRetirementCommit, error)
}

func (ProjectExecutionRetirementRequest) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("project_execution_retirement"))
}
func (ProjectExecutionRetirementRequest) LogValue() slog.Value {
	return slog.StringValue("project_execution_retirement")
}
func (ProjectExecutionRetirementRequest) MarshalJSON() ([]byte, error) {
	return []byte(`"project_execution_retirement"`), nil
}
