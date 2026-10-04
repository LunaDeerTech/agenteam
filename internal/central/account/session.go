package account

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

type SessionView struct {
	User    c.User
	Session c.Session
	CSRF    sc.SecretMaterial
}

func sessionVerifier(raw []byte) ([]byte, error) {
	b, e := base64.RawURLEncoding.Strict().DecodeString(string(raw))
	defer clear(b)
	if e != nil || len(b) != 32 || base64.RawURLEncoding.EncodeToString(b) != string(raw) {
		return nil, fault(foundation.Unauthenticated, nil)
	}
	h := sha256.New()
	_, _ = h.Write([]byte("agenteam.account.session.v1\x00"))
	_, _ = h.Write(b)
	return h.Sum(nil), nil
}
func (s *Service) Authenticate(ctx context.Context, cookie sc.SecretMaterial) (identity.Actor, error) {
	op, e := s.begin(ctx, false)
	if e != nil {
		return identity.Actor{}, e
	}
	defer s.finish(op)
	var actor identity.Actor
	e = cookie.Use(func(raw []byte) error {
		verifier, e := sessionVerifier(raw)
		if e != nil {
			return e
		}
		var uid, sid string
		e = s.state().store.QueryRow(op.ctx, `SELECT user_id::text,id::text FROM agenteam_account.sessions WHERE token_verifier=$1`, verifier).Scan(&uid, &sid)
		if errors.Is(e, pgx.ErrNoRows) {
			return fault(foundation.Unauthenticated, nil)
		}
		if e != nil {
			return unavailable(e)
		}
		user, e := parseID[identity.User](uid)
		if e != nil {
			return e
		}
		session, e := parseID[identity.Session](sid)
		if e != nil {
			return e
		}
		actor, e = identity.NewHuman(user, session)
		if e != nil {
			return unavailable(e)
		}
		return s.state().deps.Authority.RequireCurrentSession(op.ctx, foundation.Tx{}, actor)
	})
	if e != nil {
		return identity.Actor{}, portError(e)
	}
	return actor, nil
}
func (s *Service) GetSession(ctx context.Context, cookie sc.SecretMaterial) (SessionView, error) {
	op, e := s.begin(ctx, false)
	if e != nil {
		return SessionView{}, e
	}
	defer s.finish(op)
	ctx = op.ctx
	actor, e := s.Authenticate(ctx, cookie)
	if e != nil {
		return SessionView{}, e
	}
	r, e := s.state().deps.Authority.current(ctx, foundation.Tx{}, actor)
	if e != nil {
		return SessionView{}, e
	}
	var token sc.SecretMaterial
	e = cookie.Use(func(raw []byte) error {
		v, e := s.state().keys.mac(r.kid, "csrf-v1", []byte("session"), []byte(r.session.ID.String()), raw)
		if e != nil {
			return e
		}
		token, e = sc.NewSecretMaterial([]byte(base64.RawURLEncoding.EncodeToString(v)))
		return e
	})
	if e != nil {
		return SessionView{}, portError(e)
	}
	return SessionView{r.user.user, r.session, token}, nil
}
func (s *Service) ValidateSessionCSRF(ctx context.Context, cookie, token sc.SecretMaterial) (identity.Actor, error) {
	op, e := s.begin(ctx, false)
	if e != nil {
		return identity.Actor{}, e
	}
	defer s.finish(op)
	ctx = op.ctx
	actor, e := s.Authenticate(ctx, cookie)
	if e != nil {
		return identity.Actor{}, e
	}
	r, e := s.state().deps.Authority.current(ctx, foundation.Tx{}, actor)
	if e != nil {
		return identity.Actor{}, e
	}
	e = cookie.Use(func(raw []byte) error {
		return token.Use(func(got []byte) error {
			v, e := base64.RawURLEncoding.Strict().DecodeString(string(got))
			if e != nil || base64.RawURLEncoding.EncodeToString(v) != string(got) {
				return fault(foundation.Forbidden, nil)
			}
			if e = s.state().keys.check(r.kid, "csrf-v1", v, []byte("session"), []byte(r.session.ID.String()), raw); e != nil {
				return fault(foundation.Forbidden, e)
			}
			return nil
		})
	})
	if e != nil {
		return identity.Actor{}, portError(e)
	}
	return actor, nil
}

// TouchActivityInTx is only for explicitly declared successful user business
// operations. Authentication, GET session, polling and transport pings never
// call it. The outer business command must have collected User EX initially.
func (a *Authority) TouchActivityInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor) error {
	if actor.Validate() != nil || actor.Details().Kind != identity.Human {
		return fault(foundation.Unauthenticated, nil)
	}
	if e := a.state().store.RequireHeldLocks(ctx, tx, []foundation.LockRequest{userLock(actor.Details().UserID, foundation.Exclusive)}); e != nil {
		return unavailable(e)
	}
	if e := a.RequireCurrentSession(ctx, tx, actor); e != nil {
		return e
	}
	x, e := a.state().store.InTx(tx)
	if e != nil {
		return unavailable(e)
	}
	_, e = x.Exec(ctx, `UPDATE agenteam_account.sessions SET last_activity_at=clock_timestamp() WHERE id=$1 AND last_activity_at<=clock_timestamp()-interval '60 seconds' AND revoked_at IS NULL AND absolute_expires_at>clock_timestamp()`, actor.Details().SessionID)
	if e != nil {
		return unavailable(e)
	}
	return nil
}
