package account

import (
	"context"
	"errors"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

// RequestPasswordReset never looks up an account or prepares a Secret. Every
// legal email has the same acceptance transaction, bounded queue and IP limit.
func (s *Service) RequestPasswordReset(ctx context.Context, r c.ResetRequest) (c.ResetAccepted, error) {
	if r.Validate() != nil {
		return c.ResetAccepted{}, invalid()
	}
	op, e := s.begin(ctx, false)
	if e != nil {
		return c.ResetAccepted{}, e
	}
	defer s.finish(op)
	ctx = op.ctx
	st := s.state()
	f := r.Fields()
	if e = s.requireBrowser(ctx, f.Browser); e != nil {
		return c.ResetAccepted{}, e
	}
	email, e := NormalizeEmail(f.Email)
	if e != nil {
		return c.ResetAccepted{}, e
	}
	key, e := foundation.NewCommandIdentity("account.reset", []string{f.Browser.ID().String()}, "reset-request", f.Key)
	if e != nil {
		return c.ResetAccepted{}, e
	}
	macFor := func(kid string) ([]byte, error) {
		return st.keys.mac(kid, "command-v1", []byte("reset-request"), []byte(f.Browser.ID().String()), []byte(email))
	}
	id, e := foundation.NewID[c.Command]()
	if e != nil {
		return c.ResetAccepted{}, unavailable(e)
	}
	attempt, e := foundation.NewID[c.Attempt]()
	if e != nil {
		return c.ResetAccepted{}, unavailable(e)
	}
	type counter struct {
		kid  string
		hash []byte
	}
	var counters []counter
	locks := []foundation.LockRequest{commandLock(key), configLock("account-directory", foundation.Shared), configLock("account-security", foundation.Shared), configLock("account-mail", foundation.Exclusive), recordLock(id.String()), recordLock(attempt.String())}
	for kid := range st.keys.data().keys {
		h, e := st.keys.mac(kid, "privacy-counter-v1", []byte("reset-ip"), []byte(f.ClientIP.Unmap().String()))
		if e != nil {
			return c.ResetAccepted{}, e
		}
		counters = append(counters, counter{kid, h})
		locks = append(locks, failureLock("reset", kid, h))
	}
	cause, e := foundation.NewCommandsCause(key)
	if e != nil {
		return c.ResetAccepted{}, e
	}
	mac, e := macFor(st.keys.current())
	if e != nil {
		return c.ResetAccepted{}, e
	}
	defer clear(mac)
	out := c.ResetAccepted{Accepted: true, DeliveryChannel: "backend_log"}
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
			return unavailable(e)
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		if e = s.validateBrowserInTx(ctx, x, f.Browser); e != nil {
			return e
		}
		var configured bool
		if e = x.QueryRow(ctx, `SELECT configured FROM agenteam_account.smtp_settings WHERE singleton`).Scan(&configured); e != nil {
			return unavailable(e)
		}
		if configured {
			out.DeliveryChannel = "smtp"
		}
		old, e := loadCommand(ctx, x, string(digest([]byte(key.Canonical()))), true)
		if e == nil {
			if e = s.checkCommand(old, key, macFor); e != nil {
				return e
			}
			if old.phase != "committed" {
				return fault(foundation.InvalidState, nil)
			}
			return nil
		}
		if !hasFaultCode(e, foundation.NotFound) {
			return e
		}
		var requests int64
		for _, c := range counters {
			var n int64
			if e = x.QueryRow(ctx, `SELECT coalesce(sum(failures),0)::bigint FROM agenteam_account.auth_failures WHERE kind='reset' AND kid=$1 AND digest=$2 AND expires_at>clock_timestamp()`, c.kid, c.hash).Scan(&n); e != nil {
				return unavailable(e)
			}
			requests += n
		}
		if requests >= 30 {
			return fault(foundation.RateLimited, nil)
		}
		var queued int
		if e = x.QueryRow(ctx, `SELECT count(*)::int FROM agenteam_account.reset_requests WHERE phase='accepted'`).Scan(&queued); e != nil {
			return unavailable(e)
		}
		if queued >= 10000 {
			return fault(foundation.RateLimited, nil)
		}
		for _, c := range counters {
			if c.kid != st.keys.current() {
				continue
			}
			if _, e = x.Exec(ctx, `INSERT INTO agenteam_account.auth_failures(kind,kid,digest,failures,window_start,expires_at) VALUES('reset',$1,$2,1,clock_timestamp(),clock_timestamp()+interval '1 hour') ON CONFLICT(kind,kid,digest) DO UPDATE SET failures=CASE WHEN agenteam_account.auth_failures.expires_at<=clock_timestamp() THEN 1 ELSE agenteam_account.auth_failures.failures+1 END,window_start=CASE WHEN agenteam_account.auth_failures.expires_at<=clock_timestamp() THEN clock_timestamp() ELSE agenteam_account.auth_failures.window_start END,expires_at=CASE WHEN agenteam_account.auth_failures.expires_at<=clock_timestamp() THEN clock_timestamp()+interval '1 hour' ELSE agenteam_account.auth_failures.expires_at END`, c.kid, c.hash); e != nil {
				return unavailable(e)
			}
		}
		if _, e = x.Exec(ctx, `INSERT INTO agenteam_account.commands(id,namespace,owner_id,command_key,command_name,identity_digest,semantic_kid,semantic_mac,actor_kind,browser_id,browser_expires_at,origin_process_id,attempt_id,phase,result_code,result_version,completed_at) VALUES($1,'account.reset',$2,$3,'reset-request',$4,$5,$6,'browser',$2,$7,$8,$9,'committed','COMPLETED',1,clock_timestamp())`, id.String(), f.Browser.ID().String(), string(f.Key), string(digest([]byte(key.Canonical()))), st.keys.current(), mac, f.Browser.ExpiresAt().Time(), st.process.String(), attempt.String()); e != nil {
			return unavailable(e)
		}
		if _, e = x.Exec(ctx, `INSERT INTO agenteam_account.reset_requests(id,command_id,canonical_email,browser_id,phase) VALUES($1,$1,$2,$3,'accepted')`, id.String(), email, f.Browser.ID().String()); e != nil {
			return unavailable(e)
		}
		if _, e = x.Exec(ctx, `INSERT INTO agenteam_account.auth_attempts(id,kind,command_id,outcome,phase,completed_at) VALUES($1,'reset_request',$2,'success','completed',clock_timestamp())`, attempt.String(), id.String()); e != nil {
			return unavailable(e)
		}
		actor, e := serviceActor(identity.AccountAuth, attempt.String())
		if e != nil {
			return e
		}
		return s.mutationAudit(ctx, tx, actor, ac.AccountPasswordResetRequest, ac.AccountAttemptResource, attempt.String(), attempt.String(), ac.AccountMetadataFields{AttemptID: attempt.String(), Version: 1, Phase: ac.AccountAccepted})
	})
	if result.State() == foundation.Unknown {
		cmd, e := s.lookupMutation(ctx, key, macFor, identity.Actor{}, f.Browser, false)
		if e != nil || cmd.phase != "committed" {
			return c.ResetAccepted{}, confirmationError(result, e)
		}
	} else if e = resultError(result); e != nil {
		return c.ResetAccepted{}, e
	}
	return out, nil
}
func processedReset(ctx context.Context, x postgres.SQLExecutor, id string) error {
	_, e := x.Exec(ctx, `UPDATE agenteam_account.reset_requests SET phase='processed',completed_at=clock_timestamp() WHERE command_id=$1 AND phase='accepted'`, id)
	return portError(e)
}

