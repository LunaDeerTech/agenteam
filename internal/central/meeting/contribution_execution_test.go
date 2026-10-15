package meeting

import (
	"testing"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
)

func executionViewID[K any](t *testing.T) f.ID[K] {
	t.Helper()
	id, err := f.NewID[K]()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestContributionExecutionProjectionPreservesOriginalWait(t *testing.T) {
	project := executionViewID[i.Project](t)
	agent := executionViewID[i.Agent](t)
	execution := executionViewID[i.Execution](t)
	trigger := ec.Trigger{Kind: "meeting", MeetingID: executionViewID[c.Meeting](t).String(), TurnID: executionViewID[c.Turn](t).String(), ContributionID: executionViewID[c.Contribution](t).String(), ParticipantID: executionViewID[c.Participant](t).String()}
	generation, attempt := f.Version(2), f.Version(3)
	intent := intentRecord{execution: &execution, request: ec.LaunchRequest{ProjectID: project, AgentID: agent, Trigger: trigger, Purpose: "meeting/response", Lineage: ec.Lineage{ContributionGeneration: &generation, ContributionAttempt: &attempt}}}
	at, err := f.NewInstant(time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	reference := executionViewID[struct{}](t).String()
	reason := "approval"
	value := ec.MeetingExecutionResult{Summary: ec.Summary{ID: execution, ProjectID: project, AgentID: agent, Trigger: trigger, Purpose: "meeting/response", Status: ec.Waiting, Version: 4, CreatedAt: at, StartedAt: &at}, WaitingReason: &reason, WaitingReference: &reference}
	view, err := projectContributionExecution(intent, value)
	if err != nil || view.ID != execution || view.WaitingReference == nil || *view.WaitingReference != reference || view.WaitingReason == nil || *view.WaitingReason != "approval" || view.Generation != 2 || view.Attempt != 3 || view.ContributionID.String() != trigger.ContributionID {
		t.Fatal("original waiting facts changed", err)
	}
	for _, change := range []func(*ec.MeetingExecutionResult){
		func(v *ec.MeetingExecutionResult) { v.Summary.ID = executionViewID[i.Execution](t) },
		func(v *ec.MeetingExecutionResult) { v.Summary.ProjectID = executionViewID[i.Project](t) },
		func(v *ec.MeetingExecutionResult) { v.Summary.AgentID = executionViewID[i.Agent](t) },
		func(v *ec.MeetingExecutionResult) {
			v.Summary.Trigger.ContributionID = executionViewID[c.Contribution](t).String()
		},
		func(v *ec.MeetingExecutionResult) { v.Summary.Status = ec.Running },
		func(v *ec.MeetingExecutionResult) { v.WaitingReference = nil },
		func(v *ec.MeetingExecutionResult) { v.Summary.CompletedAt = &at },
	} {
		other := value
		change(&other)
		if _, err := projectContributionExecution(intent, other); err == nil {
			t.Fatal("foreign or inconsistent Execution facts were projected")
		}
	}
}
