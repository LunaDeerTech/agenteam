package account

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

// SystemHTTPFacade supplies the additional account query/settings surface with
// the deployment's existing cursor ring. It has no mutable late-binding hook.
type SystemHTTPFacade struct {
	core       *Service
	pagination cursor.Keyring
}

func NewSystemHTTPFacade(core *Service, pagination cursor.Keyring) (*SystemHTTPFacade, error) {
	if e := httpValidateCore(core); e != nil {
		return nil, e
	}
	if pagination.Validate() != nil {
		return nil, invalid()
	}
	for _, key := range core.state().keys.data().keys {
		if pagination.ContainsMaterial(key[:]) {
			return nil, invalid()
		}
	}
	return &SystemHTTPFacade{core: core, pagination: pagination}, nil
}

type HTTPListRequest struct {
	Cursor string
	Limit  int
}
type HTTPUserList struct {
	Items      []c.User
	NextCursor string
}
type HTTPInvitation struct {
	ID                   c.InvitationID
	Email                string
	Version              foundation.Version
	CreatedAt, ExpiresAt foundation.Instant
}
type HTTPInvitationList struct {
	Items      []HTTPInvitation
	NextCursor string
}
type HTTPMailJob struct {
	Status    c.MailJobStatus
	Channel   string
	CreatedAt foundation.Instant
}
type HTTPMailJobList struct {
	Items      []HTTPMailJob
	NextCursor string
}
type HTTPAccountSettingsUpdate struct {
	Actor                                                                                    identity.Actor
	Key                                                                                      foundation.IdempotencyKey
	ExpectedVersion                                                                          foundation.Version
	SessionIdleSeconds, SessionAbsoluteSeconds, PasswordResetSeconds, ChallengeAfterFailures foundation.Progress
}

