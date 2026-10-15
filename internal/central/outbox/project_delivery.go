package outbox

import (
	"context"
	"reflect"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

type projectDeliveryKey struct{}
type projectDeliveryWitness struct {
	service *Service
	state   *serviceState
	request oc.ProjectRequest
	tx      f.Tx
	process oc.ProcessID
	locks   []f.LockRequest
	active  bool
}

// Only the original delivery preparation/application lends this witness. It
// does not create a claim, acquire locks, or authorize a handler's own writes.
func (s *Service) withProjectDelivery(ctx context.Context, tx f.Tx, snap deliverySnapshot, locks []f.LockRequest, fn func(context.Context, oc.ProjectRequest) error) error {
	request, err := deliveryProjectRequest(snap)
	if err != nil {
		return err
	}
	r := s.state()
	if snap.phase != oc.Processing || nilPort(r.auth.Processes) || snap.process != r.auth.Processes.CurrentProcess() {
		return failure(f.Forbidden, nil)
	}
	w := &projectDeliveryWitness{service: s, state: r, request: request, tx: tx, process: snap.process, locks: append([]f.LockRequest(nil), locks...), active: true}
	defer func() { w.active = false }()
	return fn(context.WithValue(ctx, projectDeliveryKey{}, w), request)
}

// RequireProjectDelivery verifies the exact live Outbox call rather than a
// caller-provided Service actor or DeliveryIdentity. A zero tx is discovery
// only. Validation requires the original full lock union, live Store tx and
// original process guard; copied contexts expire with their callback.
func RequireProjectDelivery(ctx context.Context, tx f.Tx, store any, request oc.ProjectRequest) error {
	if ctx == nil || request.Validate() != nil || request.Details().Kind != oc.DeliverProject {
		return invalid()
	}
	w, _ := ctx.Value(projectDeliveryKey{}).(*projectDeliveryWitness)
	if w == nil || !w.active || w.service == nil || w.service.data == nil || w.service.state() != w.state || w.tx != tx || !sameProjectDeliveryRequest(w.request, request) {
		return failure(f.Forbidden, nil)
	}
	if nilPort(store) || !reflect.TypeOf(store).Comparable() || w.state.store != store {
		return failure(f.Forbidden, nil)
	}
	if nilPort(w.state.auth.Processes) || w.process.Validate() != nil || w.state.auth.Processes.CurrentProcess() != w.process {
		return failure(f.Forbidden, nil)
	}
	if tx.Valid() {
		if _, err := w.state.store.InTx(tx); err != nil {
			return portError(err)
		}
		if len(w.locks) == 0 {
			return failure(f.Forbidden, nil)
		}
		if err := w.state.store.RequireHeldLocks(ctx, tx, w.locks); err != nil {
			return portError(err)
		}
		guard, ok := w.state.auth.Processes.(interface {
			RequireCurrentProcessInTx(context.Context, f.Tx, oc.ProcessID) error
		})
		if !ok || nilPort(guard) {
			return failure(f.DependencyUnbound, nil)
		}
		if err := guard.RequireCurrentProcessInTx(ctx, tx, w.process); err != nil {
			return portError(err)
		}
	}
	return ctx.Err()
}

func sameProjectDeliveryRequest(a, b oc.ProjectRequest) bool {
	x, y := a.Details(), b.Details()
	// Actor's sealed representation contains a function; compare its issued
	// identity through Equal, not reflection over that representation.
	return x.Kind == y.Kind && x.ProjectID == y.ProjectID && x.Stage == y.Stage &&
		x.Actor.Equal(y.Actor) && x.Delivery == y.Delivery && reflect.DeepEqual(x.Event, y.Event)
}
