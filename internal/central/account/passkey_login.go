package account

import (
	"bytes"
	"context"
	"encoding/json"
	"net/netip"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/account/passkeycrypto"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type passkeyLoginProof struct {
	operation          passkeyOperation
	credentialID, user string
	version            int64
	updated            pc.Credential
}

func (s *Service) BeginPasskeyLogin(ctx context.Context, v *pc.Verifier, browser c.BrowserIdentity) (PasskeyOptions, error) {
	var out PasskeyOptions
	if e := passkeyUnavailable(v); e != nil {
		return out, e
	}
	op, e := s.begin(ctx, false)
	if e != nil {
		return out, e
	}
	defer s.finish(op)
	if e = s.requireBrowser(op.ctx, browser); e != nil {
		return out, e
	}
	options, data, e := v.BeginDiscoverableLogin()
	if e != nil {
		return out, unavailable(nil)
	}
	id, e := foundation.NewID[struct{}]()
	if e != nil {
		return out, unavailable(e)
	}
	cause, e := recoveryCause("passkey-login-options")
	if e != nil {
		return out, e
	}
	st := s.state()
	var expiry time.Time
	result := st.store.WithinTx(op.ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, []foundation.LockRequest{recordLock(browser.ID().String())}); e != nil {
			return unavailable(e)
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		if e = s.validateBrowserInTx(ctx, x, browser); e != nil {
			return e
		}
		var count int
		if e = x.QueryRow(ctx, `SELECT count(*) FROM agenteam_account.passkey_operations WHERE browser_id=$1 AND expires_at>clock_timestamp()`, browser.ID().String()).Scan(&count); e != nil {
			return unavailable(e)
		}
		if count >= 20 {
			return fault(foundation.RateLimited, nil)
		}
		e = x.QueryRow(ctx, `INSERT INTO agenteam_account.passkey_operations(id,purpose,browser_id,session_data,created_at,expires_at) SELECT $1,'login',$2,$3,n,n+interval '5 minutes' FROM (SELECT clock_timestamp() n) z RETURNING expires_at`, id.String(), browser.ID().String(), []byte(data)).Scan(&expiry)
		return portError(e)
	})
	if e = resultError(result); e != nil {
		return out, e
	}
	return PasskeyOptions{id.String(), instant(expiry), options}, nil
}

func (s *Service) CompletePasskeyLogin(ctx context.Context, v *pc.Verifier, browser c.BrowserIdentity, key foundation.IdempotencyKey, ip netip.Addr, operationID string, credential json.RawMessage) (LoginResponse, error) {
	if passkeyUnavailable(v) != nil || !passkeyID(operationID) || key.Validate() != nil || len(credential) == 0 {
		return LoginResponse{}, invalid()
	}
	op, e := s.begin(ctx, false)
	if e != nil {
		return LoginResponse{}, e
	}
	defer s.finish(op)
	if e = s.requireBrowser(op.ctx, browser); e != nil {
		return LoginResponse{}, e
	}
	st := s.state()
	operation, e := loadPasskeyOperation(op.ctx, st.store, operationID)
	if e != nil {
		return LoginResponse{}, e
	}
	if operation.purpose != "login" || operation.browser != browser.ID().String() || operation.failures >= 5 {
		return LoginResponse{}, fault(foundation.Unauthenticated, nil)
	}
	proof := &passkeyLoginProof{operation: operation}
	lookup := func(rawID, handle []byte) (pc.User, error) {
		e := st.store.QueryRow(op.ctx, `SELECT id::text,user_id::text,version FROM agenteam_account.passkeys WHERE credential_id=$1 AND revoked_at IS NULL`, rawID).Scan(&proof.credentialID, &proof.user, &proof.version)
		if e != nil {
			return pc.User{}, fault(foundation.Unauthenticated, nil)
		}
		if len(handle) == 0 || !bytes.Equal(handle, []byte(proof.user)) {
			return pc.User{}, fault(foundation.Unauthenticated, nil)
		}
		return loadPasskeyUser(op.ctx, st.store, proof.user)
	}
	_, proof.updated, e = v.FinishAssertion(lookup, operation.data, credential)
	if e != nil {
		if recorded := s.recordPasskeyLoginFailure(op.ctx, browser, operationID); recorded != nil {
			return LoginResponse{}, recorded
		}
		return LoginResponse{}, fault(foundation.Unauthenticated, nil)
	}
	user, e := loadUser(op.ctx, st.store, proof.user)
	if e != nil {
		return LoginResponse{}, e
	}
	// This is a keyed semantic input, never a password substitute supplied by an
	// HTTP caller. The private proof is required independently in the commit Tx.
	semantic, e := json.Marshal([]any{"passkey-login-v1", operationID, credential})
	if e != nil {
		return LoginResponse{}, invalid()
	}
	defer clear(semantic)
	material, e := sc.NewSecretMaterial(semantic)
	if e != nil {
		return LoginResponse{}, unavailable(e)
	}
	defer material.Destroy()
	request, e := c.NewLoginRequest(c.LoginFields{Browser: browser, Key: key, Email: user.user.Email, Password: material, ClientIP: ip})
	if e != nil {
		return LoginResponse{}, e
	}
	return s.login(ctx, request, proof)
}

