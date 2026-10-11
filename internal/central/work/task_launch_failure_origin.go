package work

import (
	"context"
	"reflect"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func cloneTaskRelaunchRecord(r *taskRelaunchRecord) *taskRelaunchRecord {
	if r == nil {
		return nil
	}
	v := *r
	v.Task = r.Task.Clone()
	v.Sprint = r.Sprint.Clone()
	return &v
}
func taskFailureBaseline(r c.TaskLaunchFailureRequest, facts c.TaskLaunchFailureFacts, claim schedulerClaimRecord, relaunch *taskRelaunchRecord) (c.Task, error) {
	if facts.ValidateFor(r) != nil {
		return c.Task{}, internal(nil)
	}
	if r.Relaunch == nil {
		if relaunch != nil || validateSchedulerClaimRecord(&claim) != nil || claim.Request != r.Claim || !sameValue(claim.Guard, facts.Guard) {
			return c.Task{}, internal(nil)
		}
		return claim.After.Clone(), nil
	}
	if relaunch == nil || !reflect.ValueOf(claim).IsZero() || validateTaskRelaunchRecord(relaunch) != nil || relaunch.Request != *r.Relaunch || facts.Relaunch == nil || relaunch.source() != *facts.Relaunch {
		return c.Task{}, internal(nil)
	}
	return relaunch.Task.Clone(), nil
}

func sameTaskFailureOrigin(claim schedulerClaimRecord, relaunch *taskRelaunchRecord, record taskFailureRecord) bool {
	if relaunch != nil {
		return reflect.ValueOf(claim).IsZero() && reflect.ValueOf(record.Claim).IsZero() && record.Relaunch != nil && sameValue(relaunch, record.Relaunch)
	}
	return record.Relaunch == nil && sameValue(claim, record.Claim)
}
func failureDiscoveryLocks(r c.TaskLaunchFailureRequest) ([]f.LockRequest, error) {
	if r.Relaunch != nil {
		return taskRelaunchLocks(*r.Relaunch)
	}
	return claimDiscoveryLocks(r.Claim)
}
func (r taskFailureRecord) event(events c.TaskLaunchFailureEvents) (event.Event, error) {
	if !r.Changed || r.Header == nil {
		return event.Event{}, fault(f.Forbidden)
	}
	if r.Relaunch != nil {
		if r.Event != nil || r.RelaunchEvent == nil {
			return event.Event{}, fault(f.Forbidden)
		}
		return events.NewTaskRelaunchFailed(*r.Header, *r.RelaunchEvent)
	}
	if r.Event == nil || r.RelaunchEvent != nil {
		return event.Event{}, fault(f.Forbidden)
	}
	return events.NewTaskLaunchFailed(*r.Header, *r.Event)
}
func verifyTaskFailureOrigin(ctx context.Context, x postgres.SQLExecutor, r *taskFailureRecord) error {
	if r.Relaunch != nil {
		actual, err := loadTaskRelaunch(ctx, x, r.Before.ProjectID, r.Request.DispatchID())
		if err != nil {
			return err
		}
		if actual == nil || !sameValue(actual, r.Relaunch) {
			return fault(f.Forbidden)
		}
	} else {
		actual, err := loadSchedulerClaim(ctx, x, r.Before.ProjectID, r.Request.DispatchID())
		if err != nil {
			return err
		}
		if actual == nil || !sameValue(*actual, r.Claim) {
			return fault(f.Forbidden)
		}
	}
	return ctx.Err()
}