func (f *SystemHTTPFacade) httpRead(ctx context.Context, actor identity.Actor, name string, locks []foundation.LockRequest, fn func(context.Context, postgres.SQLExecutor) error) error {
	if f == nil || f.core == nil || actor.Validate() != nil || actor.Details().Kind != identity.Human {
		return fault(foundation.Unauthenticated, nil)
	}
	op, e := f.core.begin(ctx, false)
	if e != nil {
		return e
	}
	defer f.core.finish(op)
	cause, e := recoveryCause("http-" + name)
	if e != nil {
		return e
	}
	locks = append(locks, userLock(actor.Details().UserID, foundation.Shared))
	st := f.core.state()
	r := st.store.WithinTx(op.ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
			return unavailable(e)
		}
		if _, e := st.deps.Authority.AuthorizeSystem(ctx, tx, actor, identity.Read); e != nil {
			return e
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		return fn(ctx, x)
	})
	return resultError(r)
}
func (f *SystemHTTPFacade) httpDeliveryChannel(ctx context.Context) (string, error) {
	op, e := f.core.begin(ctx, false)
	if e != nil {
		return "", e
	}
	defer f.core.finish(op)
	cause, e := recoveryCause("http-bootstrap")
	if e != nil {
		return "", e
	}
	channel := ""
	r := f.core.state().store.WithinTx(op.ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		st := f.core.state()
		if e := st.store.AcquireAll(ctx, tx, []foundation.LockRequest{configLock("account-mail", foundation.Shared)}); e != nil {
			return unavailable(e)
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		var configured bool
		if e = x.QueryRow(ctx, `SELECT configured FROM agenteam_account.smtp_settings WHERE singleton`).Scan(&configured); e != nil {
			return unavailable(e)
		}
		channel = "backend_log"
		if configured {
			channel = "smtp"
		}
		return nil
	})
	if e = resultError(r); e != nil {
		return "", e
	}
	return channel, nil
}
func (f *SystemHTTPFacade) GetAccountSettings(ctx context.Context, actor identity.Actor) (c.Settings, error) {
	var out c.Settings
	e := f.httpRead(ctx, actor, "settings", []foundation.LockRequest{configLock("account-security", foundation.Shared)}, func(ctx context.Context, x postgres.SQLExecutor) error {
		var e error
		out, e = loadSettings(ctx, x)
		return e
	})
	return out, e
}
func httpSettingsValidate(q HTTPAccountSettingsUpdate) error {
	if q.ExpectedVersion.Validate() != nil || q.ExpectedVersion == foundation.Version(math.MaxInt64) || q.SessionIdleSeconds < 900 || q.SessionIdleSeconds > 2592000 || q.SessionAbsoluteSeconds < 3600 || q.SessionAbsoluteSeconds > 7776000 || q.SessionIdleSeconds > q.SessionAbsoluteSeconds || q.PasswordResetSeconds < 300 || q.PasswordResetSeconds > 7200 || q.ChallengeAfterFailures < 1 || q.ChallengeAfterFailures > 20 {
		return invalid()
	}
	return nil
}
func (f *SystemHTTPFacade) UpdateAccountSettings(ctx context.Context, q HTTPAccountSettingsUpdate) (c.Settings, error) {
	if f == nil || f.core == nil || q.Actor.Validate() != nil || q.Actor.Details().Kind != identity.Human {
		return c.Settings{}, fault(foundation.Unauthenticated, nil)
	}
	s := f.core
	st := s.state()
	op, e := s.begin(ctx, false)
	if e != nil {
		return c.Settings{}, e
	}
	defer s.finish(op)
	ctx = op.ctx
	if _, e = st.deps.Authority.AuthorizeSystem(ctx, foundation.Tx{}, q.Actor, identity.Mutate); e != nil {
		return c.Settings{}, e
	}
	if q.Key.Validate() != nil || httpSettingsValidate(q) != nil {
		return c.Settings{}, invalid()
	}
	key, e := foundation.NewCommandIdentity("account.settings", []string{q.Actor.Details().UserID}, "settings-update", q.Key)
	if e != nil {
		return c.Settings{}, e
	}
	semantic, e := json.Marshal(struct{ Version, Idle, Absolute, Reset, Challenge string }{q.ExpectedVersion.String(), q.SessionIdleSeconds.String(), q.SessionAbsoluteSeconds.String(), q.PasswordResetSeconds.String(), q.ChallengeAfterFailures.String()})
	if e != nil {
		return c.Settings{}, unavailable(e)
	}
	macFor := func(kid string) ([]byte, error) {
		return st.keys.mac(kid, "command-v1", []byte(key.Canonical()), semantic)
	}
	projection := func(cmd commandRecord) (c.Settings, error) {
		id, e := parseID[c.SettingsRecord](cmd.resource)
		out := c.Settings{ID: id, Version: foundation.Version(cmd.resultVersion), SessionIdleSeconds: q.SessionIdleSeconds, SessionAbsoluteSeconds: q.SessionAbsoluteSeconds, PasswordResetSeconds: q.PasswordResetSeconds, ChallengeAfterFailures: q.ChallengeAfterFailures}
		if e != nil || cmd.name != "settings-update" || cmd.actorKind != "human" || cmd.user != q.Actor.Details().UserID || cmd.phase != "committed" || cmd.resultCode != "COMPLETED" || cmd.expectedVersion != int64(q.ExpectedVersion) || cmd.resultVersion != int64(q.ExpectedVersion)+1 || out.Validate() != nil {
			return c.Settings{}, unavailable(nil)
		}
		return out, nil
	}
	old, e := s.lookupMutation(ctx, key, macFor, q.Actor, c.BrowserIdentity{}, true)
	if e == nil {
		return projection(old)
	}
	if !hasFaultCode(e, foundation.NotFound) {
		return c.Settings{}, e
	}
	before, e := f.GetAccountSettings(ctx, q.Actor)
	if e != nil {
		return c.Settings{}, e
	}
	id, e := foundation.NewID[c.Command]()
	if e != nil {
		return c.Settings{}, unavailable(e)
	}
	mac, e := macFor(st.keys.current())
	if e != nil {
		return c.Settings{}, e
	}
	defer clear(mac)
	cause, e := foundation.NewCommandsCause(key)
	if e != nil {
		return c.Settings{}, e
	}
	locks := []foundation.LockRequest{commandLock(key), configLock("account-directory", foundation.Shared), configLock("account-security", foundation.Exclusive), userLock(q.Actor.Details().UserID, foundation.Exclusive), recordLock(id.String()), recordLock(before.ID.String())}
	var saved commandRecord
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
			return unavailable(e)
		}
		if _, e := st.deps.Authority.AuthorizeSystem(ctx, tx, q.Actor, identity.Mutate); e != nil {
			return e
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		found, e := loadCommand(ctx, x, string(digest([]byte(key.Canonical()))), true)
		if e == nil {
			if e = s.checkCommand(found, key, macFor); e != nil {
				return e
			}
			saved = found
			_, e = projection(saved)
			return e
		}
		if !hasFaultCode(e, foundation.NotFound) {
			return e
		}
		current, e := loadSettings(ctx, x)
		if e != nil {
			return e
		}
		if current.ID != before.ID || current.Version != q.ExpectedVersion {
			return fault(foundation.VersionConflict, nil)
		}
		_, e = x.Exec(ctx, `INSERT INTO agenteam_account.commands(id,namespace,owner_id,command_key,command_name,identity_digest,semantic_kid,semantic_mac,actor_kind,user_id,session_id,origin_process_id,resource_id,expected_version,phase,result_code,result_version,completed_at) VALUES($1,'account.settings',$2,$3,'settings-update',$4,$5,$6,'human',$2,$7,$8,$9,$10,'committed','COMPLETED',$11,clock_timestamp())`, id.String(), q.Actor.Details().UserID, string(q.Key), string(digest([]byte(key.Canonical()))), st.keys.current(), mac, q.Actor.Details().SessionID, st.process.String(), current.ID.String(), int64(q.ExpectedVersion), int64(q.ExpectedVersion)+1)
		if e != nil {
			return unavailable(e)
		}
		tag, e := x.Exec(ctx, `UPDATE agenteam_account.account_settings SET version=version+1,session_idle_seconds=$3,session_absolute_seconds=$4,password_reset_seconds=$5,challenge_after_failures=$6 WHERE id=$1 AND version=$2`, current.ID.String(), int64(q.ExpectedVersion), int64(q.SessionIdleSeconds), int64(q.SessionAbsoluteSeconds), int64(q.PasswordResetSeconds), int64(q.ChallengeAfterFailures))
		if e != nil {
			return unavailable(e)
		}
		if tag.RowsAffected() != 1 {
			return fault(foundation.VersionConflict, nil)
		}
		if e = s.mutationAudit(ctx, tx, q.Actor, ac.AccountSettingsUpdate, ac.AccountSettingsResource, current.ID.String(), id.String(), ac.AccountMetadataFields{Version: q.ExpectedVersion + 1, Phase: ac.AccountUpdated, ChangedFields: []ac.AccountChangedField{ac.SessionIdleChanged, ac.SessionAbsoluteChanged, ac.PasswordResetTTLChanged, ac.ChallengeThresholdChanged}}); e != nil {
			return e
		}
		if e = st.deps.Authority.TouchActivityInTx(ctx, tx, q.Actor); e != nil {
			return e
		}
		saved, e = loadCommand(ctx, x, id.String(), false)
		return e
	})
	if result.State() == foundation.Unknown {
		saved, e = s.lookupMutation(ctx, key, macFor, q.Actor, c.BrowserIdentity{}, true)
		if e != nil {
			return c.Settings{}, confirmationError(result, e)
		}
		out, e := projection(saved)
		if e != nil {
			return c.Settings{}, confirmationError(result, e)
		}
		return out, nil
	}
	if e = resultError(result); e != nil {
		return c.Settings{}, e
	}
	return projection(saved)
}

