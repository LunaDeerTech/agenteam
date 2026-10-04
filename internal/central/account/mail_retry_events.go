package account

import (
	"context"
	"crypto/subtle"
	"encoding/json"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func (s *Service) prepareRetryDelivery(ctx context.Context, cmd commandRecord, o deliveryOrigin) (identity.Actor, event.Event, oc.AppendPlan, error) {
	if nilPort(s.state().deps.Events) || s.state().deps.DeliveryRequested.Schema().EventType != c.DeliveryRequestedType {
		return identity.Actor{}, event.Event{}, oc.AppendPlan{}, fault(foundation.DependencyUnbound, nil)
	}
	u, e := parseID[identity.User](cmd.user)
	if e != nil {
		return identity.Actor{}, event.Event{}, oc.AppendPlan{}, e
	}
	session, e := parseID[identity.Session](cmd.session)
	if e != nil {
		return identity.Actor{}, event.Event{}, oc.AppendPlan{}, e
	}
	actor, e := identity.NewHuman(u, session)
	if e != nil {
		return actor, event.Event{}, oc.AppendPlan{}, e
	}
	id, _ := parseID[c.DeliveryIntent](cmd.id.String())
	evt, e := event.NewEvent(s.state().deps.DeliveryRequested, deliveryHeader(cmd), c.DeliveryRequested{IntentID: id, Kind: c.DeliveryKind(o.root.Kind)})
	if e != nil {
		return actor, evt, oc.AppendPlan{}, e
	}
	plan, e := s.state().deps.Events.PrepareAppend(ctx, actor, evt)
	return actor, evt, plan, portError(e)
}
func (a *Authority) retryEventFacts(ctx context.Context, x postgres.SQLExecutor, actor identity.Actor, summary event.Summary) (commandRecord, deliveryOrigin, error) {
	var empty deliveryOrigin
	if actor.Validate() != nil || actor.Details().Kind != identity.Human || summary.Producer != c.AccountProducer || summary.Header.EventType != c.DeliveryRequestedType {
		return commandRecord{}, empty, fault(foundation.Forbidden, nil)
	}
	cmd, e := loadCommand(ctx, x, summary.Header.AggregateID.String(), false)
	if e != nil {
		return cmd, empty, e
	}
	if cmd.name != "mail-retry" || cmd.actorKind != "human" || cmd.user != actor.Details().UserID || cmd.session != actor.Details().SessionID {
		return cmd, empty, fault(foundation.Forbidden, nil)
	}
	o, e := loadJobOrigin(ctx, x, cmd.resource)
	if e != nil {
		return cmd, o, e
	}
	want, e := json.Marshal(deliveryHeader(cmd))
	if e != nil {
		return cmd, o, unavailable(e)
	}
	got, e := json.Marshal(summary.Header)
	if e != nil {
		return cmd, o, unavailable(e)
	}
	id, _ := parseID[c.DeliveryIntent](cmd.id.String())
	payload, e := json.Marshal(c.DeliveryRequested{IntentID: id, Kind: c.DeliveryKind(o.root.Kind)})
	if e != nil {
		return cmd, o, unavailable(e)
	}
	if string(got) != string(want) || digest(payload) != summary.PayloadDigest {
		return cmd, o, fault(foundation.Forbidden, nil)
	}
	semantic, e := retrySemantic(cmd.identity, cmd.user, cmd.resource, cmd.expectedVersion)
	if e != nil {
		return cmd, o, unavailable(e)
	}
	wanted, e := a.state().keys.mac(cmd.kid, "command-v1", semantic)
	if e != nil {
		return cmd, o, e
	}
	defer clear(wanted)
	if subtle.ConstantTimeCompare(wanted, cmd.mac) != 1 {
		return cmd, o, fault(foundation.Forbidden, nil)
	}
	return cmd, o, nil
}
func (a *Authority) discoverRetryDelivery(ctx context.Context, actor identity.Actor, summary event.Summary) (oc.Dependencies, error) {
	cmd, o, e := a.retryEventFacts(ctx, a.state().store, actor, summary)
	if e != nil {
		return oc.Dependencies{}, e
	}
	if _, e = a.AuthorizeSystem(ctx, foundation.Tx{}, actor, identity.Mutate); e != nil {
		return oc.Dependencies{}, e
	}
	b, e := appendBinding(actor, summary)
	if e != nil {
		return oc.Dependencies{}, e
	}
	return oc.NewDependencies(a.state().eventIssuer, b, retryLocks(cmd, o), []byte(retryMapping(cmd, o)))
}
func (a *Authority) validateRetryDeliveryInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, summary event.Summary, deps oc.Dependencies, stage oc.Stage) error {
	x, e := a.state().store.InTx(tx)
	if e != nil {
		return unavailable(e)
	}
	cmd, o, e := a.retryEventFacts(ctx, x, actor, summary)
	if e != nil {
		return e
	}
	if string(deps.Opaque()) != string(retryMapping(cmd, o)) {
		return fault(foundation.ResourceBusy, nil)
	}
	if e = a.state().store.RequireHeldLocks(ctx, tx, retryLocks(cmd, o)); e != nil {
		return unavailable(e)
	}
	if _, e = a.AuthorizeSystem(ctx, tx, actor, identity.Mutate); e != nil {
		return e
	}
	if cmd.phase != "planned" && cmd.phase != "committed" {
		return fault(foundation.InvalidState, nil)
	}
	if stage == oc.NewFact {
		if e = retryReceipt(ctx, x, cmd, o); e != nil {
			return e
		}
		var sourceVersion int64
		if e = x.QueryRow(ctx, `SELECT version FROM agenteam_account.mail_jobs WHERE id=$1`, cmd.resource).Scan(&sourceVersion); e != nil {
			return unavailable(e)
		}
		if sourceVersion != cmd.expectedVersion+1 {
			return fault(foundation.ResourceBusy, nil)
		}
		return originLive(ctx, x, o)
	}
	return nil
}
