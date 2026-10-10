package skill

import (
	"context"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// This kind exists only in the actual call ledger, never in the work table.
// D08 and D05 retain their own durable cleanup claims and worker identities.
const cleanupCallKind workKind = "cleanup"

// Cleanup implements only this participant's Skills domain. The composition
// root must also include every other enabled agent-skills-variables domain;
// no Name method registers this service as their complete participant.
func (s *Service) Cleanup(ctx context.Context, actor id.Actor, cause pc.LifecycleCause, scope pc.ScopeRef, checkpoint *pc.CleanupCheckpoint) (pc.CleanupReport, error) {
	if err := stopArguments(actor, cause, scope); err != nil {
		return pc.CleanupReport{}, err
	}
	if cause.Action != pc.Delete || ctx == nil {
		return pc.CleanupReport{}, invalid()
	}
	hint, err := parseCleanupCheckpoint(checkpoint, cause, scope)
	if err != nil {
		return pc.CleanupReport{}, err
	}
	state := s.state()
	if state == nil {
		return pc.CleanupReport{}, fault(f.DependencyUnbound)
	}
	// The optional capability must belong to the SAME actual Object instance.
	// Check it before closing any irreversible gate; no successful fallback.
	purger, ok := state.objects.(oc.DeletedObjectMetadataPurger)
	if !ok || nilPort(purger) {
		return pc.CleanupReport{}, fault(f.DependencyUnbound)
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	call, err := s.beginCleanup(ctx, scope.ProjectID)
	if err != nil {
		return pc.CleanupReport{}, err
	}
	defer s.end(call)
	ctx = call.ctx
	// Ordinary commands are real cleanup parents, including an interrupted
	// reservation without a canonical Skill. One bounded step shares this call,
	// its original deadline and actual Object tail before builtin finalization.
	if handled, err := s.cleanupInstallations(ctx, actor, cause, scope, purger); err != nil {
		return pc.CleanupReport{}, err
	} else if handled {
		return cleanupReport(cause, scope, nil, false)
	}

	snapshot, err := s.inspectCleanup(ctx, actor, cause, scope, hint)
	if err != nil {
		return pc.CleanupReport{}, err
	}
	if snapshot.empty {
		return cleanupReport(cause, scope, nil, true)
	}
	if !snapshot.ready {
		return cleanupReport(cause, scope, snapshot.cleanup, false)
	}
	row := *snapshot.row
	c := snapshot.cleanup
	if c == nil {
		c, err = s.gateCleanup(ctx, actor, cause, scope, row)
		if err != nil {
			return pc.CleanupReport{}, err
		}
	}
	if c.phase != cleanupCompleted {
		objectCause, err := c.objectCause(row)
		if err != nil {
			return pc.CleanupReport{}, err
		}
		// This synchronous call includes its real native I/O and transaction
		// tail. Cancellation and a caller-built result are not retirement.
		result, err := state.objects.DeleteUnreferencedWithinBudget(ctx, objectCause, row.object)
		if err != nil {
			return pc.CleanupReport{}, portError(err)
		}
		if result.OperationID != c.id || result.State != oc.CleanupPending && result.State != oc.CleanupCompleted {
			return pc.CleanupReport{}, unavailable(nil)
		}
		completed := result.State == oc.CleanupCompleted
		if completed && (len(result.Remaining.References) != 0 || len(result.Remaining.ActiveLeases) != 0) {
			return pc.CleanupReport{}, unavailable(nil)
		}
		next := cleanupPending
		if completed {
			next = cleanupCompleted
		}
		current := *c
		err = s.cleanupTransaction(ctx, actor, cause, scope, &row, nil, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor, actual *initializationRow, _ oc.LockedAccess) error {
			persisted, err := s.requireCleanup(ctx, x, actual, current)
			if err != nil {
				return err
			}
			if persisted.phase == cleanupCompleted {
				current = *persisted
				return nil
			}
			if persisted.phase != next {
				if err = setCleanupPhase(ctx, x, *persisted, next); err != nil {
					return err
				}
				persisted.phase, persisted.version = next, persisted.version+1
			}
			current = *persisted
			return nil
		})
		if err != nil {
			return pc.CleanupReport{}, err
		}
		c = &current
		// Committing physical completion is a distinct bounded step. No
		// second full budget or extra local history batch is added here.
		return cleanupReport(cause, scope, c, false)
	}

	removed := false
	err = s.cleanupTransaction(ctx, actor, cause, scope, &row, nil, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor, actual *initializationRow, _ oc.LockedAccess) error {
		persisted, err := s.requireCleanup(ctx, x, actual, *c)
		if err != nil {
			return err
		}
		if persisted.phase != cleanupCompleted {
			return fault(f.InvalidState)
		}
		removed, err = compressCleanupHistory(ctx, x, *actual)
		return err
	})
	if err != nil {
		return pc.CleanupReport{}, err
	}
	if removed {
		return cleanupReport(cause, scope, c, false)
	}

	objectCause, err := c.objectCause(row)
	if err != nil {
		return pc.CleanupReport{}, err
	}
	request, err := oc.NewObjectCleanupAccess(oc.AccessRequestDetails{Operation: oc.PurgeDeletedObjectMetadataAccess, Cleanup: objectCause, ObjectID: row.object})
	if err != nil {
		return pc.CleanupReport{}, err
	}
	plan, err := s.cleanupPlan(ctx, request)
	if err != nil {
		return pc.CleanupReport{}, err
	}
	complete := false
	err = s.cleanupTransaction(ctx, actor, cause, scope, &row, []oc.AccessLockPlan{plan}, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, actual *initializationRow, locked oc.LockedAccess) error {
		persisted, err := s.requireCleanup(ctx, x, actual, *c)
		if err != nil {
			return err
		}
		if persisted.phase != cleanupCompleted {
			return fault(f.InvalidState)
		}
		empty, err := cleanupHistoryEmpty(ctx, x, *actual)
		if err != nil {
			return err
		}
		if !empty {
			return fault(f.ResourceBusy)
		}
		result, err := purger.PurgeDeletedObjectMetadataInTx(ctx, tx, objectCause, row.object, plan, locked)
		if err != nil {
			return portError(err)
		}
		if !result.MatchesOperation(c.id, row.object) {
			return unavailable(nil)
		}
		if result.State == oc.CleanupPending {
			return nil
		}
		// This is still the original union Tx. A failure in any local delete
		// rolls back the provider's final four anchors, too.
		if err = deleteCleanupCore(ctx, x, *actual, *persisted); err != nil {
			return err
		}
		complete = true
		return nil
	})
	if err != nil {
		return pc.CleanupReport{}, err
	}
	return cleanupReport(cause, scope, c, complete)
}

