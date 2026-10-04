package object

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sort"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

var errAccessMappingChanged = errors.New("lock_plan_changed")

func accessChanged() error {
	return foundation.NewFault(foundation.ResourceBusy, foundation.NotCommitted).WithCause(errAccessMappingChanged)
}

func (s *Service) DiscoverAccess(ctx context.Context, request oc.AccessRequest) (oc.AccessLockPlan, error) {
	if request.Validate() != nil {
		return oc.AccessLockPlan{}, invalid()
	}
	r := s.state()
	if nilPort(r.auth.Planner) {
		return oc.AccessLockPlan{}, failure(foundation.DependencyUnbound, nil)
	}
	if request.Details().Kind != oc.MaintenanceAccess {
		if err := s.metadataAdmission(ctx); err != nil {
			return oc.AccessLockPlan{}, err
		}
	}
	facts, err := s.accessFacts(ctx, r.store, request)
	if err != nil {
		return oc.AccessLockPlan{}, err
	}
	dependencyRequest, err := accessDependencyRequest(request, facts.objects)
	if err != nil {
		return oc.AccessLockPlan{}, err
	}
	dependencies, err := r.auth.Planner.Discover(ctx, dependencyRequest)
	if err != nil {
		return oc.AccessLockPlan{}, portError(err)
	}
	if dependencies.Validate() != nil {
		return oc.AccessLockPlan{}, unavailable(nil)
	}
	locks := append(facts.locks, dependencies.Locks()...)
	plan, err := oc.NewAccessLockPlan(r.accessIssuer, oc.AccessPlanDetails{Request: request, DependencyRequest: dependencyRequest, Dependencies: dependencies, DomainBinding: facts.binding, Objects: facts.objects, Locks: locks})
	if err != nil {
		return oc.AccessLockPlan{}, invalid()
	}
	return plan, nil
}
func (s *Service) AcquireAccessPlansInTx(ctx context.Context, tx foundation.Tx, plans []oc.AccessLockPlan, extra []foundation.LockRequest) (oc.LockedAccess, error) {
	r := s.state()
	if _, err := executor(s, tx); err != nil {
		return oc.LockedAccess{}, err
	}
	if len(plans) == 0 || len(plans) > 100 {
		return oc.LockedAccess{}, invalid()
	}
	token, err := oc.NewLockedAccess(r.accessIssuer, tx, plans, extra)
	if err != nil {
		return oc.LockedAccess{}, invalid()
	}
	// Keep only live handles. This does not rely on a caller supplying the Tx
	// context, and a cancelled caller context cannot permit a second acquisition.
	r.accessMu.Lock()
	for old := range r.accessTransactions {
		if _, err := r.store.InTx(old); err != nil {
			delete(r.accessTransactions, old)
		}
	}
	if r.accessTransactions[tx] {
		r.accessMu.Unlock()
		return oc.LockedAccess{}, accessChanged()
	}
	r.accessTransactions[tx] = true
	r.accessMu.Unlock()
	if err = r.store.AcquireAll(ctx, tx, token.Locks()); err != nil {
		return oc.LockedAccess{}, unavailable(err)
	}
	return token, nil
}
func (s *Service) ValidateAccessPlanInTx(ctx context.Context, tx foundation.Tx, request oc.AccessRequest, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
	r := s.state()
	e, err := executor(s, tx)
	if err != nil {
		return err
	}
	r.accessMu.Lock()
	acquired := r.accessTransactions[tx]
	r.accessMu.Unlock()
	if !acquired || !locked.Matches(r.accessIssuer, tx, plan, request) {
		return invalid()
	}
	if nilPort(r.auth.Planner) {
		return failure(foundation.DependencyUnbound, nil)
	}
	facts, err := s.accessFacts(ctx, e, request)
	if err != nil {
		return err
	}
	d := plan.Details()
	if facts.binding != d.DomainBinding {
		return accessChanged()
	}
	dependencyRequest, err := accessDependencyRequest(request, facts.objects)
	if err != nil {
		return err
	}
	if !dependencyRequest.Equal(d.DependencyRequest) {
		return accessChanged()
	}
	if err = r.auth.Planner.ValidateInTx(ctx, tx, dependencyRequest, d.Dependencies); err != nil {
		return portError(err)
	}
	return nil
}
func accessDependencyRequest(request oc.AccessRequest, objects []oc.ObjectID) (oc.AccessRequest, error) {
	d := request.Details()
	if d.Kind == oc.ProjectCleanupAccess {
		d.Objects = objects
		return oc.NewProjectCleanupAccess(d)
	}
	return request, nil
}

type accessFacts struct {
	binding foundation.Digest
	objects []oc.ObjectID
	locks   []foundation.LockRequest
}

