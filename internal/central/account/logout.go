package account

import (
	"context"
	"crypto/subtle"
	"math"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type LogoutRequest struct {
	Actor identity.Actor
	Key   foundation.IdempotencyKey
}

func (s *Service) Logout(ctx context.Context, request LogoutRequest) error {
	if request.Actor.Validate() != nil || request.Actor.Details().Kind != identity.Human || request.Key.Validate() != nil {
		return invalid()
	}
	op, e := s.begin(ctx, false)
	if e != nil {
		return e
	}
	defer s.finish(op)
	ctx = op.ctx
	st := s.state()
	if nilPort(st.deps.Events) || st.deps.SessionsRevoked.Schema().EventType != c.SessionsRevokedType {
		return fault(foundation.DependencyUnbound, nil)
	}
	if e = st.deps.Authority.RequireCurrentSession(ctx, foundation.Tx{}, request.Actor); e != nil {
		return e
	}
	a := request.Actor.Details()
	command, e := foundation.NewCommandIdentity("account.logout", []string{a.UserID}, "logout", request.Key)
	if e != nil {
		return e
	}
	cause, e := foundation.NewCommandsCause(command)
	if e != nil {
		return e
	}
	newID, e := foundation.NewID[c.Command]()
	if e != nil {
		return unavailable(e)
	}
	eventID, e := foundation.NewID[event.EventIdentity]()
	if e != nil {
		return unavailable(e)
	}
	macFor := func(kid string) ([]byte, error) {
		return st.keys.mac(kid, "command-v1", []byte("logout"), []byte(a.UserID), []byte(a.SessionID))
	}
	mac, e := macFor(st.keys.current())
	if e != nil {
		return e
	}
	locks := []foundation.LockRequest{commandLock(command), configLock("account-directory", foundation.Shared), configLock("account-security", foundation.Shared), userLock(a.UserID, foundation.Exclusive), recordLock(newID.String())}
	var cmd commandRecord
	var evt event.Event
	replanEvent := func(ctx context.Context, tx foundation.Tx, cmd commandRecord, next foundation.Sequence) error {
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		var rawHeader, rawPayload []byte
		var savedID, savedDigest string
		if e = x.QueryRow(ctx, `SELECT event_id::text,event_header,event_payload,event_digest FROM agenteam_account.commands WHERE id=$1`, cmd.id.String()).Scan(&savedID, &rawHeader, &rawPayload, &savedDigest); e != nil {
			return unavailable(e)
		}
		h, e := event.DecodeHeader(rawHeader)
		if e != nil {
			return unavailable(e)
		}
		p, e := (event.JSONCodec[c.SessionsRevoked]{}).Decode(rawPayload)
		if e != nil {
			return unavailable(e)
		}
		evt, e = event.NewEvent(st.deps.SessionsRevoked, h, p)
		if e != nil {
			return portError(e)
		}
		if h.EventID.String() != savedID || h.Scope.Kind != event.SystemScope || h.AggregateID.String() != a.UserID || h.AggregateSequence == nil || h.AggregateVersion == nil || int64(*h.AggregateVersion) != int64(*h.AggregateSequence) || p.UserID.String() != a.UserID || p.SessionID != a.SessionID || p.Scope != c.OneSession || p.Reason != c.LoggedOut || evt.Summary().PayloadDigest.String() != savedDigest {
			return fault(foundation.InvalidState, nil)
		}
		if *h.AggregateSequence > next {
			return fault(foundation.InvalidState, nil)
		}
		if *h.AggregateSequence == next {
			return nil
		}
		// The original Command and User writers serialize this still-planned
		// event with any old COMMIT. Only its internal sequence/version change;
		// EventID, occurrence time, payload and command semantics remain fixed.
		// PrepareAppend below must obtain a fresh exact-header binding.
		version := foundation.Version(next)
		h.AggregateSequence, h.AggregateVersion = &next, &version
		evt, e = event.NewEvent(st.deps.SessionsRevoked, h, p)
		if e != nil {
			return portError(e)
		}
		header, e := evt.HeaderJSON()
		if e != nil {
			return portError(e)
		}
		tag, e := x.Exec(ctx, `UPDATE agenteam_account.commands SET event_header=$2::jsonb WHERE id=$1 AND phase='planned'`, cmd.id.String(), string(header))
		if e != nil {
			return unavailable(e)
		}
		if tag.RowsAffected() != 1 {
			return fault(foundation.ResourceBusy, nil)
		}
		return nil
	}
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
			return unavailable(e)
		}
		if e := st.deps.Authority.RequireCurrentSession(ctx, tx, request.Actor); e != nil {
			return e
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		old, e := loadCommand(ctx, x, string(digest([]byte(command.Canonical()))), true)
		if e == nil {
			expected, e := macFor(old.kid)
			if e != nil {
				return e
			}
			if old.name != "logout" || old.actorKind != "human" || old.user != a.UserID || old.session != a.SessionID || old.identity.Canonical() != command.Canonical() || subtle.ConstantTimeCompare(old.mac, expected) != 1 {
				return fault(foundation.IdempotencyKeyReused, nil)
			}
			if old.phase != "planned" || old.resultCode != "" || old.resultVersion != 0 {
				return fault(foundation.InvalidState, nil)
			}
			cmd = old
			u, e := loadUser(ctx, x, a.UserID)
			if e != nil {
				return e
			}
			if int64(u.sequence) == math.MaxInt64 {
				return fault(foundation.InvalidState, nil)
			}
			return replanEvent(ctx, tx, cmd, u.sequence+1)
		}
		if !hasFaultCode(e, foundation.NotFound) {
			return e
		}
		u, e := loadUser(ctx, x, a.UserID)
		if e != nil {
			return e
		}
		if int64(u.sequence) == math.MaxInt64 {
			return fault(foundation.InvalidState, nil)
		}
		seq := u.sequence + 1
		version := foundation.Version(seq)
		var now time.Time
		if e = x.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
			return unavailable(e)
		}
		aggregate, e := foundation.ParseID[event.Aggregate](a.UserID)
		if e != nil {
			return e
		}
		payload := c.SessionsRevoked{UserID: u.user.ID, Scope: c.OneSession, SessionID: a.SessionID, Reason: c.LoggedOut}
		evt, e = event.NewEvent(st.deps.SessionsRevoked, event.Header{EventID: eventID, EventType: c.SessionsRevokedType, SchemaVersion: 1, OccurredAt: instant(now), Scope: event.Scope{Kind: event.SystemScope}, AggregateType: c.UserAuthAggregate, AggregateID: aggregate, AggregateVersion: &version, AggregateSequence: &seq}, payload)
		if e != nil {
			return e
		}
		header, e := evt.HeaderJSON()
		if e != nil {
			return e
		}
		_, e = x.Exec(ctx, `INSERT INTO agenteam_account.commands(id,namespace,owner_id,command_key,command_name,identity_digest,semantic_kid,semantic_mac,actor_kind,user_id,session_id,origin_process_id,phase,event_id,event_header,event_payload,event_digest) VALUES($1,'account.logout',$2,$3,'logout',$4,$5,$6,'human',$2,$7,$8,'planned',$9,$10::jsonb,$11,$12)`, newID.String(), a.UserID, string(request.Key), string(digest([]byte(command.Canonical()))), st.keys.current(), mac, a.SessionID, st.process.String(), eventID.String(), string(header), evt.PayloadBytes(), string(evt.Summary().PayloadDigest))
		if e != nil {
			return unavailable(e)
		}
		cmd, e = loadCommand(ctx, x, newID.String(), false)
		return e
	})
	// No business side effect has happened during planning. Unknown remains a
	// checkpoint; an external retry still proves its current Session first.
	if e = resultError(result); e != nil {
		return e
	}
	plan, e := st.deps.Events.PrepareAppend(ctx, request.Actor, evt)
	if e != nil {
		return portError(e)
	}
	locks = append(commandLocks(cmd), userLock(a.UserID, foundation.Exclusive))
	locks = append(locks, plan.Locks()...)
	result = st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
			return unavailable(e)
		}
		if e := st.deps.Authority.RequireCurrentSession(ctx, tx, request.Actor); e != nil {
			return e
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		current, e := loadCommand(ctx, x, cmd.id.String(), false)
		if e != nil {
			return e
		}
		expected, e := macFor(current.kid)
		if e != nil {
			return e
		}
		if current.name != "logout" || current.user != a.UserID || current.session != a.SessionID || subtle.ConstantTimeCompare(current.mac, expected) != 1 {
			return fault(foundation.IdempotencyKeyReused, nil)
		}
		if current.phase != "planned" {
			return fault(foundation.InvalidState, nil)
		}
		seq := *evt.Header().AggregateSequence
		m, e := ac.AccountMetadata(ac.AccountLogout, ac.AccountMetadataFields{UserID: a.UserID, SessionID: a.SessionID, Version: foundation.Version(seq), Phase: ac.AccountRevoked})
		if e != nil {
			return e
		}
		resource, e := ac.NewResource(ac.SessionResource, a.SessionID)
		if e != nil {
			return e
		}
		entry, e := ac.NewEntry(ac.EntryFields{Scope: identity.SystemScope(), Actor: request.Actor, Action: ac.AccountLogout, Outcome: ac.Success, Resource: resource, Metadata: m})
		if e != nil {
			return e
		}
		key, e := ac.NewAppendKey(ac.AccountProducer, cmd.id.String(), 0)
		if e != nil {
			return e
		}
		if _, e = st.deps.Audit.AppendInTx(ctx, tx, entry, key); e != nil {
			return portError(e)
		}
		tag, e := x.Exec(ctx, `UPDATE agenteam_account.users SET auth_sequence=$2 WHERE id=$1 AND auth_sequence=$2-1`, a.UserID, int64(seq))
		if e != nil {
			return unavailable(e)
		}
		if tag.RowsAffected() != 1 {
			return fault(foundation.ResourceBusy, nil)
		}
		if _, e = st.deps.Events.AppendEventInTx(ctx, tx, request.Actor, evt, plan); e != nil {
			return portError(e)
		}
		if _, e = x.Exec(ctx, `UPDATE agenteam_account.sessions SET revoked_at=clock_timestamp(),revoked_reason='logout' WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL`, a.SessionID, a.UserID); e != nil {
			return unavailable(e)
		}
		_, e = x.Exec(ctx, `UPDATE agenteam_account.commands SET phase='committed',result_code='COMPLETED',result_version=$2,completed_at=clock_timestamp() WHERE id=$1`, cmd.id.String(), int64(seq))
		return portError(e)
	})
	if result.State() == foundation.Unknown {
		confirmation := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
			if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
				return unavailable(e)
			}
			x, e := st.store.InTx(tx)
			if e != nil {
				return unavailable(e)
			}
			current, e := loadCommand(ctx, x, cmd.id.String(), false)
			if e != nil {
				return e
			}
			expected, e := macFor(current.kid)
			if e != nil {
				return e
			}
			if current.name != "logout" || current.user != a.UserID || current.session != a.SessionID || subtle.ConstantTimeCompare(current.mac, expected) != 1 {
				return fault(foundation.IdempotencyKeyReused, nil)
			}
			var revoked bool
			e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_account.sessions WHERE id=$1 AND user_id=$2 AND revoked_reason='logout' AND revoked_at IS NOT NULL)`, a.SessionID, a.UserID).Scan(&revoked)
			if e != nil {
				return unavailable(e)
			}
			if current.phase != "committed" || !revoked {
				return fault(foundation.CommitUnknown, nil)
			}
			return nil
		})
		if e = resultError(confirmation); e != nil {
			return confirmationError(result, e)
		}
		return nil
	}
	return resultError(result)
}
