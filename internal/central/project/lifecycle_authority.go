package project

import (
	"context"
	"slices"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// LifecycleAuthority authenticates persisted causes. Version declarations are
// immutable metadata, not participant bindings or evidence of completed work.
type LifecycleAuthority struct {
	store        Store
	declarations map[lifecycleParticipantKey]c.ParticipantRegistration
	issuer       oc.PlanIssuer
}

func NewLifecycleAuthority(store Store, current c.RequiredManifest, compatible []c.ParticipantRegistration) (*LifecycleAuthority, error) {
	if nilPort(store) {
		return nil, fault(foundation.DependencyUnbound)
	}
	if err := current.Require(); err != nil {
		return nil, err
	}
	a := &LifecycleAuthority{store: store, declarations: make(map[lifecycleParticipantKey]c.ParticipantRegistration), issuer: oc.NewPlanIssuer()}
	for _, entry := range append(current.Entries(), compatible...) {
		entry, err := normalizeLifecycleRegistration(entry)
		if err != nil {
			return nil, err
		}
		key := lifecycleParticipantKey{entry.Name, entry.ContractVersion}
		if _, exists := a.declarations[key]; exists {
			return nil, invalid()
		}
		a.declarations[key] = entry
	}
	return a, nil
}

func (a *LifecycleAuthority) bound() bool {
	return a != nil && !nilPort(a.store) && a.declarations != nil
}

func lifecycleActorProject(actor identity.Actor, cause c.LifecycleCause) (c.ProjectID, error) {
	if actor.Validate() != nil || cause.Validate() != nil {
		return c.ProjectID{}, invalid()
	}
	d := actor.Details()
	if d.Kind != identity.Service || d.ServiceName != identity.ProjectLifecycle || d.CauseRef != cause.OperationID.String() {
		return c.ProjectID{}, fault(foundation.Forbidden)
	}
	project, err := foundation.ParseID[identity.Project](d.ProjectID)
	if err != nil {
		return c.ProjectID{}, fault(foundation.Forbidden)
	}
	return project, nil
}

func (a *LifecycleAuthority) lifecycleExecutor(ctx context.Context, tx foundation.Tx, project c.ProjectID) (postgres.SQLExecutor, error) {
	if !a.bound() {
		return nil, fault(foundation.DependencyUnbound)
	}
	x, err := a.store.InTx(tx)
	if err != nil {
		return nil, unavailable(err)
	}
	if err = a.store.RequireHeldLocks(ctx, tx, []foundation.LockRequest{projectLock(project, foundation.Shared)}); err != nil {
		return nil, unavailable(err)
	}
	return x, nil
}

type lifecycleFact struct {
	state      c.OperationState
	stopFailed bool
}

func (f lifecycleFact) authorize(inspect bool) error {
	if f.state == c.OperationStopping && !f.stopFailed {
		return nil
	}
	if inspect && (f.state == c.OperationCleaning || f.state == c.OperationFailed || f.state == c.OperationCompleted) {
		return nil
	}
	return fault(foundation.InvalidState)
}

// The caller holds Project SH in this same Store transaction. A deletion
// receipt proves only identity; its consumer must match its own action/version.
func (a *LifecycleAuthority) lifecycleFact(ctx context.Context, x postgres.SQLExecutor, project c.ProjectID, cause c.LifecycleCause, participant c.ParticipantName) (lifecycleFact, error) {
	var fact lifecycleFact
	p, err := loadProject(ctx, x, project)
	if err != nil {
		return fact, err
	}
	receipt, err := loadDeletionReceipt(ctx, x, project)
	if err != nil {
		return fact, err
	}
	if p != nil && receipt != nil {
		return fact, unavailable(nil)
	}
	if p == nil {
		if receipt == nil || cause.Action != c.Delete || receipt.Details().OperationID != cause.OperationID {
			return fact, fault(foundation.Forbidden)
		}
		return lifecycleFact{state: c.OperationCompleted}, nil
	}
	if p.operation == nil || *p.operation != cause.OperationID {
		return fact, fault(foundation.Forbidden)
	}
	if !p.initialized {
		return fact, fault(foundation.InvalidState)
	}
	r, err := loadLifecycleOperation(ctx, x, project, cause.OperationID)
	if err != nil {
		return fact, err
	}
	if r == nil || r.owner != p.ref.OwnerUserID.String() {
		return fact, unavailable(nil)
	}
	o := r.operation
	if o.Action != cause.Action || o.ProjectVersion != cause.ProjectVersion {
		return fact, fault(foundation.Forbidden)
	}
	selected := participant == ""
	for _, entry := range r.manifest.Entries() {
		registered, exists := a.declarations[lifecycleParticipantKey{entry.Name, entry.ContractVersion}]
		if !exists {
			return fact, fault(foundation.DependencyUnbound)
		}
		if registered.OwnerModule != entry.OwnerModule || !slices.Equal(registered.ReferenceKinds, entry.ReferenceKinds) || !slices.Equal(registered.CleanupAfter, entry.CleanupAfter) {
			return fact, unavailable(nil)
		}
		if entry.Name == participant {
			selected = true
		}
	}
	if !selected {
		return fact, fault(foundation.DependencyUnbound)
	}
	if o.State == c.OperationCompleted {
		if o.Action != c.Archive || p.ref.Lifecycle != c.Archived || o.CompletedProjectVersion == nil || p.ref.Version != *o.CompletedProjectVersion {
			return fact, unavailable(nil)
		}
	} else {
		gate := c.Archiving
		if o.Action == c.Delete {
			gate = c.Deleting
		}
		if p.ref.Lifecycle != gate || p.ref.Version != o.ProjectVersion {
			return fact, unavailable(nil)
		}
	}
	fact.state = o.State
	for _, row := range r.participants {
		if row.name == participant {
			fact.stopFailed = row.stop == "failed"
		}
	}
	return fact, nil
}

func (a *LifecycleAuthority) ValidateLifecycleInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, cause c.LifecycleCause, participant c.ParticipantName, phase c.OperationPhase) error {
	if !a.bound() {
		return fault(foundation.DependencyUnbound)
	}
	if participant.Validate() != nil || phase.Validate() != nil {
		return invalid()
	}
	project, err := lifecycleActorProject(actor, cause)
	if err != nil {
		return err
	}
	if phase == c.CleanupPhase {
		return fault(foundation.DependencyUnbound)
	}
	x, err := a.lifecycleExecutor(ctx, tx, project)
	if err != nil {
		return err
	}
	fact, err := a.lifecycleFact(ctx, x, project, cause, participant)
	if err != nil {
		return err
	}
	return fact.authorize(false)
}

var _ LifecycleFacts = (*LifecycleAuthority)(nil)
