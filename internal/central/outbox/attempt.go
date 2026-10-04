package outbox

import (
	"context"
	"errors"
	"math"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

// record deliberately excludes payload. Technical claim/recovery never needs
// a business codec or permission to read a domain table.
type record struct {
	id                                                             oc.DeliveryID
	eventID                                                        event.EventID
	handler                                                        event.StableName
	typ                                                            event.StableName
	schema                                                         uint32
	scope                                                          event.Scope
	effect                                                         oc.HandlerEffect
	phase                                                          oc.Phase
	version, cycle, cycleAttempts, lifetime, fence, pass, sequence int64
	attempt                                                        oc.AttemptID
	process                                                        oc.ProcessID
	due, deadline, created                                         time.Time
	joined                                                         *time.Time
	retired                                                        bool
	reason                                                         oc.SafeReason
}

const recordColumns = `d.id::text,d.event_id::text,d.handler_id,e.event_type,e.schema_version,d.scope,coalesce(d.project_id::text,''),h.effect,d.phase,d.version,d.redrive_cycle,d.cycle_attempts,d.lifetime_attempts,d.fence,d.recovery_pass,e.sequence,coalesce(d.current_attempt_id::text,''),coalesce(a.process_id::text,''),d.next_attempt_at,coalesce(a.deadline,d.next_attempt_at),d.created_at,coalesce(d.safe_reason,''),a.joined_at,coalesce(a.checkpoint IN ('not_committed','stopped') AND a.finished_at IS NOT NULL,false)`
const recordFrom = ` FROM agenteam_outbox.deliveries d JOIN agenteam_outbox.events e ON e.id=d.event_id JOIN agenteam_outbox.handlers h ON h.id=d.handler_id LEFT JOIN agenteam_outbox.attempts a ON a.id=d.current_attempt_id AND a.delivery_id=d.id AND a.fence=d.fence `

type scanner interface{ Scan(...any) error }

func scanRecord(row scanner) (record, error) {
	var d record
	var id, eid, handler, typ, scope, project, effect, phase, attempt, process, reason string
	var schema int64
	err := row.Scan(&id, &eid, &handler, &typ, &schema, &scope, &project, &effect, &phase, &d.version, &d.cycle, &d.cycleAttempts, &d.lifetime, &d.fence, &d.pass, &d.sequence, &attempt, &process, &d.due, &d.deadline, &d.created, &reason, &d.joined, &d.retired)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, failure(foundation.NotFound, nil)
	}
	if err != nil {
		return d, unavailable(err)
	}
	d.id, err = foundation.ParseID[oc.Delivery](id)
	if err != nil {
		return d, unavailable(err)
	}
	d.eventID, err = foundation.ParseID[event.EventIdentity](eid)
	if err != nil {
		return d, unavailable(err)
	}
	d.handler = event.StableName(handler)
	d.typ = event.StableName(typ)
	d.scope.Kind = event.ScopeKind(scope)
	d.effect = oc.HandlerEffect(effect)
	d.phase = oc.Phase(phase)
	d.reason = oc.SafeReason(reason)
	if project != "" {
		d.scope.ProjectID, err = foundation.ParseID[event.Project](project)
		if err != nil {
			return d, unavailable(err)
		}
	}
	if attempt != "" {
		d.attempt, err = foundation.ParseID[oc.Attempt](attempt)
		if err != nil {
			return d, unavailable(err)
		}
	}
	if process != "" {
		d.process, err = foundation.ParseID[oc.Process](process)
		if err != nil {
			return d, unavailable(err)
		}
	}
	if d.handler.Validate() != nil || d.typ.Validate() != nil || d.scope.Validate() != nil || !d.effect.Valid() || !d.phase.Valid() || schema < 1 || schema > math.MaxUint32 || d.version < 1 || d.cycle < 0 || d.cycleAttempts < 0 || d.cycleAttempts > 8 || d.lifetime < d.cycleAttempts || d.fence < 0 || d.pass < 0 || d.sequence < 1 || reason != "" && !d.reason.Valid() {
		return d, unavailable(nil)
	}
	if attempt == "" {
		if d.fence != 0 || process != "" || d.phase != oc.Pending && !(d.phase == oc.DeadLetter && d.scope.Kind == event.ProjectScope && d.cycleAttempts == 0 && d.lifetime == 0 && d.reason == oc.ProjectStopped) {
			return d, unavailable(nil)
		}
	} else if process == "" || d.fence < 1 {
		return d, unavailable(nil)
	}
	d.schema = uint32(schema)
	return d, nil
}
func (s *Service) deliveryRecord(ctx context.Context, x postgres.SQLExecutor, id oc.DeliveryID) (record, error) {
	return scanRecord(x.QueryRow(ctx, `SELECT `+recordColumns+recordFrom+` WHERE d.id=$1`, id.String()))
}
func (d record) identity() oc.DeliveryIdentity {
	return oc.DeliveryIdentity{EventID: d.eventID, DeliveryID: d.id, AttemptID: d.attempt, Fence: foundation.Version(d.fence), HandlerID: d.handler, Scope: d.scope, Effect: d.effect}
}
func technicalLocks(d record) []foundation.LockRequest {
	locks := []foundation.LockRequest{{Key: deliveryLock(d.id), Mode: foundation.Exclusive}}
	if d.scope.Kind == event.ProjectScope {
		k, _ := foundation.ProjectLock(d.scope.ProjectID.String())
		locks = append(locks, foundation.LockRequest{Key: k, Mode: foundation.Shared})
	}
	return locks
}

