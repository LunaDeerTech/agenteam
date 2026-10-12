package secret

import (
	"context"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

const maxProjectCleanups = 256

type projectCleanupCall struct {
	actor    identity.Actor
	cause    sc.LifecycleDetails
	active   bool
	retained bool
	original foundation.CommitResult
}

func (s *serviceState) borrowProjectCleanup(actor identity.Actor, cause sc.LifecycleDetails, project identity.ProjectID) (*projectCleanupCall, error) {
	s.cleanupMu.Lock()
	defer s.cleanupMu.Unlock()
	if call := s.cleanupCalls[project]; call != nil {
		if call.active {
			return nil, failure(Busy, foundation.ResourceBusy, nil)
		}
		if !call.actor.Equal(actor) || call.cause != cause {
			return nil, failure(AuthorizationRejected, foundation.InvalidState, nil)
		}
		call.active = true
		return call, nil
	}
	if len(s.cleanupCalls) >= maxProjectCleanups {
		return nil, failure(Busy, foundation.ResourceBusy, nil)
	}
	if s.cleanupCalls == nil {
		s.cleanupCalls = make(map[identity.ProjectID]*projectCleanupCall)
	}
	call := &projectCleanupCall{actor: actor, cause: cause, active: true}
	s.cleanupCalls[project] = call
	return call, nil
}

func (s *serviceState) releaseProjectCleanup(project identity.ProjectID, call *projectCleanupCall) {
	s.cleanupMu.Lock()
	defer s.cleanupMu.Unlock()
	call.active = false
	if !call.retained {
		delete(s.cleanupCalls, project)
	}
}

// Keep the original result private. Formatting the error never exposes its
// cause payload; the public Fault carries only the original attempt identity.
type projectCleanupCommitFailure struct{ result foundation.CommitResult }

func (*projectCleanupCommitFailure) Error() string { return string(CommitUnknown) }

func projectCleanupUnknown(result foundation.CommitResult) error {
	fault := foundation.NewFault(foundation.CommitUnknown, foundation.Unknown)
	if result.AttemptID().Validate() == nil {
		fault.CauseID = result.AttemptID().String()
	}
	fault.RetryHint = "lookup"
	fault = fault.WithCause(&projectCleanupCommitFailure{result: result})
	return &Error{code: CommitUnknown, cause: func() error { return fault }}
}

func projectCleanupResultError(result foundation.CommitResult) error {
	if result.State() == foundation.Unknown {
		return projectCleanupUnknown(result)
	}
	return commitError(result)
}

func projectCleanupEmpty(ctx context.Context, e postgres.SQLExecutor, project identity.ProjectID) (bool, error) {
	var empty bool
	err := e.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM agenteam_secret.secret_payloads WHERE scope='project' AND project_id=$1) AND NOT EXISTS(SELECT 1 FROM agenteam_secret.secrets WHERE scope='project' AND project_id=$1) AND NOT EXISTS(SELECT 1 FROM agenteam_secret.secret_command_receipts WHERE scope='project' AND project_id=$1) AND NOT EXISTS(SELECT 1 FROM agenteam_secret.project_variable_receipts WHERE project_id=$1) AND NOT EXISTS(SELECT 1 FROM agenteam_secret.secret_references WHERE project_id=$1) AND NOT EXISTS(SELECT 1 FROM agenteam_secret.secret_leases WHERE project_id=$1)`, project.String()).Scan(&empty)
	if err != nil {
		return false, unavailable(err)
	}
	if err = ctx.Err(); err != nil {
		return false, unavailable(err)
	}
	return empty, nil
}
