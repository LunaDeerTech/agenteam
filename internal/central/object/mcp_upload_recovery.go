package object

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func mcpUploadRecoveryRequest(ctx context.Context, cause oc.ObjectCleanupCause, key f.IdempotencyKey, process oc.ProcessID) error {
	if cause.Validate() != nil || cause.Details().Reason != oc.AbandonedAttempt || process.Validate() != nil {
		return invalid()
	}
	return mcpUploadRequest(ctx, cause.Details().Owner, key)
}

func sameMCPUpload(a, b oc.MCPUploadMapping) bool {
	return a.Owner.Equal(b.Owner) && a.Key == b.Key && a.UploadID == b.UploadID && a.ObjectID == b.ObjectID &&
		a.AttemptID == b.AttemptID && a.ProcessID == b.ProcessID && a.AgentID == b.AgentID && a.ExecutionID == b.ExecutionID &&
		a.CreationCause == b.CreationCause && a.State == b.State && a.Disposition == b.Disposition
}

// JoinMCPUploadWithinBudget never infers process death from age or missing rows.
// Both its cleanup gate and joinedAttempt consume the original domain's live
// access proof. The existing joinedAttempt performs the bounded, exact writer
// lease release; it cannot publish a receipt or release another Object lease.
func (s *Service) JoinMCPUploadWithinBudget(ctx context.Context, cause oc.ObjectCleanupCause, key f.IdempotencyKey, process oc.ProcessID) error {
	if err := mcpUploadRecoveryRequest(ctx, cause, key, process); err != nil {
		return err
	}
	if s == nil || s.data == nil || s.state() == nil {
		return failure(f.DependencyUnbound, nil)
	}
	r := s.state()
	g, ok := r.auth.Processes.(*ProcessGuard)
	if !ok || g == nil || r.runtime == nil || r.runtime.state().service != s || r.runtime.state().guard != g || nilPort(r.auth.Cleanup) || nilPort(r.auth.Planner) {
		return failure(f.DependencyUnbound, nil)
	}
	op, finish, err := s.begin(context.WithValue(ctx, runtimeRecoveryBudgetKey{}, true))
	if err != nil {
		return err
	}
	defer finish()
	ctx = op.ctx
	current, err := g.CurrentProcess()
	if err != nil {
		return err
	}
	if current != r.process || current == process {
		return failure(f.ResourceBusy, nil)
	}
	original, found, err := mcpUploadMapping(ctx, r.store, cause.Details().Owner, key)
	if err != nil {
		return err
	}
	if !found {
		return failure(f.NotFound, nil)
	}
	if original.ProcessID != process || original.AttemptID.Validate() != nil || original.Disposition != "revoked" || cause.Details().OperationID.String() != original.UploadID.String() {
		return failure(f.Forbidden, nil)
	}
	if err = g.ConfirmStopped(ctx, process); err != nil {
		return err
	}
	request, err := oc.NewObjectCleanupAccess(oc.AccessRequestDetails{Operation: oc.CleanupObjectAccess, Cleanup: cause, ObjectID: original.ObjectID})
	if err != nil {
		return invalid()
	}
	result := s.withinAccess(ctx, recoveryCause(), request, func(ctx context.Context, tx f.Tx, _ oc.AccessLockPlan, _ oc.LockedAccess) error {
		if err := g.RequireCurrentProcessInTx(ctx, tx, current); err != nil {
			return err
		}
		if err := r.auth.Cleanup.CheckCleanupInTx(ctx, tx, cause, original.ObjectID); err != nil {
			return portError(err)
		}
		x, err := executor(s, tx)
		if err != nil {
			return err
		}
		actual, found, err := mcpUploadMapping(ctx, x, cause.Details().Owner, key)
		if err != nil {
			return err
		}
		if !found || !sameMCPUpload(actual, original) {
			return failure(f.Forbidden, nil)
		}
		a, found, err := loadAttempt(ctx, x, actual.AttemptID)
		if err != nil {
			return err
		}
		if !found || !a.cleaning || a.process != process || a.upload != actual.UploadID || a.object != actual.ObjectID {
			return failure(f.Forbidden, nil)
		}
		return nil
	})
	if err = commitError(result); err != nil {
		return err
	}
	return s.joinedAttempt(ctx, original.AttemptID, process)
}

var _ oc.MCPUploadRecovery = (*Service)(nil)
