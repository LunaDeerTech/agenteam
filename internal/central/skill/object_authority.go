package skill

import (
	"context"
	"encoding/json"
	"errors"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/jackc/pgx/v5"
)

var (
	_ oc.ResourceAuthority   = (*Authority)(nil)
	_ oc.AccessPlanner       = (*Authority)(nil)
	_ oc.ObjectReadAuthority = (*Authority)(nil)
	_ oc.ProjectGate         = (*Authority)(nil)
)

func skillOwner(owner oc.ObjectOwner) (id.ProjectID, error) {
	if owner.Validate() != nil {
		return id.ProjectID{}, invalid()
	}
	d := owner.Details()
	if d.Kind != oc.SkillRevision {
		return id.ProjectID{}, fault(f.DependencyUnbound)
	}
	project, e := f.ParseID[id.Project](d.ProjectID)
	if e != nil {
		return project, invalid()
	}
	return project, nil
}
func ownerInitialization(ctx context.Context, x postgres.SQLExecutor, owner oc.ObjectOwner) (*initializationRow, error) {
	project, e := skillOwner(owner)
	if e != nil {
		return nil, e
	}
	row, e := loadInitialization(ctx, x, project)
	if e != nil {
		return nil, e
	}
	if row == nil || row.request.ProjectID != project || row.revision.String() != owner.Details().ID {
		return nil, fault(f.Forbidden)
	}
	return row, nil
}
func ownerLocks(row initializationRow, actor id.Actor, intent id.AccessIntent) ([]f.LockRequest, error) {
	if actor.Validate() != nil {
		return nil, invalid()
	}
	switch actor.Details().Kind {
	case id.Service:
		if e := initializationActor(actor, row.request); e != nil {
			return nil, e
		}
		if intent != id.Read && intent != id.Mutate && intent != id.Converge {
			return nil, fault(f.Forbidden)
		}
		return row.locks(f.Exclusive, row.object)
	case id.Human:
		if intent != id.Read {
			return nil, fault(f.Forbidden)
		}
		locks := []f.LockRequest{userLock(actor.Details().UserID, f.Shared), projectLock(row.request.ProjectID, f.Shared), skillLock(row.skill, f.Shared)}
		if row.object != (oc.ObjectID{}) {
			locks = append(locks, objectLock(row.object, f.Shared))
		}
		return oc.NormalizeAccessLocks(locks)
	case id.AgentRun:
		return nil, fault(f.DependencyUnbound)
	default:
		return nil, fault(f.Forbidden)
	}
}

func (a *Authority) checkOwnerRow(ctx context.Context, tx f.Tx, actor id.Actor, row initializationRow, intent id.AccessIntent) error {
	state := a.state()
	if state == nil {
		return fault(f.DependencyUnbound)
	}
	locks, e := ownerLocks(row, actor, intent)
	if e != nil {
		return e
	}
	if e = state.store.RequireHeldLocks(ctx, tx, locks); e != nil {
		return portError(e)
	}
	if _, e = state.store.InTx(tx); e != nil {
		return portError(e)
	}
	if actor.Details().Kind == id.Human {
		grant, e := state.projects.RequireOwnerInTx(ctx, tx, actor, row.request.ProjectID, id.Read)
		if e != nil {
			return portError(e)
		}
		if !grant.Matches(actor, row.request.ProjectID) {
			return unavailable(nil)
		}
		if row.phase != initializationPublished {
			return fault(f.InvalidState)
		}
		return nil
	}
	if intent == id.Converge {
		return portError(state.projects.ValidateInitializationConvergenceInTx(ctx, tx, actor, row.request))
	}
	return portError(state.projects.ValidateInitializationInTx(ctx, tx, actor, row.request.CreationID, row.request.ProjectID, row.request.InitializationKey))
}

