package execution

import (
	"context"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// DispatchObservation reads only Execution-owned facts. The domain caller
// checks its real authority before invoking this trusted transaction adapter.
// In particular it does not recursively invoke ServiceProjectAccess.
type DispatchObservation struct{ store Store }

func NewDispatchObservation(store Store) (*DispatchObservation, error) {
	if nilPort(store) {
		return nil, fault(f.DependencyUnbound)
	}
	return &DispatchObservation{store: store}, nil
}

func dispatchObservationLocks(project i.ProjectID, agent i.AgentID) []f.LockRequest {
	key, _ := f.ProjectScheduleLock(project.String())
	return []f.LockRequest{projectLock(project), {Key: key, Mode: f.Exclusive}, agentLock(agent, f.Shared)}
}

func (r *DispatchObservation) executor(ctx context.Context, tx f.Tx, locks []f.LockRequest) (postgres.SQLExecutor, error) {
	if r == nil || nilPort(r.store) {
		return nil, fault(f.DependencyUnbound)
	}
	x, err := r.store.InTx(tx)
	if err != nil {
		return nil, portError(err)
	}
	if nilPort(x) {
		return nil, fault(f.DependencyUnbound)
	}
	locks, err = oc.NormalizeLocks(locks)
	if err != nil {
		return nil, portError(err)
	}
	if err = r.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return nil, portError(err)
	}
	return x, ctx.Err()
}

func (r *DispatchObservation) LookupLaunchInTx(ctx context.Context, tx f.Tx, key c.LaunchLookupKey, expected f.Digest, dispatchID string) (c.LaunchLookup, error) {
	if ctx == nil {
		return c.LaunchLookup{}, invalid()
	}
	if err := ctx.Err(); err != nil {
		return c.LaunchLookup{}, err
	}
	if key.Validate() != nil || expected.Validate() != nil {
		return c.LaunchLookup{}, invalid()
	}
	if _, err := f.ParseID[struct{}](dispatchID); err != nil {
		return c.LaunchLookup{}, invalid()
	}
	command, _ := key.Command()
	x, err := r.executor(ctx, tx, append(dispatchObservationLocks(key.ProjectID, key.AgentID), commandLock(command)))
	if err != nil {
		return c.LaunchLookup{}, err
	}
	row, err := loadLaunch(ctx, x, key)
	// The original SQL call has returned before cancellation is observed. No
	// cancelled or failed read returns a partial association or a free result.
	if ctx.Err() != nil {
		return c.LaunchLookup{}, ctx.Err()
	}
	if err != nil {
		return c.LaunchLookup{}, err
	}
	if row == nil {
		return c.LaunchLookup{}, nil
	}
	if row.summary.ProjectID != key.ProjectID || row.summary.AgentID != key.AgentID || row.launch.Meta.IdempotencyKey != key.IdempotencyKey {
		return c.LaunchLookup{}, unavailable(nil)
	}
	if row.digest != expected {
		return c.LaunchLookup{}, fault(f.IdempotencyKeyReused)
	}
	if row.launch.Trigger.Kind != "task" || row.launch.Lineage.DispatchID != dispatchID {
		return c.LaunchLookup{}, fault(f.ConfirmationStale)
	}
	value := row.summary.Clone()
	return c.LaunchLookup{Found: true, RequestDigest: row.digest, Execution: &value}, nil
}

// The global active-Agent unique index is the canonical slot. No trigger,
// Scheduler linkage, Task state, or cancellation filter may narrow this read.
const agentSlotObservationSQL = `SELECT ` + executionColumns + ` FROM agenteam_execution.executions WHERE agent_id=$1 AND status IN ('created','preparing','running','waiting')`

func (r *DispatchObservation) AgentSlotInTx(ctx context.Context, tx f.Tx, project i.ProjectID, agent i.AgentID) (c.AgentSlotObservation, error) {
	if ctx == nil {
		return c.AgentSlotObservation{}, invalid()
	}
	if err := ctx.Err(); err != nil {
		return c.AgentSlotObservation{}, err
	}
	if project.Validate() != nil || agent.Validate() != nil {
		return c.AgentSlotObservation{}, invalid()
	}
	x, err := r.executor(ctx, tx, dispatchObservationLocks(project, agent))
	if err != nil {
		return c.AgentSlotObservation{}, err
	}
	row, err := scanExecution(x.QueryRow(ctx, agentSlotObservationSQL, agent.String()))
	if ctx.Err() != nil {
		return c.AgentSlotObservation{}, ctx.Err()
	}
	if err != nil {
		return c.AgentSlotObservation{}, err
	}
	if row == nil {
		return c.AgentSlotObservation{}, nil
	}
	if row.summary.ProjectID != project || row.summary.AgentID != agent || row.summary.Status.Terminal() {
		return c.AgentSlotObservation{}, unavailable(nil)
	}
	s := row.summary.Clone()
	return c.AgentSlotObservation{Occupied: true, ExecutionID: &s.ID, Status: s.Status, CancelRequestedAt: s.CancelRequestedAt}, nil
}

var _ c.DispatchObserver = (*DispatchObservation)(nil)
