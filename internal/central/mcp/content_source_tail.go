package mcp

import (
	"context"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// The original Object reader closes within its original registered Model
// invocation. This tail cannot authorize Put, publish, attach or a new read.
func (a *ContentAuthority) contentSourceMaintenance(ctx context.Context, tx f.Tx, access oc.AccessRequest, expected ...oc.AccessDependencies) (oc.AccessDependencies, error) {
	if ctx == nil || access.Validate() != nil || access.Details().Kind != oc.MaintenanceAccess || access.Details().Operation != oc.ReleaseReaderAccess || len(expected) > 1 {
		return oc.AccessDependencies{}, fail(f.Forbidden)
	}
	tail, ok := a.reads.(ec.MCPContentReadTailAuthority)
	if !ok || nilPort(tail) {
		return oc.AccessDependencies{}, fail(f.DependencyUnbound)
	}
	actor, err := tail.MCPContentReadActor(ctx)
	if err != nil {
		return oc.AccessDependencies{}, safeError(err)
	}
	var owner oc.ObjectOwner
	var x postgres.SQLExecutor = a.service().store
	if tx.Valid() {
		if len(expected) != 1 || expected[0].Validate() != nil {
			return oc.AccessDependencies{}, fail(f.InvalidArgument)
		}
		if err = a.service().store.RequireHeldLocks(ctx, tx, expected[0].Locks()); err != nil {
			return oc.AccessDependencies{}, safeError(err)
		}
		x, err = a.service().store.InTx(tx)
		if err == nil {
			owner, err = a.maintenance.ResolveInTx(ctx, tx, access)
		}
	} else {
		if len(expected) != 0 {
			return oc.AccessDependencies{}, fail(f.InvalidArgument)
		}
		owner, err = a.maintenance.Discover(ctx, access)
	}
	if err != nil {
		return oc.AccessDependencies{}, safeError(err)
	}
	request, err := loadContentReadRequest(ctx, x, actor, owner)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	var exact bool
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mcp.materialized_contents WHERE content_id=$1 AND materialization_id=$2 AND object_id=$3)`, request.Reference.Details().FileID, request.MaterializationID, access.Details().ObjectID.String()).Scan(&exact)
	if err != nil {
		return oc.AccessDependencies{}, safeError(err)
	}
	if !exact {
		return oc.AccessDependencies{}, fail(f.Forbidden)
	}
	if tx.Valid() {
		if err = tail.ValidateMCPContentReadTailInTx(ctx, tx, request, access, expected[0]); err != nil {
			return oc.AccessDependencies{}, safeError(err)
		}
		return expected[0], ctx.Err()
	}
	plan, err := tail.DiscoverMCPContentReadTail(ctx, request, access)
	if err != nil {
		return oc.AccessDependencies{}, safeError(err)
	}
	if plan.Validate() != nil {
		return oc.AccessDependencies{}, fail(f.DependencyUnbound)
	}
	return plan, ctx.Err()
}