// Existence can change from prospective to existing within publication's Tx.
// It is not part of a cached dependency plan; every grant checks the same-Tx row.
func skillExistence(ctx context.Context, x postgres.SQLExecutor, row initializationRow) (oc.OwnerExistence, f.Version, error) {
	var creation, revision string
	var protected, serving bool
	var version int64
	e := x.QueryRow(ctx, `SELECT creation_id::text,revision_id::text,protected,serving,version FROM agenteam_skill.skills WHERE project_id=$1 AND id=$2`, row.request.ProjectID.String(), row.skill.String()).Scan(&creation, &revision, &protected, &serving, &version)
	if errors.Is(e, pgx.ErrNoRows) {
		if row.phase == initializationPublished {
			return "", 0, unavailable(nil)
		}
		return oc.ProspectiveOwner, 1, nil
	}
	if e != nil {
		return "", 0, unavailable(e)
	}
	if creation != row.request.CreationID.String() || revision != row.revision.String() || !protected || f.Version(version).Validate() != nil {
		return "", 0, unavailable(nil)
	}
	if !serving {
		return "", 0, fault(f.ResourceDeleted)
	}
	return oc.ExistingOwner, f.Version(version), nil
}
func (a *Authority) AuthorizeOwnerInTx(ctx context.Context, tx f.Tx, actor id.Actor, owner oc.ObjectOwner, intent id.AccessIntent) (oc.OwnerAuthorization, error) {
	state := a.state()
	if state == nil {
		return oc.OwnerAuthorization{}, fault(f.DependencyUnbound)
	}
	x, e := state.store.InTx(tx)
	if e != nil {
		return oc.OwnerAuthorization{}, portError(e)
	}
	row, e := ownerInitialization(ctx, x, owner)
	if e != nil {
		if notInitializationOwner(e) {
			return a.authorizeInstallationInTx(ctx, tx, actor, owner, intent)
		}
		return oc.OwnerAuthorization{}, e
	}
	if e = a.checkOwnerRow(ctx, tx, actor, *row, intent); e != nil {
		return oc.OwnerAuthorization{}, e
	}
	existence, version, e := skillExistence(ctx, x, *row)
	if e != nil {
		return oc.OwnerAuthorization{}, e
	}
	if actor.Details().Kind == id.Human && existence != oc.ExistingOwner {
		return oc.OwnerAuthorization{}, fault(f.InvalidState)
	}
	if intent == id.Mutate && row.phase != initializationPlanned && row.phase != initializationReserved {
		return oc.OwnerAuthorization{}, fault(f.InvalidState)
	}
	return oc.NewOwnerAuthorization(oc.OwnerAuthorizationDetails{Actor: actor, Owner: owner, Intent: intent, Existence: existence, CreationCause: row.request.CreationID.String(), Version: version})
}
func (a *Authority) AuthorizeOwner(ctx context.Context, actor id.Actor, owner oc.ObjectOwner, intent id.AccessIntent) (oc.OwnerAuthorization, error) {
	state := a.state()
	if state == nil {
		return oc.OwnerAuthorization{}, fault(f.DependencyUnbound)
	}
	row, e := ownerInitialization(ctx, state.store, owner)
	if e != nil {
		if notInitializationOwner(e) {
			return a.authorizeInstallation(ctx, actor, owner, intent)
		}
		return oc.OwnerAuthorization{}, e
	}
	locks, e := ownerLocks(*row, actor, intent)
	if e != nil {
		return oc.OwnerAuthorization{}, e
	}
	command, e := initializationIdentity(row.request)
	if e != nil {
		return oc.OwnerAuthorization{}, e
	}
	cause, e := f.NewCommandsCause(command)
	if e != nil {
		return oc.OwnerAuthorization{}, e
	}
	var out oc.OwnerAuthorization
	var callbackErr error
	result := state.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) (err error) {
		defer func() { callbackErr = err }()
		if e := state.store.AcquireAll(ctx, tx, locks); e != nil {
			return portError(e)
		}
		out, err = a.AuthorizeOwnerInTx(ctx, tx, actor, owner, intent)
		return err
	})
	if result.State() == f.NotCommitted && callbackErr != nil {
		return oc.OwnerAuthorization{}, callbackErr
	}
	if e = commitError(result); e != nil {
		return oc.OwnerAuthorization{}, e
	}
	return out, nil
}
func (a *Authority) CheckInTx(ctx context.Context, tx f.Tx, actor id.Actor, owner oc.ObjectOwner, intent id.AccessIntent) error {
	_, e := a.AuthorizeOwnerInTx(ctx, tx, actor, owner, intent)
	return e
}
func (a *Authority) AuthorizeObjectReadInTx(ctx context.Context, tx f.Tx, actor id.Actor, owner oc.ObjectOwner, object oc.ObjectID) (oc.OwnerAuthorization, error) {
	if actor.Validate() != nil || object.Validate() != nil {
		return oc.OwnerAuthorization{}, invalid()
	}
	if actor.Details().Kind != id.Human {
		return oc.OwnerAuthorization{}, fault(f.Forbidden)
	}
	grant, e := a.AuthorizeOwnerInTx(ctx, tx, actor, owner, id.Read)
	if e != nil {
		return oc.OwnerAuthorization{}, e
	}
	x, e := a.state().store.InTx(tx)
	if e != nil {
		return oc.OwnerAuthorization{}, portError(e)
	}
	row, e := ownerInitialization(ctx, x, owner)
	if e != nil {
		if notInitializationOwner(e) {
			installed, err := ownerInstallation(ctx, x, owner)
			if err != nil {
				return oc.OwnerAuthorization{}, err
			}
			if installed.phase != installationPublished || installed.object != object {
				return oc.OwnerAuthorization{}, fault(f.Forbidden)
			}
			details := grant.Details()
			details.ReadObjectID = object
			return oc.NewOwnerAuthorization(details)
		}
		return oc.OwnerAuthorization{}, e
	}
	if row.object != object {
		return oc.OwnerAuthorization{}, fault(f.Forbidden)
	}
	if _, _, e = loadPublished(ctx, x, *row); e != nil {
		return oc.OwnerAuthorization{}, e
	}
	d := grant.Details()
	d.ReadObjectID = object
	return oc.NewOwnerAuthorization(d)
}

