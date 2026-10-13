package contract

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// These are immutable PV capture facts, not permission to retire an Execution.
// The concrete PV bridge supplies the original Execution owner's private scope.
// This leaf contract deliberately has no dependency on Execution contracts.
type ProjectExecutionLeaseReference struct {
	VariableID i.ProjectVariableID
	LeaseID    LeaseID
}

type ProjectExecutionLeaseRetirementRequest struct {
	ProjectID          i.ProjectID
	AgentID            i.AgentID
	ExecutionID        i.ExecutionID
	EnvironmentPresent bool
	AgentVersion       f.Version
	AttemptBinding     f.Digest
	CaptureDigest      f.Digest
	OrdinaryCount      int
	SecretCount        int
	References         []ProjectExecutionLeaseReference
}

func (r ProjectExecutionLeaseRetirementRequest) Validate() error {
	if r.ProjectID.Validate() != nil || r.AgentID.Validate() != nil || r.ExecutionID.Validate() != nil || len(r.References) > 256 {
		return bad()
	}
	if !r.EnvironmentPresent {
		if r.AgentVersion != 0 || r.AttemptBinding != "" || r.CaptureDigest != "" || r.OrdinaryCount != 0 || r.SecretCount != 0 || len(r.References) != 0 {
			return bad()
		}
		return nil
	}
	if r.AgentVersion.Validate() != nil || r.AttemptBinding.Validate() != nil || r.CaptureDigest.Validate() != nil || r.OrdinaryCount < 0 || r.OrdinaryCount > 4096 || r.SecretCount != len(r.References) {
		return bad()
	}
	leases := make(map[LeaseID]bool, len(r.References))
	for n, ref := range r.References {
		if ref.VariableID.Validate() != nil || ref.LeaseID.Validate() != nil || leases[ref.LeaseID] || n > 0 && r.References[n-1].VariableID.String() >= ref.VariableID.String() {
			return bad()
		}
		leases[ref.LeaseID] = true
	}
	return nil
}

func (r ProjectExecutionLeaseRetirementRequest) Clone() ProjectExecutionLeaseRetirementRequest {
	r.References = slices.Clone(r.References)
	return r
}
func (r ProjectExecutionLeaseRetirementRequest) Equal(other ProjectExecutionLeaseRetirementRequest) bool {
	a, b := r, other
	a.References, b.References = nil, nil
	return a.ProjectID == b.ProjectID && a.AgentID == b.AgentID && a.ExecutionID == b.ExecutionID && a.EnvironmentPresent == b.EnvironmentPresent && a.AgentVersion == b.AgentVersion && a.AttemptBinding == b.AttemptBinding && a.CaptureDigest == b.CaptureDigest && a.OrdinaryCount == b.OrdinaryCount && a.SecretCount == b.SecretCount && slices.Equal(r.References, other.References)
}
func (r ProjectExecutionLeaseRetirementRequest) RequiredLocks() []f.LockRequest {
	if r.Validate() != nil {
		return nil
	}
	p, _ := f.ProjectLock(r.ProjectID.String())
	s, _ := f.ProjectScheduleLock(r.ProjectID.String())
	a, _ := f.AgentLock(r.AgentID.String())
	e, _ := f.AggregateLock(f.ExecutionAggregate, r.ExecutionID.String())
	return []f.LockRequest{{Key: p, Mode: f.Exclusive}, {Key: s, Mode: f.Exclusive}, {Key: a, Mode: f.Exclusive}, {Key: e, Mode: f.Exclusive}}
}

type ProjectExecutionLeaseRetirementPlan interface {
	RequiredLocks() []f.LockRequest
	ProjectExecutionLeaseRetirementDigest() f.Digest
}
type ProjectExecutionLeaseRetirement interface {
	DiscoverProjectExecutionLeasesInTx(context.Context, f.Tx, ProjectExecutionLeaseRetirementRequest) (ProjectExecutionLeaseRetirementPlan, error)
	RetireProjectExecutionLeasesInTx(context.Context, f.Tx, ProjectExecutionLeaseRetirementRequest, ProjectExecutionLeaseRetirementPlan) error
	RequireProjectExecutionLeasesRetiredInTx(context.Context, f.Tx, ProjectExecutionLeaseRetirementRequest, ProjectExecutionLeaseRetirementPlan) error
}

// Each check consumes a distinct callback-local PV witness in the original
// caller's live transaction. The writing witness includes actual consumer return
// proof, authorizing the existing monotonic release before physical retirement.
type ProjectExecutionLeaseRetirementAuthority interface {
	CheckProjectExecutionLeaseDiscoveryInTx(context.Context, f.Tx, ProjectExecutionLeaseRetirementRequest) error
	CheckProjectExecutionLeaseRetirementInTx(context.Context, f.Tx, ProjectExecutionLeaseRetirementRequest) error
	CheckProjectExecutionLeasesRetiredInTx(context.Context, f.Tx, ProjectExecutionLeaseRetirementRequest) error
}

func (ProjectExecutionLeaseRetirementRequest) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("project_execution_lease_retirement"))
}
func (ProjectExecutionLeaseRetirementRequest) LogValue() slog.Value {
	return slog.StringValue("project_execution_lease_retirement")
}
func (ProjectExecutionLeaseRetirementRequest) MarshalJSON() ([]byte, error) {
	return []byte(`"project_execution_lease_retirement"`), nil
}
func (*ProjectExecutionLeaseRetirementRequest) UnmarshalJSON([]byte) error { return bad() }
