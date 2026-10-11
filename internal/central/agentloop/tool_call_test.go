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
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

func firstToolRequest(t *testing.T) DirectTextRequest {
	t.Helper()
	v := requestFields(t, "task/work")
	id := requestID[i.Tool](t, 901)
	reg := tc.ReadFileRegistration()
	agent := v.Agent.Fields()
	agent.AllowedToolIDs = []i.ToolID{id}
	var err error
	v.Agent, err = ac.NewAgentConfig(agent)
	requestOK(t, err)
	v.Tools = []tc.ExecutionTool{{ToolID: id, SpecRevision: 1, ModelVisibleName: "tool_" + strings.ReplaceAll(id.String(), "-", ""), BindingSnapshot: tc.BuiltinExecutionBinding{RunnerBinding: reg.Binding, ScopeResolverID: reg.ScopeResolverID, RiskClassifierID: reg.RiskClassifierID, Class: reg.Class}}}
	v.Model.Snapshot.Capabilities.ToolCalls = true
	captured := requestContext(t, v)
	actor, err := i.NewAgentRun(v.Request.Launch.ProjectID, v.Request.Launch.AgentID, v.Request.ExecutionID)
	requestOK(t, err)
	request, err := BuildToolCallRequest(context.Background(), captured, actor, requestID[ec.Round](t, 902).String(), requestID[mc.Call](t, 903), reg.Definition)
	requestOK(t, err)
	return request
}
func firstToolResponse(t *testing.T, r DirectTextRequest) mc.ModelResponse {
	t.Helper()
	args := json.RawMessage(`{"mount_id":"01900000-0000-7000-8000-000000000007","path":"README.md","offset":"0","length":64,"encoding":"utf8"}`)
	return mc.ModelResponse{CallID: r.Request().CallID, InvocationID: requestID[mc.Invocation](t, 904), Message: mc.Message{Role: "assistant", Parts: []mc.MessagePart{{Text: &mc.TextPart{Text: "inspect"}}, {ToolCall: &mc.ToolCallPart{ID: "call-1", Name: r.Request().Tools[0].Name, Arguments: args}}}}, FinishReason: "tool_calls", Usage: mc.Usage{Source: mc.UnknownUsage}}
}
func TestToolCallProjectionRequiresExplicitController(t *testing.T) {
	r := firstToolRequest(t)
	v := r.Request()
	if v.Input.SchemaVersion != 2 || v.ToolChoice.Kind != "required" || len(v.Tools) != 1 || !strings.Contains(v.Messages[0].Parts[0].Text.Text, v.Tools[0].Name) {
		t.Fatal("tool input/name table missing")
	}
	caller := &loopJSONCaller{begin: func(context.Context, mc.ModelRequest) (mc.JSONCall, error) {
		t.Fatal("Accept must not begin")
		return nil, nil
	}}
	old, err := NewDirectTextController(caller)
	requestOK(t, err)
	_, err = old.Accept(r)
	loopCode(t, err, f.CapabilityUnsupported)
	tool, err := NewToolCallController(caller)
	requestOK(t, err)
	_, err = tool.Accept(loopCandidate(t))
	loopCode(t, err, f.CapabilityUnsupported)
	session, err := tool.Accept(r)
	requestOK(t, err)
	session.Stop()
	requestOK(t, session.Drain(context.Background()))
	requestOK(t, tool.Drain(context.Background()))
	if !tool.Joined() {
		t.Fatal("accepted owner did not retire")
	}
}
func TestToolCallOriginalHandleMustActuallyJoin(t *testing.T) {
	r := firstToolRequest(t)
	response := firstToolResponse(t, r)
	begins, closes := 0, 0
	joined, allowJoin := false, false
	call := &loopJSONCall{result: func(context.Context) (mc.ModelResponse, error) { return response.Clone(), nil }, close: func(context.Context) error {
		closes++
		if !allowJoin {
			return context.DeadlineExceeded
		}
		joined = true
		return nil
	}, joined: func() bool { return joined }}
	controller, err := NewToolCallController(&loopJSONCaller{begin: func(context.Context, mc.ModelRequest) (mc.JSONCall, error) { begins++; return call, nil }})
	requestOK(t, err)
	session, err := controller.Accept(r)
	requestOK(t, err)
	got, err := session.Result(context.Background())
	if err == nil || got.CallID.Validate() == nil || session.Joined() || begins != 1 || closes != 1 {
		t.Fatal("unfinished original Close published success")
	}
	allowJoin = true
	got, err = session.Result(context.Background())
	requestOK(t, err)
	if !session.Joined() || got.InvocationID != response.InvocationID || begins != 1 || closes != 2 {
		t.Fatal("recovery replaced original handle")
	}
	requestOK(t, controller.Drain(context.Background()))
}
func TestToolCallResponseRejectsWrongIdentityAndShape(t *testing.T) {
	r := firstToolRequest(t)
	good := firstToolResponse(t, r)
	requestOK(t, validateSessionResponse(r, good))
	for name, mutate := range map[string]func(*mc.ModelResponse){
		"call":     func(v *mc.ModelResponse) { v.CallID = requestID[mc.Call](t, 999) },
		"finish":   func(v *mc.ModelResponse) { v.FinishReason = "stop" },
		"name":     func(v *mc.ModelResponse) { v.Message.Parts[1].ToolCall.Name = "invented" },
		"parallel": func(v *mc.ModelResponse) { v.Message.Parts = append(v.Message.Parts, v.Message.Parts[1]) },
		"unknown argument": func(v *mc.ModelResponse) {
			v.Message.Parts[1].ToolCall.Arguments = json.RawMessage(`{"absolute_path":"/etc/passwd"}`)
		},
		"path": func(v *mc.ModelResponse) {
			v.Message.Parts[1].ToolCall.Arguments = json.RawMessage(`{"mount_id":"01900000-0000-7000-8000-000000000007","path":"../escape","offset":"0","length":64,"encoding":"utf8"}`)
		},
	} {
		t.Run(name, func(t *testing.T) {
			v := good.Clone()
			mutate(&v)
			if validateSessionResponse(r, v) == nil {
				t.Fatal("invalid complete response accepted")
			}
		})
	}
}
