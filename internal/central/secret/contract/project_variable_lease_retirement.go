package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// This request identifies a captured lease, not current credential material or
// a generic Purpose grant. Credential rotation after capture does not prevent
// retirement of the original lease; its original version remains stored.
type ProjectVariableLeaseRetirementRequest struct {
	ProjectID       i.ProjectID
	AgentID         i.AgentID
	ExecutionID     i.ExecutionID
	VariableID      i.ProjectVariableID
	VariableVersion f.Version
	AgentVersion    f.Version
	Ref             CredentialRef
	LeaseID         LeaseID
	AttemptBinding  f.Digest
}

func (r ProjectVariableLeaseRetirementRequest) Validate() error {
	if r.ProjectID.Validate() != nil || r.AgentID.Validate() != nil || r.ExecutionID.Validate() != nil || r.VariableID.Validate() != nil || r.VariableVersion.Validate() != nil || r.AgentVersion.Validate() != nil || r.Ref.Validate() != nil || r.LeaseID.Validate() != nil || r.AttemptBinding.Validate() != nil {
		return bad()
	}
	scope := r.Ref.Details().Scope.Details()
	if scope.Kind != i.ProjectScope || scope.ProjectID != r.ProjectID.String() {
		return bad()
	}
	return nil
}
func (r ProjectVariableLeaseRetirementRequest) RequiredLocks() []f.LockRequest {
	if r.Validate() != nil {
		return nil
	}
	p, _ := f.ProjectLock(r.ProjectID.String())
	s, _ := f.ProjectScheduleLock(r.ProjectID.String())
	a, _ := f.AgentLock(r.AgentID.String())
	e, _ := f.AggregateLock(f.ExecutionAggregate, r.ExecutionID.String())
	c, _ := f.AggregateLock(f.CredentialRefAggregate, r.Ref.Details().ID.String())
	return []f.LockRequest{{Key: p, Mode: f.Shared}, {Key: s, Mode: f.Exclusive}, {Key: a, Mode: f.Shared}, {Key: e, Mode: f.Exclusive}, {Key: c, Mode: f.Exclusive}}
}

type ProjectVariableLeaseRetirementPlan interface{ RequiredLocks() []f.LockRequest }
type ProjectVariableLeaseRetirement interface {
	DiscoverProjectVariableLeaseRetirement(context.Context, ProjectVariableLeaseRetirementRequest) (ProjectVariableLeaseRetirementPlan, error)
	RetireProjectVariableLeaseInTx(context.Context, f.Tx, ProjectVariableLeaseRetirementRequest, ProjectVariableLeaseRetirementPlan) error
	ProjectVariableLeaseRetiredInTx(context.Context, f.Tx, ProjectVariableLeaseRetirementRequest, ProjectVariableLeaseRetirementPlan) (bool, error)
}

// ProjectVariable alone issues callback-local witnesses after verifying the
// original environment's complete persisted set and real Execution proof.
// Observe and write stages are distinct; neither allows acquisition or value
// reads. These checks never call back into Secret.
type ProjectVariableLeaseRetirementAuthority interface {
	CheckProjectVariableLeaseRetirementPlan(context.Context, ProjectVariableLeaseRetirementRequest) error
	CheckProjectVariableLeaseRetirementInTx(context.Context, f.Tx, ProjectVariableLeaseRetirementRequest) error
	CheckProjectVariableLeaseRetiredInTx(context.Context, f.Tx, ProjectVariableLeaseRetirementRequest) error
}

func (ProjectVariableLeaseRetirementRequest) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "project_variable_lease_retirement_request")
}
func (ProjectVariableLeaseRetirementRequest) LogValue() slog.Value {
	return slog.StringValue("project_variable_lease_retirement_request")
}
func (ProjectVariableLeaseRetirementRequest) MarshalJSON() ([]byte, error) {
	return []byte(`"project_variable_lease_retirement_request"`), nil
}
func (*ProjectVariableLeaseRetirementRequest) UnmarshalJSON([]byte) error { return bad() }
