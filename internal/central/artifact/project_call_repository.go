package artifact

import (
	"context"
	"encoding/json"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func artifactProjectLock(project i.ProjectID, mode f.LockMode) f.LockRequest {
	key, _ := f.ProjectLock(project.String())
	return f.LockRequest{Key: key, Mode: mode}
}
func (r artifactCallRecord) valid() bool {
	if r.ID.Validate() != nil || r.Project.Validate() != nil || r.Process.Validate() != nil || !artifactCallKind(r.Kind) {
		return false
	}
	if r.Instance != "" {
		if _, err := f.ParseID[artifactInstanceID](r.Instance); err != nil {
			return false
		}
	}
	return true
}
func (r artifactCallRecord) same(other artifactCallRecord) bool {
	r.Joined, other.Joined = false, false
	return r == other
}

const artifactCallProjection = `jsonb_build_object('id',id,'project_id',project_id,'process_id',process_id,'instance_id',COALESCE(instance_id::text,''),'kind',kind,'joined',joined_at IS NOT NULL)`

func readArtifactCall(ctx context.Context, x postgres.SQLExecutor, id f.ID[artifactCallID]) (*artifactCallRecord, error) {
	var raw []byte
	err := x.QueryRow(ctx, `SELECT `+artifactCallProjection+` FROM agenteam_artifact.project_work WHERE id=$1`, id.String()).Scan(&raw)
	if noRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, unavailable(err)
	}
	var out artifactCallRecord
	if json.Unmarshal(raw, &out) != nil || !out.valid() || out.ID != id {
		return nil, unavailable(nil)
	}
	return &out, nil
}
func listArtifactCalls(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID, deleteReads bool) ([]artifactCallRecord, bool, error) {
	var raw []byte
	err := x.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(record ORDER BY id),'[]'::jsonb) FROM (SELECT id,`+artifactCallProjection+` AS record FROM agenteam_artifact.project_work WHERE project_id=$1 AND joined_at IS NULL AND (kind<>'read' OR $2::boolean) ORDER BY id LIMIT 101) bounded`, project.String(), deleteReads).Scan(&raw)
	if err != nil {
		return nil, false, unavailable(err)
	}
	var out []artifactCallRecord
	if len(raw) > 65536 || json.Unmarshal(raw, &out) != nil || out == nil || len(out) > 101 {
		return nil, false, unavailable(nil)
	}
	for n, r := range out {
		if !r.valid() || r.Project != project || r.Joined || !deleteReads && r.Kind == "read" || n > 0 && out[n-1].ID.String() >= r.ID.String() {
			return nil, false, unavailable(nil)
		}
	}
	more := len(out) > 100
	if more {
		out = out[:100]
	}
	return out, more, nil
}
func (s *projectCallOwnershipState) register(ctx context.Context, v ac.Invocation, record artifactCallRecord) (f.CommitResult, error) {
	ctx = context.WithValue(ctx, artifactCallRegistrationKey{}, s)
	cause, _ := f.NewRecoveryCause("artifact-call-register", record.ID.String(), record.Project.String())
	intent := i.Mutate
	if record.Kind == "read" {
		intent = i.Read
	}
	if record.Kind == "converge" {
		intent = i.Converge
	}
	validated := false
	result := s.service.within(ctx, invocationSubject(v), intent, cause, nil, []f.LockRequest{artifactProjectLock(record.Project, f.Shared)}, func(ctx context.Context, tx f.Tx, _ oc.LockedAccess) error {
		if err := s.requireProcess(ctx, tx); err != nil {
			return portError(err)
		}
		x, err := s.service.state().store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		// The current parent grant precedes this epoch capture. A historical
		// archive is not a permanent tombstone after Restore; later calls carry
		// that epoch and cannot be canceled by the old stop operation.
		tag, err := x.Exec(ctx, `INSERT INTO agenteam_artifact.project_work(id,project_id,process_id,instance_id,kind,resource_id,admission_version,admission_operation) SELECT $1,$2,$3,$4,$5,$1,COALESCE(s.project_version,0),s.operation_id FROM (SELECT 1) base LEFT JOIN LATERAL (SELECT project_version,operation_id FROM agenteam_artifact.project_stops WHERE project_id=$2 ORDER BY project_version DESC LIMIT 1) s ON true`, record.ID.String(), record.Project.String(), record.Process.String(), record.Instance, record.Kind)
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return unavailable(nil)
		}
		validated = ctx.Err() == nil
		return ctx.Err()
	})
	return result, artifactJoinedError(ctx, result, validated)
}
func (s *projectCallOwnershipState) checkpoint(ctx context.Context, r artifactCallRecord, allowAbsent bool) (f.CommitResult, error) {
	cause, _ := f.NewRecoveryCause("artifact-call-return", r.ID.String(), r.Project.String())
	validated := false
	result := s.service.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.service.state().store.AcquireAll(ctx, tx, []f.LockRequest{artifactProjectLock(r.Project, f.Exclusive)}); err != nil {
			return portError(err)
		}
		if err := s.requireProcess(ctx, tx); err != nil {
			return portError(err)
		}
		x, err := s.service.state().store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		current, err := readArtifactCall(ctx, x, r.ID)
		if err != nil {
			return err
		}
		if current == nil {
			if allowAbsent {
				validated = ctx.Err() == nil
				return ctx.Err()
			}
			return failure(f.InvalidState, nil)
		}
		if !current.same(r) {
			return failure(f.InvalidState, nil)
		}
		if !current.Joined {
			tag, err := x.Exec(ctx, `UPDATE agenteam_artifact.project_work SET joined_at=GREATEST(clock_timestamp(),created_at) WHERE id=$1 AND project_id=$2 AND process_id=$3 AND instance_id=$4 AND kind=$5 AND joined_at IS NULL`, r.ID.String(), r.Project.String(), r.Process.String(), r.Instance, r.Kind)
			if err != nil {
				return unavailable(err)
			}
			if tag.RowsAffected() != 1 {
				return failure(f.VersionConflict, nil)
			}
		}
		validated = ctx.Err() == nil
		return ctx.Err()
	})
	return result, artifactJoinedError(ctx, result, validated)
}

// Called inside the business transaction after its complete lock union. The
// registration transaction itself is identified privately and skips this fence.
func (s *projectCallOwnershipState) requireRunning(ctx context.Context, tx f.Tx) error {
	w, ok := ctx.Value(artifactCallContextKey{}).(artifactCallWitness)
	if !ok || w.state != s || w.call == nil {
		return failure(f.DependencyUnbound, nil)
	}
	s.mu.Lock()
	r := w.call.record
	active := s.owners[r.ID] == w.call && !w.call.returned
	registration := w.call.registration
	s.mu.Unlock()
	if !active {
		return failure(f.InvalidState, nil)
	}
	if registration.State() != f.Committed {
		return failure(f.InvalidState, nil)
	}
	if err := s.requireProcess(ctx, tx); err != nil {
		return portError(err)
	}
	x, err := s.service.state().store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	current, err := readArtifactCall(ctx, x, r.ID)
	if err != nil {
		return err
	}
	if current == nil || !current.same(r) || current.Joined {
		return failure(f.InvalidState, nil)
	}
	var revoked bool
	if err = x.QueryRow(ctx, `SELECT revoked_by IS NOT NULL FROM agenteam_artifact.project_work WHERE id=$1`, r.ID.String()).Scan(&revoked); err != nil {
		return unavailable(err)
	}
	if revoked {
		return failure(f.ProjectNotActive, nil)
	}
	return ctx.Err()
}
