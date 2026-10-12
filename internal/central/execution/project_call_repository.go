package execution

import (
	"context"
	"encoding/json"
	"errors"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

func validProjectCallLane(l projectCallLane) bool {
	switch l {
	case projectLaneService, projectLanePreparation, projectLaneDirect, projectLaneAssociated:
		return true
	}
	return false
}
func (r projectCallRecord) valid() bool {
	return r.ID.Validate() == nil && r.Project.Validate() == nil && r.Process.Validate() == nil && r.Instance.Validate() == nil && validProjectCallLane(r.Lane) && len(r.Identity) > 0 && len(r.Identity) <= 8192 && r.Digest == projectInvocationDigest(r.Identity) && (r.Phase == "running" || r.Phase == "business_closed")
}
func (r projectCallRecord) sameOwner(v projectCallRecord) bool {
	r.Phase = "running"
	v.Phase = "running"
	return r == v
}

const projectCallProjection = `jsonb_build_object('id',id,'project_id',project_id,'process_id',process_id,'instance_id',instance_id,'lane',lane,'invocation_identity',invocation_identity,'invocation_digest',invocation_digest,'phase',phase)`

func loadProjectCall(ctx context.Context, x postgres.SQLExecutor, id f.ID[projectCallID]) (*projectCallRecord, error) {
	var raw []byte
	err := x.QueryRow(ctx, `SELECT `+projectCallProjection+` FROM agenteam_execution.project_calls WHERE id=$1`, id.String()).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, portError(err)
	}
	var r projectCallRecord
	if json.Unmarshal(raw, &r) != nil || !r.valid() || r.ID != id {
		return nil, unavailable(nil)
	}
	return &r, ctx.Err()
}
func closeProjectCall(ctx context.Context, x postgres.SQLExecutor, r projectCallRecord) error {
	tag, err := x.Exec(ctx, `UPDATE agenteam_execution.project_calls SET phase='business_closed',closed_at=GREATEST(registered_at,clock_timestamp()) WHERE id=$1 AND project_id=$2 AND process_id=$3 AND instance_id=$4 AND lane=$5 AND invocation_identity=$6 AND invocation_digest=$7 AND phase='running'`, r.ID.String(), r.Project.String(), r.Process.String(), r.Instance.String(), string(r.Lane), r.Identity, r.Digest.String())
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.VersionConflict)
	}
	return ctx.Err()
}

func (s *projectCallState) register(ctx context.Context, e *projectCallEntry) (f.CommitResult, bool) {
	r := e.record
	cause, _ := f.NewRecoveryCause("execution-call-register", r.ID.String(), r.Project.String())
	validated := false
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		locks := []f.LockRequest{projectLock(r.Project)}
		if r.Lane == projectLaneService {
			locks = scopeLocks(e.actor, r.Project, e.launchKey.AgentID, f.Shared)
			if e.actor.Details().Kind == i.Service && e.actor.Details().ServiceName == i.Scheduler {
				key, _ := f.ProjectScheduleLock(r.Project.String())
				locks = append(locks, f.LockRequest{Key: key, Mode: f.Exclusive})
			}
		}
		locks, lockErr := normalizeProjectRegistrationLocks(locks)
		if lockErr != nil {
			return lockErr
		}
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := s.proof.RequireCurrentProcessInTx(ctx, tx, s.process); err != nil {
			return portError(err)
		}
		if r.Lane == projectLaneService {
			if err := s.service.deps.Authority.requireScope(ctx, tx, e.actor, r.Project, e.launchKey.AgentID, i.Read); err != nil {
				return err
			}
		} else {
			if _, err := s.preparation.state.projects.RequirePreparingProjectInTx(ctx, tx, r.Project); err != nil {
				return portError(err)
			}
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if nilPort(x) {
			return fault(f.DependencyUnbound)
		}
		tag, err := x.Exec(ctx, `INSERT INTO agenteam_execution.project_calls(id,project_id,process_id,instance_id,lane,invocation_identity,invocation_digest,phase,registered_at) VALUES($1,$2,$3,$4,$5,$6,$7,'running',clock_timestamp())`, r.ID.String(), r.Project.String(), r.Process.String(), r.Instance.String(), string(r.Lane), r.Identity, r.Digest.String())
		if err != nil {
			return portError(err)
		}
		if tag.RowsAffected() != 1 {
			return unavailable(nil)
		}
		validated = true
		return ctx.Err()
	})
	return result, validated
}

// Only an original registered invocation can lend its transaction. The legacy
// constructors have no witness and retain their original behavior.
func requireProjectCallInTx(ctx context.Context, tx f.Tx, store Store) error {
	if ctx == nil {
		return invalid()
	}
	w, ok := ctx.Value(projectCallContextKey{}).(projectCallWitness)
	if !ok {
		return nil
	}
	if w.state == nil || w.entry == nil || !sameProjectCallPort(w.state.store, store) {
		return fault(f.DependencyUnbound)
	}
	s, e := w.state, w.entry
	s.mu.Lock()
	r := e.record
	valid := s.owners[r.ID] == e && !e.bodyReturned && e.registrationKnown && !e.settling
	s.mu.Unlock()
	if !valid {
		return fault(f.InvalidState)
	}
	if err := store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(r.Project)}); err != nil {
		return portError(err)
	}
	if err := s.proof.RequireCurrentProcessInTx(ctx, tx, s.process); err != nil {
		return portError(err)
	}
	x, err := store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if nilPort(x) {
		return fault(f.DependencyUnbound)
	}
	current, err := loadProjectCall(ctx, x, r.ID)
	if err != nil {
		return err
	}
	if current == nil || !r.sameOwner(*current) || current.Phase != "running" {
		return fault(f.InvalidState)
	}
	return ctx.Err()
}

