package account

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type smtpRecord struct {
	ID                                    string
	Version                               int64
	Configured, Enabled                   bool
	Host, Mode, Username, From, Name, Ref string
	Port                                  int
	Retries, Interval                     int64
}

func loadSMTP(ctx context.Context, x postgres.SQLExecutor) (smtpRecord, error) {
	var r smtpRecord
	e := x.QueryRow(ctx, `SELECT id::text,version,configured,enabled,coalesce(host,''),coalesce(port,0),coalesce(tls_mode,''),coalesce(auth_username,''),coalesce(from_address,''),sender_name,coalesce(password_ref::text,''),auto_retry_count,retry_interval_seconds FROM agenteam_account.smtp_settings WHERE singleton`).Scan(&r.ID, &r.Version, &r.Configured, &r.Enabled, &r.Host, &r.Port, &r.Mode, &r.Username, &r.From, &r.Name, &r.Ref, &r.Retries, &r.Interval)
	return r, portError(e)
}

type mailRecord struct {
	Job, Intent, Kind, Link, User, Initiator, Recipient  string
	JobPhase, CurrentAttempt                             string
	JobVersion, JobFence, Fence, Attempts                int64
	Attempt, Process, Protocol, Result, Channel          string
	ConfigVersion, PasswordVersion                       int64
	TokenRef, CredentialRef, TokenLease, CredentialLease string
	Joined, Terminal                                     bool
	Created                                              time.Time
	Origin                                               string
	RootBinding                                          foundation.Digest
	origin                                               deliveryOrigin
}

func loadMail(ctx context.Context, x postgres.SQLExecutor, job, attempt string) (mailRecord, error) {
	var r mailRecord
	e := x.QueryRow(ctx, `SELECT j.id::text,i.id::text,i.kind,coalesce(i.link_id::text,''),CASE WHEN i.kind='password_reset' THEN coalesce(c.user_id::text,'') ELSE '' END,coalesce(c.password_version,0),i.initiator_id::text,coalesce(i.recipient,''),j.phase,coalesce(j.current_attempt_id::text,''),j.version,j.fence,coalesce(a.fence,j.fence),j.attempts,coalesce(a.id::text,''),coalesce(a.process_id::text,''),coalesce(a.phase,''),coalesce(a.result,''),coalesce(a.channel,''),coalesce(a.config_version,0),coalesce(a.token_ref::text,''),coalesce(a.credential_ref::text,''),coalesce(a.token_lease_id::text,''),coalesce(a.credential_lease_id::text,''),coalesce(a.io_joined,false),coalesce(a.terminal,false),j.created_at
FROM agenteam_account.mail_jobs j JOIN agenteam_account.delivery_intents i ON i.id=j.intent_id
LEFT JOIN agenteam_account.commands c ON c.id=i.id
LEFT JOIN agenteam_account.mail_attempts a ON a.job_id=j.id AND a.id=coalesce(nullif($2,'')::uuid,j.current_attempt_id)
WHERE j.id=$1`, job, attempt).Scan(&r.Job, &r.Intent, &r.Kind, &r.Link, &r.User, &r.PasswordVersion, &r.Initiator, &r.Recipient, &r.JobPhase, &r.CurrentAttempt, &r.JobVersion, &r.JobFence, &r.Fence, &r.Attempts, &r.Attempt, &r.Process, &r.Protocol, &r.Result, &r.Channel, &r.ConfigVersion, &r.TokenRef, &r.CredentialRef, &r.TokenLease, &r.CredentialLease, &r.Joined, &r.Terminal, &r.Created)
	if errors.Is(e, pgx.ErrNoRows) {
		return r, fault(foundation.NotFound, nil)
	}
	if e != nil {
		return r, portError(e)
	}
	o, e := loadDeliveryOrigin(ctx, x, r.Intent)
	if e != nil {
		return r, e
	}
	r.Origin = o.root.ID
	r.RootBinding = o.binding()
	r.origin = o
	r.User = o.user()
	r.PasswordVersion = o.rootCommand.passwordVersion
	return r, nil
}
func mailLocks(r mailRecord) []foundation.LockRequest {
	l := []foundation.LockRequest{configLock("account-directory", foundation.Shared), configLock("account-mail", foundation.Shared), recordLock(r.Job), recordLock(r.Intent)}
	for _, id := range []string{r.Attempt, r.Link} {
		if id != "" {
			l = append(l, recordLock(id))
		}
	}
	if r.User != "" {
		l = append(l, userLock(r.User, foundation.Shared))
	}
	return append(l, r.origin.locks()...)
}
func mailBinding(r mailRecord) foundation.Digest {
	// Only immutable claim facts. Mutable stage belongs to the usage plan and
	// is re-read under the same writers at every operation.
	b, _ := json.Marshal(struct {
		Job, Intent, Kind, Link, User, Initiator, Attempt, Process, Channel, TokenRef, CredentialRef, TokenLease, CredentialLease, Origin string
		RootBinding                                                                                                                       foundation.Digest
		Fence, Config, PasswordVersion                                                                                                    int64
	}{r.Job, r.Intent, r.Kind, r.Link, r.User, r.Initiator, r.Attempt, r.Process, r.Channel, r.TokenRef, r.CredentialRef, r.TokenLease, r.CredentialLease, r.Origin, r.RootBinding, r.Fence, r.ConfigVersion, r.PasswordVersion})
	return digest(b)
}
func mailMapping(r mailRecord) foundation.Digest { b, _ := json.Marshal(r); return digest(b) }
func mailCurrent(r mailRecord) error {
	if r.Attempt == "" || r.CurrentAttempt != r.Attempt || r.JobFence != r.Fence || r.Terminal || r.Joined || r.Protocol == "closed" || r.JobPhase != "claimed" && r.JobPhase != "sending" && r.JobPhase != "processing" && r.JobPhase != "unknown" {
		return fault(foundation.InvalidState, nil)
	}
	return nil
}
func mailLive(ctx context.Context, x postgres.SQLExecutor, r mailRecord, cfg smtpRecord) (string, time.Time, error) {
	if cfg.Version != r.ConfigVersion {
		return "", time.Time{}, fault(foundation.ResourceBusy, nil)
	}
	if r.Channel == string(c.SMTPChannel) {
		if !cfg.Configured || !cfg.Enabled || cfg.Ref != r.CredentialRef {
			return "", time.Time{}, fault(foundation.InvalidState, nil)
		}
	} else if cfg.Configured || r.Kind == string(c.TestDelivery) {
		return "", time.Time{}, fault(foundation.InvalidState, nil)
	}
	if r.Kind == string(c.TestDelivery) {
		return r.Recipient, time.Time{}, nil
	}
	link, e := loadLink(ctx, x, c.TokenKind(r.Kind), r.Link)
	if e != nil {
		return "", time.Time{}, e
	}
	if !link.live || link.ref != r.TokenRef {
		return "", time.Time{}, fault(foundation.ResourceDeleted, nil)
	}
	if r.Kind == string(c.ResetDelivery) {
		if link.user != r.User || link.passwordVersion != r.PasswordVersion || r.PasswordVersion < 1 {
			return "", time.Time{}, fault(foundation.ResourceDeleted, nil)
		}
		u, e := loadUser(ctx, x, link.user)
		if e != nil {
			return "", time.Time{}, e
		}
		if int64(u.passwordVersion) != link.passwordVersion {
			return "", time.Time{}, fault(foundation.ResourceDeleted, nil)
		}
		link.email = u.user.Email
	}
	return link.email, link.expires, nil
}
