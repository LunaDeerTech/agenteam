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

// Only the old mapper's explicit no-matching-owner result selects the other
// canonical source. Corruption, a failed SQL call, or a current gate error must
// never be converted into a fallback authorization.
func notInitializationOwner(err error) bool {
	var known *f.Fault
	return errors.As(err, &known) && known.Code == f.Forbidden
}

func ownerInstallation(ctx context.Context, x postgres.SQLExecutor, owner oc.ObjectOwner) (*installationRow, error) {
	project, err := skillOwner(owner)
	if err != nil {
		return nil, err
	}
	revision, err := f.ParseID[sc.Revision](owner.Details().ID)
	if err != nil {
		return nil, invalid()
	}
	row, err := loadInstallationRevision(ctx, x, project, revision)
	if err != nil {
		return nil, err
	}
	if row == nil || row.project != project || row.revision != revision {
		return nil, fault(f.Forbidden)
	}
	return row, nil
}

func installationOwnerLocks(row installationRow, actor id.Actor, intent id.AccessIntent) ([]f.LockRequest, error) {
	user, err := installationActor(actor)
	if err != nil {
		return nil, err
	}
	if row.validate() != nil {
		return nil, unavailable(nil)
	}
	if intent == id.Read && row.phase == installationPublished {
		return oc.NormalizeAccessLocks([]f.LockRequest{userLock(user.String(), f.Shared), projectLock(row.project, f.Shared), skillLock(row.skill, f.Shared), objectLock(row.object, f.Shared)})
	}
	if user != row.user || intent != id.Read && intent != id.Mutate {
		return nil, fault(f.Forbidden)
	}
	return row.locks(f.Exclusive)
}

func (a *Authority) checkInstallationOwner(ctx context.Context, tx f.Tx, actor id.Actor, row installationRow, intent id.AccessIntent) error {
	state := a.state()
	if state == nil {
		return fault(f.DependencyUnbound)
	}
	locks, err := installationOwnerLocks(row, actor, intent)
	if err != nil {
		return err
	}
	if err = state.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	if _, err = state.store.InTx(tx); err != nil {
		return portError(err)
	}
	if err = a.installationOwnerInTx(ctx, tx, actor, row.project, id.Read); err != nil {
		return err
	}
	if intent == id.Mutate {
		if row.phase != installationPlanned && row.phase != installationReserved {
			return fault(f.InvalidState)
		}
		return a.installationOwnerInTx(ctx, tx, actor, row.project, id.Mutate)
	}
	return nil
}

func installationExistence(ctx context.Context, x postgres.SQLExecutor, row installationRow) (oc.OwnerExistence, f.Version, error) {
	var installation, revision string
	var protected, serving bool
	var version int64
	err := x.QueryRow(ctx, `SELECT installation_id::text,revision_id::text,protected,serving,version FROM agenteam_skill.skills WHERE project_id=$1 AND id=$2`, row.project.String(), row.skill.String()).Scan(&installation, &revision, &protected, &serving, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		if row.phase == installationPublished {
			return "", 0, unavailable(nil)
		}
		return oc.ProspectiveOwner, 1, nil
	}
	if err != nil {
		return "", 0, unavailable(err)
	}
	if installation != row.id.String() || revision != row.revision.String() || protected || f.Version(version).Validate() != nil {
		return "", 0, unavailable(nil)
	}
	if !serving {
		return "", 0, fault(f.ResourceDeleted)
	}
	return oc.ExistingOwner, f.Version(version), nil
}

func (a *Authority) authorizeInstallationInTx(ctx context.Context, tx f.Tx, actor id.Actor, owner oc.ObjectOwner, intent id.AccessIntent) (oc.OwnerAuthorization, error) {
	x, err := a.state().store.InTx(tx)
	if err != nil {
		return oc.OwnerAuthorization{}, portError(err)
	}
	row, err := ownerInstallation(ctx, x, owner)
	if err != nil {
		return oc.OwnerAuthorization{}, err
	}
	if err = a.checkInstallationOwner(ctx, tx, actor, *row, intent); err != nil {
		return oc.OwnerAuthorization{}, err
	}
	existence, version, err := installationExistence(ctx, x, *row)
	if err != nil {
		return oc.OwnerAuthorization{}, err
	}
	return oc.NewOwnerAuthorization(oc.OwnerAuthorizationDetails{Actor: actor, Owner: owner, Intent: intent, Existence: existence, CreationCause: row.id.String(), Version: version})
}

