package account

import (
	"context"
	"errors"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

type linkRecord struct {
	id, kind, email, user, ref     string
	verifier                       []byte
	version, passwordVersion       int64
	created, expires, lastDelivery time.Time
	live                           bool
}

func loadLink(ctx context.Context, x postgres.SQLExecutor, kind c.TokenKind, id string) (linkRecord, error) {
	r := linkRecord{kind: string(kind)}
	var e error
	switch kind {
	case c.InvitationToken:
		e = x.QueryRow(ctx, `SELECT id::text,canonical_email,material_ref::text,token_verifier,version,created_at,expires_at,coalesce(last_delivery_at,'0001-01-01Z'::timestamptz),expires_at>clock_timestamp() FROM agenteam_account.invitations WHERE id=$1`, id).Scan(&r.id, &r.email, &r.ref, &r.verifier, &r.version, &r.created, &r.expires, &r.lastDelivery, &r.live)
	case c.PasswordResetToken:
		e = x.QueryRow(ctx, `SELECT r.id::text,r.user_id::text,r.material_ref::text,r.token_verifier,r.version,r.password_version,r.created_at,r.expires_at,coalesce(r.last_delivery_at,'0001-01-01Z'::timestamptz),r.expires_at>clock_timestamp() AND r.password_version=u.password_version FROM agenteam_account.password_resets r JOIN agenteam_account.users u ON u.id=r.user_id WHERE r.id=$1`, id).Scan(&r.id, &r.user, &r.ref, &r.verifier, &r.version, &r.passwordVersion, &r.created, &r.expires, &r.lastDelivery, &r.live)
	default:
		return r, invalid()
	}
	if errors.Is(e, pgx.ErrNoRows) {
		return r, fault(foundation.ResourceDeleted, nil)
	}
	return r, portError(e)
}
func invitationReceipt(cmd commandRecord) (c.InvitationReceipt, error) {
	id, e := parseID[c.Invitation](cmd.resource)
	if e != nil {
		return c.InvitationReceipt{}, e
	}
	job, e := parseID[c.MailJob](cmd.attempt)
	if e != nil {
		return c.InvitationReceipt{}, e
	}
	return c.InvitationReceipt{ID: id, JobID: job, Version: foundation.Version(cmd.resultVersion)}, nil
}
func (s *Service) CreateInvitation(ctx context.Context, r c.InvitationCreate) (c.InvitationReceipt, error) {
	return s.createInvitation(ctx, r, nil)
}
func (s *Service) ResendInvitation(ctx context.Context, r c.InvitationResend) (c.InvitationReceipt, error) {
	if r.Actor.Validate() != nil || r.Actor.Details().Kind != identity.Human || r.Key.Validate() != nil || r.ID.Validate() != nil || r.ExpectedVersion.Validate() != nil {
		return c.InvitationReceipt{}, invalid()
	}
	input, e := c.NewInvitationCreate(c.InvitationCreateFields{Actor: r.Actor, Key: r.Key})
	if e != nil {
		return c.InvitationReceipt{}, e
	}
	return s.createInvitation(ctx, input, &r)
}
func (s *Service) createInvitation(ctx context.Context, r c.InvitationCreate, target *c.InvitationResend) (c.InvitationReceipt, error) {
	if r.Validate() != nil {
		return c.InvitationReceipt{}, invalid()
	}
	op, e := s.begin(ctx, false)
	if e != nil {
		return c.InvitationReceipt{}, e
	}
	defer s.finish(op)
	ctx = op.ctx
	st := s.state()
	f := r.Fields()
	if _, e = st.deps.Authority.AuthorizeSystem(ctx, foundation.Tx{}, f.Actor, identity.Mutate); e != nil {
		return c.InvitationReceipt{}, e
	}
	var email string
	if target == nil {
		email, e = NormalizeEmail(f.Email)
	}
	if e != nil {
		return c.InvitationReceipt{}, e
	}
	key, e := foundation.NewCommandIdentity("account.invitation", []string{f.Actor.Details().UserID}, "invite-create", f.Key)
	if e != nil {
		return c.InvitationReceipt{}, e
	}
	if e = s.trackMutation(key, op); e != nil {
		return c.InvitationReceipt{}, e
	}
	macFor := func(kid string) ([]byte, error) {
		if target != nil {
			return st.keys.mac(kid, "command-v1", []byte("invite-resend"), []byte(f.Actor.Details().UserID), []byte(target.ID.String()), []byte(target.ExpectedVersion.String()))
		}
		return st.keys.mac(kid, "command-v1", []byte("invite-create"), []byte(f.Actor.Details().UserID), []byte(email))
	}
	if target != nil {
		old, err := s.lookupMutation(ctx, key, macFor, f.Actor, c.BrowserIdentity{}, true)
		if err == nil && old.phase == "committed" {
			return invitationReceipt(old)
		}
		if err != nil && !hasFaultCode(err, foundation.NotFound) {
			return c.InvitationReceipt{}, err
		}
		link, err := loadLink(ctx, st.store, c.InvitationToken, target.ID.String())
		if err != nil {
			return c.InvitationReceipt{}, err
		}
		if !link.live {
			return c.InvitationReceipt{}, fault(foundation.ResourceDeleted, nil)
		}
		email = link.email
	}
	mac, e := macFor(st.keys.current())
	if e != nil {
		return c.InvitationReceipt{}, e
	}
	defer clear(mac)
	commandID, e := foundation.NewID[c.Command]()
	if e != nil {
		return c.InvitationReceipt{}, unavailable(e)
	}
	linkID, e := foundation.NewID[c.Invitation]()
	if e != nil {
		return c.InvitationReceipt{}, unavailable(e)
	}
	job, e := foundation.NewID[c.MailJob]()
	if e != nil {
		return c.InvitationReceipt{}, unavailable(e)
	}
	cause, e := foundation.NewCommandsCause(key)
	if e != nil {
		return c.InvitationReceipt{}, e
	}
	locks := []foundation.LockRequest{commandLock(key), configLock("account-directory", foundation.Exclusive), configLock("account-security", foundation.Shared), configLock("account-mail", foundation.Exclusive), userLock(f.Actor.Details().UserID, foundation.Shared)}
	var cmd commandRecord
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
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
			if old.phase != "planned" && old.phase != "committed" {
				return fault(foundation.InvalidState, nil)
			}
			if old.phase == "planned" {
				if _, e = x.Exec(ctx, `UPDATE agenteam_account.commands SET session_id=$2 WHERE id=$1`, old.id.String(), f.Actor.Details().SessionID); e != nil {
					return unavailable(e)
				}
				old, e = loadCommand(ctx, x, old.id.String(), false)
				if e != nil {
					return e
				}
			}
			cmd = old
			return nil
		}
		if !hasFaultCode(e, foundation.NotFound) {
			return e
		}
		if target != nil {
			link, e := loadLink(ctx, x, c.InvitationToken, target.ID.String())
			if e != nil {
				return e
			}
			if !link.live || link.email != email {
				return fault(foundation.ResourceDeleted, nil)
			}
			if link.version != int64(target.ExpectedVersion) {
				return fault(foundation.VersionConflict, nil)
			}
		}
		var registered bool
		if e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.users WHERE email=$1)`, email).Scan(&registered); e != nil {
			return unavailable(e)
		}
		if registered {
			return field("/email", "ALREADY_EXISTS")
		}
		var existing string
		e = x.QueryRow(ctx, `SELECT id::text FROM agenteam_account.invitations WHERE canonical_email=$1`, email).Scan(&existing)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return unavailable(e)
		}
		resource := linkID.String()
		if target != nil && existing != target.ID.String() {
			return fault(foundation.ResourceBusy, nil)
		}
		if existing != "" {
			resource = existing
		}
		_, e = x.Exec(ctx, `INSERT INTO agenteam_account.commands(id,namespace,owner_id,command_key,command_name,identity_digest,semantic_kid,semantic_mac,actor_kind,user_id,session_id,origin_process_id,attempt_id,resource_id,phase) VALUES($1,'account.invitation',$2,$3,'invite-create',$4,$5,$6,'human',$2,$7,$8,$9,$10,'planned')`, commandID.String(), f.Actor.Details().UserID, string(f.Key), string(digest([]byte(key.Canonical()))), st.keys.current(), mac, f.Actor.Details().SessionID, st.process.String(), job.String(), resource)
		if e != nil {
			return unavailable(e)
		}
		cmd, e = loadCommand(ctx, x, commandID.String(), false)
		return e
	})
	if result.State() == foundation.Unknown {
		cmd, e = s.lookupMutation(ctx, key, macFor, f.Actor, c.BrowserIdentity{}, true)
		if e != nil {
			return c.InvitationReceipt{}, confirmationError(result, e)
		}
	} else if e = resultError(result); e != nil {
		return c.InvitationReceipt{}, e
	}
	if cmd.phase == "committed" {
		return invitationReceipt(cmd)
	}
	// Any retry replans only an uncommitted command under its original writer;
	// changing a prepared binding invalidates the older exact producer/Secret plan.
	link, e := loadLink(ctx, st.store, c.InvitationToken, cmd.resource)
	exists := e == nil
	if e != nil && !hasFaultCode(e, foundation.ResourceDeleted) {
		return c.InvitationReceipt{}, e
	}
	if target != nil && (!exists || !link.live || link.id != target.ID.String()) {
		return c.InvitationReceipt{}, fault(foundation.ResourceDeleted, nil)
	}
	if exists && !link.live {
		if e = s.expireLink(ctx, c.InvitationToken, cmd.resource); e != nil {
			return c.InvitationReceipt{}, e
		}
		exists = false
		// The expired link's material identity is permanently retired. Allocate a
		// fresh link while keeping the original command, event, and delivery IDs.
		r := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
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
			current, e := loadCommand(ctx, x, cmd.id.String(), false)
			if e != nil {
				return e
			}
			if e = s.checkCommand(current, key, macFor); e != nil {
				return e
			}
			if current.phase != "planned" || current.resource != cmd.resource {
				return fault(foundation.ResourceBusy, nil)
			}
			if _, e = x.Exec(ctx, `UPDATE agenteam_account.commands SET resource_id=$2,secret_command_digest=NULL,secret_write_binding=NULL,planned_secret_ref=NULL WHERE id=$1`, cmd.id.String(), linkID.String()); e != nil {
				return unavailable(e)
			}
			cmd, e = loadCommand(ctx, x, cmd.id.String(), false)
			return e
		})
		if e = resultError(r); e != nil {
			return c.InvitationReceipt{}, e
		}
	}
	var prepared secret.PreparedServiceWrite
	var usage sc.UsageRequest
	var usagePlan sc.UsageDependencies
	var verifier []byte
	if !exists {
		prepared, usage, usagePlan, verifier, e = s.prepareLinkMaterial(ctx, cmd, c.InvitationToken, macFor, f.Actor, c.BrowserIdentity{})
		if e != nil {
			return c.InvitationReceipt{}, e
		}
	}
	actor, evt, appendPlan, e := s.prepareDelivery(ctx, cmd)
	if e != nil {
		return c.InvitationReceipt{}, e
	}
	all := append(append([]foundation.LockRequest(nil), locks...), commandLocks(cmd)...)
	all = append(all, appendPlan.Locks()...)
	if !exists {
		all = append(all, prepared.RequiredLocks()...)
		all = append(all, usagePlan.RequiredLocks()...)
	}
	result = st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, all); e != nil {
			return unavailable(e)
		}
		if _, e := st.deps.Authority.AuthorizeSystem(ctx, tx, f.Actor, identity.Mutate); e != nil {
			return e
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		current, e := loadCommand(ctx, x, cmd.id.String(), false)
		if e != nil {
			return e
		}
		if e = s.checkCommand(current, key, macFor); e != nil {
			return e
		}
		if current.phase == "committed" {
			cmd = current
			return nil
		}
		if current.phase != "planned" {
			return fault(foundation.InvalidState, nil)
		}
		if commandMapping(current) != commandMapping(cmd) {
			return fault(foundation.ResourceBusy, nil)
		}
		var registered bool
		if e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.users WHERE email=$1)`, email).Scan(&registered); e != nil {
			return unavailable(e)
		}
		if registered {
			return field("/email", "ALREADY_EXISTS")
		}
		if exists {
			currentLink, e := loadLink(ctx, x, c.InvitationToken, cmd.resource)
			if e != nil {
				return e
			}
			if target != nil && currentLink.version != int64(target.ExpectedVersion) {
				return fault(foundation.VersionConflict, nil)
			}
			if !currentLink.live || currentLink.ref != link.ref {
				return fault(foundation.ResourceBusy, nil)
			}
			var ready bool
			if e = x.QueryRow(ctx, `SELECT last_delivery_at IS NULL OR last_delivery_at<=clock_timestamp()-interval '60 seconds' FROM agenteam_account.invitations WHERE id=$1`, cmd.resource).Scan(&ready); e != nil {
				return unavailable(e)
			}
			if !ready {
				return fault(foundation.RateLimited, nil)
			}
		} else {
			if current.plannedRef != prepared.Ref().Details().ID.String() {
				return fault(foundation.ResourceBusy, nil)
			}
			_, e = x.Exec(ctx, `INSERT INTO agenteam_account.invitations(id,canonical_email,token_verifier,material_ref,version,created_at,expires_at,created_by) SELECT $1,$2,$3,$4,1,n,n+interval '24 hours',$5 FROM (SELECT clock_timestamp() n) z`, cmd.resource, email, verifier, prepared.Ref().Details().ID.String(), f.Actor.Details().UserID)
			if e != nil {
				return accountConflict(e)
			}
		}
		if e = completeCommand(ctx, x, cmd.id.String(), 1); e != nil {
			return e
		}
		if !exists {
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
		if _, e = st.deps.Events.AppendEventInTx(ctx, tx, actor, evt, appendPlan); e != nil {
			return portError(e)
		}
		if e = s.mutationAudit(ctx, tx, f.Actor, ac.AccountInviteCreate, ac.InvitationResource, cmd.resource, cmd.id.String(), ac.AccountMetadataFields{InvitationID: cmd.resource, InitiatorID: f.Actor.Details().UserID, Version: 1, Phase: ac.AccountCreated}); e != nil {
			return e
		}
		if _, e = x.Exec(ctx, `UPDATE agenteam_account.invitations SET last_delivery_at=clock_timestamp() WHERE id=$1`, cmd.resource); e != nil {
			return unavailable(e)
		}

		cmd, e = loadCommand(ctx, x, cmd.id.String(), false)
		return e
	})
	if result.State() == foundation.Unknown {
		cmd, e = s.lookupMutation(ctx, key, macFor, f.Actor, c.BrowserIdentity{}, true)
		if e != nil {
			return c.InvitationReceipt{}, confirmationError(result, e)
		}
		if cmd.phase != "committed" {
			return c.InvitationReceipt{}, confirmationError(result, nil)
		}
	} else if e = resultError(result); e != nil {
		return c.InvitationReceipt{}, e
	}
	return invitationReceipt(cmd)
}

