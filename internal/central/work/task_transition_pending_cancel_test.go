package work

import (
	"context"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func TestTaskTransitionPendingCancellationRequiresOriginalPostimage(t *testing.T) {
	store, deps := pureTransitionPorts(t)
	s, err := NewTaskTransition(store, deps)
	if err != nil {
		t.Fatal(err)
	}
	r, actor, _ := pureTransitionRecord(t)
	scope := c.TaskPendingCancellationScope{ProjectID: r.Input.Project, TaskID: r.Input.Task.String(), CommandID: r.ID, DispatchIDs: []string{pureID[struct{}](t, 71).String()}, OccurredAt: r.Plan.After.Task.UpdatedAt}
	pureCode(t, s.RequirePendingTaskCancellationInTx(context.Background(), f.NewTx(), actor, scope), f.Forbidden)
	ctx := context.WithValue(context.Background(), pendingTaskCancelKey{}, &pendingTaskCancelWitness{owner: s.state(), tx: f.NewTx(), actor: actor.Details(), record: r})
	pureCode(t, s.RequirePendingTaskCancellationInTx(ctx, f.NewTx(), actor, scope), f.Forbidden)
	if store.touches.Load() != 0 {
		t.Fatal("scope or foreign transaction reached SQL")
	}
	r.Plan.CancelDispatches = scope.DispatchIDs
	if validateCancellationRecord(r) == nil {
		t.Fatal("pending cancellation attached to nonterminal transition")
	}
	r.Plan.After.Task.State = c.TaskStateCancelled
	if err = validateCancellationRecord(r); err != nil {
		t.Fatal(err)
	}
	r.Plan.CancelDispatches = []string{"foreign"}
	if validateCancellationRecord(r) == nil {
		t.Fatal("invalid pending identity entered durable plan")
	}
}
