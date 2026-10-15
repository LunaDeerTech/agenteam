package execution

import (
	"context"
	"testing"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

func TestMeetingToolSourceUsesCanonicalLaunchIdentity(t *testing.T) {
	for _, kind := range []string{"task", "meeting"} {
		t.Run(kind, func(t *testing.T) {
			x := newLaunchControl(t)
			request := x.request
			want := request.Trigger.TaskID
			if kind == "meeting" {
				want = newTestID[struct{}](t).String()
				request.Trigger = ec.Trigger{Kind: "meeting", MeetingID: want, TurnID: newTestID[struct{}](t).String(), ContributionID: newTestID[struct{}](t).String(), ParticipantID: newTestID[struct{}](t).String()}
				request.Purpose = "meeting/response"
			}
			binding := tc.ToolCallBinding{ExecutionID: newTestID[i.Execution](t), ProjectID: request.ProjectID, AgentID: request.AgentID}
			row := &executionRecord{summary: ec.Summary{ID: binding.ExecutionID, ProjectID: binding.ProjectID, AgentID: binding.AgentID, Status: ec.Succeeded}, launch: request}
			source, err := meetingToolSource(row, binding)
			if err != nil || source.ExecutionID != binding.ExecutionID || source.Origin == nil || source.Origin.Kind != kind || source.Origin.ID != want {
				t.Fatal("canonical provenance changed", err)
			}
			for _, change := range []func(*executionRecord){
				func(r *executionRecord) { r.summary.ID = newTestID[i.Execution](t) },
				func(r *executionRecord) { r.summary.ProjectID = newTestID[i.Project](t) },
				func(r *executionRecord) { r.launch.ProjectID = newTestID[i.Project](t) },
				func(r *executionRecord) { r.launch.AgentID = newTestID[i.Agent](t) },
				func(r *executionRecord) { r.launch.Trigger.Kind = "client-supplied" },
			} {
				changed := *row
				change(&changed)
				got, err := meetingToolSource(&changed, binding)
				if err == nil || got.ExecutionID.Validate() == nil || got.Origin != nil {
					t.Fatal("foreign or invalid launch exposed provenance")
				}
			}
		})
	}
}

// A structurally valid Tool binding is insufficient for either current use
// or original lookup. Both paths must reject a foreign plan before any SQL.
func TestMeetingToolSourceRejectsForeignPlanBeforeReading(t *testing.T) {
	x := newReadFileAuthorityControl(t)
	x.driver.driver.state.multi = &multiRoundState{}
	binding, err := toolBinding(&x.stored)
	if err != nil {
		t.Fatal(err)
	}
	binding.RunnerBinding = tc.RunnerBinding{}
	binding.Binding = tc.BuiltinBinding{HandlerID: "meeting.request", ContractRevision: 1}
	if err := binding.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, lookup := range []bool{false, true} {
		got, err := x.driver.ReadMeetingToolSourceInTx(context.Background(), f.NewTx(), binding, &toolCallPlan{binding: binding}, lookup)
		requireCode(t, err, f.Forbidden)
		if got.ExecutionID.Validate() == nil || got.Origin != nil {
			t.Fatal("a foreign plan exposed source identity")
		}
	}
}