func (s *Service) prepareLinkMaterial(ctx context.Context, cmd commandRecord, kind c.TokenKind, mac commandMAC, actor identity.Actor, browser c.BrowserIdentity) (secret.PreparedServiceWrite, sc.UsageRequest, sc.UsageDependencies, []byte, error) {
	var empty secret.PreparedServiceWrite
	var noUsage sc.UsageRequest
	var noPlan sc.UsageDependencies
	st := s.state()
	raw, e := randomToken()
	if e != nil {
		return empty, noUsage, noPlan, nil, e
	}
	value, e := sc.NewSecretMaterial([]byte(raw))
	raw = ""
	if e != nil {
		return empty, noUsage, noPlan, nil, portError(e)
	}
	defer value.Destroy()
	var verifier []byte
	e = value.Use(func(b []byte) error { var e error; verifier, e = tokenVerifier(kind, b); return e })
	if e != nil {
		return empty, noUsage, noPlan, nil, e
	}
	key, e := secretIdentity(cmd.resource)
	if e != nil {
		return empty, noUsage, noPlan, nil, e
	}
	writeActor, e := serviceActor(identity.AccountAuth, cmd.id.String())
	if e != nil {
		return empty, noUsage, noPlan, nil, e
	}
	owner := sc.InvitationMaterial
	if kind == c.PasswordResetToken {
		owner = sc.PasswordResetMaterial
	}
	write, e := sc.NewServiceWriteRequest(sc.ServiceWriteFields{Actor: writeActor, Scope: identity.SystemScope(), Identity: key, OwnerKind: owner, OwnerID: cmd.resource, Kind: sc.Create, Purpose: sc.System, Value: value})
	if e != nil {
		return empty, noUsage, noPlan, nil, e
	}
	binding, e := sc.ServiceWriteBinding(write)
	if e != nil {
		return empty, noUsage, noPlan, nil, e
	}
	dg, e := canonicalCommandDigest(key)
	if e != nil {
		return empty, noUsage, noPlan, nil, e
	}
	cause, e := foundation.NewCommandsCause(cmd.identity)
	if e != nil {
		return empty, noUsage, noPlan, nil, e
	}
	save := func(ref string) error {
		r := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
			if e := st.store.AcquireAll(ctx, tx, commandLocks(cmd)); e != nil {
				return unavailable(e)
			}
			x, e := st.store.InTx(tx)
			if e != nil {
				return unavailable(e)
			}
			if actor.Validate() == nil {
				if _, e = st.deps.Authority.AuthorizeSystem(ctx, tx, actor, identity.Mutate); e != nil {
					return e
				}
			}
			if browser.Validate() == nil {
				if e = s.validateBrowserInTx(ctx, x, browser); e != nil {
					return e
				}
			}
			current, e := loadCommand(ctx, x, cmd.id.String(), false)
			if e != nil {
				return e
			}
			if e = s.checkCommand(current, cmd.identity, mac); e != nil {
				return e
			}
			if current.resource != cmd.resource || current.name != cmd.name || current.name == "invite-create" && current.phase != "planned" {
				return fault(foundation.ResourceBusy, nil)
			}
			if current.name == "reset-request" {
				var pending bool
				if e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.reset_requests WHERE command_id=$1 AND phase='accepted')`, cmd.id.String()).Scan(&pending); e != nil {
					return unavailable(e)
				}
				if !pending {
					return fault(foundation.ResourceBusy, nil)
				}
			}
			if ref == "" {
				_, e = x.Exec(ctx, `UPDATE agenteam_account.commands SET secret_command_digest=$2,secret_write_binding=$3,planned_secret_ref=NULL WHERE id=$1`, cmd.id.String(), string(dg), string(binding))
			} else {
				if current.secretBinding != string(binding) {
					return fault(foundation.ResourceBusy, nil)
				}
				_, e = x.Exec(ctx, `UPDATE agenteam_account.commands SET planned_secret_ref=$2 WHERE id=$1`, cmd.id.String(), ref)
			}
			return portError(e)
		})
		return resultError(r)
	}
	if e = save(""); e != nil {
		return empty, noUsage, noPlan, nil, e
	}
	prepared, e := st.deps.Secrets.PrepareServiceWrite(ctx, write)
	if e != nil {
		return empty, noUsage, noPlan, nil, portError(e)
	}
	if e = save(prepared.Ref().Details().ID.String()); e != nil {
		return empty, noUsage, noPlan, nil, e
	}
	usage, e := linkUsage(cmd.resource, prepared.Ref(), true)
	if e != nil {
		return empty, noUsage, noPlan, nil, e
	}
	plan, e := st.deps.Secrets.DiscoverUsage(ctx, usage)
	return prepared, usage, plan, verifier, portError(e)
}