func (s *Service) accessFacts(ctx context.Context, e postgres.SQLExecutor, request oc.AccessRequest) (accessFacts, error) {
	d := request.Details()
	var out accessFacts
	if d.Kind == oc.TransferAccess {
		return s.transferAccessFacts(ctx, e, d.Transfer)
	}
	if d.Kind == oc.MaintenanceAccess && d.InstanceID != s.state().process {
		return out, invalid()
	}
	owners := map[string]oc.ObjectOwner{}
	commands := map[string]foundation.CommandIdentity{}
	addOwner := func(owner oc.ObjectOwner) {
		if owner.Validate() == nil {
			v := owner.Details()
			owners[string(v.Kind)+":"+owner.Partition()+":"+v.ID] = owner
		}
	}
	addCommand := func(owner oc.ObjectOwner, key foundation.IdempotencyKey) error {
		c, err := commandIdentity(owner, key)
		if err != nil {
			return err
		}
		commands[c.Canonical()] = c
		return nil
	}
	addOwner(d.Owner)
	addOwner(d.Cleanup.Details().Owner)
	addOwner(d.Source.Details().Owner)
	if d.ObjectID.Validate() == nil {
		out.objects = []oc.ObjectID{d.ObjectID}
	}
	switch d.Kind {
	case oc.OwnerAccess:
		switch d.Operation {
		case oc.ReserveAccess:
			p, err := s.prepared(d.Prepared, d.Actor, d.Owner)
			if err != nil {
				return out, err
			}
			command, err := commandIdentity(d.Owner, d.Command.IdempotencyKey)
			if err != nil {
				return out, err
			}
			u, found, err := loadCommand(ctx, e, command)
			if err != nil {
				return out, err
			}
			object := p.objectID
			if found {
				object = u.object
			}
			out.objects = []oc.ObjectID{object}
			commands[command.Canonical()] = command
		case oc.LookupAccess, oc.CancelAccess:
			command, err := commandIdentity(d.Owner, d.Key)
			if err != nil {
				return out, err
			}
			commands[command.Canonical()] = command
			u, found, err := loadCommand(ctx, e, command)
			if err != nil {
				return out, err
			}
			if found {
				out.objects = []oc.ObjectID{u.object}
			}
		case oc.SendAccess, oc.PublishAccess:
			out.objects = []oc.ObjectID{d.Attempt.Details().ObjectID}
		case oc.ConsumeAccess:
			out.objects = []oc.ObjectID{d.Receipt.Details().ObjectID}
		}
	case oc.ProjectCleanupAccess:
		if d.Operation == oc.FinishProjectAccess {
			out.objects = append([]oc.ObjectID(nil), d.Objects...)
		} else {
			rows, err := e.Query(ctx, `SELECT id::text FROM agenteam_object.objects WHERE project_id=$1 ORDER BY project_cleanup_pass,id LIMIT 100`, d.ProjectCleanup.Details().ProjectID.String())
			if err != nil {
				return out, unavailable(err)
			}
			for rows.Next() {
				var raw string
				if err = rows.Scan(&raw); err != nil {
					rows.Close()
					return out, unavailable(err)
				}
				id, err := foundation.ParseID[oc.StoredObject](raw)
				if err != nil {
					rows.Close()
					return out, unavailable(err)
				}
				out.objects = append(out.objects, id)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return out, unavailable(err)
			}
		}
	case oc.SourceAccess:
		out.objects = []oc.ObjectID{d.Source.Details().Meta.ID}
	}
	// Actual object rows discover their command/partition; parent business
	// aggregates are exclusively supplied by the owning-domain planner.
	for _, id := range out.objects {
		u, found, err := scanUpload(e.QueryRow(ctx, `SELECT `+uploadColumns+` FROM agenteam_object.uploads WHERE object_id=$1`, id.String()))
		if err != nil {
			return out, err
		}
		if found {
			addOwner(u.owner)
			if err = addCommand(u.owner, u.command); err != nil {
				return out, err
			}
		}
	}
	mode := foundation.Exclusive
	if d.Operation == oc.PrepareAccess || d.Operation == oc.LookupAccess || d.Operation == oc.StatAccess || d.Operation == oc.ValidateSourceAccess || d.Operation == oc.InspectAccess {
		mode = foundation.Shared
	}
	add := func(key foundation.LockKey, mode foundation.LockMode) {
		out.locks = append(out.locks, foundation.LockRequest{Key: key, Mode: mode})
	}
	actor := d.Actor.Details()
	if actor.UserID != "" {
		key, err := foundation.UserLock(actor.UserID)
		if err != nil {
			return out, invalid()
		}
		add(key, foundation.Shared)
	}
	if actor.ProjectID != "" {
		key, err := foundation.ProjectLock(actor.ProjectID)
		if err != nil {
			return out, invalid()
		}
		add(key, foundation.Shared)
	}
	if actor.AgentID != "" {
		key, err := foundation.AgentLock(actor.AgentID)
		if err != nil {
			return out, invalid()
		}
		add(key, foundation.Shared)
	}
	if actor.ExecutionID != "" {
		key, err := foundation.AggregateLock(foundation.ExecutionAggregate, actor.ExecutionID)
		if err != nil {
			return out, invalid()
		}
		add(key, foundation.Shared)
	}
	ownerNames := make([]string, 0, len(owners))
	for name := range owners {
		ownerNames = append(ownerNames, name)
	}
	sort.Strings(ownerNames)
	for _, name := range ownerNames {
		owner := owners[name]
		v := owner.Details()
		if v.Kind == oc.Avatar {
			key, _ := foundation.UserLock(v.ID)
			add(key, foundation.Shared)
		} else {
			key, _ := foundation.ProjectLock(v.ProjectID)
			add(key, foundation.Shared)
		}
		// This is an object reference mutex, never a guessed business parent gate.
		key, err := foundation.RecordLock(foundation.ReferenceRecordLock, "object-owner:"+string(v.Kind)+":"+v.ID)
		if err != nil {
			return out, invalid()
		}
		add(key, mode)
	}
	commandNames := make([]string, 0, len(commands))
	for name, c := range commands {
		commandNames = append(commandNames, name)
		key, _ := foundation.CommandLock(c)
		add(key, mode)
	}
	sort.Strings(commandNames)
	for _, id := range out.objects {
		key, _ := foundation.AggregateLock(foundation.ObjectAggregate, id.String())
		add(key, mode)
	}
	if d.Operation == oc.ReserveAccess {
		key, _ := foundation.SystemConfigLock("object-attempt-admission")
		add(key, foundation.Exclusive)
	}
	if d.Kind == oc.ProjectCleanupAccess {
		key, _ := foundation.ProjectLock(d.ProjectCleanup.Details().ProjectID.String())
		add(key, foundation.Exclusive)
	}
	if d.Kind == oc.LeaseAccess {
		key, err := foundation.RecordLock(foundation.ReferenceRecordLock, "object-lease:"+string(d.LeaseOwner.Details().Kind)+":"+d.LeaseOwner.Details().ID)
		if err != nil {
			return out, invalid()
		}
		add(key, foundation.Exclusive)
	}
	ids := make([]string, len(out.objects))
	for i, id := range out.objects {
		ids[i] = id.String()
	}
	raw, err := json.Marshal([]any{ids, ownerNames, commandNames})
	if err != nil {
		return out, invalid()
	}
	sum := sha256.Sum256(raw)
	out.binding = newDigest(sum[:])
	return out, nil
}

