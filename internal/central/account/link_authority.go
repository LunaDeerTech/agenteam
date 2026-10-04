package account

import (
	"context"
	"errors"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

func (a *Authority) linkReferenceMapping(ctx context.Context, x postgres.SQLExecutor, r sc.UsageRequest) (foundation.Digest, []foundation.LockRequest, error) {
	if r.Purpose != sc.System {
		return "", nil, fault(foundation.DependencyUnbound, nil)
	}
	if r.Retain {
		var id string
		e := x.QueryRow(ctx, `SELECT id::text FROM agenteam_account.commands WHERE resource_id=$1 AND planned_secret_ref=$2 AND command_name IN ('invite-create','reset-request') AND phase IN ('planned','committed')`, r.ReferenceOwner, r.Ref.Details().ID.String()).Scan(&id)
		if e != nil {
			return "", nil, fault(foundation.Forbidden, e)
		}
		cmd, e := loadCommand(ctx, x, id, false)
		if e != nil {
			return "", nil, e
		}
		locks := append(commandLocks(cmd), configLock("account-mail", foundation.Shared))
		return digest([]byte(string(commandMapping(cmd)) + "\x00" + r.ReferenceOwner + "\x00" + r.Ref.Details().ID.String())), locks, nil
	}
	var ref string
	e := x.QueryRow(ctx, `SELECT material_ref::text FROM agenteam_account.invitations WHERE id=$1 UNION ALL SELECT material_ref::text FROM agenteam_account.password_resets WHERE id=$1`, r.ReferenceOwner).Scan(&ref)
	if errors.Is(e, pgx.ErrNoRows) {
		e = x.QueryRow(ctx, `SELECT credential_id::text FROM agenteam_account.material_cleanup WHERE owner_id=$1 AND owner_kind IN ('invitation','password_reset') AND credential_id=$2 AND phase<>'completed'`, r.ReferenceOwner, r.Ref.Details().ID.String()).Scan(&ref)
	}
	if e != nil || ref != r.Ref.Details().ID.String() {
		return "", nil, fault(foundation.Forbidden, e)
	}
	return digest([]byte(r.ReferenceOwner + "\x00" + ref)), []foundation.LockRequest{configLock("account-directory", foundation.Exclusive), configLock("account-mail", foundation.Exclusive), recordLock(r.ReferenceOwner)}, nil
}
func (a *Authority) validateLinkReference(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor, r sc.UsageRequest) error {
	if r.Purpose != sc.System {
		return fault(foundation.DependencyUnbound, nil)
	}
	var permitted bool
	if r.Retain {
		e := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.commands c WHERE c.resource_id=$1 AND c.planned_secret_ref=$2 AND c.phase='committed' AND ((c.command_name='invite-create' AND EXISTS(SELECT 1 FROM agenteam_account.invitations i WHERE i.id=c.resource_id AND i.material_ref=c.planned_secret_ref AND i.expires_at>clock_timestamp())) OR (c.command_name='reset-request' AND EXISTS(SELECT 1 FROM agenteam_account.password_resets r JOIN agenteam_account.users u ON u.id=r.user_id WHERE r.id=c.resource_id AND r.material_ref=c.planned_secret_ref AND r.expires_at>clock_timestamp() AND r.password_version=u.password_version))))`, r.ReferenceOwner, r.Ref.Details().ID.String()).Scan(&permitted)
		if e != nil {
			return unavailable(e)
		}
	} else {
		e := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.material_cleanup WHERE owner_id=$1 AND credential_id=$2 AND owner_kind IN ('invitation','password_reset') AND purpose='system' AND phase IN ('reference','leases','delete','completed'))`, r.ReferenceOwner, r.Ref.Details().ID.String()).Scan(&permitted)
		if e != nil {
			return unavailable(e)
		}
	}
	if !permitted {
		return fault(foundation.Forbidden, nil)
	}
	return nil
}