func (r *runtimeState) claim(ctx context.Context, a *execution) (record, bool, error) {
	s := r.svc
	d := a.record
	priorTerminal := d.attempt.Validate() != nil || d.joined != nil || d.retired
	if !priorTerminal && d.process != s.state().auth.Processes.CurrentProcess() {
		priorTerminal = s.state().auth.Processes.ConfirmStopped(ctx, d.process) == nil
	}
	if !priorTerminal {
		return record{}, false, failure(foundation.ResourceBusy, nil)
	}
	var saved record
	result := s.state().store.WithinTx(ctx, recoveryCause("outbox.claim"), func(ctx context.Context, tx foundation.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, technicalLocks(d)); err != nil {
			return unavailable(err)
		}
		x, err := executor(s, tx)
		if err != nil {
			return err
		}
		current, err := s.deliveryRecord(ctx, x, d.id)
		if err != nil {
			return err
		}
		if current.scope != d.scope || current.handler != d.handler || current.eventID != d.eventID {
			return failure(foundation.ResourceBusy, nil)
		}
		if current.attempt != d.attempt || current.fence != d.fence {
			return failure(foundation.ResourceBusy, nil)
		}
		if current.phase != oc.Pending && current.phase != oc.RetryWait {
			return failure(foundation.ResourceBusy, nil)
		}
		var due bool
		if err = x.QueryRow(ctx, `SELECT next_attempt_at<=clock_timestamp() FROM agenteam_outbox.deliveries WHERE id=$1`, d.id.String()).Scan(&due); err != nil {
			return unavailable(err)
		}
		if !due {
			return failure(foundation.ResourceBusy, nil)
		}
		if err = deliveryLocalGate(ctx, x, current.scope); err != nil {
			return err
		}
		s.state().mu.RLock()
		_, bound := s.state().handlers[d.handler]
		s.state().mu.RUnlock()
		if !bound || d.scope.Kind == event.ProjectScope && nilPort(s.state().auth.Projects) {
			return failure(foundation.DependencyUnbound, nil)
		}
		if current.cycleAttempts >= 8 || current.fence == math.MaxInt64 || current.lifetime == math.MaxInt64 || current.version == math.MaxInt64 {
			return failure(foundation.InvalidState, nil)
		}
		fence := current.fence + 1
		err = x.QueryRow(ctx, `INSERT INTO agenteam_outbox.attempts(id,delivery_id,process_id,fence,redrive_cycle,cycle_number,lifetime_number,deadline) VALUES($1,$2,$3,$4,$5,$6,$7,clock_timestamp()+$8*interval '1 microsecond') RETURNING deadline`, a.id.String(), d.id.String(), s.state().auth.Processes.CurrentProcess().String(), fence, current.cycle, current.cycleAttempts+1, current.lifetime+1, r.options.AttemptTimeout.Microseconds()).Scan(&current.deadline)
		if err != nil {
			return unavailable(err)
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_outbox.deliveries SET phase='processing',current_attempt_id=$2,fence=$3,cycle_attempts=cycle_attempts+1,lifetime_attempts=lifetime_attempts+1,version=version+1,safe_reason=NULL,last_at=clock_timestamp() WHERE id=$1`, d.id.String(), a.id.String(), fence)
		if err != nil {
			return unavailable(err)
		}
		current.attempt = a.id
		current.fence = fence
		current.process = s.state().auth.Processes.CurrentProcess()
		current.phase = oc.Processing
		current.cycleAttempts++
		current.lifetime++
		current.version++
		saved = current
		return nil
	})
	if result.State() == foundation.Committed {
		return saved, true, nil
	}
	if result.State() != foundation.Unknown {
		return record{}, false, commitError(result)
	}
	// Claim confirmation waits on exactly the original DB writer locks. An
	// unconfirmed/absent row is never permission to enter the callback.
	confirmed := false
	verify := s.state().store.WithinTx(ctx, recoveryCause("outbox.claim-confirm"), func(ctx context.Context, tx foundation.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, technicalLocks(d)); err != nil {
			return unavailable(err)
		}
		x, err := executor(s, tx)
		if err != nil {
			return err
		}
		current, err := s.deliveryRecord(ctx, x, d.id)
		if err != nil {
			return err
		}
		if current.attempt == a.id && current.process == s.state().auth.Processes.CurrentProcess() && current.phase == oc.Processing {
			saved = current
			confirmed = true
		}
		return nil
	})
	if verify.State() != foundation.Committed {
		return saved, false, foundation.NewFault(foundation.CommitUnknown, foundation.Unknown)
	}
	if !confirmed {
		return record{}, false, nil
	}
	return saved, true, nil
}

func retryDelay(number int64, base time.Duration) time.Duration {
	if number < 1 {
		number = 1
	}
	if number > 6 {
		return 60 * base
	}
	return time.Duration(1<<uint(number-1)) * base
}

// checkpoint is only called after this callback/transaction returned, or after
// exact foreign process death was proved. Marker absence alone is insufficient.
func (r *runtimeState) checkpoint(ctx context.Context, d record, outcome oc.Result, reason oc.SafeReason, returned *time.Time) error {
	s := r.svc
	result := s.state().store.WithinTx(ctx, recoveryCause("outbox.attempt-checkpoint"), func(ctx context.Context, tx foundation.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, technicalLocks(d)); err != nil {
			return unavailable(err)
		}
		x, err := executor(s, tx)
		if err != nil {
			return err
		}
		current, err := s.deliveryRecord(ctx, x, d.id)
		if err != nil {
			return err
		}
		if current.attempt != d.attempt || current.fence != d.fence || current.process != d.process || current.scope != d.scope {
			return failure(foundation.ResourceBusy, nil)
		}
		_, found, err := readProcessed(ctx, x, current.identity())
		if err != nil {
			return err
		}
		if found {
			return finishDeliveryInTx(ctx, x, current.identity())
		}
		if current.phase != oc.Processing {
			return nil
		}
		if !reason.Valid() {
			reason = oc.Unavailable
		}
		phase := oc.RetryWait
		checkpoint := "retry"
		if outcome.Kind() == oc.DeadLetterRequested {
			phase = oc.DeadLetter
			checkpoint = "dead_letter"
		} else if current.cycleAttempts >= 8 {
			phase = oc.Failed
			checkpoint = "failed"
		}
		if _, err = x.Exec(ctx, `UPDATE agenteam_outbox.attempts SET handler_returned_at=coalesce(handler_returned_at,$6::timestamptz),finished_at=coalesce(finished_at,clock_timestamp()),checkpoint=$4,commit_unknown=false,safe_reason=$5 WHERE id=$1 AND delivery_id=$2 AND fence=$3`, d.attempt.String(), d.id.String(), d.fence, checkpoint, string(reason), returned); err != nil {
			return unavailable(err)
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_outbox.deliveries SET phase=$4,version=version+1,safe_reason=$5,next_attempt_at=clock_timestamp()+$6*interval '1 microsecond',last_at=clock_timestamp() WHERE id=$1 AND current_attempt_id=$2 AND fence=$3 AND phase='processing'`, d.id.String(), d.attempt.String(), d.fence, string(phase), string(reason), retryDelay(current.cycleAttempts, r.options.RetryBase).Microseconds())
		if err != nil || tag.RowsAffected() != 1 {
			return unavailable(err)
		}
		return nil
	})
	return commitError(result)
}

