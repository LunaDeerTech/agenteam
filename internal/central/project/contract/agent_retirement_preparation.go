package contract

import (
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// AgentRetirementPreparationRequest identifies a read in one original Delete
// stop round. Its fields are not an issuer, a stopped report or retirement rights.
type AgentRetirementPreparationRequest struct {
	Actor                 i.Actor
	ProjectID             i.ProjectID
	Cause                 LifecycleCause
	AgentID               i.AgentID
	ExpectedConfigVersion f.Version
}

func (r AgentRetirementPreparationRequest) Validate() error {
	if r.Actor.Validate() != nil || r.ProjectID.Validate() != nil || r.Cause.Validate() != nil || r.AgentID.Validate() != nil || r.ExpectedConfigVersion.Validate() != nil {
		return invalid("", "INVALID_AGENT_RETIREMENT_PREPARATION")
	}
	d := r.Actor.Details()
	if r.Cause.Action != Delete || d.Kind != i.Service || d.ServiceName != i.ProjectLifecycle || d.ProjectID != r.ProjectID.String() || d.CauseRef != r.Cause.OperationID.String() {
		return fault(f.Forbidden)
	}
	return nil
}

// RequiredLocks are already held by the caller. The preparation owner verifies
// them without acquiring locks or taking ownership of the caller's transaction.
func (r AgentRetirementPreparationRequest) RequiredLocks() ([]f.LockRequest, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	project, _ := f.ProjectLock(r.ProjectID.String())
	schedule, _ := f.ProjectScheduleLock(r.ProjectID.String())
	agent, _ := f.AgentLock(r.AgentID.String())
	return []f.LockRequest{{Key: project, Mode: f.Shared}, {Key: schedule, Mode: f.Exclusive}, {Key: agent, Mode: f.Exclusive}}, nil
}
