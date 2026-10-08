package audit

import (
	"context"
	"errors"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

const projectReadBudget = 3 * time.Second

type projectReadRun struct{}
type projectReadUnknown struct{ result foundation.CommitResult }

func (projectReadUnknown) Error() string { return string(foundation.CommitUnknown) }

// ProjectReadUnknownAttempt retains the original physical read transaction.
// It is not a receipt lookup or a reason to retry the observation automatically.
func ProjectReadUnknownAttempt(err error) (foundation.CommitResult, bool) {
	var unknown projectReadUnknown
	if errors.As(err, &unknown) {
		return unknown.result, true
	}
	return foundation.CommitResult{}, false
}

// ListProject authorizes and observes under the same User/Project shared locks.
// A complete scan, including its extra pagination row, is only a candidate
// until the real transaction returns Committed within the original budget.
func (s *Service) ListProject(ctx context.Context, actor identity.Actor, project identity.ProjectID, filter c.Filter, page foundation.PageRequest) (foundation.Page[c.SafeRecord], error) {
	var candidate foundation.Page[c.SafeRecord]
	err := s.projectRead(ctx, actor, project, func(ctx context.Context, sql postgres.SQLExecutor, scope identity.Scope) error {
		check, err := s.projectPageCheck(scope, filter, page)
		if err != nil {
			return err
		}
		candidate, err = s.listRecords(ctx, sql, scope, filter, page, check)
		return err
	})
	if err != nil {
		return foundation.Page[c.SafeRecord]{}, err
	}
	return candidate, nil
}

// GetProject has the same current-authorization and actual publication boundary.
func (s *Service) GetProject(ctx context.Context, actor identity.Actor, project identity.ProjectID, id c.ID) (c.SafeRecord, error) {
	var candidate c.SafeRecord
	err := s.projectRead(ctx, actor, project, func(ctx context.Context, sql postgres.SQLExecutor, scope identity.Scope) error {
		var err error
		candidate, err = getRecord(ctx, sql, scope, id, func(record c.SafeRecord) error {
			if !record.Scope.Equal(scope) || record.AuditID != id {
				return unavailable(nil)
			}
			return nil
		})
		return err
	})
	if err != nil {
		return c.SafeRecord{}, err
	}
	return candidate, nil
}

func (s *Service) projectRead(parent context.Context, actor identity.Actor, project identity.ProjectID, read func(context.Context, postgres.SQLExecutor, identity.Scope) error) error {
	if parent == nil || project.Validate() != nil {
		return invalid("project_read")
	}
	if actor.Validate() != nil || actor.Details().Kind != identity.Human {
		return failure(foundation.Forbidden, "human_required", nil)
	}
	ctx, cancel := context.WithTimeout(parent, projectReadBudget)
	defer cancel()
	if err := readContextError(ctx); err != nil {
		return unavailable(err)
	}
	if s == nil || nilPort(s.store) || nilPort(s.auth.Sessions) || nilPort(s.auth.Projects) {
		return failure(foundation.DependencyUnbound, "project_read_unbound", nil)
	}
	scope, _ := identity.InProject(project)
	userLock, _ := foundation.UserLock(actor.Details().UserID)
	projectLock, _ := foundation.ProjectLock(project.String())
	run, err := foundation.NewID[projectReadRun]()
	if err != nil {
		return unavailable(err)
	}
	cause, err := foundation.NewRecoveryCause("audit.project-read", run.String(), "")
	if err != nil {
		return unavailable(err)
	}
	complete := false
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if err := readContextError(ctx); err != nil {
			return unavailable(err)
		}
		for _, lock := range []foundation.LockKey{userLock, projectLock} {
			if err := s.store.Acquire(ctx, tx, lock, foundation.Shared); err != nil {
				return unavailable(err)
			}
		}
		if err := s.authorizeHuman(ctx, tx, actor, scope, identity.Read); err != nil {
			return err
		}
		sql, err := s.store.InTx(tx)
		if err != nil || nilPort(sql) {
			return unavailable(err)
		}
		if err := read(ctx, sql, scope); err != nil {
			return err
		}
		if err := readContextError(ctx); err != nil {
			return unavailable(err)
		}
		complete = true
		return nil
	})
	switch result.State() {
	case foundation.NotCommitted:
		if fault := result.Fault(); fault != nil {
			return fault
		}
		return foundation.NewFault(foundation.InternalError, foundation.NotCommitted)
	case foundation.Committed:
		if err := readContextError(ctx); err != nil {
			return unavailable(err)
		}
		if !complete {
			return unavailable(nil)
		}
		return nil
	default:
		fault := foundation.NewFault(foundation.CommitUnknown, foundation.Unknown)
		fault.RetryHint = "lookup"
		if result.AttemptID().Validate() == nil {
			fault.CauseID = result.AttemptID().String()
		}
		return fault.WithCause(projectReadUnknown{result})
	}
}