func (r *runtimeState) markJoined(ctx context.Context, d record, returned *time.Time) error {
	if d.attempt.Validate() != nil {
		return nil
	}
	s := r.svc
	result := s.state().store.WithinTx(ctx, recoveryCause("outbox.local-join"), func(ctx context.Context, tx foundation.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, technicalLocks(d)); err != nil {
			return unavailable(err)
		}
		x, err := executor(s, tx)
		if err != nil {
			return err
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_outbox.attempts SET handler_returned_at=coalesce(handler_returned_at,$5::timestamptz),joined_at=CASE WHEN $5::timestamptz IS NOT NULL OR handler_returned_at IS NOT NULL THEN coalesce(joined_at,clock_timestamp()) ELSE NULL END,checkpoint=CASE WHEN $5::timestamptz IS NULL AND handler_returned_at IS NULL AND checkpoint IN ('retry','failed','dead_letter') THEN 'not_committed' ELSE checkpoint END WHERE id=$1 AND delivery_id=$2 AND fence=$3 AND process_id=$4`, d.attempt.String(), d.id.String(), d.fence, d.process.String(), returned)
		return portResult(err)
	})
	return commitError(result)
}

func (r *runtimeState) confirmRetirement(ctx context.Context, d record) (bool, error) {
	var exists bool
	s := r.svc
	commit := s.state().store.WithinTx(ctx, recoveryCause("outbox.claim-retire"), func(ctx context.Context, tx foundation.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, technicalLocks(d)); err != nil {
			return unavailable(err)
		}
		x, err := executor(s, tx)
		if err != nil {
			return err
		}
		err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_outbox.attempts WHERE id=$1 AND delivery_id=$2 AND fence=$3 AND process_id=$4)`, d.attempt.String(), d.id.String(), d.fence, d.process.String()).Scan(&exists)
		return portResult(err)
	})
	return exists, commitError(commit)
}
