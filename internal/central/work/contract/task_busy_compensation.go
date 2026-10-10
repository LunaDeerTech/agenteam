package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// TaskBusyCompensationRequest identifies one durably confirmed AgentBusy
// outcome. These fields are data, never proof of that outcome or permission to
// change a Task. Claim is the original, immutable claim request.
type TaskBusyCompensationRequest struct {
	Claim           TaskClaimRequest
	DispatchVersion f.Version
	LaunchAttempt   int64
}

func (v TaskBusyCompensationRequest) Validate() error {
	if v.Claim.Validate() != nil || v.DispatchVersion.Validate() != nil || v.LaunchAttempt <= 0 {
		return invalid("", "INVALID_TASK_BUSY_COMPENSATION")
	}
	return nil
}
func (v TaskBusyCompensationRequest) Clone() TaskBusyCompensationRequest { return v }
func (TaskBusyCompensationRequest) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "task_busy_compensation")
}
func (TaskBusyCompensationRequest) LogValue() slog.Value {
	return slog.StringValue("task_busy_compensation")
}

// Plans and applied values are private concrete values of the Work writer.
// Restored is only a result; interface satisfaction cannot authorize a write.
type TaskBusyCompensationPlan interface{ RequiredLocks() []f.LockRequest }
type AppliedTaskBusyCompensation interface{ Restored() bool }

type SchedulerTaskBusyCompensations interface {
	DiscoverTaskBusyCompensation(context.Context, i.Actor, TaskBusyCompensationRequest) (TaskBusyCompensationPlan, error)
	ApplyTaskBusyCompensationInTx(context.Context, f.Tx, i.Actor, TaskBusyCompensationRequest, TaskBusyCompensationPlan) (AppliedTaskBusyCompensation, error)
	CheckTaskBusyCompensationAppliedInTx(context.Context, f.Tx, i.Actor, TaskBusyCompensationRequest, TaskBusyCompensationPlan, AppliedTaskBusyCompensation) error
}

// Scheduler verifies its private discovery/applying call, the live original
// transaction, complete held locks, exact pending Dispatch and persisted busy
// attempt. It returns the canonical guard, which Work compares with its own
// immutable claim. Final proof also binds the original plan and rejects other
// pending claims in the protected source group; only this Dispatch is exempt.
// A paused Scheduler retains its confirmed-busy pending fact and rejects both
// restoration and preservation/settlement until scheduling resumes.
type SchedulerTaskBusyAuthority interface {
	RequireTaskBusyCompensationDiscoveryInTx(context.Context, f.Tx, i.Actor, TaskBusyCompensationRequest) (TaskClaimGuard, error)
	RequireTaskBusyCompensationInTx(context.Context, f.Tx, i.Actor, TaskBusyCompensationRequest, TaskBusyCompensationPlan) (TaskClaimGuard, error)
}
