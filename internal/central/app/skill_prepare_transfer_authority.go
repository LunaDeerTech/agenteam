package app

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
)

type skillPrepareTransferSource interface {
	oc.RunnerTransferAuthority
	oc.TransferObservationAuthority
	oc.SkillTransferRetirementAuthority
	oc.LeaseAuthority
	DiscoverPreparationLease(context.Context, oc.AccessRequest) (oc.AccessDependencies, error)
	ValidatePreparationLeaseInTx(context.Context, f.Tx, oc.AccessRequest, oc.AccessDependencies) error
}

type skillPrepareMaintenanceSource interface {
	oc.RunnerTransferAuthority
	oc.LeaseAuthority
	DiscoverMaintenanceLease(context.Context, oc.AccessRequest) (oc.AccessDependencies, error)
	ValidateMaintenanceLeaseInTx(context.Context, f.Tx, oc.AccessRequest, oc.AccessDependencies) error
}

// Both providers are fixed before Object exists. Selection never grants access:
// each provider must validate its own original private call in the live Tx.
type skillPrepareTransferAuthority struct {
	preparation skillPrepareTransferSource
	maintenance skillPrepareMaintenanceSource
}

func newSkillPrepareTransferAuthority(store object.Store, preparation *skill.PreparationAuthority) (*skillPrepareTransferAuthority, error) {
	if preparation == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	maintenance, err := object.NewTransferMaintenanceAuthority(store)
	if err != nil {
		return nil, err
	}
	return &skillPrepareTransferAuthority{preparation: preparation, maintenance: maintenance}, nil
}

func skillPrepareMaintenanceActor(actor id.Actor) bool {
	return actor.Validate() == nil && actor.Details().Kind == id.Service && actor.Details().ServiceName == id.ObjectMaintenance
}

func (a *skillPrepareTransferAuthority) transferSource(r oc.TransferAccessRequest) (oc.RunnerTransferAuthority, error) {
	if a == nil || runtimeInformationNil(a.preparation) || runtimeInformationNil(a.maintenance) {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	if r.Validate() != nil {
		return nil, f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	d := r.Details()
	if skillPrepareMaintenanceActor(d.Actor) {
		if d.Spec.Details().Direction != oc.TransferGET || d.Spec.Details().Owner.Details().Kind != oc.SkillRevision ||
			(d.Operation != oc.TransferCleanup && d.Operation != oc.TransferConfirmTerminal) {
			return nil, f.NewFault(f.Forbidden, f.NotStarted)
		}
		return a.maintenance, nil
	}
	return a.preparation, nil
}

func (a *skillPrepareTransferAuthority) Discover(ctx context.Context, r oc.TransferAccessRequest) (oc.AccessDependencies, error) {
	provider, err := a.transferSource(r)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	return provider.Discover(ctx, r)
}

func (a *skillPrepareTransferAuthority) ValidateInTx(ctx context.Context, tx f.Tx, r oc.TransferAccessRequest, plan oc.AccessDependencies) (oc.TransferAuthorization, error) {
	provider, err := a.transferSource(r)
	if err != nil {
		return oc.TransferAuthorization{}, err
	}
	return provider.ValidateInTx(ctx, tx, r, plan)
}

func (a *skillPrepareTransferAuthority) maintenanceLease(r oc.AccessRequest) (bool, error) {
	if a == nil || runtimeInformationNil(a.preparation) || runtimeInformationNil(a.maintenance) {
		return false, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	if r.Validate() != nil {
		return false, f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	d := r.Details()
	maintenance := skillPrepareMaintenanceActor(d.Actor)
	if maintenance && (d.Kind != oc.LeaseAccess || d.LeaseOwner.Details().Kind != oc.TransferOwner || d.Operation != oc.ReleaseUseAccess) {
		return false, f.NewFault(f.Forbidden, f.NotStarted)
	}
	return maintenance, nil
}

func (a *skillPrepareTransferAuthority) DiscoverPreparationLease(ctx context.Context, r oc.AccessRequest) (oc.AccessDependencies, error) {
	maintenance, err := a.maintenanceLease(r)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	if maintenance {
		return a.maintenance.DiscoverMaintenanceLease(ctx, r)
	}
	return a.preparation.DiscoverPreparationLease(ctx, r)
}

func (a *skillPrepareTransferAuthority) ValidatePreparationLeaseInTx(ctx context.Context, tx f.Tx, r oc.AccessRequest, plan oc.AccessDependencies) error {
	maintenance, err := a.maintenanceLease(r)
	if err != nil {
		return err
	}
	if maintenance {
		return a.maintenance.ValidateMaintenanceLeaseInTx(ctx, tx, r, plan)
	}
	return a.preparation.ValidatePreparationLeaseInTx(ctx, tx, r, plan)
}

func (a *skillPrepareTransferAuthority) AuthorizeLeaseInTx(ctx context.Context, tx f.Tx, actor id.Actor, object oc.ObjectID, owner oc.LeaseOwner, action oc.LeaseAction) error {
	if a == nil || runtimeInformationNil(a.preparation) || runtimeInformationNil(a.maintenance) {
		return f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	if skillPrepareMaintenanceActor(actor) {
		if action != oc.ReleaseLease || owner.Validate() != nil || owner.Details().Kind != oc.TransferOwner {
			return f.NewFault(f.Forbidden, f.NotStarted)
		}
		return a.maintenance.AuthorizeLeaseInTx(ctx, tx, actor, object, owner, action)
	}
	return a.preparation.AuthorizeLeaseInTx(ctx, tx, actor, object, owner, action)
}

func (a *skillPrepareTransferAuthority) DiscoverTransferObservation(ctx context.Context, r oc.TransferObservationRequest) (oc.AccessDependencies, error) {
	return a.preparation.DiscoverTransferObservation(ctx, r)
}

func (a *skillPrepareTransferAuthority) ValidateTransferObservationInTx(ctx context.Context, tx f.Tx, r oc.TransferObservationRequest, plan oc.AccessDependencies) (oc.TransferObservationFacts, error) {
	return a.preparation.ValidateTransferObservationInTx(ctx, tx, r, plan)
}

func (a *skillPrepareTransferAuthority) DiscoverSkillTransferRetirement(ctx context.Context, r oc.SkillTransferRetirementRequest) (oc.AccessDependencies, error) {
	return a.preparation.DiscoverSkillTransferRetirement(ctx, r)
}

func (a *skillPrepareTransferAuthority) ValidateSkillTransferRetirementInTx(ctx context.Context, tx f.Tx, r oc.SkillTransferRetirementRequest, plan oc.AccessDependencies) error {
	return a.preparation.ValidateSkillTransferRetirementInTx(ctx, tx, r, plan)
}

var _ oc.RunnerTransferAuthority = (*skillPrepareTransferAuthority)(nil)
var _ oc.TransferObservationAuthority = (*skillPrepareTransferAuthority)(nil)
var _ oc.SkillTransferRetirementAuthority = (*skillPrepareTransferAuthority)(nil)
