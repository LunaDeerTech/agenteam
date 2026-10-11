//go:build integration

package projectvariable_test

import (
	"testing"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/scheduler"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// Relaunch starts from a real completed Model call. The original Task remains
// in_progress, and the original Claim/Launch/Execution lineage stays intact.
// The relaunch cases are added against the frozen production contracts; this
// setup does not invent a terminal writer or a relaunch authorization.
type schedulerRelaunchFixture struct {
	execution *schedulerExecutionFixture
	original  scheduler.Dispatch
	task      wc.Task
}

func newSchedulerRelaunchFixture(t *testing.T) *schedulerRelaunchFixture {
	t.Helper()
	x := newSchedulerExecutionFixture(t, false)
	x.start(t)
	runner := x.runner(t, true)
	first, err := runner.RunTraversal(ctxFor(t))
	firstRoundRequire(t, err)
	if len(first.Visits) != 1 || first.Visits[0].Err != nil || first.Visits[0].Action != scheduler.ProjectVisitClaim {
		t.Fatal("initial real traversal did not claim and launch the original todo")
	}
	dispatch := first.Visits[0].Dispatch
	x.bindOriginal(t, dispatch)
	x.requireDelivery(t, first, dispatch)
	x.awaitTerminal(t, ec.Succeeded)
	stopSchedulerExecutionRunner(t, runner)
	x.requireTerminal(t, ec.Succeeded)
	c := x.round.capture
	task, err := c.v.taskReader.GetTask(ctxFor(t), c.v.base.ownerBrowser.actor, c.v.base.project.ID, c.v.task.ID)
	firstRoundRequire(t, err)
	if task.State != wc.TaskStateInProgress {
		t.Fatal("the real terminal Execution changed the independently owned Task")
	}
	// Keep the original executor and its real providers owned by the existing
	// fixture cleanup. Reconstructing Scheduler owners must not stop or replace
	// the execution graph, or depend on mutable capture-observation taps.
	return &schedulerRelaunchFixture{execution: x, original: dispatch, task: task}
}
