package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"math"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type requeueCommand struct {
	identity     foundation.CommandIdentity
	hash, digest foundation.Digest
	delivery     oc.DeliveryID
	user         string
	expected     foundation.Version
	reason       oc.RequeueReason
}

func newRequeueCommand(actor identity.Actor, meta foundation.CommandMeta, d record, expected foundation.Version, reason oc.RequeueReason) (requeueCommand, error) {
	var c requeueCommand
	if d.scope.Validate() != nil || actor.Validate() != nil || actor.Details().Kind != identity.Human || meta.Validate() != nil || expected.Validate() != nil || !reason.Valid() || meta.ExpectedVersion != nil && *meta.ExpectedVersion != expected {
		return c, invalid()
	}
	user := actor.Details().UserID
	owners := []string{user}
	project := ""
	if d.scope.Kind == event.ProjectScope {
		project = d.scope.ProjectID.String()
		owners = append(owners, project)
	}
	id, err := foundation.NewCommandIdentity("outbox", owners, "requeue", meta.IdempotencyKey)
	if err != nil {
		return c, invalid()
	}
	raw, err := json.Marshal(struct {
		Scope, Project, User, Delivery string
		Expected                       foundation.Version
		Reason                         oc.RequeueReason
	}{string(d.scope.Kind), project, user, d.id.String(), expected, reason})
	if err != nil {
		return c, invalid()
	}
	return requeueCommand{id, oc.DigestBytes([]byte(id.Canonical())), oc.DigestBytes(raw), d.id, user, expected, reason}, nil
}
func readRequeue(ctx context.Context, x postgres.SQLExecutor, c requeueCommand) (oc.RequeueReceipt, bool, error) {
	var id, digest, user string
	var cycle, version int64
	err := x.QueryRow(ctx, `SELECT delivery_id::text,semantic_digest,user_id::text,redrive_cycle,result_version FROM agenteam_outbox.requeue_commands WHERE command_hash=$1`, c.hash.String()).Scan(&id, &digest, &user, &cycle, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return oc.RequeueReceipt{}, false, nil
	}
	if err != nil {
		return oc.RequeueReceipt{}, false, unavailable(err)
	}
	if digest != c.digest.String() || user != c.user || id != c.delivery.String() {
		return oc.RequeueReceipt{}, false, failure(foundation.IdempotencyKeyReused, nil)
	}
	if cycle < 1 || version <= int64(c.expected) {
		return oc.RequeueReceipt{}, false, unavailable(nil)
	}
	return oc.RequeueReceipt{DeliveryID: c.delivery, Cycle: foundation.Progress(cycle), Version: foundation.Version(version)}, true, nil
}
func (s *Service) Requeue(ctx context.Context, actor identity.Actor, meta foundation.CommandMeta, id oc.DeliveryID, expected foundation.Version, reason oc.RequeueReason) (oc.RequeueReceipt, error) {
	var empty oc.RequeueReceipt
	if id.Validate() != nil || meta.Validate() != nil || expected.Validate() != nil || !reason.Valid() {
		return empty, invalid()
	}
	ctx, done, err := s.beginAdministration(ctx)
	if err != nil {
		return empty, err
	}
	defer done()
	if err = s.requireHuman(ctx, foundation.Tx{}, actor); err != nil {
		return empty, err
	}
	d, err := s.deliveryRecord(ctx, s.state().store, id)
	if err != nil {
		return empty, err
	}
	scope, err := identityScope(d.scope)
	if err != nil {
		return empty, err
	}
	current, err := s.planHuman(ctx, actor, scope, oc.RequeueProject, d, oc.CurrentAccess)
	if err != nil {
		return empty, err
	}
	c, err := newRequeueCommand(actor, meta, d, expected, reason)
	if err != nil {
		return empty, err
	}
	key, _ := foundation.CommandLock(c.identity)
	locks := append(current.locks, foundation.LockRequest{Key: key, Mode: foundation.Exclusive}, foundation.LockRequest{Key: deliveryLock(id), Mode: foundation.Exclusive})
	cause, _ := foundation.NewCommandsCause(c.identity)
	var saved oc.RequeueReceipt
	var found bool
	check := func(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor) error {
		if err := s.authorizeHuman(ctx, tx, current, identity.Read); err != nil {
			return err
		}
		now, err := s.deliveryRecord(ctx, x, id)
		if err != nil {
			return err
		}
		if now.scope != d.scope || now.eventID != d.eventID || now.handler != d.handler || now.effect != d.effect {
			return failure(foundation.ResourceBusy, nil)
		}
		if err = deliveryLocalGate(ctx, x, d.scope); err != nil {
			return err
		}
		saved, found, err = readRequeue(ctx, x, c)
		return err
	}
	// The receipt-only stage deliberately needs no new-fact planner/handler or
	// Audit port. Current visibility is still checked on every replay.
	first := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, locks); err != nil {
			return unavailable(err)
		}
		x, err := executor(s, tx)
		if err != nil {
			return err
		}
		return check(ctx, tx, x)
	})
	if err = commitError(first); err != nil {
		return empty, err
	}
	if found {
		return saved, nil
	}
	fresh, err := s.planHuman(ctx, actor, scope, oc.RequeueProject, d, oc.NewFact)
	if err != nil {
		return empty, err
	}
	locks = append(locks, fresh.locks...)
	if nilPort(s.state().auth.Audit) {
		return empty, failure(foundation.DependencyUnbound, nil)
	}
	result := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, locks); err != nil {
			return unavailable(err)
		}
		x, err := executor(s, tx)
		if err != nil {
			return err
		}
		if err = check(ctx, tx, x); err != nil || found {
			return err
		}
		if err = s.authorizeHuman(ctx, tx, fresh, identity.Mutate); err != nil {
			return err
		}
		now, err := s.deliveryRecord(ctx, x, id)
		if err != nil {
			return err
		}
		if now.version != int64(expected) {
			return failure(foundation.VersionConflict, nil)
		}
		if now.phase != oc.Failed && now.phase != oc.DeadLetter {
			return failure(foundation.InvalidState, nil)
		}
		var processed bool
		if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_outbox.processed WHERE delivery_id=$1)`, id.String()).Scan(&processed); err != nil {
			return unavailable(err)
		}
		if processed {
			return failure(foundation.InvalidState, nil)
		}
		s.state().mu.RLock()
		_, bound := s.state().handlers[now.handler]
		s.state().mu.RUnlock()
		if !bound {
			return failure(foundation.DependencyUnbound, nil)
		}
		if now.version == math.MaxInt64 || now.cycle == math.MaxInt64 {
			return failure(foundation.InvalidState, nil)
		}
		metadata, err := ac.OutboxRequeueMetadata(ac.OutboxRequeueFields{DeliveryID: id.String(), EventID: now.eventID.String(), HandlerID: string(now.handler), FromState: string(now.phase), RedriveCycle: foundation.Version(now.cycle + 1), Reason: ac.RequeueReason(reason)})
		if err != nil {
			return err
		}
		resource, err := ac.NewResource(ac.OutboxDeliveryResource, id.String())
		if err != nil {
			return err
		}
		entry, err := ac.NewEntry(ac.EntryFields{Scope: scope, Actor: actor, Action: ac.OutboxDeliveryRequeue, Outcome: ac.Success, Resource: resource, Metadata: metadata, Associations: ac.Associations{RequestID: meta.RequestID.String()}})
		if err != nil {
			return err
		}
		appendKey, err := audit.CommandAppendKey(ac.OutboxProducer, c.identity, 0)
		if err != nil {
			return err
		}
		receipt, err := s.state().auth.Audit.AppendInTx(ctx, tx, entry, appendKey)
		if err != nil {
			return portError(err)
		}
		if receipt.AuditID.Validate() != nil {
			return unavailable(nil)
		}
		if _, err = x.Exec(ctx, `UPDATE agenteam_outbox.deliveries SET phase='pending',version=version+1,redrive_cycle=redrive_cycle+1,cycle_attempts=0,next_attempt_at=clock_timestamp(),safe_reason=NULL,last_at=clock_timestamp() WHERE id=$1`, id.String()); err != nil {
			return unavailable(err)
		}
		if _, err = x.Exec(ctx, `INSERT INTO agenteam_outbox.requeue_commands(command_hash,semantic_digest,user_id,delivery_id,from_state,expected_version,redrive_cycle,result_version,reason_code,audit_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, c.hash.String(), c.digest.String(), c.user, id.String(), string(now.phase), int64(expected), now.cycle+1, now.version+1, string(reason), receipt.AuditID.String()); err != nil {
			return unavailable(err)
		}
		saved = oc.RequeueReceipt{DeliveryID: id, Cycle: foundation.Progress(now.cycle + 1), Version: foundation.Version(now.version + 1)}
		return nil
	})
	if result.State() == foundation.Unknown {
		found = false
		confirmation := s.state().store.WithinTx(ctx, recoveryCause("outbox.requeue-confirm"), func(ctx context.Context, tx foundation.Tx) error {
			if err := s.state().store.AcquireAll(ctx, tx, locks); err != nil {
				return unavailable(err)
			}
			x, err := executor(s, tx)
			if err != nil {
				return err
			}
			return check(ctx, tx, x)
		})
		if confirmation.State() != foundation.Committed || !found {
			return empty, foundation.NewFault(foundation.CommitUnknown, foundation.Unknown)
		}
	} else if err = commitError(result); err != nil {
		return empty, err
	}
	s.state().mu.RLock()
	runtime := s.state().runtime
	s.state().mu.RUnlock()
	if runtime != nil {
		select {
		case runtime.data().wake <- struct{}{}:
		default:
		}
	}
	return saved, nil
}

var _ oc.DeliveryAdministration = (*Service)(nil)
