package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type deliveryPlanData struct {
	issuer      *serviceState
	snapshot    deliverySnapshot
	handler     oc.HandlerDefinition
	handlerPlan oc.HandlerPlan
	project     oc.Dependencies
	locks       []foundation.LockRequest
}

// DeliveryPlan is issued only for a committed, current claim belonging to this
// process. It is preparation, not a cached authorization or commit result.
type DeliveryPlan struct{ data func() deliveryPlanData }

func (p DeliveryPlan) Locks() []foundation.LockRequest {
	if p.data == nil {
		return nil
	}
	return append([]foundation.LockRequest(nil), p.data().locks...)
}
func (p DeliveryPlan) Identity() oc.DeliveryIdentity {
	if p.data == nil {
		return oc.DeliveryIdentity{}
	}
	return p.data().snapshot.identity
}
func (p DeliveryPlan) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "outbox_delivery_plan") }
func (p DeliveryPlan) MarshalJSON() ([]byte, error) { return []byte(`"outbox_delivery_plan"`), nil }
func (*DeliveryPlan) UnmarshalJSON([]byte) error    { return invalid() }
func (p DeliveryPlan) LogValue() slog.Value         { return slog.StringValue("outbox_delivery_plan") }

func (s *Service) PrepareDelivery(ctx context.Context, id oc.DeliveryID) (plan DeliveryPlan, err error) {
	defer func() {
		if recover() != nil {
			plan = DeliveryPlan{}
			err = failure(foundation.InternalError, nil)
		}
	}()
	r := s.state()
	if id.Validate() != nil {
		return plan, invalid()
	}
	if nilPort(r.auth.Processes) {
		return plan, failure(foundation.DependencyUnbound, nil)
	}
	snap, err := s.readDelivery(ctx, r.store, id)
	if err != nil {
		return plan, err
	}
	if snap.phase != oc.Processing || snap.process != r.auth.Processes.CurrentProcess() {
		return plan, failure(foundation.InvalidState, nil)
	}
	r.mu.RLock()
	handler, ok := r.handlers[snap.identity.HandlerID]
	r.mu.RUnlock()
	if !ok {
		return plan, failure(foundation.DependencyUnbound, nil)
	}
	if !supports(handler, snap.event.Header()) {
		return plan, failure(foundation.SchemaUnsupported, nil)
	}
	hp, err := handler.Handler.Prepare(ctx, snap.event)
	if err != nil {
		return plan, portError(err)
	}
	if !hp.Matches(handler.ID, snap.event) {
		return plan, invalid()
	}
	locks := hp.Locks()
	locks = append(locks, foundation.LockRequest{Key: registryLock(), Mode: foundation.Shared}, foundation.LockRequest{Key: deliveryLock(id), Mode: foundation.Exclusive}, foundation.LockRequest{Key: streamLock(snap), Mode: foundation.Exclusive})
	var project oc.Dependencies
	if snap.identity.Scope.Kind == event.ProjectScope {
		if nilPort(r.auth.Projects) {
			return plan, failure(foundation.DependencyUnbound, nil)
		}
		request, err := deliveryProjectRequest(snap)
		if err != nil {
			return plan, err
		}
		project, err = r.auth.Projects.Discover(ctx, request)
		if err != nil {
			return plan, portError(err)
		}
		if project.Validate() != nil {
			return plan, unavailable(nil)
		}
		key, _ := foundation.ProjectLock(snap.identity.Scope.ProjectID.String())
		locks = append(locks, foundation.LockRequest{Key: key, Mode: foundation.Shared})
		locks = append(locks, project.Locks()...)
	}
	locks, err = oc.NormalizeLocks(locks)
	if err != nil {
		return plan, err
	}
	d := deliveryPlanData{r, snap, handler, hp, project, locks}
	return DeliveryPlan{func() deliveryPlanData { return d }}, nil
}
func streamLock(snap deliverySnapshot) foundation.LockKey {
	h := snap.event.Header()
	b, _ := json.Marshal(struct {
		Handler event.StableName
		Scope   event.Scope
		Type    event.StableName
		ID      event.AggregateID
	}{snap.identity.HandlerID, h.Scope, h.AggregateType, h.AggregateID})
	k, _ := foundation.RecordLock(foundation.OutboxRecordLock, "stream:"+oc.DigestBytes(b).String())
	return k
}
func deliveryProjectRequest(snap deliverySnapshot) (oc.ProjectRequest, error) {
	d := snap.identity
	cause, err := d.CauseRef()
	if err != nil {
		return oc.ProjectRequest{}, err
	}
	role, _ := identity.RegisterService(identity.OutboxDelivery)
	scope, err := oc.ScopeIdentity(d.Scope)
	if err != nil {
		return oc.ProjectRequest{}, err
	}
	actor, err := role.Actor(cause, scope)
	if err != nil {
		return oc.ProjectRequest{}, invalid()
	}
	pid, err := foundation.ParseID[identity.Project](d.Scope.ProjectID.String())
	if err != nil {
		return oc.ProjectRequest{}, invalid()
	}
	return oc.NewProjectRequest(oc.ProjectRequestDetails{Kind: oc.DeliverProject, ProjectID: pid, Actor: actor, Event: snap.event.Summary(), Delivery: d})
}

