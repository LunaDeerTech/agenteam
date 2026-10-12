package work

import (
	"context"
	"encoding/json"
	"errors"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
)

func (r projectCallRecord) valid() bool {
	return r.ID.Validate() == nil && r.Project.Validate() == nil && r.Process.Validate() == nil && r.Instance.Validate() == nil &&
		(r.Lane == projectCallStructure || r.Lane == projectCallTask || r.Lane == projectCallTransition || r.Lane == projectCallBlocker || r.Lane == projectCallSprint) &&
		(r.Class == projectCallRead || r.Class == projectCallMutation) && (r.Phase == "running" || r.Phase == "business_closed")
}
func (r projectCallRecord) sameOwner(v projectCallRecord) bool {
	r.Phase = "running"
	v.Phase = "running"
	return r == v
}

const projectCallProjection = `jsonb_build_object('id',id,'project_id',project_id,'process_id',process_id,'instance_id',instance_id,'lane',lane,'class',class,'phase',phase)`

func loadProjectCall(ctx context.Context, x postgres.SQLExecutor, id f.ID[projectCallID]) (*projectCallRecord, error) {
	var raw []byte
	err := x.QueryRow(ctx, `SELECT `+projectCallProjection+` FROM agenteam_work.project_calls WHERE id=$1`, id.String()).Scan(&raw)
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
	return &out, ctx.Err()
}

func closeProjectCall(ctx context.Context, x postgres.SQLExecutor, r projectCallRecord) error {
	tag, err := x.Exec(ctx, `UPDATE agenteam_work.project_calls SET phase='business_closed',closed_at=GREATEST(clock_timestamp(),registered_at) WHERE id=$1 AND project_id=$2 AND process_id=$3 AND instance_id=$4 AND lane=$5 AND class=$6 AND phase='running'`, r.ID.String(), r.Project.String(), r.Process.String(), r.Instance.String(), string(r.Lane), string(r.Class))
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.VersionConflict)
	}
	return ctx.Err()
}

func (s *projectCallOwnershipState) register(ctx context.Context, actor i.Actor, owner *projectCallOwner) (f.CommitResult, bool) {
	r := owner.record
	cause, _ := f.NewRecoveryCause("work-call-register", r.ID.String(), r.Project.String())
	validated := false
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, []f.LockRequest{userLock(actor.Details().UserID, f.Shared), projectLock(r.Project, f.Shared)}); err != nil {
			return portError(err)
		}
		if err := s.proof.RequireCurrentProcessInTx(ctx, tx, s.process); err != nil {
			return portError(err)
		}
		// Work's original receipt-before-Mutate order must survive ownership
		// registration. Under this same P SH, Archive makes a mutation entry
		// a read-only replay attempt; its original business gate still rejects
		// planned completion and every new write.
		access, err := s.structure.state().deps.Authority.state().projects.RequireOwnerInTx(ctx, tx, actor, r.Project, i.Read)
		if err != nil {
			return portError(err)
		}
		if access.Project().ID != r.Project {
			return fault(f.Forbidden)
		}
		switch access.Project().Lifecycle {
		case pc.Active:
		case pc.Archiving, pc.Archived:
			r.Class = projectCallRead
		default:
			return fault(f.Forbidden)
		}
		s.mu.Lock()
		owner.record.Class = r.Class
		owner.classified = true
		if owner.entry.class == projectCallMutation && r.Class == projectCallRead {
			originalActor := actor
			owner.replayActor = &originalActor
		}
		s.signalLocked()
		s.mu.Unlock()
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if nilPort(x) {
			return fault(f.DependencyUnbound)
		}
		tag, err := x.Exec(ctx, `INSERT INTO agenteam_work.project_calls(id,project_id,process_id,instance_id,lane,class,phase,registered_at) VALUES($1,$2,$3,$4,$5,$6,'running',clock_timestamp())`, r.ID.String(), r.Project.String(), r.Process.String(), r.Instance.String(), string(r.Lane), string(r.Class))
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return unavailable(nil)
		}
		validated = true
		return ctx.Err()
	})
	return result, validated
}

