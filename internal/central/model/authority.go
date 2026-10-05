package model

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type Authorizations struct {
	Sessions id.SessionAuthority
	System   id.SystemAuthority
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
	s := &authorityState{store: store, auth: d, usageIssuer: sc.NewPlanIssuer(), eventIssuer: oc.NewPlanIssuer()}
	return &Authority{data: func() *authorityState { return s }}, nil
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