// ApplyDelivery owns the delivery-cause transaction and its one complete lock
// acquisition. Only the registered HandleInTx callback supplies domain writes.
// Retry/Reject/panic rollback here; durable scheduling is the dispatcher's job.
// Unknown never triggers another callback or an implicit transaction retry.
func (s *Service) ApplyDelivery(ctx context.Context, plan DeliveryPlan) (foundation.CommitResult, oc.Result) {
	commit, outcome, _ := s.applyDeliveryObserved(ctx, plan)
	return commit, outcome
}
func (s *Service) applyDeliveryObserved(ctx context.Context, plan DeliveryPlan) (foundation.CommitResult, oc.Result, *time.Time) {
	if plan.data == nil || plan.data().issuer != s.state() {
		return foundation.NotCommittedResult(foundation.NewFault(foundation.InvalidArgument, foundation.NotCommitted)), oc.Result{}, nil
	}
	d := plan.data()
	ctx, cancel := context.WithDeadline(ctx, d.snapshot.deadline)
	defer cancel()
	cause, _ := foundation.NewDeliveryCause(d.snapshot.identity.EventID.String(), string(d.snapshot.identity.HandlerID))
	var outcome oc.Result
	var returned *time.Time
	commit := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) (err error) {
		defer func() {
			if recover() != nil {
				outcome = oc.Retry(oc.HandlerPanic)
				err = failure(foundation.InternalError, nil)
			}
		}()
		if err = s.state().store.AcquireAll(ctx, tx, d.locks); err != nil {
			return unavailable(err)
		}
		outcome, err = s.applyDeliveryInTx(ctx, tx, d, &returned)
		return err
	})
	return commit, outcome, returned
}
func (s *Service) applyDeliveryInTx(ctx context.Context, tx foundation.Tx, d deliveryPlanData, returned **time.Time) (oc.Result, error) {
	if err := s.state().store.RequireHeldLocks(ctx, tx, d.locks); err != nil {
		return oc.Result{}, unavailable(err)
	}
	x, err := executor(s, tx)
	if err != nil {
		return oc.Result{}, err
	}
	current, err := s.readDelivery(ctx, x, d.snapshot.identity.DeliveryID)
	if err != nil {
		return oc.Result{}, err
	}
	before, _ := oc.EventDigest(d.snapshot.event)
	after, _ := oc.EventDigest(current.event)
	if current.identity != d.snapshot.identity || current.process != d.snapshot.process || before != after {
		return oc.Result{}, failure(foundation.ResourceBusy, nil)
	}
	if err = deliveryLocalGate(ctx, x, current.identity.Scope); err != nil {
		return oc.Result{}, err
	}
	receipt, found, err := readProcessed(ctx, x, current.identity)
	if err != nil {
		return oc.Result{}, err
	}
	if found {
		if current.phase != oc.Succeeded && current.phase != oc.Processing {
			return oc.Result{}, failure(foundation.InvalidState, nil)
		}
		if err = finishDeliveryInTx(ctx, x, current.identity); err != nil {
			return oc.Result{}, err
		}
		return oc.Ack(receipt.ResultDigest), nil
	}
	if current.phase != oc.Processing {
		return oc.Result{}, failure(foundation.InvalidState, nil)
	}
	if current.identity.Scope.Kind == event.ProjectScope {
		request, err := deliveryProjectRequest(current)
		if err != nil {
			return oc.Result{}, err
		}
		if err = s.state().auth.Projects.ValidateInTx(ctx, tx, request, d.project); err != nil {
			return oc.Result{}, portError(err)
		}
	}
	if err = d.handler.Handler.ValidateInTx(ctx, tx, current.event, d.handlerPlan); err != nil {
		return oc.Result{}, portError(err)
	}
	result := func() oc.Result {
		defer func() { at := time.Now().UTC().Truncate(time.Microsecond); *returned = &at }()
		return d.handler.Handler.HandleInTx(ctx, tx, current.event, d.handlerPlan)
	}()
	if !result.Valid() {
		return oc.Reject(oc.InvalidEvent), failure(foundation.InvalidState, nil)
	}
	if result.Kind() != oc.Acknowledged {
		return result, failure(foundation.InvalidState, nil)
	}
	id := current.identity
	if _, err = x.Exec(ctx, `INSERT INTO agenteam_outbox.processed(event_id,handler_id,delivery_id,attempt_id,fence,result_digest) VALUES($1,$2,$3,$4,$5,$6)`, id.EventID.String(), string(id.HandlerID), id.DeliveryID.String(), id.AttemptID.String(), int64(id.Fence), result.Digest().String()); err != nil {
		return oc.Result{}, unavailable(err)
	}
	if err = finishDeliveryInTx(ctx, x, id, *returned); err != nil {
		return oc.Result{}, err
	}
	return result, nil
}
func finishDeliveryInTx(ctx context.Context, x postgres.SQLExecutor, id oc.DeliveryIdentity, returned ...*time.Time) error {
	var at any
	if len(returned) > 0 && returned[0] != nil {
		at = *returned[0]
	}
	tag, err := x.Exec(ctx, `UPDATE agenteam_outbox.attempts SET handler_returned_at=coalesce(handler_returned_at,$4::timestamptz),finished_at=coalesce(finished_at,clock_timestamp()),checkpoint='succeeded',commit_unknown=false,safe_reason=NULL WHERE id=$1 AND delivery_id=$2 AND fence=$3`, id.AttemptID.String(), id.DeliveryID.String(), int64(id.Fence), at)
	if err != nil || tag.RowsAffected() != 1 {
		return unavailable(err)
	}
	// joined_at is deliberately untouched: a DB row cannot prove a goroutine
	// and its transaction/checkpoint cleanup have actually joined.
	tag, err = x.Exec(ctx, `UPDATE agenteam_outbox.deliveries SET phase='succeeded',version=CASE WHEN phase='succeeded' THEN version ELSE version+1 END,safe_reason=NULL,last_at=clock_timestamp() WHERE id=$1 AND current_attempt_id=$2 AND fence=$3 AND phase IN ('processing','succeeded')`, id.DeliveryID.String(), id.AttemptID.String(), int64(id.Fence))
	if err != nil || tag.RowsAffected() != 1 {
		return unavailable(err)
	}
	return nil
}
func deliveryLocalGate(ctx context.Context, x postgres.SQLExecutor, scope event.Scope) error {
	if scope.Kind != event.ProjectScope {
		return nil
	}
	var action string
	err := x.QueryRow(ctx, `SELECT action FROM agenteam_outbox.project_lifecycle WHERE project_id=$1`, scope.ProjectID.String()).Scan(&action)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return unavailable(err)
	}
	if action == "delete" {
		return failure(foundation.ResourceDeleted, nil)
	}
	return nil
}

