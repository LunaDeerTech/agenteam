package model

import (
	"context"
	"reflect"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type ProjectAuthority interface {
	AuthorizeProject(context.Context, f.Tx, id.Actor, id.ProjectID, id.AccessIntent) (id.AccessGrant, error)
}

type Authorizations struct {
	Sessions id.SessionAuthority
	System   id.SystemAuthority
	Projects ProjectAuthority
}
type Authority struct{ data func() *authorityState }
type authorityState struct {
	store       Store
	auth        Authorizations
	usageIssuer sc.PlanIssuer
	eventIssuer oc.PlanIssuer
}

func NewAuthority(store Store, d Authorizations) (*Authority, error) {
	if nilPort(store) || nilPort(d.Sessions) || nilPort(d.System) {
		return nil, fault(f.DependencyUnbound)
	}
	if d.Projects != nil {
		value := reflect.ValueOf(d.Projects)
		if nilPort(d.Projects) || value.Kind() == reflect.Chan && value.IsNil() {
			return nil, fault(f.DependencyUnbound)
		}
	}
	s := &authorityState{store: store, auth: d, usageIssuer: sc.NewPlanIssuer(), eventIssuer: oc.NewPlanIssuer()}
	return &Authority{data: func() *authorityState { return s }}, nil
}

// The Project port supplies current Owner facts. Model owns its transaction
// and complete lock plan; an InTx check never acquires or opens a transaction.
func (a *Authority) currentScope(ctx context.Context, tx f.Tx, actor id.Actor, scope id.Scope, intent id.AccessIntent) error {
	if scope.Validate() != nil || (intent != id.Read && intent != id.Mutate) {
		return fault(f.InvalidArgument)
	}
	if scope.Details().Kind == id.System {
		return a.current(ctx, tx, actor, intent)
	}
	if a.state() == nil || nilPort(a.state().auth.Projects) {
		return fault(f.DependencyUnbound)
	}
	if err := human(actor); err != nil {
		return err
	}
	project, err := f.ParseID[id.Project](scope.Details().ProjectID)
	if err != nil {
		return fault(f.InvalidArgument)
	}
	locks := []f.LockRequest{userLock(actor.Details().UserID), projectLock(project.String())}
	if !tx.Valid() {
		cause, err := readCause("project-authorize")
		if err != nil {
			return err
		}
		result := a.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
			if err := a.state().store.AcquireAll(ctx, tx, locks); err != nil {
				return portError(err)
			}
			return a.currentScope(ctx, tx, actor, scope, intent)
		})
		return commitError(result)
	}
	if _, err := a.state().store.InTx(tx); err != nil {
		return portError(err)
	}
	if err := a.state().store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	if err := a.state().auth.Sessions.RequireCurrentSession(ctx, tx, actor); err != nil {
		return portError(err)
	}
	grant, err := a.state().auth.Projects.AuthorizeProject(ctx, tx, actor, project, intent)
	if err != nil {
		return portError(err)
	}
	if !grant.Matches(actor, scope, intent) {
		return fault(f.Forbidden)
	}
	return nil
}
func (a *Authority) state() *authorityState {
	if a == nil || a.data == nil {
		return nil
	}
	return a.data()
}
func human(actor id.Actor) error {
	if actor.Validate() != nil {
		return fault(f.Unauthenticated)
	}
	if actor.Details().Kind != id.Human {
		return fault(f.Forbidden)
	}
	return nil
}
func (a *Authority) current(ctx context.Context, tx f.Tx, actor id.Actor, intent id.AccessIntent) error {
	if a.state() == nil {
		return fault(f.DependencyUnbound)
	}
	if e := human(actor); e != nil {
		return e
	}
	if tx.Valid() {
		if e := a.state().store.RequireHeldLocks(ctx, tx, []f.LockRequest{userLock(actor.Details().UserID)}); e != nil {
			return portError(e)
		}
	}
	if e := a.state().auth.Sessions.RequireCurrentSession(ctx, tx, actor); e != nil {
		return portError(e)
	}
	g, e := a.state().auth.System.AuthorizeSystem(ctx, tx, actor, intent)
	if e != nil {
		return portError(e)
	}
	if !g.Matches(actor, id.SystemScope(), intent) {
		return fault(f.Forbidden)
	}
	return nil
}
