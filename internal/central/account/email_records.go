package account

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

type EmailProofReceipt struct {
	RequestID   string             `json:"request_id"`
	ExpiresAt   foundation.Instant `json:"expires_at"`
	ResendAfter foundation.Instant `json:"resend_after"`
}
type EmailChangeReceipt struct {
	Version   foundation.Version `json:"version"`
	Email     string             `json:"email"`
	CreatedAt foundation.Instant `json:"created_at"`
}

// Inputs are service values; the HTTP boundary decodes sensitive fields into
// SecretMaterial and destroys them after the synchronous operation joins.
type EmailProofRequest struct {
	Actor                           identity.Actor
	Key                             foundation.IdempotencyKey
	Kind, RequestID, GrantID, Email string
	ExpectedVersion                 foundation.Version
	Password, Authorization         sc.SecretMaterial
}

func (r EmailProofRequest) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "email_proof_request")
}
func (r EmailProofRequest) MarshalJSON() ([]byte, error) { return []byte(`"email_proof_request"`), nil }
func (r EmailProofRequest) LogValue() slog.Value         { return slog.StringValue("email_proof_request") }

type emailRequest struct {
	id, user, session, kind, original, target, grant, authorization, phase, kid, ref string
	userVersion, passwordVersion, passkeyVersion, version                            int64
	attempts, deliveries                                                             int
	mac                                                                              []byte
	created, expires, lastDelivery                                                   time.Time
	live                                                                             bool
}

