package meeting

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
)

type DeletionProjectGate interface {
	RequireMeetingDeletionProjectInTx(context.Context, f.Tx, i.Actor, i.ProjectID) error
}

type deletionRuntimeKey struct{}
type deletionRuntimeContext struct {
	runtime  *Runtime
	original deletionRecord
	active   bool
}

func originalDeletionRuntime(ctx context.Context, p i.ProjectID, m c.MeetingID) *deletionRuntimeContext {
	if ctx == nil {
		return nil
	}
	w, _ := ctx.Value(deletionRuntimeKey{}).(*deletionRuntimeContext)
	if w == nil || !w.active || w.runtime == nil || w.original.project != p || w.original.meeting != m || w.original.phase != c.DeletionCancelling {
		return nil
	}
	return w
}

func (r *Runtime) runtimeLocks(ctx context.Context, p i.ProjectID, m c.MeetingID, locks []f.LockRequest) ([]f.LockRequest, error) {
	w := originalDeletionRuntime(ctx, p, m)
	if w == nil {
		return normalize(locks)
	}
	if w.runtime != r || r.deps.DeletionAuthority == nil || r.deps.DeletionAuthority.original != r.deps.Authority {
		return nil, fault(f.Forbidden)
	}
	command, err := commandIdentity(p, w.original.key)
	if err != nil {
		return nil, err
	}
	return normalize(append(locks, projectLock(p, f.Exclusive), meetingLock(m, f.Exclusive), commandLock(command)))
}

type deletionProjectKey struct{}
type deletionProjectWitness struct {
	owner  *deletionRuntimeContext
	tx     f.Tx
	actor  i.Actor
	active bool
}

func (r *Runtime) requireRuntimeProjectInTx(ctx context.Context, tx f.Tx, p i.ProjectID, m c.MeetingID) error {
	owner := originalDeletionRuntime(ctx, p, m)
	if owner == nil {
		_, err := r.deps.Projects.RequirePreparingProjectInTx(ctx, tx, p)
		return portError(err)
	}
	if owner.runtime != r || nilPort(r.deps.DeletionProjects) {
		return fault(f.DependencyUnbound)
	}
	scope, err := i.InProject(p)
	if err != nil {
		return portError(err)
	}
	actor, err := r.deps.Actor.Actor(owner.original.id.String(), scope)
	if err != nil {
		return portError(err)
	}
	w := &deletionProjectWitness{owner: owner, tx: tx, actor: actor, active: true}
	defer func() { w.active = false }()
	return portError(r.deps.DeletionProjects.RequireMeetingDeletionProjectInTx(context.WithValue(ctx, deletionProjectKey{}, w), tx, actor, p))
}

func (a *DeletionAuthority) RequireMeetingDeletionBindingInTx(ctx context.Context, tx f.Tx, store any) error {
	if a == nil || ctx == nil || !tx.Valid() {
		return fault(f.Forbidden)
	}
	w, _ := ctx.Value(deletionProjectKey{}).(*deletionProjectWitness)
	if w == nil || !w.active || w.tx != tx || w.owner == nil || !w.owner.active || w.owner.runtime == nil || w.owner.runtime.deps.DeletionAuthority != a || !sameStore(w.owner.runtime.store, store) {
		return fault(f.Forbidden)
	}
	return a.requireCurrentInTx(ctx, tx, store)
}

func (a *DeletionAuthority) RequireMeetingCleanupInTx(ctx context.Context, tx f.Tx, actor i.Actor, p i.ProjectID, owner i.UserID) error {
	if a == nil || a.original == nil {
		return fault(f.DependencyUnbound)
	}
	if err := a.RequireMeetingDeletionBindingInTx(ctx, tx, a.original.store); err != nil {
		return err
	}
	w := ctx.Value(deletionProjectKey{}).(*deletionProjectWitness)
	original := w.owner.original
	d := actor.Details()
	if !w.actor.Equal(actor) || p != original.project || owner != original.actor || d.Kind != i.Service || d.ServiceName != i.MeetingRuntime || d.ProjectID != p.String() || d.CauseRef != original.id.String() || original.phase != c.DeletionCancelling {
		return fault(f.Forbidden)
	}
	command, err := commandIdentity(p, original.key)
	if err != nil {
		return err
	}
	if err := a.original.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(p, f.Exclusive), meetingLock(original.meeting, f.Exclusive), commandLock(command)}); err != nil {
		return portError(err)
	}
	x, err := a.original.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	current, err := loadDeletion(ctx, x, p, original.meeting)
	if err != nil {
		return err
	}
	if current != original {
		return fault(f.ConfirmationStale)
	}
	session, err := loadSession(ctx, x, p, original.meeting)
	if err != nil {
		return err
	}
	if !session.Deleting || session.Version < original.accepted {
		return fault(f.ConfirmationStale)
	}
	return ctx.Err()
}