func httpListBinding(kind string) (cursor.Binding, error) {
	if kind != "users" && kind != "invitations" && kind != "mail-jobs" {
		return cursor.Binding{}, invalid()
	}
	query, e := json.Marshal(struct {
		Resource string `json:"resource"`
		Filter   string `json:"filter"`
	}{kind, "all"})
	if e != nil {
		return cursor.Binding{}, e
	}
	d, e := cursor.Digest(query)
	return cursor.Binding{Scope: identity.SystemScope(), QueryDigest: d, Order: cursor.AuditOrder}, e
}
func (f *SystemHTTPFacade) httpListPosition(kind string, q HTTPListRequest) (int, time.Time, string, error) {
	limit := q.Limit
	if limit == 0 {
		limit = 25
	}
	if limit < 1 || limit > 100 {
		return 0, time.Time{}, "", invalid()
	}
	if q.Cursor == "" {
		return limit, time.Time{}, "", nil
	}
	b, e := httpListBinding(kind)
	if e != nil {
		return 0, time.Time{}, "", e
	}
	p, e := f.pagination.Verify(q.Cursor, b)
	if e != nil {
		return 0, time.Time{}, "", e
	}
	if len(p.Scalars) != 2 || p.Scalars[0].Kind() != "instant" || p.Scalars[1].Kind() != "uuid" || p.OrderGeneration != nil {
		return 0, time.Time{}, "", fault(foundation.CursorInvalid, nil)
	}
	t, e := foundation.ParseInstant(p.Scalars[0].Value())
	if e != nil {
		return 0, time.Time{}, "", fault(foundation.CursorInvalid, nil)
	}
	return limit, t.Time(), p.Scalars[1].Value(), nil
}
func (f *SystemHTTPFacade) httpNextCursor(kind string, created time.Time, id string) (string, error) {
	b, e := httpListBinding(kind)
	if e != nil {
		return "", e
	}
	t, e := cursor.Instant(instant(created))
	if e != nil {
		return "", unavailable(e)
	}
	u, e := cursor.UUID(id)
	if e != nil {
		return "", unavailable(e)
	}
	return f.pagination.Sign(b, cursor.Position{Scalars: []cursor.Scalar{t, u}})
}
func httpPositionArgs(created time.Time, id string) (any, any) {
	if id == "" {
		return nil, nil
	}
	return created, id
}

