package skill

import (
	"context"
	"encoding/json"
	"errors"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type maintenanceMapping struct {
	row     initializationRow
	attempt oc.AttemptID
	process oc.ProcessID
}

// Maintenance requests carry no caller Actor. D05 checks its exact current
// InstanceID and private writer/reader/process proof before executing them.
// This provider contributes only its real parent and immutable mapping. It is
// not an authorization token for new reads, writes, or reference removal.
func loadMaintenanceMapping(ctx context.Context, x postgres.SQLExecutor, request oc.AccessRequest) (maintenanceMapping, error) {
	var out maintenanceMapping
	if request.Validate() != nil || request.Details().Kind != oc.MaintenanceAccess {
		return out, invalid()
	}
	d := request.Details()
	switch d.Operation {
	case oc.InspectAccess, oc.FinishWriterAccess, oc.JoinAttemptAccess, oc.ReleaseReaderAccess, oc.ReleaseProcessAccess:
	default:
		return out, fault(f.DependencyUnbound)
	}
	var projectText string
	e := x.QueryRow(ctx, `SELECT project_id::text FROM agenteam_skill.initializations WHERE object_id=$1`, d.ObjectID.String()).Scan(&projectText)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, fault(f.DependencyUnbound)
	}
	if e != nil {
		return out, unavailable(e)
	}
	project, e := f.ParseID[id.Project](projectText)
	if e != nil {
		return out, unavailable(e)
	}
	row, e := loadInitialization(ctx, x, project)
	if e != nil {
		return out, e
	}
	if row == nil || row.request.ProjectID != project || row.object != d.ObjectID {
		return out, unavailable(nil)
	}
	out.row = *row
	if d.Operation == oc.ReleaseReaderAccess && row.phase != initializationPublished {
		return out, fault(f.Forbidden)
	}
	if d.AttemptID != (oc.AttemptID{}) {
		var attempt, process string
		e = x.QueryRow(ctx, `SELECT attempt_id::text,process_id::text FROM agenteam_skill.object_attempts WHERE attempt_id=$1 AND project_id=$2 AND creation_id=$3 AND skill_id=$4 AND revision_id=$5 AND object_id=$6 AND upload_id=$7`, d.AttemptID.String(), project.String(), row.request.CreationID.String(), row.skill.String(), row.revision.String(), row.object.String(), row.upload.String()).Scan(&attempt, &process)
		if errors.Is(e, pgx.ErrNoRows) {
			return out, fault(f.Forbidden)
		}
		if e != nil {
			return out, unavailable(e)
		}
		if out.attempt, e = f.ParseID[oc.Attempt](attempt); e != nil {
			return out, unavailable(e)
		}
		if out.process, e = f.ParseID[oc.Process](process); e != nil {
			return out, unavailable(e)
		}
		if out.attempt != d.AttemptID {
			return out, unavailable(nil)
		}
		if d.Operation == oc.FinishWriterAccess && d.InstanceID != out.process {
			return out, fault(f.Forbidden)
		}
		if d.Operation == oc.JoinAttemptAccess && d.ProcessID != out.process {
			return out, fault(f.Forbidden)
		}
	}
	if d.Operation == oc.ReleaseProcessAccess {
		var exact bool
		e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_skill.work WHERE project_id=$1 AND skill_id=$2 AND process_id=$3)`, project.String(), row.skill.String(), d.ProcessID.String()).Scan(&exact)
		if e != nil {
			return out, unavailable(e)
		}
		if !exact {
			return out, fault(f.Forbidden)
		}
		out.process = d.ProcessID
	}
	return out, nil
}

func (m maintenanceMapping) dependencies() (oc.AccessDependencies, error) {
	locks, e := m.row.locks(f.Exclusive, m.row.object)
	if e != nil {
		return oc.AccessDependencies{}, e
	}
	material, e := json.Marshal(struct {
		Project, Creation, Key, Skill, Revision, Object, Upload, Attempt, Process string
		Semantic                                                                  f.Digest
	}{m.row.request.ProjectID.String(), m.row.request.CreationID.String(), string(m.row.request.InitializationKey), m.row.skill.String(), m.row.revision.String(), m.row.object.String(), m.row.upload.String(), m.attempt.String(), m.process.String(), m.row.semantic})
	if e != nil {
		return oc.AccessDependencies{}, unavailable(e)
	}
	return oc.NewAccessDependencies(sum(material), locks)
}

func (a *Authority) discoverMaintenance(ctx context.Context, request oc.AccessRequest) (oc.AccessDependencies, error) {
	m, e := loadMaintenanceMapping(ctx, a.state().store, request)
	if e != nil {
		return oc.AccessDependencies{}, e
	}
	return m.dependencies()
}
func (a *Authority) validateMaintenance(ctx context.Context, tx f.Tx, request oc.AccessRequest, expected oc.AccessDependencies) error {
	x, e := a.state().store.InTx(tx)
	if e != nil {
		return portError(e)
	}
	m, e := loadMaintenanceMapping(ctx, x, request)
	if e != nil {
		return e
	}
	current, e := m.dependencies()
	if e != nil {
		return e
	}
	if !current.Equal(expected) {
		return fault(f.ResourceBusy)
	}
	// No active-only Project gate is used to block an already-owned technical
	// return after archival/deletion. Publishing and irreversible cleanup still
	// have their separate, current domain authority and Audit gates.
	return nil
}
