package account

import (
	"bytes"
	"context"
	"io"
	"math"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func (p *ProfileService) PutAvatar(ctx context.Context, r c.AvatarUpload) (c.ProfileView, error) {
	if e := r.Validate(); e != nil {
		if !nilPort(r.Body) {
			_ = r.Body.Close()
		}
		return c.ProfileView{}, e
	}
	core := p.state().core
	op, e := core.begin(ctx, false)
	if e != nil {
		_ = r.Body.Close()
		return c.ProfileView{}, e
	}
	defer core.finish(op)
	ctx = op.ctx
	joinBody := closeOwnedBody(ctx, r.Body)
	defer joinBody()
	if e = core.state().deps.Authority.RequireCurrentSession(ctx, foundation.Tx{}, r.Actor); e != nil {
		return c.ProfileView{}, e
	}
	image, e := readAvatarInput(ctx, r)
	if e != nil {
		return c.ProfileView{}, e
	}
	defer clear(image.jpeg)
	mac, e := p.profileMAC(r.ProfileMutation, "avatar-put", r.MediaType, image.originalDigest.String())
	if e != nil {
		return c.ProfileView{}, e
	}
	change, cmd, e := p.planAvatar(ctx, r.ProfileMutation, mac, op)
	if e != nil {
		return c.ProfileView{}, e
	}
	if cmd.phase == "committed" {
		return p.currentProfile(ctx, r.Actor)
	}
	owner := avatarOwner(change.user)
	expected := digest(image.jpeg)
	prepared, e := p.state().objects.PreparePayload(ctx, r.Actor, owner, "image/jpeg", int64(len(image.jpeg)), &expected, io.NopCloser(bytes.NewReader(image.jpeg)))
	if e != nil {
		return c.ProfileView{}, portError(e)
	}
	defer p.state().objects.DiscardPrepared(prepared)
	meta := foundation.CommandMeta{IdempotencyKey: foundation.IdempotencyKey(change.id)}
	if meta.RequestID.Validate() != nil {
		meta.RequestID, e = foundation.NewID[foundation.Request]()
		if e != nil {
			return c.ProfileView{}, unavailable(e)
		}
	}
	request, e := oc.NewOwnerAccess(oc.AccessRequestDetails{Operation: oc.ReserveAccess, Actor: r.Actor, Owner: owner, Intent: identity.Mutate, Command: &meta, Prepared: prepared})
	if e != nil {
		return c.ProfileView{}, e
	}
	plan, e := p.state().objects.DiscoverAccess(ctx, request)
	if e != nil {
		return c.ProfileView{}, portError(e)
	}
	cause, _ := foundation.NewCommandsCause(cmd.identity)
	var handle oc.UploadAttempt
	result := core.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		locked, e := p.state().objects.AcquireAccessPlansInTx(ctx, tx, []oc.AccessLockPlan{plan}, profileLocks(r.ProfileMutation, cmd.identity))
		if e != nil {
			return portError(e)
		}
		if e = core.state().deps.Authority.RequireCurrentSession(ctx, tx, r.Actor); e != nil {
			return e
		}
		x, e := core.state().store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		current, e := loadAvatarChange(ctx, x, change.id)
		if e != nil {
			return e
		}
		if current.phase != "preparing" || current.object != "" {
			return fault(foundation.ResourceBusy, nil)
		}
		handle, e = p.state().objects.ReserveUploadInTx(ctx, tx, r.Actor, owner, meta, prepared, plan, locked)
		if e != nil {
			return portError(e)
		}
		d := handle.Details()
		_, e = x.Exec(ctx, `UPDATE agenteam_account.avatar_changes SET new_object_id=$2,upload_id=$3,attempt_id=$4 WHERE id=$1`, change.id, d.ObjectID.String(), d.UploadID.String(), d.ID.String())
		return portError(e)
	})
	if result.State() == foundation.Unknown {
		confirmed, err := p.confirmAvatar(ctx, r.ProfileMutation, cmd.identity, mac)
		if err != nil || confirmed.object == "" {
			return c.ProfileView{}, confirmationError(result, err)
		}
		change = confirmed
		handle, e = change.handle()
		if e != nil {
			return c.ProfileView{}, confirmationError(result, e)
		}
	} else if e = resultError(result); e != nil {
		return c.ProfileView{}, e
	} else {
		change.object = handle.Details().ObjectID.String()
		change.upload = handle.Details().UploadID.String()
		change.attempt = handle.Details().ID.String()
	}
	handle, e = p.state().objects.UploadPrepared(ctx, r.Actor, owner, prepared, handle)
	if e != nil {
		return c.ProfileView{}, portError(e)
	}
	request, _ = oc.NewOwnerAccess(oc.AccessRequestDetails{Operation: oc.PublishAccess, Actor: r.Actor, Owner: owner, Intent: identity.Mutate, Attempt: handle})
	plan, e = p.state().objects.DiscoverAccess(ctx, request)
	if e != nil {
		return c.ProfileView{}, portError(e)
	}
	result = core.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		locked, e := p.state().objects.AcquireAccessPlansInTx(ctx, tx, []oc.AccessLockPlan{plan}, profileLocks(r.ProfileMutation, cmd.identity))
		if e != nil {
			return portError(e)
		}
		x, e := core.state().store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		current, e := loadAvatarChange(ctx, x, change.id)
		if e != nil {
			return e
		}
		if current.phase != "preparing" || current.object != change.object {
			return fault(foundation.InvalidState, nil)
		}
		if _, e = p.state().objects.PublishVerifiedInTx(ctx, tx, r.Actor, owner, handle, plan, locked); e != nil {
			return portError(e)
		}
		_, e = x.Exec(ctx, `UPDATE agenteam_account.avatar_changes SET phase='published' WHERE id=$1`, change.id)
		return portError(e)
	})
	if result.State() == foundation.Unknown {
		confirmed, err := p.confirmAvatar(ctx, r.ProfileMutation, cmd.identity, mac)
		if err != nil || confirmed.phase != "published" {
			return c.ProfileView{}, confirmationError(result, err)
		}
		change = confirmed
	} else if e = resultError(result); e != nil {
		return c.ProfileView{}, e
	} else {
		change.phase = "published"
	}
	return p.applyAvatar(ctx, r.ProfileMutation, cmd, change, mac)
}
func (p *ProfileService) DeleteAvatar(ctx context.Context, r c.ProfileMutation) (c.ProfileView, error) {
	if e := r.Validate(); e != nil {
		return c.ProfileView{}, e
	}
	core := p.state().core
	op, e := core.begin(ctx, false)
	if e != nil {
		return c.ProfileView{}, e
	}
	defer core.finish(op)
	ctx = op.ctx
	mac, e := p.profileMAC(r, "avatar-delete")
	if e != nil {
		return c.ProfileView{}, e
	}
	change, cmd, e := p.planAvatar(ctx, r, mac, op)
	if e != nil {
		return c.ProfileView{}, e
	}
	if cmd.phase == "committed" {
		return p.currentProfile(ctx, r.Actor)
	}
	return p.applyAvatar(ctx, r, cmd, change, mac)
}
func (p *ProfileService) planAvatar(ctx context.Context, r c.ProfileMutation, mac commandMAC, op *operation) (avatarChange, commandRecord, error) {
	core := p.state().core
	st := core.state()
	key, e := profileIdentity(r, "avatar-update")
	if e != nil {
		return avatarChange{}, commandRecord{}, e
	}
	cause, _ := foundation.NewCommandsCause(key)
	id, e := foundation.NewID[c.Command]()
	if e != nil {
		return avatarChange{}, commandRecord{}, unavailable(e)
	}
	local := &avatarLocal{op: op, identity: key}
	p.state().mu.Lock()
	p.state().local[id.String()] = local
	p.state().mu.Unlock()
	var change avatarChange
	var cmd commandRecord
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, profileLocks(r, key)); e != nil {
			return unavailable(e)
		}
		current, e := st.deps.Authority.current(ctx, tx, r.Actor)
		if e != nil {
			return e
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		old, e := loadCommand(ctx, x, string(digest([]byte(key.Canonical()))), true)
		if e == nil {
			if e = core.checkCommand(old, key, mac); e != nil {
				return e
			}
			cmd = old
			change, e = loadAvatarChange(ctx, x, old.id.String())
			if e != nil {
				return e
			}
			if old.phase == "committed" {
				return nil
			}
			if old.phase != "planned" {
				return fault(foundation.InvalidState, nil)
			}
			return fault(foundation.ResourceBusy, nil)
		}
		if !hasFaultCode(e, foundation.NotFound) {
			return e
		}
		if current.user.user.Version != r.ExpectedVersion {
			return fault(foundation.VersionConflict, nil)
		}
		if r.ExpectedVersion == foundation.Version(math.MaxInt64) {
			return fault(foundation.InvalidState, nil)
		}
		previous, e := currentAvatar(ctx, x, r.Actor.Details().UserID)
		if e != nil {
			return e
		}
		if e = p.insertProfileCommand(ctx, x, r, key, id, mac); e != nil {
			return e
		}
		prev := ""
		if previous.Validate() == nil {
			prev = previous.String()
		}
		_, e = x.Exec(ctx, `INSERT INTO agenteam_account.avatar_changes(id,command_id,user_id,previous_object_id,origin_process_id,phase,version) VALUES($1,$1,$2,$3,$4,'preparing',$5)`, id.String(), r.Actor.Details().UserID, null(prev), st.process.String(), int64(r.ExpectedVersion))
		if e != nil {
			return portError(e)
		}
		cmd, e = loadCommand(ctx, x, id.String(), false)
		if e != nil {
			return e
		}
		change, e = loadAvatarChange(ctx, x, id.String())
		return e
	})
	if result.State() == foundation.Unknown {
		confirmed, err := p.confirmAvatar(ctx, r, key, mac)
		if err != nil {
			return change, cmd, confirmationError(result, err)
		}
		if confirmed.id != id.String() {
			return change, cmd, confirmationError(result, nil)
		}
		change = confirmed
		cmd, e = loadCommand(ctx, st.store, confirmed.command, false)
		if e != nil {
			return change, cmd, confirmationError(result, e)
		}
	} else if e = resultError(result); e != nil {
		p.state().mu.Lock()
		delete(p.state().local, id.String())
		p.state().mu.Unlock()
		return change, cmd, e
	}
	if change.id != id.String() {
		p.state().mu.Lock()
		delete(p.state().local, id.String())
		p.state().mu.Unlock()
	}
	return change, cmd, nil
}
func (p *ProfileService) confirmAvatar(ctx context.Context, r c.ProfileMutation, key foundation.CommandIdentity, mac commandMAC) (avatarChange, error) {
	st := p.state().core.state()
	cause, _ := foundation.NewCommandsCause(key)
	var out avatarChange
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, profileLocks(r, key)); e != nil {
			return unavailable(e)
		}
		if e := st.deps.Authority.RequireCurrentSession(ctx, tx, r.Actor); e != nil {
			return e
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		cmd, e := loadCommand(ctx, x, string(digest([]byte(key.Canonical()))), true)
		if e != nil {
			return e
		}
		if e = p.state().core.checkCommand(cmd, key, mac); e != nil {
			return e
		}
		out, e = loadAvatarChange(ctx, x, cmd.id.String())
		return e
	})
	return out, resultError(result)
}
func (p *ProfileService) applyAvatar(ctx context.Context, r c.ProfileMutation, cmd commandRecord, change avatarChange, mac commandMAC) (c.ProfileView, error) {
	core := p.state().core
	st := core.state()
	owner := avatarOwner(change.user)
	var plans []oc.AccessLockPlan
	var attach, release oc.AccessLockPlan
	var cleanup oc.ObjectCleanupCause
	var oldID, newID oc.ObjectID
	var e error
	if change.object != "" {
		newID, e = parseID[oc.StoredObject](change.object)
		if e != nil {
			return c.ProfileView{}, e
		}
		request, _ := oc.NewOwnerAccess(oc.AccessRequestDetails{Operation: oc.AttachAccess, Actor: r.Actor, Owner: owner, Intent: identity.Mutate, ObjectID: newID})
		attach, e = p.state().objects.DiscoverAccess(ctx, request)
		if e != nil {
			return c.ProfileView{}, portError(e)
		}
		plans = append(plans, attach)
	}
	if change.previous != "" {
		oldID, e = parseID[oc.StoredObject](change.previous)
		if e != nil {
			return c.ProfileView{}, e
		}
		original, err := loadAvatarObject(ctx, st.store, oldID)
		if err != nil {
			return c.ProfileView{}, err
		}
		upload, e := parseID[oc.Upload](original.upload)
		if e != nil {
			return c.ProfileView{}, e
		}
		cleanup, e = avatarCause(change.id, change.user, oc.ReplacedObject)
		if e != nil {
			return c.ProfileView{}, e
		}
		request, e := oc.NewCleanupReleaseAccess(oc.AccessRequestDetails{Operation: oc.ReleaseForCleanupAccess, Cleanup: cleanup, ObjectID: oldID, UploadID: upload})
		if e != nil {
			return c.ProfileView{}, e
		}
		release, e = p.state().objects.DiscoverAccess(ctx, request)
		if e != nil {
			return c.ProfileView{}, portError(e)
		}
		plans = append(plans, release)
	}
	cause, _ := foundation.NewCommandsCause(cmd.identity)
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		var locked oc.LockedAccess
		var e error
		if len(plans) > 0 {
			locked, e = p.state().objects.AcquireAccessPlansInTx(ctx, tx, plans, profileLocks(r, cmd.identity))
		} else {
			e = st.store.AcquireAll(ctx, tx, profileLocks(r, cmd.identity))
		}
		if e != nil {
			return portError(e)
		}
		session, e := st.deps.Authority.current(ctx, tx, r.Actor)
		if e != nil {
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
		if e = core.checkCommand(current, cmd.identity, mac); e != nil {
			return e
		}
		if current.phase == "committed" {
			return nil
		}
		if current.phase != "planned" {
			return fault(foundation.InvalidState, nil)
		}
		now, e := loadAvatarChange(ctx, x, change.id)
		if e != nil {
			return e
		}
		if now.phase != change.phase || now.object != change.object || now.previous != change.previous {
			return fault(foundation.ResourceBusy, nil)
		}
		if session.user.user.Version != r.ExpectedVersion {
			return fault(foundation.VersionConflict, nil)
		}
		previous, e := currentAvatar(ctx, x, change.user)
		if e != nil {
			return e
		}
		actual := ""
		if previous.Validate() == nil {
			actual = previous.String()
		}
		if actual != change.previous {
			return fault(foundation.ResourceBusy, nil)
		}
		if newID.Validate() == nil {
			if _, e = p.state().objects.AttachObjectInTx(ctx, tx, r.Actor, owner, newID, attach, locked); e != nil {
				return portError(e)
			}
		}
		if _, e = x.Exec(ctx, `UPDATE agenteam_account.users SET avatar_object_id=$2,version=version+1,updated_at=clock_timestamp() WHERE id=$1`, change.user, null(change.object)); e != nil {
			return portError(e)
		}
		if _, e = x.Exec(ctx, `UPDATE agenteam_account.avatar_changes SET phase='applied',completed_at=clock_timestamp() WHERE id=$1`, change.id); e != nil {
			return portError(e)
		}
		if e = completeCommand(ctx, x, cmd.id.String(), int64(r.ExpectedVersion)+1); e != nil {
			return e
		}
		if oldID.Validate() == nil {
			if _, e = x.Exec(ctx, `INSERT INTO agenteam_account.avatar_cleanup(id,user_id,object_id,change_id,phase) VALUES($1,$2,$3,$1,'pending')`, change.id, change.user, oldID.String()); e != nil {
				return portError(e)
			}
			if e = p.state().objects.ReleaseForCleanupInTx(ctx, tx, cleanup, oldID, release, locked); e != nil {
				return portError(e)
			}
		}
		return core.mutationAudit(ctx, tx, r.Actor, ac.AccountAvatarUpdate, ac.UserResource, change.user, change.id, ac.AccountMetadataFields{UserID: change.user, Version: r.ExpectedVersion + 1, ChangedFields: []ac.AccountChangedField{ac.AvatarChanged}, Phase: ac.AccountUpdated})
	})
	if result.State() == foundation.Unknown {
		confirmed, err := p.confirmAvatar(ctx, r, cmd.identity, mac)
		if err != nil || confirmed.phase != "applied" {
			return c.ProfileView{}, confirmationError(result, err)
		}
	} else if e = resultError(result); e != nil {
		return c.ProfileView{}, e
	}
	return p.currentProfile(ctx, r.Actor)
}
