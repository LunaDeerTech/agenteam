package account

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

func commandMapping(r commandRecord) foundation.Digest {
	b, _ := json.Marshal(struct {
		ID, Identity, Kind, User, Session, Browser, Resource, Process string
		PasswordVersion                                               int64
		Semantic                                                      []byte
	}{r.id.String(), r.identity.Canonical(), r.name, r.user, r.session, r.browser, r.resource, r.process, r.passwordVersion, r.mac})
	return digest(b)
}
func (a *Authority) writeMapping(ctx context.Context, x postgres.SQLExecutor, r sc.ServiceWriteRequest) (foundation.Digest, []foundation.LockRequest, error) {
	f := r.Fields()
	actor := f.Actor.Details()
	if actor.ServiceName == identity.AccountAuth {
		cmd, e := loadCommand(ctx, x, actor.CauseRef, false)
		if e != nil {
			return "", nil, e
		}
		owner := cmd.resource
		expected := sc.InvitationMaterial
		switch cmd.name {
		case "login":
			owner = cmd.id.String()
			expected = sc.LoginResponseMaterial
		case "reset-request":
			expected = sc.PasswordResetMaterial
		case "invite-create":
		default:
			return "", nil, fault(foundation.Forbidden, nil)
		}
		binding, bindingErr := sc.ServiceWriteBinding(r)
		commandDigest, digestErr := canonicalCommandDigest(f.Identity)
		if bindingErr != nil || digestErr != nil || string(binding) != cmd.secretBinding || f.OwnerID != owner || f.OwnerKind != expected || cmd.secretDigest != string(commandDigest) {
			return "", nil, fault(foundation.Forbidden, nil)
		}
		return commandMapping(cmd), commandLocks(cmd), nil
	}
	if actor.ServiceName == identity.AccountMaintenance {
		var kind, owner, ref, purpose, key string
		var version int64
		e := x.QueryRow(ctx, `SELECT owner_kind,owner_id::text,credential_id::text,purpose,version,coalesce(secret_command_digest,'') FROM agenteam_account.material_cleanup WHERE id=$1`, actor.CauseRef).Scan(&kind, &owner, &ref, &purpose, &version, &key)
		if errors.Is(e, pgx.ErrNoRows) {
			return "", nil, fault(foundation.Forbidden, nil)
		}
		if e != nil {
			return "", nil, unavailable(e)
		}
		commandDigest, digestErr := canonicalCommandDigest(f.Identity)
		if digestErr != nil {
			return "", nil, digestErr
		}
		if string(f.OwnerKind) != kind || f.OwnerID != owner || f.Ref.Details().ID.String() != ref || string(f.Purpose) != purpose || int64(f.ExpectedVersion) != version || string(commandDigest) != key {
			return "", nil, fault(foundation.Forbidden, nil)
		}
		b, _ := json.Marshal([]string{actor.CauseRef, kind, owner, ref, purpose, key})
		return digest(b), []foundation.LockRequest{recordLock(actor.CauseRef)}, nil
	}
	return "", nil, fault(foundation.Forbidden, nil)
}
func (a *Authority) DiscoverServiceWrite(ctx context.Context, r sc.ServiceWriteRequest) (sc.WriteDependencies, error) {
	if r.Validate() != nil {
		return sc.WriteDependencies{}, invalid()
	}
	binding, e := sc.ServiceWriteBinding(r)
	if e != nil {
		return sc.WriteDependencies{}, e
	}
	mapping, locks, e := a.writeMapping(ctx, a.state().store, r)
	if e != nil {
		return sc.WriteDependencies{}, e
	}
	return sc.NewWriteDependencies(a.state().secretIssuer, binding, mapping, locks)
}
func (a *Authority) ValidateServiceWriteInTx(ctx context.Context, tx foundation.Tx, r sc.ServiceWriteRequest, d sc.WriteDependencies) error {
	if r.Validate() != nil || d.Validate() != nil {
		return invalid()
	}
	x, e := a.state().store.InTx(tx)
	if e != nil {
		return unavailable(e)
	}
	binding, e := sc.ServiceWriteBinding(r)
	if e != nil {
		return e
	}
	mapping, locks, e := a.writeMapping(ctx, x, r)
	if e != nil {
		return e
	}
	if !d.Matches(a.state().secretIssuer, binding, mapping) {
		return fault(foundation.ResourceBusy, nil)
	}
	if e = a.state().store.RequireHeldLocks(ctx, tx, locks); e != nil {
		return unavailable(e)
	}
	f := r.Fields()
	if f.Actor.Details().ServiceName == identity.AccountAuth {
		cmd, e := loadCommand(ctx, x, f.Actor.Details().CauseRef, false)
		if e != nil {
			return e
		}
		if cmd.phase != "planned" && cmd.phase != "committed" {
			return fault(foundation.InvalidState, nil)
		}
		if cmd.actorKind == "human" {
			u, _ := parseID[identity.User](cmd.user)
			sid, _ := parseID[identity.Session](cmd.session)
			actor, e := identity.NewHuman(u, sid)
			if e != nil {
				return unavailable(e)
			}
			if _, e = a.AuthorizeSystem(ctx, tx, actor, identity.Mutate); e != nil {
				return e
			}
		}
		if cmd.name == "login" {
			u, e := loadUser(ctx, x, cmd.user)
			if e != nil {
				return e
			}
			if int64(u.passwordVersion) != cmd.passwordVersion {
				return fault(foundation.Unauthenticated, nil)
			}
			if cmd.phase == "committed" {
				return a.responseCommandCurrent(ctx, tx, cmd)
			}
		}
		return nil
	}
	var phase string
	e = x.QueryRow(ctx, `SELECT phase FROM agenteam_account.material_cleanup WHERE id=$1`, f.Actor.Details().CauseRef).Scan(&phase)
	if e != nil {
		return unavailable(e)
	}
	if phase != "delete" && phase != "completed" {
		return fault(foundation.InvalidState, nil)
	}
	return nil
}
func (a *Authority) CheckReferenceInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, ref sc.CredentialRef, purpose sc.Purpose, owner string, retain bool) error {
	// Classification is read-only, not authorization. A real account owner is
	// directed to the explicit planner; strangers cannot gain a fallback grant.
	x, e := a.state().store.InTx(tx)
	if e != nil {
		return unavailable(e)
	}
	action := sc.ReleaseReferenceUsage
	if retain {
		action = sc.RetainReferenceUsage
	}
	r := sc.UsageRequest{Actor: actor, Ref: ref, Purpose: purpose, ReferenceOwner: owner, Action: action, Retain: retain}
	if r.Validate() != nil {
		return invalid()
	}
	if _, _, e = a.usageMapping(ctx, x, r); e != nil {
		return e
	}
	return fault(foundation.ResourceBusy, nil)
}

