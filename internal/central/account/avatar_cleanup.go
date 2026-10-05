package account

import (
	"context"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func (p *ProfileService) cleanCancelledAvatar(ctx context.Context, change avatarChange) error {
	object, e := parseID[oc.StoredObject](change.object)
	if e != nil {
		return e
	}
	actor, e := serviceActor(identity.ObjectMaintenance, change.cleanup)
	if e != nil {
		return e
	}
	// Reserved uploads use the original maintenance cancellation port. An
	// existing-owner Publish already installed a canonical and needs the explicit
	// irreversible reference-cleanup port instead.
	result, e := p.state().objects.CancelUploadWithinBudget(ctx, actor, avatarOwner(change.user), foundation.IdempotencyKey(change.id))
	if e == nil && result.Cleanup == oc.CleanupCompleted {
		return p.finishAvatarCleanup(ctx, change, false)
	}
	if e != nil && !hasFaultCode(e, foundation.InvalidState) {
		return portError(e)
	}
	cause, e := avatarCause(change.cleanup, change.user, oc.CancelledUpload)
	if e != nil {
		return e
	}
	upload, e := parseID[oc.Upload](change.upload)
	if e != nil {
		return e
	}
	request, e := oc.NewCleanupReleaseAccess(oc.AccessRequestDetails{Operation: oc.ReleaseForCleanupAccess, Cleanup: cause, ObjectID: object, UploadID: upload})
	if e != nil {
		return e
	}
	plan, e := p.state().objects.DiscoverAccess(ctx, request)
	if e != nil {
		return portError(e)
	}
	txCause, e := recoveryCause("avatar-gate")
	if e != nil {
		return e
	}
	st := p.state().core.state()
	committed := st.store.WithinTx(ctx, txCause, func(ctx context.Context, tx foundation.Tx) error {
		locked, e := p.state().objects.AcquireAccessPlansInTx(ctx, tx, []oc.AccessLockPlan{plan}, nil)
		if e != nil {
			return portError(e)
		}
		if e = p.state().objects.ReleaseForCleanupInTx(ctx, tx, cause, object, plan, locked); e != nil {
			return portError(e)
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		_, e = x.Exec(ctx, `UPDATE agenteam_account.avatar_changes SET phase='cleanup_pending' WHERE id=$1 AND phase='cancelled'`, change.id)
		return portError(e)
	})
	if e = resultError(committed); e != nil {
		return e
	}
	cleaned, e := p.state().objects.DeleteUnreferencedWithinBudget(ctx, cause, object)
	if e != nil {
		return portError(e)
	}
	if cleaned.State != oc.CleanupCompleted {
		return fault(foundation.ResourceBusy, nil)
	}
	return p.finishAvatarCleanup(ctx, change, false)
}
func (p *ProfileService) cleanReplacedAvatar(ctx context.Context, id string) error {
	st := p.state().core.state()
	var user, object, changeID, phase string
	e := st.store.QueryRow(ctx, `SELECT user_id::text,object_id::text,change_id::text,phase FROM agenteam_account.avatar_cleanup WHERE id=$1`, id).Scan(&user, &object, &changeID, &phase)
	if e != nil {
		return unavailable(e)
	}
	if phase == "completed" {
		return nil
	}
	change, e := loadAvatarChange(ctx, st.store, changeID)
	if e != nil {
		return e
	}
	if change.user != user || change.previous != object || id != changeID {
		return fault(foundation.InvalidState, nil)
	}
	cause, e := avatarCause(id, user, oc.ReplacedObject)
	if e != nil {
		return e
	}
	oid, e := parseID[oc.StoredObject](object)
	if e != nil {
		return e
	}
	cleaned, e := p.state().objects.DeleteUnreferencedWithinBudget(ctx, cause, oid)
	if e != nil {
		return portError(e)
	}
	if cleaned.State != oc.CleanupCompleted {
		return fault(foundation.ResourceBusy, nil)
	}
	return p.finishAvatarCleanup(ctx, change, true)
}
func (p *ProfileService) finishAvatarCleanup(ctx context.Context, change avatarChange, replaced bool) error {
	st := p.state().core.state()
	cmd, e := loadCommand(ctx, st.store, change.command, false)
	if e != nil {
		return e
	}
	cause, e := recoveryCause("avatar-cleaned")
	if e != nil {
		return e
	}
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		locks := append(commandLocks(cmd), userLock(change.user, foundation.Exclusive))
		if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
			return unavailable(e)
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		now, e := loadAvatarChange(ctx, x, change.id)
		if e != nil {
			return e
		}
		if now.user != change.user || now.object != change.object || now.previous != change.previous {
			return fault(foundation.ResourceBusy, nil)
		}
		if replaced {
			if now.phase != "applied" {
				return fault(foundation.InvalidState, nil)
			}
			_, e = x.Exec(ctx, `UPDATE agenteam_account.avatar_cleanup SET phase='completed',completed_at=clock_timestamp() WHERE id=$1 AND phase='pending'`, change.id)
		} else {
			if now.phase != "cancelled" && now.phase != "cleanup_pending" && now.phase != "completed" {
				return fault(foundation.InvalidState, nil)
			}
			_, e = x.Exec(ctx, `UPDATE agenteam_account.avatar_changes SET phase='completed',completed_at=clock_timestamp() WHERE id=$1`, change.id)
		}
		return portError(e)
	})
	return resultError(result)
}
