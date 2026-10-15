package object

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"reflect"
	"sync/atomic"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// TransferMaintenanceAuthority converges only an original Skill GET whose
// independent retirement proof was already validated and persisted. Its public
// DTOs cannot create the private Recover call that authorizes these methods.
// Construction precedes Object Service and performs no I/O or mutable binding.
type TransferMaintenanceAuthority struct{ store Store }

func NewTransferMaintenanceAuthority(store Store) (*TransferMaintenanceAuthority, error) {
	if nilPort(store) {
		return nil, failure(f.DependencyUnbound, nil)
	}
	if !reflect.ValueOf(store).Comparable() {
		return nil, invalid()
	}
	if _, ok := store.(interface {
		RequireHeldLocks(context.Context, f.Tx, []f.LockRequest) error
	}); !ok {
		return nil, failure(f.DependencyUnbound, nil)
	}
	return &TransferMaintenanceAuthority{store: store}, nil
}

type transferMaintenanceKey struct{}
type transferMaintenanceWitness struct {
	service *TransferService
	actor   id.Actor
	row     transferRow
	active  atomic.Bool
}

// Only the real bounded Recover enumeration calls this entry. A copy of the
// original row identifies work; it does not itself prove retirement or death.
func (t *TransferService) transferMaintenanceContext(ctx context.Context, actor id.Actor, r transferRow) (context.Context, func()) {
	if r.spec.Details().Direction != oc.TransferGET || r.spec.Details().Owner.Details().Kind != oc.SkillRevision {
		return ctx, func() {}
	}
	w := &transferMaintenanceWitness{service: t, actor: actor, row: r}
	w.active.Store(true)
	return context.WithValue(ctx, transferMaintenanceKey{}, w), func() { w.active.Store(false) }
}

func (a *TransferMaintenanceAuthority) witness(ctx context.Context) (*transferMaintenanceWitness, error) {
	if a == nil || nilPort(a.store) {
		return nil, failure(f.DependencyUnbound, nil)
	}
	if ctx == nil {
		return nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, unavailable(err)
	}
	w, _ := ctx.Value(transferMaintenanceKey{}).(*transferMaintenanceWitness)
	if w == nil || !w.active.Load() || w.service == nil || w.service.data == nil || w.service.state().objects == nil {
		return nil, failure(f.Forbidden, nil)
	}
	s := w.service.state().objects.state()
	if !sameProjectAuditStore(a.store, s.store) || w.row.spec.Validate() != nil || w.actor.Validate() != nil {
		return nil, failure(f.Forbidden, nil)
	}
	d, original := w.actor.Details(), w.row.spec.Details()
	if d.Kind != id.Service || d.ServiceName != id.ObjectMaintenance || d.CauseRef != w.row.id.String() || d.ProjectID != original.Owner.Details().ProjectID || original.Direction != oc.TransferGET || original.Owner.Details().Kind != oc.SkillRevision {
		return nil, failure(f.Forbidden, nil)
	}
	if w.row.retirement.Validate() != nil || len(w.row.retirementDigest) != sha256.Size || (oc.TransferEvidenceRef{ID: w.row.retirement, Kind: w.row.retirementKind}).Validate() != nil {
		// A pending reference, elapsed time or old process exit is not a proof.
		return nil, failure(f.ResourceBusy, nil)
	}
	return w, nil
}

func maintenanceRowBinding(r transferRow) (f.Digest, error) {
	// Include the complete immutable request plus original initiator and every
	// mutable checkpoint used for retirement. No secret material is projected.
	registration, _ := id.RegisterService(id.ObjectMaintenance)
	actor, err := registration.Actor(r.id.String(), r.spec.Details().Owner.Scope())
	if err != nil {
		return "", invalid()
	}
	request, err := r.request(actor, oc.TransferCleanup, nil, nil, oc.PreparedPayload{})
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal([]any{request.Details().Transfer.Fingerprint(), r.actor.Details(), r.stable, r.digest, r.runnerGeneration, r.operationVersion, r.execution.String(), r.phase, r.revoked, r.cleanup, r.completed.String(), r.completedDigest, r.retirement.String(), r.retirementKind, r.retirementDigest, r.pendingRetirement.String(), r.pendingKind, r.expires, r.leaseActive})
	if err != nil {
		return "", invalid()
	}
	hash := sha256.Sum256(raw)
	return newDigest(hash[:]), nil
}

func (a *TransferMaintenanceAuthority) request(ctx context.Context, request oc.TransferAccessRequest) (*transferMaintenanceWitness, error) {
	w, err := a.witness(ctx)
	if err != nil {
		return nil, err
	}
	if request.Validate() != nil {
		return nil, invalid()
	}
	d := request.Details()
	var evidence *oc.TransferEvidenceRef
	switch d.Operation {
	case oc.TransferCleanup:
	case oc.TransferConfirmTerminal:
		evidence = &oc.TransferEvidenceRef{ID: w.row.retirement, Kind: w.row.retirementKind}
	default:
		return nil, failure(f.Forbidden, nil)
	}
	expected, err := w.row.request(w.actor, d.Operation, evidence, nil, oc.PreparedPayload{})
	if err != nil || !expected.Details().Transfer.Equal(request) {
		return nil, failure(f.Forbidden, nil)
	}
	return w, nil
}

func (a *TransferMaintenanceAuthority) dependencies(ctx context.Context, x postgres.SQLExecutor, w *transferMaintenanceWitness) (oc.AccessDependencies, error) {
	request, err := w.row.request(w.actor, oc.TransferCleanup, nil, nil, oc.PreparedPayload{})
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	facts, err := w.service.state().objects.transferAccessFacts(ctx, x, request.Details().Transfer)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	binding, err := maintenanceRowBinding(w.row)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	return oc.NewAccessDependencies(binding, facts.locks)
}

