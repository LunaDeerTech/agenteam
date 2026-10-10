package work

import (
	"context"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

const TaskWorkPromptRevision = "agenteam/task-work/v1"
const TaskReviewPromptRevision = "agenteam/task-review/v1"

const taskWorkInstructions = `Work on the captured Task in its captured Sprint and Milestone. The Task's title, description, plan, current state, assignee, version, unresolved blockers and bounded recent history are fixed input for this execution, not a live view of subsequent changes.

Use only the tools and resources actually supplied to this execution. A Task capability mentioned here does not make an absent tool available. Request Task state changes through an available Task transition tool; ordinary model text does not change Task state. Use the captured Task version for the first mutation. If a version conflict occurs, read the current Task through an available Task read tool and reconsider the mutation instead of blindly repeating it.

Follow the Task plan and report material limitations accurately. Use an available Task history tool for earlier events when needed; the captured recent history is only a bounded suffix. Do not infer a successful mutation from an uncertain outcome or invent current Task facts when a required tool is unavailable.
`

const taskReviewInstructions = `Review the captured Task in its captured Sprint and Milestone. The Task's title, description, plan, current state, assignee, version, unresolved blockers and bounded recent history are fixed input for this execution, not a live view of subsequent changes.

Evaluate the work against the captured requirements and record actionable findings through tools actually supplied to this execution. Request any Task state change through an available Task transition tool; review text alone does not approve or transition a Task. Use the captured Task version for the first mutation. If a version conflict occurs, read the current Task through an available Task read tool and reconsider the mutation instead of blindly repeating it.

Use an available Task history tool for earlier events when needed. A referenced capability does not authorize an absent tool, and unavailable evidence is not a successful review. Report limitations and uncertain outcomes without inventing facts or repeating a mutation whose outcome is unknown.
`

// TaskContextBuilder has no Store. It decodes the original captured Work input
// and adds only fixed, versioned scenario instructions. It cannot produce a
// Launch permit, read a newer Task, or advance an Execution into running.
type TaskContextBuilder struct{}

func taskContextInstructions(purpose string) (ec.PromptComponent, error) {
	switch purpose {
	case "task/work":
		return ec.NewPromptComponent(TaskWorkPromptRevision, taskWorkInstructions)
	case "task/review":
		return ec.NewPromptComponent(TaskReviewPromptRevision, taskReviewInstructions)
	default:
		return ec.PromptComponent{}, fault(f.CapabilityUnsupported)
	}
}
func (TaskContextBuilder) BuildTriggerContext(ctx context.Context, request ec.PreparationRequest, captured ec.CapturedTriggerInput) (ec.TriggerContext, error) {
	var zero ec.TriggerContext
	if ctx == nil {
		return zero, fault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if request.Validate() != nil || captured.Validate() != nil {
		return zero, fault(f.InvalidArgument)
	}
	if request.Launch.Trigger.Kind != "task" {
		return zero, fault(f.CapabilityUnsupported)
	}
	input, err := DecodeCapturedTaskInput(captured, request)
	if err != nil {
		return zero, taskTriggerError(err)
	}
	component, err := taskContextInstructions(input.Purpose())
	if err != nil {
		return zero, err
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	return ec.NewTriggerContext(request, captured, component)
}

// DecodeTaskContext is the Work-owned typed view for consumers of the built
// context. It verifies this provider's exact component revision/content and
// reuses the source decoder; it does not query current business state.
func DecodeTaskContext(v ec.TriggerContext) (TaskTriggerInput, error) {
	if v.Validate() != nil {
		return TaskTriggerInput{}, fault(f.InvalidArgument)
	}
	input, err := DecodeCapturedTaskInput(v.Source(), v.Request())
	if err != nil {
		return TaskTriggerInput{}, err
	}
	want, err := taskContextInstructions(input.Purpose())
	if err != nil {
		return TaskTriggerInput{}, err
	}
	got := v.Instructions()
	if got.Revision() != want.Revision() || got.Digest() != want.Digest() || got.Content() != want.Content() {
		return TaskTriggerInput{}, fault(f.SchemaUnsupported)
	}
	return input, nil
}

var _ ec.TriggerContextBuilder = TaskContextBuilder{}
