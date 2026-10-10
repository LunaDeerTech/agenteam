package execution

import (
	"context"
	"slices"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func (r *DispatchObservation) CountAssociatedInTx(ctx context.Context, tx f.Tx, project i.ProjectID, associated []c.AssociatedDispatch) (int64, error) {
	if ctx == nil {
		return 0, invalid()
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if project.Validate() != nil || associated == nil {
		return 0, invalid()
	}
	if len(associated) > c.MaxDispatchCapacityBatch {
		return 0, fault(f.PayloadTooLarge)
	}
	batch := slices.Clone(associated)
	executions := make(map[i.ExecutionID]struct{}, len(batch))
	dispatches := make(map[string]struct{}, len(batch))
	for _, item := range batch {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if item.ExecutionID.Validate() != nil || item.AgentID.Validate() != nil || item.Key.Validate() != nil || item.Digest.Validate() != nil {
			return 0, invalid()
		}
		if _, err := f.ParseID[struct{}](item.DispatchID); err != nil {
			return 0, invalid()
		}
		if _, exists := executions[item.ExecutionID]; exists {
			return 0, invalid()
		}
		if _, exists := dispatches[item.DispatchID]; exists {
			return 0, invalid()
		}
		executions[item.ExecutionID], dispatches[item.DispatchID] = struct{}{}, struct{}{}
	}
	schedule, _ := f.ProjectScheduleLock(project.String())
	// These are observations of already linked Task Executions, not current
	// Agent authorization or an uncommitted Launch lookup. Existing Task Launch
	// and preparation status writes hold this same gate. Future Task lifecycle
	// writers must preserve it, including waiting/resume/terminal transitions.
	x, err := r.executor(ctx, tx, []f.LockRequest{projectLock(project), {Key: schedule, Mode: f.Exclusive}})
	if err != nil {
		return 0, err
	}
	var count int64
	for _, item := range batch {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		row, err := loadExecution(ctx, x, item.ExecutionID)
		if cancelled := ctx.Err(); cancelled != nil {
			return 0, cancelled
		}
		if err != nil {
			return 0, err
		}
		if row == nil {
			return 0, unavailable(nil)
		}
		if row.summary.ID != item.ExecutionID || row.summary.ProjectID != project || row.summary.AgentID != item.AgentID || row.launch.Meta.IdempotencyKey != item.Key || row.digest != item.Digest || row.launch.Trigger.Kind != "task" || row.launch.Lineage.DispatchID != item.DispatchID {
			return 0, fault(f.ConfirmationStale)
		}
		switch row.summary.Status {
		case c.Created, c.Preparing, c.Running:
			count++
		}
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return count, nil
}

var _ c.DispatchCapacityReader = (*DispatchObservation)(nil)
