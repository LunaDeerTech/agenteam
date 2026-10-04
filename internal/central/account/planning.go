package account

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

func (a *Authority) Initialize(ctx context.Context) error {
	cause, e := recoveryCause("initialize")
	if e != nil {
		return e
	}
	s := a.state()
	r := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := s.store.AcquireAll(ctx, tx, []foundation.LockRequest{configLock("account-security", foundation.Exclusive), configLock("account-directory", foundation.Exclusive)}); e != nil {
			return unavailable(e)
		}
		x, e := s.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		for kid, key := range s.keys.data().keys {
			fingerprint := sha256.Sum256(append([]byte("agenteam.account.key.v1\x00"), key[:]...))
			var old []byte
			e = x.QueryRow(ctx, `SELECT fingerprint FROM agenteam_account.account_key_registry WHERE kid=$1`, kid).Scan(&old)
			if errors.Is(e, pgx.ErrNoRows) {
				if _, e = x.Exec(ctx, `INSERT INTO agenteam_account.account_key_registry(kid,fingerprint) VALUES($1,$2)`, kid, fingerprint[:]); e != nil {
					return unavailable(e)
				}
			} else if e != nil {
				return unavailable(e)
			} else if subtle.ConstantTimeCompare(old, fingerprint[:]) != 1 {
				return field("/account_keyring", "KEY_REUSED")
			}
		}
		rows, e := x.Query(ctx, `SELECT kid FROM agenteam_account.account_key_registry r WHERE anonymous_until>clock_timestamp() OR EXISTS(SELECT 1 FROM agenteam_account.commands c WHERE c.semantic_kid=r.kid) OR EXISTS(SELECT 1 FROM agenteam_account.sessions s WHERE s.csrf_kid=r.kid AND s.absolute_expires_at>clock_timestamp()) OR EXISTS(SELECT 1 FROM agenteam_account.auth_failures f WHERE f.kid=r.kid AND f.expires_at>clock_timestamp()) OR EXISTS(SELECT 1 FROM agenteam_account.challenges c WHERE c.subject_kid=r.kid AND c.expires_at>clock_timestamp())`)
		if e != nil {
			return unavailable(e)
		}
		defer rows.Close()
		for rows.Next() {
			var kid string
			if e = rows.Scan(&kid); e != nil {
				return unavailable(e)
			}
			if _, ok := s.keys.data().keys[kid]; !ok {
				return field("/account_keyring", "KEY_REQUIRED")
			}
		}
		if e = rows.Err(); e != nil {
			return unavailable(e)
		}
		rows.Close()
		settings, e := foundation.NewID[c.SettingsRecord]()
		if e != nil {
			return unavailable(e)
		}
		mail, e := foundation.NewID[c.SettingsRecord]()
		if e != nil {
			return unavailable(e)
		}
		if _, e = x.Exec(ctx, `INSERT INTO agenteam_account.account_settings(singleton,id,version,session_idle_seconds,session_absolute_seconds,password_reset_seconds,challenge_after_failures) VALUES(true,$1,1,604800,2592000,1800,5) ON CONFLICT(singleton) DO NOTHING`, settings.String()); e != nil {
			return unavailable(e)
		}
		if _, e = x.Exec(ctx, `INSERT INTO agenteam_account.smtp_settings(singleton,id,version,configured,enabled) VALUES(true,$1,1,false,false) ON CONFLICT(singleton) DO NOTHING`, mail.String()); e != nil {
			return unavailable(e)
		}
		return nil
	})
	return resultError(r)
}
func loadSettings(ctx context.Context, x postgres.SQLExecutor) (c.Settings, error) {
	var v c.Settings
	var raw string
	var version, idle, absolute, reset, challenge int64
	e := x.QueryRow(ctx, `SELECT id::text,version,session_idle_seconds,session_absolute_seconds,password_reset_seconds,challenge_after_failures FROM agenteam_account.account_settings WHERE singleton`).Scan(&raw, &version, &idle, &absolute, &reset, &challenge)
	if e != nil {
		return v, unavailable(e)
	}
	v.ID, e = parseID[c.SettingsRecord](raw)
	v.Version = foundation.Version(version)
	v.SessionIdleSeconds = foundation.Progress(idle)
	v.SessionAbsoluteSeconds = foundation.Progress(absolute)
	v.PasswordResetSeconds = foundation.Progress(reset)
	v.ChallengeAfterFailures = foundation.Progress(challenge)
	if e != nil || v.Validate() != nil {
		return c.Settings{}, unavailable(e)
	}
	return v, nil
}
func (a *Authority) CheckStorage(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if _, e := loadSettings(ctx, a.state().store); e != nil {
		return e
	}
	var count int64
	if e := a.state().store.QueryRow(ctx, `SELECT count(*) FROM agenteam_account.account_key_registry`).Scan(&count); e != nil || count < 1 {
		return unavailable(e)
	}
	return nil
}
