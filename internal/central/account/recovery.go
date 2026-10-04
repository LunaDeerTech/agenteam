package account

import (
	"context"
	"errors"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

// RecoveryStatus reports bounded work, not the absence of all future work.
// Pending includes protected live/unknown owners. Neither elapsed time nor a
// cancelled request proves that its material reader has actually returned.
type RecoveryStatus struct {
	Examined int `json:"examined"`
	Advanced int `json:"advanced"`
	Pending  int `json:"pending"`
}

// Recover joins no foreign work and performs no external delivery. It uses
// exact local completion or the trusted ProcessAuthority's death proof, and
// serializes every decision with the original database writer.
func (s *Service) Recover(ctx context.Context) (RecoveryStatus, error) {
	op, e := s.begin(ctx, true)
	if e != nil {
		return RecoveryStatus{}, e
	}
	defer s.finish(op)
	ctx = op.ctx
	var status RecoveryStatus
	var first error
	first = s.recoverLocal(ctx, &status)
	accept := func(e error) {
		status.Examined++
		if e == nil {
			status.Advanced++
			return
		}
		status.Pending++
		if !hasFaultCode(e, foundation.ResourceBusy) && first == nil {
			first = e
		}
	}
	accept(s.recoverBootstrap(ctx))
	if e := s.retireMutations(ctx, &status); e != nil && first == nil {
		first = e
	}
	for _, stage := range []struct {
		table, pass, where string
		work               func(context.Context, string) error
	}{
		{"reset_requests", "pass", "phase='accepted'", s.processReset},
		{"reset_requests", "pass", "phase='processed' AND completed_at<clock_timestamp()-interval '24 hours' AND EXISTS(SELECT 1 FROM agenteam_account.commands c WHERE c.id=agenteam_account.reset_requests.command_id AND c.browser_expires_at<clock_timestamp())", s.forgetResetRecipient},
		{"commands", "cleanup_pass", "command_name IN ('invite-create','password-change','reset-complete') AND phase='planned'", s.recoverMutation},
		{"commands", "cleanup_pass", "EXISTS(SELECT 1 FROM agenteam_account.delivery_intents i LEFT JOIN agenteam_account.mail_jobs j ON j.intent_id=i.id WHERE i.id=agenteam_account.commands.id AND j.id IS NULL)", s.reconcileDelivery},
		{"commands", "cleanup_pass", "command_name='invite-create' AND EXISTS(SELECT 1 FROM agenteam_account.invitations i WHERE i.id=agenteam_account.commands.resource_id AND i.expires_at<=clock_timestamp())", func(ctx context.Context, id string) error { return s.expireCommandLink(ctx, id, c.InvitationToken) }},
		{"commands", "cleanup_pass", "command_name='reset-request' AND EXISTS(SELECT 1 FROM agenteam_account.password_resets r JOIN agenteam_account.users u ON u.id=r.user_id WHERE r.id=agenteam_account.commands.resource_id AND (r.expires_at<=clock_timestamp() OR r.password_version<>u.password_version))", func(ctx context.Context, id string) error { return s.expireCommandLink(ctx, id, c.PasswordResetToken) }},
		{"material_cleanup", "pass", "owner_kind IN ('invitation','password_reset') AND phase<>'completed'", s.cleanLinkMaterial},
		{"response_plans", "pass", "true", s.recoverResponse},
		{"commands", "cleanup_pass", "command_name='login' AND phase='planned'", s.recoverLogin},
		{"commands", "cleanup_pass", "command_name='login' AND phase='committed' AND response_secret_ref IS NOT NULL AND (response_expires_at<=clock_timestamp() OR EXISTS(SELECT 1 FROM agenteam_account.sessions s JOIN agenteam_account.users u ON u.id=s.user_id WHERE s.id=agenteam_account.commands.session_id AND (s.revoked_at IS NOT NULL OR u.password_version<>agenteam_account.commands.password_version)))", s.expireResponse},
		{"material_cleanup", "pass", "owner_kind='login_response' AND phase<>'completed'", s.cleanMaterial},
		{"commands", "cleanup_pass", "actor_kind='browser' AND command_name='login' AND phase<>'planned' AND response_secret_ref IS NULL AND completed_at<clock_timestamp()-interval '24 hours' AND browser_expires_at<clock_timestamp()", s.forgetAnonymousLogin},
	} {
		if ctx.Err() != nil {
			return status, unavailable(ctx.Err())
		}
		ids, err := s.recoveryBatch(ctx, stage.table, stage.pass, stage.where)
		if err != nil {
			return status, err
		}
		for _, id := range ids {
			if ctx.Err() != nil {
				return status, unavailable(ctx.Err())
			}
			item, cancel := context.WithTimeout(ctx, 2*time.Second)
			err := stage.work(item, id)
			if ctx.Err() == nil && item.Err() != nil && (errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)) {
				err = fault(foundation.ResourceBusy, err)
			}
			cancel()
			accept(err)
		}
	}
	if ctx.Err() != nil {
		return status, unavailable(ctx.Err())
	}
	if e := s.recoverExpiredCounters(ctx); e != nil && first == nil {
		first = e
	}
	if e := s.recoverExpiredChallenges(ctx); e != nil && first == nil {
		first = e
	}
	return status, first
}

