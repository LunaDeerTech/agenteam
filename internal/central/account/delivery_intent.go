package account

import (
	"context"
	"encoding/json"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func commandDeliveryKind(cmd commandRecord) (c.DeliveryKind, error) {
	switch cmd.name {
	case "invite-create":
		return c.InvitationDelivery, nil
	case "reset-request":
		return c.ResetDelivery, nil
	case "smtp-test":
		return c.TestDelivery, nil
	}
	return "", fault(foundation.Forbidden, nil)
}
func deliveryHeader(cmd commandRecord) event.Header {
	id, _ := foundation.ParseID[event.EventIdentity](cmd.id.String())
	aggregate, _ := foundation.ParseID[event.Aggregate](cmd.id.String())
	v, q := foundation.Version(1), foundation.Sequence(1)
	return event.Header{EventID: id, EventType: c.DeliveryRequestedType, SchemaVersion: 1, OccurredAt: instant(cmd.created), Scope: event.Scope{Kind: event.SystemScope}, AggregateType: c.DeliveryAggregate, AggregateID: aggregate, AggregateVersion: &v, AggregateSequence: &q}
}
func (s *Service) prepareDelivery(ctx context.Context, cmd commandRecord) (identity.Actor, event.Event, oc.AppendPlan, error) {
	if nilPort(s.state().deps.Events) || s.state().deps.DeliveryRequested.Schema().EventType != c.DeliveryRequestedType {
		return identity.Actor{}, event.Event{}, oc.AppendPlan{}, fault(foundation.DependencyUnbound, nil)
	}
	kind, e := commandDeliveryKind(cmd)
	if e != nil {
		return identity.Actor{}, event.Event{}, oc.AppendPlan{}, e
	}
	id, _ := foundation.ParseID[c.DeliveryIntent](cmd.id.String())
	evt, e := event.NewEvent(s.state().deps.DeliveryRequested, deliveryHeader(cmd), c.DeliveryRequested{IntentID: id, Kind: kind})
	if e != nil {
		return identity.Actor{}, evt, oc.AppendPlan{}, e
	}
	var actor identity.Actor
	if cmd.actorKind == "human" {
		u, _ := parseID[identity.User](cmd.user)
		sid, _ := parseID[identity.Session](cmd.session)
		actor, e = identity.NewHuman(u, sid)
	} else {
		actor, e = serviceActor(identity.AccountAuth, cmd.id.String())
	}
	if e != nil {
		return actor, evt, oc.AppendPlan{}, e
	}
	plan, e := s.state().deps.Events.PrepareAppend(ctx, actor, evt)
	return actor, evt, plan, portError(e)
}
func (a *Authority) deliveryCommand(ctx context.Context, x postgres.SQLExecutor, actor identity.Actor, summary event.Summary) (commandRecord, error) {
	h := summary.Header
	if h.EventType != c.DeliveryRequestedType || summary.Producer != c.AccountProducer || actor.Validate() != nil {
		return commandRecord{}, fault(foundation.Forbidden, nil)
	}
	cmd, e := loadCommand(ctx, x, h.AggregateID.String(), false)
	if e != nil {
		return cmd, e
	}
	kind, e := commandDeliveryKind(cmd)
	if e != nil {
		return cmd, e
	}
	want, _ := json.Marshal(deliveryHeader(cmd))
	got, _ := json.Marshal(h)
	id, _ := foundation.ParseID[c.DeliveryIntent](cmd.id.String())
	payload, e := json.Marshal(c.DeliveryRequested{IntentID: id, Kind: kind})
	if e != nil {
		return cmd, unavailable(e)
	}
	if string(want) != string(got) || digest(payload) != summary.PayloadDigest {
		return cmd, fault(foundation.Forbidden, nil)
	}
	d := actor.Details()
	if cmd.actorKind == "human" {
		if d.Kind != identity.Human || d.UserID != cmd.user || d.SessionID != cmd.session {
			return cmd, fault(foundation.Forbidden, nil)
		}
	} else if cmd.name != "reset-request" || cmd.actorKind != "browser" || d.Kind != identity.Service || d.ServiceName != identity.AccountAuth || d.CauseRef != cmd.id.String() {
		return cmd, fault(foundation.Forbidden, nil)
	}
	return cmd, nil
}
func (a *Authority) validateDeliveryAppend(ctx context.Context, tx foundation.Tx, actor identity.Actor, summary event.Summary, deps oc.Dependencies, stage oc.Stage) error {
	x, e := a.state().store.InTx(tx)
	if e != nil {
		return unavailable(e)
	}
	cmd, e := a.deliveryCommand(ctx, x, actor, summary)
	if e != nil {
		return e
	}
	if string(deps.Opaque()) != string(commandMapping(cmd)) {
		return fault(foundation.ResourceBusy, nil)
	}
	if cmd.actorKind == "human" {
		if _, e = a.AuthorizeSystem(ctx, tx, actor, identity.Mutate); e != nil {
			return e
		}
	} else {
		var accepted bool
		e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.reset_requests WHERE command_id=$1 AND phase IN ('accepted','processed'))`, cmd.id.String()).Scan(&accepted)
		if e != nil {
			return unavailable(e)
		}
		if !accepted {
			return fault(foundation.Forbidden, nil)
		}
	}
	if cmd.phase != "planned" && cmd.phase != "committed" {
		return fault(foundation.InvalidState, nil)
	}
	if stage == oc.NewFact {
		var exact bool
		e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.delivery_intents WHERE id=$1 AND job_id=$2 AND ((kind='test' AND link_id IS NULL) OR (kind<>'test' AND link_id=$3)))`, cmd.id.String(), cmd.attempt, cmd.resource).Scan(&exact)
		if e != nil {
			return unavailable(e)
		}
		if !exact || cmd.phase != "committed" {
			return fault(foundation.Forbidden, nil)
		}
		kind, e := commandDeliveryKind(cmd)
		if e != nil {
			return e
		}
		if kind == c.TestDelivery {
			cfg, e := loadSMTP(ctx, x)
			if e != nil {
				return e
			}
			if !cfg.Configured || cfg.Version != cmd.expectedVersion {
				return fault(foundation.ResourceBusy, nil)
			}
			return nil
		}
		link, e := loadLink(ctx, x, c.TokenKind(kind), cmd.resource)
		if e != nil {
			return e
		}
		if !link.live || kind == c.ResetDelivery && (link.user != cmd.user || link.passwordVersion != cmd.passwordVersion) {
			return fault(foundation.ResourceDeleted, nil)
		}
	}
	return nil
}
func checkDeliveryCapacity(ctx context.Context, x postgres.SQLExecutor) error {
	var pending int
	if e := x.QueryRow(ctx, `SELECT count(*)::int FROM agenteam_account.delivery_intents i LEFT JOIN agenteam_account.mail_jobs j ON j.intent_id=i.id WHERE j.id IS NULL OR j.phase IN ('pending','claimed','sending','retry_wait','processing','unknown')`).Scan(&pending); e != nil {
		return unavailable(e)
	}
	if pending >= 10000 {
		return fault(foundation.RateLimited, nil)
	}
	return nil
}

