package account

import (
	"context"
	"crypto/subtle"
	"errors"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

func hasFaultCode(e error, code foundation.Code) bool {
	var f *foundation.Fault
	return errors.As(e, &f) && f != nil && f.Code == code
}
func serviceActor(name identity.ServiceName, cause string) (identity.Actor, error) {
	r, e := identity.RegisterService(name)
	if e != nil {
		return identity.Actor{}, e
	}
	return r.Actor(cause, identity.SystemScope())
}
func (s *Service) loginMAC(r c.LoginRequest, kid, email string) ([]byte, error) {
	var mac []byte
	e := r.Fields().Password.Use(func(b []byte) error {
		var e error
		mac, e = s.state().keys.mac(kid, "command-v1", []byte("login"), []byte(r.Fields().Browser.ID().String()), []byte(email), b)
		return e
	})
	return mac, portError(e)
}
func (s *Service) verifyLoginCommand(ctx context.Context, x postgres.SQLExecutor, r c.LoginRequest, email string, cmd commandRecord) error {
	f := r.Fields()
	if cmd.name != "login" || cmd.actorKind != "browser" || cmd.browser != f.Browser.ID().String() {
		return fault(foundation.Forbidden, nil)
	}
	mac, e := s.loginMAC(r, cmd.kid, email)
	if e != nil {
		return e
	}
	defer clear(mac)
	if subtle.ConstantTimeCompare(cmd.mac, mac) != 1 {
		return fault(foundation.IdempotencyKeyReused, nil)
	}
	var live bool
	if e = x.QueryRow(ctx, `SELECT clock_timestamp()<$1`, f.Browser.ExpiresAt().Time()).Scan(&live); e != nil {
		return unavailable(e)
	}
	if !live {
		return fault(foundation.Unauthenticated, nil)
	}
	return nil
}
func loginIdentity(r c.LoginRequest) (foundation.CommandIdentity, error) {
	return foundation.NewCommandIdentity("account.login", []string{r.Fields().Browser.ID().String()}, "login", r.Fields().Key)
}
func secretIdentity(owner string) (foundation.CommandIdentity, error) {
	return foundation.NewCommandIdentity("secret.account", []string{owner}, "create", foundation.IdempotencyKey(owner))
}
func (s *Service) Login(ctx context.Context, r c.LoginRequest) (LoginResponse, error) {
	responseCtx := ctx
	if r.Validate() != nil {
		return LoginResponse{}, invalid()
	}
	op, e := s.begin(ctx, false)
	if e != nil {
		return LoginResponse{}, e
	}
	defer s.finish(op)
	ctx = op.ctx
	if e := s.requireBrowser(ctx, r.Fields().Browser); e != nil {
		return LoginResponse{}, e
	}
	email, e := NormalizeEmail(r.Fields().Email)
	if e != nil {
		return LoginResponse{}, e
	}
	identityKey, e := loginIdentity(r)
	if e != nil {
		return LoginResponse{}, invalid()
	}
	st := s.state()
	cmdID, e := foundation.NewID[c.Command]()
	if e != nil {
		return LoginResponse{}, unavailable(e)
	}
	st.mu.Lock()
	st.logins[cmdID.String()] = &loginOperation{op: op, identity: identityKey}
	st.mu.Unlock()
	retainLocal := false
	defer func() {
		if !retainLocal {
			st.mu.Lock()
			delete(st.logins, cmdID.String())
			st.mu.Unlock()
		}
	}()
	attempt, e := foundation.NewID[c.Attempt]()
	if e != nil {
		return LoginResponse{}, unavailable(e)
	}
	sessionID, e := foundation.NewID[identity.Session]()
	if e != nil {
		return LoginResponse{}, unavailable(e)
	}
	raw, e := randomToken()
	if e != nil {
		return LoginResponse{}, e
	}
	cookie, e := sc.NewSecretMaterial([]byte(raw))
	raw = ""
	if e != nil {
		return LoginResponse{}, unavailable(e)
	}
	defer cookie.Destroy()
	secretCommand, e := secretIdentity(cmdID.String())
	if e != nil {
		return LoginResponse{}, invalid()
	}
	secretDigest, e := canonicalCommandDigest(secretCommand)
	if e != nil {
		return LoginResponse{}, e
	}
	actor, e := serviceActor(identity.AccountAuth, cmdID.String())
	if e != nil {
		return LoginResponse{}, e
	}
	write, e := sc.NewServiceWriteRequest(sc.ServiceWriteFields{Actor: actor, Scope: identity.SystemScope(), Identity: secretCommand, OwnerKind: sc.LoginResponseMaterial, OwnerID: cmdID.String(), Kind: sc.Create, Purpose: sc.System, Value: cookie})
	if e != nil {
		return LoginResponse{}, e
	}
	binding, e := sc.ServiceWriteBinding(write)
	if e != nil {
		return LoginResponse{}, e
	}
	var user userRecord
	var exists bool
	var uid string
	e = st.store.QueryRow(ctx, `SELECT id::text FROM agenteam_account.users WHERE email=$1`, email).Scan(&uid)
	if e == nil {
		exists = true
		user, e = loadUser(ctx, st.store, uid)
	} else if errors.Is(e, pgx.ErrNoRows) {
		e = nil
	}
	if e != nil {
		return LoginResponse{}, portError(e)
	}
	locks := []foundation.LockRequest{commandLock(identityKey), configLock("account-directory", foundation.Shared), configLock("account-security", foundation.Shared), recordLock(cmdID.String()), recordLock(attempt.String())}
	if exists {
		locks = append(locks, userLock(uid, foundation.Exclusive))
	}
	privacyLocks, e := s.privacyLocks(r, email)
	if e != nil {
		return LoginResponse{}, e
	}
	locks = append(locks, privacyLocks...)
	mac, e := s.loginMAC(r, st.keys.current(), email)
	if e != nil {
		return LoginResponse{}, e
	}
	defer clear(mac)
	var cmd commandRecord
	created := false
	cause, e := foundation.NewCommandsCause(identityKey)
	if e != nil {
		return LoginResponse{}, e
	}
	retainLocal = true
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
			return unavailable(e)
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		old, e := loadCommand(ctx, x, string(digest([]byte(identityKey.Canonical()))), true)
		if e == nil {
			if e = s.verifyLoginCommand(ctx, x, r, email, old); e != nil {
				return e
			}
			cmd = old
			return nil
		}
		if !hasFaultCode(e, foundation.NotFound) {
			return e
		}
		var currentID string
		e = x.QueryRow(ctx, `SELECT coalesce((SELECT id::text FROM agenteam_account.users WHERE email=$1),'')`, email).Scan(&currentID)
		if e != nil {
			return unavailable(e)
		}
		if currentID != uid {
			return fault(foundation.ResourceBusy, nil)
		}
		if exists {
			u, e := loadUser(ctx, x, uid)
			if e != nil {
				return e
			}
			user = u
		}
		if e = s.requireChallenge(ctx, tx, r, email, false); e != nil {
			return e
		}
		_, e = x.Exec(ctx, `INSERT INTO agenteam_account.commands(id,namespace,owner_id,command_key,command_name,identity_digest,semantic_kid,semantic_mac,actor_kind,user_id,session_id,browser_id,browser_expires_at,origin_process_id,password_version,attempt_id,phase,secret_command_digest,secret_write_binding) VALUES($1,'account.login',$2,$3,'login',$4,$5,$6,'browser',$7,$8,$2,$9,$10,$11,$12,'planned',$13,$14)`, cmdID.String(), r.Fields().Browser.ID().String(), string(r.Fields().Key), string(digest([]byte(identityKey.Canonical()))), st.keys.current(), mac, null(uid), nullableSession(exists, sessionID.String()), r.Fields().Browser.ExpiresAt().Time(), st.process.String(), nullableVersion(exists, int64(user.passwordVersion)), attempt.String(), string(secretDigest), string(binding))
		if e != nil {
			return unavailable(e)
		}
		cmd, e = loadCommand(ctx, x, cmdID.String(), false)
		created = e == nil
		return e
	})
	if result.State() == foundation.Unknown {
		cmd, e = s.lookupLogin(ctx, r, email, identityKey)
		if e != nil {
			return LoginResponse{}, confirmationError(result, e)
		}
		created = cmd.id == cmdID && cmd.phase == "planned"
	} else if e = resultError(result); e != nil {
		retainLocal = false
		return LoginResponse{}, e
	}
	if !created {
		retainLocal = false
		return s.loginHistorical(responseCtx, r, email, cmd)
	}
	encoded := dummyPHC
	if exists {
		encoded = user.phc
	}
	ok, e := st.hasher.Verify(ctx, r.Fields().Password, encoded)
	if e != nil {
		return LoginResponse{}, e
	}
	ok = ok && exists
	if !ok {
		e = s.failLogin(ctx, r, email, cmd)
		if hasFaultCode(e, foundation.Unauthenticated) {
			retainLocal = false
		}
		return LoginResponse{}, e
	}
	prepared, e := st.deps.Secrets.PrepareServiceWrite(ctx, write)
	if e != nil {
		return LoginResponse{}, portError(e)
	}
	// Persist the exact prepared reference before collecting its usage plan.
	// This is a technical identity, never a reference or material-read grant.
	result = st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, commandLocks(cmd)); e != nil {
			return unavailable(e)
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		old, e := loadCommand(ctx, x, cmd.id.String(), false)
		if e != nil {
			return e
		}
		if e = s.verifyLoginCommand(ctx, x, r, email, old); e != nil {
			return e
		}
		if old.phase != "planned" || old.plannedRef != "" && old.plannedRef != prepared.Ref().Details().ID.String() {
			return fault(foundation.ResourceBusy, nil)
		}
		_, e = x.Exec(ctx, `UPDATE agenteam_account.commands SET planned_secret_ref=$2 WHERE id=$1`, cmd.id.String(), prepared.Ref().Details().ID.String())
		return portError(e)
	})
	if e = resultError(result); e != nil {
		return LoginResponse{}, e
	}
	refActor, e := serviceActor(identity.SecretService, cmd.id.String())
	if e != nil {
		return LoginResponse{}, e
	}
	usage := sc.UsageRequest{Actor: refActor, Ref: prepared.Ref(), Purpose: sc.System, ReferenceOwner: cmd.id.String(), Action: sc.RetainReferenceUsage, Retain: true}
	deps, e := st.deps.Secrets.DiscoverUsage(ctx, usage)
	if e != nil {
		return LoginResponse{}, portError(e)
	}
	locks = append(commandLocks(cmd), userLock(uid, foundation.Exclusive))
	locks = append(locks, prepared.RequiredLocks()...)
	locks = append(locks, deps.RequiredLocks()...)
	locks = append(locks, privacyLocks...)
	var verifier []byte
	e = cookie.Use(func(raw []byte) error { var e error; verifier, e = sessionVerifier(raw); return e })
	if e != nil {
		return LoginResponse{}, e
	}
	result = st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
			return unavailable(e)
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		current, e := loadCommand(ctx, x, cmd.id.String(), false)
		if e != nil {
			return e
		}
		if e = s.verifyLoginCommand(ctx, x, r, email, current); e != nil {
			return e
		}
		if current.phase != "planned" {
			return fault(foundation.ResourceBusy, nil)
		}
		u, e := loadUser(ctx, x, uid)
		if e != nil {
			return e
		}
		if int64(u.passwordVersion) != cmd.passwordVersion {
			return fault(foundation.Unauthenticated, nil)
		}
		if e = s.requireChallenge(ctx, tx, r, email, true); e != nil {
			return e
		}
		settings, e := loadSettings(ctx, x)
		if e != nil {
			return e
		}
		_, e = x.Exec(ctx, `INSERT INTO agenteam_account.sessions(id,user_id,token_verifier,csrf_kid,issued_at,last_activity_at,idle_seconds,absolute_expires_at) SELECT $1,$2,$3,$4,n,n,$5,n+$6*interval '1 second' FROM (SELECT clock_timestamp() n) z`, cmd.session, uid, verifier, st.keys.current(), int64(settings.SessionIdleSeconds), int64(settings.SessionAbsoluteSeconds))
		if e != nil {
			return unavailable(e)
		}
		_, e = x.Exec(ctx, `UPDATE agenteam_account.commands SET phase='committed',result_version=1,result_code='COMPLETED',response_secret_ref=$2,response_expires_at=clock_timestamp()+interval '5 minutes',completed_at=clock_timestamp() WHERE id=$1`, cmd.id.String(), prepared.Ref().Details().ID.String())
		if e != nil {
			return unavailable(e)
		}
		if _, e = st.deps.Secrets.ApplyServiceWriteInTx(ctx, tx, prepared); e != nil {
			return portError(e)
		}
		if _, e = st.deps.Secrets.ApplyUsageInTx(ctx, tx, usage, deps); e != nil {
			return portError(e)
		}
		if e = s.loginAudit(ctx, tx, x, cmd, ac.Success); e != nil {
			return e
		}
		return s.updateFailures(ctx, x, r, email, true)
	})
	if result.State() != foundation.Committed && result.State() != foundation.Unknown {
		return LoginResponse{}, resultError(result)
	}
	confirmed, e := s.lookupLogin(ctx, r, email, identityKey)
	if e != nil {
		return LoginResponse{}, confirmationError(result, e)
	}
	if confirmed.phase != "committed" {
		if result.State() == foundation.Unknown {
			return LoginResponse{}, confirmationError(result, nil)
		}
		return LoginResponse{}, unavailable(nil)
	}
	retainLocal = false
	return s.openLoginResponse(responseCtx, r, email, confirmed)
}
func nullableSession(present bool, v string) any {
	if !present {
		return nil
	}
	return v
}
func nullableVersion(present bool, v int64) any {
	if !present {
		return nil
	}
	return v
}
func (s *Service) lookupLogin(ctx context.Context, r c.LoginRequest, email string, key foundation.CommandIdentity) (commandRecord, error) {
	st := s.state()
	cause, e := foundation.NewCommandsCause(key)
	if e != nil {
		return commandRecord{}, e
	}
	var out commandRecord
	// The command writer serializes even the absence observation with an old
	// COMMIT. Metadata authorization follows later under its complete plan.
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, []foundation.LockRequest{commandLock(key)}); e != nil {
			return unavailable(e)
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		out, e = loadCommand(ctx, x, string(digest([]byte(key.Canonical()))), true)
		if e != nil {
			return e
		}
		return s.verifyLoginCommand(ctx, x, r, email, out)
	})
	if e = resultError(result); e != nil {
		return commandRecord{}, e
	}
	return out, nil
}
func (s *Service) loginHistorical(ctx context.Context, r c.LoginRequest, email string, cmd commandRecord) (LoginResponse, error) {
	switch cmd.phase {
	case "committed":
		return s.openLoginResponse(ctx, r, email, cmd)
	case "failed":
		return LoginResponse{}, fault(foundation.Code(cmd.resultCode), nil)
	case "planned":
		return LoginResponse{}, fault(foundation.ResourceBusy, nil)
	default:
		return LoginResponse{}, fault(foundation.InvalidState, nil)
	}
}
func (s *Service) failLogin(ctx context.Context, r c.LoginRequest, email string, cmd commandRecord) error {
	st := s.state()
	cause, e := foundation.NewCommandsCause(cmd.identity)
	if e != nil {
		return e
	}
	privacyLocks, e := s.privacyLocks(r, email)
	if e != nil {
		return e
	}
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, append(commandLocks(cmd), privacyLocks...)); e != nil {
			return unavailable(e)
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		old, e := loadCommand(ctx, x, cmd.id.String(), false)
		if e != nil {
			return e
		}
		if e = s.verifyLoginCommand(ctx, x, r, email, old); e != nil {
			return e
		}
		if old.phase != "planned" {
			return fault(foundation.ResourceBusy, nil)
		}
		if e = s.requireChallenge(ctx, tx, r, email, true); e != nil {
			return e
		}
		if e = s.loginAudit(ctx, tx, x, cmd, ac.Denied); e != nil {
			return e
		}
		if e = s.updateFailures(ctx, x, r, email, false); e != nil {
			return e
		}
		_, e = x.Exec(ctx, `UPDATE agenteam_account.commands SET phase='failed',result_code='UNAUTHENTICATED',completed_at=clock_timestamp() WHERE id=$1`, cmd.id.String())
		return portError(e)
	})
	if result.State() == foundation.Unknown {
		old, e := s.lookupLogin(ctx, r, email, cmd.identity)
		if e != nil {
			return confirmationError(result, e)
		}
		if old.phase == "failed" {
			return fault(foundation.Unauthenticated, nil)
		}
		return confirmationError(result, nil)
	}
	if e = resultError(result); e != nil {
		return e
	}
	return fault(foundation.Unauthenticated, nil)
}
func (s *Service) loginAudit(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor, cmd commandRecord, outcome ac.Outcome) error {
	_, e := x.Exec(ctx, `INSERT INTO agenteam_account.auth_attempts(id,kind,command_id,user_id,outcome,phase,completed_at) VALUES($1,'login',$2,$3,$4,'completed',clock_timestamp())`, cmd.attempt, cmd.id.String(), null(cmd.user), string(outcome))
	if e != nil {
		return unavailable(e)
	}
	phase := ac.AccountAuthenticated
	reason := ac.AccountReason("")
	if outcome == ac.Denied {
		phase = ac.AccountRejected
		reason = ac.CredentialsRejected
	}
	session := ""
	if outcome == ac.Success {
		session = cmd.session
	}
	m, e := ac.AccountMetadata(ac.AccountLogin, ac.AccountMetadataFields{AttemptID: cmd.attempt, UserID: cmd.user, SessionID: session, Version: 1, Phase: phase, Reason: reason})
	if e != nil {
		return e
	}
	actor, e := serviceActor(identity.AccountAuth, cmd.attempt)
	if e != nil {
		return e
	}
	resource, e := ac.NewResource(ac.AccountAttemptResource, cmd.attempt)
	if e != nil {
		return e
	}
	entry, e := ac.NewEntry(ac.EntryFields{Scope: identity.SystemScope(), Actor: actor, Action: ac.AccountLogin, Outcome: outcome, Resource: resource, Metadata: m})
	if e != nil {
		return e
	}
	key, e := ac.NewAppendKey(ac.AccountProducer, cmd.attempt, 0)
	if e != nil {
		return e
	}
	_, e = s.state().deps.Audit.AppendInTx(ctx, tx, entry, key)
	return portError(e)
}

