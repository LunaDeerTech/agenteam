package outbox

import (
	"context"
	"errors"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type deliverySnapshot struct {
	identity oc.DeliveryIdentity
	process  oc.ProcessID
	phase    oc.Phase
	event    event.Event
	deadline time.Time
}

func (s *Service) readDelivery(ctx context.Context, x postgres.SQLExecutor, id oc.DeliveryID) (deliverySnapshot, error) {
	var d deliverySnapshot
	var eventID, handler, phase, attempt, process, producer, effect string
	var fence int64
	var header, payload []byte
	err := x.QueryRow(ctx, `SELECT d.event_id::text,d.handler_id,d.phase,coalesce(d.current_attempt_id::text,''),d.fence,coalesce(a.process_id::text,''),coalesce(a.deadline,d.next_attempt_at),e.producer,e.header,e.payload,h.effect FROM agenteam_outbox.deliveries d JOIN agenteam_outbox.events e ON e.id=d.event_id JOIN agenteam_outbox.handlers h ON h.id=d.handler_id LEFT JOIN agenteam_outbox.attempts a ON a.id=d.current_attempt_id AND a.delivery_id=d.id AND a.fence=d.fence WHERE d.id=$1`, id.String()).Scan(&eventID, &handler, &phase, &attempt, &fence, &process, &d.deadline, &producer, &header, &payload, &effect)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, failure(foundation.NotFound, nil)
	}
	if err != nil {
		return d, unavailable(err)
	}
	h, err := event.DecodeHeader(header)
	if err != nil || h.EventID.String() != eventID {
		return d, unavailable(err)
	}
	d.event, err = s.state().catalog.Restore(event.StableName(producer), h, payload)
	if err != nil {
		return d, err
	}
	d.phase = oc.Phase(phase)
	if !d.phase.Valid() {
		return d, unavailable(nil)
	}
	if attempt == "" || process == "" || fence < 1 {
		return d, failure(foundation.InvalidState, nil)
	}
	aid, err := foundation.ParseID[oc.Attempt](attempt)
	if err != nil {
		return d, unavailable(err)
	}
	d.process, err = foundation.ParseID[oc.Process](process)
	if err != nil {
		return d, unavailable(err)
	}
	d.identity = oc.DeliveryIdentity{EventID: h.EventID, DeliveryID: id, AttemptID: aid, Fence: foundation.Version(fence), HandlerID: event.StableName(handler), Scope: h.Scope, Effect: oc.HandlerEffect(effect)}
	if !d.identity.Valid() {
		return d, unavailable(nil)
	}
	return d, nil
}
func readProcessed(ctx context.Context, x postgres.SQLExecutor, d oc.DeliveryIdentity) (oc.ProcessedReceipt, bool, error) {
	var delivery, attempt, digest string
	var fence int64
	var at time.Time
	err := x.QueryRow(ctx, `SELECT delivery_id::text,attempt_id::text,fence,processed_at,result_digest FROM agenteam_outbox.processed WHERE event_id=$1 AND handler_id=$2`, d.EventID.String(), string(d.HandlerID)).Scan(&delivery, &attempt, &fence, &at, &digest)
	if errors.Is(err, pgx.ErrNoRows) {
		return oc.ProcessedReceipt{}, false, nil
	}
	if err != nil {
		return oc.ProcessedReceipt{}, false, unavailable(err)
	}
	if delivery != d.DeliveryID.String() || attempt != d.AttemptID.String() || fence != int64(d.Fence) {
		return oc.ProcessedReceipt{}, false, failure(foundation.InvalidState, nil)
	}
	instant, err := foundation.NewInstant(at)
	sum := foundation.Digest(digest)
	if err != nil || sum.Validate() != nil {
		return oc.ProcessedReceipt{}, false, unavailable(err)
	}
	return oc.ProcessedReceipt{EventID: d.EventID, HandlerID: d.HandlerID, DeliveryID: d.DeliveryID, AttemptID: d.AttemptID, Fence: d.Fence, ProcessedAt: instant, ResultDigest: sum}, true, nil
}
