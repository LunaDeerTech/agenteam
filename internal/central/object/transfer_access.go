package object

import (
	"context"
	"crypto/sha256"
	"encoding/json"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type transferPlanner struct {
	base      oc.AccessPlanner
	authority oc.RunnerTransferAuthority
}

func NewTransferAccessPlanner(base oc.AccessPlanner, authority oc.RunnerTransferAuthority) oc.AccessPlanner {
	return &transferPlanner{base, authority}
}
func (p *transferPlanner) Discover(ctx context.Context, r oc.AccessRequest) (oc.AccessDependencies, error) {
	if r.Validate() != nil {
		return oc.AccessDependencies{}, invalid()
	}
	if r.Details().Kind == oc.TransferAccess {
		if nilPort(p.authority) {
			return oc.AccessDependencies{}, failure(foundation.DependencyUnbound, nil)
		}
		return p.authority.Discover(ctx, r.Details().Transfer)
	}
	if nilPort(p.base) {
		return oc.AccessDependencies{}, failure(foundation.DependencyUnbound, nil)
	}
	return p.base.Discover(ctx, r)
}
func (p *transferPlanner) ValidateInTx(ctx context.Context, tx foundation.Tx, r oc.AccessRequest, d oc.AccessDependencies) error {
	if r.Validate() != nil {
		return invalid()
	}
	if r.Details().Kind == oc.TransferAccess {
		if nilPort(p.authority) {
			return failure(foundation.DependencyUnbound, nil)
		}
		a, err := p.authority.ValidateInTx(ctx, tx, r.Details().Transfer, d)
		if err != nil {
			return err
		}
		if !a.Matches(r.Details().Transfer) {
			return failure(foundation.Forbidden, nil)
		}
		return nil
	}
	if nilPort(p.base) {
		return failure(foundation.DependencyUnbound, nil)
	}
	return p.base.ValidateInTx(ctx, tx, r, d)
}

func (s *Service) transferAccessFacts(ctx context.Context, e postgres.SQLExecutor, r oc.TransferAccessRequest) (accessFacts, error) {
	var out accessFacts
	d := r.Details()
	spec := d.Spec.Details()
	issue, err := transferCommand(spec.Owner, d.IssueCommand.IdempotencyKey)
	if err != nil {
		return out, err
	}
	current, found, err := loadTransferCommand(ctx, e, issue)
	if err != nil {
		return out, err
	}
	object := d.ObjectID
	var existingUpload uploadRow
	var uploadFound bool
	if found {
		object = current.object
	}
	var uploadCommand foundation.CommandIdentity
	if spec.Direction == oc.TransferPUT {
		uploadCommand, err = commandIdentity(spec.Owner, spec.UploadCommand.IdempotencyKey)
		if err != nil {
			return out, err
		}
		existingUpload, uploadFound, err = loadCommand(ctx, e, uploadCommand)
		if err != nil {
			return out, err
		}
		if uploadFound {
			object = existingUpload.object
		}
	}
	out.objects = []oc.ObjectID{object}
	mode := foundation.Exclusive
	if d.Operation == oc.TransferInspect {
		mode = foundation.Shared
	}
	add := func(k foundation.LockKey, m foundation.LockMode) {
		out.locks = append(out.locks, foundation.LockRequest{Key: k, Mode: m})
	}
	key, _ := foundation.CommandLock(issue)
	add(key, mode)
	if uploadCommand.Validate() == nil {
		key, _ = foundation.CommandLock(uploadCommand)
		add(key, mode)
	}
	for _, name := range []string{"runner-transfer-authority"} {
		key, _ = foundation.SystemConfigLock(name)
		add(key, foundation.Shared)
	}
	if d.Operation == oc.TransferIssue {
		key, _ = foundation.SystemConfigLock("object-transfer-admission")
		add(key, foundation.Exclusive)
	}
	if spec.Direction == oc.TransferPUT && (d.Operation == oc.TransferIssue || d.Operation == oc.TransferReserveCandidate) {
		key, _ = foundation.SystemConfigLock("object-attempt-admission")
		add(key, foundation.Exclusive)
	}
	key, _ = foundation.ProjectLock(spec.Owner.Details().ProjectID)
	add(key, foundation.Shared)
	a := d.Actor.Details()
	if a.UserID != "" {
		key, _ = foundation.UserLock(a.UserID)
		add(key, foundation.Shared)
	}
	if a.AgentID != "" {
		key, _ = foundation.AgentLock(a.AgentID)
		add(key, foundation.Shared)
	}
	if a.ExecutionID != "" {
		key, _ = foundation.AggregateLock(foundation.ExecutionAggregate, a.ExecutionID)
		add(key, foundation.Shared)
	}
	key, _ = foundation.AggregateLock(foundation.ObjectAggregate, object.String())
	add(key, mode)
	for _, name := range []string{"object-owner:" + string(spec.Owner.Details().Kind) + ":" + spec.Owner.Details().ID, "object-transfer:" + d.ID.String(), "object-lease:transfer:" + d.ID.String()} {
		key, err = foundation.RecordLock(foundation.ReferenceRecordLock, name)
		if err != nil {
			return out, invalid()
		}
		add(key, mode)
	}
	if d.CancelCommand != nil {
		c, err := foundation.NewCommandIdentity("object-transfer", []string{d.ID.String()}, "cancel", d.CancelCommand.IdempotencyKey)
		if err != nil {
			return out, invalid()
		}
		key, _ = foundation.CommandLock(c)
		add(key, foundation.Exclusive)
	}
	var facts any
	if found {
		facts = []any{current.id.String(), current.object.String(), current.upload.String(), current.staging.String(), current.candidate.String(), current.lease.String(), current.sourceLease.String(), int64(current.version)}
	}
	var upload any
	if uploadFound {
		upload = []any{existingUpload.id.String(), existingUpload.object.String(), existingUpload.attempt.String()}
	}
	raw, err := json.Marshal([]any{object.String(), issue.Canonical(), facts, upload})
	if err != nil {
		return out, invalid()
	}
	hash := sha256.Sum256(raw)
	out.binding = newDigest(hash[:])
	return out, nil
}
func (t *TransferService) authorize(ctx context.Context, tx foundation.Tx, request oc.AccessRequest, plan oc.AccessLockPlan, locked oc.LockedAccess) (oc.TransferAuthorization, error) {
	if err := t.state().objects.ValidateAccessPlanInTx(ctx, tx, request, plan, locked); err != nil {
		return oc.TransferAuthorization{}, err
	}
	if nilPort(t.state().authority) {
		return oc.TransferAuthorization{}, failure(foundation.DependencyUnbound, nil)
	}
	a, err := t.state().authority.ValidateInTx(ctx, tx, request.Details().Transfer, plan.Details().Dependencies)
	if err != nil {
		return oc.TransferAuthorization{}, portError(err)
	}
	if !a.Matches(request.Details().Transfer) {
		return oc.TransferAuthorization{}, failure(foundation.Forbidden, nil)
	}
	e, err := executor(t.state().objects, tx)
	if err != nil {
		return oc.TransferAuthorization{}, err
	}
	r, exists, err := loadTransfer(ctx, e, request.Details().Transfer.Details().ID)
	if err != nil {
		return oc.TransferAuthorization{}, err
	}
	if exists && (r.runnerGeneration != a.Details().RunnerGeneration || r.operationVersion != a.Details().OperationVersion || r.execution != a.Details().ExecutionID) {
		return oc.TransferAuthorization{}, failure(foundation.Forbidden, nil)
	}
	// Runner/Operation authority cannot stand in for the owning domain. Check
	// these ports before command semantics, versions or historical results. A
	// terminal fact may converge an archived operation without admitting a new
	// payload. Maintenance instead uses its exact cleanup/lease ports below.
	d := request.Details().Transfer.Details()
	if d.Actor.Details().Kind != identity.Service {
		intent, gateIntent := identity.Mutate, identity.Mutate
		switch d.Operation {
		case oc.TransferInspect:
			intent, gateIntent = identity.Read, identity.Read
		case oc.TransferCancel, oc.TransferConfirmTerminal:
			intent, gateIntent = identity.Converge, identity.Converge
		case oc.TransferCapture:
			if d.Spec.Details().Direction == oc.TransferGET || exists && r.phase == "complete" {
				intent, gateIntent = identity.Converge, identity.Converge
			}
		case oc.TransferIssue, oc.TransferMaterialize:
			if d.Spec.Details().Direction == oc.TransferGET {
				intent = identity.Read
			}
		}
		s := t.state().objects
		if _, err = s.authorize(ctx, tx, d.Actor, d.Spec.Details().Owner, intent); err != nil {
			return oc.TransferAuthorization{}, err
		}
		if err = s.gate(ctx, tx, d.Actor, d.Spec.Details().Owner, gateIntent); err != nil {
			return oc.TransferAuthorization{}, err
		}
	}
	return a, nil
}
func (t *TransferService) within(ctx context.Context, request oc.AccessRequest, extras []oc.AccessRequest, fn func(context.Context, foundation.Tx, []oc.AccessLockPlan, oc.LockedAccess, oc.TransferAuthorization) error) foundation.CommitResult {
	requests := append([]oc.AccessRequest{request}, extras...)
	plans := make([]oc.AccessLockPlan, len(requests))
	for i, r := range requests {
		p, err := t.state().objects.DiscoverAccess(ctx, r)
		if err != nil {
			return rejectedAccess(err)
		}
		plans[i] = p
	}
	return t.state().objects.state().store.WithinTx(ctx, recoveryCause(), func(ctx context.Context, tx foundation.Tx) error {
		locked, err := t.state().objects.AcquireAccessPlansInTx(ctx, tx, plans, nil)
		if err != nil {
			return err
		}
		a, err := t.authorize(ctx, tx, request, plans[0], locked)
		if err != nil {
			return err
		}
		for i := 1; i < len(requests); i++ {
			if err = t.state().objects.ValidateAccessPlanInTx(ctx, tx, requests[i], plans[i], locked); err != nil {
				return err
			}
		}
		return fn(ctx, tx, plans, locked, a)
	})
}
func transferReadRequest(actor identity.Actor, owner oc.ObjectOwner, id oc.ObjectID) oc.AccessRequest {
	r, _ := oc.NewObjectReadAccess(oc.AccessRequestDetails{Operation: oc.StatAccess, Actor: actor, Owner: owner, ObjectID: id, Intent: identity.Read})
	return r
}
