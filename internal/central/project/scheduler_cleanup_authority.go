package project

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/scheduler/contract"
)

// SchedulerProjectCleanupAuthority never issues permission from a public
// cause. Only the original fixed cleanup driver's active round is consumable.
type SchedulerProjectCleanupAuthority struct{ authority *LifecycleAuthority }

func NewSchedulerProjectCleanupAuthority(a *LifecycleAuthority, executorStore Store) (*SchedulerProjectCleanupAuthority, error) {
	if !a.bound() || nilPort(executorStore) || !sameStore(a.store, executorStore) {
		return nil, fault(f.DependencyUnbound)
	}
	return &SchedulerProjectCleanupAuthority{a}, nil
}
func (a *SchedulerProjectCleanupAuthority) RequireProjectCleanupInTx(ctx context.Context, tx f.Tx, actor i.Actor, cause c.LifecycleCause, target c.ScopeRef) error {
	if a == nil || !a.authority.bound() {
		return fault(f.DependencyUnbound)
	}
	project, err := lifecycleActorProject(actor, cause)
	if err != nil {
		return err
	}
	if c.RequireProjectScope(target) != nil || target.ProjectID != project {
		return fault(f.Forbidden)
	}
	return a.authority.validateSchedulerCleanupInTx(ctx, tx, project, cause)
}
func (a *LifecycleAuthority) validateSchedulerCleanupInTx(ctx context.Context, tx f.Tx, project c.ProjectID, cause c.LifecycleCause) error {
	if !a.bound() {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil || !tx.Valid() || project.Validate() != nil || cause.Validate() != nil {
		return invalid()
	}
	if err := ctx.Err(); err != nil {
		return portError(err)
	}
	if cause.Action != c.Delete {
		return fault(f.Forbidden)
	}
	w, _ := ctx.Value(lifecycleCleanupScopeKey{}).(*lifecycleCleanupScope)
	if w == nil || w.state == nil || w.state.authority != a || !sameStore(w.state.store, a.store) || nilPort(w.state.scheduler) || w.round.participant != c.SchedulerParticipant || w.round.agentStep || w.round.variablesStep || w.round.terminalStep || w.round.secretParentStep {
		return fault(f.Forbidden)
	}
	if err := a.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(project, f.Exclusive), schedulerLock(project)}); err != nil {
		return portError(err)
	}
	x, err := a.lifecycleExecutor(ctx, tx, project)
	if err != nil {
		return err
	}
	if nilPort(x) {
		return fault(f.DependencyUnbound)
	}
	if err := a.requireCleanupClaimInTx(ctx, tx, cause, project, c.SchedulerParticipant); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.open {
		return fault(f.Forbidden)
	}
	fact, err := a.lifecycleFact(ctx, x, project, cause, c.SchedulerParticipant)
	if err != nil {
		return err
	}
	if fact.state != c.OperationCleaning {
		return fault(f.InvalidState)
	}
	r, err := loadLifecycleOperation(ctx, x, project, cause.OperationID)
	if err != nil {
		return err
	}
	if err = schedulerCleanupProgress(r); err != nil {
		return err
	}
	return portError(ctx.Err())
}

// This is only the persisted prerequisite predicate. It neither grants a
// cleanup transaction nor substitutes for any owner's actual returned call.
func schedulerCleanupProgress(r *lifecycleRecord) error {
	if r == nil {
		return unavailable(nil)
	}
	if r.operation.Action != c.Delete || r.operation.State != c.OperationCleaning || r.cleanupStage != "domains" {
		return fault(f.InvalidState)
	}
	states := map[c.ParticipantName]string{}
	for _, row := range r.participants {
		if row.stop != "stopped" {
			return fault(f.InvalidState)
		}
		if _, ok := states[row.name]; ok {
			return unavailable(nil)
		}
		states[row.name] = row.cleanup
	}
	entries := r.manifest.Entries()
	if len(states) != len(entries) {
		return unavailable(nil)
	}
	for _, entry := range entries {
		if _, ok := states[entry.Name]; !ok {
			return unavailable(nil)
		}
	}
	if states[c.SchedulerParticipant] != "required" && states[c.SchedulerParticipant] != "pending" {
		return fault(f.InvalidState)
	}
	for _, entry := range entries {
		if entry.Name != c.SchedulerParticipant {
			continue
		}
		for _, prior := range entry.CleanupAfter {
			if states[prior] != "completed" {
				return fault(f.InvalidState)
			}
		}
		return nil
	}
	return fault(f.DependencyUnbound)
}

var _ sc.ProjectCleanupAuthority = (*SchedulerProjectCleanupAuthority)(nil)