// processReset is called only by registered, bounded recovery work. The public
// accepting request has already returned, without waiting for this path.
func (s *Service) processReset(ctx context.Context, id string) error {
	st := s.state()
	cmd, e := loadCommand(ctx, st.store, id, false)
	if e != nil {
		return e
	}
	if cmd.name != "reset-request" || cmd.phase != "committed" {
		return fault(foundation.InvalidState, nil)
	}
	candidate, e := foundation.NewID[c.PasswordReset]()
	if e != nil {
		return unavailable(e)
	}
	cause, e := recoveryCause("reset-plan")
	if e != nil {
		return e
	}
	var link linkRecord
	var exists, done bool
	r := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		locks := append(commandLocks(cmd), configLock("account-directory", foundation.Exclusive), configLock("account-mail", foundation.Exclusive))
		if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
			return unavailable(e)
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		current, e := loadCommand(ctx, x, id, false)
		if e != nil {
			return e
		}
		var email, phase string
		if e = x.QueryRow(ctx, `SELECT canonical_email,phase FROM agenteam_account.reset_requests WHERE command_id=$1`, id).Scan(&email, &phase); e != nil {
			return unavailable(e)
		}
		if phase != "accepted" {
			done = true
			return nil
		}
		var user string
		var pwd int64
		e = x.QueryRow(ctx, `SELECT id::text,password_version FROM agenteam_account.users WHERE email=$1`, email).Scan(&user, &pwd)
		if errors.Is(e, pgx.ErrNoRows) {
			done = true
			return processedReset(ctx, x, id)
		}
		if e != nil {
			return unavailable(e)
		}
		var existing string
		e = x.QueryRow(ctx, `SELECT id::text FROM agenteam_account.password_resets WHERE user_id=$1`, user).Scan(&existing)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return unavailable(e)
		}
		resource := candidate.String()
		var retired bool
		if current.resource != "" {
			if e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.material_cleanup WHERE owner_kind='password_reset' AND owner_id=$1)`, current.resource).Scan(&retired); e != nil {
				return unavailable(e)
			}
		}
		if current.resource != "" && !retired && current.user == user && current.passwordVersion == pwd {
			resource = current.resource
		}
		if existing != "" {
			resource = existing
			link, e = loadLink(ctx, x, c.PasswordResetToken, existing)
			if e != nil {
				return e
			}
			exists = true
		}
		if _, e = x.Exec(ctx, `UPDATE agenteam_account.commands SET user_id=$2,password_version=$3,resource_id=$4,planned_secret_ref=CASE WHEN resource_id IS NOT DISTINCT FROM $4::uuid THEN planned_secret_ref END,secret_write_binding=CASE WHEN resource_id IS NOT DISTINCT FROM $4::uuid THEN secret_write_binding END,secret_command_digest=CASE WHEN resource_id IS NOT DISTINCT FROM $4::uuid THEN secret_command_digest END WHERE id=$1`, id, user, pwd, resource); e != nil {
			return unavailable(e)
		}
		cmd, e = loadCommand(ctx, x, id, false)
		return e
	})
	if e = resultError(r); e != nil {
		return e
	}
	if done {
		return nil
	}
	if exists && !link.live {
		if e = s.expireLink(ctx, c.PasswordResetToken, link.id); e != nil {
			return e
		}
		return fault(foundation.ResourceBusy, nil)
	}
	// Current semantic HMAC is already a committed accepted command. The worker
	// compares that exact digest, never reconstructs a browser credential.
	macFor := func(kid string) ([]byte, error) {
		if kid != cmd.kid {
			return nil, fault(foundation.Forbidden, nil)
		}
		return append([]byte(nil), cmd.mac...), nil
	}
	var prepared secret.PreparedServiceWrite
	var usage sc.UsageRequest
	var usagePlan sc.UsageDependencies
	var verifier []byte
	if !exists {
		prepared, usage, usagePlan, verifier, e = s.prepareLinkMaterial(ctx, cmd, c.PasswordResetToken, macFor, identity.Actor{}, c.BrowserIdentity{})
		if e != nil {
			return e
		}
	}
	actor, evt, plan, e := s.prepareDelivery(ctx, cmd)
	if e != nil {
		return e
	}
	locks := append(commandLocks(cmd), configLock("account-directory", foundation.Exclusive), configLock("account-mail", foundation.Exclusive), userLock(cmd.user, foundation.Exclusive))
	locks = append(locks, plan.Locks()...)
	if !exists {
		locks = append(locks, prepared.RequiredLocks()...)
		locks = append(locks, usagePlan.RequiredLocks()...)
	}
	r = st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
			return unavailable(e)
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		current, e := loadCommand(ctx, x, id, false)
		if e != nil {
			return e
		}
		if commandMapping(current) != commandMapping(cmd) {
			return fault(foundation.ResourceBusy, nil)
		}
		var pending bool
		if e = x.QueryRow(ctx, `SELECT phase='accepted' FROM agenteam_account.reset_requests WHERE command_id=$1`, id).Scan(&pending); e != nil {
			return unavailable(e)
		}
		if !pending {
			return nil
		}
		u, e := loadUser(ctx, x, cmd.user)
		if e != nil {
			return e
		}
		if int64(u.passwordVersion) != cmd.passwordVersion {
			return fault(foundation.ResourceBusy, nil)
		}
		if exists {
			now, e := loadLink(ctx, x, c.PasswordResetToken, cmd.resource)
			if e != nil {
				return e
			}
			if !now.live || now.ref != link.ref {
				return fault(foundation.ResourceBusy, nil)
			}
			var ready bool
			if e = x.QueryRow(ctx, `SELECT last_delivery_at IS NULL OR last_delivery_at<=clock_timestamp()-interval '60 seconds' FROM agenteam_account.password_resets WHERE id=$1`, cmd.resource).Scan(&ready); e != nil {
				return unavailable(e)
			}
			if !ready {
				return processedReset(ctx, x, id)
			}
		} else {
			if current.plannedRef != prepared.Ref().Details().ID.String() {
				return fault(foundation.ResourceBusy, nil)
			}
			settings, e := loadSettings(ctx, x)
			if e != nil {
				return e
			}
			if _, e = x.Exec(ctx, `INSERT INTO agenteam_account.password_resets(id,user_id,token_verifier,material_ref,version,password_version,created_at,expires_at) SELECT $1,$2,$3,$4,1,$5,n,n+$6*interval '1 second' FROM (SELECT clock_timestamp() n) z`, cmd.resource, cmd.user, verifier, prepared.Ref().Details().ID.String(), cmd.passwordVersion, int64(settings.PasswordResetSeconds)); e != nil {
				return unavailable(e)
			}
			if _, e = st.deps.Secrets.ApplyServiceWriteInTx(ctx, tx, prepared); e != nil {
				return portError(e)
			}
			if _, e = st.deps.Secrets.ApplyUsageInTx(ctx, tx, usage, usagePlan); e != nil {
				return portError(e)
			}
		}
		if e = s.insertDelivery(ctx, x, cmd); e != nil {
			return e
		}
		if _, e = st.deps.Events.AppendEventInTx(ctx, tx, actor, evt, plan); e != nil {
			return portError(e)
		}
		if _, e = x.Exec(ctx, `UPDATE agenteam_account.password_resets SET last_delivery_at=clock_timestamp() WHERE id=$1`, cmd.resource); e != nil {
			return unavailable(e)
		}
		return processedReset(ctx, x, id)
	})
	return resultError(r)
}

// The asynchronous routing address is not a permanent receipt. Once its
// browser and short recovery window have expired, retain only the original
// command HMAC/IDs and privacy-safe attempt; no delivery fact needs this email.
func (s *Service) forgetResetRecipient(ctx context.Context, id string) error {
	st := s.state()
	cmd, e := loadCommand(ctx, st.store, id, false)
	if e != nil {
		return e
	}
	cause, e := recoveryCause("reset-recipient-expire")
	if e != nil {
		return e
	}
	r := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, commandLocks(cmd)); e != nil {
			return unavailable(e)
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		_, e = x.Exec(ctx, `DELETE FROM agenteam_account.reset_requests r USING agenteam_account.commands c WHERE r.command_id=c.id AND r.id=$1 AND r.phase='processed' AND r.completed_at<clock_timestamp()-interval '24 hours' AND c.browser_expires_at<clock_timestamp()`, id)
		return portError(e)
	})
	return resultError(r)
}
