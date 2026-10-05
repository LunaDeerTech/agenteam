package account

import (
	"context"
	"errors"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type avatarChange struct {
	id, command, user, previous, object, upload, attempt, process, cleanup, phase string
	version                                                                       int64
}

const avatarColumns = `id::text,command_id::text,user_id::text,coalesce(previous_object_id::text,''),coalesce(new_object_id::text,''),coalesce(upload_id::text,''),coalesce(attempt_id::text,''),origin_process_id::text,coalesce(cleanup_cause::text,''),phase,version`

func scanAvatar(row postgres.Row) (avatarChange, error) {
	var v avatarChange
	e := row.Scan(&v.id, &v.command, &v.user, &v.previous, &v.object, &v.upload, &v.attempt, &v.process, &v.cleanup, &v.phase, &v.version)
	if errors.Is(e, pgx.ErrNoRows) {
		return v, fault(foundation.NotFound, nil)
	}
	return v, portError(e)
}
func loadAvatarChange(ctx context.Context, x postgres.SQLExecutor, id string) (avatarChange, error) {
	return scanAvatar(x.QueryRow(ctx, `SELECT `+avatarColumns+` FROM agenteam_account.avatar_changes WHERE id=$1`, id))
}
func loadAvatarObject(ctx context.Context, x postgres.SQLExecutor, id oc.ObjectID) (avatarChange, error) {
	// Each object comes only from one exact Reserve result. A contradictory map
	// is not resolved by selecting whichever row happens to be first.
	rows, e := x.Query(ctx, `SELECT `+avatarColumns+` FROM agenteam_account.avatar_changes WHERE new_object_id=$1 ORDER BY id LIMIT 2`, id.String())
	if e != nil {
		return avatarChange{}, unavailable(e)
	}
	defer rows.Close()
	if !rows.Next() {
		if rows.Err() != nil {
			return avatarChange{}, unavailable(rows.Err())
		}
		return avatarChange{}, fault(foundation.NotFound, nil)
	}
	v, e := scanAvatar(rows)
	if e != nil {
		return v, e
	}
	if rows.Next() {
		return avatarChange{}, fault(foundation.InvalidState, nil)
	}
	return v, portError(rows.Err())
}
func (v avatarChange) handle() (oc.UploadAttempt, error) {
	object, e := parseID[oc.StoredObject](v.object)
	if e != nil {
		return oc.UploadAttempt{}, e
	}
	upload, e := parseID[oc.Upload](v.upload)
	if e != nil {
		return oc.UploadAttempt{}, e
	}
	attempt, e := parseID[oc.Attempt](v.attempt)
	if e != nil {
		return oc.UploadAttempt{}, e
	}
	return oc.NewUploadAttempt(oc.AttemptDetails{ObjectID: object, UploadID: upload, ID: attempt})
}
func avatarOwner(user string) oc.ObjectOwner {
	o, _ := oc.NewObjectOwner(oc.Avatar, user, "")
	return o
}
func avatarCause(id, user string, reason oc.CleanupReason) (oc.ObjectCleanupCause, error) {
	op, e := parseID[oc.CleanupOperation](id)
	if e != nil {
		return oc.ObjectCleanupCause{}, e
	}
	return oc.NewObjectCleanupCause(oc.CleanupDetails{OperationID: op, Owner: avatarOwner(user), Reason: reason})
}
func avatarCommandID(v avatarChange) c.CommandID { id, _ := parseID[c.Command](v.command); return id }
