package account

import (
	"context"
	"math"
	"reflect"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// The caller holds the original command/User writers. Only a still-planned
// event may move to the next internal sequence, retaining EventID and payload.
func (s *Service) planPasswordEvent(ctx context.Context, x postgres.SQLExecutor, cmd commandRecord, u userRecord, reason c.RevocationReason) (event.Event, error) {
	if u.sequence == foundation.Sequence(math.MaxInt64) {
		return event.Event{}, fault(foundation.InvalidState, nil)
	}
	next := u.sequence + 1
	version := foundation.Version(next)
	payload := c.SessionsRevoked{UserID: u.user.ID, Scope: c.AllSessions, Reason: reason}
	var rawHeader, rawPayload []byte
	var dg string
	if e := x.QueryRow(ctx, `SELECT event_header,event_payload,coalesce(event_digest,'') FROM agenteam_account.commands WHERE id=$1 AND phase='planned'`, cmd.id.String()).Scan(&rawHeader, &rawPayload, &dg); e != nil {
		return event.Event{}, unavailable(e)
	}
	var header event.Header
	if len(rawHeader) > 0 {
		var e error
		header, e = event.DecodeHeader(rawHeader)
		if e != nil {
			return event.Event{}, unavailable(e)
		}
		old, e := (event.JSONCodec[c.SessionsRevoked]{}).Decode(rawPayload)
		if e != nil {
			return event.Event{}, unavailable(e)
		}
		if !reflect.DeepEqual(old, payload) || digest(rawPayload).String() != dg || header.AggregateID.String() != cmd.user || header.EventType != c.SessionsRevokedType || header.Scope.Kind != event.SystemScope || header.AggregateSequence == nil || header.AggregateVersion == nil || int64(*header.AggregateVersion) != int64(*header.AggregateSequence) || *header.AggregateSequence > next {
			return event.Event{}, fault(foundation.InvalidState, nil)
		}
	} else {
		id, e := foundation.NewID[event.EventIdentity]()
		if e != nil {
			return event.Event{}, unavailable(e)
		}
		aggregate, e := foundation.ParseID[event.Aggregate](cmd.user)
		if e != nil {
			return event.Event{}, e
		}
		header = event.Header{EventID: id, EventType: c.SessionsRevokedType, SchemaVersion: 1, OccurredAt: instant(cmd.created), Scope: event.Scope{Kind: event.SystemScope}, AggregateType: c.UserAuthAggregate, AggregateID: aggregate}
	}
	header.AggregateSequence = &next
	header.AggregateVersion = &version
	evt, e := event.NewEvent(s.state().deps.SessionsRevoked, header, payload)
	if e != nil {
		return evt, e
	}
	h, e := evt.HeaderJSON()
	if e != nil {
		return evt, e
	}
	p := evt.PayloadBytes()
	_, e = x.Exec(ctx, `UPDATE agenteam_account.commands SET event_id=$2,event_header=$3::jsonb,event_payload=$4,event_digest=$5 WHERE id=$1 AND phase='planned'`, cmd.id.String(), header.EventID.String(), string(h), p, evt.Summary().PayloadDigest.String())
	return evt, portError(e)
}
