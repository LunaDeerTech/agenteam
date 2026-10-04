//go:build integration

package objects_test

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

// The fixture owns these real SQL facts. Discovery records parent identities,
// not permission or existing/prospective state; Validate only rechecks mapping.
func (a *authority) Discover(ctx context.Context, request oc.AccessRequest) (oc.AccessDependencies, error) {
	return a.dependencies(ctx, a.store, request)
}
func (a *authority) ValidateInTx(ctx context.Context, tx foundation.Tx, request oc.AccessRequest, expected oc.AccessDependencies) error {
	e, err := a.exec(tx)
	if err != nil {
		return err
	}
	actual, err := a.dependencies(ctx, e, request)
	if err != nil {
		return err
	}
	if !expected.Equal(actual) {
		return fault(foundation.ResourceBusy)
	}
	return nil
}
func (a *authority) dependencies(ctx context.Context, e postgres.SQLExecutor, request oc.AccessRequest) (oc.AccessDependencies, error) {
	if request.Validate() != nil {
		return oc.AccessDependencies{}, fault(foundation.InvalidArgument)
	}
	d := request.Details()
	var locks []foundation.LockRequest
	var mapping []string
	add := func(key foundation.LockKey, err error) {
		if err == nil {
			locks = append(locks, foundation.LockRequest{Key: key, Mode: foundation.Shared})
		}
	}
	actor := d.Actor.Details()
	if actor.UserID != "" {
		add(foundation.UserLock(actor.UserID))
		mapping = append(mapping, "actor-user:"+actor.UserID)
	}
	if actor.AgentID != "" {
		add(foundation.AgentLock(actor.AgentID))
		mapping = append(mapping, "actor-agent:"+actor.AgentID)
	}
	if actor.ExecutionID != "" {
		add(foundation.AggregateLock(foundation.ExecutionAggregate, actor.ExecutionID))
		var agent, project string
		err := e.QueryRow(ctx, `SELECT agent_id::text,project_id::text FROM object_fixture.executions WHERE id=$1`, actor.ExecutionID).Scan(&agent, &project)
		if err != nil {
			return oc.AccessDependencies{}, err
		}
		mapping = append(mapping, "actor-execution:"+actor.ExecutionID+":"+agent+":"+project)
	}
	owners := map[string]bool{}
	objects := append([]oc.ObjectID(nil), d.Objects...)
	if d.Owner.Validate() == nil {
		owners[d.Owner.Details().ID] = true
	}
	if d.Cleanup.Validate() == nil {
		owners[d.Cleanup.Details().Owner.Details().ID] = true
	}
	if d.Source.Validate() == nil {
		owners[d.Source.Details().Owner.Details().ID] = true
		objects = append(objects, d.Source.Details().Meta.ID)
	}
	if d.ObjectID.Validate() == nil {
		objects = append(objects, d.ObjectID)
	}
	if d.Attempt.Validate() == nil {
		objects = append(objects, d.Attempt.Details().ObjectID)
	}
	if d.Receipt.Validate() == nil {
		objects = append(objects, d.Receipt.Details().ObjectID)
	}
	if d.ProjectCleanup.Validate() == nil {
		add(foundation.ProjectLock(d.ProjectCleanup.Details().ProjectID.String()))
		mapping = append(mapping, "project:"+d.ProjectCleanup.Details().ProjectID.String())
	}
	seen := map[oc.ObjectID]bool{}
	for _, id := range objects {
		if seen[id] {
			continue
		}
		seen[id] = true
		add(foundation.AggregateLock(foundation.ObjectAggregate, id.String()))
		var owner string
		err := e.QueryRow(ctx, `SELECT owner_id::text FROM agenteam_object.uploads WHERE object_id=$1`, id.String()).Scan(&owner)
		if err == nil {
			owners[owner] = true
			mapping = append(mapping, "object:"+id.String()+":"+owner)
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return oc.AccessDependencies{}, err
		}
		rows, err := e.Query(ctx, `SELECT id::text,owner_id::text,kind,coalesce(lease_id::text,'') FROM object_fixture.uses WHERE object_id=$1 ORDER BY id`, id.String())
		if err != nil {
			return oc.AccessDependencies{}, err
		}
		for rows.Next() {
			var use, own, kind, lease string
			if err = rows.Scan(&use, &own, &kind, &lease); err != nil {
				rows.Close()
				return oc.AccessDependencies{}, err
			}
			owners[own] = true
			mapping = append(mapping, "use:"+use+":"+own+":"+kind+":"+lease)
			add(foundation.RecordLock(foundation.ReferenceRecordLock, "fixture-use:"+use))
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return oc.AccessDependencies{}, err
		}
	}
	for owner := range owners {
		var kind, project, user, parent string
		err := e.QueryRow(ctx, `SELECT kind,coalesce(project_id::text,''),coalesce(user_id::text,''),coalesce(parent_id::text,'') FROM object_fixture.owners WHERE id=$1`, owner).Scan(&kind, &project, &user, &parent)
		if errors.Is(err, pgx.ErrNoRows) {
			return oc.AccessDependencies{}, fault(foundation.Forbidden)
		}
		if err != nil {
			return oc.AccessDependencies{}, err
		}
		mapping = append(mapping, "owner:"+owner+":"+kind+":"+project+":"+user+":"+parent)
		if user != "" {
			add(foundation.UserLock(user))
		}
		if project != "" {
			add(foundation.ProjectLock(project))
		}
		switch oc.OwnerKind(kind) {
		case oc.ExecutionPayload:
			if parent == "" {
				return oc.AccessDependencies{}, fault(foundation.DependencyUnbound)
			}
			add(foundation.AggregateLock(foundation.ExecutionAggregate, parent))
		case oc.SkillRevision:
			if parent == "" {
				return oc.AccessDependencies{}, fault(foundation.DependencyUnbound)
			}
			add(foundation.AggregateLock(foundation.SkillAggregate, parent))
		case oc.MeetingFile:
			if parent == "" {
				return oc.AccessDependencies{}, fault(foundation.DependencyUnbound)
			}
			add(foundation.AggregateLock(foundation.MeetingAggregate, parent))
		case oc.Knowledge:
			add(foundation.KnowledgeTreeLock(project))
		case oc.MCPContent:
			if parent == "" {
				return oc.AccessDependencies{}, fault(foundation.DependencyUnbound)
			}
			add(foundation.AggregateLock(foundation.MCPConnectionAggregate, parent))
		}
		add(foundation.RecordLock(foundation.ReferenceRecordLock, "fixture-owner:"+owner))
	}
	if d.Kind == oc.MaintenanceAccess {
		mapping = append(mapping, "maintenance-instance:"+d.InstanceID.String())
		add(foundation.SystemConfigLock("object-maintenance"))
	}
	if d.Kind == oc.LeaseAccess {
		mapping = append(mapping, "lease-use:"+d.LeaseOwner.Details().ID)
		add(foundation.RecordLock(foundation.ReferenceRecordLock, "fixture-use:"+d.LeaseOwner.Details().ID))
	}
	sort.Strings(mapping)
	raw, err := json.Marshal(mapping)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	return oc.NewAccessDependencies(digest(raw), locks)
}

