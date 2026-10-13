package project

import (
	"bytes"
	"context"
	"sync"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type ExecutionProjectRetirementAuthority struct{ authority *LifecycleAuthority }

func NewExecutionProjectRetirementAuthority(a *LifecycleAuthority, executorStore Store) (*ExecutionProjectRetirementAuthority, error) {
	if !a.bound() || nilPort(executorStore) || !sameStore(a.store, executorStore) {
		return nil, fault(f.DependencyUnbound)
	}
	return &ExecutionProjectRetirementAuthority{a}, nil
}

type executionRetirementScopeKey struct{}
type executionRetirementScope struct {
	state        *lifecycleCleanupState
	round        *executionCleanupRound
	mu           sync.Mutex
	open         bool
	confirmation bool
}

func (a *ExecutionProjectRetirementAuthority) RequireProjectExecutionCleanupInTx(ctx context.Context, tx f.Tx, actor i.Actor, cause c.LifecycleCause, target c.ScopeRef) error {
	if a == nil || !a.authority.bound() {
		return fault(f.DependencyUnbound)
	}
	p, err := lifecycleActorProject(actor, cause)
	if err != nil {
		return err
	}
	if c.RequireProjectScope(target) != nil || target.ProjectID != p {
		return fault(f.Forbidden)
	}
	return a.authority.validateExecutionRetirementInTx(ctx, tx, p, cause)
}

func (a *LifecycleAuthority) validateExecutionRetirementInTx(ctx context.Context, tx f.Tx, p c.ProjectID, cause c.LifecycleCause) error {
	if !a.bound() {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil || !tx.Valid() || p.Validate() != nil || cause.Validate() != nil {
		return invalid()
	}
	if err := ctx.Err(); err != nil {
		return portError(err)
	}
	w, _ := ctx.Value(executionRetirementScopeKey{}).(*executionRetirementScope)
	if cause.Action != c.Delete || w == nil || w.state == nil || w.round == nil || w.state.authority != a || !sameStore(w.state.store, a.store) || nilPort(w.state.executions) {
		return fault(f.Forbidden)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	round := w.round.round
	if !w.open || round.participant != c.ExecutionParticipant || round.claim.project != p || round.cause != cause || round.agentStep || round.variablesStep || round.terminalStep || round.secretParentStep {
		return fault(f.Forbidden)
	}
	if err := a.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(p, f.Exclusive), schedulerLock(p)}); err != nil {
		return portError(err)
	}
	x, err := a.lifecycleExecutor(ctx, tx, p)
	if err != nil {
		return err
	}
	if nilPort(x) {
		return fault(f.DependencyUnbound)
	}
	if _, err = a.lifecycleFact(ctx, x, p, cause, c.ExecutionParticipant); err != nil {
		return err
	}
	r, err := loadLifecycleOperation(ctx, x, p, cause.OperationID)
	if err != nil {
		return err
	}
	if err = executionCleanupProgress(r, w.confirmation); err != nil {
		return err
	}
	actual, err := loadLifecycleStopClaim(ctx, x, cause.OperationID)
	if err != nil {
		return err
	}
	if actual == nil || actual.project != round.claim.project || actual.operation != round.claim.operation || actual.process != round.claim.process || actual.attempt != round.claim.attempt || actual.fence != round.claim.fence {
		return fault(f.ResourceBusy)
	}
	version, _, cp, err := cleanupParticipantRow(ctx, x, round)
	if err != nil {
		return err
	}
	// The original mutation writes its checkpoint and terminal claim together.
	// A confirmation may observe that exact postimage or its unchanged preimage.
	before := actual.phase == "running" && version == round.rowVersion && r.operation.Version == round.operationVersion && sameExecutionCheckpoint(cp, round.checkpoint)
	after := w.confirmation && w.round.appliedCheckpoint != nil && actual.phase == "terminal" && version == round.rowVersion+1 && r.operation.Version == round.operationVersion+1 && sameExecutionCheckpoint(cp, w.round.appliedCheckpoint)
	if w.round.restored {
		after = w.confirmation && actual.phase == "terminal" && version == round.rowVersion && r.operation.Version == round.operationVersion && sameExecutionCheckpoint(cp, w.round.appliedCheckpoint)
		if after {
			proof, e := decodeExecutionRetirementCheckpoint(cp, cause, p)
			if e != nil {
				return e
			}
			after = proof.Commit.ProcessID == actual.process.String() && proof.Commit.AttemptID == actual.attempt && proof.Commit.Fence == actual.fence
		}
	}
	if !before && !after {
		return fault(f.ResourceBusy)
	}
	return portError(ctx.Err())
}

func sameExecutionCheckpoint(a, b *c.CleanupCheckpoint) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Schema() == b.Schema() && bytes.Equal(a.Bytes(), b.Bytes())
}