type privacyKey struct {
	kid         string
	subject, ip []byte
}

func (s *Service) privacyKeys(r c.LoginRequest, email string) ([]privacyKey, error) {
	out := make([]privacyKey, 0, len(s.state().keys.data().keys))
	for kid := range s.state().keys.data().keys {
		subject, e := s.state().keys.mac(kid, "privacy-counter-v1", []byte("subject"), []byte(email))
		if e != nil {
			return nil, e
		}
		ip, e := s.state().keys.mac(kid, "privacy-counter-v1", []byte("ip"), []byte(r.Fields().ClientIP.Unmap().String()))
		if e != nil {
			return nil, e
		}
		out = append(out, privacyKey{kid, subject, ip})
	}
	return out, nil
}
func (s *Service) requireChallenge(ctx context.Context, tx foundation.Tx, r c.LoginRequest, email string, consume bool) error {
	x, e := s.state().store.InTx(tx)
	if e != nil {
		return unavailable(e)
	}
	settings, e := loadSettings(ctx, x)
	if e != nil {
		return e
	}
	keys, e := s.privacyKeys(r, email)
	if e != nil {
		return e
	}
	var subjects, ips int64
	for _, k := range keys {
		var a, b int64
		e = x.QueryRow(ctx, `SELECT coalesce(sum(failures) FILTER(WHERE kind='subject'),0)::bigint,coalesce(sum(failures) FILTER(WHERE kind='ip'),0)::bigint FROM agenteam_account.auth_failures WHERE kid=$1 AND expires_at>clock_timestamp() AND ((kind='subject' AND digest=$2) OR (kind='ip' AND digest=$3))`, k.kid, k.subject, k.ip).Scan(&a, &b)
		if e != nil {
			return unavailable(e)
		}
		subjects += a
		ips += b
	}
	if subjects < int64(settings.ChallengeAfterFailures) && ips < 10*int64(settings.ChallengeAfterFailures) {
		return nil
	}
	if nilPort(s.state().deps.Challenges) {
		return fault(foundation.ChallengeRequired, nil)
	}
	// Only the final transaction consumes a one-use proof. The preflight has
	// not authenticated the password and must not spend a proof prematurely.
	if !consume {
		return portError(s.state().deps.Challenges.PreviewLoginInTx(ctx, tx, r))
	}
	return portError(s.state().deps.Challenges.CheckLoginInTx(ctx, tx, r))
}
func (s *Service) updateFailures(ctx context.Context, x postgres.SQLExecutor, r c.LoginRequest, email string, success bool) error {
	keys, e := s.privacyKeys(r, email)
	if e != nil {
		return e
	}
	if success {
		for _, k := range keys {
			if _, e = x.Exec(ctx, `DELETE FROM agenteam_account.auth_failures WHERE kind='subject' AND kid=$1 AND digest=$2`, k.kid, k.subject); e != nil {
				return unavailable(e)
			}
		}
		return nil
	}
	for _, k := range keys {
		if k.kid != s.state().keys.current() {
			continue
		}
		for _, v := range []struct {
			kind    string
			hash    []byte
			seconds int64
		}{{"subject", k.subject, 86400}, {"ip", k.ip, 900}} {
			_, e = x.Exec(ctx, `INSERT INTO agenteam_account.auth_failures(kind,kid,digest,failures,window_start,expires_at) VALUES($1,$2,$3,1,clock_timestamp(),clock_timestamp()+$4*interval '1 second') ON CONFLICT(kind,kid,digest) DO UPDATE SET failures=CASE WHEN agenteam_account.auth_failures.expires_at<=clock_timestamp() THEN 1 ELSE LEAST(agenteam_account.auth_failures.failures+1,1000000) END,window_start=CASE WHEN agenteam_account.auth_failures.expires_at<=clock_timestamp() THEN clock_timestamp() ELSE agenteam_account.auth_failures.window_start END,expires_at=CASE WHEN agenteam_account.auth_failures.expires_at<=clock_timestamp() OR $1='subject' THEN clock_timestamp()+$4*interval '1 second' ELSE agenteam_account.auth_failures.expires_at END`, v.kind, k.kid, v.hash, v.seconds)
			if e != nil {
				return unavailable(e)
			}
		}
	}
	return nil
}

