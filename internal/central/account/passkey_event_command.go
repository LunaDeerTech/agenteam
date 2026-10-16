package account

import (
	"context"
	"encoding/json"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type securityEventPlanner func(context.Context, postgres.SQLExecutor, commandRecord, userRecord) (*event.Event, error)

// The planned event is durable before PrepareAppend. The final transaction
// rechecks the current session and sequence, commits the proof and mutation,
// and appends the revocation fact together with the recoverable receipt.
func (s *Service) securityCommandWithEvent(ctx context.Context, actor identity.Actor, key foundation.IdempotencyKey, name string, semantic []byte, planEvent securityEventPlanner, change func(context.Context, foundation.Tx, postgres.SQLExecutor, c.CommandID) (any, error)) (json.RawMessage, error) {
	if planEvent == nil {
		return s.securityCommand(ctx, actor, key, name, semantic, change)
	}
	if actor.Validate() != nil || actor.Details().Kind != identity.Human || key.Validate() != nil {
		return nil, invalid()
	}
	op, e := s.begin(ctx, false)
	if e != nil {
		return nil, e
	}
	defer s.finish(op)
	ctx = op.ctx
	identityKey, e := foundation.NewCommandIdentity("account.security", []string{actor.Details().UserID}, name, key)
	if e != nil {
		return nil, invalid()
	}
	if e = s.trackMutation(identityKey, op); e != nil {
		return nil, e
	}
	cause, e := foundation.NewCommandsCause(identityKey)
	if e != nil {
		return nil, e
	}
	id, e := foundation.NewID[c.Command]()
	if e != nil {
		return nil, unavailable(e)
	}
	st := s.state()
	var out json.RawMessage
	var cmd commandRecord
	var evt *event.Event
	mac := func(kid string) ([]byte, error) {
		return st.keys.mac(kid, "command-v1", []byte(name), []byte(actor.Details().UserID), []byte(actor.Details().SessionID), semantic)
	}
	locks := []foundation.LockRequest{commandLock(identityKey), configLock("account-directory", foundation.Exclusive), configLock("account-security", foundation.Shared), configLock("account-mail", foundation.Exclusive), userLock(actor.Details().UserID, foundation.Exclusive)}
	savedReceipt := func(ctx context.Context, x postgres.SQLExecutor, id string) error {
		return portError(x.QueryRow(ctx, `SELECT receipt FROM agenteam_account.security_receipts WHERE command_id=$1`, id).Scan(&out))
	}
	saveReceipt := func(ctx context.Context, x postgres.SQLExecutor, id string, receipt any) error {
		var e error
		out, e = json.Marshal(receipt)
		if e != nil {
			return unavailable(e)
		}
		if _, e = x.Exec(ctx, `INSERT INTO agenteam_account.security_receipts(command_id,receipt) VALUES($1,$2)`, id, out); e != nil {
			return unavailable(e)
		}
		return completeCommand(ctx, x, id, 1)
	}
	planned := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
			return unavailable(e)
		}
		if e := st.deps.Authority.RequireCurrentSession(ctx, tx, actor); e != nil {
			return e
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		old, e := loadCommand(ctx, x, string(digest([]byte(identityKey.Canonical()))), true)
		if e == nil {
			if e = s.checkCommand(old, identityKey, mac); e != nil {
				return e
			}
			if old.phase == "committed" {
				return savedReceipt(ctx, x, old.id.String())
			}
			if old.phase != "planned" {
				return fault(foundation.InvalidState, nil)
			}
			cmd = old
		} else {
			if !hasFaultCode(e, foundation.NotFound) {
				return e
			}
			sig, e := mac(st.keys.current())
			if e != nil {
				return e
			}
			defer clear(sig)
			_, e = x.Exec(ctx, `INSERT INTO agenteam_account.commands(id,namespace,owner_id,command_key,command_name,identity_digest,semantic_kid,semantic_mac,actor_kind,user_id,session_id,origin_process_id,resource_id,phase) VALUES($1,'account.security',$2,$3,$4,$5,$6,$7,'human',$2,$8,$9,$2,'planned')`, id.String(), actor.Details().UserID, string(key), name, string(digest([]byte(identityKey.Canonical()))), st.keys.current(), sig, actor.Details().SessionID, st.process.String())
			if e != nil {
				return unavailable(e)
			}
			cmd, e = loadCommand(ctx, x, id.String(), false)
			if e != nil {
				return e
			}
		}
		u, e := loadUser(ctx, x, cmd.user)
		if e != nil {
			return e
		}
		evt, e = planEvent(ctx, x, cmd, u)
		if e != nil {
			return e
		}
		if evt != nil {
			if evt.Validate() != nil {
				return invalid()
			}
			return nil
		}
		receipt, e := change(ctx, tx, x, cmd.id)
		if e != nil {
			return e
		}
		return saveReceipt(ctx, x, cmd.id.String(), receipt)
	})
	if e = resultError(planned); e != nil {
		return nil, e
	}
	if out != nil {
		return out, nil
	}
	if evt == nil {
		return nil, fault(foundation.InvalidState, nil)
	}
	plan, e := st.deps.Events.PrepareAppend(ctx, actor, *evt)
	if e != nil {
		return nil, portError(e)
	}
	all := append(append([]foundation.LockRequest{}, locks...), commandLocks(cmd)...)
	all = append(all, plan.Locks()...)
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, all); e != nil {
			return unavailable(e)
		}
		if e := st.deps.Authority.RequireCurrentSession(ctx, tx, actor); e != nil {
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
		if e = s.checkCommand(current, identityKey, mac); e != nil {
			return e
		}
		if current.phase == "committed" {
			return savedReceipt(ctx, x, current.id.String())
		}
		if current.phase != "planned" || commandMapping(current) != commandMapping(cmd) {
			return fault(foundation.ResourceBusy, nil)
		}
		u, e := loadUser(ctx, x, cmd.user)
		if e != nil {
			return e
		}
		header := evt.Header()
		if header.AggregateSequence == nil || u.sequence+1 != *header.AggregateSequence {
			return fault(foundation.ResourceBusy, nil)
		}
		receipt, e := change(ctx, tx, x, cmd.id)
		if e != nil {
			return e
		}
		raw, e := json.Marshal(receipt)
		if e != nil {
			return unavailable(e)
		}
		var rejection struct {
			Rejected string `json:"rejected"`
		}
		_ = json.Unmarshal(raw, &rejection)
		if rejection.Rejected == "" {
			if _, e = x.Exec(ctx, `UPDATE agenteam_account.users SET auth_sequence=$2 WHERE id=$1`, cmd.user, int64(*header.AggregateSequence)); e != nil {
				return unavailable(e)
			}
			if _, e = st.deps.Events.AppendEventInTx(ctx, tx, actor, *evt, plan); e != nil {
				return portError(e)
			}
		} else {
			if _, e = x.Exec(ctx, `UPDATE agenteam_account.commands SET event_id=NULL,event_header=NULL,event_payload=NULL,event_digest=NULL WHERE id=$1`, cmd.id.String()); e != nil {
				return unavailable(e)
			}
		}
		return saveReceipt(ctx, x, cmd.id.String(), receipt)
	})
	return out, resultError(result)
}
