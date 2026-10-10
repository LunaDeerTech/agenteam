package skill

import (
	"context"
	"encoding/json"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

var _ oc.CleanupAuthority = (*Authority)(nil)

func cleanupActor(project id.ProjectID, cause pc.LifecycleCause) (id.Actor, error) {
	if project.Validate() != nil || cause.Validate() != nil || cause.Action != pc.Delete {
		return id.Actor{}, invalid()
	}
	scope, err := id.InProject(project)
	if err != nil {
		return id.Actor{}, err
	}
	registration, err := id.RegisterService(id.ProjectLifecycle)
	if err != nil {
		return id.Actor{}, err
	}
	return registration.Actor(cause.OperationID.String(), scope)
}

func cleanupObjectProject(cause oc.ObjectCleanupCause, object oc.ObjectID) (id.ProjectID, error) {
	if cause.Validate() != nil || object.Validate() != nil {
		return id.ProjectID{}, invalid()
	}
	d := cause.Details()
	if d.Owner.Details().Kind != oc.SkillRevision {
		return id.ProjectID{}, fault(f.DependencyUnbound)
	}
	if d.Reason != oc.ProjectDeleted {
		return id.ProjectID{}, fault(f.Forbidden)
	}
	return skillOwner(d.Owner)
}

// No current Project permission is cached in the durable gate. Reconstructing
// its Service actor is only an input to the real same-Tx Project authority.
func (a *Authority) checkCleanupRowInTx(ctx context.Context, tx f.Tx, r initializationRow, c cleanupRow) error {
	state := a.state()
	if state == nil {
		return fault(f.DependencyUnbound)
	}
	if !c.matches(r) {
		return fault(f.Forbidden)
	}
	locks, err := r.locks(f.Exclusive, r.object)
	if err != nil {
		return err
	}
	if err = state.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	x, err := state.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	actor, err := cleanupActor(c.project, c.cause)
	if err != nil {
		return err
	}
	if err = state.projects.ValidateLifecycleInTx(ctx, tx, actor, c.cause, pc.SkillsParticipant, pc.CleanupPhase); err != nil {
		return portError(err)
	}
	if err = cleanupCore(ctx, x, r, true); err != nil {
		return err
	}
	joined, err := cleanupWorkJoined(ctx, x, c.project)
	if err != nil {
		return err
	}
	if !joined {
		return fault(f.ResourceBusy)
	}
	return nil
}

func (a *Authority) CheckCleanupInTx(ctx context.Context, tx f.Tx, cause oc.ObjectCleanupCause, object oc.ObjectID) error {
	state := a.state()
	if state == nil {
		return fault(f.DependencyUnbound)
	}
	project, err := cleanupObjectProject(cause, object)
	if err != nil {
		return err
	}
	// Check the known parent locks before reading its private mapping, then
	// require the original command/Skill union before authorizing any action.
	if err = state.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(project, f.Exclusive), objectLock(object, f.Exclusive)}); err != nil {
		return portError(err)
	}
	x, err := state.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if _, err := ownerInitialization(ctx, x, cause.Details().Owner); err != nil {
		if notInitializationOwner(err) {
			installed, gate, e := loadInstallationCleanupObject(ctx, x, object)
			if e != nil {
				return e
			}
			if gate == nil || installed.project != project {
				return fault(f.Forbidden)
			}
			exact, e := installationCleanupCause(*gate, *installed)
			if e != nil || exact.Details().OperationID != cause.Details().OperationID || exact.Details().Reason != cause.Details().Reason || !exact.Details().Owner.Equal(cause.Details().Owner) {
				return fault(f.Forbidden)
			}
			return a.checkInstallationCleanupInTx(ctx, tx, *installed, *gate)
		}
		return err
	}
	r, c, err := loadCleanupObject(ctx, x, cause, object, false)
	if err != nil {
		return err
	}
	return a.checkCleanupRowInTx(ctx, tx, *r, *c)
}

// A revision's retained parent is not authority for every Object in a Project.
// The immutable root router must bind the actual Project/Object provider here.
func (a *Authority) CheckProjectCleanupInTx(context.Context, f.Tx, id.Actor, oc.ProjectCleanupCause) error {
	return fault(f.DependencyUnbound)
}

