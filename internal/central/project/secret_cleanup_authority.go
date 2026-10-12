package project

import (
	"bytes"
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func secretLifecycleCause(cause sc.LifecycleCause) (c.LifecycleCause, error) {
	if cause.Validate() != nil {
		return c.LifecycleCause{}, invalid()
	}
	d := cause.Details()
	if !d.Deleting {
		return c.LifecycleCause{}, fault(f.Forbidden)
	}
	operation, err := f.ParseID[c.Operation](d.OperationID.String())
	if err != nil {
		return c.LifecycleCause{}, invalid()
	}
	return c.LifecycleCause{OperationID: operation, Action: c.Delete, ProjectVersion: d.ProjectVersion}, nil
}

// Every original Secret deletion and its read-only Unknown confirmation uses
// this gate. The public deleting label is never an issuer.
func (a *LifecycleAuthority) validateSecretCleanupInTx(ctx context.Context, tx f.Tx, project c.ProjectID, cause c.LifecycleCause) error {
	if ctx == nil || !tx.Valid() || project.Validate() != nil || cause.Validate() != nil {
		return invalid()
	}
	if cause.Action != c.Delete {
		return fault(f.Forbidden)
	}
	if err := ctx.Err(); err != nil {
		return portError(err)
	}
	x, err := a.lifecycleExecutor(ctx, tx, project)
	if err != nil {
		return err
	}
	if nilPort(x) {
		return fault(f.DependencyUnbound)
	}
	if err = a.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(project, f.Exclusive)}); err != nil {
		return unavailable(err)
	}
	fact, err := a.lifecycleFact(ctx, x, project, cause, c.SecretParticipant)
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
	if err = secretCleanupProgress(r); err != nil {
		return err
	}
	return a.requireSecretCleanupClaimInTx(ctx, tx, cause, project)
}

// This predicate describes only persisted prerequisites. Actual authority
// also requires the original fixed driver's live Secret claim and owner tail.
func secretCleanupProgress(r *lifecycleRecord) error {
	if r == nil {
		return unavailable(nil)
	}
	if r.operation.Action != c.Delete || r.operation.State != c.OperationCleaning || r.cleanupStage != "domains" {
		return fault(f.InvalidState)
	}
	states := make(map[c.ParticipantName]string, len(r.participants))
	for _, row := range r.participants {
		if row.stop != "stopped" {
			return unavailable(nil)
		}
		if _, exists := states[row.name]; exists {
			return unavailable(nil)
		}
		states[row.name] = row.cleanup
	}
	entries := r.manifest.Entries()
	if len(states) != len(entries) {
		return unavailable(nil)
	}
	for _, entry := range entries {
		if _, exists := states[entry.Name]; !exists {
			return unavailable(nil)
		}
	}
	if states[c.SecretParticipant] != "required" && states[c.SecretParticipant] != "pending" || states[c.SkillsParticipant] != "completed" {
		return fault(f.InvalidState)
	}
	for _, entry := range entries {
		if entry.Name != c.SecretParticipant {
			continue
		}
		business := false
		for _, dependency := range entry.CleanupAfter {
			if states[dependency] != "completed" {
				return fault(f.InvalidState)
			}
			business = business || dependency == c.SkillsParticipant
		}
		if !business {
			return fault(f.DependencyUnbound)
		}
		return nil
	}
	return fault(f.DependencyUnbound)
}

func (a *LifecycleAuthority) requireSecretCleanupClaimInTx(ctx context.Context, tx f.Tx, cause c.LifecycleCause, project c.ProjectID) error {
	if ctx == nil {
		return invalid()
	}
	w, _ := ctx.Value(lifecycleCleanupScopeKey{}).(*lifecycleCleanupScope)
	if w == nil || w.state == nil || nilPort(w.state.secret) || w.round.participant != c.SecretParticipant || w.round.agentStep || w.round.variablesStep || w.round.terminalStep {
		return fault(f.Forbidden)
	}
	if err := a.requireCleanupClaimInTx(ctx, tx, cause, project, c.SecretParticipant); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.open || w.round.secretParentCheckpoint == nil || w.round.secretParentCheckpoint.Schema() != 6 {
		return fault(f.Forbidden)
	}
	x, err := a.lifecycleExecutor(ctx, tx, project)
	if err != nil {
		return err
	}
	if nilPort(x) {
		return fault(f.DependencyUnbound)
	}
	parent := w.round
	parent.participant = c.SkillsParticipant
	_, _, checkpoint, err := cleanupParticipantRow(ctx, x, parent)
	if err != nil {
		return err
	}
	if checkpoint == nil || checkpoint.Schema() != 6 || !bytes.Equal(checkpoint.Bytes(), w.round.secretParentCheckpoint.Bytes()) {
		return fault(f.Forbidden)
	}
	if _, err = decodeTerminalCleanupCheckpoint(checkpoint, cause, project); err != nil {
		return err
	}
	return portError(ctx.Err())
}
