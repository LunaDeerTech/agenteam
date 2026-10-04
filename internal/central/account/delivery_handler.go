package account

import (
	"context"
	"encoding/json"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type deliveryHandler struct{ service *Service }
type deliveryFact struct{ ID, Job, Kind, Link, User string }

func loadDelivery(ctx context.Context, x postgres.SQLExecutor, id string) (deliveryFact, error) {
	var f deliveryFact
	e := x.QueryRow(ctx, `SELECT i.id::text,i.job_id::text,i.kind,coalesce(i.link_id::text,''),coalesce(r.user_id::text,'') FROM agenteam_account.delivery_intents i LEFT JOIN agenteam_account.password_resets r ON i.kind='password_reset' AND r.id=i.link_id WHERE i.id=$1`, id).Scan(&f.ID, &f.Job, &f.Kind, &f.Link, &f.User)
	return f, portError(e)
}
func deliveryMapping(f deliveryFact) []byte { b, _ := json.Marshal(f); return b }
func deliveryLocks(f deliveryFact) []foundation.LockRequest {
	l := []foundation.LockRequest{configLock("account-directory", foundation.Shared), configLock("account-mail", foundation.Shared), recordLock(f.ID), recordLock(f.Job)}
	if f.Link != "" {
		l = append(l, recordLock(f.Link))
	}
	if f.User != "" {
		l = append(l, userLock(f.User, foundation.Shared))
	}
	return l
}

// MailHandler installs only the durable enqueue handler. SMTP/log delivery has
// its own later admission and actual-I/O lifecycle; an acknowledged event is
// never evidence of an email having been sent.
func (s *Service) MailHandler() (oc.HandlerDefinition, error) {
	if s == nil || s.state() == nil || s.state().deps.DeliveryRequested.Schema().EventType != c.DeliveryRequestedType {
		return oc.HandlerDefinition{}, fault(foundation.DependencyUnbound, nil)
	}
	return oc.HandlerDefinition{ID: c.MailEnqueueHandler, Subscriptions: []oc.Subscription{{EventType: c.DeliveryRequestedType, Versions: []uint32{1}}}, Effect: oc.CanonicalConverge, Ordering: oc.CanonicalReconcile, Handler: &deliveryHandler{s}}, nil
}
func (h *deliveryHandler) eventFact(ctx context.Context, x postgres.SQLExecutor, e event.Event) (deliveryFact, error) {
	p, err := event.DecodeEvent(h.service.state().deps.DeliveryRequested, e)
	if err != nil {
		return deliveryFact{}, portError(err)
	}
	f, err := loadDelivery(ctx, x, p.IntentID.String())
	if err != nil {
		return f, err
	}
	if f.Kind != string(p.Kind) || e.Header().Scope.Kind != event.SystemScope || e.Header().AggregateID.String() != f.ID {
		return f, fault(foundation.Forbidden, nil)
	}
	return f, nil
}
func (h *deliveryHandler) Prepare(ctx context.Context, e event.Event) (oc.HandlerPlan, error) {
	f, err := h.eventFact(ctx, h.service.state().store, e)
	if err != nil {
		return oc.HandlerPlan{}, err
	}
	binding, err := oc.EventDigest(e)
	if err != nil {
		return oc.HandlerPlan{}, err
	}
	d, err := oc.NewDependencies(h.service.state().deps.Authority.state().eventIssuer, binding, deliveryLocks(f), deliveryMapping(f))
	if err != nil {
		return oc.HandlerPlan{}, err
	}
	return oc.NewHandlerPlan(c.MailEnqueueHandler, e, d)
}
func (h *deliveryHandler) ValidateInTx(ctx context.Context, tx foundation.Tx, e event.Event, p oc.HandlerPlan) error {
	b, err := oc.EventDigest(e)
	if err != nil || !p.Matches(c.MailEnqueueHandler, e) || !p.Dependencies().Matches(h.service.state().deps.Authority.state().eventIssuer, b) {
		return fault(foundation.Forbidden, nil)
	}
	if err = h.service.state().store.RequireHeldLocks(ctx, tx, p.Locks()); err != nil {
		return unavailable(err)
	}
	x, err := h.service.state().store.InTx(tx)
	if err != nil {
		return unavailable(err)
	}
	f, err := h.eventFact(ctx, x, e)
	if err != nil {
		return err
	}
	if string(deliveryMapping(f)) != string(p.Dependencies().Opaque()) {
		return fault(foundation.ResourceBusy, nil)
	}
	return nil
}
func (h *deliveryHandler) HandleInTx(ctx context.Context, tx foundation.Tx, e event.Event, p oc.HandlerPlan) oc.Result {
	if err := h.ValidateInTx(ctx, tx, e, p); err != nil {
		return oc.Retry(oc.AuthorizationChanged)
	}
	x, err := h.service.state().store.InTx(tx)
	if err != nil {
		return oc.Retry(oc.Unavailable)
	}
	f, err := h.eventFact(ctx, x, e)
	if err != nil {
		return oc.Retry(oc.Unavailable)
	}
	if err = enqueueDelivery(ctx, x, f); err != nil {
		return oc.Retry(oc.Unavailable)
	}
	return oc.Ack(digest(deliveryMapping(f)))
}
func enqueueDelivery(ctx context.Context, x postgres.SQLExecutor, f deliveryFact) error {
	var live bool
	switch c.DeliveryKind(f.Kind) {
	case c.InvitationDelivery:
		if e := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.invitations WHERE id=$1 AND expires_at>clock_timestamp())`, f.Link).Scan(&live); e != nil {
			return unavailable(e)
		}
	case c.ResetDelivery:
		if e := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.password_resets r JOIN agenteam_account.users u ON u.id=r.user_id WHERE r.id=$1 AND r.expires_at>clock_timestamp() AND r.password_version=u.password_version)`, f.Link).Scan(&live); e != nil {
			return unavailable(e)
		}
	default:
		return fault(foundation.DependencyUnbound, nil)
	}
	phase := "pending"
	var reason any
	var completed any
	if !live {
		phase = "cancelled"
		reason = "token_invalid"
	}
	_, e := x.Exec(ctx, `INSERT INTO agenteam_account.mail_jobs(id,intent_id,phase,version,reason,completed_at) VALUES($1,$2,$3,1,$4,CASE WHEN $3='cancelled' THEN clock_timestamp() ELSE $5::timestamptz END) ON CONFLICT(intent_id) DO NOTHING`, f.Job, f.ID, phase, reason, completed)
	return portError(e)
}

