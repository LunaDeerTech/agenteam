package account

import (
	"context"
	"errors"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type commandRecord struct {
	id                                                                                                                                                c.CommandID
	identity                                                                                                                                          foundation.CommandIdentity
	name, phase, kid, actorKind, user, session, browser, process, attempt, resource, secretDigest, secretBinding, responseRef, plannedRef, resultCode string
	mac                                                                                                                                               []byte
	passwordVersion, expectedVersion, resultVersion                                                                                                   int64
	responseExpires, browserExpires, completed, created                                                                                               time.Time
}

func loadCommand(ctx context.Context, x postgres.SQLExecutor, id string, byDigest bool) (commandRecord, error) {
	var r commandRecord
	var raw, namespace, owner, key string
	column := "id"
	if byDigest {
		column = "identity_digest"
	}
	err := x.QueryRow(ctx, `SELECT id::text,namespace,owner_id::text,command_key,command_name,phase,semantic_kid,semantic_mac,actor_kind,coalesce(user_id::text,''),coalesce(session_id::text,''),coalesce(browser_id::text,''),origin_process_id::text,coalesce(attempt_id::text,''),coalesce(resource_id::text,''),coalesce(secret_command_digest,''),coalesce(secret_write_binding,''),coalesce(response_secret_ref::text,''),coalesce(planned_secret_ref::text,''),coalesce(result_code,''),coalesce(password_version,0),expected_version,coalesce(result_version,0),coalesce(response_expires_at,'0001-01-01Z'::timestamptz),coalesce(browser_expires_at,'0001-01-01Z'::timestamptz),coalesce(completed_at,'0001-01-01Z'::timestamptz),created_at FROM agenteam_account.commands WHERE `+column+`=$1`, id).Scan(&raw, &namespace, &owner, &key, &r.name, &r.phase, &r.kid, &r.mac, &r.actorKind, &r.user, &r.session, &r.browser, &r.process, &r.attempt, &r.resource, &r.secretDigest, &r.secretBinding, &r.responseRef, &r.plannedRef, &r.resultCode, &r.passwordVersion, &r.expectedVersion, &r.resultVersion, &r.responseExpires, &r.browserExpires, &r.completed, &r.created)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, fault(foundation.NotFound, nil)
	}
	if err != nil {
		return r, unavailable(err)
	}
	r.id, err = parseID[c.Command](raw)
	if err != nil {
		return r, err
	}
	r.identity, err = foundation.NewCommandIdentity(namespace, []string{owner}, r.name, foundation.IdempotencyKey(key))
	if err != nil || len(r.mac) != 32 {
		return commandRecord{}, unavailable(err)
	}
	return r, nil
}
func commandLocks(r commandRecord) []foundation.LockRequest {
	locks := []foundation.LockRequest{commandLock(r.identity), configLock("account-directory", foundation.Shared), configLock("account-security", foundation.Shared), recordLock(r.id.String())}
	if r.user != "" {
		locks = append(locks, userLock(r.user, foundation.Shared))
	}
	if r.attempt != "" {
		locks = append(locks, recordLock(r.attempt))
	}
	if r.resource != "" {
		locks = append(locks, recordLock(r.resource))
	}
	return locks
}