func loadCleanupObject(ctx context.Context, x postgres.SQLExecutor, cause oc.ObjectCleanupCause, object oc.ObjectID, proposed bool) (*initializationRow, *cleanupRow, error) {
	project, err := cleanupObjectProject(cause, object)
	if err != nil {
		return nil, nil, err
	}
	r, err := ownerInitialization(ctx, x, cause.Details().Owner)
	if err != nil {
		return nil, nil, err
	}
	if r.phase != initializationPublished || r.object != object || r.request.ProjectID != project {
		return nil, nil, fault(f.Forbidden)
	}
	c, err := loadCleanup(ctx, x, project)
	if err != nil {
		return nil, nil, err
	}
	if c == nil {
		if proposed {
			return r, nil, nil
		}
		return nil, nil, fault(f.Forbidden)
	}
	if !c.matches(*r) || c.id != cause.Details().OperationID {
		return nil, nil, fault(f.Forbidden)
	}
	return r, c, nil
}

func cleanupAccessPhase(d oc.AccessRequestDetails, c *cleanupRow, discovering bool) error {
	switch d.Kind {
	case oc.CleanupReleaseAccess:
		if d.Operation != oc.ReleaseForCleanupAccess {
			return fault(f.Forbidden)
		}
		if c == nil && discovering {
			return nil
		}
		if c == nil || c.phase != cleanupGated {
			return fault(f.InvalidState)
		}
	case oc.ObjectCleanupAccess:
		if c == nil {
			return fault(f.Forbidden)
		}
		switch d.Operation {
		case oc.CleanupObjectAccess:
			if c.phase != cleanupGated && c.phase != cleanupPending {
				return fault(f.InvalidState)
			}
		case oc.PurgeDeletedObjectMetadataAccess:
			if c.phase != cleanupCompleted {
				return fault(f.InvalidState)
			}
		default:
			return fault(f.DependencyUnbound)
		}
	default:
		return fault(f.DependencyUnbound)
	}
	return nil
}

// The digest binds immutable parent and request material, not a phase flag.
// First Release discovery precedes inserting the gate; Validate sees the real
// newly inserted/closed facts in that same Tx and separately checks authority.
func cleanupDependencies(r initializationRow, request oc.AccessRequest) (oc.AccessDependencies, error) {
	locks, err := r.locks(f.Exclusive, r.object)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	d := request.Details()
	c := d.Cleanup.Details()
	raw, err := json.Marshal(struct {
		Project, Creation, Key, Skill, Revision, Object, Upload, Attempt       string
		Semantic                                                               f.Digest
		Kind                                                                   oc.AccessKind
		Operation                                                              oc.AccessOperation
		Cleanup, RequestUpload, Instance, RequestedAttempt, Worker, Checkpoint string
		Fence                                                                  int64
		Reason                                                                 oc.CleanupReason
	}{r.request.ProjectID.String(), r.request.CreationID.String(), string(r.request.InitializationKey), r.skill.String(), r.revision.String(), r.object.String(), r.upload.String(), r.attempt.String(), r.semantic, d.Kind, d.Operation, c.OperationID.String(), d.UploadID.String(), d.InstanceID.String(), d.AttemptID.String(), d.WorkerID.String(), d.CleanupID.String(), int64(d.Fence), c.Reason})
	if err != nil {
		return oc.AccessDependencies{}, unavailable(err)
	}
	return oc.NewAccessDependencies(sum(raw), locks)
}

func (a *Authority) discoverCleanup(ctx context.Context, request oc.AccessRequest) (oc.AccessDependencies, error) {
	d := request.Details()
	r, c, err := loadCleanupObject(ctx, a.state().store, d.Cleanup, d.ObjectID, d.Kind == oc.CleanupReleaseAccess)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	if d.Kind == oc.CleanupReleaseAccess && d.UploadID != r.upload {
		return oc.AccessDependencies{}, fault(f.Forbidden)
	}
	if err = cleanupAccessPhase(d, c, true); err != nil {
		return oc.AccessDependencies{}, err
	}
	return cleanupDependencies(*r, request)
}

func (a *Authority) validateCleanup(ctx context.Context, tx f.Tx, request oc.AccessRequest, expected oc.AccessDependencies) error {
	x, err := a.state().store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	d := request.Details()
	r, c, err := loadCleanupObject(ctx, x, d.Cleanup, d.ObjectID, false)
	if err != nil {
		return err
	}
	if d.Kind == oc.CleanupReleaseAccess && d.UploadID != r.upload {
		return fault(f.Forbidden)
	}
	if err = cleanupAccessPhase(d, c, false); err != nil {
		return err
	}
	current, err := cleanupDependencies(*r, request)
	if err != nil {
		return err
	}
	if !current.Equal(expected) {
		return fault(f.ResourceBusy)
	}
	return a.checkCleanupRowInTx(ctx, tx, *r, *c)
}