func loadEmailRequest(ctx context.Context, x postgres.SQLExecutor, id string) (emailRequest, error) {
	var r emailRequest
	e := x.QueryRow(ctx, `SELECT id::text,user_id::text,coalesce(session_id::text,''),kind,original_email,target_email,user_version,password_version,passkey_version,coalesce(grant_id::text,''),coalesce(authorization_id::text,''),phase,coalesce(code_kid,''),code_mac,coalesce(material_ref::text,''),attempts,deliveries,version,created_at,expires_at,last_delivery_at,expires_at>clock_timestamp() FROM agenteam_account.email_requests WHERE id=$1`, id).Scan(&r.id, &r.user, &r.session, &r.kind, &r.original, &r.target, &r.userVersion, &r.passwordVersion, &r.passkeyVersion, &r.grant, &r.authorization, &r.phase, &r.kid, &r.mac, &r.ref, &r.attempts, &r.deliveries, &r.version, &r.created, &r.expires, &r.lastDelivery, &r.live)
	if errors.Is(e, pgx.ErrNoRows) {
		return r, field("/code", "INVALID_OR_EXPIRED")
	}
	return r, portError(e)
}
func loadEmailLink(ctx context.Context, x postgres.SQLExecutor, kind c.TokenKind, id string) (linkRecord, error) {
	r, e := loadEmailRequest(ctx, x, id)
	if e != nil {
		return linkRecord{}, fault(foundation.ResourceDeleted, e)
	}
	if (kind == c.EmailNoticeToken) != (r.kind == "old_email_notice") {
		return linkRecord{}, fault(foundation.ResourceDeleted, nil)
	}
	var current bool
	e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.users u WHERE u.id=$1 AND ($4='old_email_notice' OR (u.password_version=$2 AND u.passkey_version=$3 AND u.email=$5 AND u.version=$6)))`, r.user, r.passwordVersion, r.passkeyVersion, r.kind, r.original, r.userVersion).Scan(&current)
	if e != nil {
		return linkRecord{}, unavailable(e)
	}
	return linkRecord{id: r.id, kind: string(kind), email: r.target, user: r.user, ref: r.ref, version: r.version, passwordVersion: r.passwordVersion, created: r.created, expires: r.expires, lastDelivery: r.lastDelivery, live: r.live && (r.phase == "pending" || r.kind == "old_email_notice" && r.phase == "verified") && current}, nil
}
func isEmailDeliveryCommand(name string) bool {
	switch name {
	case "passkey-recovery-request", "email-change-request", "email-initialize-request", "operator-recovery-request", "email-code-resend":
		return true
	}
	return false
}
func isEmailDelivery(kind string) bool {
	return kind == string(c.EmailCodeDelivery) || kind == string(c.EmailNoticeDelivery)
}
func emailRequestName(kind string) string {
	switch kind {
	case "passkey_recovery":
		return "passkey-recovery-request"
	case "email_change":
		return "email-change-request"
	case "initialization":
		return "email-initialize-request"
	case "operator_recovery":
		return "operator-recovery-request"
	}
	return ""
}
func emailReceipt(r emailRequest) EmailProofReceipt {
	return EmailProofReceipt{r.id, instant(r.expires), instant(r.lastDelivery.Add(time.Minute))}
}

// The random salt is held inside Secret ciphertext. It prevents the service
// write binding's value digest from becoming a six-digit offline code oracle.
func newEmailCodeMaterial(kind string) (sc.SecretMaterial, []byte, error) {
	n, e := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if e != nil {
		return sc.SecretMaterial{}, nil, unavailable(e)
	}
	code := []byte(fmt.Sprintf("%06d", n.Int64()))
	salt := make([]byte, 32)
	if _, e = rand.Read(salt); e != nil {
		clear(code)
		return sc.SecretMaterial{}, nil, unavailable(e)
	}
	defer clear(salt)
	b, e := json.Marshal(struct{ Code, Salt, Purpose string }{string(code), base64.RawURLEncoding.EncodeToString(salt), kind})
	if e != nil {
		clear(code)
		return sc.SecretMaterial{}, nil, unavailable(e)
	}
	defer clear(b)
	m, e := sc.NewSecretMaterial(b)
	if e != nil {
		clear(code)
		return sc.SecretMaterial{}, nil, portError(e)
	}
	return m, code, nil
}
func validEmailCode(b []byte) bool {
	if len(b) != 6 {
		return false
	}
	for _, v := range b {
		if v < '0' || v > '9' {
			return false
		}
	}
	return true
}
func (s *Service) emailCodeMAC(kid string, r emailRequest, code []byte) ([]byte, error) {
	return s.state().keys.mac(kid, "email-code-v1", []byte(r.id), []byte(r.user), []byte(r.session), []byte(r.kind), []byte(r.target), []byte(fmt.Sprint(r.version)), code)
}

func emailRate(ctx context.Context, x postgres.SQLExecutor, user, kind string, send bool) error {
	_, e := x.Exec(ctx, `INSERT INTO agenteam_account.email_rate_limits(user_id,kind) VALUES($1,$2) ON CONFLICT(user_id,kind) DO NOTHING`, user, kind)
	if e != nil {
		return unavailable(e)
	}
	_, e = x.Exec(ctx, `UPDATE agenteam_account.email_rate_limits SET window_started_at=clock_timestamp(),failures=0,deliveries=0 WHERE user_id=$1 AND kind=$2 AND window_started_at<=clock_timestamp()-interval '10 minutes'`, user, kind)
	if e != nil {
		return unavailable(e)
	}
	var allowed bool
	e = x.QueryRow(ctx, `SELECT failures<5 AND (NOT $3 OR (deliveries<5 AND (last_delivery_at IS NULL OR last_delivery_at<=clock_timestamp()-interval '60 seconds'))) FROM agenteam_account.email_rate_limits WHERE user_id=$1 AND kind=$2 FOR UPDATE`, user, kind, send).Scan(&allowed)
	if e != nil {
		return unavailable(e)
	}
	if !allowed {
		return fault(foundation.RateLimited, nil)
	}
	return nil
}

// retireEmailMaterialInTx preserves the request as proof/audit history. Only
// its previous exact Secret reference is released; active leases still join
// through the existing mail/Secret cleanup lifecycle before deletion succeeds.
func (s *Service) retireEmailMaterialInTx(ctx context.Context, tx foundation.Tx, p linkRemoval, guard *mailGuard) error {
	if e := s.state().deps.Authority.requireMailExclusive(guard); e != nil {
		return e
	}
	st := s.state()
	if e := st.store.RequireHeldLocks(ctx, tx, p.locks()); e != nil {
		return unavailable(e)
	}
	x, e := st.store.InTx(tx)
	if e != nil {
		return unavailable(e)
	}
	current, e := loadEmailRequest(ctx, x, p.link.id)
	if e != nil {
		return e
	}
	if current.ref != p.link.ref || current.version != p.link.version {
		return fault(foundation.ResourceBusy, nil)
	}
	_, e = x.Exec(ctx, `INSERT INTO agenteam_account.material_cleanup(id,owner_kind,owner_id,credential_id,purpose,phase,version,secret_command_digest) VALUES($1,'email_code',$2,$3,'system','reference',1,$4) ON CONFLICT(owner_kind,owner_id,credential_id) DO NOTHING`, p.id, p.link.id, p.link.ref, string(p.digest))
	if e != nil {
		return unavailable(e)
	}
	if _, e = st.deps.Secrets.ApplyUsageInTx(ctx, tx, p.request, p.deps); e != nil {
		return portError(e)
	}
	_, e = x.Exec(ctx, `UPDATE agenteam_account.material_cleanup SET phase='delete' WHERE owner_kind='email_code' AND owner_id=$1 AND credential_id=$2 AND phase='reference'`, p.link.id, p.link.ref)
	if e != nil {
		return unavailable(e)
	}
	_, e = x.Exec(ctx, `UPDATE agenteam_account.mail_jobs SET phase='cancelled',reason='token_invalid',version=version+1,completed_at=clock_timestamp() WHERE intent_id IN (SELECT id FROM agenteam_account.delivery_intents WHERE kind='email_code' AND link_id=$1) AND phase IN ('pending','retry_wait')`, p.link.id)
	return portError(e)
}
