package project

import (
	"context"
	"errors"
	account "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
)

// LifecycleFacts is bound by B03's persisted participant/cause validator. This
// is a real optional dependency, not an Owner grant for a service actor.
type LifecycleFacts interface {
	ValidateLifecycleInTx(context.Context, foundation.Tx, identity.Actor, c.LifecycleCause, c.ParticipantName, c.OperationPhase) error
}
type AuthorityDependencies struct {
	Sessions  identity.SessionAuthority
	Routes    account.CurrentUserRoutes
	Lifecycle LifecycleFacts
}
type Authority struct{ data func() *authorityState }
type authorityState struct {
	store         Store
	sessions      identity.SessionAuthority
	routes        account.CurrentUserRoutes
	lifecycle     LifecycleFacts
	eventIssuer   oc.PlanIssuer
	projectIssuer oc.PlanIssuer
}

func NewAuthority(store Store, d AuthorityDependencies) (*Authority, error) {
	if nilPort(store) || nilPort(d.Sessions) {
		return nil, fault(foundation.DependencyUnbound)
	}
	if facts, ok := d.Lifecycle.(*LifecycleAuthority); ok && (!facts.bound() || !sameStore(store, facts.store)) {
		return nil, fault(foundation.DependencyUnbound)
	}
	st := &authorityState{store: store, sessions: d.Sessions, routes: d.Routes, lifecycle: d.Lifecycle, eventIssuer: oc.NewPlanIssuer(), projectIssuer: oc.NewPlanIssuer()}
	return &Authority{data: func() *authorityState { return st }}, nil
}
func (a *Authority) state() *authorityState {
	if a == nil || a.data == nil {
		return nil
	}
	return a.data()
}
func human(actor identity.Actor) error {
	if actor.Validate() != nil {
		return fault(foundation.Unauthenticated)
	}
	return c.CheckOwnerActorKind(actor)
}
func (a *Authority) current(ctx context.Context, tx foundation.Tx, actor identity.Actor, id c.ProjectID, mode foundation.LockMode) (postgres.SQLExecutor, error) {
	if a.state() == nil {
		return nil, fault(foundation.DependencyUnbound)
	}
	if e := human(actor); e != nil {
		return nil, e
	}
	if id.Validate() != nil {
		return nil, invalid()
	}
	if !tx.Valid() {
		return nil, invalid()
	}
	if e := a.state().store.RequireHeldLocks(ctx, tx, []foundation.LockRequest{userLock(actor.Details().UserID, foundation.Shared), projectLock(id, mode)}); e != nil {
		return nil, unavailable(e)
	}
	if e := a.state().sessions.RequireCurrentSession(ctx, tx, actor); e != nil {
		return nil, portError(e)
	}
	x, e := a.state().store.InTx(tx)
	return x, portError(e)
}
func (a *Authority) RequireOwnerInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, id c.ProjectID, intent identity.AccessIntent) (c.ProjectAccess, error) {
	x, e := a.current(ctx, tx, actor, id, foundation.Shared)
	if e != nil {
		return c.ProjectAccess{}, e
	}
	p, e := ownerProject(ctx, x, actor, id)
	if e != nil {
		return c.ProjectAccess{}, e
	}
	initialized := c.InitializationPending
	if p.initialized {
		initialized = c.Initialized
	}
	if e = c.CheckOwnerGate(p.ref.Lifecycle, initialized, intent); e != nil {
		return c.ProjectAccess{}, e
	}
	now, e := dbNow(ctx, x)
	if e != nil {
		return c.ProjectAccess{}, e
	}
	return c.NewProjectAccess(actor, p.ref, now)
}
func (a *Authority) AuthorizeProject(ctx context.Context, tx foundation.Tx, actor identity.Actor, id c.ProjectID, intent identity.AccessIntent) (identity.AccessGrant, error) {
	if e := human(actor); e != nil {
		return identity.AccessGrant{}, e
	}
	if id.Validate() != nil {
		return identity.AccessGrant{}, invalid()
	}
	if a.state() == nil {
		return identity.AccessGrant{}, fault(foundation.DependencyUnbound)
	}
	var access c.ProjectAccess
	if tx.Valid() {
		var e error
		access, e = a.RequireOwnerInTx(ctx, tx, actor, id, intent)
		if e != nil {
			return identity.AccessGrant{}, e
		}
	} else {
		cause, e := readCause("authorize")
		if e != nil {
			return identity.AccessGrant{}, e
		}
		result := a.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
			if e := a.state().store.AcquireAll(ctx, tx, []foundation.LockRequest{userLock(actor.Details().UserID, foundation.Shared), projectLock(id, foundation.Shared)}); e != nil {
				return unavailable(e)
			}
			var e error
			access, e = a.RequireOwnerInTx(ctx, tx, actor, id, intent)
			return e
		})
		if e := commitError(result); e != nil {
			return identity.AccessGrant{}, e
		}
	}
	scope, _ := identity.InProject(id)
	return identity.NewAccessGrant(actor, scope, intent, access.CheckedAt(), access.Project().Version)
}
func (a *Authority) ResolveProjectPath(ctx context.Context, actor identity.Actor, username, name string) (c.ProjectRef, error) {
	if e := human(actor); e != nil {
		return c.ProjectRef{}, e
	}
	_, e := c.NormalizeProjectPath(username, name)
	if e != nil {
		return c.ProjectRef{}, e
	}
	routeUser, _ := c.NormalizeUserRoute(username)
	routeName, _ := c.NormalizeName(name)
	if a.state() == nil || nilPort(a.state().routes) {
		return c.ProjectRef{}, fault(foundation.DependencyUnbound)
	}
	// Discovery is not authorization. Every round rechecks both canonical
	// mappings under the complete User+Project lock plan in one transaction.
	for range 3 {
		var candidate string
		e := a.state().store.QueryRow(ctx, `SELECT id::text FROM agenteam_project.projects WHERE owner_user_id=$1 AND normalized_name=$2`, actor.Details().UserID, routeName).Scan(&candidate)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return c.ProjectRef{}, unavailable(e)
		}
		locks := []foundation.LockRequest{userLock(actor.Details().UserID, foundation.Shared)}
		var id c.ProjectID
		if candidate != "" {
			id, e = parseID[identity.Project](candidate)
			if e != nil {
				return c.ProjectRef{}, e
			}
			locks = append(locks, projectLock(id, foundation.Shared))
		}
		cause, e := readCause("resolve")
		if e != nil {
			return c.ProjectRef{}, e
		}
		var ref c.ProjectRef
		changed := false
		result := a.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
			if e := a.state().store.AcquireAll(ctx, tx, locks); e != nil {
				return unavailable(e)
			}
			current, e := a.state().routes.CurrentUserRouteInTx(ctx, tx, actor)
			if e != nil {
				return portError(e)
			}
			if current.UserID.String() != actor.Details().UserID || current.Username != routeUser {
				return fault(foundation.NotFound)
			}
			x, e := a.state().store.InTx(tx)
			if e != nil {
				return unavailable(e)
			}
			var actual string
			e = x.QueryRow(ctx, `SELECT id::text FROM agenteam_project.projects WHERE owner_user_id=$1 AND normalized_name=$2`, actor.Details().UserID, routeName).Scan(&actual)
			if errors.Is(e, pgx.ErrNoRows) {
				return fault(foundation.NotFound)
			}
			if e != nil {
				return unavailable(e)
			}
			if actual != candidate {
				changed = true
				return nil
			}
			access, e := a.RequireOwnerInTx(ctx, tx, actor, id, identity.Read)
			if e != nil {
				return e
			}
			ref = access.Project()
			return nil
		})
		if e := commitError(result); e != nil {
			return c.ProjectRef{}, e
		}
		if !changed {
			return ref, nil
		}
	}
	return c.ProjectRef{}, fault(foundation.ResourceBusy)
}
func (a *Authority) ValidateInitializationInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, creation c.CreationID, project c.ProjectID, key foundation.IdempotencyKey) error {
	if a.state() == nil {
		return fault(foundation.DependencyUnbound)
	}
	if !tx.Valid() || actor.Validate() != nil || creation.Validate() != nil || project.Validate() != nil || key.Validate() != nil {
		return invalid()
	}
	d := actor.Details()
	if d.Kind != identity.Service || d.ServiceName != identity.ProjectInitialization || d.ProjectID != project.String() || d.CauseRef != creation.String() {
		return fault(foundation.Forbidden)
	}
	if e := a.state().store.RequireHeldLocks(ctx, tx, []foundation.LockRequest{projectLock(project, foundation.Exclusive)}); e != nil {
		return unavailable(e)
	}
	x, e := a.state().store.InTx(tx)
	if e != nil {
		return unavailable(e)
	}
	r, e := loadCreation(ctx, x, creation)
	if e != nil {
		return e
	}
	p, e := loadProject(ctx, x, project)
	if e != nil {
		return e
	}
	if r == nil || p == nil || r.operation.ProjectID != project || r.initializationKey != key || p.creation != creation || r.owner != p.ref.OwnerUserID || p.ref.Lifecycle != c.Active {
		return fault(foundation.Forbidden)
	}
	// Idempotent confirmation may recheck a completed canonical creation. The
	// provider still proves its own exact protected Skill and revision.
	if r.operation.State != c.CreationInitializing && r.operation.State != c.CreationCompleted {
		return fault(foundation.InvalidState)
	}
	if (r.operation.State == c.CreationCompleted) != p.initialized {
		return unavailable(nil)
	}
	return nil
}
func (a *Authority) ValidateLifecycleInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, cause c.LifecycleCause, participant c.ParticipantName, phase c.OperationPhase) error {
	if a.state() == nil || nilPort(a.state().lifecycle) {
		return fault(foundation.DependencyUnbound)
	}
	return portError(a.state().lifecycle.ValidateLifecycleInTx(ctx, tx, actor, cause, participant, phase))
}

var _ c.ProjectAuthority = (*Authority)(nil)
