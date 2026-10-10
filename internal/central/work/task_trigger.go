package work

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// TaskTrigger reads Work-owned source material. It deliberately does not
// implement TriggerProvider: assignment/transition and Scheduler launch-intent
// writers are separate required authorities. This slice never issues a permit.
type TaskTrigger struct{ state *taskTriggerState }
type taskTriggerState struct {
	store     Store
	authority *Authority
	captures  ec.TriggerCaptureAuthority
}

func NewTaskTrigger(store Store, authority *Authority, captures ec.TriggerCaptureAuthority) (*TaskTrigger, error) {
	if nilPort(store) || authority.state() == nil || !sameStore(store, authority.state().store) {
		return nil, fault(f.DependencyUnbound)
	}
	// A missing Execution provider allows ordinary Human observations only.
	if nilPort(captures) {
		captures = nil
	}
	return &TaskTrigger{&taskTriggerState{store, authority, captures}}, nil
}
func taskTriggerError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return portError(err)
}
func (p *TaskTrigger) valid(ctx context.Context) error {
	if p == nil || p.state == nil {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil {
		return fault(f.InvalidArgument)
	}
	return ctx.Err()
}

// ReadTaskInput is an ordinary current Owner observation. A backlog Task is a
// valid observation and is never upgraded to a launch/capture permission.
func (p *TaskTrigger) ReadTaskInput(ctx context.Context, actor i.Actor, project c.ProjectID, id c.TaskID, purpose string) (TaskTriggerInput, error) {
	if err := p.valid(ctx); err != nil {
		return TaskTriggerInput{}, err
	}
	if err := readInput(ctx, actor, project); err != nil {
		return TaskTriggerInput{}, taskTriggerError(err)
	}
	if id.Validate() != nil || purpose != "task/work" && purpose != "task/review" {
		return TaskTriggerInput{}, fault(f.InvalidArgument)
	}
	locks, err := oc.NormalizeLocks([]f.LockRequest{userLock(actor.Details().UserID, f.Shared), projectLock(project, f.Shared), taskScheduleLock(project, f.Shared), taskLock(id.String(), f.Shared)})
	if err != nil {
		return TaskTriggerInput{}, taskTriggerError(err)
	}
	cause, err := readCause("work.task-trigger.read")
	if err != nil {
		return TaskTriggerInput{}, taskTriggerError(err)
	}
	var input TaskTriggerInput
	result := p.state.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := p.state.store.AcquireAll(ctx, tx, locks); err != nil {
			return taskTriggerError(err)
		}
		x, err := p.state.store.InTx(tx)
		if err != nil {
			return taskTriggerError(err)
		}
		access, err := p.state.authority.state().projects.RequireOwnerInTx(ctx, tx, actor, project, i.Read)
		if err != nil {
			return taskTriggerError(err)
		}
		if !access.Matches(actor, project) {
			return fault(f.Forbidden)
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		input, err = loadTaskTriggerInput(ctx, x, access.Project(), id, purpose)
		return taskTriggerError(err)
	})
	if err = txError(result); err != nil {
		return TaskTriggerInput{}, err
	}
	if err = ctx.Err(); err != nil {
		return TaskTriggerInput{}, err
	}
	if input.Validate() != nil {
		return TaskTriggerInput{}, internal(nil)
	}
	return input, nil
}

type taskCapturePlan struct {
	owner    *taskTriggerState
	request  ec.PreparationRequest
	inputID  ec.TriggerInputID
	locks    []f.LockRequest
	mu       sync.Mutex
	consumed bool
}

func (p *taskCapturePlan) RequiredLocks() []f.LockRequest {
	if p == nil {
		return nil
	}
	return slices.Clone(p.locks)
}
func taskCaptureRequest(execution i.ExecutionID, request ec.LaunchRequest) (ec.PreparationRequest, error) {
	original := ec.PreparationRequest{ExecutionID: execution, Launch: request.Clone()}
	if original.Validate() != nil {
		return ec.PreparationRequest{}, fault(f.InvalidArgument)
	}
	if request.Trigger.Kind != "task" {
		return ec.PreparationRequest{}, fault(f.CapabilityUnsupported)
	}
	// Constraints and retry/regenerate semantics need the real source-policy
	// interpreter. Their presence cannot be ignored or treated as an empty list.
	if len(request.Policy.AllowedResourceConstraints) != 0 || request.Lineage.RetryOf != nil || request.Lineage.RegenerateOf != nil {
		return ec.PreparationRequest{}, fault(f.DependencyUnbound)
	}
	return original, nil
}

