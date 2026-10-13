package usage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
	"github.com/jackc/pgx/v5"
)

func (r projectCallRecord) valid() bool {
	if r.ID.Validate() != nil || r.Project.Validate() != nil || r.Process.Validate() != nil || r.Instance.Validate() != nil {
		return false
	}
	switch r.Lane {
	case projectCallList, projectCallAggregate, projectCallSummary, projectCallRebuild:
		if r.CallID != "" {
			return false
		}
	case projectCallInvocation:
		if !scalarID(r.CallID) {
			return false
		}
	default:
		return false
	}
	at, err := time.Parse(time.RFC3339Nano, r.Registered)
	if err != nil {
		return false
	}
	if r.Phase == "running" {
		return r.Closed == ""
	}
	end, e := time.Parse(time.RFC3339Nano, r.Closed)
	return r.Phase == "business_closed" && e == nil && !end.Before(at)
}
func (r projectCallRecord) sameOwner(v projectCallRecord) bool {
	r.Phase, v.Phase = "running", "running"
	r.Closed, v.Closed = "", ""
	if r.Registered == "" {
		r.Registered = v.Registered
	}
	return r == v
}

const projectCallProjection = `jsonb_build_object('id',id,'project_id',project_id,'process_id',process_id,'instance_id',instance_id,'lane',lane,'call_id',COALESCE(call_id::text,''),'phase',phase,'registered_at',registered_at,'closed_at',COALESCE(to_jsonb(closed_at),'""'::jsonb))`

