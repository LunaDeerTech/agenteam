package app

import (
	"context"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/artifact"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/knowledge"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
)

// The root fixes its real authorities at construction. Selection is never an
// authorization attempt: errors from the selected domain are returned as-is.
type objectAuthorities struct {
	avatar      *account.AvatarAuthority
	knowledge   *knowledge.Authority
	skills      *skill.Authority
	artifacts   *artifact.OwnerProvider
	mcp         objectOwnerAuthority
	maintenance *object.MaintenanceOwnerResolver
	preparation *skillPrepareTransferAuthority
}

type objectOwnerAuthority interface {
	oc.AccessPlanner
	oc.ResourceAuthority
	oc.ObjectReadAuthority
	oc.ProjectGate
}

type objectCleanupOwner interface {
	CheckCleanupInTx(context.Context, f.Tx, oc.ObjectCleanupCause, oc.ObjectID) error
}

func newObjectAuthorities(store object.Store, avatar *account.AvatarAuthority, documents *knowledge.Authority, skills *skill.Authority) (*objectAuthorities, error) {
	return newObjectAuthoritiesWithArtifacts(store, avatar, documents, skills, nil)
}

func newObjectAuthoritiesWithArtifacts(store object.Store, avatar *account.AvatarAuthority, documents *knowledge.Authority, skills *skill.Authority, artifacts *artifact.OwnerProvider, mcp ...objectOwnerAuthority) (*objectAuthorities, error) {
	return newObjectAuthoritiesWithPreparation(store, avatar, documents, skills, artifacts, nil, mcp...)
}

func newObjectAuthoritiesWithPreparation(store object.Store, avatar *account.AvatarAuthority, documents *knowledge.Authority, skills *skill.Authority, artifacts *artifact.OwnerProvider, preparation *skillPrepareTransferAuthority, mcp ...objectOwnerAuthority) (*objectAuthorities, error) {
	if avatar == nil || documents == nil || skills == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	resolver, err := object.NewMaintenanceOwnerResolver(store)
	if err != nil {
		return nil, err
	}
	if len(mcp) > 1 || len(mcp) == 1 && runtimeInformationNil(mcp[0]) {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	var remote objectOwnerAuthority
	if len(mcp) == 1 {
		remote = mcp[0]
	}
	return &objectAuthorities{mcp: remote, avatar: avatar, knowledge: documents, skills: skills, artifacts: artifacts, maintenance: resolver, preparation: preparation}, nil
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
	case oc.MCPContent:
		if a.mcp != nil {
			return a.mcp, nil
		}
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	case oc.Artifact:
		if a.artifacts != nil {
			return a.artifacts, nil
		}
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
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
	if request.Validate() == nil && request.Details().Kind == oc.LeaseAccess && request.Details().LeaseOwner.Details().Kind == oc.TransferOwner && a.preparation != nil {
		return a.preparation.DiscoverPreparationLease(ctx, request)
	}
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
	if request.Validate() == nil && request.Details().Kind == oc.LeaseAccess && request.Details().LeaseOwner.Details().Kind == oc.TransferOwner && a.preparation != nil {
		return a.preparation.ValidatePreparationLeaseInTx(ctx, tx, request, expected)
	}
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
	case oc.MCPContent:
		cleanup, ok := a.mcp.(objectCleanupOwner)
		if !ok || runtimeInformationNil(cleanup) {
			return f.NewFault(f.DependencyUnbound, f.NotStarted)
		}
		return cleanup.CheckCleanupInTx(ctx, tx, cause, object)
	case oc.Artifact:
		if a.artifacts != nil {
			return a.artifacts.CheckCleanupInTx(ctx, tx, cause, object)
		}
		return f.NewFault(f.DependencyUnbound, f.NotStarted)
	default:
		return f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
}

func (*objectAuthorities) CheckProjectCleanupInTx(context.Context, f.Tx, id.Actor, oc.ProjectCleanupCause) error {
	return f.NewFault(f.DependencyUnbound, f.NotStarted)
}

func (a *objectAuthorities) AuthorizeLeaseInTx(ctx context.Context, tx f.Tx, actor id.Actor, object oc.ObjectID, owner oc.LeaseOwner, action oc.LeaseAction) error {
	if owner.Validate() == nil && owner.Details().Kind == oc.TransferOwner && a.preparation != nil {
		return a.preparation.AuthorizeLeaseInTx(ctx, tx, actor, object, owner, action)
	}
	return f.NewFault(f.DependencyUnbound, f.NotStarted)
}

var _ oc.AccessPlanner = (*objectAuthorities)(nil)
var _ oc.ResourceAuthority = (*objectAuthorities)(nil)
var _ oc.ObjectReadAuthority = (*objectAuthorities)(nil)
var _ oc.ProjectGate = (*objectAuthorities)(nil)
var _ oc.CleanupAuthority = (*objectAuthorities)(nil)
var _ oc.LeaseAuthority = (*objectAuthorities)(nil)
