package contract

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func runnerContractID[T any](t *testing.T, n int) f.ID[T] {
	t.Helper()
	v, err := f.ParseID[T](fmt.Sprintf("01900000-0000-7000-8000-%012x", n))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestToolRunnerBindingPreservesBuiltinEncodingAndClosedChoice(t *testing.T) {
	b := BuiltinExecutionBinding{Binding: BuiltinBinding{HandlerID: "skill.install", ContractRevision: 1}, ScopeResolverID: "skill.install", RiskClassifierID: "skill.install", Class: OrdinaryTool}
	raw, err := json.Marshal(b)
	if err != nil || string(raw) != `{"Binding":{"handler_id":"skill.install","contract_revision":"1"},"ScopeResolverID":"skill.install","RiskClassifierID":"skill.install","Class":"ordinary"}` {
		t.Fatal("legacy Builtin capture encoding changed")
	}
	r := ReadFileRegistration()
	b = BuiltinExecutionBinding{RunnerBinding: r.Binding, ScopeResolverID: r.ScopeResolverID, RiskClassifierID: r.RiskClassifierID, Class: r.Class}
	raw, err = json.Marshal(b)
	if err != nil || string(raw) != `{"RunnerBinding":{"operation_name":"read-file","operation_revision":"1","protocol_major":1},"ScopeResolverID":"runner.read-file","RiskClassifierID":"runner.read-file","Class":"ordinary"}` {
		t.Fatal("Runner binding is not a distinct closed branch")
	}
	tool := runnerContractID[id.Tool](t, 1)
	v := ExecutionTool{ToolID: tool, SpecRevision: 1, ModelVisibleName: "tool_" + strings.ReplaceAll(tool.String(), "-", ""), BindingSnapshot: b}
	if v.Validate() != nil {
		t.Fatal("fixed Runner capture rejected")
	}
	for _, mutate := range []func(*ExecutionTool){
		func(v *ExecutionTool) { v.BindingSnapshot.Binding = BuiltinBinding{"skill.install", 1} },
		func(v *ExecutionTool) { v.BindingSnapshot.RunnerBinding = RunnerBinding{} },
		func(v *ExecutionTool) { v.BindingSnapshot.RunnerBinding.OperationName = "list-directory" },
		func(v *ExecutionTool) { v.BindingSnapshot.RunnerBinding.OperationRevision = "01" },
		func(v *ExecutionTool) { v.BindingSnapshot.RunnerBinding.ProtocolMajor = 2 },
		func(v *ExecutionTool) { v.BindingSnapshot.ScopeResolverID = "forged" },
		func(v *ExecutionTool) { v.BindingSnapshot.Class = CoreTool },
	} {
		bad := v
		mutate(&bad)
		if bad.Validate() == nil {
			t.Fatal("unsupported or ambiguous Runner capture accepted")
		}
	}
	if r.Validate(context.Background()) != nil {
		t.Fatal("fixed source rejected")
	}
	r.Definition.InputSchema = []byte(`{"type":"object"}`)
	if r.Validate(context.Background()) == nil {
		t.Fatal("weakened source schema accepted")
	}
	if (BuiltinRegistration{Definition: ReadFileDefinition(), Binding: BuiltinBinding{"read-file", 1}, ScopeResolverID: "runner.read-file", RiskClassifierID: "runner.read-file", Class: OrdinaryTool}).Validate(context.Background()) == nil {
		t.Fatal("Runner definition disguised as Builtin")
	}
}

func TestToolRunnerCallBindingDoesNotBroadenBuiltin(t *testing.T) {
	p := runnerContractID[id.Project](t, 1)
	a := runnerContractID[id.Agent](t, 2)
	e := runnerContractID[id.Execution](t, 3)
	actor, err := id.NewAgentRun(p, a, e)
	if err != nil {
		t.Fatal(err)
	}
	tool := runnerContractID[id.Tool](t, 4)
	digest, _ := f.ParseDigest("sha256:" + strings.Repeat("a", 64))
	b := ToolCallBinding{Actor: actor, ProjectID: p, AgentID: a, ExecutionID: e, RoundID: runnerContractID[struct{}](t, 5).String(), SnapshotID: runnerContractID[struct{}](t, 6).String(), InputBindingID: runnerContractID[struct{}](t, 7).String(), LogicalCallID: runnerContractID[mc.Call](t, 8), InvocationID: runnerContractID[mc.Invocation](t, 9), CallID: "provider-call", ModelVisibleName: "tool_" + strings.ReplaceAll(tool.String(), "-", ""), Spec: SpecRef{ToolID: tool, SpecRevision: 1}, RunnerBinding: ReadFileRegistration().Binding, Input: ToolInputReference{PayloadID: runnerContractID[struct{}](t, 10).String(), SHA256: digest, Bytes: 4}, CanonicalArguments: digest}
	if b.Validate() != nil {
		t.Fatal("Runner call projection rejected")
	}
	b.Binding = BuiltinBinding{"skill.install", 1}
	if b.Validate() == nil {
		t.Fatal("ambiguous call accepted")
	}
	b.RunnerBinding = RunnerBinding{}
	if b.Validate() != nil {
		t.Fatal("legacy skill.install rejected")
	}
	b.Binding.HandlerID = "another.builtin"
	if b.Validate() == nil {
		t.Fatal("old Builtin runtime scope broadened")
	}
}

func TestToolRunnerReadFileArgumentsAreClosedAndProtected(t *testing.T) {
	good := `{"mount_id":"01900000-0000-7000-8000-000000000001","path":"private-file-canary/a.txt","offset":"0","length":64,"encoding":"utf8"}`
	v, err := DecodeReadFileToolArguments(context.Background(), []byte(good))
	if err != nil || v.Arguments.Path != "private-file-canary/a.txt" || v.Arguments.Offset != 0 || v.Arguments.Length != 64 || v.Arguments.Encoding != "utf8" {
		t.Fatal("valid bounded intent changed")
	}
	for _, raw := range []string{
		strings.Replace(good, `"offset":"0"`, `"offset":0`, 1),
		strings.Replace(good, `"offset":"0"`, `"offset":"00"`, 1),
		strings.Replace(good, `"offset":"0"`, `"offset":"9223372036854775807"`, 1),
		strings.Replace(good, `"length":64`, `"length":null`, 1),
		strings.Replace(good, `"length":64`, `"length":65537`, 1),
		strings.Replace(good, `"length":64`, `"length":64,"length":1`, 1),
		strings.Replace(good, `"encoding":"utf8"`, `"encoding":"utf8","workspace_name":"private"`, 1),
		strings.Replace(good, `"path":"private-file-canary/a.txt"`, `"path":"../outside"`, 1),
		strings.Replace(good, `"path":"private-file-canary/a.txt"`, `"path":"/etc/passwd"`, 1),
		strings.Replace(good, `"path":"private-file-canary/a.txt"`, `"path":"\ud800"`, 1),
		strings.Replace(good, `,"encoding":"utf8"`, ``, 1), good + ` {}`,
	} {
		got, err := DecodeReadFileToolArguments(context.Background(), []byte(raw))
		if err == nil || got != (ReadFileToolArguments{}) {
			t.Fatal("invalid Tool intent retained or accepted")
		}
	}
	raw, err := json.Marshal(v)
	if err != nil || strings.Contains(string(raw)+fmt.Sprintf("%+v %#v", v, v), "private-file-canary") {
		t.Fatal("protected intent leaked through default output")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = DecodeReadFileToolArguments(ctx, []byte(good)); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled decoder advanced")
	}
}