func (a *Authority) authorizeInstallation(ctx context.Context, actor id.Actor, owner oc.ObjectOwner, intent id.AccessIntent) (oc.OwnerAuthorization, error) {
	state := a.state()
	row, err := ownerInstallation(ctx, state.store, owner)
	if err != nil {
		return oc.OwnerAuthorization{}, err
	}
	locks, err := installationOwnerLocks(*row, actor, intent)
	if err != nil {
		return oc.OwnerAuthorization{}, err
	}
	command, err := row.identity()
	if err != nil {
		return oc.OwnerAuthorization{}, err
	}
	cause, err := f.NewCommandsCause(command)
	if err != nil {
		return oc.OwnerAuthorization{}, err
	}
	var grant oc.OwnerAuthorization
	var callbackErr error
	result := state.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) (err error) {
		defer func() { callbackErr = err }()
		if err = state.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		grant, err = a.authorizeInstallationInTx(ctx, tx, actor, owner, intent)
		return err
	})
	if result.State() == f.NotCommitted && callbackErr != nil {
		return oc.OwnerAuthorization{}, callbackErr
	}
	if err = commitError(result); err != nil {
		return oc.OwnerAuthorization{}, err
	}
	return grant, nil
}

func installationAccess(row installationRow, request oc.AccessRequest) error {
	if row.validate() != nil || request.Validate() != nil {
		return invalid()
	}
	d := request.Details()
	owner, err := row.owner()
	if err != nil || !d.Owner.Equal(owner) {
		return fault(f.Forbidden)
	}
	user, err := installationActor(d.Actor)
	if err != nil {
		return err
	}
	if d.Kind == oc.ObjectReadAccess {
		if d.Intent != id.Read || row.phase != installationPublished || d.ObjectID != row.object || d.Operation != oc.StatAccess && d.Operation != oc.ReadAccess {
			return fault(f.Forbidden)
		}
		return nil
	}
	if d.Kind != oc.OwnerAccess || user != row.user {
		return fault(f.Forbidden)
	}
	preparedMatches := func() bool {
		p := d.Prepared.Details()
		return d.Prepared.Validate() == nil && p.MediaType == sc.PackageMediaType && p.Length == int64(row.pkg.size) && p.SHA256 == row.pkg.packageDigest
	}
	switch d.Operation {
	case oc.PrepareAccess:
		if row.phase != installationPlanned || d.Intent != id.Mutate {
			return fault(f.InvalidState)
		}
	case oc.ReserveAccess:
		if row.phase != installationPlanned || d.Intent != id.Mutate {
			return fault(f.InvalidState)
		}
		if d.Command.IdempotencyKey != row.key || d.Command.ExpectedVersion != nil || !preparedMatches() {
			return fault(f.IdempotencyKeyReused)
		}
	case oc.SendAccess, oc.PublishAccess:
		attempt := d.Attempt.Details()
		if d.Intent != id.Mutate || row.phase != installationReserved || attempt.ID != row.attempt || attempt.ObjectID != row.object || attempt.UploadID != row.upload {
			return fault(f.ResourceBusy)
		}
		if d.Operation == oc.SendAccess && !preparedMatches() {
			return fault(f.IdempotencyKeyReused)
		}
	case oc.LookupAccess:
		if d.Intent != id.Read || d.Key != row.key {
			return fault(f.IdempotencyKeyReused)
		}
	default:
		return fault(f.Forbidden)
	}
	return nil
}

func installationDependencies(row installationRow, actor id.Actor, intent id.AccessIntent) (oc.AccessDependencies, error) {
	locks, err := installationOwnerLocks(row, actor, intent)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	material, err := json.Marshal(struct {
		Project, Installation, User, Key, Skill, Revision, Object, Upload, Attempt string
		Semantic                                                                   f.Digest
		Phase                                                                      installationPhase
		Version                                                                    f.Version
	}{row.project.String(), row.id.String(), row.user.String(), row.key.String(), row.skill.String(), row.revision.String(), row.object.String(), row.upload.String(), row.attempt.String(), row.semantic, row.phase, row.version})
	if err != nil {
		return oc.AccessDependencies{}, unavailable(err)
	}
	return oc.NewAccessDependencies(sum(material), locks)
}

func (a *Authority) discoverInstallation(ctx context.Context, request oc.AccessRequest) (oc.AccessDependencies, error) {
	row, err := ownerInstallation(ctx, a.state().store, request.Details().Owner)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	if err = installationAccess(*row, request); err != nil {
		return oc.AccessDependencies{}, err
	}
	return installationDependencies(*row, request.Details().Actor, request.Details().Intent)
}

func (a *Authority) validateInstallation(ctx context.Context, tx f.Tx, request oc.AccessRequest, expected oc.AccessDependencies) error {
	x, err := a.state().store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	d := request.Details()
	row, err := ownerInstallation(ctx, x, d.Owner)
	if err != nil {
		return err
	}
	if err = a.checkInstallationOwner(ctx, tx, d.Actor, *row, d.Intent); err != nil {
		return err
	}
	if err = installationAccess(*row, request); err != nil {
		return err
	}
	current, err := installationDependencies(*row, d.Actor, d.Intent)
	if err != nil {
		return err
	}
	if !current.Equal(expected) {
		return fault(f.ResourceBusy)
	}
	_, _, err = installationExistence(ctx, x, *row)
	return err
}