func (s *Service) recoverExpiredChallenges(ctx context.Context) error {
	cause, e := recoveryCause("challenge-expire")
	if e != nil {
		return e
	}
	r := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := s.state().store.AcquireAll(ctx, tx, []foundation.LockRequest{challengeLock()}); e != nil {
			return unavailable(e)
		}
		x, e := s.state().store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		_, e = x.Exec(ctx, `DELETE FROM agenteam_account.challenges WHERE id IN (SELECT id FROM agenteam_account.challenges WHERE expires_at<=clock_timestamp() ORDER BY expires_at,id LIMIT 100)`)
		return portError(e)
	})
	return resultError(r)
}

// Expired privacy counters have no I/O owner. Lock the exact same keys as
// login, recheck expiration in the mutation, and remove a bounded oldest batch.
func (s *Service) recoverExpiredCounters(ctx context.Context) error {
	type counter struct {
		kind, kid string
		digest    []byte
	}
	st := s.state()
	rows, e := st.store.Query(ctx, `SELECT kind,kid,digest FROM agenteam_account.auth_failures WHERE expires_at<=clock_timestamp() ORDER BY expires_at,kind,kid,digest LIMIT 100`)
	if e != nil {
		return unavailable(e)
	}
	var batch []counter
	for rows.Next() {
		var c counter
		if e = rows.Scan(&c.kind, &c.kid, &c.digest); e != nil {
			break
		}
		batch = append(batch, c)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return unavailable(e)
	}
	if len(batch) == 0 {
		return nil
	}
	locks := []foundation.LockRequest{configLock("account-security", foundation.Shared)}
	for _, c := range batch {
		locks = append(locks, failureLock(c.kind, c.kid, c.digest))
	}
	cause, e := recoveryCause("privacy-expire")
	if e != nil {
		return e
	}
	r := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
			return unavailable(e)
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		for _, c := range batch {
			if _, e = x.Exec(ctx, `DELETE FROM agenteam_account.auth_failures WHERE kind=$1 AND kid=$2 AND digest=$3 AND expires_at<=clock_timestamp()`, c.kind, c.kid, c.digest); e != nil {
				return unavailable(e)
			}
		}
		return nil
	})
	return resultError(r)
}