// LookupLogin verifies the same full request; it never exposes another
// browser's receipt or returns a cookie solely because a command exists.
func (s *Service) LookupLogin(ctx context.Context, r c.LoginRequest) (LoginResponse, error) {
	responseCtx := ctx
	if r.Validate() != nil {
		return LoginResponse{}, invalid()
	}
	op, e := s.begin(ctx, false)
	if e != nil {
		return LoginResponse{}, e
	}
	defer s.finish(op)
	ctx = op.ctx
	if e = s.requireBrowser(ctx, r.Fields().Browser); e != nil {
		return LoginResponse{}, e
	}
	email, e := NormalizeEmail(r.Fields().Email)
	if e != nil {
		return LoginResponse{}, e
	}
	key, e := loginIdentity(r)
	if e != nil {
		return LoginResponse{}, e
	}
	cmd, e := s.lookupLogin(ctx, r, email, key)
	if e != nil {
		return LoginResponse{}, e
	}
	return s.loginHistorical(responseCtx, r, email, cmd)
}

func (s *Service) privacyLocks(r c.LoginRequest, email string) ([]foundation.LockRequest, error) {
	keys, e := s.privacyKeys(r, email)
	if e != nil {
		return nil, e
	}
	out := make([]foundation.LockRequest, 0, 2*len(keys)+1)
	out = append(out, challengeLock())
	for _, k := range keys {
		out = append(out, failureLock("subject", k.kid, k.subject), failureLock("ip", k.kid, k.ip))
	}
	return out, nil
}
func failureLock(kind, kid string, value []byte) foundation.LockRequest {
	return recordLock("account-failure-" + kind + ":" + string(digest(append([]byte(kid+"\x00"), value...))))
}