type installationMaintenance struct {
	row     installationRow
	attempt oc.AttemptID
	process oc.ProcessID
}

func loadInstallationMaintenance(ctx context.Context, x postgres.SQLExecutor, request oc.AccessRequest) (installationMaintenance, error) {
	var out installationMaintenance
	if request.Validate() != nil || request.Details().Kind != oc.MaintenanceAccess {
		return out, invalid()
	}
	d := request.Details()
	switch d.Operation {
	case oc.InspectAccess, oc.FinishWriterAccess, oc.JoinAttemptAccess, oc.ReleaseReaderAccess, oc.ReleaseProcessAccess:
	default:
		return out, fault(f.DependencyUnbound)
	}
	row, err := scanInstallation(x.QueryRow(ctx, `SELECT `+installationColumns+` FROM agenteam_skill.installations WHERE object_id=$1`, d.ObjectID.String()))
	if err != nil {
		return out, err
	}
	if row == nil {
		return out, fault(f.DependencyUnbound)
	}
	if row.object != d.ObjectID {
		return out, unavailable(nil)
	}
	out.row = *row
	if d.Operation == oc.ReleaseReaderAccess && row.phase != installationPublished {
		return out, fault(f.Forbidden)
	}
	if d.AttemptID != (oc.AttemptID{}) {
		var attempt, process string
		err = x.QueryRow(ctx, `SELECT attempt_id::text,process_id::text FROM agenteam_skill.installation_attempts WHERE attempt_id=$1 AND project_id=$2 AND installation_id=$3 AND skill_id=$4 AND revision_id=$5 AND object_id=$6 AND upload_id=$7`, d.AttemptID.String(), row.project.String(), row.id.String(), row.skill.String(), row.revision.String(), row.object.String(), row.upload.String()).Scan(&attempt, &process)
		if errors.Is(err, pgx.ErrNoRows) {
			return out, fault(f.Forbidden)
		}
		if err != nil {
			return out, unavailable(err)
		}
		if out.attempt, err = f.ParseID[oc.Attempt](attempt); err != nil {
			return out, unavailable(err)
		}
		if out.process, err = f.ParseID[oc.Process](process); err != nil {
			return out, unavailable(err)
		}
		if out.attempt != d.AttemptID || d.Operation == oc.FinishWriterAccess && d.InstanceID != out.process || d.Operation == oc.JoinAttemptAccess && d.ProcessID != out.process {
			return out, fault(f.Forbidden)
		}
	}
	if d.Operation == oc.ReleaseProcessAccess {
		var exact bool
		err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_skill.work WHERE project_id=$1 AND skill_id=$2 AND process_id=$3 AND kind IN ('installation','installed_package_reader'))`, row.project.String(), row.skill.String(), d.ProcessID.String()).Scan(&exact)
		if err != nil {
			return out, unavailable(err)
		}
		if !exact {
			return out, fault(f.Forbidden)
		}
		out.process = d.ProcessID
	}
	return out, nil
}

func (m installationMaintenance) dependencies() (oc.AccessDependencies, error) {
	locks, err := m.row.locks(f.Exclusive)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	material, err := json.Marshal(struct {
		Project, Installation, User, Key, Skill, Revision, Object, Upload, Attempt, Process string
		Semantic                                                                            f.Digest
	}{m.row.project.String(), m.row.id.String(), m.row.user.String(), m.row.key.String(), m.row.skill.String(), m.row.revision.String(), m.row.object.String(), m.row.upload.String(), m.attempt.String(), m.process.String(), m.row.semantic})
	if err != nil {
		return oc.AccessDependencies{}, unavailable(err)
	}
	return oc.NewAccessDependencies(sum(material), locks)
}

func unboundInitializationMapping(err error) bool {
	var known *f.Fault
	return errors.As(err, &known) && known.Code == f.DependencyUnbound
}

func (a *Authority) discoverInstallationMaintenance(ctx context.Context, request oc.AccessRequest) (oc.AccessDependencies, error) {
	m, err := loadInstallationMaintenance(ctx, a.state().store, request)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	return m.dependencies()
}

func (a *Authority) validateInstallationMaintenance(ctx context.Context, tx f.Tx, request oc.AccessRequest, expected oc.AccessDependencies) error {
	x, err := a.state().store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	m, err := loadInstallationMaintenance(ctx, x, request)
	if err != nil {
		return err
	}
	current, err := m.dependencies()
	if err != nil {
		return err
	}
	if !current.Equal(expected) {
		return fault(f.ResourceBusy)
	}
	// The outer planner has required the complete expected lock union. D05's
	// original instance/writer/reader proof remains the authority for actual
	// retirement. An active-only gate cannot veto an already-owned I/O tail.
	return nil
}