// DiscoverCapture performs no protected reads. The fixed Work lock protocol
// covers placement and children; the first Task version is read under this
// union only after the Execution owner proves the original preparing call.
func (p *TaskTrigger) DiscoverCapture(ctx context.Context, execution i.ExecutionID, request ec.LaunchRequest) (ec.CapturePlan, error) {
	if err := p.valid(ctx); err != nil {
		return nil, err
	}
	original, err := taskCaptureRequest(execution, request)
	if err != nil {
		return nil, err
	}
	if p.state.captures == nil {
		return nil, fault(f.DependencyUnbound)
	}
	agent, _ := f.AggregateLock(f.AgentAggregate, request.AgentID.String())
	run, _ := f.AggregateLock(f.ExecutionAggregate, execution.String())
	locks, err := oc.NormalizeLocks([]f.LockRequest{projectLock(request.ProjectID, f.Shared), taskScheduleLock(request.ProjectID, f.Shared), taskLock(request.Trigger.TaskID, f.Shared), {Key: agent, Mode: f.Shared}, {Key: run, Mode: f.Exclusive}})
	if err != nil {
		return nil, taskTriggerError(err)
	}
	input, err := f.NewID[ec.TriggerInput]()
	if err != nil {
		return nil, taskTriggerError(err)
	}
	return &taskCapturePlan{owner: p.state, request: original, inputID: input, locks: locks}, nil
}
func (p *TaskTrigger) CaptureInputInTx(ctx context.Context, tx f.Tx, execution i.ExecutionID, request ec.LaunchRequest, plan ec.CapturePlan) (ec.CapturedTriggerInput, error) {
	if err := p.valid(ctx); err != nil {
		return ec.CapturedTriggerInput{}, err
	}
	original, err := taskCaptureRequest(execution, request)
	if err != nil {
		return ec.CapturedTriggerInput{}, err
	}
	if p.state.captures == nil {
		return ec.CapturedTriggerInput{}, fault(f.DependencyUnbound)
	}
	selected, ok := plan.(*taskCapturePlan)
	if !ok || selected == nil || selected.owner != p.state || !selected.request.Equal(original) {
		return ec.CapturedTriggerInput{}, fault(f.Forbidden)
	}
	selected.mu.Lock()
	if selected.consumed {
		selected.mu.Unlock()
		return ec.CapturedTriggerInput{}, fault(f.Forbidden)
	}
	selected.consumed = true
	selected.mu.Unlock()
	x, err := p.state.store.InTx(tx)
	if err != nil {
		return ec.CapturedTriggerInput{}, taskTriggerError(err)
	}
	if err = p.state.store.RequireHeldLocks(ctx, tx, selected.locks); err != nil {
		return ec.CapturedTriggerInput{}, taskTriggerError(err)
	}
	project, err := p.state.captures.RequireTriggerCaptureInTx(ctx, tx, execution, original.Launch.Clone())
	if err != nil {
		return ec.CapturedTriggerInput{}, taskTriggerError(err)
	}
	if err = ctx.Err(); err != nil {
		return ec.CapturedTriggerInput{}, err
	}
	if project.Validate() != nil || project.ID != request.ProjectID || project.Lifecycle != pc.Active {
		return ec.CapturedTriggerInput{}, fault(f.Forbidden)
	}
	id, err := f.ParseID[c.Task](request.Trigger.TaskID)
	if err != nil {
		return ec.CapturedTriggerInput{}, fault(f.InvalidArgument)
	}
	input, err := loadTaskTriggerInput(ctx, x, project, id, request.Purpose)
	if err != nil {
		return ec.CapturedTriggerInput{}, taskTriggerError(err)
	}
	if err = taskCaptureEligible(input, request); err != nil {
		return ec.CapturedTriggerInput{}, err
	}
	binding, _ := request.Digest()
	raw, err := json.Marshal(capturedTaskWire{1, execution, binding, input.data()})
	if err != nil {
		return ec.CapturedTriggerInput{}, internal(nil)
	}
	if err = ctx.Err(); err != nil {
		return ec.CapturedTriggerInput{}, err
	}
	return ec.NewCapturedTriggerInput(ec.TriggerInputRef{ProviderType: "task", SchemaVersion: 1, InputID: selected.inputID, Digest: ec.TriggerInputDigest(raw)}, raw)
}
func taskCaptureEligible(input TaskTriggerInput, request ec.LaunchRequest) error {
	if input.Validate() != nil || request.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	task := input.Task()
	if task.ProjectID != request.ProjectID || task.ID.String() != request.Trigger.TaskID || input.Purpose() != request.Purpose || task.AssigneeAgentID == nil || *task.AssigneeAgentID != request.AgentID || input.Sprint().State != c.Current || len(input.UnresolvedBlockers()) != 0 {
		return fault(f.InvalidState)
	}
	if request.Purpose == "task/work" && task.State != c.TaskStateInProgress || request.Purpose == "task/review" && task.State != c.TaskStateInReview {
		return fault(f.InvalidState)
	}
	return nil
}

var _ ec.TriggerCaptureProvider = (*TaskTrigger)(nil)
