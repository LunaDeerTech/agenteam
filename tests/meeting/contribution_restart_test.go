//go:build integration

package meeting_test

import (
	"context"
	"testing"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/meeting"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
)

type replacementExecution struct {
	*controlledExecution
	failNext bool
	requests []ec.LaunchRequest
}

func (v *replacementExecution) GenerateSummary(ctx context.Context, input meeting.SummaryInput) (meeting.SummaryCandidate, error) {
	result, err := v.controlledExecution.GenerateSummary(ctx, input)
	if !input.Initial {
		result.Title = nil
	}
	return result, err
}

func (v *replacementExecution) Launch(ctx context.Context, actor i.Actor, request ec.LaunchRequest) (ec.LaunchResult, error) {
	result, err := v.controlledExecution.Launch(ctx, actor, request)
	if err == nil {
		v.requests = append(v.requests, request.Clone())
	}
	return result, err
}
func (v *replacementExecution) AdvanceMeeting(ctx context.Context, project i.ProjectID, association ec.AssociatedMeeting) (ec.ExecutionAdvance, error) {
	if !v.failNext {
		return v.controlledExecution.AdvanceMeeting(ctx, project, association)
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.failNext = false
	row := v.entries[association.ExecutionID]
	row.Status, row.Version = ec.Failed, row.Version+1
	v.entries[association.ExecutionID] = row
	return ec.ExecutionAdvance{Status: ec.Failed}, nil
}

// Real PostgreSQL owns the immutable history, receipts and recovery state;
// execution completion and Summary text are explicit controlled dependencies.
func TestMeetingContributionReplacementPreservesHistoryAndSummary(t *testing.T) {
	v := setup(t)
	_, receipt := v.start(t, "sequential")
	ctx := testContext(t)
	authority, err := meeting.NewAuthority(v.store)
	if err != nil {
		t.Fatal(err)
	}
	executions := &replacementExecution{controlledExecution: &controlledExecution{store: v.store, authority: authority, entries: map[i.ExecutionID]ec.Summary{}}}
	service, err := meeting.NewService(v.store, meeting.Dependencies{Projects: v.authority, Agents: v.authority, Directory: v.authority, DecisionAuthority: authority, DecisionResults: executions})
	if err != nil {
		t.Fatal(err)
	}
	actor, _ := i.RegisterService(i.MeetingRuntime)
	deps := meeting.RuntimeDependencies{Projects: v.authority, Launcher: executions, Executor: executions, Results: executions, Authority: authority, Summaries: executions, Actor: actor}
	runtime, err := meeting.NewRuntime(v.store, deps)
	if err != nil {
		t.Fatal(err)
	}
	detail := func() c.Detail {
		t.Helper()
		out, err := service.GetDetail(ctx, v.authority.actor, v.authority.project.ID, receipt.MeetingID)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	advance := func(count int) {
		t.Helper()
		for n := 0; n < count; n++ {
			if _, err := runtime.Advance(ctx, v.authority.project.ID, receipt.MeetingID); err != nil {
				t.Fatal("advance", n, err)
			}
		}
	}
	advance(3)
	before := detail()
	if before.Turns[0].Status != "completed" || before.Session.Summary == nil {
		t.Fatal("original Turn not finalized")
	}
	original, other := before.Turns[0].Contributions[1], before.Turns[0].Contributions[2]
	request := c.ControlRequest{ProjectID: v.authority.project.ID, MeetingID: receipt.MeetingID, Action: "regenerate-contribution", TurnID: receipt.TurnID, ContributionID: &original.ID, ExpectedVersion: before.Session.Version, Meta: meta(t)}
	committed, err := service.Control(ctx, v.authority.actor, request)
	if err != nil {
		t.Fatal("regenerate", err)
	}
	pending := detail()
	if len(executions.requests) != 2 || pending.Turns[0].Status != "completed" || pending.Turns[0].Contributions[1].Generation != 2 || pending.Turns[0].Contributions[1].Attempt != 1 || pending.Turns[0].Contributions[1].ExecutionID != nil || pending.Turns[0].Contributions[1].Message.ID != original.Message.ID || pending.Session.Summary.PendingUpdate {
		t.Fatal("command launched work, removed the old reply or reopened history")
	}
	if replay, err := service.Control(ctx, v.authority.actor, request); err != nil || replay.Version != committed.Version {
		t.Fatal("regenerate replay", err)
	}
	runtime.Stop()
	if !runtime.Joined() {
		t.Fatal("old runtime did not join")
	}
	runtime, err = meeting.NewRuntime(v.store, deps)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Stop()
	executions.failNext = true
	advance(1)
	failed := detail()
	failedContribution := failed.Turns[0].Contributions[1]
	if failedContribution.Status != "failed" || failedContribution.Message.ID != original.Message.ID || failed.Session.Summary.PendingUpdate || len(executions.summaryInputs) != 1 {
		t.Fatal("failed replacement changed the effective input or summary")
	}
	if executions.requests[2].Lineage.RegenerateOf == nil || *executions.requests[2].Lineage.RegenerateOf != *original.ExecutionID {
		t.Fatal("missing regeneration lineage")
	}
	request.Action, request.ExpectedVersion, request.Meta = "retry-contribution", failed.Session.Version, meta(t)
	if _, err = service.Control(ctx, v.authority.actor, request); err != nil {
		t.Fatal("retry", err)
	}
	advance(1)
	after := detail()
	replacement := after.Turns[0].Contributions[1]
	if replacement.Generation != 2 || replacement.Attempt != 2 || replacement.Status != "completed" || replacement.Message.ID == original.Message.ID || *replacement.ExecutionID == *failedContribution.ExecutionID || *after.Turns[0].Contributions[2].ExecutionID != *other.ExecutionID {
		t.Fatal("retry lost contribution identity, history or affected another Agent")
	}
	if after.Turns[0].Status != "completed" || after.Session.Summary.Version != before.Session.Summary.Version || !after.Session.Summary.PendingUpdate || after.Session.Summary.InputDigest != before.Session.Summary.InputDigest || *after.Session.Title != *before.Session.Title || len(executions.summaryInputs) != 1 {
		t.Fatal("historical replacement rewrote summary coverage or title")
	}
	if len(replacement.History) != 3 || replacement.History[0].ExecutionID != *original.ExecutionID || replacement.History[0].Message == nil || replacement.History[0].Message.ID != original.Message.ID || replacement.History[1].Message != nil || replacement.History[1].RegenerateOf == nil || *replacement.History[1].RegenerateOf != *original.ExecutionID || replacement.History[2].RetryOf == nil || *replacement.History[2].RetryOf != *failedContribution.ExecutionID || replacement.History[2].Message == nil || replacement.History[2].Message.ID != replacement.Message.ID {
		t.Fatal("detail lost immutable replies or original execution lineage")
	}
	if executions.requests[3].Lineage.RetryOf == nil || *executions.requests[3].Lineage.RetryOf != *failedContribution.ExecutionID {
		t.Fatal("missing retry lineage")
	}
	for _, input := range executions.inputs[2:] {
		found := false
		for _, message := range input.Messages {
			if message.ContributionID == original.ID {
				found = true
				if message.Generation != 1 || message.Attempt != 1 || message.Message.ID != original.Message.ID {
					t.Fatal("old message relabelled as replacement generation")
				}
			}
		}
		if !found {
			t.Fatal("regeneration did not freeze the original visible conversation")
		}
	}
	var history, messages int
	if err = v.store.QueryRow(ctx, `SELECT count(*),count(message_id) FROM agenteam_meeting.contribution_executions WHERE contribution_id=$1`, original.ID.String()).Scan(&history, &messages); err != nil || history != 3 || messages != 2 {
		t.Fatal("immutable history", history, messages, err)
	}
	send := c.SendRequest{ProjectID: v.authority.project.ID, MeetingID: receipt.MeetingID, ExpectedVersion: after.Session.Version, Mode: "sequential", Content: []c.InlineNode{{Type: "text", Text: "Continue using the replaced reply."}}, Meta: meta(t)}
	if _, err = service.Send(ctx, v.authority.actor, send); err != nil {
		t.Fatal("new ordinary Turn", err)
	}
	advance(3)
	final := detail()
	if final.Session.Summary.Version != 2 || final.Session.Summary.PendingUpdate || len(executions.summaryInputs) != 2 || len(executions.requests) != 6 {
		t.Fatal("next ordinary finalize did not refresh pending summary")
	}
	if !executions.inputs[4].Summary.PendingUpdate {
		t.Fatal("next Agent input concealed stale summary coverage")
	}
}
