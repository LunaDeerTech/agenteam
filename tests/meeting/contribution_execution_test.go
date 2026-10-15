//go:build integration

package meeting_test

import (
	"context"
	"testing"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/meeting"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
)

// A controlled waiting Execution tests the real Meeting transaction and
// source boundary. Actual Approval/Executor behavior is tested in the App.
type waitingViewExecution struct {
	*controlledExecution
	reference string
	reads     int
}

func (v *waitingViewExecution) AdvanceMeeting(_ context.Context, _ i.ProjectID, m ec.AssociatedMeeting) (ec.ExecutionAdvance, error) {
	row := v.entries[m.ExecutionID]
	at, err := f.NewInstant(time.Now().UTC())
	if err != nil {
		return ec.ExecutionAdvance{}, err
	}
	row.Status, row.Version, row.StartedAt = ec.Waiting, row.Version+1, &at
	v.entries[m.ExecutionID] = row
	return ec.ExecutionAdvance{Status: ec.Waiting}, nil
}

func (v *waitingViewExecution) ReadMeetingExecutionInTx(ctx context.Context, tx f.Tx, project i.ProjectID, m ec.AssociatedMeeting) (ec.MeetingExecutionResult, error) {
	locks, err := ec.MeetingExecutionReadLocks(project, m)
	if err != nil {
		return ec.MeetingExecutionResult{}, err
	}
	if err = v.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return ec.MeetingExecutionResult{}, err
	}
	result, err := v.controlledExecution.ReadMeetingExecutionInTx(ctx, tx, project, m)
	if err != nil {
		return result, err
	}
	v.reads++
	if result.Summary.Status == ec.Waiting {
		reason := "approval"
		result.WaitingReason, result.WaitingReference = &reason, &v.reference
	}
	return result, nil
}

func TestMeetingContributionExecutionReadUsesCurrentSource(t *testing.T) {
	v := setup(t)
	_, receipt := v.start(t, "sequential")
	authority, err := meeting.NewAuthority(v.store)
	if err != nil {
		t.Fatal(err)
	}
	controlled := &waitingViewExecution{controlledExecution: &controlledExecution{store: v.store, authority: authority, entries: map[i.ExecutionID]ec.Summary{}}, reference: ident[struct{}](t).String()}
	service, err := meeting.NewService(v.store, meeting.Dependencies{Projects: v.authority, Agents: v.authority, Directory: v.authority, DecisionAuthority: authority, DecisionResults: controlled})
	if err != nil {
		t.Fatal(err)
	}
	actor, _ := i.RegisterService(i.MeetingRuntime)
	runtime, err := meeting.NewRuntime(v.store, meeting.RuntimeDependencies{Projects: v.authority, Launcher: controlled, Executor: controlled, Results: controlled, Authority: authority, Actor: actor})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Stop()
	ctx := testContext(t)
	if _, err = runtime.Advance(ctx, v.authority.project.ID, receipt.MeetingID); err != nil {
		t.Fatal(err)
	}
	detail, err := service.GetDetail(ctx, v.authority.actor, v.authority.project.ID, receipt.MeetingID)
	if err != nil {
		t.Fatal(err)
	}
	contribution := detail.Turns[0].Contributions[1]
	got, err := service.GetContributionExecution(ctx, v.authority.actor, v.authority.project.ID, receipt.MeetingID, contribution.ID)
	if err != nil || got.Validate() != nil || got.ID != *contribution.ExecutionID || got.Status != ec.Waiting || got.WaitingReference == nil || *got.WaitingReference != controlled.reference || contribution.Status != "running" {
		t.Fatal("current wait not projected without changing Contribution", err)
	}
	reads := controlled.reads
	other, _ := i.NewHuman(ident[i.User](t), ident[i.Session](t))
	for _, input := range []struct {
		actor        i.Actor
		meeting      c.MeetingID
		contribution c.ContributionID
	}{
		{other, receipt.MeetingID, contribution.ID},
		{v.authority.actor, ident[c.Meeting](t), contribution.ID},
		{v.authority.actor, receipt.MeetingID, detail.Turns[0].Contributions[0].ID},
		{v.authority.actor, receipt.MeetingID, detail.Turns[0].Contributions[2].ID},
	} {
		if _, err = service.GetContributionExecution(ctx, input.actor, v.authority.project.ID, input.meeting, input.contribution); err == nil {
			t.Fatal("foreign source or contribution without Execution was readable")
		}
	}
	if controlled.reads != reads {
		t.Fatal("rejected source invoked the Execution reader")
	}
}