func projectCallLocks(project i.ProjectID) []f.LockRequest {
	p, _ := f.ProjectLock(project.String())
	schedule, _ := f.ProjectScheduleLock(project.String())
	return []f.LockRequest{{Key: p, Mode: f.Exclusive}, {Key: schedule, Mode: f.Exclusive}}
}
func (s *projectCallState) callTx(ctx context.Context, cause f.TransactionCause, r projectCallRecord, stop *ownedProjectStop, extra []f.LockRequest, fn func(context.Context, f.Tx, postgres.SQLExecutor) error) f.CommitResult {
	return s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		locks, err := normalizeProjectCallLocks(r.Project, extra)
		if err != nil {
			return err
		}
		if err = s.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err = s.proof.RequireCurrentProcessInTx(ctx, tx, s.process); err != nil {
			return portError(err)
		}
		if stop != nil {
			if err = s.authority.RequireProjectStopInTx(ctx, tx, stop.actor, stop.cause, stop.scope); err != nil {
				return portError(err)
			}
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if nilPort(x) {
			return fault(f.DependencyUnbound)
		}
		if err = fn(ctx, tx, x); err != nil {
			return err
		}
		return ctx.Err()
	})
}

// A read-only inspection cannot turn into a close after another confirmation
// cleared Unknown: selection and permission share this reservation.
func (s *projectCallState) settle(ctx context.Context, e *projectCallEntry, stop *ownedProjectStop, readOnly bool) (bool, error) {
	s.mu.Lock()
	if s.owners[e.record.ID] != e {
		s.mu.Unlock()
		return false, nil
	}
	if s.projectKeyBusyLocked(e.record.Project, e.record.Lane, e.identity, e) {
		s.mu.Unlock()
		return false, fault(f.ResourceBusy)
	}
	if !e.bodyReturned || e.settling {
		s.mu.Unlock()
		return false, fault(f.ResourceBusy)
	}
	if e.outcomeErr != nil {
		original := e.outcomeErr
		if !e.returned && !(e.technicalOnly && e.priorReturned) {
			s.mu.Unlock()
			return true, original
		}
		e.settling = true
		s.mu.Unlock()
		defer func() { s.mu.Lock(); e.settling = false; s.signalLocked(); s.mu.Unlock() }()
		return true, s.confirmBusinessOutcome(ctx, e, stop)
	}
	if readOnly && e.unknown == nil {
		s.mu.Unlock()
		return true, nil
	}
	e.settling = true
	r := e.record
	original := e.unknown
	registrationUnknown := e.unknownRegistration
	s.mu.Unlock()
	defer func() { s.mu.Lock(); e.settling = false; s.signalLocked(); s.mu.Unlock() }()
	cause, _ := f.NewRecoveryCause("execution-call-return", r.ID.String(), r.Project.String())
	if original != nil {
		cause = original.Cause()
	}
	validated, absent, closed := false, false, false
	result := s.callTx(ctx, cause, r, stop, nil, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		current, err := loadProjectCall(ctx, x, r.ID)
		if err != nil {
			return err
		}
		if current == nil {
			if original == nil || !registrationUnknown {
				return fault(f.InvalidState)
			}
			absent = true
		} else {
			if !r.sameOwner(*current) {
				return fault(f.ConfirmationStale)
			}
			closed = current.Phase == "business_closed"
			if original == nil && !closed {
				if err = closeProjectCall(ctx, x, r); err != nil {
					return err
				}
				closed = true
			}
		}
		validated = true
		return ctx.Err()
	})
	if original != nil {
		if result.State() != f.Committed || !validated || ctx.Err() != nil {
			return true, commitError(*original)
		}
		s.mu.Lock()
		e.unknown = nil
		e.registrationKnown = !absent
		e.absent = absent
		e.closed = closed
		s.signalLocked()
		s.mu.Unlock()
		s.retireProjectEntry(e)
		return true, nil
	}
	if result.State() == f.Unknown {
		s.mu.Lock()
		saved := result
		e.unknown = &saved
		e.unknownRegistration = false
		e.hadUnknown = true
		s.rememberParentErrorLocked(e, commitError(result))
		s.signalLocked()
		s.mu.Unlock()
		return false, commitError(result)
	}
	if err := commitError(result); err != nil {
		return false, err
	}
	if !validated || ctx.Err() != nil {
		return false, unavailable(ctx.Err())
	}
	s.mu.Lock()
	e.closed = true
	s.signalLocked()
	s.mu.Unlock()
	s.retireProjectEntry(e)
	return false, nil
}

func listProjectCalls(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID) ([]projectCallRecord, error) {
	var raw []byte
	err := x.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(`+projectCallProjection+` ORDER BY id),'[]'::jsonb) FROM agenteam_execution.project_calls WHERE project_id=$1`, project.String()).Scan(&raw)
	if err != nil {
		return nil, portError(err)
	}
	var rows []projectCallRecord
	if json.Unmarshal(raw, &rows) != nil || rows == nil {
		return nil, unavailable(nil)
	}
	for n, r := range rows {
		if !r.valid() || r.Project != project || n > 0 && rows[n-1].ID.String() >= r.ID.String() {
			return nil, unavailable(nil)
		}
	}
	return rows, ctx.Err()
}
