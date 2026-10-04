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
	if h.EventType == c.DeliveryRequestedType {
		return a.deliveryCommand(ctx, x, actor, summary)
	}
	if actor.Validate() != nil || summary.Producer != c.AccountProducer || h.EventType != c.SessionsRevokedType || h.SchemaVersion != 1 || h.AggregateType != c.UserAuthAggregate || h.Scope.Kind != event.SystemScope || h.AggregateVersion == nil || h.AggregateSequence == nil || int64(*h.AggregateVersion) != int64(*h.AggregateSequence) {
		return commandRecord{}, fault(foundation.Forbidden, nil)
	}
	var id, hash string
	var header, payload []byte
	e := x.QueryRow(ctx, `SELECT id::text,event_digest,event_header,event_payload FROM agenteam_account.commands WHERE event_id=$1`, h.EventID.String()).Scan(&id, &hash, &header, &payload)
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
	if cmd.user != h.AggregateID.String() || digest(payload) != summary.PayloadDigest {
		return commandRecord{}, fault(foundation.Forbidden, nil)
	}
	var value c.SessionsRevoked
	if json.Unmarshal(payload, &value) != nil || value.Validate() != nil || value.UserID.String() != cmd.user {
		return commandRecord{}, fault(foundation.Forbidden, nil)
	}
	d := actor.Details()
	switch cmd.name {
	case "logout", "password-change":
		if d.Kind != identity.Human || cmd.actorKind != "human" || cmd.user != d.UserID || cmd.session != d.SessionID {
			return commandRecord{}, fault(foundation.Forbidden, nil)
		}
		if cmd.name == "logout" && (value.Scope != c.OneSession || value.Reason != c.LoggedOut || value.SessionID != cmd.session) || cmd.name == "password-change" && (value.Scope != c.AllSessions || value.Reason != c.PasswordChanged) {
			return commandRecord{}, fault(foundation.Forbidden, nil)
		}
	case "reset-complete":
		if d.Kind != identity.Service || d.ServiceName != identity.AccountAuth || d.CauseRef != cmd.id.String() || cmd.actorKind != "browser" || value.Scope != c.AllSessions || value.Reason != c.PasswordWasReset {
			return commandRecord{}, fault(foundation.Forbidden, nil)
		}
	default:
		return commandRecord{}, fault(foundation.Forbidden, nil)
	}
	return cmd, nil
}
func (a *Authority) DiscoverAppend(ctx context.Context, actor identity.Actor, summary event.Summary) (oc.Dependencies, error) {
	if summary.Header.EventType == c.DeliveryRequestedType {
		cmd, e := loadCommand(ctx, a.state().store, summary.Header.AggregateID.String(), false)
		if e != nil {
			return oc.Dependencies{}, e
		}
		if cmd.name == "mail-retry" {
			return a.discoverRetryDelivery(ctx, actor, summary)
		}
	}

	cmd, e := a.appendCommand(ctx, a.state().store, actor, summary)
	if e != nil {
		return oc.Dependencies{}, e
	}
	binding, e := appendBinding(actor, summary)
	if e != nil {
		return oc.Dependencies{}, e
	}
	locks := commandLocks(cmd)
	if cmd.user != "" {
		locks = append(locks, userLock(cmd.user, foundation.Exclusive))
	}
	if summary.Header.EventType == c.DeliveryRequestedType {
		locks = append(locks, configLock("account-mail", foundation.Shared))
		return oc.NewDependencies(a.state().eventIssuer, binding, locks, []byte(commandMapping(cmd)))
	}
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
	if summary.Header.EventType == c.DeliveryRequestedType {
		x, e := a.state().store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		cmd, e := loadCommand(ctx, x, summary.Header.AggregateID.String(), false)
		if e != nil {
			return e
		}
		if cmd.name == "mail-retry" {
			return a.validateRetryDeliveryInTx(ctx, tx, actor, summary, deps, stage)
		}
		return a.validateDeliveryAppend(ctx, tx, actor, summary, deps, stage)
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
	if actor.Details().Kind == identity.Human {
		if e = a.RequireCurrentSession(ctx, tx, actor); e != nil {
			return e
		}
	} else {
		// A registered pre-auth service is not an authorization bypass. The
		// exact command was established by the original browser/token proof;
		// its browser, link and password generation must all remain current.
		var valid bool
		e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.password_resets r JOIN agenteam_account.users u ON u.id=r.user_id WHERE r.id=$1 AND r.user_id=$2 AND r.password_version=$3 AND u.password_version=r.password_version AND r.expires_at>clock_timestamp() AND $4::timestamptz>clock_timestamp())`, cmd.resource, cmd.user, cmd.passwordVersion, cmd.browserExpires).Scan(&valid)
		if e != nil {
			return unavailable(e)
		}
		if !valid {
			return fault(foundation.ResourceDeleted, nil)
		}
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
