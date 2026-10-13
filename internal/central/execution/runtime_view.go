package execution

import (
	"context"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// GetRuntimeView returns only the original producer's durable final snapshot.
// Pending is not an active Runtime View; no subscription or token stream is
// synthesized by reading canonical state. Errors never return a partial view.
func (r *TaskExecutionReader) GetRuntimeView(ctx context.Context, actor i.Actor, project i.ProjectID, task string, id i.ExecutionID) (out ec.TaskRuntimeView, err error) {
	if err = taskReadInput(ctx, actor, project, task); err != nil {
		return out, err
	}
	if id.Validate() != nil {
		return out, invalid()
	}
	request := taskExecutionRead{task: task, execution: id, runtime: true}
	ctx, entry, err := r.begin(ctx, actor, project, request)
	if entry != nil {
		defer func() {
			if err != nil {
				out = ec.TaskRuntimeView{}
			}
		}()
		defer finishProjectCall(r.state, entry, &err)
	}
	if err != nil {
		return out, err
	}
	err = r.read(ctx, actor, project, request, func(ctx context.Context, x postgres.SQLExecutor) error {
		var e error
		out, e = loadTaskRuntimeView(ctx, x, project, task, id)
		return e
	})
	return out, err
}
