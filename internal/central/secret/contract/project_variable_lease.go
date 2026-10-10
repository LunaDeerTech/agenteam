package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// ProjectVariableLeaseRequest is metadata, not authority. It is accepted only
// with the Environment provider's private original discovery/final witness.
// The credential's version is rechecked during capture, not pinned as the value
// for later processes. No plaintext, payload, ciphertext or encryption key is
// returned by this SQL-only lease path.
type ProjectVariableLeaseRequest struct {
	ProjectID         i.ProjectID
	AgentID           i.AgentID
	ExecutionID       i.ExecutionID
	VariableID        i.ProjectVariableID
	VariableVersion   f.Version
	AgentVersion      f.Version
	Ref               CredentialRef
	CredentialVersion f.Version
	AttemptBinding    f.Digest
}

func (r ProjectVariableLeaseRequest) Validate() error {
	if r.ProjectID.Validate() != nil || r.AgentID.Validate() != nil || r.ExecutionID.Validate() != nil || r.VariableID.Validate() != nil || r.VariableVersion.Validate() != nil || r.AgentVersion.Validate() != nil || r.Ref.Validate() != nil || r.CredentialVersion.Validate() != nil || r.AttemptBinding.Validate() != nil {
		return bad()
	}
	s := r.Ref.Details().Scope.Details()
	if s.Kind != i.ProjectScope || s.ProjectID != r.ProjectID.String() {
		return bad()
	}
	return nil
}
func (r ProjectVariableLeaseRequest) RequiredLocks() []f.LockRequest {
	if r.Validate() != nil {
		return nil
	}
	p, _ := f.ProjectLock(r.ProjectID.String())
	s, _ := f.ProjectScheduleLock(r.ProjectID.String())
	a, _ := f.AgentLock(r.AgentID.String())
	e, _ := f.AggregateLock(f.ExecutionAggregate, r.ExecutionID.String())
	c, _ := f.AggregateLock(f.CredentialRefAggregate, r.Ref.Details().ID.String())
	// Execution EX already serializes all leases for this Execution; credential
	// EX serializes this credential's references/delete across Executions. A
	// second record key would protect no additional fact and exceed the global
	// lock budget at the supported 256-Secret capability boundary.
	return []f.LockRequest{{Key: p, Mode: f.Shared}, {Key: s, Mode: f.Exclusive}, {Key: a, Mode: f.Shared}, {Key: e, Mode: f.Exclusive}, {Key: c, Mode: f.Exclusive}}
}

type ProjectVariableLeasePlan interface{ RequiredLocks() []f.LockRequest }
type ProjectVariableLeases interface {
	DiscoverProjectVariableLease(context.Context, ProjectVariableLeaseRequest) (ProjectVariableLeasePlan, error)
	AcquireProjectVariableLeaseInTx(context.Context, f.Tx, ProjectVariableLeaseRequest, ProjectVariableLeasePlan) (CredentialLease, error)
}

// A planning check consumes only the Environment authority's private immutable
// candidate established by its real discovery transaction. The final check
// consumes its private same-Tx witness after the real Execution owner has
// validated Trigger, current Agent configuration and the full lock union, and
// after Environment has re-read the exact variable/reference set. These methods
// neither recurse into Secret nor issue a grant from the public request shape.
type ProjectVariableLeaseAuthority interface {
	CheckProjectVariableLeasePlan(context.Context, ProjectVariableLeaseRequest) error
	CheckProjectVariableLeaseInTx(context.Context, f.Tx, ProjectVariableLeaseRequest) error
}

func (ProjectVariableLeaseRequest) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "project_variable_lease_request")
}
func (ProjectVariableLeaseRequest) LogValue() slog.Value {
	return slog.StringValue("project_variable_lease_request")
}
func (ProjectVariableLeaseRequest) MarshalJSON() ([]byte, error) {
	return []byte(`"project_variable_lease_request"`), nil
}