func (s *Service) forgetAnonymousLogin(ctx context.Context, id string) error {
	st := s.state()
	cmd, e := loadCommand(ctx, st.store, id, false)
	if e != nil {
		return e
	}
	cause, e := recoveryCause("forget-anonymous")
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
		var eligible bool
		e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.commands c WHERE c.id=$1 AND actor_kind='browser' AND command_name='login' AND phase<>'planned' AND response_secret_ref IS NULL AND completed_at<clock_timestamp()-interval '24 hours' AND browser_expires_at<clock_timestamp() AND NOT EXISTS(SELECT 1 FROM agenteam_account.response_plans p WHERE p.command_id=c.id) AND NOT EXISTS(SELECT 1 FROM agenteam_account.material_cleanup m WHERE m.owner_kind='login_response' AND m.owner_id=c.id AND m.phase<>'completed'))`, id).Scan(&eligible)
		if e != nil {
			return unavailable(e)
		}
		if !eligible {
			return fault(foundation.ResourceBusy, nil)
		}
		if _, e = x.Exec(ctx, `DELETE FROM agenteam_account.auth_attempts WHERE command_id=$1 AND kind IN ('login','response_read') AND phase IN ('completed','released')`, id); e != nil {
			return unavailable(e)
		}
		_, e = x.Exec(ctx, `DELETE FROM agenteam_account.commands WHERE id=$1`, id)
		return portError(e)
	})
	return resultError(r)
}

// Only compiled table/column/predicate constants reach this helper. Advancing
// the durable pass before trying an item prevents a protected prefix from
// starving independent items, including across worker restarts.
func (s *Service) recoveryBatch(ctx context.Context, table, pass, predicate string) ([]string, error) {
	cause, e := recoveryCause("batch")
	if e != nil {
		return nil, e
	}
	var ids []string
	r := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := s.state().store.AcquireAll(ctx, tx, []foundation.LockRequest{configLock("account-recovery", foundation.Exclusive)}); e != nil {
			return unavailable(e)
		}
		x, e := s.state().store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		rows, e := x.Query(ctx, `WITH batch AS (SELECT id FROM agenteam_account.`+table+` WHERE `+predicate+` ORDER BY `+pass+`,id LIMIT 100 FOR UPDATE), bumped AS (UPDATE agenteam_account.`+table+` t SET `+pass+`=t.`+pass+`+1 FROM batch WHERE t.id=batch.id RETURNING t.id,t.`+pass+` AS next_pass) SELECT id::text FROM bumped ORDER BY next_pass,id`)
		if e != nil {
			return unavailable(e)
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				return unavailable(e)
			}
			ids = append(ids, id)
		}
		return portError(rows.Err())
	})
	return ids, resultError(r)
}

func (s *Service) stopped(ctx context.Context, process string, local *operation) error {
	if process == s.state().process.String() {
		if operationJoined(local) {
			return nil
		}
		return fault(foundation.ResourceBusy, nil)
	}
	id, e := parseID[c.Process](process)
	if e != nil {
		return e
	}
	return portError(s.state().deps.Processes.ConfirmStopped(ctx, id))
}
func (s *Service) recoverLogin(ctx context.Context, id string) error {
	st := s.state()
	cmd, e := loadCommand(ctx, st.store, id, false)
	if e != nil {
		return e
	}
	st.mu.Lock()
	tracked := st.logins[id]
	st.mu.Unlock()
	var local *operation
	if tracked != nil {
		local = tracked.op
	}
	if e = s.stopped(ctx, cmd.process, local); e != nil {
		return e
	}
	cause, e := recoveryCause("login-cancel")
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
		current, e := loadCommand(ctx, x, id, false)
		if e != nil {
			return e
		}
		if commandMapping(current) != commandMapping(cmd) {
			return fault(foundation.ResourceBusy, nil)
		}
		_, e = x.Exec(ctx, `UPDATE agenteam_account.commands SET phase='cancelled',result_code='INVALID_STATE',completed_at=clock_timestamp(),planned_secret_ref=NULL,secret_write_binding=NULL WHERE id=$1 AND phase='planned'`, id)
		return portError(e)
	})
	if e = resultError(r); e == nil {
		st.mu.Lock()
		delete(st.logins, id)
		st.mu.Unlock()
	}
	return e
}
func (s *Service) recoverResponse(ctx context.Context, id string) error {
	st := s.state()
	p, e := loadResponsePlan(ctx, st.store, id)
	if e != nil {
		return e
	}
	st.mu.Lock()
	v := st.responses[id]
	st.mu.Unlock()
	var local *operation
	if v != nil {
		if responseMapping(v.plan) != responseMapping(p) {
			return fault(foundation.ResourceBusy, nil)
		}
		local = v.op
	}
	if !p.joined {
		if e = s.stopped(ctx, p.process, local); e != nil {
			return e
		}
	}
	cmd, e := loadCommand(ctx, st.store, p.command, false)
	if e != nil {
		return e
	}
	cause, e := recoveryCause("response-check")
	if e != nil {
		return e
	}
	active := false
	r := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, append(commandLocks(cmd), recordLock(id))); e != nil {
			return unavailable(e)
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		current, e := loadResponsePlan(ctx, x, id)
		if e != nil {
			return e
		}
		if responseMapping(current) != responseMapping(p) {
			return fault(foundation.ResourceBusy, nil)
		}
		var phase, process, lease string
		var fence int64
		e = x.QueryRow(ctx, `SELECT phase,process_id::text,lease_id::text,fence FROM agenteam_account.auth_attempts WHERE id=$1 AND kind='response_read'`, id).Scan(&phase, &process, &lease, &fence)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return unavailable(e)
		}
		if e == nil {
			if process != p.process || lease != p.lease || fence != p.fence {
				return fault(foundation.Forbidden, nil)
			}
			if phase != "released" {
				active = true
				return nil
			}
			if !current.joined {
				return fault(foundation.InvalidState, nil)
			}
			if _, e = x.Exec(ctx, `DELETE FROM agenteam_account.auth_attempts WHERE id=$1 AND kind='response_read' AND phase='released'`, id); e != nil {
				return unavailable(e)
			}
		}
		// An absent attempt after the original writer lock cannot have a lease:
		// the attempt and that exact lease are always committed together.
		_, e = x.Exec(ctx, `DELETE FROM agenteam_account.response_plans WHERE id=$1`, id)
		return portError(e)
	})
	if e = resultError(r); e != nil {
		return e
	}
	if active {
		return s.releaseResponse(ctx, p)
	}
	st.mu.Lock()
	delete(st.responses, id)
	st.mu.Unlock()
	return nil
}

func cleanupIdentity(owner, id string) (foundation.CommandIdentity, error) {
	return foundation.NewCommandIdentity("secret.account", []string{owner}, "delete", foundation.IdempotencyKey(id))
}
func responseReference(cmd commandRecord, ref sc.CredentialRef) (sc.UsageRequest, error) {
	a, e := serviceActor(identity.SecretService, cmd.id.String())
	if e != nil {
		return sc.UsageRequest{}, e
	}
	return sc.UsageRequest{Actor: a, Ref: ref, Purpose: sc.System, ReferenceOwner: cmd.id.String(), Action: sc.ReleaseReferenceUsage}, nil
}
func (s *Service) expireResponse(ctx context.Context, id string) error {
	st := s.state()
	cmd, e := loadCommand(ctx, st.store, id, false)
	if e != nil {
		return e
	}
	if cmd.responseRef == "" {
		return nil
	}
	refID, e := parseID[sc.Credential](cmd.responseRef)
	if e != nil {
		return e
	}
	ref, e := sc.NewCredentialRef(refID, identity.SystemScope())
	if e != nil {
		return e
	}
	request, e := responseReference(cmd, ref)
	if e != nil {
		return e
	}
	plan, e := st.deps.Secrets.DiscoverUsage(ctx, request)
	if e != nil {
		return portError(e)
	}
	cleanupID, e := foundation.NewID[struct{}]()
	if e != nil {
		return unavailable(e)
	}
	identityKey, e := cleanupIdentity(id, cleanupID.String())
	if e != nil {
		return e
	}
	dg, e := canonicalCommandDigest(identityKey)
	if e != nil {
		return e
	}
	cause, e := recoveryCause("response-expire")
	if e != nil {
		return e
	}
	r := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		locks := append(plan.RequiredLocks(), recordLock(cleanupID.String()))
		if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
			return unavailable(e)
		}
		if _, e := st.deps.Secrets.ApplyUsageInTx(ctx, tx, request, plan); e != nil {
			return portError(e)
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		_, e = x.Exec(ctx, `INSERT INTO agenteam_account.material_cleanup(id,owner_kind,owner_id,credential_id,purpose,phase,version,secret_command_digest) VALUES($1,'login_response',$2,$3,'system','delete',1,$4) ON CONFLICT(owner_kind,owner_id,credential_id) DO NOTHING`, cleanupID.String(), id, cmd.responseRef, string(dg))
		return portError(e)
	})
	return resultError(r)
}
func (s *Service) cleanMaterial(ctx context.Context, id string) error {
	st := s.state()
	var owner, refRaw, phase string
	e := st.store.QueryRow(ctx, `SELECT owner_id::text,credential_id::text,phase FROM agenteam_account.material_cleanup WHERE id=$1 AND owner_kind='login_response'`, id).Scan(&owner, &refRaw, &phase)
	if e != nil {
		return unavailable(e)
	}
	if phase == "completed" {
		return nil
	}
	cmd, e := loadCommand(ctx, st.store, owner, false)
	if e != nil {
		return e
	}
	refID, e := parseID[sc.Credential](refRaw)
	if e != nil {
		return e
	}
	ref, e := sc.NewCredentialRef(refID, identity.SystemScope())
	if e != nil {
		return e
	}
	actor, e := serviceActor(identity.AccountMaintenance, id)
	if e != nil {
		return e
	}
	key, e := cleanupIdentity(owner, id)
	if e != nil {
		return e
	}
	request, e := sc.NewServiceWriteRequest(sc.ServiceWriteFields{Actor: actor, Scope: identity.SystemScope(), Identity: key, OwnerKind: sc.LoginResponseMaterial, OwnerID: owner, Kind: sc.Delete, Ref: ref, ExpectedVersion: 1, Purpose: sc.System})
	if e != nil {
		return e
	}
	prepared, e := st.deps.Secrets.PrepareServiceWrite(ctx, request)
	if e != nil {
		return portError(e)
	}
	cause, e := foundation.NewCommandsCause(key)
	if e != nil {
		return e
	}
	r := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, append(prepared.RequiredLocks(), commandLocks(cmd)...)); e != nil {
			return unavailable(e)
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		// Preserve all recovery bindings until response readers/leases have
		// actually joined. Released plans are removed in the preceding stage.
		var pending bool
		if e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.response_plans WHERE command_id=$1)`, owner).Scan(&pending); e != nil {
			return unavailable(e)
		}
		if pending {
			return fault(foundation.ResourceBusy, nil)
		}
		if _, e = st.deps.Secrets.ApplyServiceWriteInTx(ctx, tx, prepared); e != nil {
			return portError(e)
		}
		if _, e = x.Exec(ctx, `UPDATE agenteam_account.commands SET response_secret_ref=NULL,response_expires_at=NULL,planned_secret_ref=NULL,secret_write_binding=NULL WHERE id=$1 AND response_secret_ref=$2`, owner, refRaw); e != nil {
			return unavailable(e)
		}
		_, e = x.Exec(ctx, `UPDATE agenteam_account.material_cleanup SET phase='completed',completed_at=coalesce(completed_at,clock_timestamp()) WHERE id=$1`, id)
		return portError(e)
	})
	return resultError(r)
}
func (s *Service) recoverBootstrap(ctx context.Context) error {
	st := s.state()
	b, e := s.bootstrapFact(ctx)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil
	}
	if e != nil {
		return unavailable(e)
	}
	if b.state != "eligible" && b.state != "attempted" {
		return nil
	}
	st.mu.Lock()
	local := st.bootstraps[b.operation]
	st.mu.Unlock()
	if e = s.stopped(ctx, b.process, local); e != nil {
		return e
	}
	key, e := foundation.NewCommandIdentity("account.bootstrap", nil, "initialize", foundation.IdempotencyKey("initial-administrator"))
	if e != nil {
		return e
	}
	cause, e := foundation.NewCommandsCause(key)
	if e != nil {
		return e
	}
	state := "abandoned"
	if b.state == "attempted" {
		state = "unknown"
	}
	r := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, []foundation.LockRequest{commandLock(key), configLock("account-directory", foundation.Exclusive), userLock(b.user, foundation.Exclusive)}); e != nil {
			return unavailable(e)
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		_, e = x.Exec(ctx, `UPDATE agenteam_account.bootstrap SET log_state=$4 WHERE operation_id=$1 AND creator_process_id=$2 AND log_state=$3`, b.operation, b.process, b.state, state)
		return portError(e)
	})
	if e = resultError(r); e == nil {
		st.mu.Lock()
		delete(st.bootstraps, b.operation)
		st.mu.Unlock()
	}
	return e
}
