package app

import (
	"context"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/knowledge"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
)

// The root binds exactly these three real authorities. Selection is never an
// authorization attempt: errors from the selected domain are returned as-is.
type objectAuthorities struct {
	avatar      *account.AvatarAuthority
	knowledge   *knowledge.Authority
	skills      *skill.Authority
	maintenance *object.MaintenanceOwnerResolver
}

type objectOwnerAuthority interface {
	oc.AccessPlanner
	oc.ResourceAuthority
	oc.ObjectReadAuthority
	oc.ProjectGate
}

func newObjectAuthorities(store object.Store, avatar *account.AvatarAuthority, documents *knowledge.Authority, skills *skill.Authority) (*objectAuthorities, error) {
	if avatar == nil || documents == nil || skills == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	resolver, err := object.NewMaintenanceOwnerResolver(store)
	if err != nil {
		return nil, err
	}
	return &objectAuthorities{avatar: avatar, knowledge: documents, skills: skills, maintenance: resolver}, nil
}

func (a *objectAuthorities) owner(owner oc.ObjectOwner) (objectOwnerAuthority, error) {
	if owner.Validate() != nil {
		return nil, f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	switch owner.Details().Kind {
	case oc.Avatar:
		return a.avatar, nil
	case oc.Knowledge:
		return a.knowledge, nil
	case oc.SkillRevision:
		return a.skills, nil
	default:
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
}

func (a *objectAuthorities) requestOwner(request oc.AccessRequest) (oc.ObjectOwner, error) {
	if request.Validate() != nil {
		return oc.ObjectOwner{}, f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	d := request.Details()
	switch d.Kind {
	case oc.OwnerAccess, oc.ObjectReadAccess:
		return d.Owner, nil
	case oc.SourceAccess:
		return d.Source.Details().Owner, nil
	case oc.ObjectCleanupAccess, oc.CleanupReleaseAccess:
		return d.Cleanup.Details().Owner, nil
	default:
		// Stable leases, whole-Project cleanup and Transfer have no bound
		// owning authority. Maintenance must resolve Object's own mapping.
		return oc.ObjectOwner{}, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
}

func (a *objectAuthorities) Discover(ctx context.Context, request oc.AccessRequest) (oc.AccessDependencies, error) {
	var owner oc.ObjectOwner
	var err error
	if request.Validate() == nil && request.Details().Kind == oc.MaintenanceAccess {
		owner, err = a.maintenance.Discover(ctx, request)
	} else {
		owner, err = a.requestOwner(request)
	}
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	provider, err := a.owner(owner)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	return provider.Discover(ctx, request)
}

func (a *objectAuthorities) ValidateInTx(ctx context.Context, tx f.Tx, request oc.AccessRequest, expected oc.AccessDependencies) error {
	var owner oc.ObjectOwner
	var err error
	if request.Validate() == nil && request.Details().Kind == oc.MaintenanceAccess {
		// Re-read in the original live Store/Tx after Object's DomainBinding
		// check. The discovery result is neither cached nor used as a grant.
		owner, err = a.maintenance.ResolveInTx(ctx, tx, request)
	} else {
		owner, err = a.requestOwner(request)
	}
	if err != nil {
		return err
	}
	provider, err := a.owner(owner)
	if err != nil {
		return err
	}
	return provider.ValidateInTx(ctx, tx, request, expected)
}

func (a *objectAuthorities) AuthorizeOwner(ctx context.Context, actor id.Actor, owner oc.ObjectOwner, intent id.AccessIntent) (oc.OwnerAuthorization, error) {
	provider, err := a.owner(owner)
	if err != nil {
		return oc.OwnerAuthorization{}, err
	}
	return provider.AuthorizeOwner(ctx, actor, owner, intent)
}

func (a *objectAuthorities) AuthorizeOwnerInTx(ctx context.Context, tx f.Tx, actor id.Actor, owner oc.ObjectOwner, intent id.AccessIntent) (oc.OwnerAuthorization, error) {
	provider, err := a.owner(owner)
	if err != nil {
		return oc.OwnerAuthorization{}, err
	}
	return provider.AuthorizeOwnerInTx(ctx, tx, actor, owner, intent)
}

func (a *objectAuthorities) AuthorizeObjectReadInTx(ctx context.Context, tx f.Tx, actor id.Actor, owner oc.ObjectOwner, object oc.ObjectID) (oc.OwnerAuthorization, error) {
	provider, err := a.owner(owner)
	if err != nil {
		return oc.OwnerAuthorization{}, err
	}
	return provider.AuthorizeObjectReadInTx(ctx, tx, actor, owner, object)
}

func (a *objectAuthorities) CheckInTx(ctx context.Context, tx f.Tx, actor id.Actor, owner oc.ObjectOwner, intent id.AccessIntent) error {
	provider, err := a.owner(owner)
	if err != nil {
		return err
	}
	return provider.CheckInTx(ctx, tx, actor, owner, intent)
}

func (a *objectAuthorities) CheckCleanupInTx(ctx context.Context, tx f.Tx, cause oc.ObjectCleanupCause, object oc.ObjectID) error {
	if cause.Validate() != nil {
		return f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	switch cause.Details().Owner.Details().Kind {
	case oc.Avatar:
		return a.avatar.CheckCleanupInTx(ctx, tx, cause, object)
	case oc.Knowledge:
		return a.knowledge.CheckCleanupInTx(ctx, tx, cause, object)
	default:
		return f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
}

func (*objectAuthorities) CheckProjectCleanupInTx(context.Context, f.Tx, id.Actor, oc.ProjectCleanupCause) error {
	return f.NewFault(f.DependencyUnbound, f.NotStarted)
}

func (*objectAuthorities) AuthorizeLeaseInTx(context.Context, f.Tx, id.Actor, oc.ObjectID, oc.LeaseOwner, oc.LeaseAction) error {
	return f.NewFault(f.DependencyUnbound, f.NotStarted)
}

var _ oc.AccessPlanner = (*objectAuthorities)(nil)
var _ oc.ResourceAuthority = (*objectAuthorities)(nil)
var _ oc.ObjectReadAuthority = (*objectAuthorities)(nil)
var _ oc.ProjectGate = (*objectAuthorities)(nil)
var _ oc.CleanupAuthority = (*objectAuthorities)(nil)
var _ oc.LeaseAuthority = (*objectAuthorities)(nil)
