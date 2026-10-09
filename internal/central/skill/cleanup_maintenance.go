package skill

import (
	"context"
	"encoding/json"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func cleanupMaintenanceOperation(op oc.AccessOperation) bool {
	return op == oc.ClaimCleanupAccess || op == oc.CheckpointCleanupAccess || op == oc.FinalizeCleanupAccess
}

type cleanupMaintenanceMapping struct {
	row     initializationRow
	cleanup cleanupRow
	process oc.ProcessID // original candidate; never the current cleanup worker
}

func loadCleanupMaintenance(ctx context.Context, x postgres.SQLExecutor, request oc.AccessRequest) (cleanupMaintenanceMapping, error) {
	var out cleanupMaintenanceMapping
	d := request.Details()
	if request.Validate() != nil || d.Kind != oc.MaintenanceAccess || !cleanupMaintenanceOperation(d.Operation) {
		return out, invalid()
	}
	project, err := cleanupProjectForObject(ctx, x, d.ObjectID)
	if err != nil {
		return out, err
	}
	r, err := loadInitialization(ctx, x, project)
	if err != nil {
		return out, err
	}
	c, err := loadCleanup(ctx, x, project)
	if err != nil {
		return out, err
	}
	if r == nil || c == nil || r.phase != initializationPublished || r.object != d.ObjectID || !c.matches(*r) || c.phase != cleanupGated && c.phase != cleanupPending {
		return out, fault(f.Forbidden)
	}
	out.row, out.cleanup = *r, *c
	if d.Operation != oc.FinalizeCleanupAccess {
		var process string
		err = x.QueryRow(ctx, `SELECT process_id::text FROM agenteam_skill.object_attempts WHERE attempt_id=$1 AND project_id=$2 AND creation_id=$3 AND skill_id=$4 AND revision_id=$5 AND object_id=$6 AND upload_id=$7`, d.AttemptID.String(), project.String(), r.request.CreationID.String(), r.skill.String(), r.revision.String(), r.object.String(), r.upload.String()).Scan(&process)
		if err != nil {
			return out, unavailable(err)
		}
		out.process, err = f.ParseID[oc.Process](process)
		if err != nil {
			return out, unavailable(err)
		}
	}
	return out, nil
}

func (m cleanupMaintenanceMapping) dependencies(request oc.AccessRequest) (oc.AccessDependencies, error) {
	base, err := cleanupDependencies(m.row, request)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	// Do not conflate D05 Checkpoint CleanupID (a private operation row) with
	// this domain's ProjectDeleted operation. Both stay separately bound.
	raw, err := json.Marshal(struct {
		Parent                              f.Digest
		Cleanup, Operation, OriginalProcess string
		Version                             f.Version
	}{base.Mapping(), m.cleanup.id.String(), m.cleanup.cause.OperationID.String(), m.process.String(), m.cleanup.cause.ProjectVersion})
	if err != nil {
		return oc.AccessDependencies{}, unavailable(err)
	}
	return oc.NewAccessDependencies(sum(raw), base.Locks())
}

func (a *Authority) discoverCleanupMaintenance(ctx context.Context, request oc.AccessRequest) (oc.AccessDependencies, error) {
	m, err := loadCleanupMaintenance(ctx, a.state().store, request)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	return m.dependencies(request)
}

func (a *Authority) validateCleanupMaintenance(ctx context.Context, tx f.Tx, request oc.AccessRequest, expected oc.AccessDependencies) error {
	x, err := a.state().store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	m, err := loadCleanupMaintenance(ctx, x, request)
	if err != nil {
		return err
	}
	current, err := m.dependencies(request)
	if err != nil {
		return err
	}
	if !current.Equal(expected) {
		return fault(f.ResourceBusy)
	}
	return a.checkCleanupRowInTx(ctx, tx, m.row, m.cleanup)
}