// ConfirmProcessed serializes with the exact Apply transaction before reading
// its marker. Absence is only an observation; it does not authorize re-execution
// or release an attempt. The dispatcher must separately prove actual join/death.
func (s *Service) ConfirmProcessed(ctx context.Context, plan DeliveryPlan) (oc.ProcessedReceipt, bool, error) {
	if plan.data == nil || plan.data().issuer != s.state() {
		return oc.ProcessedReceipt{}, false, invalid()
	}
	d := plan.data()
	var receipt oc.ProcessedReceipt
	var found bool
	locks := []foundation.LockRequest{{Key: deliveryLock(d.snapshot.identity.DeliveryID), Mode: foundation.Exclusive}}
	if d.snapshot.identity.Scope.Kind == event.ProjectScope {
		key, _ := foundation.ProjectLock(d.snapshot.identity.Scope.ProjectID.String())
		locks = append(locks, foundation.LockRequest{Key: key, Mode: foundation.Shared})
	}
	result := s.state().store.WithinTx(ctx, recoveryCause("outbox.marker-confirm"), func(ctx context.Context, tx foundation.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, locks); err != nil {
			return unavailable(err)
		}
		x, err := executor(s, tx)
		if err != nil {
			return err
		}
		receipt, found, err = readProcessed(ctx, x, d.snapshot.identity)
		return err
	})
	if err := commitError(result); err != nil {
		// Failure of the verification transaction does not classify the old
		// Apply as rolled back. Its original outcome remains unresolved.
		return oc.ProcessedReceipt{}, false, foundation.NewFault(foundation.CommitUnknown, foundation.Unknown).WithCause(err)
	}
	return receipt, found, nil
}
