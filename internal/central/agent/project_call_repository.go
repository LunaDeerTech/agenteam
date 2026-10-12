package agent

import (
	"context"
	"encoding/json"
	"errors"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

func (r projectCallRecord) valid() bool {
	return r.ID.Validate() == nil && r.Project.Validate() == nil && r.Process.Validate() == nil && r.Instance.Validate() == nil &&
		(r.Lane == projectCallService || r.Lane == projectCallDirectory) && (r.Class == "mutation" || r.Class == "read") &&
		(r.Lane != projectCallDirectory || r.Class == "read") && (r.Phase == "running" || r.Phase == "joined")
}
func (r projectCallRecord) sameOwner(other projectCallRecord) bool {
	r.Phase, other.Phase = "running", "running"
	return r == other
}

const projectCallProjection = `jsonb_build_object('id',id,'project_id',project_id,'process_id',process_id,'instance_id',instance_id,'lane',lane,'class',class,'phase',phase)`

func loadProjectCall(ctx context.Context, x postgres.SQLExecutor, id f.ID[projectCallIdentity]) (*projectCallRecord, error) {
	var raw []byte
	err := x.QueryRow(ctx, `SELECT `+projectCallProjection+` FROM agenteam_agent.project_calls WHERE id=$1`, id.String()).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, unavailable(err)
	}
	var out projectCallRecord
	if json.Unmarshal(raw, &out) != nil || !out.valid() || out.ID != id {
		return nil, unavailable(nil)
	}
	return &out, nil
}

// Both the SQL subquery and decoded result are bounded. One QueryRow closes
// its rows before this returns, including errors and an empty running set.
func listProjectCalls(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID, includeReads bool) ([]projectCallRecord, bool, error) {
	var raw []byte
	err := x.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(record ORDER BY id),'[]'::jsonb) FROM (SELECT id,`+projectCallProjection+` AS record FROM agenteam_agent.project_calls WHERE project_id=$1 AND phase='running' AND (class='mutation' OR $2::boolean) ORDER BY id LIMIT 101) AS bounded`, project.String(), includeReads).Scan(&raw)
	if err != nil {
		return nil, false, unavailable(err)
	}
	var out []projectCallRecord
	if len(raw) > 65536 || json.Unmarshal(raw, &out) != nil || out == nil || len(out) > 101 {
		return nil, false, unavailable(nil)
	}
	for n, record := range out {
		if !record.valid() || record.Project != project || record.Phase != "running" || !includeReads && record.Class != "mutation" || n > 0 && out[n-1].ID.String() >= record.ID.String() {
			return nil, false, unavailable(nil)
		}
	}
	more := len(out) > 100
	if more {
		out = out[:100]
	}
	return out, more, nil
}

func joinProjectCall(ctx context.Context, x postgres.SQLExecutor, r projectCallRecord) error {
	tag, err := x.Exec(ctx, `UPDATE agenteam_agent.project_calls SET phase='joined',joined_at=GREATEST(clock_timestamp(),registered_at) WHERE id=$1 AND project_id=$2 AND process_id=$3 AND instance_id=$4 AND lane=$5 AND class=$6 AND phase='running'`, r.ID.String(), r.Project.String(), r.Process.String(), r.Instance.String(), string(r.Lane), r.Class)
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.VersionConflict)
	}
	return nil
}

func (s *projectCallOwnershipState) register(ctx context.Context, actor i.Actor, r projectCallRecord) f.CommitResult {
	cause, _ := f.NewRecoveryCause("agent-call-register", r.ID.String(), r.Project.String())
	return s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, []f.LockRequest{userLock(actor, f.Shared), projectLock(r.Project, f.Shared)}); err != nil {
			return portError(err)
		}
		if err := s.proof.RequireCurrentProcessInTx(ctx, tx, s.process); err != nil {
			return portError(err)
		}
		grant, err := s.service.state.deps.Authority.state.projects.RequireOwnerInTx(ctx, tx, actor, r.Project, i.Read)
		if err != nil {
			return portError(err)
		}
		if !grant.Matches(actor, r.Project) {
			return fault(f.Forbidden)
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		tag, err := x.Exec(ctx, `INSERT INTO agenteam_agent.project_calls(id,project_id,process_id,instance_id,lane,class,phase,registered_at) VALUES($1,$2,$3,$4,$5,$6,'running',clock_timestamp())`, r.ID.String(), r.Project.String(), r.Process.String(), r.Instance.String(), string(r.Lane), r.Class)
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return unavailable(nil)
		}
		return nil
	})
}

// Original formal business transactions already own their complete lock union.
// An observer's Project EX terminal cut must be followed by this fence before
// an old queued transaction can read or write any business data.
func (s *projectCallOwnershipState) requireRunning(ctx context.Context, tx f.Tx) error {
	if s == nil {
		return nil
	}
	w, ok := ctx.Value(projectCallContextKey{}).(projectCallWitness)
	if !ok || w.state != s || w.owner == nil {
		return fault(f.DependencyUnbound)
	}
	s.mu.Lock()
	live := s.owners[w.owner.record.ID] == w.owner && !w.owner.returned && w.owner.registration.State() == f.Committed
	r := w.owner.record
	s.mu.Unlock()
	if !live {
		return fault(f.InvalidState)
	}
	if err := s.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(r.Project, f.Shared)}); err != nil {
		return portError(err)
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	current, err := loadProjectCall(ctx, x, r.ID)
	if err != nil {
		return err
	}
	if current == nil || !r.sameOwner(*current) || current.Phase != "running" {
		return fault(f.InvalidState)
	}
	return nil
}

// A retained original returned proof permits metadata-only settlement. The
// Project EX cut is also mandatory for a registration-Unknown absent row.
func (s *projectCallOwnershipState) checkpoint(ctx context.Context, r projectCallRecord, allowAbsent bool) f.CommitResult {
	cause, _ := f.NewRecoveryCause("agent-call-return", r.ID.String(), r.Project.String())
	return s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, []f.LockRequest{projectLock(r.Project, f.Exclusive)}); err != nil {
			return portError(err)
		}
		if err := s.proof.RequireCurrentProcessInTx(ctx, tx, s.process); err != nil {
			return portError(err)
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		current, err := loadProjectCall(ctx, x, r.ID)
		if err != nil {
			return err
		}
		if current == nil {
			if allowAbsent {
				return nil
			}
			return fault(f.InvalidState)
		}
		if !r.sameOwner(*current) {
			return fault(f.InvalidState)
		}
		if current.Phase == "joined" {
			return nil
		}
		return joinProjectCall(ctx, x, r)
	})
}