func (f *SystemHTTPFacade) ListUsers(ctx context.Context, actor identity.Actor, q HTTPListRequest) (HTTPUserList, error) {
	out := HTTPUserList{Items: []c.User{}}
	e := f.httpRead(ctx, actor, "users", []foundation.LockRequest{configLock("account-directory", foundation.Shared)}, func(ctx context.Context, x postgres.SQLExecutor) error {
		limit, created, id, e := f.httpListPosition("users", q)
		if e != nil {
			return e
		}
		dateArg, idArg := httpPositionArgs(created, id)
		rows, e := x.Query(ctx, `SELECT id::text,email,username,display_name,role,theme,version,initial_password_suggestion,created_at FROM agenteam_account.users WHERE ($1::timestamptz IS NULL OR (created_at,id)<($1,$2::uuid)) ORDER BY created_at DESC,id DESC LIMIT $3`, dateArg, idArg, limit+1)
		if e != nil {
			return unavailable(e)
		}
		defer rows.Close()
		var last time.Time
		var lastID string
		for rows.Next() {
			var u c.User
			var raw string
			var version int64
			var at time.Time
			if e = rows.Scan(&raw, &u.Email, &u.Username, &u.DisplayName, &u.Role, &u.Theme, &version, &u.InitialPasswordSuggestion, &at); e != nil {
				return unavailable(e)
			}
			u.ID, e = parseID[identity.User](raw)
			u.Version = foundation.Version(version)
			if e != nil || u.Version.Validate() != nil || !u.Role.Valid() || !u.Theme.Valid() {
				return unavailable(e)
			}
			if len(out.Items) == limit {
				out.NextCursor, e = f.httpNextCursor("users", last, lastID)
				if e != nil {
					return e
				}
				break
			}
			out.Items = append(out.Items, u)
			last = at
			lastID = raw
		}
		return portError(rows.Err())
	})
	if e != nil {
		return HTTPUserList{}, e
	}
	return out, nil
}
func (f *SystemHTTPFacade) ListInvitations(ctx context.Context, actor identity.Actor, q HTTPListRequest) (HTTPInvitationList, error) {
	out := HTTPInvitationList{Items: []HTTPInvitation{}}
	e := f.httpRead(ctx, actor, "invitations", []foundation.LockRequest{configLock("account-directory", foundation.Shared)}, func(ctx context.Context, x postgres.SQLExecutor) error {
		limit, created, id, e := f.httpListPosition("invitations", q)
		if e != nil {
			return e
		}
		dateArg, idArg := httpPositionArgs(created, id)
		rows, e := x.Query(ctx, `SELECT id::text,canonical_email,version,created_at,expires_at FROM agenteam_account.invitations WHERE ($1::timestamptz IS NULL OR (created_at,id)<($1,$2::uuid)) ORDER BY created_at DESC,id DESC LIMIT $3`, dateArg, idArg, limit+1)
		if e != nil {
			return unavailable(e)
		}
		defer rows.Close()
		var last time.Time
		var lastID string
		for rows.Next() {
			var v HTTPInvitation
			var raw string
			var version int64
			var at, expires time.Time
			if e = rows.Scan(&raw, &v.Email, &version, &at, &expires); e != nil {
				return unavailable(e)
			}
			v.ID, e = parseID[c.Invitation](raw)
			v.Version = foundation.Version(version)
			v.CreatedAt = instant(at)
			v.ExpiresAt = instant(expires)
			if e != nil || v.Version.Validate() != nil || v.CreatedAt.Validate() != nil || v.ExpiresAt.Validate() != nil {
				return unavailable(e)
			}
			if len(out.Items) == limit {
				out.NextCursor, e = f.httpNextCursor("invitations", last, lastID)
				if e != nil {
					return e
				}
				break
			}
			out.Items = append(out.Items, v)
			last = at
			lastID = raw
		}
		return portError(rows.Err())
	})
	if e != nil {
		return HTTPInvitationList{}, e
	}
	return out, nil
}

