package object

import (
	"context"
	"errors"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

// MaintenanceOwnerResolver exposes only Object's original upload ownership for
// fixed root routing. The result is not authorization. Object's access plan and
// the selected domain must still validate the original request and held locks.
type MaintenanceOwnerResolver struct{ store Store }

func NewMaintenanceOwnerResolver(store Store) (*MaintenanceOwnerResolver, error) {
	if nilPort(store) {
		return nil, failure(foundation.DependencyUnbound, nil)
	}
	return &MaintenanceOwnerResolver{store: store}, nil
}

func (r *MaintenanceOwnerResolver) Discover(ctx context.Context, request oc.AccessRequest) (oc.ObjectOwner, error) {
	if r == nil || nilPort(r.store) {
		return oc.ObjectOwner{}, failure(foundation.DependencyUnbound, nil)
	}
	return maintenanceOwner(ctx, r.store, request)
}

// ResolveInTx uses the very same Store and the caller's live transaction. It
// neither opens a transaction nor acquires additional locks.
func (r *MaintenanceOwnerResolver) ResolveInTx(ctx context.Context, tx foundation.Tx, request oc.AccessRequest) (oc.ObjectOwner, error) {
	if r == nil || nilPort(r.store) {
		return oc.ObjectOwner{}, failure(foundation.DependencyUnbound, nil)
	}
	if err := validMaintenanceOwnerRequest(ctx, request); err != nil {
		return oc.ObjectOwner{}, err
	}
	x, err := r.store.InTx(tx)
	if err != nil {
		return oc.ObjectOwner{}, portError(err)
	}
	return maintenanceOwner(ctx, x, request)
}

func validMaintenanceOwnerRequest(ctx context.Context, request oc.AccessRequest) error {
	if ctx == nil || request.Validate() != nil || request.Details().Kind != oc.MaintenanceAccess || request.Details().ObjectID.Validate() != nil {
		return invalid()
	}
	if err := ctx.Err(); err != nil {
		return portError(err)
	}
	return nil
}

func maintenanceOwner(ctx context.Context, x postgres.SQLExecutor, request oc.AccessRequest) (oc.ObjectOwner, error) {
	if err := validMaintenanceOwnerRequest(ctx, request); err != nil {
		return oc.ObjectOwner{}, err
	}
	var kind oc.OwnerKind
	var owner, project string
	err := x.QueryRow(ctx, `SELECT owner_kind,owner_id::text,coalesce(project_id::text,'') FROM agenteam_object.uploads WHERE object_id=$1`, request.Details().ObjectID.String()).Scan(&kind, &owner, &project)
	if errors.Is(err, pgx.ErrNoRows) {
		return oc.ObjectOwner{}, failure(foundation.DependencyUnbound, nil)
	}
	if err != nil {
		return oc.ObjectOwner{}, portError(err)
	}
	value, err := oc.NewObjectOwner(kind, owner, project)
	if err != nil {
		return oc.ObjectOwner{}, unavailable(err)
	}
	return value, nil
}
