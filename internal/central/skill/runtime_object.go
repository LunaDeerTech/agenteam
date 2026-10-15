package skill

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func hasRuntimeRead(ctx context.Context) bool {
	return ctx != nil && ctx.Value(runtimeReadKey{}) != nil
}
func (a *Authority) runtimeObjectWitness(ctx context.Context, actor id.Actor, owner oc.ObjectOwner, intent id.AccessIntent) (*runtimeReadWitness, error) {
	w, _ := ctx.Value(runtimeReadKey{}).(*runtimeReadWitness)
	if w == nil || !w.active.Load() || w.authority != a || intent != id.Read || !w.invocation.Actor.Equal(actor) || owner.Validate() != nil || owner.Details().Kind != oc.SkillRevision || owner.Details().ProjectID != w.invocation.ProjectID.String() || owner.Details().ID != w.binding.RevisionID.String() {
		return nil, fault(f.Forbidden)
	}
	return w, nil
}
func (a *Authority) runtimeObjectAuthorizationInTx(ctx context.Context, tx f.Tx, actor id.Actor, owner oc.ObjectOwner, intent id.AccessIntent) (oc.OwnerAuthorization, error) {
	w, err := a.runtimeObjectWitness(ctx, actor, owner, intent)
	if err != nil {
		return oc.OwnerAuthorization{}, err
	}
	var out oc.OwnerAuthorization
	err = a.withRuntimeReadInTx(ctx, tx, w, func(current context.Context) error {
		if err := a.requireRuntimeAssignmentInTx(current, tx, w); err != nil {
			return err
		}
		x, err := a.state().store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		initial, err := loadInitialization(current, x, w.invocation.ProjectID)
		if err != nil {
			return err
		}
		if initial == nil {
			return fault(f.Forbidden)
		}
		var existence oc.OwnerExistence
		var version f.Version
		var cause string
		if initial.revision == w.binding.RevisionID {
			existence, version, err = skillExistence(current, x, *initial)
			cause = initial.request.CreationID.String()
		} else {
			installed, e := ownerInstallation(current, x, owner)
			if e != nil {
				return e
			}
			existence, version, err = installationExistence(current, x, *installed)
			cause = installed.id.String()
		}
		if err != nil {
			return err
		}
		if existence != oc.ExistingOwner {
			return fault(f.Forbidden)
		}
		out, err = oc.NewOwnerAuthorization(oc.OwnerAuthorizationDetails{Actor: actor, Owner: owner, Intent: intent, Existence: existence, CreationCause: cause, Version: version})
		return err
	})
	return out, err
}
func (a *Authority) runtimeObjectAuthorization(ctx context.Context, actor id.Actor, owner oc.ObjectOwner, intent id.AccessIntent) (oc.OwnerAuthorization, error) {
	w, err := a.runtimeObjectWitness(ctx, actor, owner, intent)
	if err != nil {
		return oc.OwnerAuthorization{}, err
	}
	var out oc.OwnerAuthorization
	err = a.runtimeReadTx(ctx, w, func(ctx context.Context, tx f.Tx, _ postgres.SQLExecutor) error {
		var e error
		out, e = a.runtimeObjectAuthorizationInTx(ctx, tx, actor, owner, intent)
		return e
	})
	return out, err
}
func (a *Authority) runtimeObjectDependencies(ctx context.Context, r oc.AccessRequest) (oc.AccessDependencies, error) {
	d := r.Details()
	w, err := a.runtimeObjectWitness(ctx, d.Actor, d.Owner, d.Intent)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	if !(d.Kind == oc.OwnerAccess && d.Operation == oc.PrepareReadAccess || d.Kind == oc.ObjectReadAccess && (d.Operation == oc.StatAccess || d.Operation == oc.ReadAccess) && d.ObjectID == w.metadata.Object.ID) {
		return oc.AccessDependencies{}, fault(f.Forbidden)
	}
	digest, _, err := captureDigest(struct {
		Operation, Attempt           string
		Arguments                    f.Digest
		Revision, Assignment, Object string
	}{w.invocation.OperationID, w.invocation.AttemptID, sum(w.arguments), w.binding.RevisionID.String(), w.binding.AssignmentID.String(), w.metadata.Object.ID.String()})
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	return oc.NewAccessDependencies(digest, w.locks)
}
func (a *Authority) validateRuntimeObjectInTx(ctx context.Context, tx f.Tx, r oc.AccessRequest, expected oc.AccessDependencies) error {
	current, err := a.runtimeObjectDependencies(ctx, r)
	if err != nil {
		return err
	}
	if !current.Equal(expected) {
		return fault(f.Forbidden)
	}
	d := r.Details()
	_, err = a.runtimeObjectAuthorizationInTx(ctx, tx, d.Actor, d.Owner, d.Intent)
	return err
}
