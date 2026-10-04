package account

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

func appendBinding(actor identity.Actor, summary event.Summary) (foundation.Digest, error) {
	if actor.Validate() != nil || summary.Header.Validate() != nil || summary.PayloadDigest.Validate() != nil {
		return "", invalid()
	}
	b, e := json.Marshal(struct {
		Actor    identity.ActorDetails
		Producer event.StableName
		Header   event.Header
		Payload  foundation.Digest
	}{actor.Details(), summary.Producer, summary.Header, summary.PayloadDigest})
	if e != nil {
		return "", invalid()
	}
	return digest(b), nil
}
func (a *Authority) appendCommand(ctx context.Context, x postgres.SQLExecutor, actor identity.Actor, summary event.Summary) (commandRecord, error) {
	h := summary.Header
	if actor.Validate() != nil || actor.Details().Kind != identity.Human || summary.Producer != c.AccountProducer || h.EventType != c.SessionsRevokedType || h.SchemaVersion != 1 || h.AggregateType != c.UserAuthAggregate || h.Scope.Kind != event.SystemScope || h.AggregateID.String() != actor.Details().UserID || h.AggregateVersion == nil || h.AggregateSequence == nil || int64(*h.AggregateVersion) != int64(*h.AggregateSequence) {
		return commandRecord{}, fault(foundation.Forbidden, nil)
	}
	var id, hash string
	var header []byte
	e := x.QueryRow(ctx, `SELECT id::text,event_digest,event_header FROM agenteam_account.commands WHERE event_id=$1`, h.EventID.String()).Scan(&id, &hash, &header)
	if errors.Is(e, pgx.ErrNoRows) {
		return commandRecord{}, fault(foundation.Forbidden, nil)
	}
	if e != nil {
		return commandRecord{}, unavailable(e)
	}
	saved, e := event.DecodeHeader(header)
	if e != nil || !reflect.DeepEqual(saved, h) || hash != summary.PayloadDigest.String() {
		return commandRecord{}, fault(foundation.Forbidden, nil)
	}
	cmd, e := loadCommand(ctx, x, id, false)
	if e != nil {
		return cmd, e
	}
	if cmd.name != "logout" || cmd.actorKind != "human" || cmd.user != actor.Details().UserID || cmd.session != actor.Details().SessionID {
		return commandRecord{}, fault(foundation.Forbidden, nil)
	}
	return cmd, nil
}
func (a *Authority) DiscoverAppend(ctx context.Context, actor identity.Actor, summary event.Summary) (oc.Dependencies, error) {
	cmd, e := a.appendCommand(ctx, a.state().store, actor, summary)
	if e != nil {
		return oc.Dependencies{}, e
	}
	binding, e := appendBinding(actor, summary)
	if e != nil {
		return oc.Dependencies{}, e
	}
	locks := append(commandLocks(cmd), userLock(cmd.user, foundation.Exclusive))
	return oc.NewDependencies(a.state().eventIssuer, binding, locks, []byte(cmd.id.String()))
}
func (a *Authority) ValidateAppendInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, summary event.Summary, deps oc.Dependencies, stage oc.Stage) error {
	if !stage.Valid() {
		return invalid()
	}
	binding, e := appendBinding(actor, summary)
	if e != nil || !deps.Matches(a.state().eventIssuer, binding) {
		return fault(foundation.Forbidden, nil)
	}
	if e = a.state().store.RequireHeldLocks(ctx, tx, deps.Locks()); e != nil {
		return unavailable(e)
	}
	if e = a.RequireCurrentSession(ctx, tx, actor); e != nil {
		return e
	}
	x, e := a.state().store.InTx(tx)
	if e != nil {
		return unavailable(e)
	}
	cmd, e := a.appendCommand(ctx, x, actor, summary)
	if e != nil {
		return e
	}
	if string(deps.Opaque()) != cmd.id.String() {
		return fault(foundation.ResourceBusy, nil)
	}
	if stage == oc.NewFact {
		if cmd.phase != "planned" {
			return fault(foundation.InvalidState, nil)
		}
		u, e := loadUser(ctx, x, cmd.user)
		if e != nil {
			return e
		}
		if u.sequence != *summary.Header.AggregateSequence {
			return fault(foundation.ResourceBusy, nil)
		}
	}
	return nil
}

var _ oc.ProducerAuthority = (*Authority)(nil)
