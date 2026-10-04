package account

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type authorityState struct {
	store        Store
	keys         Keyring
	secretIssuer sc.PlanIssuer
	eventIssuer  oc.PlanIssuer
	mail         *mailAdmission
}
type Authority struct{ data func() *authorityState }

func NewAuthority(store Store, keys Keyring) (*Authority, error) {
	if nilPort(store) || keys.Validate() != nil {
		return nil, invalid()
	}
	s := &authorityState{store: store, keys: keys, secretIssuer: sc.NewPlanIssuer(), eventIssuer: oc.NewPlanIssuer(), mail: &mailAdmission{}}
	return &Authority{func() *authorityState { return s }}, nil
}
func (a *Authority) state() *authorityState { return a.data() }
func (a *Authority) current(ctx context.Context, tx foundation.Tx, actor identity.Actor) (sessionRecord, error) {
	if a == nil || a.data == nil || actor.Validate() != nil || actor.Details().Kind != identity.Human {
		return sessionRecord{}, fault(foundation.Unauthenticated, nil)
	}
	s := a.state()
	if !tx.Valid() {
		cause, e := recoveryCause("session")
		if e != nil {
			return sessionRecord{}, e
		}
		var out sessionRecord
		r := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
			if e := s.store.AcquireAll(ctx, tx, []foundation.LockRequest{userLock(actor.Details().UserID, foundation.Shared)}); e != nil {
				return unavailable(e)
			}
			var e error
			out, e = a.current(ctx, tx, actor)
			return e
		})
		if e := resultError(r); e != nil {
			return sessionRecord{}, e
		}
		return out, nil
	}
	if e := s.store.RequireHeldLocks(ctx, tx, []foundation.LockRequest{userLock(actor.Details().UserID, foundation.Shared)}); e != nil {
		return sessionRecord{}, unavailable(e)
	}
	e, err := s.store.InTx(tx)
	if err != nil {
		return sessionRecord{}, unavailable(err)
	}
	out, err := loadCurrentSession(ctx, e, actor)
	if err != nil {
		return sessionRecord{}, err
	}
	if _, ok := s.keys.data().keys[out.kid]; !ok {
		return sessionRecord{}, unavailable(nil)
	}
	return out, nil
}
func (a *Authority) RequireCurrentSession(ctx context.Context, tx foundation.Tx, actor identity.Actor) error {
	_, e := a.current(ctx, tx, actor)
	return e
}
func (a *Authority) AuthorizeSystem(ctx context.Context, tx foundation.Tx, actor identity.Actor, intent identity.AccessIntent) (identity.AccessGrant, error) {
	r, e := a.current(ctx, tx, actor)
	if e != nil {
		return identity.AccessGrant{}, e
	}
	if r.user.user.Role != "admin" {
		return identity.AccessGrant{}, fault(foundation.Forbidden, nil)
	}
	now, e := foundation.NewInstant(r.checked)
	if e != nil {
		return identity.AccessGrant{}, unavailable(e)
	}
	return identity.NewAccessGrant(actor, identity.SystemScope(), intent, now, r.user.user.Version)
}
func (a Authority) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "account_authority") }
func (a Authority) MarshalJSON() ([]byte, error) { return []byte(`"account_authority"`), nil }
func (a Authority) LogValue() slog.Value         { return slog.StringValue("account_authority") }

var _ identity.SessionAuthority = (*Authority)(nil)
var _ identity.SystemAuthority = (*Authority)(nil)
