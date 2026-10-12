package artifact

import (
	"context"
	"errors"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

const maxProjectCleanupCalls = 256

type projectCleanupCall struct {
	actor            identity.Actor
	cause            oc.ProjectCleanupDetails
	active, retained bool
	original         foundation.CommitResult
	childUnknown     error
}
type projectCleanupContextKey struct{}
type projectCleanupContext struct {
	state *serviceState
	call  *projectCleanupCall
}

func (r *serviceState) originalCleanupCall(ctx context.Context, subject ac.AccessSubject, intent identity.AccessIntent) bool {
	owner, _ := ctx.Value(projectCleanupContextKey{}).(projectCleanupContext)
	if intent != identity.Lifecycle || owner.state != r || owner.call == nil || subject.ProjectID != owner.call.cause.ProjectID || !subject.Actor.Equal(owner.call.actor) {
		return false
	}
	r.cleanupMu.Lock()
	defer r.cleanupMu.Unlock()
	return r.cleanupCalls[subject.ProjectID] == owner.call && owner.call.active
}

// RequireProjectCleanupBinding validates the fixed cleanup composition only.
// It neither issues a Project grant nor narrows ordinary Artifact construction.
func (s *Service) RequireProjectCleanupBinding() error {
	if s == nil || s.data == nil || s.state() == nil {
		return failure(foundation.DependencyUnbound, nil)
	}
	r := s.state()
	objects, ok := r.objects.(*object.Service)
	cleaner, same := r.cleaner.(*object.Service)
	if !ok || !same || objects == nil || objects != cleaner {
		return failure(foundation.DependencyUnbound, nil)
	}
	return objects.RequireProjectCleanupStore(r.store)
}

func (r *serviceState) borrowProjectCleanup(actor identity.Actor, cause oc.ProjectCleanupDetails) (*projectCleanupCall, error) {
	r.cleanupMu.Lock()
	defer r.cleanupMu.Unlock()
	if call := r.cleanupCalls[cause.ProjectID]; call != nil {
		if call.active {
			return nil, failure(foundation.ResourceBusy, nil)
		}
		if !call.actor.Equal(actor) || call.cause != cause {
			return nil, failure(foundation.InvalidState, nil)
		}
		call.active = true
		return call, nil
	}
	if len(r.cleanupCalls) >= maxProjectCleanupCalls {
		return nil, failure(foundation.ResourceBusy, nil)
	}
	if r.cleanupCalls == nil {
		r.cleanupCalls = make(map[identity.ProjectID]*projectCleanupCall)
	}
	call := &projectCleanupCall{actor: actor, cause: cause, active: true}
	r.cleanupCalls[cause.ProjectID] = call
	return call, nil
}
func (r *serviceState) releaseProjectCleanup(call *projectCleanupCall) {
	r.cleanupMu.Lock()
	call.active = false
	if !call.retained {
		delete(r.cleanupCalls, call.cause.ProjectID)
	}
	r.cleanupMu.Unlock()
	if r.ownership != nil {
		r.ownership.mu.Lock()
		r.ownership.signalLocked()
		r.ownership.mu.Unlock()
	}
}

type projectCleanupCommitFailure struct{ result foundation.CommitResult }

func (*projectCleanupCommitFailure) Error() string { return string(foundation.CommitUnknown) }
func projectCleanupUnknown(result foundation.CommitResult) error {
	fault := foundation.NewFault(foundation.CommitUnknown, foundation.Unknown)
	if result.AttemptID().Validate() == nil {
		fault.CauseID = result.AttemptID().String()
	}
	fault.RetryHint = "lookup"
	return fault.WithCause(&projectCleanupCommitFailure{result: result})
}
func (call *projectCleanupCall) failure() error {
	if call.childUnknown != nil {
		return call.childUnknown
	}
	return projectCleanupUnknown(call.original)
}
func (s *Service) captureProjectCleanup(ctx context.Context, result foundation.CommitResult) {
	owner, _ := ctx.Value(projectCleanupContextKey{}).(projectCleanupContext)
	if owner.state == s.state() && owner.call != nil && !owner.call.retained && result.State() == foundation.Unknown {
		owner.call.original, owner.call.retained = result, true
	}
}
func (s *Service) projectCleanupSourcesPending(ctx context.Context, e postgres.SQLExecutor, project identity.ProjectID) (bool, error) {
	r := s.state()
	r.mu.Lock()
	pending := false
	for key := range r.leases {
		p, known := r.leaseProjects[key]
		source, sourceKnown := r.leaseSources[key]
		if !known || !sourceKnown || p.Validate() != nil || p == project || source == project.String() {
			pending = true
			break
		}
	}
	r.mu.Unlock()
	var durable bool
	if err := e.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_artifact.commands WHERE source_lease_id IS NOT NULL AND (project_id=$1::text::uuid OR source->>'project'=$1::text OR source IS NULL))`, project.String()).Scan(&durable); err != nil {
		return false, unavailable(err)
	}
	if err := ctx.Err(); err != nil {
		return false, unavailable(err)
	}
	return pending || durable, nil
}

func (s *Service) CleanupProject(ctx context.Context, actor identity.Actor, cause oc.ProjectCleanupCause) (oc.ProjectCleanupResult, error) {
	var zero oc.ProjectCleanupResult
	if ctx == nil || actor.Validate() != nil || cause.Validate() != nil {
		return zero, invalid()
	}
	if s == nil || s.data == nil || s.state() == nil || nilPort(s.state().store) {
		return zero, failure(foundation.DependencyUnbound, nil)
	}
	r := s.state()
	if r.ownership != nil {
		done, err := r.ownership.beginControl()
		if err != nil {
			return zero, err
		}
		defer done()
	}
	call, err := r.borrowProjectCleanup(actor, cause.Details())
	if err != nil {
		return zero, err
	}
	defer r.releaseProjectCleanup(call)
	if err := ctx.Err(); err != nil {
		if call.retained {
			return zero, errors.Join(call.failure(), unavailable(err))
		}
		return zero, unavailable(err)
	}
	run := context.WithValue(ctx, projectCleanupContextKey{}, projectCleanupContext{state: r, call: call})
	if call.retained {
		out, err := s.confirmProjectCleanup(run, actor, cause, call)
		if err != nil || ctx.Err() != nil {
			return zero, errors.Join(call.failure(), err, ctx.Err())
		}
		call.retained = false
		return out, nil
	}
	out, err := s.cleanupProject(run, actor, cause)
	if call.retained {
		return zero, errors.Join(call.failure(), err)
	}
	return out, err
}

// Confirmation observes current facts and never repeats a metadata or physical
// deletion. Both domains must return before the original retained call releases.
func (s *Service) confirmProjectCleanup(ctx context.Context, actor identity.Actor, cause oc.ProjectCleanupCause, call *projectCleanupCall) (oc.ProjectCleanupResult, error) {
	d := cause.Details()
	out := oc.ProjectCleanupResult{State: oc.CleanupPending, ProjectID: d.ProjectID, OperationID: d.OperationID}
	result := s.withinCleanup(ctx, actor, cause, nil, nil, func(ctx context.Context, _ foundation.Tx, e postgres.SQLExecutor, _ oc.LockedAccess) error {
		pending, err := s.projectCleanupSourcesPending(ctx, e, d.ProjectID)
		if err != nil {
			return err
		}
		var operation, state string
		var version int64
		err = e.QueryRow(ctx, `SELECT operation_id::text,project_version,state FROM agenteam_artifact.cleanup WHERE project_id=$1`, d.ProjectID.String()).Scan(&operation, &version, &state)
		if noRows(err) {
			return nil
		}
		if err != nil {
			return unavailable(err)
		}
		if operation != d.OperationID.String() || version != int64(d.Version) {
			return failure(foundation.Forbidden, nil)
		}
		var remains bool
		if err := e.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_artifact.artifacts WHERE project_id=$1) OR EXISTS(SELECT 1 FROM agenteam_artifact.commands WHERE project_id=$1) OR EXISTS(SELECT 1 FROM agenteam_artifact.upload_intents WHERE project_id=$1)`, d.ProjectID.String()).Scan(&remains); err != nil {
			return unavailable(err)
		}
		if !pending && !remains && state == "completed" {
			out.State = oc.CleanupCompleted
		}
		return nil
	})
	if err := commitError(result); err != nil {
		return oc.ProjectCleanupResult{}, err
	}
	if call.childUnknown != nil {
		if err := s.RequireProjectCleanupBinding(); err != nil {
			return oc.ProjectCleanupResult{}, err
		}
		registration, _ := identity.RegisterService(identity.ObjectMaintenance)
		maintenance, err := registration.Actor(d.OperationID.String(), identityScope(d.ProjectID))
		if err != nil {
			return oc.ProjectCleanupResult{}, invalid()
		}
		child, err := s.state().cleaner.(*object.Service).ConfirmProjectCleanup(ctx, maintenance, cause)
		if err != nil {
			return oc.ProjectCleanupResult{}, err
		}
		if child.State != oc.CleanupCompleted {
			out.State = oc.CleanupPending
		}
	}
	return out, nil
}