func initializationAccess(row initializationRow, r oc.AccessRequest) error {
	d := r.Details()
	if d.Actor.Details().Kind == id.AgentRun {
		return fault(f.DependencyUnbound)
	}
	if d.Actor.Details().Kind == id.Human {
		if d.Intent != id.Read {
			return fault(f.Forbidden)
		}
		if d.Kind == oc.OwnerAccess && d.Operation == oc.PrepareReadAccess {
			return nil
		}
		if d.Kind == oc.ObjectReadAccess && (d.Operation == oc.StatAccess || d.Operation == oc.ReadAccess) && d.ObjectID == row.object {
			return nil
		}
		return fault(f.Forbidden)
	}
	if e := initializationActor(d.Actor, row.request); e != nil {
		return e
	}
	if d.Kind != oc.OwnerAccess {
		return fault(f.Forbidden)
	}
	switch d.Operation {
	case oc.PrepareAccess:
		if row.phase != initializationPlanned && row.phase != initializationReserved {
			return fault(f.InvalidState)
		}
	case oc.ReserveAccess:
		if d.Command.IdempotencyKey != row.request.InitializationKey || d.Command.ExpectedVersion != nil {
			return fault(f.IdempotencyKeyReused)
		}
		if row.phase != initializationPlanned && row.phase != initializationReserved {
			return fault(f.InvalidState)
		}
		p := d.Prepared.Details()
		if p.MediaType != sc.PackageMediaType || p.Length != int64(row.bundle.size) || p.SHA256 != row.bundle.packageDigest {
			return fault(f.IdempotencyKeyReused)
		}
	case oc.SendAccess, oc.PublishAccess:
		attempt := d.Attempt.Details()
		if row.phase != initializationReserved || attempt.ID != row.attempt || attempt.ObjectID != row.object || attempt.UploadID != row.upload {
			return fault(f.ResourceBusy)
		}
		if d.Operation == oc.SendAccess {
			p := d.Prepared.Details()
			if p.MediaType != sc.PackageMediaType || p.Length != int64(row.bundle.size) || p.SHA256 != row.bundle.packageDigest {
				return fault(f.IdempotencyKeyReused)
			}
		}
	case oc.LookupAccess, oc.CancelAccess:
		if d.Key != row.request.InitializationKey {
			return fault(f.IdempotencyKeyReused)
		}
	default:
		return fault(f.Forbidden)
	}
	return nil
}
func mappingDependencies(row initializationRow, actor id.Actor, intent id.AccessIntent) (oc.AccessDependencies, error) {
	locks, e := ownerLocks(row, actor, intent)
	if e != nil {
		return oc.AccessDependencies{}, e
	}
	material, e := json.Marshal(struct {
		Project, Creation, Key, Skill, Revision, Object, Upload, Attempt string
		Semantic                                                         f.Digest
	}{row.request.ProjectID.String(), row.request.CreationID.String(), string(row.request.InitializationKey), row.skill.String(), row.revision.String(), row.object.String(), row.upload.String(), row.attempt.String(), row.semantic})
	if e != nil {
		return oc.AccessDependencies{}, unavailable(e)
	}
	return oc.NewAccessDependencies(sum(material), locks)
}
func (a *Authority) Discover(ctx context.Context, request oc.AccessRequest) (oc.AccessDependencies, error) {
	state := a.state()
	if state == nil {
		return oc.AccessDependencies{}, fault(f.DependencyUnbound)
	}
	if request.Validate() != nil {
		return oc.AccessDependencies{}, invalid()
	}
	d := request.Details()
	if d.Kind == oc.MaintenanceAccess {
		return a.discoverMaintenance(ctx, request)
	}
	if d.Kind == oc.CleanupReleaseAccess || d.Kind == oc.ObjectCleanupAccess {
		if _, err := ownerInitialization(ctx, state.store, d.Cleanup.Details().Owner); err != nil {
			if notInitializationOwner(err) {
				return a.discoverInstallationCleanup(ctx, request)
			}
			return oc.AccessDependencies{}, err
		}
		return a.discoverCleanup(ctx, request)
	}
	if d.Kind != oc.OwnerAccess && d.Kind != oc.ObjectReadAccess {
		return oc.AccessDependencies{}, fault(f.DependencyUnbound)
	}
	row, e := ownerInitialization(ctx, state.store, d.Owner)
	if e != nil {
		if notInitializationOwner(e) {
			return a.discoverInstallation(ctx, request)
		}
		return oc.AccessDependencies{}, e
	}
	if e = initializationAccess(*row, request); e != nil {
		return oc.AccessDependencies{}, e
	}
	return mappingDependencies(*row, d.Actor, d.Intent)
}
func (a *Authority) ValidateInTx(ctx context.Context, tx f.Tx, request oc.AccessRequest, expected oc.AccessDependencies) error {
	state := a.state()
	if state == nil {
		return fault(f.DependencyUnbound)
	}
	if request.Validate() != nil || expected.Validate() != nil {
		return invalid()
	}
	if e := state.store.RequireHeldLocks(ctx, tx, expected.Locks()); e != nil {
		return portError(e)
	}
	x, e := state.store.InTx(tx)
	if e != nil {
		return portError(e)
	}
	d := request.Details()
	if d.Kind == oc.MaintenanceAccess {
		return a.validateMaintenance(ctx, tx, request, expected)
	}
	if d.Kind == oc.CleanupReleaseAccess || d.Kind == oc.ObjectCleanupAccess {
		if _, err := ownerInitialization(ctx, x, d.Cleanup.Details().Owner); err != nil {
			if notInitializationOwner(err) {
				return a.validateInstallationCleanup(ctx, tx, request, expected)
			}
			return err
		}
		return a.validateCleanup(ctx, tx, request, expected)
	}
	if d.Kind != oc.OwnerAccess && d.Kind != oc.ObjectReadAccess {
		return fault(f.DependencyUnbound)
	}
	row, e := ownerInitialization(ctx, x, d.Owner)
	if e != nil {
		if notInitializationOwner(e) {
			return a.validateInstallation(ctx, tx, request, expected)
		}
		return e
	}
	if e = a.checkOwnerRow(ctx, tx, d.Actor, *row, d.Intent); e != nil {
		return e
	}
	if e = initializationAccess(*row, request); e != nil {
		return e
	}
	current, e := mappingDependencies(*row, d.Actor, d.Intent)
	if e != nil {
		return e
	}
	if !current.Equal(expected) {
		return fault(f.ResourceBusy)
	}
	_, _, e = skillExistence(ctx, x, *row)
	return e
}
