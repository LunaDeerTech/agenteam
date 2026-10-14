package project

import (
	"context"
	"encoding/json"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	gc "github.com/LunaDeerTech/agenteam/internal/central/governance/approval/contract"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"slices"
)

const governanceEventPurpose = "project.governance.approval.v1"

func governanceEventBinding(request oc.ProjectRequest) (f.Digest, []f.LockRequest, error) {
	if request.Validate() != nil {
		return "", nil, invalid()
	}
	d := request.Details()
	h := d.Event.Header
	a := d.Actor.Details()
	if d.Kind != oc.AppendProject || d.Event.Producer != gc.Producer || h.SchemaVersion != 1 || h.AggregateType != gc.Aggregate || (h.EventType != gc.RequestedName && h.EventType != gc.ResolvedName) {
		return "", nil, fault(f.DependencyUnbound)
	}
	if h.Scope.Kind != event.ProjectScope || h.Scope.ProjectID.String() != d.ProjectID.String() || h.AggregateSequence != nil || h.AggregateVersion == nil {
		return "", nil, fault(f.Forbidden)
	}
	locks := []f.LockRequest{projectLock(d.ProjectID, f.Shared)}
	if h.EventType == gc.RequestedName {
		if *h.AggregateVersion != 1 || a.Kind != i.AgentRun || a.ProjectID != d.ProjectID.String() {
			return "", nil, fault(f.Forbidden)
		}
	} else {
		if *h.AggregateVersion != 2 || a.Kind != i.Human {
			return "", nil, fault(f.Forbidden)
		}
		locks = append(locks, userLock(a.UserID, f.Shared))
	}
	raw, err := json.Marshal(struct {
		Purpose string
		Actor   i.ActorDetails
		Summary event.Summary
		Project i.ProjectID
	}{governanceEventPurpose, a, d.Event, d.ProjectID})
	if err != nil {
		return "", nil, invalid()
	}
	locks, err = oc.NormalizeLocks(locks)
	return digest(raw), locks, err
}
func (a *Authority) discoverGovernanceEvent(request oc.ProjectRequest) (oc.Dependencies, error) {
	if request.Details().Stage != oc.CurrentAccess {
		return oc.Dependencies{}, fault(f.Forbidden)
	}
	binding, locks, err := governanceEventBinding(request)
	if err != nil {
		return oc.Dependencies{}, err
	}
	return oc.NewDependencies(a.state().projectIssuer, binding, locks, []byte(governanceEventPurpose))
}
func (a *Authority) validateGovernanceEventInTx(ctx context.Context, tx f.Tx, request oc.ProjectRequest, deps oc.Dependencies) error {
	binding, locks, err := governanceEventBinding(request)
	if err != nil {
		return err
	}
	if !deps.Matches(a.state().projectIssuer, binding) || string(deps.Opaque()) != governanceEventPurpose || !slices.EqualFunc(deps.Locks(), locks, func(a, b f.LockRequest) bool { return a.Key.Canonical() == b.Key.Canonical() && a.Mode == b.Mode }) {
		return fault(f.Forbidden)
	}
	x, err := a.state().store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if err = a.state().store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	d := request.Details()
	if d.Actor.Details().Kind == i.Human {
		intent := i.Read
		if d.Stage == oc.NewFact {
			intent = i.Mutate
		}
		_, err = a.RequireOwnerInTx(ctx, tx, d.Actor, d.ProjectID, intent)
		return err
	}
	p, err := loadProject(ctx, x, d.ProjectID)
	if err != nil {
		return err
	}
	if p == nil || !p.initialized || p.ref.Lifecycle != pc.Active {
		return fault(f.ProjectNotActive)
	}
	// Governance's independently registered producer proves the private original
	// current Execution witness and exact request postimage in this same Tx.
	return ctx.Err()
}