func (s *Service) projectPageCheck(scope identity.Scope, filter c.Filter, page foundation.PageRequest) (func(c.SafeRecord) error, error) {
	if filter.Validate() != nil || page.Validate() != nil {
		return nil, invalid("page_filter")
	}
	var beforeAt foundation.Instant
	var beforeID string
	if page.Cursor != "" {
		digest, err := filterDigest(filter)
		if err != nil {
			return nil, err
		}
		position, err := s.keys.Verify(page.Cursor, cursor.Binding{Scope: scope, QueryDigest: digest, Order: cursor.AuditOrder})
		if err != nil {
			return nil, err
		}
		if len(position.Scalars) != 2 || position.Scalars[0].Kind() != "instant" || position.Scalars[1].Kind() != "uuid" || position.OrderGeneration != nil {
			return nil, failure(foundation.CursorInvalid, "cursor_invalid", nil)
		}
		beforeAt, err = foundation.ParseInstant(position.Scalars[0].Value())
		if err != nil {
			return nil, failure(foundation.CursorInvalid, "cursor_invalid", nil)
		}
		beforeID = position.Scalars[1].Value()
	}
	return func(record c.SafeRecord) error {
		if !record.Scope.Equal(scope) || !projectRecordMatches(record, filter) {
			return unavailable(nil)
		}
		if beforeID != "" {
			order := record.CreatedAt.Time().Compare(beforeAt.Time())
			if order > 0 || order == 0 && record.AuditID.String() >= beforeID {
				return unavailable(nil)
			}
		}
		beforeAt, beforeID = record.CreatedAt, record.AuditID.String()
		return nil
	}, nil
}

// SQL supplies the same predicates. Checking every returned row prevents a
// damaged dependency (including the sentinel) from weakening those bindings.
func projectRecordMatches(r c.SafeRecord, f c.Filter) bool {
	if f.From != nil && r.CreatedAt.Time().Before(f.From.Time()) || f.To != nil && !r.CreatedAt.Time().Before(f.To.Time()) {
		return false
	}
	resource, a := r.Resource.Details(), r.Associations
	for _, pair := range [][2]string{
		{string(f.ActorKind), string(r.Actor.Kind)}, {f.ActorID, r.Actor.ID},
		{string(f.Action), string(r.Action)}, {string(f.Outcome), string(r.Outcome)},
		{string(f.ResourceKind), string(resource.Kind)}, {f.ResourceID, resource.ID},
		{f.ToolID, a.ToolID}, {f.ExecutionID, a.ExecutionID}, {f.OperationID, a.OperationID},
		{f.ApprovalID, a.ApprovalID}, {f.RunnerID, a.RunnerID},
	} {
		if pair[0] != "" && pair[0] != pair[1] {
			return false
		}
	}
	return f.AgentID == "" || r.Actor.Kind == identity.AgentRun && r.Actor.ID == f.AgentID || resource.Kind == c.AgentResource && resource.ID == f.AgentID
}
