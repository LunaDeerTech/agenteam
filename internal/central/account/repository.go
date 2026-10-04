package account

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type Store interface {
	postgres.SQLExecutor
	InTx(foundation.Tx) (postgres.SQLExecutor, error)
	WithinTx(context.Context, foundation.TransactionCause, func(context.Context, foundation.Tx) error) foundation.CommitResult
	AcquireAll(context.Context, foundation.Tx, []foundation.LockRequest) error
	RequireHeldLocks(context.Context, foundation.Tx, []foundation.LockRequest) error
}

func recoveryCause(name string) (foundation.TransactionCause, error) {
	id, e := foundation.NewID[struct{}]()
	if e != nil {
		return foundation.TransactionCause{}, unavailable(e)
	}
	return foundation.NewRecoveryCause("account."+name, id.String(), "")
}
func digest(b []byte) foundation.Digest {
	h := sha256.Sum256(b)
	return foundation.Digest("sha256:" + hex.EncodeToString(h[:]))
}
func userLock(id string, mode foundation.LockMode) foundation.LockRequest {
	k, _ := foundation.UserLock(id)
	return foundation.LockRequest{Key: k, Mode: mode}
}
func configLock(name string, mode foundation.LockMode) foundation.LockRequest {
	k, _ := foundation.SystemConfigLock(name)
	return foundation.LockRequest{Key: k, Mode: mode}
}
func commandLock(id foundation.CommandIdentity) foundation.LockRequest {
	k, _ := foundation.CommandLock(id)
	return foundation.LockRequest{Key: k, Mode: foundation.Exclusive}
}
func recordLock(id string) foundation.LockRequest {
	k, _ := foundation.RecordLock(foundation.ReferenceRecordLock, id)
	return foundation.LockRequest{Key: k, Mode: foundation.Exclusive}
}
func null(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func instant(t time.Time) foundation.Instant { v, _ := foundation.NewInstant(t); return v }
func parseID[K any](s string) (foundation.ID[K], error) {
	v, e := foundation.ParseID[K](s)
	if e != nil {
		return v, unavailable(e)
	}
	return v, nil
}

type userRecord struct {
	user            c.User
	phc             string
	passwordVersion foundation.Version
	sequence        foundation.Sequence
}

func loadUser(ctx context.Context, e postgres.SQLExecutor, id string) (userRecord, error) {
	var r userRecord
	var raw string
	var version, pwd, seq int64
	err := e.QueryRow(ctx, `SELECT id::text,email,username,display_name,role,theme,version,initial_password_suggestion,password_phc,password_version,auth_sequence FROM agenteam_account.users WHERE id=$1`, id).Scan(&raw, &r.user.Email, &r.user.Username, &r.user.DisplayName, &r.user.Role, &r.user.Theme, &version, &r.user.InitialPasswordSuggestion, &r.phc, &pwd, &seq)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, fault(foundation.NotFound, nil)
	}
	if err != nil {
		return r, unavailable(err)
	}
	r.user.ID, err = parseID[identity.User](raw)
	r.user.Version = foundation.Version(version)
	r.passwordVersion = foundation.Version(pwd)
	r.sequence = foundation.Sequence(seq)
	if err != nil || r.user.Version.Validate() != nil || r.passwordVersion.Validate() != nil || r.sequence.Validate() != nil || !r.user.Role.Valid() || !r.user.Theme.Valid() {
		return r, unavailable(nil)
	}
	return r, nil
}

type sessionRecord struct {
	session c.Session
	user    userRecord
	kid     string
	checked time.Time
}

func loadCurrentSession(ctx context.Context, e postgres.SQLExecutor, actor identity.Actor) (sessionRecord, error) {
	a := actor.Details()
	var r sessionRecord
	var user, session, reason string
	var issued, absolute, idle time.Time
	var current, revoked bool
	var checked time.Time
	err := e.QueryRow(ctx, `SELECT id::text,user_id::text,csrf_kid,issued_at,absolute_expires_at,LEAST(absolute_expires_at,last_activity_at+idle_seconds*interval '1 second'),revoked_at IS NOT NULL,coalesce(revoked_reason,''),clock_timestamp(),revoked_at IS NULL AND clock_timestamp()<absolute_expires_at AND clock_timestamp()<last_activity_at+idle_seconds*interval '1 second' FROM agenteam_account.sessions WHERE id=$1`, a.SessionID).Scan(&session, &user, &r.kid, &issued, &absolute, &idle, &revoked, &reason, &checked, &current)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && user != a.UserID {
		return r, fault(foundation.Unauthenticated, nil)
	}
	if err != nil {
		return r, unavailable(err)
	}
	if revoked {
		return r, fault(foundation.SessionRevoked, nil)
	}
	if !current {
		return r, fault(foundation.Unauthenticated, nil)
	}
	r.checked = checked
	r.session.ID, err = parseID[identity.Session](session)
	if err != nil {
		return r, err
	}
	r.session.IssuedAt = instant(issued)
	r.session.AbsoluteExpiresAt = instant(absolute)
	r.session.IdleExpiresAt = instant(idle)
	r.user, err = loadUser(ctx, e, user)
	return r, err
}

func canonicalCommandDigest(c foundation.CommandIdentity) (foundation.Digest, error) {
	if c.Validate() != nil {
		return "", invalid()
	}
	d, e := cursor.Digest([]byte(c.Canonical()))
	if e != nil {
		return "", unavailable(e)
	}
	return d, nil
}