// ReconcileDeliveryIntents is a bounded canonical scan independent of event
// registration boundaries. The durable intent is authoritative; the handler
// and this path converge through the same unique job and gate locks.
func (s *Service) ReconcileDeliveryIntents(ctx context.Context) (RecoveryStatus, error) {
	op, e := s.begin(ctx, true)
	if e != nil {
		return RecoveryStatus{}, e
	}
	defer s.finish(op)
	ctx = op.ctx
	ids, e := s.recoveryBatch(ctx, "commands", "cleanup_pass", `EXISTS(SELECT 1 FROM agenteam_account.delivery_intents i LEFT JOIN agenteam_account.mail_jobs j ON j.intent_id=i.id WHERE i.id=agenteam_account.commands.id AND j.id IS NULL)`)
	if e != nil {
		return RecoveryStatus{}, e
	}
	var status RecoveryStatus
	var first error
	for _, id := range ids {
		if ctx.Err() != nil {
			return status, unavailable(ctx.Err())
		}
		status.Examined++
		item, cancel := context.WithTimeout(ctx, 2*time.Second)
		e = s.reconcileDelivery(item, id)
		cancel()
		if e == nil {
			status.Advanced++
		} else {
			status.Pending++
			if first == nil {
				first = e
			}
		}
	}
	return status, first
}
func (s *Service) reconcileDelivery(ctx context.Context, id string) error {
	f, e := loadDelivery(ctx, s.state().store, id)
	if e != nil {
		return e
	}
	cause, e := recoveryCause("delivery-enqueue")
	if e != nil {
		return e
	}
	r := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := s.state().store.AcquireAll(ctx, tx, deliveryLocks(f)); e != nil {
			return unavailable(e)
		}
		x, e := s.state().store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		now, e := loadDelivery(ctx, x, id)
		if e != nil {
			return e
		}
		if string(deliveryMapping(now)) != string(deliveryMapping(f)) {
			return fault(foundation.ResourceBusy, nil)
		}
		return enqueueDelivery(ctx, x, now)
	})
	return resultError(r)
}