func (a *TransferMaintenanceAuthority) Discover(ctx context.Context, request oc.TransferAccessRequest) (oc.AccessDependencies, error) {
	w, err := a.request(ctx, request)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	return a.dependencies(ctx, a.store, w)
}

func (a *TransferMaintenanceAuthority) validate(ctx context.Context, tx f.Tx, w *transferMaintenanceWitness, plan oc.AccessDependencies) error {
	if plan.Validate() != nil {
		return invalid()
	}
	held := a.store.(interface {
		RequireHeldLocks(context.Context, f.Tx, []f.LockRequest) error
	})
	if err := held.RequireHeldLocks(ctx, tx, plan.Locks()); err != nil {
		return portError(err)
	}
	s := w.service.state().objects.state()
	guard, ok := s.auth.Processes.(interface {
		RequireCurrentProcessInTx(context.Context, f.Tx, oc.ProcessID) error
	})
	if !ok || nilPort(guard) {
		return failure(f.DependencyUnbound, nil)
	}
	if err := guard.RequireCurrentProcessInTx(ctx, tx, s.process); err != nil {
		return portError(err)
	}
	x, err := a.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	actual, err := a.dependencies(ctx, x, w)
	if err != nil {
		return err
	}
	if !actual.Equal(plan) {
		return failure(f.Forbidden, nil)
	}
	current, found, err := loadTransfer(ctx, x, w.row.id)
	if err != nil {
		return err
	}
	if !found {
		return failure(f.Forbidden, nil)
	}
	old, err := maintenanceRowBinding(w.row)
	if err != nil {
		return err
	}
	now, err := maintenanceRowBinding(current)
	if err != nil || old != now {
		return accessChanged()
	}
	var lease bool
	if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_object.object_leases WHERE id=$1 AND object_id=$2 AND owner_kind='transfer' AND owner_id=$3 AND process_id IS NULL AND (state='active')=$4)`, current.lease.String(), current.object.String(), current.id.String(), current.leaseActive).Scan(&lease); err != nil {
		return unavailable(err)
	}
	if !lease || !w.active.Load() {
		return failure(f.Forbidden, nil)
	}
	return unavailableIf(ctx.Err())
}

func (a *TransferMaintenanceAuthority) ValidateInTx(ctx context.Context, tx f.Tx, request oc.TransferAccessRequest, plan oc.AccessDependencies) (oc.TransferAuthorization, error) {
	w, err := a.request(ctx, request)
	if err != nil {
		return oc.TransferAuthorization{}, err
	}
	if err = a.validate(ctx, tx, w, plan); err != nil {
		return oc.TransferAuthorization{}, err
	}
	d := oc.TransferAuthorizationDetails{Request: request, RunnerGeneration: w.row.runnerGeneration, OperationVersion: w.row.operationVersion, ExecutionID: w.row.execution}
	if request.Details().Operation == oc.TransferConfirmTerminal {
		d.Retirement = &oc.TransferRetirement{Evidence: oc.TransferEvidenceRef{ID: w.row.retirement, Kind: w.row.retirementKind}, Digest: newDigest(w.row.retirementDigest), AllRequestsJoined: true, StorageAdmissionClosed: true}
	}
	return oc.NewTransferAuthorization(d)
}

func (a *TransferMaintenanceAuthority) leaseRequest(ctx context.Context, request oc.AccessRequest) (*transferMaintenanceWitness, error) {
	w, err := a.witness(ctx)
	if err != nil {
		return nil, err
	}
	if request.Validate() != nil {
		return nil, invalid()
	}
	d := request.Details()
	if d.Kind != oc.LeaseAccess || d.Operation != oc.ReleaseUseAccess || !d.Actor.Equal(w.actor) || d.ObjectID != w.row.object || d.LeaseOwner.Details().Kind != oc.TransferOwner || d.LeaseOwner.Details().ID != w.row.id.String() {
		return nil, failure(f.Forbidden, nil)
	}
	return w, nil
}

func (a *TransferMaintenanceAuthority) DiscoverMaintenanceLease(ctx context.Context, request oc.AccessRequest) (oc.AccessDependencies, error) {
	w, err := a.leaseRequest(ctx, request)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	return a.dependencies(ctx, a.store, w)
}

func (a *TransferMaintenanceAuthority) ValidateMaintenanceLeaseInTx(ctx context.Context, tx f.Tx, request oc.AccessRequest, plan oc.AccessDependencies) error {
	w, err := a.leaseRequest(ctx, request)
	if err != nil {
		return err
	}
	return a.validate(ctx, tx, w, plan)
}

func (a *TransferMaintenanceAuthority) AuthorizeLeaseInTx(ctx context.Context, tx f.Tx, actor id.Actor, object oc.ObjectID, owner oc.LeaseOwner, action oc.LeaseAction) error {
	if action != oc.ReleaseLease {
		return failure(f.Forbidden, nil)
	}
	request, err := oc.NewLeaseAccess(oc.AccessRequestDetails{Operation: oc.ReleaseUseAccess, Actor: actor, ObjectID: object, LeaseOwner: owner})
	if err != nil {
		return err
	}
	w, err := a.leaseRequest(ctx, request)
	if err != nil {
		return err
	}
	x, err := a.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	plan, err := a.dependencies(ctx, x, w)
	if err != nil {
		return err
	}
	return a.ValidateMaintenanceLeaseInTx(ctx, tx, request, plan)
}

var _ oc.RunnerTransferAuthority = (*TransferMaintenanceAuthority)(nil)
var _ oc.LeaseAuthority = (*TransferMaintenanceAuthority)(nil)