const accountHTTPMailSelect = `SELECT i.job_id::text,coalesce(j.phase,'enqueue_pending'),coalesce(j.attempts,0),coalesce(j.version,1),coalesce(j.reason,''),coalesce(a.channel,CASE WHEN cfg.configured THEN 'smtp' ELSE 'log' END),i.created_at FROM agenteam_account.delivery_intents i LEFT JOIN agenteam_account.mail_jobs j ON j.intent_id=i.id LEFT JOIN agenteam_account.mail_attempts a ON a.id=j.current_attempt_id CROSS JOIN agenteam_account.smtp_settings cfg `

type httpRowScanner interface{ Scan(...any) error }

func httpScanMail(row httpRowScanner) (HTTPMailJob, error) {
	var v HTTPMailJob
	var raw, channel string
	var version, attempts int64
	var created time.Time
	if e := row.Scan(&raw, &v.Status.Phase, &attempts, &version, &v.Status.Reason, &channel, &created); e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			return v, fault(foundation.NotFound, nil)
		}
		return v, unavailable(e)
	}
	var e error
	v.Status.JobID, e = parseID[c.MailJob](raw)
	v.Status.Version = foundation.Version(version)
	v.Status.Attempts = foundation.Progress(attempts)
	v.CreatedAt = instant(created)
	if e != nil || v.Status.Version.Validate() != nil || attempts < 0 || attempts > 6 || v.Status.Reason != "" && !v.Status.Reason.Valid() || v.CreatedAt.Validate() != nil {
		return HTTPMailJob{}, unavailable(e)
	}
	if v.Status.Phase == "processing" {
		v.Status.Phase = "unknown"
	}
	switch v.Status.Phase {
	case "enqueue_pending", "pending", "claimed", "sending", "retry_wait", "sent", "failed", "unknown", "cancelled":
	default:
		return HTTPMailJob{}, unavailable(nil)
	}
	switch channel {
	case "smtp":
		v.Channel = "smtp"
	case "log":
		v.Channel = "backend_log"
	default:
		return HTTPMailJob{}, unavailable(nil)
	}
	return v, nil
}
func (f *SystemHTTPFacade) ListMailJobs(ctx context.Context, actor identity.Actor, q HTTPListRequest) (HTTPMailJobList, error) {
	out := HTTPMailJobList{Items: []HTTPMailJob{}}
	e := f.httpRead(ctx, actor, "mail-jobs", []foundation.LockRequest{configLock("account-mail", foundation.Shared)}, func(ctx context.Context, x postgres.SQLExecutor) error {
		limit, created, id, e := f.httpListPosition("mail-jobs", q)
		if e != nil {
			return e
		}
		dateArg, idArg := httpPositionArgs(created, id)
		rows, e := x.Query(ctx, accountHTTPMailSelect+`WHERE cfg.singleton AND ($1::timestamptz IS NULL OR (i.created_at,i.job_id)<($1,$2::uuid)) ORDER BY i.created_at DESC,i.job_id DESC LIMIT $3`, dateArg, idArg, limit+1)
		if e != nil {
			return unavailable(e)
		}
		defer rows.Close()
		var last HTTPMailJob
		for rows.Next() {
			v, e := httpScanMail(rows)
			if e != nil {
				return e
			}
			if len(out.Items) == limit {
				out.NextCursor, e = f.httpNextCursor("mail-jobs", last.CreatedAt.Time(), last.Status.JobID.String())
				if e != nil {
					return e
				}
				break
			}
			out.Items = append(out.Items, v)
			last = v
		}
		return portError(rows.Err())
	})
	if e != nil {
		return HTTPMailJobList{}, e
	}
	return out, nil
}
func (f *SystemHTTPFacade) GetMailJob(ctx context.Context, actor identity.Actor, id c.JobID) (HTTPMailJob, error) {
	var out HTTPMailJob
	e := f.httpRead(ctx, actor, "mail-job", []foundation.LockRequest{configLock("account-mail", foundation.Shared)}, func(ctx context.Context, x postgres.SQLExecutor) error {
		if id.Validate() != nil {
			return invalid()
		}
		var e error
		out, e = httpScanMail(x.QueryRow(ctx, accountHTTPMailSelect+`WHERE cfg.singleton AND i.job_id=$1`, id.String()))
		return e
	})
	return out, e
}