func (a *ExecutionProjectRetirementAuthority) ProjectExecutionRetirementCommitInTx(ctx context.Context, tx f.Tx, actor i.Actor, cause c.LifecycleCause, target c.ScopeRef) (ec.ProjectExecutionRetirementCommit, error) {
	if err := a.RequireProjectExecutionCleanupInTx(ctx, tx, actor, cause, target); err != nil {
		return ec.ProjectExecutionRetirementCommit{}, err
	}
	w := ctx.Value(executionRetirementScopeKey{}).(*executionRetirementScope)
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.open {
		return ec.ProjectExecutionRetirementCommit{}, fault(f.Forbidden)
	}
	if w.confirmation {
		if w.round.appliedCheckpoint == nil {
			return ec.ProjectExecutionRetirementCommit{}, fault(f.Forbidden)
		}
		cp, err := decodeExecutionRetirementCheckpoint(w.round.appliedCheckpoint, cause, target.ProjectID)
		if err != nil {
			return ec.ProjectExecutionRetirementCommit{}, err
		}
		return cp.Commit, nil
	}
	x, err := a.authority.store.InTx(tx)
	if err != nil {
		return ec.ProjectExecutionRetirementCommit{}, portError(err)
	}
	if nilPort(x) {
		return ec.ProjectExecutionRetirementCommit{}, fault(f.DependencyUnbound)
	}
	var xid string
	if err = x.QueryRow(ctx, `SELECT pg_current_xact_id()::text`).Scan(&xid); err != nil {
		return ec.ProjectExecutionRetirementCommit{}, unavailable(err)
	}
	claim := w.round.round.claim
	value := ec.ProjectExecutionRetirementCommit{ProjectID: target.ProjectID, OperationID: cause.OperationID, ProjectVersion: cause.ProjectVersion, ProcessID: claim.process.String(), AttemptID: claim.attempt, Fence: claim.fence, TransactionID: xid}
	if err = value.Validate(); err != nil {
		return ec.ProjectExecutionRetirementCommit{}, unavailable(err)
	}
	return value, portError(ctx.Err())
}

func executionCleanupProgress(r *lifecycleRecord, confirmation bool) error {
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
	if states[c.SchedulerParticipant] != "completed" {
		return fault(f.InvalidState)
	}
	if states[c.ExecutionParticipant] != "required" && states[c.ExecutionParticipant] != "pending" && !(confirmation && states[c.ExecutionParticipant] == "completed") {
		return fault(f.InvalidState)
	}
	for _, entry := range entries {
		if entry.Name != c.ExecutionParticipant {
			continue
		}
		found := false
		for _, prior := range entry.CleanupAfter {
			if prior == c.SchedulerParticipant {
				found = true
			}
			if states[prior] != "completed" {
				return fault(f.InvalidState)
			}
		}
		if !found {
			return fault(f.DependencyUnbound)
		}
		return nil
	}
	return fault(f.DependencyUnbound)
}

var _ ec.ProjectExecutionCleanupAuthority = (*ExecutionProjectRetirementAuthority)(nil)