type responsePlan struct {
	id, command, browser, user, session, kid, ref, process, lease string
	mac                                                           []byte
	passwordVersion, fence                                        int64
	expires                                                       time.Time
	joined                                                        bool
}

func loadResponsePlan(ctx context.Context, x postgres.SQLExecutor, id string) (responsePlan, error) {
	var p responsePlan
	e := x.QueryRow(ctx, `SELECT id::text,command_id::text,browser_id::text,user_id::text,session_id::text,semantic_kid,semantic_mac,password_version,credential_id::text,expires_at,process_id::text,fence,lease_id::text,joined_at IS NOT NULL FROM agenteam_account.response_plans WHERE id=$1`, id).Scan(&p.id, &p.command, &p.browser, &p.user, &p.session, &p.kid, &p.mac, &p.passwordVersion, &p.ref, &p.expires, &p.process, &p.fence, &p.lease, &p.joined)
	if errors.Is(e, pgx.ErrNoRows) {
		return p, fault(foundation.Forbidden, nil)
	}
	if e != nil {
		return p, unavailable(e)
	}
	return p, nil
}
func responseMapping(p responsePlan) foundation.Digest {
	b, _ := json.Marshal(struct {
		ID, Command, Browser, User, Session, Kid, Ref, Process, Lease string
		MAC                                                           []byte
		PasswordVersion, Fence                                        int64
		Expires                                                       time.Time
	}{p.id, p.command, p.browser, p.user, p.session, p.kid, p.ref, p.process, p.lease, p.mac, p.passwordVersion, p.fence, p.expires})
	return digest(b)
}
func (a *Authority) usageMapping(ctx context.Context, x postgres.SQLExecutor, r sc.UsageRequest) (foundation.Digest, []foundation.LockRequest, error) {
	if r.Ref.Details().Scope.Details().Kind != identity.System || r.Actor.Details().Kind != identity.Service || r.Actor.Details().ServiceName != identity.SecretService || r.Purpose != sc.System && r.Purpose != sc.SMTP {
		return "", nil, fault(foundation.Forbidden, nil)
	}
	if r.Action == sc.RetainReferenceUsage || r.Action == sc.ReleaseReferenceUsage {
		if r.Actor.Details().CauseRef != r.ReferenceOwner {
			return "", nil, fault(foundation.Forbidden, nil)
		}
		cmd, e := loadCommand(ctx, x, r.ReferenceOwner, false)
		if e == nil && cmd.name == "login" {
			if r.Purpose != sc.System || cmd.plannedRef != r.Ref.Details().ID.String() && cmd.responseRef != r.Ref.Details().ID.String() {
				return "", nil, fault(foundation.Forbidden, nil)
			}
			return commandMapping(cmd), commandLocks(cmd), nil
		}
		if r.Purpose == sc.SMTP {
			return a.smtpReferenceMapping(ctx, x, r)
		}
		return a.linkReferenceMapping(ctx, x, r)

	}
	if r.Actor.Details().CauseRef != r.LeaseOwner.Details().ID {
		return "", nil, fault(foundation.Forbidden, nil)
	}
	if r.LeaseOwner.Details().Kind == sc.AccountResponseOwner {
		p, e := loadResponsePlan(ctx, x, r.LeaseOwner.Details().ID)
		if e != nil {
			return "", nil, e
		}
		if p.ref != r.Ref.Details().ID.String() || p.lease != r.LeaseID.String() || r.Purpose != sc.System {
			return "", nil, fault(foundation.Forbidden, nil)
		}
		cmd, e := loadCommand(ctx, x, p.command, false)
		if e != nil {
			return "", nil, e
		}
		locks := append(commandLocks(cmd), userLock(p.user, foundation.Shared), recordLock(p.id))
		return responseMapping(p), locks, nil
	}
	if r.LeaseOwner.Details().Kind == sc.AccountDeliveryOwner {
		return a.deliveryUsageMapping(ctx, x, r)
	}
	return "", nil, fault(foundation.Forbidden, nil)
}
func (a *Authority) DiscoverUsage(ctx context.Context, r sc.UsageRequest) (sc.UsageDependencies, error) {
	binding, e := sc.UsageBinding(r)
	if e != nil {
		return sc.UsageDependencies{}, e
	}
	mapping, locks, e := a.usageMapping(ctx, a.state().store, r)
	if e != nil {
		return sc.UsageDependencies{}, e
	}
	return sc.NewUsageDependencies(a.state().secretIssuer, binding, mapping, locks)
}
func (a *Authority) ValidateUsageInTx(ctx context.Context, tx foundation.Tx, r sc.UsageRequest, d sc.UsageDependencies) error {
	binding, e := sc.UsageBinding(r)
	if e != nil {
		return e
	}
	x, e := a.state().store.InTx(tx)
	if e != nil {
		return unavailable(e)
	}
	mapping, locks, e := a.usageMapping(ctx, x, r)
	if e != nil {
		return e
	}
	if !d.Matches(a.state().secretIssuer, binding, mapping) {
		return fault(foundation.ResourceBusy, nil)
	}
	if e = a.state().store.RequireHeldLocks(ctx, tx, locks); e != nil {
		return unavailable(e)
	}
	if r.Action == sc.RetainReferenceUsage || r.Action == sc.ReleaseReferenceUsage {
		cmd, e := loadCommand(ctx, x, r.ReferenceOwner, false)
		if e == nil && cmd.name == "login" {
			if r.Retain {
				if cmd.phase != "committed" || cmd.responseRef != r.Ref.Details().ID.String() {
					return fault(foundation.InvalidState, nil)
				}
				return a.responseCommandCurrent(ctx, tx, cmd)
			}
			var elapsed bool
			e = x.QueryRow(ctx, `SELECT c.response_expires_at<=clock_timestamp() OR s.revoked_at IS NOT NULL OR u.password_version<>c.password_version FROM agenteam_account.commands c JOIN agenteam_account.sessions s ON s.id=c.session_id JOIN agenteam_account.users u ON u.id=c.user_id WHERE c.id=$1`, r.ReferenceOwner).Scan(&elapsed)
			if e != nil {
				return unavailable(e)
			}
			if !elapsed {
				return fault(foundation.ResourceBusy, nil)
			}
			return nil
		}
		if r.Purpose == sc.SMTP {
			return a.validateSMTPReference(ctx, tx, x, r)
		}
		return a.validateLinkReference(ctx, tx, x, r)
	}
	action := sc.AcquireLease
	if r.Action == sc.ReleaseLeaseUsage {
		action = sc.ReleaseLease
	}
	if r.Action == sc.ReadLeaseUsage {
		action = sc.ReadLease
	}
	_, e = a.AuthorizeLeaseInTx(ctx, tx, r.Actor, r.Ref, r.LeaseOwner, action)
	return e
}
func (a *Authority) responseCommandCurrent(ctx context.Context, tx foundation.Tx, cmd commandRecord) error {
	if cmd.phase != "committed" || cmd.name != "login" || cmd.responseRef == "" {
		return fault(foundation.Unauthenticated, nil)
	}
	x, e := a.state().store.InTx(tx)
	if e != nil {
		return unavailable(e)
	}
	var live bool
	e = x.QueryRow(ctx, `SELECT response_expires_at>clock_timestamp() FROM agenteam_account.commands WHERE id=$1`, cmd.id.String()).Scan(&live)
	if e != nil {
		return unavailable(e)
	}
	if !live {
		return fault(foundation.Unauthenticated, nil)
	}
	u, e := loadUser(ctx, x, cmd.user)
	if e != nil {
		return e
	}
	if int64(u.passwordVersion) != cmd.passwordVersion {
		return fault(foundation.Unauthenticated, nil)
	}
	uid, _ := parseID[identity.User](cmd.user)
	sid, _ := parseID[identity.Session](cmd.session)
	actor, e := identity.NewHuman(uid, sid)
	if e != nil {
		return unavailable(e)
	}
	return a.RequireCurrentSession(ctx, tx, actor)
}
func (a *Authority) AuthorizeLeaseInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, ref sc.CredentialRef, owner sc.CredentialLeaseOwner, action sc.LeaseAction) (sc.UseGrant, error) {
	empty := sc.UseGrant{}
	if actor.Validate() != nil || actor.Details().Kind != identity.Service || actor.Details().ServiceName != identity.SecretService || actor.Details().CauseRef != owner.Details().ID || ref.Validate() != nil || ref.Details().Scope.Details().Kind != identity.System {
		return empty, fault(foundation.Forbidden, nil)
	}
	x, e := a.state().store.InTx(tx)
	if e != nil {
		return empty, unavailable(e)
	}
	if owner.Details().Kind == sc.AccountDeliveryOwner {
		return a.authorizeDeliveryLease(ctx, tx, x, actor, ref, owner, action)
	}
	if owner.Details().Kind != sc.AccountResponseOwner {
		return empty, fault(foundation.DependencyUnbound, nil)
	}
	p, e := loadResponsePlan(ctx, x, owner.Details().ID)
	if e != nil {
		return empty, e
	}
	cmd, e := loadCommand(ctx, x, p.command, false)
	if e != nil {
		return empty, e
	}
	if e = a.state().store.RequireHeldLocks(ctx, tx, append(commandLocks(cmd), userLock(p.user, foundation.Shared), recordLock(p.id))); e != nil {
		return empty, unavailable(e)
	}
	if p.ref != ref.Details().ID.String() || p.ref != cmd.responseRef || p.user != cmd.user || p.session != cmd.session || p.browser != cmd.browser || p.kid != cmd.kid || p.passwordVersion != cmd.passwordVersion || subtle.ConstantTimeCompare(p.mac, cmd.mac) != 1 || !p.expires.Equal(cmd.responseExpires) {
		return empty, fault(foundation.Forbidden, nil)
	}
	var phase, process, lease string
	var fence int64
	e = x.QueryRow(ctx, `SELECT phase,process_id::text,fence,lease_id::text FROM agenteam_account.auth_attempts WHERE id=$1 AND kind='response_read' AND command_id=$2 AND browser_id=$3 AND user_id=$4 AND session_id=$5 AND password_version=$6 AND credential_id=$7 AND semantic_kid=$8 AND semantic_mac=$9 AND expires_at=$10`, p.id, p.command, p.browser, p.user, p.session, p.passwordVersion, p.ref, p.kid, p.mac, p.expires).Scan(&phase, &process, &fence, &lease)
	if e != nil {
		return empty, fault(foundation.Forbidden, e)
	}
	if process != p.process || fence != p.fence || lease != p.lease {
		return empty, fault(foundation.Forbidden, nil)
	}
	if action == sc.ReleaseLease {
		if !p.joined || phase != "released" {
			return empty, fault(foundation.ResourceBusy, nil)
		}
	} else {
		if p.joined || phase != "active" {
			return empty, fault(foundation.InvalidState, nil)
		}
		if e = a.responseCommandCurrent(ctx, tx, cmd); e != nil {
			return empty, e
		}
	}
	return sc.UseGrant{Subject: actor, Consumer: sc.System}, nil
}

var _ sc.AccountWriteAuthority = (*Authority)(nil)
var _ sc.UsageAuthority = (*Authority)(nil)
var _ sc.UsagePlanner = (*Authority)(nil)
