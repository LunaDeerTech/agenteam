package account

import (
	"context"
	"errors"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

const httpMailJobManagementTimeout = 3 * time.Second

type HTTPMailJobManagement struct {
	HTTPMailJob
	Kind           c.DeliveryKind
	AttemptChannel *string
	AttemptResult  *c.DeliveryResult
}

type HTTPMailJobManagementList struct {
	Items      []HTTPMailJobManagement
	NextCursor string
}

// Page before joining, but retain broken mappings and missing current rows for
// the scanner. Both unique job mappings must agree in this statement snapshot.
const accountHTTPMailManagementColumns = `i.job_id::text,i.kind,i.created_at,
((j.id IS NULL AND by_id.id IS NULL) OR (j.id=i.job_id AND by_id.id=j.id AND by_id.intent_id=i.id)) IS TRUE,
j.id IS NOT NULL,coalesce(j.phase,'enqueue_pending'),coalesce(j.attempts,0),coalesce(j.version,1),coalesce(j.reason,''),coalesce(j.fence,0),
coalesce(j.current_attempt_id::text,''),coalesce(a.id::text,''),coalesce(a.job_id::text,''),coalesce(a.fence,0),coalesce(a.phase,''),a.channel,a.result,coalesce(a.terminal,false),coalesce(a.io_joined,false),cfg.configured`

const accountHTTPMailManagementJoins = ` LEFT JOIN agenteam_account.mail_jobs j ON j.intent_id=i.id
LEFT JOIN agenteam_account.mail_jobs by_id ON by_id.id=i.job_id
LEFT JOIN agenteam_account.mail_attempts a ON a.id=j.current_attempt_id
LEFT JOIN agenteam_account.smtp_settings cfg ON cfg.singleton `

const accountHTTPMailManagementListSQL = `WITH page AS MATERIALIZED (
SELECT id,job_id,kind,created_at FROM agenteam_account.delivery_intents
WHERE ($1::timestamptz IS NULL OR (created_at,job_id)<($1,$2::uuid))
ORDER BY created_at DESC,job_id DESC LIMIT $3)
SELECT ` + accountHTTPMailManagementColumns + ` FROM page i` + accountHTTPMailManagementJoins + `ORDER BY i.created_at DESC,i.job_id DESC`

const accountHTTPMailManagementDetailSQL = `SELECT ` + accountHTTPMailManagementColumns + ` FROM agenteam_account.delivery_intents i` + accountHTTPMailManagementJoins + `WHERE i.job_id=$1`

func (f *SystemHTTPFacade) ListMailJobManagement(ctx context.Context, actor identity.Actor, query HTTPListRequest) (HTTPMailJobManagementList, error) {
	ctx, cancel := context.WithTimeout(ctx, httpMailJobManagementTimeout)
	defer cancel()
	var out HTTPMailJobManagementList
	e := f.httpRead(ctx, actor, "mail-job-management-list", []foundation.LockRequest{configLock("account-mail", foundation.Shared)}, func(ctx context.Context, x postgres.SQLExecutor) error {
		limit, created, id, e := f.httpMailJobManagementPosition(query)
		if e != nil {
			return e
		}
		at, after := httpPositionArgs(created, id)
		rows, e := x.Query(ctx, accountHTTPMailManagementListSQL, at, after, limit+1)
		if e != nil {
			return unavailable(e)
		}
		out, e = f.httpScanMailJobManagementList(ctx, rows, limit)
		return e
	})
	if e != nil {
		return HTTPMailJobManagementList{}, e
	}
	if e = ctx.Err(); e != nil {
		return HTTPMailJobManagementList{}, unavailable(e)
	}
	return out, nil
}

func (f *SystemHTTPFacade) GetMailJobManagement(ctx context.Context, actor identity.Actor, id c.JobID) (HTTPMailJobManagement, error) {
	ctx, cancel := context.WithTimeout(ctx, httpMailJobManagementTimeout)
	defer cancel()
	if id.Validate() != nil {
		return HTTPMailJobManagement{}, invalid()
	}
	var out HTTPMailJobManagement
	e := f.httpRead(ctx, actor, "mail-job-management", []foundation.LockRequest{configLock("account-mail", foundation.Shared)}, func(ctx context.Context, x postgres.SQLExecutor) error {
		var e error
		out, e = httpScanMailJobManagement(x.QueryRow(ctx, accountHTTPMailManagementDetailSQL, id.String()))
		if e != nil {
			return e
		}
		if out.Status.JobID != id {
			return unavailable(nil)
		}
		return nil
	})
	if e != nil {
		return HTTPMailJobManagement{}, e
	}
	if e = ctx.Err(); e != nil {
		return HTTPMailJobManagement{}, unavailable(e)
	}
	return out, nil
}

type httpMailJobManagementRows interface {
	httpRowScanner
	Next() bool
	Err() error
	Close()
}

func (f *SystemHTTPFacade) httpScanMailJobManagementList(ctx context.Context, rows httpMailJobManagementRows, limit int) (out HTTPMailJobManagementList, err error) {
	defer func() {
		rows.Close()
		if e := rows.Err(); e != nil {
			err = unavailable(e)
		}
		if e := ctx.Err(); e != nil {
			err = unavailable(e)
		}
		if err != nil {
			out = HTTPMailJobManagementList{}
		}
	}()
	out.Items = make([]HTTPMailJobManagement, 0, limit)
	for rows.Next() {
		item, e := httpScanMailJobManagement(rows)
		if e != nil {
			return out, e
		}
		if len(out.Items) == limit {
			out.NextCursor, err = f.httpMailJobManagementCursor(out.Items[len(out.Items)-1])
			return out, err
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}

func httpScanMailJobManagement(row httpRowScanner) (HTTPMailJobManagement, error) {
	var out HTTPMailJobManagement
	var job, phase, current, attempt, attemptJob, protocol string
	var attempts, version, fence, attemptFence int64
	var created time.Time
	var mapping, hasJob, terminal, joined bool
	var channel, result *string
	var configured *bool
	if e := row.Scan(&job, &out.Kind, &created, &mapping, &hasJob, &phase, &attempts, &version, &out.Status.Reason, &fence,
		&current, &attempt, &attemptJob, &attemptFence, &protocol, &channel, &result, &terminal, &joined, &configured); e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			return HTTPMailJobManagement{}, fault(foundation.NotFound, nil)
		}
		return HTTPMailJobManagement{}, unavailable(e)
	}
	var e error
	out.Status.JobID, e = parseID[c.MailJob](job)
	if e != nil {
		return HTTPMailJobManagement{}, unavailable(e)
	}
	out.CreatedAt, e = foundation.NewInstant(created)
	if e != nil {
		return HTTPMailJobManagement{}, unavailable(e)
	}
	out.Status.Attempts, out.Status.Version = foundation.Progress(attempts), foundation.Version(version)
	if !mapping || !out.Kind.Valid() || configured == nil || out.Status.Version.Validate() != nil || attempts < 0 || attempts > 6 || fence < 0 || out.Status.Reason != "" && !out.Status.Reason.Valid() {
		return HTTPMailJobManagement{}, unavailable(nil)
	}
	if !hasJob {
		if phase != "enqueue_pending" || attempts != 0 || version != 1 || fence != 0 || out.Status.Reason != "" || current != "" {
			return HTTPMailJobManagement{}, unavailable(nil)
		}
	} else {
		switch phase {
		case "pending", "claimed", "sending", "retry_wait", "sent", "failed", "unknown", "cancelled", "processing":
		default:
			return HTTPMailJobManagement{}, unavailable(nil)
		}
	}
	out.Channel = "backend_log"
	if *configured {
		out.Channel = "smtp"
	}
	if current == "" {
		if attempt != "" || attemptJob != "" || attemptFence != 0 || protocol != "" || channel != nil || result != nil || terminal || joined {
			return HTTPMailJobManagement{}, unavailable(nil)
		}
		switch phase {
		case "claimed", "sending", "retry_wait", "sent", "failed":
			return HTTPMailJobManagement{}, unavailable(nil)
		}
	} else {
		if _, e = parseID[struct{}](current); e != nil || current != attempt || attemptJob != job || !hasJob || attempts < 1 || fence < 1 || fence != attemptFence || channel == nil {
			return HTTPMailJobManagement{}, unavailable(e)
		}
		switch protocol {
		case "negotiation", "auth", "envelope", "data", "awaiting_acceptance", "closed":
		default:
			return HTTPMailJobManagement{}, unavailable(nil)
		}
		switch *channel {
		case "smtp":
			out.Channel = "smtp"
		case "log":
			out.Channel = "backend_log"
		default:
			return HTTPMailJobManagement{}, unavailable(nil)
		}
		actualChannel := out.Channel
		out.AttemptChannel = &actualChannel
		if result == nil {
			if protocol == "closed" || terminal {
				return HTTPMailJobManagement{}, unavailable(nil)
			}
		} else {
			if protocol != "closed" || !terminal || !joined {
				return HTTPMailJobManagement{}, unavailable(nil)
			}
			actualResult := c.DeliveryResult(*result)
			switch actualResult {
			case c.DeliverySent, c.DeliveryFailed, c.DeliveryUnknown, c.DeliveryCancelled:
			default:
				return HTTPMailJobManagement{}, unavailable(nil)
			}
			out.AttemptResult = &actualResult
		}
	}
	if phase == "processing" {
		phase = "unknown"
	}
	out.Status.Phase = phase
	return out, nil
}

func httpMailJobManagementBinding() (cursor.Binding, error) {
	digest, e := cursor.Digest([]byte(`{"resource":"mail-job-management","filter":"all"}`))
	return cursor.Binding{Scope: identity.SystemScope(), QueryDigest: digest, Order: cursor.AuditOrder}, e
}

func (f *SystemHTTPFacade) httpMailJobManagementPosition(query HTTPListRequest) (int, time.Time, string, error) {
	limit := query.Limit
	if limit == 0 {
		limit = 25
	}
	if limit < 1 || limit > 100 {
		return 0, time.Time{}, "", invalid()
	}
	if query.Cursor == "" {
		return limit, time.Time{}, "", nil
	}
	if len(query.Cursor) > 8192 {
		return 0, time.Time{}, "", fault(foundation.CursorInvalid, nil)
	}
	binding, e := httpMailJobManagementBinding()
	if e != nil {
		return 0, time.Time{}, "", e
	}
	position, e := f.pagination.Verify(query.Cursor, binding)
	if e != nil {
		return 0, time.Time{}, "", e
	}
	if len(position.Scalars) != 2 || position.Scalars[0].Kind() != "instant" || position.Scalars[1].Kind() != "uuid" || position.OrderGeneration != nil {
		return 0, time.Time{}, "", fault(foundation.CursorInvalid, nil)
	}
	created, e := foundation.ParseInstant(position.Scalars[0].Value())
	if e != nil {
		return 0, time.Time{}, "", fault(foundation.CursorInvalid, nil)
	}
	return limit, created.Time(), position.Scalars[1].Value(), nil
}

func (f *SystemHTTPFacade) httpMailJobManagementCursor(item HTTPMailJobManagement) (string, error) {
	binding, e := httpMailJobManagementBinding()
	if e != nil {
		return "", e
	}
	at, e := cursor.Instant(item.CreatedAt)
	if e != nil {
		return "", unavailable(e)
	}
	id, e := cursor.UUID(item.Status.JobID.String())
	if e != nil {
		return "", unavailable(e)
	}
	return f.pagination.Sign(binding, cursor.Position{Scalars: []cursor.Scalar{at, id}})
}
