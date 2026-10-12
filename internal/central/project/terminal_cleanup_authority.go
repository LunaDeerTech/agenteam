package project

import (
	"bytes"
	"context"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// This gate belongs only to the fixed terminal driver round. Neither an old
// Agent/Skills/Variables scope nor a public receipt DTO can issue it.
func (a *Authority) RequireProjectTerminalCleanupInTx(ctx context.Context, tx f.Tx, request c.ProjectTerminalCleanupRequest) error {
	if ctx == nil || !tx.Valid() {
		return invalid()
	}
	if err := request.Validate(); err != nil {
		return portError(err)
	}
	if err := ctx.Err(); err != nil {
		return portError(err)
	}
	s := a.state()
	if s == nil {
		return fault(f.DependencyUnbound)
	}
	lifecycle, ok := s.lifecycle.(*LifecycleAuthority)
	if !ok || !lifecycle.bound() || !sameStore(s.store, lifecycle.store) {
		return fault(f.DependencyUnbound)
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if nilPort(x) {
		return fault(f.DependencyUnbound)
	}
	if err = s.store.RequireHeldLocks(ctx, tx, request.RequiredLocks()); err != nil {
		return portError(err)
	}
	return lifecycle.requireTerminalCleanupClaimInTx(ctx, tx, request)
}

func terminalCleanupProgress(r *lifecycleRecord, completed bool) error {
	if !completed {
		return skillsCleanupProgress(r)
	}
	if r == nil || r.operation.Action != c.Delete || r.operation.State != c.OperationCleaning || r.cleanupStage != "domains" {
		return fault(f.InvalidState)
	}
	states := make(map[c.ParticipantName]string, len(r.participants))
	for _, row := range r.participants {
		if row.stop != "stopped" {
			return unavailable(nil)
		}
		states[row.name] = row.cleanup
	}
	if states[c.SkillsParticipant] != "completed" {
		return fault(f.InvalidState)
	}
	for _, entry := range r.manifest.Entries() {
		if entry.Name == c.SkillsParticipant {
			for _, dependency := range entry.CleanupAfter {
				if states[dependency] != "completed" {
					return fault(f.InvalidState)
				}
			}
			return nil
		}
	}
	return fault(f.DependencyUnbound)
}

func (a *LifecycleAuthority) requireTerminalCleanupClaimInTx(ctx context.Context, tx f.Tx, request c.ProjectTerminalCleanupRequest) error {
	w, _ := ctx.Value(lifecycleCleanupScopeKey{}).(*lifecycleCleanupScope)
	if w == nil || w.state == nil || w.state.authority != a || !w.round.terminalStep || w.round.terminalConfirm != request.Confirmation || w.round.terminalUnapplied != request.NotApplied || w.round.agentStep || w.round.variablesStep || w.round.participant != c.SkillsParticipant || w.round.claim.project != request.ProjectID || w.round.cause != request.Cause || nilPort(w.state.terminalAgents) || nilPort(w.state.terminalVariables) {
		return fault(f.Forbidden)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.open {
		return fault(f.Forbidden)
	}
	x, err := a.lifecycleExecutor(ctx, tx, request.ProjectID)
	if err != nil {
		return err
	}
	if nilPort(x) {
		return fault(f.DependencyUnbound)
	}
	if _, err = a.lifecycleFact(ctx, x, request.ProjectID, request.Cause, c.SkillsParticipant); err != nil {
		return err
	}
	r, err := loadLifecycleOperation(ctx, x, request.ProjectID, request.Cause.OperationID)
	if err != nil {
		return err
	}
	if err = terminalCleanupProgress(r, request.Confirmation && !request.NotApplied); err != nil {
		return err
	}
	actual, err := loadLifecycleStopClaim(ctx, x, request.Cause.OperationID)
	if err != nil {
		return err
	}
	if actual == nil || actual.project != w.round.claim.project || actual.operation != w.round.claim.operation || actual.process != w.round.claim.process || actual.attempt != w.round.claim.attempt || actual.fence != w.round.claim.fence {
		return fault(f.ResourceBusy)
	}
	if request.Confirmation && !request.NotApplied {
		if actual.phase != "terminal" {
			return fault(f.ResourceBusy)
		}
	} else if actual.phase != "running" && !(request.NotApplied && actual.phase == "terminal") {
		return fault(f.ResourceBusy)
	}
	_, _, cp, err := cleanupParticipantRow(ctx, x, w.round)
	if err != nil {
		return err
	}
	if cp == nil || w.round.checkpoint == nil || cp.Schema() != w.round.checkpoint.Schema() || !bytes.Equal(cp.Bytes(), w.round.checkpoint.Bytes()) {
		return fault(f.Forbidden)
	}
	var expected c.ProjectTerminalOwnerIdentity
	if request.Confirmation && !request.NotApplied {
		receipt, err := decodeTerminalCleanupCheckpoint(cp, request.Cause, request.ProjectID)
		if err != nil {
			return err
		}
		if !receipt.matchesClaim(*actual) {
			return fault(f.Forbidden)
		}
		expected = receipt.AgentOwner
		if request.Owner.Kind == c.ProjectTerminalVariablesOwner {
			expected = receipt.VariablesOwner
		}
	} else {
		progress, _, err := decodeVariablesCleanupCheckpoint(cp, request.Cause, request.ProjectID)
		if err != nil {
			return err
		}
		if cp.Schema() != 5 || progress.State != c.CleanupCompleted {
			return fault(f.InvalidState)
		}
		expected = w.state.terminalAgents.ProjectTerminalCleanupIdentity()
		if request.Owner.Kind == c.ProjectTerminalVariablesOwner {
			expected = w.state.terminalVariables.ProjectTerminalCleanupIdentity()
		}
	}
	if expected.Validate() != nil || expected != request.Owner {
		return fault(f.Forbidden)
	}
	return portError(ctx.Err())
}

// Application verifies the last exact anchor before any command deletion.
// Receipt confirmation has no permission to reload a deleted command or to
// reissue prepared/canonical retirement authority.
func (a *Authority) RequireProjectTerminalAgentAppliedInTx(ctx context.Context, tx f.Tx, plan ac.ProjectRetirementPlan) error {
	if ctx == nil || !tx.Valid() || plan.Validate() != nil {
		return invalid()
	}
	w, _ := ctx.Value(lifecycleCleanupScopeKey{}).(*lifecycleCleanupScope)
	if w == nil || w.state == nil || nilPort(w.state.terminalAgents) || w.round.terminalConfirm || w.round.terminalUnapplied {
		return fault(f.Forbidden)
	}
	s := a.state()
	if s == nil {
		return fault(f.DependencyUnbound)
	}
	if _, err := s.store.InTx(tx); err != nil {
		return portError(err)
	}
	if err := s.store.RequireHeldLocks(ctx, tx, plan.Change.Request.RequiredLocks()); err != nil {
		return portError(err)
	}
	req := plan.Change.Request
	if err := a.RequireProjectTerminalCleanupInTx(ctx, tx, c.ProjectTerminalCleanupRequest{Actor: req.Actor, Cause: req.Cause, ProjectID: req.ProjectID, Owner: w.state.terminalAgents.ProjectTerminalCleanupIdentity()}); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.open {
		return fault(f.Forbidden)
	}
	_, previous, err := decodeVariablesCleanupCheckpoint(w.round.checkpoint, req.Cause, req.ProjectID)
	if err != nil {
		return err
	}
	if previous.Applied == nil || !sameAgentCleanupAnchor(*previous.Applied, agentCleanupAnchor(plan)) {
		return fault(f.Forbidden)
	}
	return portError(ctx.Err())
}

var _ c.ProjectTerminalCleanupAuthority = (*Authority)(nil)
