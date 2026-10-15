package execution

import (
	"context"
	"testing"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestPreparationMountLocksForTaskAndMeeting(t *testing.T) {
	for _, source := range []string{"task", "meeting"} {
		t.Run(source, func(t *testing.T) {
			x := newLaunchControl(t)
			request := c.PreparationRequest{ExecutionID: newTestID[i.Execution](t), Launch: x.request.Clone()}
			if source == "meeting" {
				request.Launch.Trigger = c.Trigger{Kind: "meeting", MeetingID: newTestID[struct{}](t).String(), TurnID: newTestID[struct{}](t).String(), ContributionID: newTestID[struct{}](t).String(), ParticipantID: newTestID[struct{}](t).String()}
				request.Launch.Purpose = "meeting/response"
				generation, attempt := f.Version(1), f.Version(1)
				request.Launch.Lineage = c.Lineage{ContributionGeneration: &generation, ContributionAttempt: &attempt}
			}
			if err := request.Validate(); err != nil {
				t.Fatal(err)
			}
			locks, err := preparationLocks(request)
			if err != nil {
				t.Fatal(err)
			}
			// Independent provider budget: Mount capture/material verification
			// needs these same locks even when no new capture plan is discovered.
			project, _ := f.ProjectLock(request.Launch.ProjectID.String())
			agent, _ := f.AgentLock(request.Launch.AgentID.String())
			execution, _ := f.AggregateLock(f.ExecutionAggregate, request.ExecutionID.String())
			schedule, _ := f.ProjectScheduleLock(request.Launch.ProjectID.String())
			required := []f.LockRequest{{Key: project, Mode: f.Shared}, {Key: agent, Mode: f.Shared}, {Key: execution, Mode: f.Exclusive}, {Key: schedule, Mode: f.Exclusive}}
			command, _ := request.Launch.Command()
			required = append(required, commandLock(command))
			cause, _ := f.NewRecoveryCause("execution.mount-locks", request.ExecutionID.String(), request.Launch.Meta.RequestID.String())
			check := func(budget []f.LockRequest) f.CommitResult {
				return x.store.WithinTx(context.Background(), cause, func(ctx context.Context, tx f.Tx) error {
					if err := x.store.AcquireAll(ctx, tx, budget); err != nil {
						return err
					}
					return x.store.RequireHeldLocks(ctx, tx, required)
				})
			}
			if result := check(locks); result.State() != f.Committed {
				t.Fatal("source preparation budget omitted Mount or original command gate", result)
			}
			withoutSchedule := make([]f.LockRequest, 0, len(locks))
			for _, lock := range locks {
				if lock.Key.Canonical() != schedule.Canonical() {
					withoutSchedule = append(withoutSchedule, lock)
				}
			}
			if result := check(withoutSchedule); result.State() != f.NotCommitted {
				t.Fatal("missing schedule lock escaped the original provider gate", result)
			}
		})
	}
}
