package object

import (
	"context"
	"reflect"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// This owner belongs to the original Service, including copies of its public
// handle. It retains an uncertain original result, not a replacement attempt.
type projectCleanupCall struct {
	actor            identity.Actor
	cause            oc.ProjectCleanupDetails
	active, retained bool
	original         foundation.CommitResult
}
type projectCleanupContext struct {
	owner        *serviceState
	call         *projectCleanupCall
	confirmation bool
}
type projectCleanupContextKey struct{}

// RequireProjectCleanupStore checks assembly identity only. Every actual
// cleanup transaction still consumes the current Project authority.
func (s *Service) RequireProjectCleanupStore(store any) error {
	if s == nil || s.data == nil || nilPort(store) || nilPort(s.state().store) || reflect.ValueOf(store) != reflect.ValueOf(s.state().store) {
		return failure(foundation.DependencyUnbound, nil)
	}
	return nil
}

func (r *serviceState) borrowProjectCleanup(actor identity.Actor, cause oc.ProjectCleanupDetails) (*projectCleanupCall, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if call := r.projectCleanups[cause.ProjectID]; call != nil {
		if call.active {
			return nil, failure(foundation.ResourceBusy, nil)
		}
		if !call.actor.Equal(actor) || call.cause != cause {
			return nil, failure(foundation.InvalidState, nil)
		}
		call.active = true
		return call, nil
	}
	if len(r.projectCleanups) >= 256 {
		return nil, failure(foundation.ResourceBusy, nil)
	}
	if r.projectCleanups == nil {
		r.projectCleanups = map[identity.ProjectID]*projectCleanupCall{}
	}
	call := &projectCleanupCall{actor: actor, cause: cause, active: true}
	r.projectCleanups[cause.ProjectID] = call
	return call, nil
}
func (r *serviceState) returnProjectCleanup(call *projectCleanupCall) {
	r.mu.Lock()
	defer r.mu.Unlock()
	call.active = false
	if !call.retained {
		delete(r.projectCleanups, call.cause.ProjectID)
	}
	r.notifyWorkLocked()
}
func (s *Service) currentProjectCleanup(ctx context.Context) *projectCleanupContext {
	c, _ := ctx.Value(projectCleanupContextKey{}).(*projectCleanupContext)
	if c == nil || c.owner != s.state() {
		return nil
	}
	r := s.state()
	r.mu.Lock()
	defer r.mu.Unlock()
	if !c.call.active || r.projectCleanups[c.call.cause.ProjectID] != c.call {
		return nil
	}
	return c
}
func (s *Service) captureProjectCleanup(ctx context.Context, result foundation.CommitResult) foundation.CommitResult {
	c := s.currentProjectCleanup(ctx)
	if c != nil && !c.call.retained && result.State() == foundation.Unknown {
		c.call.original, c.call.retained = result, true
	}
	return result
}

type projectCleanupFailure struct{ result foundation.CommitResult }

func (*projectCleanupFailure) Error() string { return "COMMIT_UNKNOWN" }
func projectCleanupUnknown(result foundation.CommitResult) error {
	f := foundation.NewFault(foundation.CommitUnknown, foundation.Unknown)
	if result.AttemptID().Validate() == nil {
		f.CauseID = result.AttemptID().String()
	}
	f.RetryHint = "lookup"
	return f.WithCause(&projectCleanupFailure{result: result})
}

// ConfirmProjectCleanup is deliberately read-only even when another legitimate
// caller already confirmed the retained result. Artifact can therefore finish
// its own original owner without accidentally starting a new Object batch.
func (s *Service) ConfirmProjectCleanup(ctx context.Context, actor identity.Actor, cause oc.ProjectCleanupCause) (oc.ProjectCleanupResult, error) {
	return s.runProjectCleanup(ctx, actor, cause, true)
}
func (s *Service) CleanupProject(ctx context.Context, actor identity.Actor, cause oc.ProjectCleanupCause) (oc.ProjectCleanupResult, error) {
	return s.runProjectCleanup(ctx, actor, cause, false)
}
func (s *Service) runProjectCleanup(ctx context.Context, actor identity.Actor, cause oc.ProjectCleanupCause, confirmation bool) (out oc.ProjectCleanupResult, err error) {
	if ctx == nil || actor.Validate() != nil || cause.Validate() != nil {
		return out, invalid()
	}
	if s == nil || s.data == nil || nilPort(s.state().auth.Cleanup) {
		return out, failure(foundation.DependencyUnbound, nil)
	}
	caller := ctx
	r := s.state()
	call, err := r.borrowProjectCleanup(actor, cause.Details())
	if err != nil {
		return out, err
	}
	defer r.returnProjectCleanup(call)
	defer func() {
		if call.retained {
			out, err = oc.ProjectCleanupResult{}, projectCleanupUnknown(call.original)
		}
		if err == nil && caller.Err() != nil {
			out, err = oc.ProjectCleanupResult{}, unavailable(caller.Err())
		}
		if err == nil && s.projectCleanupWorkPending(call.cause.ProjectID) {
			out.State = oc.CleanupPending
		}
	}()
	confirmation = confirmation || call.retained
	ctx = context.WithValue(ctx, projectCleanupContextKey{}, &projectCleanupContext{owner: r, call: call, confirmation: confirmation})
	ctx = context.WithValue(ctx, runtimeRecoveryBudgetKey{}, true)
	op, finish, err := s.beginBoundedCleanup(ctx)
	if err != nil {
		return out, err
	}
	defer finish()
	ctx = op.ctx
	if confirmation {
		out, err = s.confirmProjectCleanup(ctx, actor, cause)
		if err == nil {
			call.retained = false
		}
		return out, err
	}
	if err = s.resumeProjectCleanupWork(ctx, cause.Details().ProjectID); err != nil {
		return out, err
	}
	return s.cleanupProjectBatch(ctx, actor, cause)
}
func (s *Service) confirmProjectCleanup(ctx context.Context, actor identity.Actor, cause oc.ProjectCleanupCause) (oc.ProjectCleanupResult, error) {
	d := cause.Details()
	out := oc.ProjectCleanupResult{State: oc.CleanupPending, ProjectID: d.ProjectID, OperationID: d.OperationID}
	key, _ := foundation.ProjectLock(d.ProjectID.String())
	txCause, _ := foundation.NewRecoveryCause("object-project-cleanup-confirm", d.OperationID.String(), d.ProjectID.String())
	validated := false
	result := s.state().store.WithinTx(ctx, txCause, func(ctx context.Context, tx foundation.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: key, Mode: foundation.Exclusive}}); err != nil {
			return unavailable(err)
		}
		if err := s.state().auth.Cleanup.CheckProjectCleanupInTx(ctx, tx, actor, cause); err != nil {
			return portError(err)
		}
		e, err := executor(s, tx)
		if err != nil {
			return err
		}
		var count int64
		if err = e.QueryRow(ctx, `SELECT count(*) FROM agenteam_object.objects WHERE project_id=$1`, d.ProjectID.String()).Scan(&count); err != nil {
			return unavailable(err)
		}
		if err = ctx.Err(); err != nil {
			return unavailable(err)
		}
		out.Remaining = foundation.Progress(count)
		if count == 0 {
			out.State = oc.CleanupCompleted
		}
		validated = true
		return nil
	})
	s.captureProjectCleanup(ctx, result)
	if result.State() == foundation.Unknown {
		return oc.ProjectCleanupResult{}, projectCleanupUnknown(result)
	}
	if err := commitError(result); err != nil {
		return oc.ProjectCleanupResult{}, err
	}
	if !validated || ctx.Err() != nil {
		return oc.ProjectCleanupResult{}, unavailable(ctx.Err())
	}
	return out, nil
}

