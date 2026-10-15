package skill

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func hasRuntimePreparation(ctx context.Context) bool {
	return ctx != nil && ctx.Value(preparationKey{}) != nil
}
func (a *Authority) preparationWitness(ctx context.Context, actor id.Actor, owner oc.ObjectOwner, intent id.AccessIntent) (*preparationWitness, error) {
	if ctx == nil {
		return nil, fault(f.Forbidden)
	}
	w, _ := ctx.Value(preparationKey{}).(*preparationWitness)
	if w == nil || w.authority == nil || w.authority.core != a || !w.active.Load() || !w.invocation.Invocation.Actor.Equal(actor) || owner.Validate() != nil || owner.Details().Kind != oc.SkillRevision || owner.Details().ID != w.binding.RevisionID.String() || owner.Details().ProjectID != w.invocation.Invocation.ProjectID.String() || !preparationOwnerIntent(w, intent) {
		return nil, fault(f.Forbidden)
	}
	return w, nil
}

// Terminal bookkeeping is separate from body access. These conditions only
// select the private owner branch; current or retirement authority and immutable
// owner facts are still checked in the caller's original transaction below.
func preparationOwnerIntent(w *preparationWitness, intent id.AccessIntent) bool {
	if w == nil {
		return false
	}
	if w.joining {
		return w.returned && w.materialReleased && (w.dispatch == nil || w.dispatch.Retirement()) && (intent == id.Read || intent == id.Converge)
	}
	if intent == id.Converge {
		return w.returned && w.materialReleased && w.terminal != nil && w.dispatch != nil && w.dispatch.Retirement()
	}
	return !w.returned && (intent == id.Read || intent == id.Mutate)
}

