package mcp

import (
	"context"
	"encoding/json"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// This read-only mapping port is implemented by Object's original upload
// resolver. It cannot authorize maintenance or issue an access plan.
type ContentMaintenanceOwnerResolver interface {
	Discover(context.Context, oc.AccessRequest) (oc.ObjectOwner, error)
	ResolveInTx(context.Context, f.Tx, oc.AccessRequest) (oc.ObjectOwner, error)
}

func (a *ContentAuthority) contentMaintenance(ctx context.Context, tx f.Tx, request oc.AccessRequest, expected ...oc.AccessDependencies) (oc.AccessDependencies, error) {
	if a == nil || a.service() == nil || nilPort(a.maintenance) {
		return oc.AccessDependencies{}, fail(f.DependencyUnbound)
	}
	if ctx != nil && ctx.Value(contentUseKey{}) == nil {
		return a.contentSourceMaintenance(ctx, tx, request, expected...)
	}
	w, err := a.contentMaintenanceUse(ctx, request)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	var owner oc.ObjectOwner
	var x postgres.SQLExecutor = a.service().store
	if tx.Valid() {
		if err = a.service().store.RequireHeldLocks(ctx, tx, w.locks); err != nil {
			return oc.AccessDependencies{}, safeError(err)
		}
		x, err = a.service().store.InTx(tx)
		if err == nil {
			owner, err = a.maintenance.ResolveInTx(ctx, tx, request)
		}
	} else {
		owner, err = a.maintenance.Discover(ctx, request)
	}
	if err != nil {
		return oc.AccessDependencies{}, safeError(err)
	}
	project, materialization, err := contentOwner(owner)
	if err != nil || project != w.call.ProjectID || materialization != w.materialization {
		return oc.AccessDependencies{}, fail(f.Forbidden)
	}
	row, err := loadContentFact(ctx, x, materialization)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	d := request.Details()
	if !a.factsMatch(w, row) || d.Operation == oc.FinishWriterAccess && row.state != "pending" || d.Operation == oc.ReleaseReaderAccess && row.state != "ready" {
		return oc.AccessDependencies{}, fail(f.Forbidden)
	}
	if d.Operation == oc.ReleaseReaderAccess {
		var exact bool
		err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mcp.materialized_contents WHERE materialization_id=$1 AND object_id=$2)`, materialization, d.ObjectID.String()).Scan(&exact)
		if err != nil {
			return oc.AccessDependencies{}, safeError(err)
		}
		if !exact {
			return oc.AccessDependencies{}, fail(f.Forbidden)
		}
	}
	// No current admission is issued here. Revocation cannot erase an already
	// owned writer/reader's obligation to close its original Object resources.
	// Creating, publishing, attaching and reading still use the current gates.
	raw, err := json.Marshal([]string{"mcp.content.tail.v1", project.String(), materialization, w.request,
		string(d.Operation), d.ObjectID.String(), d.InstanceID.String(), d.AttemptID.String(), d.LeaseID.String()})
	if err != nil {
		return oc.AccessDependencies{}, fail(f.InvalidState)
	}
	return oc.NewAccessDependencies(f.Digest(digest(raw)), w.locks)
}

func (a *ContentAuthority) contentMaintenanceUse(ctx context.Context, request oc.AccessRequest) (*contentUse, error) {
	if ctx == nil || request.Validate() != nil || request.Details().Kind != oc.MaintenanceAccess {
		return nil, fail(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return nil, safeError(err)
	}
	d := request.Details()
	if d.Operation != oc.FinishWriterAccess && d.Operation != oc.ReleaseReaderAccess {
		return nil, fail(f.DependencyUnbound)
	}
	w, ok := ctx.Value(contentUseKey{}).(*contentUse)
	if !ok || w == nil || w.owner != a || !w.active.Load() || w.materialization == "" || w.call.Actor.Validate() != nil {
		return nil, fail(f.Forbidden)
	}
	return w, nil
}
