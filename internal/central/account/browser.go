package account

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// AnonymousContext is consumed only by a trusted HTTP adapter. Its two opaque
// materials cannot be marshalled into an ordinary response or log by accident.
type AnonymousContext struct {
	Identity     c.BrowserIdentity
	Cookie, CSRF sc.SecretMaterial
}
type browserPayload struct {
	Format  int                `json:"format"`
	Kid     string             `json:"kid"`
	Browser string             `json:"browser_id"`
	Nonce   string             `json:"nonce"`
	Issued  foundation.Instant `json:"issued"`
	Expires foundation.Instant `json:"expires"`
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	defer clear(b)
	if _, e := rand.Read(b); e != nil {
		return "", unavailable(e)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func (s *Service) NewAnonymousContext(ctx context.Context) (AnonymousContext, error) {
	op, e := s.begin(ctx, false)
	if e != nil {
		return AnonymousContext{}, e
	}
	defer s.finish(op)
	ctx = op.ctx
	st := s.state()
	id, e := foundation.NewID[c.Browser]()
	if e != nil {
		return AnonymousContext{}, unavailable(e)
	}
	nonce, e := randomToken()
	if e != nil {
		return AnonymousContext{}, e
	}
	var now time.Time
	if e = st.store.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return AnonymousContext{}, unavailable(e)
	}
	p := browserPayload{1, st.keys.current(), id.String(), nonce, instant(now), instant(now.Add(time.Hour))}
	b, e := json.Marshal(p)
	if e != nil {
		return AnonymousContext{}, unavailable(e)
	}
	mac, e := st.keys.mac(p.Kid, "anonymous-cookie-v1", b)
	if e != nil {
		return AnonymousContext{}, e
	}
	encoded := base64.RawURLEncoding.EncodeToString(b) + "." + base64.RawURLEncoding.EncodeToString(mac)
	cause, e := recoveryCause("anonymous")
	if e != nil {
		return AnonymousContext{}, e
	}
	r := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, []foundation.LockRequest{configLock("account-security", foundation.Shared)}); e != nil {
			return unavailable(e)
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		tag, e := x.Exec(ctx, `UPDATE agenteam_account.account_key_registry SET anonymous_until=greatest(coalesce(anonymous_until,$2),$2) WHERE kid=$1`, p.Kid, p.Expires.Time())
		if e != nil || tag.RowsAffected() != 1 {
			return unavailable(e)
		}
		return nil
	})
	if e = resultError(r); e != nil {
		return AnonymousContext{}, e
	}
	identity, e := c.NewBrowserIdentity(st.browserIssuer, id, p.Kid, p.Expires)
	if e != nil {
		return AnonymousContext{}, e
	}
	csrf, e := st.keys.mac(p.Kid, "csrf-v1", []byte("anonymous"), []byte(encoded))
	if e != nil {
		return AnonymousContext{}, e
	}
	cookie, _ := sc.NewSecretMaterial([]byte(encoded))
	token, _ := sc.NewSecretMaterial([]byte(base64.RawURLEncoding.EncodeToString(csrf)))
	return AnonymousContext{identity, cookie, token}, nil
}
func (s *Service) VerifyAnonymousContext(ctx context.Context, cookie, csrf sc.SecretMaterial) (c.BrowserIdentity, error) {
	op, e := s.begin(ctx, false)
	if e != nil {
		return c.BrowserIdentity{}, e
	}
	defer s.finish(op)
	ctx = op.ctx
	st := s.state()
	var out c.BrowserIdentity
	err := cookie.Use(func(raw []byte) error {
		if len(raw) > 2048 {
			return fault(foundation.Unauthenticated, nil)
		}
		parts := strings.Split(string(raw), ".")
		if len(parts) != 2 {
			return fault(foundation.Unauthenticated, nil)
		}
		b, e := base64.RawURLEncoding.Strict().DecodeString(parts[0])
		if e != nil || base64.RawURLEncoding.EncodeToString(b) != parts[0] {
			return fault(foundation.Unauthenticated, nil)
		}
		mac, e := base64.RawURLEncoding.Strict().DecodeString(parts[1])
		if e != nil || len(mac) != 32 || base64.RawURLEncoding.EncodeToString(mac) != parts[1] {
			return fault(foundation.Unauthenticated, nil)
		}
		var p browserPayload
		if json.Unmarshal(b, &p) != nil {
			return fault(foundation.Unauthenticated, nil)
		}
		canonical, e := json.Marshal(p)
		if e != nil || string(canonical) != string(b) || p.Format != 1 {
			return fault(foundation.Unauthenticated, nil)
		}
		if e = st.keys.check(p.Kid, "anonymous-cookie-v1", mac, b); e != nil {
			return e
		}
		id, e := foundation.ParseID[c.Browser](p.Browser)
		if e != nil || p.Expires.Time().Sub(p.Issued.Time()) != time.Hour {
			return fault(foundation.Unauthenticated, nil)
		}
		n, e := base64.RawURLEncoding.Strict().DecodeString(p.Nonce)
		if e != nil || len(n) != 32 || base64.RawURLEncoding.EncodeToString(n) != p.Nonce {
			return fault(foundation.Unauthenticated, nil)
		}
		var now time.Time
		if e = st.store.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
			return unavailable(e)
		}
		if now.Before(p.Issued.Time()) || !now.Before(p.Expires.Time()) {
			return fault(foundation.Unauthenticated, nil)
		}
		e = csrf.Use(func(got []byte) error {
			v, e := base64.RawURLEncoding.Strict().DecodeString(string(got))
			if e != nil || base64.RawURLEncoding.EncodeToString(v) != string(got) || st.keys.check(p.Kid, "csrf-v1", v, []byte("anonymous"), raw) != nil {
				return fault(foundation.Forbidden, nil)
			}
			return nil
		})
		if e != nil {
			return portError(e)
		}
		out, e = c.NewBrowserIdentity(st.browserIssuer, id, p.Kid, p.Expires)
		return e
	})
	if err != nil {
		return c.BrowserIdentity{}, portError(err)
	}
	return out, nil
}
func (s *Service) requireBrowser(ctx context.Context, b c.BrowserIdentity) error {
	if !b.IssuedBy(s.state().browserIssuer) {
		return fault(foundation.Unauthenticated, nil)
	}
	var now time.Time
	if e := s.state().store.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return unavailable(e)
	}
	if !now.Before(b.ExpiresAt().Time()) {
		return fault(foundation.Unauthenticated, nil)
	}
	if _, ok := s.state().keys.data().keys[b.KeyID()]; !ok {
		return unavailable(nil)
	}
	return nil
}