func (s *Service) projectCleanupWorkPending(project identity.ProjectID) bool {
	r := s.state()
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, h := range r.projectWork {
		if h.work.project == project {
			return true
		}
	}
	for w := range r.projectWriters {
		if w.handle.work.project == project {
			return true
		}
	}
	for _, h := range r.projectJoins {
		if h.work.project == project {
			return true
		}
	}
	return false
}
func (s *Service) resumeProjectCleanupWork(ctx context.Context, project identity.ProjectID) error {
	r := s.state()
	r.mu.Lock()
	var pending []*projectWorkHandle
	for _, h := range r.pendingProjectWorkLocked() {
		if h.work.project == project {
			pending = append(pending, h)
		}
	}
	r.mu.Unlock()
	for _, h := range pending {
		if err := s.joinProjectWork(ctx, h); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) checkProjectCleanupAccess(ctx context.Context, tx foundation.Tx, request oc.AccessRequest) error {
	c := s.currentProjectCleanup(ctx)
	if c == nil {
		return nil
	}
	cause, err := oc.NewProjectCleanupCause(c.call.cause)
	if err != nil {
		return invalid()
	}
	if err = s.state().auth.Cleanup.CheckProjectCleanupInTx(ctx, tx, c.call.actor, cause); err != nil {
		return portError(err)
	}
	// The private context authorizes only this Project, never an unrelated
	// maintenance object reached through another public method.
	if object := request.Details().ObjectID; object.Validate() == nil {
		e, err := executor(s, tx)
		if err != nil {
			return err
		}
		var project string
		if err = e.QueryRow(ctx, `SELECT COALESCE(project_id::text,'') FROM agenteam_object.objects WHERE id=$1`, object.String()).Scan(&project); err != nil {
			return unavailable(err)
		}
		if project != c.call.cause.ProjectID.String() {
			return failure(foundation.Forbidden, nil)
		}
	}
	return nil
}
