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

func domainRuntime(t *testing.T, v *fixture, generator meeting.SummaryGenerator) (*meeting.Runtime, *controlledExecution) {
	t.Helper()
	authority, err := meeting.NewAuthority(v.store)
	if err != nil {
		t.Fatal(err)
	}
	controlled := &controlledExecution{store: v.store, authority: authority, entries: map[i.ExecutionID]ec.Summary{}}
	if generator == nil {
		generator = controlled
	}
	actor, _ := i.RegisterService(i.MeetingRuntime)
	runtime, err := meeting.NewRuntime(v.store, meeting.RuntimeDependencies{Projects: v.authority, Launcher: controlled, Executor: controlled, Results: controlled, Authority: authority, Summaries: generator, Actor: actor})
	if err != nil {
		t.Fatal(err)
	}
	return runtime, controlled
}
func TestMeetingDomainCancelAndArchive(t *testing.T) {
	v := setup(t)
	_, receipt := v.start(t, "sequential")
	runtime, controlled := domainRuntime(t, v, nil)
	a := v.authority
	ctx := testContext(t)
	request := c.ControlRequest{ProjectID: a.project.ID, MeetingID: receipt.MeetingID, Action: "cancel-turn", TurnID: receipt.TurnID, ExpectedVersion: receipt.Version, Meta: meta(t)}
	cancelled, err := v.service.Control(ctx, a.actor, request)
	if err != nil {
		t.Fatal("cancel intent", err)
	}
	replay, found, err := v.service.LookupControl(ctx, a.actor, request)
	if err != nil || !found || replay.Version != cancelled.Version {
		t.Fatal("cancel lookup", err)
	}
	if _, err = runtime.Advance(ctx, a.project.ID, receipt.MeetingID); err != nil {
		t.Fatal("cancel queued", err)
	}
	detail, err := v.service.GetDetail(ctx, a.actor, a.project.ID, receipt.MeetingID)
	if err != nil || detail.Turns[0].Status != "cancelled" || len(controlled.inputs) != 0 || detail.Session.Summary != nil {
		t.Fatal("cancel must not launch or invent summary", err)
	}
	archive := c.ControlRequest{ProjectID: a.project.ID, MeetingID: receipt.MeetingID, Action: "archive", ExpectedVersion: detail.Session.Version, Meta: meta(t)}
	archived, err := v.service.Control(ctx, a.actor, archive)
	if err != nil {
		t.Fatal("archive", err)
	}
	send := c.SendRequest{ProjectID: a.project.ID, MeetingID: receipt.MeetingID, ExpectedVersion: archived.Version, Mode: "sequential", Content: []c.InlineNode{{Type: "text", Text: "next"}}, Meta: meta(t)}
	if _, err = v.service.Send(ctx, a.actor, send); err == nil {
		t.Fatal("send into archived session")
	}
	archive.Action = "unarchive"
	archive.ExpectedVersion = archived.Version
	archive.Meta = meta(t)
	restored, err := v.service.Control(ctx, a.actor, archive)
	if err != nil {
		t.Fatal("unarchive", err)
	}
	send.ExpectedVersion = restored.Version
	if _, err = v.service.Send(ctx, a.actor, send); err != nil {
		t.Fatal("send restored", err)
	}
	runtime.Stop()
	if !runtime.Joined() {
		t.Fatal("runtime retained")
	}
}

type failedSummary struct{ calls int }

func (v *failedSummary) GenerateSummary(ctx context.Context, input meeting.SummaryInput) (meeting.SummaryCandidate, error) {
	v.calls++
	if v.calls == 1 {
		return meeting.SummaryCandidate{}, f.NewFault(f.DependencyUnavailable, f.NotStarted)
	}
	title := "Recovered summary"
	return meeting.SummaryCandidate{Title: &title, Goals: "goal", Decisions: "", Unresolved: "", Facts: "fact"}, nil
}
func TestMeetingDomainSummaryRetry(t *testing.T) {
	v := setup(t)
	_, receipt := v.start(t, "sequential")
	summary := &failedSummary{}
	runtime, _ := domainRuntime(t, v, summary)
	a := v.authority
	ctx := testContext(t)
	var failed error
	for n := 0; n < 4; n++ {
		if _, err := runtime.Advance(ctx, a.project.ID, receipt.MeetingID); err != nil {
			failed = err
			break
		}
	}
	if failed == nil {
		t.Fatal("expected summary failure")
	}
	detail, err := v.service.GetDetail(ctx, a.actor, a.project.ID, receipt.MeetingID)
	if err != nil || detail.Turns[0].Status != "finalizing" || detail.Turns[0].SummaryError == "" || detail.Session.Summary != nil {
		t.Fatal("failure did not preserve finalizing", err)
	}
	request := c.ControlRequest{ProjectID: a.project.ID, MeetingID: receipt.MeetingID, Action: "retry-summary", TurnID: receipt.TurnID, ExpectedVersion: detail.Session.Version, Meta: meta(t)}
	if _, err = v.service.Control(ctx, a.actor, request); err != nil {
		t.Fatal("retry summary", err)
	}
	if _, err = runtime.Advance(ctx, a.project.ID, receipt.MeetingID); err != nil {
		t.Fatal("finalize retry", err)
	}
	detail, err = v.service.GetDetail(ctx, a.actor, a.project.ID, receipt.MeetingID)
	if err != nil || detail.Session.Summary == nil || detail.Session.Summary.Version != 1 || detail.Turns[0].Status != "completed" || summary.calls != 2 {
		t.Fatal("retry did not publish exactly once", err)
	}
	runtime.Stop()
}

func TestMeetingDomainParallelActuallyOverlaps(t *testing.T) {
	v := setup(t)
	_, receipt := v.start(t, "parallel")
	runtime, controlled := domainRuntime(t, v, nil)
	controlled.started = make(chan struct{}, 2)
	release := make(chan struct{})
	controlled.release = release
	ctx, cancel := context.WithTimeout(testContext(t), 5*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() { _, err := runtime.Advance(ctx, v.authority.project.ID, receipt.MeetingID); finished <- err }()
	for n := 0; n < 2; n++ {
		select {
		case <-controlled.started:
		case <-ctx.Done():
			cancel()
			<-finished
			t.Fatal("parallel contributions did not enter together")
		}
	}
	close(release)
	if err := <-finished; err != nil {
		t.Fatal("parallel join", err)
	}
	runtime.Stop()
	if !runtime.Joined() {
		t.Fatal("parallel branch escaped parent join")
	}
}
