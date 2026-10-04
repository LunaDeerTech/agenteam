package account

import (
	"context"
	"encoding/json"
	"net"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func smtpProjection(r smtpRecord) c.SMTPSettings {
	id, _ := parseID[c.SettingsRecord](r.ID)
	return c.SMTPSettings{ID: id, Version: foundation.Version(r.Version), Configured: r.Configured, Host: r.Host, Port: r.Port, TLSMode: r.Mode, Username: r.Username, SenderEmail: r.From, SenderName: r.Name, CredentialPresent: r.Ref != "", RetryCount: foundation.Progress(r.Retries), RetryIntervalSeconds: foundation.Progress(r.Interval)}
}
func (s *Service) GetSMTPSettings(ctx context.Context, actor identity.Actor) (c.SMTPSettings, error) {
	op, e := s.begin(ctx, false)
	if e != nil {
		return c.SMTPSettings{}, e
	}
	defer s.finish(op)
	ctx = op.ctx
	cause, e := recoveryCause("smtp-settings-read")
	if e != nil {
		return c.SMTPSettings{}, e
	}
	var out smtpRecord
	r := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if actor.Validate() != nil || actor.Details().Kind != identity.Human {
			return invalid()
		}
		if e := s.state().store.AcquireAll(ctx, tx, []foundation.LockRequest{userLock(actor.Details().UserID, foundation.Shared), configLock("account-mail", foundation.Shared)}); e != nil {
			return unavailable(e)
		}
		if _, e := s.state().deps.Authority.AuthorizeSystem(ctx, tx, actor, identity.Read); e != nil {
			return e
		}
		x, e := s.state().store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		out, e = loadSMTP(ctx, x)
		return e
	})
	if e = resultError(r); e != nil {
		return c.SMTPSettings{}, e
	}
	return smtpProjection(out), nil
}
func smtpFields(f c.SMTPUpdateFields) (c.SMTPUpdateFields, bool, error) {
	if f.RetryCount < 0 || f.RetryCount > 5 || f.RetryIntervalSeconds < 10 || f.RetryIntervalSeconds > 3600 || f.CredentialAction != "" && f.CredentialAction != "keep" && f.CredentialAction != "remove" {
		return f, false, invalid()
	}
	for _, v := range []string{f.Host, f.Username, f.SenderEmail, f.SenderName} {
		if !utf8.ValidString(v) || strings.IndexFunc(v, unicode.IsControl) >= 0 {
			return f, false, invalid()
		}
	}
	replace := f.Password != nil
	if replace {
		if e := f.Password.Use(func(b []byte) error {
			if len(b) > 2048 {
				return field("/password", "TOO_LONG")
			}
			for _, v := range b {
				if v == 0 || v == '\r' || v == '\n' {
					return invalid()
				}
			}
			return nil
		}); e != nil {
			return f, false, portError(e)
		}
	}
	if f.CredentialAction == "remove" && replace {
		return f, false, invalid()
	}
	if !f.Configured {
		if f.Host != "" || f.Port != 0 || f.TLSMode != "" || f.Username != "" || f.SenderEmail != "" || f.SenderName != "" || replace {
			return f, false, invalid()
		}
		return f, false, nil
	}
	if f.Port < 1 || f.Port > 65535 || f.TLSMode != "none" && f.TLSMode != "starttls" && f.TLSMode != "tls" || len(f.Username) > 256 || utf8.RuneCountInString(f.SenderName) > 80 || strings.ContainsAny(f.Host, "/@?#") {
		return f, false, invalid()
	}
	t, e := outbound.ParseTarget("https://" + net.JoinHostPort(f.Host, strconv.Itoa(f.Port)) + "/")
	if e != nil {
		return f, false, field("/host", "INVALID")
	}
	f.Host = t.Origin().Host()
	email, e := NormalizeEmail(f.SenderEmail)
	if e != nil {
		return f, false, field("/sender_email", "INVALID")
	}
	f.SenderEmail = email
	return f, replace, nil
}
func (s *Service) UpdateSMTPSettings(ctx context.Context, input c.SMTPUpdate) (c.SMTPSettingsReceipt, error) {
	if input.Validate() != nil {
		return c.SMTPSettingsReceipt{}, invalid()
	}
	f, replace, e := smtpFields(input.Fields())
	if e != nil {
		return c.SMTPSettingsReceipt{}, e
	}
	op, e := s.begin(ctx, false)
	if e != nil {
		return c.SMTPSettingsReceipt{}, e
	}
	defer s.finish(op)
	ctx = op.ctx
	st := s.state()
	if _, e = st.deps.Authority.AuthorizeSystem(ctx, foundation.Tx{}, f.Actor, identity.Mutate); e != nil {
		return c.SMTPSettingsReceipt{}, e
	}
	key, e := foundation.NewCommandIdentity("account.smtp", []string{f.Actor.Details().UserID}, "smtp-settings-update", f.Key)
	if e != nil {
		return c.SMTPSettingsReceipt{}, e
	}
	semantic, e := json.Marshal(struct {
		Configured                                    bool
		Host, Mode, User, From, Name, Action, Version string
		Port                                          int
		Retry, Interval                               int64
	}{f.Configured, f.Host, f.TLSMode, f.Username, f.SenderEmail, f.SenderName, f.CredentialAction, f.ExpectedVersion.String(), f.Port, f.RetryCount, f.RetryIntervalSeconds})
	if e != nil {
		return c.SMTPSettingsReceipt{}, unavailable(e)
	}
	macFor := func(kid string) (out []byte, e error) {
		if replace {
			e = f.Password.Use(func(b []byte) error {
				out, e = st.keys.mac(kid, "command-v1", []byte(key.Canonical()), semantic, b)
				return e
			})
			return
		}
		return st.keys.mac(kid, "command-v1", []byte(key.Canonical()), semantic, nil)
	}
	cmd, e := s.lookupMutation(ctx, key, macFor, f.Actor, c.BrowserIdentity{}, true)
	if e == nil && cmd.phase == "committed" {
		return smtpReceipt(cmd), nil
	}
	if e != nil && !hasFaultCode(e, foundation.NotFound) {
		return c.SMTPSettingsReceipt{}, e
	}
	before, e := loadSMTP(ctx, st.store)
	if e != nil {
		return c.SMTPSettingsReceipt{}, e
	}
	if before.Version != int64(f.ExpectedVersion) {
		return c.SMTPSettingsReceipt{}, fault(foundation.VersionConflict, nil)
	}
	id, e := foundation.NewID[c.Command]()
	if e != nil {
		return c.SMTPSettingsReceipt{}, unavailable(e)
	}
	cause, e := foundation.NewCommandsCause(key)
	if e != nil {
		return c.SMTPSettingsReceipt{}, e
	}
	mac, e := macFor(st.keys.current())
	if e != nil {
		return c.SMTPSettingsReceipt{}, e
	}
	defer clear(mac)
	locks := []foundation.LockRequest{commandLock(key), userLock(f.Actor.Details().UserID, foundation.Shared), configLock("account-directory", foundation.Shared), configLock("account-mail", foundation.Exclusive), recordLock(before.ID), recordLock(id.String())}
	r := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
			return unavailable(e)
		}
		if _, e := st.deps.Authority.AuthorizeSystem(ctx, tx, f.Actor, identity.Mutate); e != nil {
			return e
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		old, e := loadCommand(ctx, x, string(digest([]byte(key.Canonical()))), true)
		if e == nil {
			if e = s.checkCommand(old, key, macFor); e != nil {
				return e
			}
			cmd = old
			if cmd.phase == "planned" && cmd.session != f.Actor.Details().SessionID {
				if _, e = x.Exec(ctx, `UPDATE agenteam_account.commands SET session_id=$2 WHERE id=$1 AND phase='planned'`, cmd.id.String(), f.Actor.Details().SessionID); e != nil {
					return unavailable(e)
				}
				cmd, e = loadCommand(ctx, x, cmd.id.String(), false)
				return e
			}
			return nil
		}
		if !hasFaultCode(e, foundation.NotFound) {
			return e
		}
		_, e = x.Exec(ctx, `INSERT INTO agenteam_account.commands(id,namespace,owner_id,command_key,command_name,identity_digest,semantic_kid,semantic_mac,actor_kind,user_id,session_id,origin_process_id,resource_id,expected_version,phase) VALUES($1,'account.smtp',$2,$3,'smtp-settings-update',$4,$5,$6,'human',$2,$7,$8,$9,$10,'planned')`, id.String(), f.Actor.Details().UserID, string(f.Key), string(digest([]byte(key.Canonical()))), st.keys.current(), mac, f.Actor.Details().SessionID, st.process.String(), before.ID, int64(f.ExpectedVersion))
		if e != nil {
			return unavailable(e)
		}
		cmd, e = loadCommand(ctx, x, id.String(), false)
		return e
	})
	if r.State() == foundation.Unknown {
		cmd, e = s.lookupMutation(ctx, key, macFor, f.Actor, c.BrowserIdentity{}, true)
		if e != nil {
			return c.SMTPSettingsReceipt{}, confirmationError(r, e)
		}
	} else if e = resultError(r); e != nil {
		return c.SMTPSettingsReceipt{}, e
	}
	if cmd.phase == "committed" {
		return smtpReceipt(cmd), nil
	}
	if cmd.phase != "planned" {
		return c.SMTPSettingsReceipt{}, fault(foundation.InvalidState, nil)
	}
	newRef := before.Ref
	var prepared secret.PreparedWrite
	if replace {
		secretKey, e := foundation.NewCommandIdentity("secret", []string{f.Actor.Details().UserID}, "create", foundation.IdempotencyKey(cmd.id.String()))
		if e != nil {
			return c.SMTPSettingsReceipt{}, e
		}
		prepared, e = st.deps.Secrets.PrepareWrite(ctx, sc.WriteRequest{Actor: f.Actor, Scope: identity.SystemScope(), Identity: secretKey, Kind: sc.Create, Purpose: sc.SMTP, Value: *f.Password})
		if e != nil {
			return c.SMTPSettingsReceipt{}, portError(e)
		}
		newRef = prepared.Ref().Details().ID.String()
	}
	if !f.Configured || f.CredentialAction == "remove" {
		newRef = ""
	}
	if f.Configured && ((f.Username == "") != (newRef == "")) {
		return c.SMTPSettingsReceipt{}, field("/username", "CREDENTIAL_REQUIRED")
	}
	// Publish only a planned mapping, never a credential read capability.
	r = st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, append(locks, commandLocks(cmd)...)); e != nil {
			return unavailable(e)
		}
		if _, e := st.deps.Authority.AuthorizeSystem(ctx, tx, f.Actor, identity.Mutate); e != nil {
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
		if now.phase != "planned" {
			return fault(foundation.ResourceBusy, nil)
		}
		_, e = x.Exec(ctx, `UPDATE agenteam_account.commands SET planned_secret_ref=$2 WHERE id=$1`, cmd.id.String(), null(newRef))
		return portError(e)
	})
	if e = resultError(r); e != nil {
		return c.SMTPSettingsReceipt{}, e
	}
	var usage []sc.UsageRequest
	var plans []sc.UsageDependencies
	for _, slot := range []struct {
		ref    string
		retain bool
	}{{newRef, true}, {before.Ref, false}} {
		if slot.ref == "" || newRef == before.Ref {
			continue
		}
		q, e := smtpReference(before.ID, slot.ref, slot.retain)
		if e != nil {
			return c.SMTPSettingsReceipt{}, e
		}
		plan, e := st.deps.Secrets.DiscoverUsage(ctx, q)
		if e != nil {
			return c.SMTPSettingsReceipt{}, portError(e)
		}
		usage = append(usage, q)
		plans = append(plans, plan)
		locks = append(locks, plan.RequiredLocks()...)
	}
	locks = append(locks, commandLocks(cmd)...)
	if replace {
		locks = append(locks, prepared.RequiredLocks()...)
	}
	cleanup, e := foundation.NewID[c.Cleanup]()
	if e != nil {
		return c.SMTPSettingsReceipt{}, unavailable(e)
	}
	locks = append(locks, recordLock(cleanup.String()))
	guard, e := st.deps.Authority.mailExclusive(ctx)
	if e != nil {
		return c.SMTPSettingsReceipt{}, e
	}
	defer guard.release()
	r = st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
			return unavailable(e)
		}
		if _, e := st.deps.Authority.AuthorizeSystem(ctx, tx, f.Actor, identity.Mutate); e != nil {
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
		if now.phase == "committed" {
			return nil
		}
		if now.phase != "planned" || now.plannedRef != newRef {
			return fault(foundation.ResourceBusy, nil)
		}
		current, e := loadSMTP(ctx, x)
		if e != nil {
			return e
		}
		if current.Version != int64(f.ExpectedVersion) || current.Ref != before.Ref {
			return fault(foundation.VersionConflict, nil)
		}
		if before.Ref != "" && before.Ref != newRef {
			if e = s.queueSMTPMaterialCleanup(ctx, x, cleanup, before); e != nil {
				return e
			}
		}
		_, e = x.Exec(ctx, `UPDATE agenteam_account.smtp_settings SET configured=$2,enabled=$3,host=$4,port=$5,tls_mode=$6,auth_username=$7,from_address=$8,sender_name=$9,password_ref=$10,auto_retry_count=$11,retry_interval_seconds=$12,version=version+1 WHERE id=$1`, before.ID, f.Configured, f.Configured, null(f.Host), smtpPort(f.Port), null(f.TLSMode), null(f.Username), null(f.SenderEmail), f.SenderName, null(newRef), f.RetryCount, f.RetryIntervalSeconds)
		if e != nil {
			return unavailable(e)
		}
		if e = completeCommand(ctx, x, cmd.id.String(), before.Version+1); e != nil {
			return e
		}
		if replace {
			if _, e = st.deps.Secrets.ApplyPreparedWriteInTx(ctx, tx, prepared); e != nil {
				return portError(e)
			}
		}
		for i, q := range usage {
			if _, e = st.deps.Secrets.ApplyUsageInTx(ctx, tx, q, plans[i]); e != nil {
				return portError(e)
			}
		}
		// PUT writes this closed set of configuration fields; the metadata
		// contains names only, never their values or the credential material.
		fields := []ac.AccountChangedField{ac.SMTPHostChanged, ac.SMTPPortChanged, ac.SMTPTLSChanged, ac.SMTPUsernameChanged, ac.SMTPFromChanged, ac.SMTPEnabledChanged, ac.SMTPSenderNameChanged, ac.SMTPAutoRetryCountChanged, ac.SMTPRetryIntervalChanged}
		if before.Ref != newRef {
			fields = append(fields, ac.SMTPPasswordChanged)
		}
		return s.mutationAudit(ctx, tx, f.Actor, ac.SMTPSettingsUpdate, ac.SMTPSettingsResource, before.ID, cmd.id.String(), ac.AccountMetadataFields{Version: foundation.Version(before.Version + 1), Phase: ac.AccountUpdated, ChangedFields: fields})
	})
	if r.State() == foundation.Unknown {
		saved, e := s.lookupMutation(ctx, key, macFor, f.Actor, c.BrowserIdentity{}, true)
		if e != nil || saved.phase != "committed" {
			return c.SMTPSettingsReceipt{}, confirmationError(r, e)
		}
		cmd = saved
	} else if e = resultError(r); e != nil {
		return c.SMTPSettingsReceipt{}, e
	}
	if cmd.resultVersion == 0 {
		cmd.resultVersion = before.Version + 1
	}
	return smtpReceipt(cmd), nil
}
func smtpPort(port int) any {
	if port == 0 {
		return nil
	}
	return port
}

func smtpReceipt(cmd commandRecord) c.SMTPSettingsReceipt {
	id, _ := parseID[c.SettingsRecord](cmd.resource)
	return c.SMTPSettingsReceipt{ID: id, Version: foundation.Version(cmd.resultVersion)}
}
