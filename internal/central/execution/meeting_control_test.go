package execution

import (
	"context"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"testing"
)

func TestMeetingCancellationCannotCrossSourceOrRewriteTerminal(t *testing.T) {
	p, task := associatedTestIdentity(t)
	trigger := c.Trigger{Kind: "meeting", MeetingID: newTestID[struct{}](t).String(), TurnID: newTestID[struct{}](t).String(), ContributionID: newTestID[struct{}](t).String(), ParticipantID: newTestID[struct{}](t).String()}
	m := c.AssociatedMeeting{ExecutionID: task.ExecutionID, AgentID: task.AgentID, Key: task.Key, Digest: task.Digest, Trigger: trigger, Generation: 1, Attempt: 1}
	generation, attempt := f.Version(1), f.Version(1)
	row := &executionRecord{summary: c.Summary{ID: m.ExecutionID, ProjectID: p, AgentID: m.AgentID, Status: c.Succeeded, Version: 7}, digest: m.Digest, launch: c.LaunchRequest{Trigger: trigger, Purpose: "meeting/response", Lineage: c.Lineage{ContributionGeneration: &generation, ContributionAttempt: &attempt}}}
	if !meetingControlMatches(row, p, m) {
		t.Fatal("original binding rejected")
	}
	for _, change := range []func(*c.AssociatedMeeting){
		func(v *c.AssociatedMeeting) { v.Trigger.MeetingID = newTestID[struct{}](t).String() },
		func(v *c.AssociatedMeeting) { v.Trigger.ParticipantID = newTestID[struct{}](t).String() },
		func(v *c.AssociatedMeeting) { v.Generation++ },
		func(v *c.AssociatedMeeting) { v.ExecutionID = newTestID[i.Execution](t) },
		func(v *c.AssociatedMeeting) { v.Digest = c.TriggerInputDigest([]byte("other")) },
	} {
		bad := m
		change(&bad)
		if meetingControlMatches(row, p, bad) {
			t.Fatal("foreign cancellation accepted")
		}
	}
	// No SQL executor is provided: terminal replay must not issue any write.
	result, err := requestCancellationInTx(context.Background(), nil, row, nil)
	if err != nil || result.Version != 7 || result.CancelRequestedAt != nil || result.Status != c.Succeeded {
		t.Fatal("terminal cancellation changed result", err)
	}
	stale := f.Version(6)
	if _, err = requestCancellationInTx(context.Background(), nil, row, &stale); err == nil {
		t.Fatal("stale Task expected version accepted")
	}
}