func (s *Service) checkLoginAuthentication(ctx context.Context, tx foundation.Tx, r c.LoginRequest, email string, consume bool, p *passkeyLoginProof, commandID string) error {
	if p == nil {
		return s.requireChallenge(ctx, tx, r, email, consume)
	}
	x, e := s.state().store.InTx(tx)
	if e != nil {
		return unavailable(e)
	}
	o, e := loadPasskeyOperation(ctx, x, p.operation.id)
	if e != nil {
		return e
	}
	if o.purpose != "login" || o.browser != r.Fields().Browser.ID().String() || o.command != "" {
		return fault(foundation.Unauthenticated, nil)
	}
	if e = livePasskeyOperation(ctx, x, o); e != nil {
		return e
	}
	var current bool
	e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.passkeys WHERE id=$1 AND user_id=$2 AND version=$3 AND revoked_at IS NULL)`, p.credentialID, p.user, p.version).Scan(&current)
	if e != nil {
		return unavailable(e)
	}
	if !current {
		return fault(foundation.Unauthenticated, nil)
	}
	if !consume {
		return nil
	}
	if _, e = x.Exec(ctx, `UPDATE agenteam_account.passkeys SET public_data=$2,version=version+1,last_used_at=clock_timestamp() WHERE id=$1`, p.credentialID, []byte(p.updated.PublicData)); e != nil {
		return unavailable(e)
	}
	if _, e = x.Exec(ctx, `UPDATE agenteam_account.passkey_operations SET consumed_command_id=$2 WHERE id=$1 AND consumed_command_id IS NULL`, o.id, commandID); e != nil {
		return unavailable(e)
	}
	_, e = x.Exec(ctx, `INSERT INTO agenteam_account.passkey_logins(command_id,operation_id,credential_id,credential_version) VALUES($1,$2,$3,$4)`, commandID, o.id, p.credentialID, p.version+1)
	return portError(e)
}

// A historical cookie can only be recovered while its authenticating
// credential remains active. No new assertion or Session is minted on replay.
func currentPasskeyLogin(ctx context.Context, x postgres.SQLExecutor, commandID string) error {
	var permitted bool
	e := x.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM agenteam_account.passkey_logins WHERE command_id=$1) OR EXISTS(SELECT 1 FROM agenteam_account.passkey_logins l JOIN agenteam_account.passkeys p ON p.id=l.credential_id JOIN agenteam_account.commands c ON c.id=l.command_id WHERE l.command_id=$1 AND p.revoked_at IS NULL AND p.user_id=c.user_id)`, commandID).Scan(&permitted)
	if e != nil {
		return unavailable(e)
	}
	if !permitted {
		return fault(foundation.Unauthenticated, nil)
	}
	return nil
}