func ownerPlan(t *testing.T, service *object.Service, actor identity.Actor, owner oc.ObjectOwner, operation oc.AccessOperation, d oc.AccessRequestDetails) oc.AccessLockPlan {
	t.Helper()
	d.Actor = actor
	d.Owner = owner
	d.Operation = operation
	d.Intent = identity.Mutate
	if operation == oc.ReleaseAccess || operation == oc.CancelAccess {
		d.Intent = identity.Converge
	}
	if operation == oc.LookupAccess {
		d.Intent = identity.Read
	}
	request, err := oc.NewOwnerAccess(d)
	if err != nil {
		t.Fatal(err)
	}
	return discoverPlan(t, service, request)
}
func leasePlan(t *testing.T, service *object.Service, actor identity.Actor, objectID oc.ObjectID, owner oc.LeaseOwner, operation oc.AccessOperation) oc.AccessLockPlan {
	t.Helper()
	request, err := oc.NewLeaseAccess(oc.AccessRequestDetails{Operation: operation, Actor: actor, ObjectID: objectID, LeaseOwner: owner})
	if err != nil {
		t.Fatal(err)
	}
	return discoverPlan(t, service, request)
}
func discoverPlan(t *testing.T, service *object.Service, request oc.AccessRequest) oc.AccessLockPlan {
	t.Helper()
	p, err := service.DiscoverAccess(contextFor(t), request)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// The plan is supplied before this helper opens a Tx. Business callbacks may
// compose their SQL and object mutation under this single complete lock batch.
func plannedTx(store object.Store, service *object.Service, ctx context.Context, cause foundation.TransactionCause, plan oc.AccessLockPlan, fn func(context.Context, foundation.Tx, oc.AccessLockPlan, oc.LockedAccess) error) foundation.CommitResult {
	return store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		locked, err := service.AcquireAccessPlansInTx(ctx, tx, []oc.AccessLockPlan{plan}, nil)
		if err != nil {
			return err
		}
		return fn(ctx, tx, plan, locked)
	})
}