// withinAccess discovers before opening the transaction. Every internal phase
// uses the same explicit plan/token path as external business composition.
func (s *Service) withinAccess(ctx context.Context, cause foundation.TransactionCause, request oc.AccessRequest, fn func(context.Context, foundation.Tx, oc.AccessLockPlan, oc.LockedAccess) error) foundation.CommitResult {
	plan, err := s.DiscoverAccess(ctx, request)
	if err != nil {
		return rejectedAccess(err)
	}
	return s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		locked, err := s.AcquireAccessPlansInTx(ctx, tx, []oc.AccessLockPlan{plan}, nil)
		if err != nil {
			return err
		}
		if err = s.ValidateAccessPlanInTx(ctx, tx, request, plan, locked); err != nil {
			return err
		}
		return fn(ctx, tx, plan, locked)
	})
}
func rejectedAccess(err error) foundation.CommitResult {
	var f *foundation.Fault
	if errors.As(err, &f) {
		return foundation.NotCommittedResult(f)
	}
	return foundation.NotCommittedResult(foundation.NewFault(foundation.DependencyUnavailable, foundation.NotCommitted).WithCause(err))
}
func ownerRequest(actor identity.Actor, owner oc.ObjectOwner, op oc.AccessOperation, extra oc.AccessRequestDetails) oc.AccessRequest {
	extra.Actor = actor
	extra.Owner = owner
	extra.Operation = op
	extra.Intent = identity.Mutate
	if op == oc.LookupAccess {
		extra.Intent = identity.Read
	}
	if op == oc.CancelAccess || op == oc.ReleaseAccess {
		extra.Intent = identity.Converge
	}
	req, _ := oc.NewOwnerAccess(extra)
	return req
}
func (s *Service) maintenanceRequest(op oc.AccessOperation, id oc.ObjectID, extra oc.AccessRequestDetails) oc.AccessRequest {
	extra.Operation = op
	extra.ObjectID = id
	extra.InstanceID = s.state().process
	req, _ := oc.NewMaintenanceAccess(extra)
	return req
}
