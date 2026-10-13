package work

import (
	"context"
	"errors"
	"fmt"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"reflect"
	"testing"
)

type completeMemberRows struct {
	count, next, closed int
	mutate              func(int, *string, *c.TaskState)
	scanErr, tailErr    error
}

func (r *completeMemberRows) Next() bool {
	if r.next >= r.count {
		return false
	}
	r.next++
	return true
}
func (r *completeMemberRows) Scan(v ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	id := fmt.Sprintf("01900000-0000-7000-8000-%012x", r.next)
	state := c.TaskStateDone
	if r.next%2 == 0 {
		state = c.TaskStateCancelled
	}
	if r.mutate != nil {
		r.mutate(r.next, &id, &state)
	}
	*v[0].(*string), *v[1].(*c.TaskState) = id, state
	return nil
}
func (r *completeMemberRows) Err() error { return r.tailErr }
func (r *completeMemberRows) Close()     { r.closed++ }

func TestSprintCompleteFullMembershipScan(t *testing.T) {
	for _, count := range []int{0, 501, 65536} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			rows := &completeMemberRows{count: count}
			ids, err := collectSprintCompletionMembers(context.Background(), rows)
			if err != nil || ids == nil || len(ids) != count || rows.next != count || rows.closed != 1 {
				t.Fatal("full terminal membership or original rows tail lost", count, err)
			}
		})
	}
	for _, name := range []string{"unfinished-last", "bad-id", "bad-state", "duplicate", "too-many", "scan-failure", "tail-failure", "cancelled"} {
		t.Run(name, func(t *testing.T) {
			rows := &completeMemberRows{count: 501}
			ctx := context.Background()
			switch name {
			case "unfinished-last":
				rows.mutate = func(n int, _ *string, s *c.TaskState) {
					if n == 501 {
						*s = c.TaskStateBacklog
					}
				}
			case "bad-id":
				rows.mutate = func(_ int, id *string, _ *c.TaskState) { *id = "invalid" }
			case "bad-state":
				rows.mutate = func(_ int, _ *string, s *c.TaskState) { *s = c.TaskState("unknown") }
			case "duplicate":
				rows.mutate = func(_ int, id *string, _ *c.TaskState) { *id = "01900000-0000-7000-8000-000000000001" }
			case "too-many":
				rows.count = 65537
			case "scan-failure":
				rows.scanErr = errors.New("controlled scan")
			case "tail-failure":
				rows.tailErr = errors.New("controlled tail")
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			ids, err := collectSprintCompletionMembers(ctx, rows)
			if err == nil || ids != nil || rows.closed != 1 {
				t.Fatal("failed full scan became an empty success", name, err)
			}
			switch name {
			case "unfinished-last":
				pureCode(t, err, f.InvalidState)
				if rows.next != 501 {
					t.Fatal("truncated membership")
				}
			case "too-many":
				pureCode(t, err, f.PayloadTooLarge)
				if rows.next != 65537 {
					t.Fatal("member limit was not reached")
				}
			case "duplicate":
				pureCode(t, err, f.InternalError)
				if rows.next != 2 {
					t.Fatal("duplicate test did not reach the second valid ID")
				}
			case "bad-id", "bad-state":
				pureCode(t, err, f.InternalError)
			case "scan-failure", "tail-failure", "cancelled":
				pureCode(t, err, f.DependencyUnavailable)
				if name == "tail-failure" && rows.next != 501 {
					t.Fatal("row error test did not reach the full tail")
				}
			}
		})
	}
}

type completeOccupancyPort struct {
	fn func(context.Context, f.Tx, i.ProjectID, []string) (ec.ExecutionOccupancy, error)
}

func (p completeOccupancyPort) ReadInTx(ctx context.Context, tx f.Tx, id i.ProjectID, ids []string) (ec.ExecutionOccupancy, error) {
	return p.fn(ctx, tx, id, ids)
}

type completePendingPort struct {
	fn func(context.Context, f.Tx, i.ProjectID, []string) (ec.DispatchOccupancy, error)
}

func (p completePendingPort) ReadInTx(ctx context.Context, tx f.Tx, id i.ProjectID, ids []string) (ec.DispatchOccupancy, error) {
	return p.fn(ctx, tx, id, ids)
}
func TestSprintCompleteOriginalTransactionOccupancy(t *testing.T) {
	for _, mode := range []string{"idle", "active", "pending", "execution-error", "pending-error", "nil-execution", "nil-pending"} {
		t.Run(mode, func(t *testing.T) {
			tx, project := f.NewTx(), pureID[i.Project](t, 3)
			ctx := context.Background()
			ids := []string{pureID[c.Task](t, 4).String(), pureID[c.Task](t, 5).String()}
			executions, dispatches := 0, 0
			check := func(got context.Context, actual f.Tx, p i.ProjectID, tasks []string) {
				if got != ctx || actual != tx || p != project || !reflect.DeepEqual(tasks, ids) {
					t.Fatal("occupancy escaped original transaction/full membership")
				}
			}
			service := &SprintLifecycleService{completion: &SprintCompletionDependencies{
				Occupancy: completeOccupancyPort{fn: func(got context.Context, actual f.Tx, p i.ProjectID, tasks []string) (ec.ExecutionOccupancy, error) {
					check(got, actual, p, tasks)
					executions++
					if mode == "execution-error" {
						return ec.ExecutionOccupancy{}, f.NewFault(f.DependencyUnavailable, f.NotStarted)
					}
					if mode == "nil-execution" {
						return ec.ExecutionOccupancy{}, nil
					}
					value := ec.ExecutionOccupancy{Active: []ec.ActiveTaskExecution{}, HistoryTaskIDs: []string{}}
					if mode == "active" {
						value.Active = []ec.ActiveTaskExecution{{TaskID: ids[1], ExecutionID: pureID[i.Execution](t, 6), AgentID: pureID[i.Agent](t, 7), Status: ec.Running}}
					}
					return value, nil
				}},
				Pending: completePendingPort{fn: func(got context.Context, actual f.Tx, p i.ProjectID, tasks []string) (ec.DispatchOccupancy, error) {
					check(got, actual, p, tasks)
					dispatches++
					if mode == "pending-error" {
						return ec.DispatchOccupancy{}, f.NewFault(f.DependencyUnavailable, f.NotStarted)
					}
					if mode == "nil-pending" {
						return ec.DispatchOccupancy{}, nil
					}
					value := ec.DispatchOccupancy{Pending: []ec.PendingTaskDispatch{}, HistoryTaskIDs: []string{}}
					if mode == "pending" {
						value.Pending = []ec.PendingTaskDispatch{{TaskID: ids[0], DispatchID: pureID[c.Task](t, 8).String(), AgentID: pureID[i.Agent](t, 7), SprintID: pureID[pc.Sprint](t, 9).String()}}
					}
					return value, nil
				}},
			}}
			err := service.checkCompletionOccupancy(ctx, tx, project, ids)
			if executions != 1 || dispatches > 1 || (err == nil) != (mode == "idle") {
				t.Fatal("occupancy outcome", mode, err)
			}
			if mode == "active" || mode == "pending" {
				pureCode(t, err, f.ResourceBusy)
			}
			if mode == "nil-execution" || mode == "nil-pending" {
				pureCode(t, err, f.InternalError)
			}
		})
	}
}
