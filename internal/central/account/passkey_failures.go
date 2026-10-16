package account

import (
	"context"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// An authenticated completion records a failed cryptographic proof in its
// command receipt. Replaying that command does not charge a second attempt.
func passkeyCredentialRejection(ctx context.Context, x postgres.SQLExecutor, operation string) (any, error) {
	if _, e := x.Exec(ctx, `UPDATE agenteam_account.passkey_operations SET failures=least(failures+1,5) WHERE id=$1 AND consumed_command_id IS NULL`, operation); e != nil {
		return nil, unavailable(e)
	}
	return map[string]string{"rejected": "invalid_credential"}, nil
}

// Public invalid assertions are bounded by the originating browser's operation,
// never by the untrusted user handle. This is an attempt counter, not a login
// receipt; no credential, session or successful command is created.
func (s *Service) recordPasskeyLoginFailure(ctx context.Context, browser c.BrowserIdentity, operation string) error {
	cause, e := recoveryCause("passkey-login-failure")
	if e != nil {
		return e
	}
	st := s.state()
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, []foundation.LockRequest{recordLock(browser.ID().String()), recordLock(operation)}); e != nil {
			return unavailable(e)
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		if e = s.validateBrowserInTx(ctx, x, browser); e != nil {
			return e
		}
		_, e = x.Exec(ctx, `UPDATE agenteam_account.passkey_operations SET failures=least(failures+1,5) WHERE id=$1 AND purpose='login' AND browser_id=$2 AND consumed_command_id IS NULL AND expires_at>clock_timestamp()`, operation, browser.ID().String())
		return portError(e)
	})
	return resultError(result)
}