func decodeProjectCall(raw []byte) (projectCallRecord, error) {
	var r projectCallRecord
	if json.Unmarshal(raw, &r) != nil || !r.valid() {
		return r, unavailable(nil)
	}
	return r, nil
}
func loadProjectCall(ctx context.Context, x postgres.SQLExecutor, id f.ID[projectCallID]) (*projectCallRecord, error) {
	var raw []byte
	err := x.QueryRow(ctx, `SELECT `+projectCallProjection+` FROM agenteam_model.usage_project_calls WHERE id=$1`, id.String()).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, portError(err)
	}
	r, err := decodeProjectCall(raw)
	if err != nil || r.ID != id {
		return nil, unavailable(err)
	}
	return &r, ctx.Err()
}
func closeProjectCall(ctx context.Context, x postgres.SQLExecutor, r projectCallRecord) error {
	tag, err := x.Exec(ctx, `UPDATE agenteam_model.usage_project_calls SET phase='business_closed',closed_at=GREATEST(clock_timestamp(),registered_at) WHERE id=$1 AND project_id=$2 AND process_id=$3 AND instance_id=$4 AND lane=$5 AND call_id IS NOT DISTINCT FROM $6::uuid AND registered_at=$7::timestamptz AND phase='running'`, r.ID.String(), r.Project.String(), r.Process.String(), r.Instance.String(), string(r.Lane), nullString(r.CallID), r.Registered)
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.VersionConflict)
	}
	return ctx.Err()
}
func (s *projectCallOwnershipState) register(ctx context.Context, o *projectCallOwner) (f.CommitResult, bool) {
	r := o.record
	cause, _ := f.NewRecoveryCause("usage-call-register", r.ID.String(), r.Project.String())
	valid := false
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		locks := []f.LockRequest{projectLock(r.Project)}
		if o.token == nil {
			u, err := f.UserLock(o.actor.Details().UserID)
			if err != nil {
				return fault(f.InvalidArgument)
			}
			locks = unionLocks(append(locks, f.LockRequest{Key: u, Mode: f.Shared}))
		}
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := s.proof.RequireCurrentProcessInTx(ctx, tx, s.process); err != nil {
			return portError(err)
		}
		if o.token == nil {
			a := s.service.state().authority.state()
			if err := a.auth.Sessions.RequireCurrentSession(ctx, tx, o.actor); err != nil {
				return portError(err)
			}
			grant, err := a.auth.Projects.AuthorizeProject(ctx, tx, o.actor, r.Project, i.Read)
			if err != nil {
				return portError(err)
			}
			scope, _ := i.InProject(r.Project)
			if !grant.Matches(o.actor, scope, i.Read) {
				return fault(f.Forbidden)
			}
		} else {
			admission, ok := s.authority.(uc.ProjectInvocationAdmissionAuthority)
			if !ok || nilPort(admission) {
				return fault(f.DependencyUnbound)
			}
			if err := admission.RequireProjectInvocationAdmissionInTx(ctx, tx, r.Project); err != nil {
				return portError(err)
			}
		}
		x, err := s.store.InTx(tx)
		if err != nil || nilPort(x) {
			return unavailable(err)
		}
		var raw []byte
		err = x.QueryRow(ctx, `INSERT INTO agenteam_model.usage_project_calls(id,project_id,process_id,instance_id,lane,call_id,phase,registered_at) VALUES($1,$2,$3,$4,$5,$6,'running',clock_timestamp()) RETURNING `+projectCallProjection, r.ID.String(), r.Project.String(), r.Process.String(), r.Instance.String(), string(r.Lane), nullString(r.CallID)).Scan(&raw)
		if err != nil {
			return portError(err)
		}
		actual, err := decodeProjectCall(raw)
		if err != nil || !r.sameOwner(actual) {
			return unavailable(err)
		}
		s.mu.Lock()
		o.record = actual
		s.signalLocked()
		s.mu.Unlock()
		valid = true
		return ctx.Err()
	})
	return result, valid
}
func (s *projectCallOwnershipState) requireRunning(ctx context.Context, tx f.Tx, p i.ProjectID, lane projectCallLane) error {
	if s == nil {
		return nil
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	w, ok := ctx.Value(projectCallContextKey{}).(projectCallWitness)
	if !ok || w.state != s || w.owner == nil {
		return fault(f.DependencyUnbound)
	}
	s.mu.Lock()
	o := w.owner
	r := o.record
	valid := s.owners[r.ID] == o && o.registrationKnown && !o.bodyReturned && !o.settling
	s.mu.Unlock()
	if !valid || r.Project != p || r.Lane != lane {
		return fault(f.Forbidden)
	}
	if err := s.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(p)}); err != nil {
		return portError(err)
	}
	if err := s.proof.RequireCurrentProcessInTx(ctx, tx, s.process); err != nil {
		return portError(err)
	}
	x, err := s.store.InTx(tx)
	if err != nil || nilPort(x) {
		return unavailable(err)
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
func (s *projectCallOwnershipState) requireReader(ctx context.Context, tx f.Tx, p i.ProjectID) error {
	if s == nil {
		return nil
	}
	w, ok := ctx.Value(projectCallContextKey{}).(projectCallWitness)
	if !ok || w.state != s || w.owner == nil {
		return fault(f.DependencyUnbound)
	}
	lane := w.owner.record.Lane
	switch lane {
	case projectCallList, projectCallAggregate, projectCallSummary, projectCallRebuild:
		return s.requireRunning(ctx, tx, p, lane)
	}
	return fault(f.Forbidden)
}
func (s *projectCallOwnershipState) callTx(ctx context.Context, cause f.TransactionCause, r projectCallRecord, stop *projectStopCall, fn func(context.Context, postgres.SQLExecutor) error) f.CommitResult {
	return s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, []f.LockRequest{usageProjectLock(r.Project, f.Exclusive)}); err != nil {
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
		if err != nil || nilPort(x) {
			return unavailable(err)
		}
		return fn(ctx, x)
	})
}
func (s *projectCallOwnershipState) settle(ctx context.Context, o *projectCallOwner, stop *projectStopCall, readOnly bool) (bool, error) {
	s.mu.Lock()
	if s.owners[o.record.ID] != o {
		s.mu.Unlock()
		return false, nil
	}
	if !o.bodyReturned || o.settling || o.active != 0 {
		s.mu.Unlock()
		return false, fault(f.ResourceBusy)
	}
	if o.absent && o.unknown == nil {
		s.mu.Unlock()
		return false, nil
	}
	if readOnly && o.unknown == nil && o.businessUnknown == nil {
		s.mu.Unlock()
		return true, nil
	}
	o.settling = true
	r := o.record
	original := o.unknown
	registrationUnknown := o.unknownRegistration
	business := o.businessUnknown
	s.signalLocked()
	s.mu.Unlock()
	defer func() { s.mu.Lock(); o.settling = false; s.signalLocked(); s.mu.Unlock() }()
	if business != nil {
		return true, s.confirmRebuild(ctx, o, stop, *business)
	}
	valid, absent, closed := false, false, false
	cause, _ := f.NewRecoveryCause("usage-call-return", r.ID.String(), r.Project.String())
	if original != nil {
		cause = original.Cause()
	}
	var observed *projectCallRecord
	result := s.callTx(ctx, cause, r, stop, func(ctx context.Context, x postgres.SQLExecutor) error {
		current, err := loadProjectCall(ctx, x, r.ID)
		if err != nil {
			return err
		}
		observed = current
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
				if err = closeProjectCall(ctx, x, *current); err != nil {
					return err
				}
				closed = true
			}
		}
		valid = true
		return ctx.Err()
	})
	if original != nil {
		if result.State() != f.Committed || !valid || ctx.Err() != nil {
			return true, txError(*original)
		}
		s.mu.Lock()
		o.unknown = nil
		o.registrationKnown = !absent
		o.absent, o.closed = absent, closed
		if observed != nil {
			o.record = *observed
		}
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
	if !valid || ctx.Err() != nil {
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
func listProjectCalls(ctx context.Context, x postgres.SQLExecutor, p i.ProjectID) ([]projectCallRecord, error) {
	var raw []byte
	err := x.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(`+projectCallProjection+` ORDER BY id),'[]'::jsonb) FROM agenteam_model.usage_project_calls WHERE project_id=$1`, p.String()).Scan(&raw)
	if err != nil {
		return nil, portError(err)
	}
	var out []projectCallRecord
	if json.Unmarshal(raw, &out) != nil || out == nil {
		return nil, unavailable(nil)
	}
	for n, r := range out {
		if !r.valid() || r.Project != p || n > 0 && out[n-1].ID.String() >= r.ID.String() {
			return nil, unavailable(nil)
		}
	}
	return out, ctx.Err()
}

// Rebuild is the only standalone Usage mutation. Keep complete physical rows,
// rather than recomputing a summary during an uncertain commit's observation.
type projectRebuildImage struct {
	execution               i.ExecutionID
	before, after           []byte
	beforeKnown, afterKnown bool
}

func readRebuildImage(ctx context.Context, x postgres.SQLExecutor, p i.ProjectID, e i.ExecutionID) ([]byte, error) {
	var raw []byte
	err := x.QueryRow(ctx, `SELECT to_jsonb(t) FROM agenteam_model.execution_usage_summaries t WHERE project_id=$1 AND execution_id=$2`, p.String(), e.String()).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, portError(err)
	}
	if len(raw) > 32<<10 {
		return nil, fault(f.ResourceBusy)
	}
	return append([]byte(nil), raw...), ctx.Err()
}
func (s *projectCallOwnershipState) rebuildImage(ctx context.Context, x postgres.SQLExecutor, p i.ProjectID, e i.ExecutionID, before bool) error {
	if s == nil {
		return nil
	}
	w, ok := ctx.Value(projectCallContextKey{}).(projectCallWitness)
	if !ok || w.state != s || w.owner == nil || w.owner.record.Lane != projectCallRebuild {
		return fault(f.DependencyUnbound)
	}
	raw, err := readRebuildImage(ctx, x, p, e)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if before {
		w.owner.rebuild = &projectRebuildImage{execution: e, before: raw, beforeKnown: true}
	} else if w.owner.rebuild != nil {
		w.owner.rebuild.after, w.owner.rebuild.afterKnown = raw, true
	}
	return nil
}
func (s *projectCallOwnershipState) captureResult(ctx context.Context, result f.CommitResult) {
	if s == nil || result.State() != f.Unknown {
		return
	}
	w, ok := ctx.Value(projectCallContextKey{}).(projectCallWitness)
	if !ok || w.state != s || w.owner == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if w.owner.record.Lane == projectCallRebuild && w.owner.rebuild != nil && w.owner.businessUnknown == nil {
		saved := result
		w.owner.businessUnknown = &saved
		s.signalLocked()
	}
}
func (s *projectCallOwnershipState) confirmRebuild(ctx context.Context, o *projectCallOwner, stop *projectStopCall, original f.CommitResult) error {
	image := o.rebuild
	if image == nil || !image.beforeKnown {
		return txError(original)
	}
	valid := false
	result := s.callTx(ctx, original.Cause(), o.record, stop, func(ctx context.Context, x postgres.SQLExecutor) error {
		row, err := loadProjectCall(ctx, x, o.record.ID)
		if err != nil {
			return err
		}
		if row == nil || !o.record.sameOwner(*row) || row.Phase != "running" {
			return fault(f.ConfirmationStale)
		}
		current, err := readRebuildImage(ctx, x, o.record.Project, image.execution)
		if err != nil {
			return err
		}
		if !bytes.Equal(current, image.before) && (!image.afterKnown || !bytes.Equal(current, image.after)) {
			return fault(f.ConfirmationStale)
		}
		valid = true
		return ctx.Err()
	})
	if result.State() != f.Committed || !valid || ctx.Err() != nil {
		return txError(original)
	}
	s.mu.Lock()
	o.businessUnknown = nil
	s.signalLocked()
	s.mu.Unlock()
	return nil
}
