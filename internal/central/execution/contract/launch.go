package contract

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// LaunchPlan is an observation for lock planning, not an authorization. A
// provider must recognize its own original plan and re-read its current facts
// in ValidateLaunchInTx. Missing providers must not produce an empty permit.
type LaunchPlan interface{ RequiredLocks() []f.LockRequest }
type LaunchPermit struct {
	ProviderType    string
	ProjectID       i.ProjectID
	AgentID         i.AgentID
	RequestDigest   f.Digest
	ReferenceDigest f.Digest
}

func (p LaunchPermit) Matches(request LaunchRequest) bool {
	digest, err := request.Digest()
	return err == nil && p.ProviderType == request.Trigger.Kind && p.ProjectID == request.ProjectID && p.AgentID == request.AgentID && p.RequestDigest == digest && p.ReferenceDigest.Validate() == nil
}

// TriggerProvider is registered explicitly as Task or Meeting. Validation must
// consume the real source's launch intent, Actor, current relationships, gates
// and the complete original policy; arbitrary resource constraints cannot be
// ignored. Both methods use only that provider's formal owner ports. The final
// method uses the original same-Store Tx and held union; it never adds locks.
// The returned permit is consumed immediately in that callback, not exported
// as a reusable Execution authority or persisted as a future launch grant.
type TriggerProvider interface {
	DiscoverLaunch(context.Context, i.Actor, LaunchRequest) (LaunchPlan, error)
	ValidateLaunchInTx(context.Context, f.Tx, i.Actor, LaunchRequest, LaunchPlan) (LaunchPermit, error)
}

// ServiceProjectAccess is the narrow missing non-Human Project gate. A real
// Scheduler/Trigger must supply its current, cause-bound authorization through
// the Project owner. A Service name alone cannot authorize anything; no Human
// Session is fabricated. Implementations use the original Tx, Project SH and
// Agent gate (plus the Task schedule lock for Launch), and no late locks.
// Human callers always use the existing current Project Owner authority.
type ServiceProjectAccess interface {
	RequireExecutionProjectInTx(context.Context, f.Tx, i.Actor, i.ProjectID, i.AgentID, i.AccessIntent) error
}

type LaunchLookupKey struct {
	ProjectID      i.ProjectID
	AgentID        i.AgentID
	IdempotencyKey f.IdempotencyKey
}

func (k LaunchLookupKey) Validate() error {
	if k.ProjectID.Validate() != nil || k.AgentID.Validate() != nil || k.IdempotencyKey.Validate() != nil {
		return invalid()
	}
	return nil
}
func (k LaunchLookupKey) Command() (f.CommandIdentity, error) {
	if err := k.Validate(); err != nil {
		return f.CommandIdentity{}, err
	}
	return f.NewCommandIdentity("execution", []string{k.ProjectID.String(), k.AgentID.String()}, "launch", k.IdempotencyKey)
}

type LaunchLookup struct {
	Found         bool
	RequestDigest f.Digest
	Execution     *Summary
}