func (s *Service) beginCleanup(ctx context.Context, project id.ProjectID) (*serviceCall, error) {
	state := s.state()
	if state == nil {
		return nil, fault(f.DependencyUnbound)
	}
	if ctx == nil || project.Validate() != nil {
		return nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, portError(err)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.stopped {
		return nil, fault(f.ShuttingDown)
	}
	for current := range state.calls {
		if current.project == project && current.kind == cleanupCallKind {
			return nil, fault(f.ResourceBusy)
		}
	}
	callCtx, cancel := context.WithCancel(ctx)
	call := &serviceCall{ctx: callCtx, cancel: cancel, project: project, kind: cleanupCallKind}
	state.calls[call] = struct{}{}
	return call, nil
}

// Unknown registrations and returned-but-unconfirmed work retain ownership.
// A concurrent Cleanup does not count as old business work or wait on itself.
func (s *Service) cleanupLocalJoined(project id.ProjectID) bool {
	state := s.state()
	state.mu.Lock()
	defer state.mu.Unlock()
	for call := range state.calls {
		if call.project == project && stoppedKind(pc.Delete, call.kind) {
			return false
		}
	}
	for _, work := range state.work {
		if work.fact.project == project {
			return false
		}
	}
	return true
}

func (s *Service) cleanupPlan(ctx context.Context, request oc.AccessRequest) (oc.AccessLockPlan, error) {
	plan, err := s.state().objects.DiscoverAccess(ctx, request)
	if err != nil {
		return oc.AccessLockPlan{}, portError(err)
	}
	if plan.Validate() != nil || !plan.Details().Request.Equal(request) {
		return oc.AccessLockPlan{}, unavailable(nil)
	}
	return plan, nil
}
