package agentloop

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/meeting"
	meetingc "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

func TestInitialLoopChoiceUsesCapturedMeetingSource(t *testing.T) {
	for _, source := range []string{"task", "meeting"} {
		t.Run(source, func(t *testing.T) {
			fields := requestFields(t, "task/work")
			tool := requestID[i.Tool](t, 901)
			registration := tc.ReadFileRegistration()
			agent := fields.Agent.Fields()
			agent.AllowedToolIDs = []i.ToolID{tool}
			var err error
			fields.Agent, err = ac.NewAgentConfig(agent)
			requestOK(t, err)
			fields.Tools = []tc.ExecutionTool{{ToolID: tool, SpecRevision: 1, ModelVisibleName: "tool_" + strings.ReplaceAll(tool.String(), "-", ""), BindingSnapshot: tc.BuiltinExecutionBinding{RunnerBinding: registration.Binding, ScopeResolverID: registration.ScopeResolverID, RiskClassifierID: registration.RiskClassifierID, Class: registration.Class}}}
			fields.Model.Snapshot.Capabilities.ToolCalls = true
			var captured ec.ExecutionContext
			if source == "task" {
				captured = requestContext(t, fields)
			} else {
				meetingID := requestID[meetingc.Meeting](t, 201)
				participant := requestID[meetingc.Participant](t, 202)
				fields.Request.Launch.Trigger = ec.Trigger{Kind: "meeting", MeetingID: meetingID.String(), ParticipantID: participant.String(), TurnID: requestID[meetingc.Turn](t, 203).String(), ContributionID: requestID[meetingc.Contribution](t, 204).String()}
				fields.Request.Launch.Purpose = "meeting/response"
				one := f.Version(1)
				fields.Request.Launch.Lineage.ContributionGeneration, fields.Request.Launch.Lineage.ContributionAttempt = &one, &one
				frozen := meeting.FrozenContext{SchemaVersion: 1, ProjectID: fields.Request.Launch.ProjectID, Meeting: meeting.MeetingIdentity{ID: meetingID, Status: "active"}, Participants: []meetingc.ParticipantView{{ID: participant, MeetingID: meetingID, Kind: "agent", SourceID: fields.Request.Launch.AgentID.String(), Name: "Reviewer", Active: true}}, References: []meetingc.Reference{}, Messages: []meeting.FrozenMessage{}}
				raw, err := json.Marshal(frozen)
				requestOK(t, err)
				fields.Trigger, err = ec.NewCapturedTriggerInput(ec.TriggerInputRef{ProviderType: "meeting", SchemaVersion: 1, InputID: requestID[ec.TriggerInput](t, 206), Digest: ec.TriggerInputDigest(raw)}, raw)
				requestOK(t, err)
				input, err := ec.NewPreparationInput(fields)
				requestOK(t, err)
				trigger, err := (meeting.ContextBuilder{}).BuildTriggerContext(context.Background(), fields.Request, fields.Trigger)
				requestOK(t, err)
				captured, err = ec.NewExecutionContext(input, trigger)
				requestOK(t, err)
			}
			actor, err := i.NewAgentRun(fields.Request.Launch.ProjectID, fields.Request.Launch.AgentID, fields.Request.ExecutionID)
			requestOK(t, err)
			value, err := BuildLoopRequest(context.Background(), captured, actor, requestID[ec.Round](t, 902).String(), requestID[mc.Call](t, 903), []tc.Definition{registration.Definition})
			requestOK(t, err)
			request := value.Request()
			choice := "required"
			if source == "meeting" {
				choice = "auto"
			}
			if request.ToolChoice != (mc.ToolChoice{Kind: choice}) || len(request.Tools) != 1 || request.Tools[0].Name != fields.Tools[0].ModelVisibleName {
				t.Fatal("source changed frozen tools or chose wrong first-round policy")
			}
			digest, err := mc.ToolInputDigest(context.Background(), request.Messages, request.Tools, request.ToolChoice)
			requestOK(t, err)
			if digest != request.Input.Digest {
				t.Fatal("choice not bound to original input")
			}
			response := mc.ModelResponse{CallID: request.CallID, InvocationID: requestID[mc.Invocation](t, 904), Message: mc.Message{Role: "assistant", Parts: []mc.MessagePart{{Text: &mc.TextPart{Text: "The contribution directly answers the conversation."}}}}, FinishReason: "stop", Usage: mc.Usage{Source: mc.UnknownUsage}}
			err = validateLoopResponse(context.Background(), request, response)
			if source == "meeting" && err != nil || source == "task" && err == nil {
				t.Fatalf("direct final response policy: %v", err)
			}
		})
	}
}