func (s *Service) insertDelivery(ctx context.Context, x postgres.SQLExecutor, cmd commandRecord, recipient ...string) error {
	kind, e := commandDeliveryKind(cmd)
	if e != nil {
		return e
	}
	if e = checkDeliveryCapacity(ctx, x); e != nil {
		return e
	}
	initiator := cmd.user
	if cmd.actorKind != "human" {
		initiator = cmd.browser
	}
	if kind == c.TestDelivery {
		if len(recipient) != 1 {
			return invalid()
		}
		email, err := NormalizeEmail(recipient[0])
		if err != nil || email != recipient[0] {
			return invalid()
		}
		_, e = x.Exec(ctx, `INSERT INTO agenteam_account.delivery_intents(id,origin_intent_id,job_id,kind,initiator_id,recipient,created_at) VALUES($1,$1,$2,'test',$3,$4,$5)`, cmd.id.String(), cmd.attempt, initiator, email, cmd.created)
		return portError(e)
	}
	_, e = x.Exec(ctx, `INSERT INTO agenteam_account.delivery_intents(id,origin_intent_id,job_id,kind,link_id,initiator_id,created_at) VALUES($1,$1,$2,$3,$4,$5,$6)`, cmd.id.String(), cmd.attempt, string(kind), cmd.resource, initiator, cmd.created)
	return portError(e)
}
