package agentloop

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func firstWriteToolRequest(t *testing.T) DirectTextRequest {
	t.Helper()
	v := requestFields(t, "task/work")
	id := requestID[i.Tool](t, 901)
	reg := tc.WriteFileRegistration()
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
	round, call := requestID[ec.Round](t, 902).String(), requestID[mc.Call](t, 903)
	if _, err = BuildToolCallRequest(context.Background(), captured, actor, round, call, reg.Definition); err == nil {
		t.Fatal("read builder accepted write capture")
	}
	request, err := BuildWriteFileToolCallRequest(context.Background(), captured, actor, round, call, reg.Definition)
	requestOK(t, err)
	return request
}
func writeToolResponse(t *testing.T, r DirectTextRequest, data []byte) mc.ModelResponse {
	t.Helper()
	args, err := json.Marshal(struct {
		Mount     string `json:"mount_id"`
		Path      string `json:"path"`
		Data      string `json:"data_base64"`
		Overwrite bool   `json:"overwrite"`
	}{"01900000-0000-7000-8000-000000000007", "binary.bin", base64.StdEncoding.EncodeToString(data), false})
	requestOK(t, err)
	return mc.ModelResponse{CallID: r.Request().CallID, InvocationID: requestID[mc.Invocation](t, 904), Message: mc.Message{Role: "assistant", Parts: []mc.MessagePart{{ToolCall: &mc.ToolCallPart{ID: "write-call", Name: r.Request().Tools[0].Name, Arguments: args}}}}, FinishReason: "tool_calls", Usage: mc.Usage{Source: mc.UnknownUsage}}
}
func TestWriteFileToolCallExplicitProfileAndMaximum(t *testing.T) {
	r := firstWriteToolRequest(t)
	data := bytes.Repeat([]byte{0, 255, 13, 10}, p.MaxWriteFileBytes/4)
	response := writeToolResponse(t, r, data)
	if len(response.Message.Parts[0].ToolCall.Arguments) <= 32<<10 {
		t.Fatal("test failed to cross read limit")
	}
	begins, closes := 0, 0
	joined := false
	caller := &loopJSONCaller{begin: func(_ context.Context, request mc.ModelRequest) (mc.JSONCall, error) {
		begins++
		if request.Input.Digest != r.Request().Input.Digest || len(request.Tools) != 1 || request.ToolChoice.Kind != "required" || !bytes.Equal(request.Tools[0].InputSchema, tc.WriteFileDefinition().InputSchema) {
			t.Fatal("captured complete write schema changed")
		}
		return &loopJSONCall{result: func(context.Context) (mc.ModelResponse, error) { return response.Clone(), nil }, close: func(context.Context) error { closes++; joined = true; return nil }, joined: func() bool { return joined }}, nil
	}}
	read, err := NewToolCallController(caller)
	requestOK(t, err)
	_, err = read.Accept(r)
	loopCode(t, err, f.CapabilityUnsupported)
	plain, err := NewDirectTextController(caller)
	requestOK(t, err)
	_, err = plain.Accept(r)
	loopCode(t, err, f.CapabilityUnsupported)
	write, err := NewWriteFileToolCallController(caller)
	requestOK(t, err)
	_, err = write.Accept(firstToolRequest(t))
	loopCode(t, err, f.CapabilityUnsupported)
	session, err := write.Accept(r)
	requestOK(t, err)
	got, err := session.Result(context.Background())
	requestOK(t, err)
	if !session.Joined() || begins != 1 || closes != 1 || !bytes.Equal(got.Message.Parts[0].ToolCall.Arguments, response.Message.Parts[0].ToolCall.Arguments) {
		t.Fatal("original complete write result/handle lost")
	}
	requestOK(t, write.Drain(context.Background()))
	requestOK(t, read.Drain(context.Background()))
	requestOK(t, plain.Drain(context.Background()))
}
func TestWriteFileToolCallRejectsWrongResponse(t *testing.T) {
	r := firstWriteToolRequest(t)
	good := writeToolResponse(t, r, nil)
	requestOK(t, validateSessionResponse(r, good))
	for name, mutate := range map[string]func(*mc.ModelResponse){
		"name":     func(v *mc.ModelResponse) { v.Message.Parts[0].ToolCall.Name = "other" },
		"call":     func(v *mc.ModelResponse) { v.CallID = requestID[mc.Call](t, 999) },
		"finish":   func(v *mc.ModelResponse) { v.FinishReason = "stop" },
		"parallel": func(v *mc.ModelResponse) { v.Message.Parts = append(v.Message.Parts, v.Message.Parts[0]) },
		"read-arguments": func(v *mc.ModelResponse) {
			v.Message.Parts[0].ToolCall.Arguments = firstToolResponse(t, firstToolRequest(t)).Message.Parts[1].ToolCall.Arguments
		},
		"missing-overwrite": func(v *mc.ModelResponse) {
			v.Message.Parts[0].ToolCall.Arguments = json.RawMessage(`{"mount_id":"01900000-0000-7000-8000-000000000007","path":"file","data_base64":""}`)
		},
	} {
		t.Run(name, func(t *testing.T) {
			v := good.Clone()
			mutate(&v)
			if validateSessionResponse(r, v) == nil {
				t.Fatal("invalid write response accepted")
			}
		})
	}
}
