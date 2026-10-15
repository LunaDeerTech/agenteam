package meeting

import (
	"testing"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
)

func TestContributionRuntimeRejectsForeignAssociation(t *testing.T) {
	project, agent, execution := executionViewID[i.Project](t), executionViewID[i.Agent](t), executionViewID[i.Execution](t)
	trigger := ec.Trigger{Kind: "meeting", MeetingID: executionViewID[c.Meeting](t).String(), TurnID: executionViewID[c.Turn](t).String(), ContributionID: executionViewID[c.Contribution](t).String(), ParticipantID: executionViewID[c.Participant](t).String()}
	generation, attempt := f.Version(2), f.Version(3)
	intent := intentRecord{execution: &execution, request: ec.LaunchRequest{ProjectID: project, AgentID: agent, Trigger: trigger, Purpose: "meeting/response", Lineage: ec.Lineage{ContributionGeneration: &generation, ContributionAttempt: &attempt}}}
	at, _ := f.NewInstant(time.Unix(1, 0).UTC())
	value := ec.MeetingRuntimeResult{Execution: ec.MeetingExecutionResult{Summary: ec.Summary{ID: execution, ProjectID: project, AgentID: agent, Trigger: trigger, Purpose: "meeting/response", Status: ec.Running, Version: 4, CreatedAt: at, StartedAt: &at}}, Availability: ec.RuntimePending}
	view, err := projectContributionRuntime(intent, value)
	if err != nil || view.Execution.ID != execution || view.Execution.Generation != generation || view.Execution.Attempt != attempt || view.Snapshot != nil || view.Availability != ec.RuntimePending {
		t.Fatal("runtime lost the exact current Contribution association", err)
	}
	for _, change := range []func(*ec.MeetingRuntimeResult){
		func(v *ec.MeetingRuntimeResult) { v.Execution.Summary.ID = executionViewID[i.Execution](t) },
		func(v *ec.MeetingRuntimeResult) { v.Execution.Summary.ProjectID = executionViewID[i.Project](t) },
		func(v *ec.MeetingRuntimeResult) {
			v.Execution.Summary.Trigger.ContributionID = executionViewID[c.Contribution](t).String()
		},
		func(v *ec.MeetingRuntimeResult) { v.Availability = ec.RuntimeAvailable },
	} {
		other := value
		change(&other)
		if _, err := projectContributionRuntime(intent, other); err == nil {
			t.Fatal("runtime projected a different association or invented snapshot")
		}
	}
}
