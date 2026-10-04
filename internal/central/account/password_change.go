package account

import (
	"context"
	"crypto/subtle"
	"fmt"
	"io"
	"log/slog"
	"sync"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// PasswordChangeResponse owns the newly issued cookie until the trusted
// response adapter has finished using it. It is never recoverable by replaying
// the old revoked Session; a lost response requires a fresh login.
type PasswordChangeResponse struct{ data func() *changedResponse }
type changedResponse struct {
	mu       sync.Mutex
	service  *Service
	op       *operation
	material sc.SecretMaterial
	uses     int
	closing  bool
	finished bool
	done     chan struct{}
	stop     func() bool
}

func (r PasswordChangeResponse) UseCookie(fn func([]byte) error) error {
	if r.data == nil || fn == nil {
		return invalid()
	}
	v := r.data()
	v.mu.Lock()
	if v.closing {
		v.mu.Unlock()
		return fault(foundation.ShuttingDown, nil)
	}
	if e := v.op.ctx.Err(); e != nil {
		v.mu.Unlock()
		return unavailable(e)
	}
	v.uses++
	v.mu.Unlock()
	defer func() {
		v.mu.Lock()
		v.uses--
		v.finishLocked()
		v.mu.Unlock()
	}()
	return portError(v.material.Use(fn))
}
func (v *changedResponse) close() {
	v.mu.Lock()
	v.closing = true
	v.finishLocked()
	v.mu.Unlock()
}
func (v *changedResponse) finishLocked() {
	// Destroy erases the owned material but does not join private copies already
	// lent to Use callbacks. Keep the operation registered until they all return.
	if !v.closing || v.uses != 0 || v.finished {
		return
	}
	v.finished = true
	v.material.Destroy()
	v.service.finish(v.op)
	close(v.done)
}
func (r PasswordChangeResponse) Close(ctx context.Context) error {
	if r.data == nil {
		return nil
	}
	v := r.data()
	v.close()
	select {
	case <-v.done:
		if v.stop != nil {
			v.stop()
		}
		return nil
	case <-ctx.Done():
		return unavailable(ctx.Err())
	}
}
func (r PasswordChangeResponse) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "password_change_response")
}
func (r PasswordChangeResponse) MarshalJSON() ([]byte, error) {
	return []byte(`"password_change_response"`), nil
}
func (*PasswordChangeResponse) UnmarshalJSON([]byte) error { return invalid() }
func (r PasswordChangeResponse) LogValue() slog.Value {
	return slog.StringValue("password_change_response")
}
func (s *Service) ChangePassword(ctx context.Context, r c.PasswordChange) (PasswordChangeResponse, error) {
	if r.Validate() != nil {
		return PasswordChangeResponse{}, invalid()
	}
	op, e := s.begin(ctx, false)
	if e != nil {
		return PasswordChangeResponse{}, e
	}
	owned := false
	defer func() {
		if !owned {
			s.finish(op)
		}
	}()
	ctx = op.ctx
	st := s.state()
	f := r.Fields()
	if nilPort(st.deps.Events) || st.deps.SessionsRevoked.Schema().EventType != c.SessionsRevokedType {
		return PasswordChangeResponse{}, fault(foundation.DependencyUnbound, nil)
	}
	current, e := st.deps.Authority.current(ctx, foundation.Tx{}, f.Actor)
	if e != nil {
		return PasswordChangeResponse{}, e
	}
	if e = passwordPair(f.Password, f.Confirmation); e != nil {
		return PasswordChangeResponse{}, e
	}
	key, e := foundation.NewCommandIdentity("account.password", []string{f.Actor.Details().UserID}, "password-change", f.Key)
	if e != nil {
		return PasswordChangeResponse{}, e
	}
	if e = s.trackMutation(key, op); e != nil {
		return PasswordChangeResponse{}, e
	}
	macFor := func(kid string) (out []byte, e error) {
		e = f.OldPassword.Use(func(old []byte) error {
			return f.Password.Use(func(p []byte) error {
				out, e = st.keys.mac(kid, "command-v1", []byte("password-change"), []byte(f.Actor.Details().UserID), []byte(f.ExpectedVersion.String()), old, p)
				return e
			})
		})
		return out, portError(e)
	}
	old, e := s.lookupMutation(ctx, key, macFor, f.Actor, c.BrowserIdentity{}, false)
	if e != nil && !hasFaultCode(e, foundation.NotFound) {
		return PasswordChangeResponse{}, e
	}
	if e == nil && old.phase != "planned" {
		return PasswordChangeResponse{}, fault(foundation.SessionRevoked, nil)
	}
	if current.user.user.Version != f.ExpectedVersion {
		return PasswordChangeResponse{}, fault(foundation.VersionConflict, nil)
	}
	ok, e := st.hasher.Verify(ctx, f.OldPassword, current.user.phc)
	if e != nil {
		return PasswordChangeResponse{}, e
	}
	if !ok {
		return PasswordChangeResponse{}, field("/current_password", "INVALID")
	}
	hash, e := st.hasher.Hash(ctx, f.Password)
	if e != nil {
		return PasswordChangeResponse{}, e
	}
	raw, e := randomToken()
	if e != nil {
		return PasswordChangeResponse{}, e
	}
	material, e := sc.NewSecretMaterial([]byte(raw))
	raw = ""
	if e != nil {
		return PasswordChangeResponse{}, e
	}
	defer func() {
		if !owned {
			material.Destroy()
		}
	}()
	var verifier []byte
	e = material.Use(func(b []byte) error { var e error; verifier, e = sessionVerifier(b); return e })
	if e != nil {
		return PasswordChangeResponse{}, e
	}
	defer clear(verifier)
	id, e := foundation.NewID[c.Command]()
	if e != nil {
		return PasswordChangeResponse{}, unavailable(e)
	}
	sid, e := foundation.NewID[identity.Session]()
	if e != nil {
		return PasswordChangeResponse{}, unavailable(e)
	}
	cause, e := foundation.NewCommandsCause(key)
	if e != nil {
		return PasswordChangeResponse{}, e
	}
	mac, e := macFor(st.keys.current())
	if e != nil {
		return PasswordChangeResponse{}, e
	}
	defer clear(mac)
	locks := []foundation.LockRequest{commandLock(key), configLock("account-directory", foundation.Shared), configLock("account-security", foundation.Shared), userLock(f.Actor.Details().UserID, foundation.Exclusive), recordLock(id.String()), recordLock(sid.String())}
	var cmd commandRecord
	var evt event.Event
	planned := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
			return unavailable(e)
		}
		if e := st.deps.Authority.RequireCurrentSession(ctx, tx, f.Actor); e != nil {
			return e
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		u, e := loadUser(ctx, x, f.Actor.Details().UserID)
		if e != nil {
			return e
		}
		if u.passwordVersion != current.user.passwordVersion {
			return fault(foundation.Unauthenticated, nil)
		}
		if u.user.Version != f.ExpectedVersion {
			return fault(foundation.VersionConflict, nil)
		}
		old, e := loadCommand(ctx, x, string(digest([]byte(key.Canonical()))), true)
		if e == nil {
			if e = s.checkCommand(old, key, macFor); e != nil {
				return e
			}
			if old.phase != "planned" || old.session != f.Actor.Details().SessionID {
				return fault(foundation.InvalidState, nil)
			}
			cmd = old
		} else {
			if !hasFaultCode(e, foundation.NotFound) {
				return e
			}
			if _, e = x.Exec(ctx, `INSERT INTO agenteam_account.commands(id,namespace,owner_id,command_key,command_name,identity_digest,semantic_kid,semantic_mac,actor_kind,user_id,session_id,origin_process_id,resource_id,password_version,expected_version,phase) VALUES($1,'account.password',$2,$3,'password-change',$4,$5,$6,'human',$2,$7,$8,$9,$10,$11,'planned')`, id.String(), f.Actor.Details().UserID, string(f.Key), string(digest([]byte(key.Canonical()))), st.keys.current(), mac, f.Actor.Details().SessionID, st.process.String(), sid.String(), int64(u.passwordVersion), int64(f.ExpectedVersion)); e != nil {
				return unavailable(e)
			}
			cmd, e = loadCommand(ctx, x, id.String(), false)
			if e != nil {
				return e
			}
		}
		evt, e = s.planPasswordEvent(ctx, x, cmd, u, c.PasswordChanged)
		return e
	})
	if e = resultError(planned); e != nil {
		return PasswordChangeResponse{}, e
	}
	plan, e := st.deps.Events.PrepareAppend(ctx, f.Actor, evt)
	if e != nil {
		return PasswordChangeResponse{}, portError(e)
	}
	locks = append(commandLocks(cmd), userLock(cmd.user, foundation.Exclusive), recordLock(cmd.resource))
	locks = append(locks, plan.Locks()...)
	guard, e := st.deps.Authority.mailExclusive(ctx)
	if e != nil {
		return PasswordChangeResponse{}, e
	}
	defer guard.release()
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
			return unavailable(e)
		}
		if e := st.deps.Authority.RequireCurrentSession(ctx, tx, f.Actor); e != nil {
			return e
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		now, e := loadCommand(ctx, x, cmd.id.String(), false)
		if e != nil {
			return e
		}
		if e = s.checkCommand(now, key, macFor); e != nil {
			return e
		}
		if commandMapping(now) != commandMapping(cmd) || now.phase != "planned" {
			return fault(foundation.ResourceBusy, nil)
		}
		u, e := loadUser(ctx, x, cmd.user)
		if e != nil {
			return e
		}
		if int64(u.passwordVersion) != cmd.passwordVersion {
			return fault(foundation.Unauthenticated, nil)
		}
		if u.user.Version != f.ExpectedVersion {
			return fault(foundation.VersionConflict, nil)
		}
		seq := *evt.Header().AggregateSequence
		if u.sequence+1 != seq {
			return fault(foundation.ResourceBusy, nil)
		}
		if _, e = x.Exec(ctx, `UPDATE agenteam_account.users SET auth_sequence=$2 WHERE id=$1`, cmd.user, int64(seq)); e != nil {
			return unavailable(e)
		}
		if _, e = st.deps.Events.AppendEventInTx(ctx, tx, f.Actor, evt, plan); e != nil {
			return portError(e)
		}
		if e = s.mutationAudit(ctx, tx, f.Actor, ac.AccountPasswordChange, ac.UserResource, cmd.user, cmd.id.String(), ac.AccountMetadataFields{UserID: cmd.user, Version: u.user.Version + 1, Phase: ac.AccountUpdated, ChangedFields: []ac.AccountChangedField{ac.PasswordChanged}}); e != nil {
			return e
		}
		settings, e := loadSettings(ctx, x)
		if e != nil {
			return e
		}
		if _, e = x.Exec(ctx, `UPDATE agenteam_account.sessions SET revoked_at=clock_timestamp(),revoked_reason='password_changed' WHERE user_id=$1 AND revoked_at IS NULL`, cmd.user); e != nil {
			return unavailable(e)
		}
		if _, e = x.Exec(ctx, `INSERT INTO agenteam_account.sessions(id,user_id,token_verifier,csrf_kid,issued_at,last_activity_at,idle_seconds,absolute_expires_at) SELECT $1,$2,$3,$4,n,n,$5,n+$6*interval '1 second' FROM (SELECT clock_timestamp() n) z`, cmd.resource, cmd.user, verifier, st.keys.current(), int64(settings.SessionIdleSeconds), int64(settings.SessionAbsoluteSeconds)); e != nil {
			return unavailable(e)
		}
		if _, e = x.Exec(ctx, `UPDATE agenteam_account.users SET password_phc=$2,password_version=password_version+1,version=version+1,initial_password_suggestion=false,updated_at=clock_timestamp() WHERE id=$1`, cmd.user, hash.encoded()); e != nil {
			return unavailable(e)
		}
		return completeCommand(ctx, x, cmd.id.String(), int64(u.user.Version+1))
	})
	if result.State() == foundation.Unknown {
		checked := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
			if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
				return unavailable(e)
			}
			x, e := st.store.InTx(tx)
			if e != nil {
				return unavailable(e)
			}
			now, e := loadCommand(ctx, x, cmd.id.String(), false)
			if e != nil {
				return e
			}
			if e = s.checkCommand(now, key, macFor); e != nil {
				return e
			}
			if now.phase != "committed" || now.resource != cmd.resource {
				return fault(foundation.CommitUnknown, nil)
			}
			var actual []byte
			var live bool
			e = x.QueryRow(ctx, `SELECT token_verifier,revoked_at IS NULL AND absolute_expires_at>clock_timestamp() AND last_activity_at+idle_seconds*interval '1 second'>clock_timestamp() FROM agenteam_account.sessions WHERE id=$1 AND user_id=$2`, cmd.resource, cmd.user).Scan(&actual, &live)
			if e != nil {
				return unavailable(e)
			}
			if !live || subtle.ConstantTimeCompare(actual, verifier) != 1 {
				return fault(foundation.SessionRevoked, nil)
			}
			return nil
		})
		if e = resultError(checked); e != nil {
			return PasswordChangeResponse{}, confirmationError(result, e)
		}
	} else if e = resultError(result); e != nil {
		return PasswordChangeResponse{}, e
	}
	if e = ctx.Err(); e != nil {
		return PasswordChangeResponse{}, unavailable(e)
	}
	v := &changedResponse{service: s, op: op, material: material, done: make(chan struct{})}
	v.stop = context.AfterFunc(op.ctx, v.close)
	owned = true
	return PasswordChangeResponse{func() *changedResponse { return v }}, nil
}
