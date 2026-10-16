//go:build integration

package app

import (
	"context"
	"errors"
	"testing"
	"time"

	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

// This checks the real migrated constraint before publishing the owned runtime.
// Both rows are rolled back; neither authorizes a browser or seeds a Passkey.
func previewPasskeyConstraints(t *testing.T, parent context.Context, db *pgfixture.Database) {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	connection := db.Connect(t)
	tx, err := connection.Begin(ctx)
	previewCheck(t, "Passkey constraint transaction", err)
	defer tx.Rollback(context.Background())
	var user, session string
	var passwordVersion, passkeyVersion int64
	err = tx.QueryRow(ctx, `SELECT u.id::text,s.id::text,u.password_version,u.passkey_version FROM agenteam_account.users u JOIN agenteam_account.sessions s ON s.user_id=u.id WHERE s.revoked_at IS NULL ORDER BY s.issued_at DESC LIMIT 1`).Scan(&user, &session, &passwordVersion, &passkeyVersion)
	previewCheck(t, "Passkey constraint current owned account", err)
	_, err = tx.Exec(ctx, `SAVEPOINT invalid_grant`)
	previewCheck(t, "Passkey constraint savepoint", err)
	_, err = tx.Exec(ctx, `INSERT INTO agenteam_account.reauth_grants(id,user_id,session_id,purpose,action,method,password_version,passkey_version,created_at,expires_at) SELECT $1,$2,$3,'change_email',NULL,'mail_recovery',$4,$5,n,n+interval '5 minutes' FROM (SELECT clock_timestamp() n) z`, guardID[struct{}](t).String(), user, session, passwordVersion, passkeyVersion)
	var pgerr *pgconn.PgError
	if !errors.As(err, &pgerr) || pgerr.Code != "23514" {
		t.Fatal("mail recovery NULL action grant was not rejected by PostgreSQL")
	}
	_, err = tx.Exec(ctx, `ROLLBACK TO SAVEPOINT invalid_grant`)
	previewCheck(t, "Passkey constraint rejected-row rollback", err)
	_, err = tx.Exec(ctx, `INSERT INTO agenteam_account.reauth_grants(id,user_id,session_id,purpose,action,method,password_version,passkey_version,created_at,expires_at) SELECT $1,$2,$3,'manage_passkeys','register','mail_recovery',$4,$5,n,n+interval '5 minutes' FROM (SELECT clock_timestamp() n) z`, guardID[struct{}](t).String(), user, session, passwordVersion, passkeyVersion)
	previewCheck(t, "Passkey constraint legitimate recovery grant", err)
	previewCheck(t, "Passkey constraint rollback", tx.Rollback(ctx))
	t.Log("Passkey SQL constraint: mail-recovery change-email/NULL rejected, registration accepted; both rolled back")
}
