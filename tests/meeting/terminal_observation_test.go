//go:build integration

package meeting_test

import (
	"context"
	"errors"
	"testing"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/meeting"
)

// This double deliberately separates a driver observation from the result
// reader's canonical facts, just as the real Associated owner forgets jobs
// after their original return. It grants no real Execution/Model capability.
type forgottenTerminalExecution struct {
	*controlledExecution
	terminal      ec.Status
	beforeAdvance bool
	advanceCalls  int
	advanceErr    error
}

func (v *forgottenTerminalExecution) Launch(ctx context.Context, actor i.Actor, request ec.LaunchRequest) (ec.LaunchResult, error) {
	result, err := v.controlledExecution.Launch(ctx, actor, request)
	if err == nil && v.beforeAdvance {
		v.finish(result.Execution.ID)
	}
	return result, err
}
func (v *forgottenTerminalExecution) finish(execution i.ExecutionID) {
	v.mu.Lock()
	defer v.mu.Unlock()
	result := v.entries[execution]
	result.Status = v.terminal
	result.Version++
	v.entries[execution] = result
}
func (v *forgottenTerminalExecution) AdvanceMeeting(_ context.Context, _ i.ProjectID, association ec.AssociatedMeeting) (ec.ExecutionAdvance, error) {
	v.advanceCalls++
	v.finish(association.ExecutionID)
	return ec.ExecutionAdvance{Phase: ec.ExecutionAccepted}, v.advanceErr
}

func TestMeetingDomainPublishesDurableTerminalAfterDriverForget(t *testing.T) {
	for _, tc := range []struct {
		name             string
		terminal         ec.Status
		before, rejected bool
	}{
		{"completed-after-accepted", ec.Succeeded, false, false},
		{"failed-after-accepted", ec.Failed, false, false},
		{"terminal-with-original-error", ec.Failed, false, true},
		{"already-terminal-never-readvanced", ec.Succeeded, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := setup(t)
			_, receipt := v.start(t, "sequential")
			authority, err := meeting.NewAuthority(v.store)
			if err != nil {
				t.Fatal(err)
			}
			controlled := &forgottenTerminalExecution{controlledExecution: &controlledExecution{store: v.store, authority: authority, entries: map[i.ExecutionID]ec.Summary{}}, terminal: tc.terminal, beforeAdvance: tc.before}
			if tc.rejected {
				controlled.advanceErr = f.NewFault(f.DependencyUnavailable, f.NotStarted)
			}
			actor, _ := i.RegisterService(i.MeetingRuntime)
			runtime, err := meeting.NewRuntime(v.store, meeting.RuntimeDependencies{Projects: v.authority, Launcher: controlled, Executor: controlled, Results: controlled, Authority: authority, Summaries: controlled, Actor: actor})
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Stop()
			_, err = runtime.Advance(testContext(t), v.authority.project.ID, receipt.MeetingID)
			if tc.rejected && !errors.Is(err, controlled.advanceErr) || !tc.rejected && err != nil {
				t.Fatal("original advance error changed", err)
			}
			detail, err := v.service.GetDetail(testContext(t), v.authority.actor, v.authority.project.ID, receipt.MeetingID)
			if err != nil {
				t.Fatal(err)
			}
			contribution := detail.Turns[0].Contributions[1]
			want := "failed"
			if tc.terminal == ec.Succeeded {
				want = "completed"
			}
			if contribution.Status != want || (contribution.Message != nil) != (tc.terminal == ec.Succeeded) {
				t.Fatal("durable terminal was not published", contribution.Status)
			}
			if detail.Turns[0].Contributions[2].Status != "pending" || len(controlled.inputs) != 1 {
				t.Fatal("observing terminal launched a successor in the same visit")
			}
			wantCalls := 1
			if tc.before {
				wantCalls = 0
			}
			if controlled.advanceCalls != wantCalls {
				t.Fatal("terminal association was advanced again", controlled.advanceCalls)
			}
		})
	}
}
