package account

import (
	"context"
	"encoding/json"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type originIntent struct {
	ID, Origin, Job, Kind, Link, Initiator, Recipient string
	Created                                           time.Time
}
type deliveryOrigin struct {
	source, root               originIntent
	sourceCommand, rootCommand commandRecord
}

func loadOriginIntent(ctx context.Context, x postgres.SQLExecutor, id string) (originIntent, error) {
	var i originIntent
	e := x.QueryRow(ctx, `SELECT id::text,origin_intent_id::text,job_id::text,kind,coalesce(link_id::text,''),initiator_id::text,coalesce(recipient,''),created_at FROM agenteam_account.delivery_intents WHERE id=$1`, id).Scan(&i.ID, &i.Origin, &i.Job, &i.Kind, &i.Link, &i.Initiator, &i.Recipient, &i.Created)
	return i, portError(e)
}

// Only one origin edge is followed. Historical Session/link expiry never
// changes immutable provenance, but no provenance is itself read/send authority.
func loadDeliveryOrigin(ctx context.Context, x postgres.SQLExecutor, id string) (deliveryOrigin, error) {
	var o deliveryOrigin
	var e error
	o.source, e = loadOriginIntent(ctx, x, id)
	if e != nil {
		return o, e
	}
	o.root = o.source
	if o.source.Origin != id {
		o.root, e = loadOriginIntent(ctx, x, o.source.Origin)
		if e != nil {
			return o, e
		}
	}
	if o.root.Origin != o.root.ID || o.source.Kind != o.root.Kind || o.source.Link != o.root.Link || o.source.Recipient != o.root.Recipient {
		return o, fault(foundation.Forbidden, nil)
	}
	o.rootCommand, e = loadCommand(ctx, x, o.root.ID, false)
	if e != nil {
		return o, e
	}
	r := o.rootCommand
	if r.phase != "committed" || r.attempt != o.root.Job || !r.created.Equal(o.root.Created) {
		return o, fault(foundation.Forbidden, nil)
	}
	switch c.DeliveryKind(o.root.Kind) {
	case c.InvitationDelivery:
		if r.name != "invite-create" || r.actorKind != "human" || r.user == "" || r.user != o.root.Initiator || r.resource != o.root.Link {
			return o, fault(foundation.Forbidden, nil)
		}
	case c.ResetDelivery:
		if r.name != "reset-request" || r.actorKind != "browser" || r.browser == "" || r.browser != o.root.Initiator || r.resource != o.root.Link || r.user == "" || r.passwordVersion < 1 {
			return o, fault(foundation.Forbidden, nil)
		}
	case c.TestDelivery:
		if r.name != "smtp-test" || r.actorKind != "human" || r.user == "" || r.user != o.root.Initiator || o.root.Link != "" || o.root.Recipient == "" || r.resource == "" || r.expectedVersion < 1 {
			return o, fault(foundation.Forbidden, nil)
		}
	default:
		return o, fault(foundation.Forbidden, nil)
	}
	o.sourceCommand = r
	if o.source.ID != o.root.ID {
		o.sourceCommand, e = loadCommand(ctx, x, o.source.ID, false)
		if e != nil {
			return o, e
		}
		s := o.sourceCommand
		if s.name != "mail-retry" || s.phase != "committed" || s.actorKind != "human" || s.user != o.source.Initiator || s.attempt != o.source.Job || !s.created.Equal(o.source.Created) {
			return o, fault(foundation.Forbidden, nil)
		}
		var exact bool
		e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.mail_jobs j JOIN agenteam_account.delivery_intents i ON i.id=j.intent_id WHERE j.id=$1 AND i.origin_intent_id=$2)`, s.resource, o.root.ID).Scan(&exact)
		if e != nil {
			return o, unavailable(e)
		}
		if !exact {
			return o, fault(foundation.Forbidden, nil)
		}
	}
	return o, nil
}
func (o deliveryOrigin) binding() foundation.Digest {
	b, _ := json.Marshal(struct {
		Source, Root               originIntent
		SourceCommand, RootCommand foundation.Digest
	}{o.source, o.root, commandMapping(o.sourceCommand), commandMapping(o.rootCommand)})
	return digest(b)
}
func (o deliveryOrigin) locks() []foundation.LockRequest {
	l := append(commandLocks(o.sourceCommand), commandLocks(o.rootCommand)...)
	l = append(l, recordLock(o.root.ID), recordLock(o.source.ID), recordLock(o.root.Job), recordLock(o.source.Job))
	if o.root.Link != "" {
		l = append(l, recordLock(o.root.Link))
	}
	return l
}
func (o deliveryOrigin) user() string {
	if o.root.Kind == string(c.ResetDelivery) {
		return o.rootCommand.user
	}
	return ""
}

func loadJobOrigin(ctx context.Context, x postgres.SQLExecutor, job string) (deliveryOrigin, error) {
	var intent string
	if e := x.QueryRow(ctx, `SELECT intent_id::text FROM agenteam_account.mail_jobs WHERE id=$1`, job).Scan(&intent); e != nil {
		return deliveryOrigin{}, portError(e)
	}
	return loadDeliveryOrigin(ctx, x, intent)
}
func originLive(ctx context.Context, x postgres.SQLExecutor, o deliveryOrigin) error {
	if o.root.Kind == string(c.TestDelivery) {
		cfg, e := loadSMTP(ctx, x)
		if e != nil {
			return e
		}
		if !cfg.Configured || !cfg.Enabled {
			return fault(foundation.InvalidState, nil)
		}
		if cfg.ID != o.rootCommand.resource {
			return fault(foundation.Forbidden, nil)
		}
		return nil
	}
	l, e := loadLink(ctx, x, c.TokenKind(o.root.Kind), o.root.Link)
	if e != nil {
		return e
	}
	if !l.live {
		return fault(foundation.ResourceDeleted, nil)
	}
	if o.root.Kind == string(c.ResetDelivery) {
		if l.user != o.rootCommand.user || l.passwordVersion != o.rootCommand.passwordVersion {
			return fault(foundation.ResourceDeleted, nil)
		}
		u, e := loadUser(ctx, x, l.user)
		if e != nil {
			return e
		}
		if int64(u.passwordVersion) != l.passwordVersion {
			return fault(foundation.ResourceDeleted, nil)
		}
	}
	return nil
}

// The root record writer serializes accepted-but-not-enqueued cycles as well
// as claimed work. Terminal unknown is different from an unjoined unknown.
func originHasOtherCycle(ctx context.Context, x postgres.SQLExecutor, root, exceptJob string) (bool, error) {
	var busy bool
	e := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.delivery_intents i LEFT JOIN agenteam_account.mail_jobs j ON j.intent_id=i.id LEFT JOIN agenteam_account.mail_attempts a ON a.id=j.current_attempt_id WHERE i.origin_intent_id=$1 AND i.job_id<>$2 AND (j.id IS NULL OR j.phase IN ('pending','retry_wait','claimed','sending','processing') OR (j.phase='unknown' AND (a.id IS NULL OR NOT a.terminal OR NOT a.io_joined)) OR (a.id IS NOT NULL AND (NOT a.terminal OR NOT a.io_joined))))`, root, exceptJob).Scan(&busy)
	return busy, portError(e)
}
