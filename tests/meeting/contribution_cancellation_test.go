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

// The controlled Execution isolates durable Meeting cancellation scope and
// recovery. Actual process cancellation is covered by the original App tests.
type cancellableContributionExecution struct {
	*controlledExecution
	waitingAgent i.AgentID
	cancelled    []i.ExecutionID
}

func (v *cancellableContributionExecution) AdvanceMeeting(ctx context.Context, p i.ProjectID, association ec.AssociatedMeeting) (ec.ExecutionAdvance, error) {
	if association.AgentID != v.waitingAgent {
		return v.controlledExecution.AdvanceMeeting(ctx, p, association)
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	row := v.entries[association.ExecutionID]
	if !row.Status.Terminal() {
		at, _ := f.NewInstant(time.Now().UTC())
		row.Status, row.StartedAt, row.Version = ec.Running, &at, row.Version+1
		v.entries[association.ExecutionID] = row
	}
	return ec.ExecutionAdvance{Status: row.Status}, nil
}
func (v *cancellableContributionExecution) ReadMeetingAgentSlotInTx(context.Context, f.Tx, i.ProjectID, i.AgentID, ec.Trigger) (ec.AgentSlotObservation, error) {
	return ec.AgentSlotObservation{}, f.NewFault(f.CapabilityUnsupported, f.NotStarted)
}
func (v *cancellableContributionExecution) RequestMeetingCancelInTx(ctx context.Context, tx f.Tx, project i.ProjectID, association ec.AssociatedMeeting) (ec.Summary, error) {
	locks, err := ec.MeetingExecutionReadLocks(project, association)
	if err != nil {
		return ec.Summary{}, err
	}
	if err = v.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return ec.Summary{}, err
	}
	if err = v.authority.RequireMeetingControlBindingInTx(ctx, tx, v.store); err != nil {
		return ec.Summary{}, err
	}
	if err = v.authority.RequireMeetingCancellationInTx(ctx, tx, project, association); err != nil {
		return ec.Summary{}, err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	row := v.entries[association.ExecutionID]
	if !row.Status.Terminal() {
		at, _ := f.NewInstant(time.Now().UTC())
		row.Status, row.CancelRequestedAt, row.CompletedAt, row.Version = ec.Cancelled, &at, &at, row.Version+1
		v.entries[association.ExecutionID] = row
		v.cancelled = append(v.cancelled, association.ExecutionID)
	}
	return row, nil
}
func (v *cancellableContributionExecution) NotifyMeetingCancellation(context.Context, i.ProjectID, ec.AssociatedMeeting) error {
	return nil
}

func TestMeetingSingleContributionCancelSurvivesRuntimeRecreation(t *testing.T) {
	v := setup(t)
	_, receipt := v.start(t, "sequential")
	authority, err := meeting.NewAuthority(v.store)
	if err != nil {
		t.Fatal(err)
	}
	controlled := &cancellableContributionExecution{controlledExecution: &controlledExecution{store: v.store, authority: authority, entries: map[i.ExecutionID]ec.Summary{}}, waitingAgent: v.authority.agents[0].ID}
	service, err := meeting.NewService(v.store, meeting.Dependencies{Projects: v.authority, Agents: v.authority, Directory: v.authority, DecisionAuthority: authority, DecisionResults: controlled})
	if err != nil {
		t.Fatal(err)
	}
	actor, _ := i.RegisterService(i.MeetingRuntime)
	deps := meeting.RuntimeDependencies{Projects: v.authority, Launcher: controlled, Executor: controlled, Results: controlled, Authority: authority, Control: controlled, Summaries: controlled, Actor: actor}
	runtime, err := meeting.NewRuntime(v.store, deps)
	if err != nil {
		t.Fatal(err)
	}
	ctx := testContext(t)
	if _, err = runtime.Advance(ctx, v.authority.project.ID, receipt.MeetingID); err != nil {
		t.Fatal(err)
	}
	detail, err := service.GetDetail(ctx, v.authority.actor, v.authority.project.ID, receipt.MeetingID)
	if err != nil {
		t.Fatal(err)
	}
	target := detail.Turns[0].Contributions[1]
	if target.ExecutionID == nil || target.Status != "running" {
		t.Fatal("original contribution is not live")
	}
	request := c.ControlRequest{ProjectID: v.authority.project.ID, MeetingID: receipt.MeetingID, Action: "cancel-contribution", TurnID: receipt.TurnID, ContributionID: &target.ID, ExpectedVersion: detail.Session.Version, Meta: meta(t)}
	foreign, _ := i.NewHuman(ident[i.User](t), ident[i.Session](t))
	if _, err = service.Control(ctx, foreign, request); err == nil {
		t.Fatal("foreign cancellation accepted")
	}
	wrong := request
	other := detail.Turns[0].Contributions[2].ID
	wrong.ContributionID, wrong.Meta = &other, meta(t)
	if _, err = service.Control(ctx, v.authority.actor, wrong); err == nil {
		t.Fatal("contribution without an Execution was cancellable")
	}
	committed, err := service.Control(ctx, v.authority.actor, request)
	if err != nil {
		t.Fatal("record cancellation", err)
	}
	if len(controlled.cancelled) != 0 {
		t.Fatal("Human transaction invoked an Execution side effect")
	}
	runtime.Stop()
	if !runtime.Joined() {
		t.Fatal("original visitor did not join")
	}
	runtime, err = meeting.NewRuntime(v.store, deps)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Stop()
	for n := 0; n < 4; n++ {
		if _, err = runtime.Advance(ctx, v.authority.project.ID, receipt.MeetingID); err != nil {
			t.Fatal("recover cancellation and continue", n, err)
		}
	}
	detail, err = service.GetDetail(ctx, v.authority.actor, v.authority.project.ID, receipt.MeetingID)
	if err != nil {
		t.Fatal(err)
	}
	turn := detail.Turns[0]
	if turn.Status != "completed" || turn.Result != "partial_failure" || turn.Contributions[1].Status != "cancelled" || !turn.Contributions[1].CancelRequested || turn.Contributions[1].Message != nil || turn.Contributions[2].Status != "completed" || turn.Contributions[2].CancelRequested || turn.Contributions[2].Message == nil || detail.Session.Summary == nil {
		t.Fatal("single cancellation stopped the Turn or changed another contribution")
	}
	if len(controlled.cancelled) != 1 || controlled.cancelled[0] != *target.ExecutionID || len(controlled.inputs) != 2 {
		t.Fatal("cancellation changed its original Execution scope")
	}
	replayed, err := service.Control(ctx, v.authority.actor, request)
	if err != nil || replayed.Version != committed.Version {
		t.Fatal("original cancellation replay failed after terminal", err)
	}
	request.Meta, request.ExpectedVersion = meta(t), detail.Session.Version
	if _, err = service.Control(ctx, v.authority.actor, request); err == nil {
		t.Fatal("new cancellation accepted terminal Execution")
	}
}