func (s *projectCallOwnershipState) requireRunning(ctx context.Context, tx f.Tx, project i.ProjectID, lane projectCallLane) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		return fault(f.InvalidArgument)
	}
	w, ok := ctx.Value(projectCallContextKey{}).(projectCallWitness)
	if !ok || w.state != s || w.owner == nil {
		return fault(f.DependencyUnbound)
	}
	s.mu.Lock()
	o := w.owner
	r := o.record
	replayActor := o.replayActor
	valid := s.owners[r.ID] == o && !o.bodyReturned && o.registrationKnown
	s.mu.Unlock()
	if !valid || r.Project != project || r.Lane != lane {
		return fault(f.InvalidState)
	}
	if err := s.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(project, f.Shared)}); err != nil {
		return portError(err)
	}
	if err := s.proof.RequireCurrentProcessInTx(ctx, tx, s.process); err != nil {
		return portError(err)
	}
	if replayActor != nil {
		// Restore may occur between registration and a business transaction.
		// This privately classified replay can never upgrade its read custody
		// into a mutation: each original P-locked transaction proves that its
		// later ordinary Mutate gate must still reject new/planned work.
		access, err := s.structure.state().deps.Authority.state().projects.RequireOwnerInTx(ctx, tx, *replayActor, project, i.Read)
		if err != nil {
			return portError(err)
		}
		current := access.Project()
		if current.ID != project || current.Lifecycle != pc.Archiving && current.Lifecycle != pc.Archived {
			return fault(f.InvalidState)
		}
	}
	x, err := s.store.InTx(tx)
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

func (s *projectCallOwnershipState) callTx(ctx context.Context, cause f.TransactionCause, r projectCallRecord, stop *projectStopCall, fn func(context.Context, postgres.SQLExecutor) error) f.CommitResult {
	return s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, []f.LockRequest{projectLock(r.Project, f.Exclusive)}); err != nil {
			return portError(err)
		}
		if err := s.proof.RequireCurrentProcessInTx(ctx, tx, s.process); err != nil {
			return portError(err)
		}
		if stop != nil {
			if err := s.authority.RequireProjectStopInTx(ctx, tx, stop.actor, stop.cause, stop.scope); err != nil {
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
		return fn(ctx, x)
	})
}

// An ambiguous technical transaction is confirmed without writes. If it left a
// running registration, only a later explicit settlement performs its close.
// This confirms call custody, never a business command's commit outcome.
func (s *projectCallOwnershipState) settle(ctx context.Context, o *projectCallOwner, stop *projectStopCall, readOnly bool) (bool, error) {
	s.mu.Lock()
	if s.owners[o.record.ID] != o {
		s.mu.Unlock()
		return false, nil
	}
	if !o.bodyReturned || o.settling {
		s.mu.Unlock()
		return false, fault(f.ResourceBusy)
	}
	// Inspection's permission is checked at the same linearization point as
	// the retained outcome. A concurrent Drain may already have confirmed it;
	// that cannot turn this inspection into a fresh closing transaction.
	if readOnly && o.unknown == nil {
		s.mu.Unlock()
		return true, nil
	}
	o.settling = true
	r := o.record
	original := o.unknown
	registrationUnknown := o.unknownRegistration
	s.signalLocked()
	s.mu.Unlock()
	defer func() { s.mu.Lock(); o.settling = false; s.signalLocked(); s.mu.Unlock() }()
	validated, absent, closed := false, false, false
	cause, _ := f.NewRecoveryCause("work-call-return", r.ID.String(), r.Project.String())
	if original != nil {
		cause = original.Cause()
	}
	result := s.callTx(ctx, cause, r, stop, func(ctx context.Context, x postgres.SQLExecutor) error {
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
				return fault(f.InvalidState)
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
			return true, txError(*original)
		}
		s.mu.Lock()
		o.unknown = nil
		o.registrationKnown = !absent
		o.absent = absent
		o.closed = closed
		if o.returned && (closed || absent) {
			delete(s.owners, r.ID)
		}
		s.signalLocked()
		s.mu.Unlock()
		return true, nil
	}
	if result.State() == f.Unknown {
		s.mu.Lock()
		saved := result
		o.unknown = &saved
		o.unknownRegistration = false
		s.signalLocked()
		s.mu.Unlock()
		return false, txError(result)
	}
	if err := txError(result); err != nil {
		return false, err
	}
	if !validated || ctx.Err() != nil {
		return false, unavailable(ctx.Err())
	}
	s.mu.Lock()
	o.closed = true
	if o.returned {
		delete(s.owners, r.ID)
	}
	s.signalLocked()
	s.mu.Unlock()
	return false, nil
}

// Include closed identities. Neither an empty running set nor a persisted
// business_closed phase proves that another local instance actually returned.
func listProjectCalls(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID, reads bool) ([]projectCallRecord, error) {
	var raw []byte
	err := x.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(`+projectCallProjection+` ORDER BY id),'[]'::jsonb) FROM agenteam_work.project_calls WHERE project_id=$1 AND (class='mutation' OR $2::boolean)`, project.String(), reads).Scan(&raw)
	if err != nil {
		return nil, unavailable(err)
	}
	var rows []projectCallRecord
	if json.Unmarshal(raw, &rows) != nil || rows == nil {
		return nil, unavailable(nil)
	}
	for n, row := range rows {
		if !row.valid() || row.Project != project || !reads && row.Class != projectCallMutation || n > 0 && rows[n-1].ID.String() >= row.ID.String() {
			return nil, unavailable(nil)
		}
	}
	return rows, ctx.Err()
}