func (a *Authority) preparationObjectAuthorizationInTx(ctx context.Context, tx f.Tx, actor id.Actor, owner oc.ObjectOwner, intent id.AccessIntent) (oc.OwnerAuthorization, error) {
	w, err := a.preparationWitness(ctx, actor, owner, intent)
	if err != nil {
		return oc.OwnerAuthorization{}, err
	}
	var out oc.OwnerAuthorization
	if w.joining {
		if err = w.authority.cleanup(ctx, tx, w); err != nil {
			return out, err
		}
		x, e := a.state().store.InTx(tx)
		if e != nil {
			return out, portError(e)
		}
		var version int64
		var cause string
		// Serving/assignment removal cannot erase the retained original transfer's
		// owner. This exact revision/object join grants only bookkeeping, never a
		// package read or a new transfer. Both references remain independently live.
		e = x.QueryRow(ctx, `SELECT s.version,COALESCE(s.creation_id,s.installation_id)::text
 FROM agenteam_skill.skills s JOIN agenteam_skill.revisions r
 ON r.project_id=s.project_id AND r.skill_id=s.id AND r.id=s.revision_id
 WHERE s.project_id::text=$1::text AND s.id::text=$2::text AND r.id::text=$3::text
 AND r.object_id::text=$4::text AND r.revision=$5`, w.invocation.Invocation.ProjectID.String(), w.binding.SkillID.String(), w.binding.RevisionID.String(), w.metadata.Object.ID.String(), int64(w.metadata.Revision)).Scan(&version, &cause)
		if e != nil {
			return out, portError(e)
		}
		return oc.NewOwnerAuthorization(oc.OwnerAuthorizationDetails{Actor: actor, Owner: owner, Intent: intent, Existence: oc.ExistingOwner, CreationCause: cause, Version: f.Version(version)})
	}
	err = w.authority.current(ctx, tx, w, func(current context.Context) error {
		x, e := a.state().store.InTx(tx)
		if e != nil {
			return portError(e)
		}
		initial, e := loadInitialization(current, x, w.invocation.Invocation.ProjectID)
		if e != nil {
			return e
		}
		if initial == nil {
			return fault(f.Forbidden)
		}
		var existence oc.OwnerExistence
		var version f.Version
		var cause string
		if initial.revision == w.binding.RevisionID {
			existence, version, e = skillExistence(current, x, *initial)
			cause = initial.request.CreationID.String()
		} else {
			installed, err := ownerInstallation(current, x, owner)
			if err != nil {
				return err
			}
			existence, version, e = installationExistence(current, x, *installed)
			cause = installed.id.String()
		}
		if e != nil {
			return e
		}
		if existence != oc.ExistingOwner {
			return fault(f.Forbidden)
		}
		out, e = oc.NewOwnerAuthorization(oc.OwnerAuthorizationDetails{Actor: actor, Owner: owner, Intent: intent, Existence: existence, CreationCause: cause, Version: version})
		return e
	})
	return out, err
}
func (a *Authority) preparationObjectAuthorization(ctx context.Context, actor id.Actor, owner oc.ObjectOwner, intent id.AccessIntent) (oc.OwnerAuthorization, error) {
	w, err := a.preparationWitness(ctx, actor, owner, intent)
	if err != nil {
		return oc.OwnerAuthorization{}, err
	}
	var out oc.OwnerAuthorization
	err = w.authority.transaction(ctx, w, func(ctx context.Context, tx f.Tx, _ postgres.SQLExecutor) error {
		var e error
		out, e = a.preparationObjectAuthorizationInTx(ctx, tx, actor, owner, intent)
		return e
	})
	return out, err
}
func (a *Authority) preparationObjectDependencies(ctx context.Context, r oc.AccessRequest) (oc.AccessDependencies, error) {
	d := r.Details()
	w, err := a.preparationWitness(ctx, d.Actor, d.Owner, d.Intent)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	if w.joining || w.returned || !(d.Kind == oc.ObjectReadAccess && (d.Operation == oc.StatAccess || d.Operation == oc.ReadAccess) && d.ObjectID == w.metadata.Object.ID || d.Kind == oc.OwnerAccess && d.Operation == oc.PrepareReadAccess) {
		return oc.AccessDependencies{}, fault(f.Forbidden)
	}
	digest, _, err := captureDigest([]any{w.invocation.Invocation.OperationID, w.invocation.Invocation.AttemptID, string(w.invocation.RequestID), sum(w.arguments), w.binding.RevisionID.String(), w.binding.AssignmentID.String(), w.metadata.Object.ID.String()})
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	return oc.NewAccessDependencies(digest, w.locks)
}
func (a *Authority) validatePreparationObjectInTx(ctx context.Context, tx f.Tx, r oc.AccessRequest, plan oc.AccessDependencies) error {
	current, err := a.preparationObjectDependencies(ctx, r)
	if err != nil {
		return err
	}
	if !current.Equal(plan) {
		return fault(f.Forbidden)
	}
	d := r.Details()
	_, err = a.preparationObjectAuthorizationInTx(ctx, tx, d.Actor, d.Owner, d.Intent)
	return err
}
func (a *PreparationAuthority) preparationLease(ctx context.Context, r oc.AccessRequest) (*preparationWitness, error) {
	w, err := a.witness(ctx)
	if err != nil {
		return nil, err
	}
	if r.Validate() != nil {
		return nil, fault(f.Forbidden)
	}
	d := r.Details()
	if d.Kind != oc.LeaseAccess || !d.Actor.Equal(w.invocation.Invocation.Actor) || d.ObjectID != w.metadata.Object.ID || d.LeaseOwner.Details().Kind != oc.TransferOwner || w.transfer.Validate() != nil || d.LeaseOwner.Details().ID != w.transfer.String() {
		return nil, fault(f.Forbidden)
	}
	if d.Operation == oc.AcquireUseAccess {
		if w.joining || w.issued || w.materialReleased {
			return nil, fault(f.Forbidden)
		}
	} else if d.Operation == oc.ReleaseUseAccess {
		if !w.joining || !w.returned || !w.materialReleased || w.dispatch != nil && !w.dispatch.Retirement() {
			return nil, fault(f.Forbidden)
		}
	} else {
		return nil, fault(f.Forbidden)
	}
	return w, nil
}
func (a *PreparationAuthority) DiscoverPreparationLease(ctx context.Context, r oc.AccessRequest) (oc.AccessDependencies, error) {
	w, err := a.preparationLease(ctx, r)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	digest, _, err := captureDigest([]any{w.invocation.Invocation.OperationID, w.invocation.Invocation.AttemptID, string(w.invocation.RequestID), w.transfer.String(), w.metadata.Object.ID.String(), r.Details().Operation})
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	return oc.NewAccessDependencies(digest, w.locks)
}
func (a *PreparationAuthority) ValidatePreparationLeaseInTx(ctx context.Context, tx f.Tx, r oc.AccessRequest, plan oc.AccessDependencies) error {
	current, err := a.DiscoverPreparationLease(ctx, r)
	if err != nil {
		return err
	}
	if !current.Equal(plan) {
		return fault(f.Forbidden)
	}
	w, err := a.preparationLease(ctx, r)
	if err != nil {
		return err
	}
	if w.joining {
		return a.cleanup(ctx, tx, w)
	}
	return a.current(ctx, tx, w, func(current context.Context) error {
		generation, e := w.dispatch.RequireCurrentInTx(current, tx)
		if e != nil {
			return e
		}
		if generation != w.runnerGeneration {
			return fault(f.ConfirmationStale)
		}
		return nil
	})
}
func (a *PreparationAuthority) AuthorizeLeaseInTx(ctx context.Context, tx f.Tx, actor id.Actor, object oc.ObjectID, owner oc.LeaseOwner, action oc.LeaseAction) error {
	operation := oc.AcquireUseAccess
	if action == oc.ReleaseLease {
		operation = oc.ReleaseUseAccess
	} else if action != oc.AcquireLease {
		return fault(f.Forbidden)
	}
	r, err := oc.NewLeaseAccess(oc.AccessRequestDetails{Operation: operation, Actor: actor, ObjectID: object, LeaseOwner: owner})
	if err != nil {
		return err
	}
	plan, err := a.DiscoverPreparationLease(ctx, r)
	if err != nil {
		return err
	}
	return a.ValidatePreparationLeaseInTx(ctx, tx, r, plan)
}

var _ oc.LeaseAuthority = (*PreparationAuthority)(nil)
