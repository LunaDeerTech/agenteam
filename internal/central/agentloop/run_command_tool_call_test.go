package agentloop

import (
	"bytes"
	"context"
	"encoding/json"
	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	"strings"
	"testing"
)

func firstCommandToolRequest(t *testing.T) DirectTextRequest {
	t.Helper()
	v := requestFields(t, "task/work")
	id := requestID[i.Tool](t, 901)
	reg := tc.RunCommandRegistration()
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
		t.Fatal("read builder accepted command capture")
	}
	request, err := BuildRunCommandToolCallRequest(context.Background(), captured, actor, round, call, reg.Definition)
	requestOK(t, err)
	return request
}
func commandToolResponse(t *testing.T, r DirectTextRequest, args []string) mc.ModelResponse {
	t.Helper()
	raw, err := json.MarshalIndent(struct {
		Mount   string   `json:"mount_id"`
		Mode    string   `json:"mode"`
		Program string   `json:"program"`
		Args    []string `json:"args"`
		CWD     string   `json:"cwd"`
	}{"01900000-0000-7000-8000-000000000007", "argv", "/bin/bash", args, "."}, "", "  ")
	requestOK(t, err)
	return mc.ModelResponse{CallID: r.Request().CallID, InvocationID: requestID[mc.Invocation](t, 904), Message: mc.Message{Role: "assistant", Parts: []mc.MessagePart{{ToolCall: &mc.ToolCallPart{ID: "command-call", Name: r.Request().Tools[0].Name, Arguments: raw}}}}, FinishReason: "tool_calls", Usage: mc.Usage{Source: mc.UnknownUsage}}
}
func TestRunCommandToolCallExplicitProfileAndOriginalArgv(t *testing.T) {
	r := firstCommandToolRequest(t)
	response := commandToolResponse(t, r, []string{strings.Repeat("<", p.MaxRunCommandArgumentBytes-len("/bin/bash"))})
	begins, closes := 0, 0
	joined := false
	caller := &loopJSONCaller{begin: func(_ context.Context, got mc.ModelRequest) (mc.JSONCall, error) {
		begins++
		if got.Input.Digest != r.Request().Input.Digest || len(got.Tools) != 1 || got.ToolChoice.Kind != "required" || !bytes.Equal(got.Tools[0].InputSchema, tc.RunCommandDefinition().InputSchema) {
			t.Fatal("complete command schema/name binding changed")
		}
		return &loopJSONCall{result: func(context.Context) (mc.ModelResponse, error) { return response.Clone(), nil }, close: func(context.Context) error { closes++; joined = true; return nil }, joined: func() bool { return joined }}, nil
	}}
	read, err := NewToolCallController(caller)
	requestOK(t, err)
	write, err := NewWriteFileToolCallController(caller)
	requestOK(t, err)
	plain, err := NewDirectTextController(caller)
	requestOK(t, err)
	command, err := NewRunCommandToolCallController(caller)
	requestOK(t, err)
	_, err = read.Accept(r)
	loopCode(t, err, f.CapabilityUnsupported)
	_, err = write.Accept(r)
	loopCode(t, err, f.CapabilityUnsupported)
	_, err = plain.Accept(r)
	loopCode(t, err, f.CapabilityUnsupported)
	_, err = command.Accept(firstToolRequest(t))
	loopCode(t, err, f.CapabilityUnsupported)
	_, err = command.Accept(firstWriteToolRequest(t))
	loopCode(t, err, f.CapabilityUnsupported)
	if begins != 0 {
		t.Fatal("wrong profile began Model work")
	}
	session, err := command.Accept(r)
	requestOK(t, err)
	got, err := session.Result(context.Background())
	requestOK(t, err)
	if !session.Joined() || begins != 1 || closes != 1 || !bytes.Equal(got.Message.Parts[0].ToolCall.Arguments, response.Message.Parts[0].ToolCall.Arguments) {
		t.Fatal("command altered original argv bytes or skipped actual handle retirement")
	}
	requestOK(t, command.Drain(context.Background()))
	requestOK(t, read.Drain(context.Background()))
	requestOK(t, write.Drain(context.Background()))
	requestOK(t, plain.Drain(context.Background()))
}
func TestRunCommandToolCallRejectsCallerAuthorityAndWrongShape(t *testing.T) {
	r := firstCommandToolRequest(t)
	good := commandToolResponse(t, r, []string{"-c", "printf '%s' '$HOME *'", ""})
	requestOK(t, validateSessionResponse(r, good))
	for name, mutate := range map[string]func(*mc.ModelResponse){
		"wrong-name": func(v *mc.ModelResponse) { v.Message.Parts[0].ToolCall.Name = "other" },
		"wrong-call": func(v *mc.ModelResponse) { v.CallID = requestID[mc.Call](t, 999) },
		"parallel":   func(v *mc.ModelResponse) { v.Message.Parts = append(v.Message.Parts, v.Message.Parts[0]) },
		"read-arguments": func(v *mc.ModelResponse) {
			v.Message.Parts[0].ToolCall.Arguments = firstToolResponse(t, firstToolRequest(t)).Message.Parts[1].ToolCall.Arguments
		},
		"write-arguments": func(v *mc.ModelResponse) {
			v.Message.Parts[0].ToolCall.Arguments = writeToolResponse(t, firstWriteToolRequest(t), nil).Message.Parts[0].ToolCall.Arguments
		},
		"env-assertion": func(v *mc.ModelResponse) {
			v.Message.Parts[0].ToolCall.Arguments = json.RawMessage(`{"mount_id":"01900000-0000-7000-8000-000000000007","mode":"argv","program":"/bin/bash","args":[],"cwd":".","env":{}}`)
		},
		"approval-assertion": func(v *mc.ModelResponse) {
			v.Message.Parts[0].ToolCall.Arguments = json.RawMessage(`{"mount_id":"01900000-0000-7000-8000-000000000007","mode":"argv","program":"/bin/bash","args":[],"cwd":".","approved":true}`)
		},
	} {
		t.Run(name, func(t *testing.T) {
			v := good.Clone()
			mutate(&v)
			if validateSessionResponse(r, v) == nil {
				t.Fatal("untrusted or wrong-profile command response accepted")
			}
		})
	}
}
