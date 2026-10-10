package skill

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type cleanupSnapshot struct {
	row          *initializationRow
	cleanup      *cleanupRow
	empty, ready bool
}

func (s *Service) inspectCleanup(ctx context.Context, actor id.Actor, cause pc.LifecycleCause, scope pc.ScopeRef, hint *cleanupCheckpointData) (cleanupSnapshot, error) {
	var out cleanupSnapshot
	discovered, err := loadInitialization(ctx, s.state().authority.state().store, scope.ProjectID)
	if err != nil {
		return out, err
	}
	err = s.cleanupTransaction(ctx, actor, cause, scope, discovered, nil, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor, row *initializationRow, _ oc.LockedAccess) error {
		out.row = row
		if row == nil {
			empty, err := cleanupAllEmpty(ctx, x, scope.ProjectID)
			if err != nil {
				return err
			}
			if !empty {
				return unavailable(nil)
			}
			out.empty = s.cleanupLocalJoined(scope.ProjectID)
			return nil
		}
		if row.phase != initializationPublished {
			return unavailable(nil)
		}
		c, err := loadCleanup(ctx, x, scope.ProjectID)
		if err != nil {
			return err
		}
		if c != nil && (!c.matches(*row) || c.cause != cause) {
			return fault(f.Forbidden)
		}
		if hint != nil && (c == nil || hint.Cleanup != c.id.String()) {
			return fault(f.Forbidden)
		}
		if err = cleanupCore(ctx, x, *row, c != nil); err != nil {
			return err
		}
		out.cleanup = c
		joined, err := cleanupWorkJoined(ctx, x, scope.ProjectID)
		if err != nil {
			return err
		}
		out.ready = joined && s.cleanupLocalJoined(scope.ProjectID)
		return nil
	})
	return out, err
}

func (s *Service) requireCleanup(ctx context.Context, x postgres.SQLExecutor, row *initializationRow, expected cleanupRow) (*cleanupRow, error) {
	if row == nil || !expected.matches(*row) {
		return nil, fault(f.ResourceBusy)
	}
	c, err := loadCleanup(ctx, x, expected.project)
	if err != nil {
		return nil, err
	}
	if c == nil || !c.matches(*row) || c.id != expected.id || c.cause != expected.cause {
		return nil, fault(f.ResourceBusy)
	}
	if err = cleanupCore(ctx, x, *row, true); err != nil {
		return nil, err
	}
	joined, err := cleanupWorkJoined(ctx, x, expected.project)
	if err != nil {
		return nil, err
	}
	if !joined || !s.cleanupLocalJoined(expected.project) {
		return nil, fault(f.ResourceBusy)
	}
	return c, nil
}

func (s *Service) gateCleanup(ctx context.Context, actor id.Actor, cause pc.LifecycleCause, scope pc.ScopeRef, row initializationRow) (*cleanupRow, error) {
	key, err := f.NewID[oc.CleanupOperation]()
	if err != nil {
		return nil, unavailable(err)
	}
	c := cleanupRow{id: key, project: scope.ProjectID, cause: cause, skill: row.skill, revision: row.revision, object: row.object, upload: row.upload, phase: cleanupGated, version: 1}
	objectCause, err := c.objectCause(row)
	if err != nil {
		return nil, err
	}
	request, err := oc.NewCleanupReleaseAccess(oc.AccessRequestDetails{Operation: oc.ReleaseForCleanupAccess, Cleanup: objectCause, ObjectID: row.object, UploadID: row.upload})
	if err != nil {
		return nil, err
	}
	plan, err := s.cleanupPlan(ctx, request)
	if err != nil {
		return nil, err
	}
	err = s.cleanupTransaction(ctx, actor, cause, scope, &row, []oc.AccessLockPlan{plan}, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, actual *initializationRow, locked oc.LockedAccess) error {
		if actual == nil || !c.matches(*actual) {
			return fault(f.ResourceBusy)
		}
		current, err := loadCleanup(ctx, x, scope.ProjectID)
		if err != nil {
			return err
		}
		// Another first caller or an original late COMMIT may now own this
		// unique gate. Rediscover its identity; never reuse this foreign plan.
		if current != nil {
			return fault(f.ResourceBusy)
		}
		joined, err := cleanupWorkJoined(ctx, x, scope.ProjectID)
		if err != nil {
			return err
		}
		if !joined || !s.cleanupLocalJoined(scope.ProjectID) {
			return fault(f.ResourceBusy)
		}
		if err = insertCleanupGate(ctx, x, *actual, c); err != nil {
			return err
		}
		return portError(s.state().objects.ReleaseForCleanupInTx(ctx, tx, objectCause, row.object, plan, locked))
	})
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Service) cleanupTransaction(ctx context.Context, actor id.Actor, cause pc.LifecycleCause, scope pc.ScopeRef, discovered *initializationRow, plans []oc.AccessLockPlan, fn func(context.Context, f.Tx, postgres.SQLExecutor, *initializationRow, oc.LockedAccess) error) error {
	if err := stopArguments(actor, cause, scope); err != nil {
		return err
	}
	if cause.Action != pc.Delete {
		return invalid()
	}
	locks := []f.LockRequest{projectLock(scope.ProjectID, f.Exclusive)}
	var err error
	if discovered != nil {
		if discovered.request.ProjectID != scope.ProjectID {
			return invalid()
		}
		locks, err = discovered.locks(f.Exclusive, discovered.object)
		if err != nil {
			return err
		}
	}
	transactionCause, err := f.NewRecoveryCause("skill-cleanup", cause.OperationID.String(), scope.ProjectID.String())
	if err != nil {
		return err
	}
	state := s.state()
	store := state.authority.state().store
	var callbackErr error
	result := store.WithinTx(ctx, transactionCause, func(ctx context.Context, tx f.Tx) (err error) {
		defer func() { callbackErr = err }()
		var locked oc.LockedAccess
		if len(plans) == 0 {
			err = store.AcquireAll(ctx, tx, locks)
		} else {
			locked, err = state.objects.AcquireAccessPlansInTx(ctx, tx, plans, locks)
		}
		if err != nil {
			return portError(err)
		}
		if err = store.RequireHeldLocks(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if err = state.authority.state().projects.ValidateLifecycleInTx(ctx, tx, actor, cause, pc.SkillsParticipant, pc.CleanupPhase); err != nil {
			return portError(err)
		}
		row, err := loadInitialization(ctx, x, scope.ProjectID)
		if err != nil {
			return err
		}
		if (row == nil) != (discovered == nil) || row != nil && !sameInitialization(*row, *discovered) {
			return fault(f.ResourceBusy)
		}
		return fn(ctx, tx, x, row, locked)
	})
	if result.State() == f.NotCommitted && callbackErr != nil {
		return callbackErr
	}
	return commitError(result)
}
