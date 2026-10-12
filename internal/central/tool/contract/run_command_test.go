package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func TestToolRunCommandRegistrationAndCapturedProfile(t *testing.T) {
	ctx := context.Background()
	v := RunCommandRegistration()
	if v.Validate(ctx) != nil || RunCommandRisk != "arbitrary_execution" || v.Definition.Annotations.ReadOnly || !v.Definition.Annotations.Destructive || v.Definition.Annotations.Idempotency != None {
		t.Fatal("command lost arbitrary-execution/non-idempotent classification")
	}
	for _, other := range []RunnerRegistration{ReadFileRegistration(), WriteFileRegistration()} {
		if other.Validate(ctx) != nil {
			t.Fatal("legacy profile rejected")
		}
		for _, part := range []string{"binding", "definition", "scope", "risk"} {
			t.Run(other.Binding.OperationName+"-"+part, func(t *testing.T) {
				bad := RunCommandRegistration()
				switch part {
				case "binding":
					bad.Binding = other.Binding
				case "definition":
					bad.Definition = other.Definition
				case "scope":
					bad.ScopeResolverID = other.ScopeResolverID
				case "risk":
					bad.RiskClassifierID = other.RiskClassifierID
				}
				if bad.Validate(ctx) == nil {
					t.Fatal("command accepted mixed file profile")
				}
			})
		}
	}
	for name, mutate := range map[string]func(*RunnerRegistration){
		"revision": func(v *RunnerRegistration) { v.Binding.OperationRevision = "2" },
		"protocol": func(v *RunnerRegistration) { v.Binding.ProtocolMajor = 2 },
		"core":     func(v *RunnerRegistration) { v.Class = CoreTool },
		"schema":   func(v *RunnerRegistration) { v.Definition.InputSchema = []byte(`{"type":"object"}`) },
		"retry":    func(v *RunnerRegistration) { v.Definition.Annotations = &Annotations{Idempotency: Inherent} },
	} {
		t.Run(name, func(t *testing.T) {
			bad := RunCommandRegistration()
			mutate(&bad)
			if bad.Validate(ctx) == nil {
				t.Fatal("changed command contract accepted")
			}
		})
	}
	tool := runnerContractID[id.Tool](t, 27)
	capture := ExecutionTool{ToolID: tool, SpecRevision: 1, ModelVisibleName: "tool_" + strings.ReplaceAll(tool.String(), "-", ""), BindingSnapshot: BuiltinExecutionBinding{RunnerBinding: v.Binding, ScopeResolverID: v.ScopeResolverID, RiskClassifierID: v.RiskClassifierID, Class: v.Class}}
	if capture.Validate() != nil {
		t.Fatal("exact command capture rejected")
	}
	for _, other := range []RunnerRegistration{ReadFileRegistration(), WriteFileRegistration()} {
		wrong := capture
		wrong.BindingSnapshot.RunnerBinding = other.Binding
		if wrong.Validate() == nil {
			t.Fatal("capture accepted mixed command scope")
		}
	}
	if (BuiltinRegistration{Definition: v.Definition, Binding: BuiltinBinding{"skill.install", 1}, ScopeResolverID: v.ScopeResolverID, RiskClassifierID: v.RiskClassifierID, Class: v.Class}).Validate(ctx) == nil {
		t.Fatal("command disguised as Builtin")
	}
}

func commandToolRaw(t *testing.T, args []string) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"mount_id": "01900000-0000-7000-8000-000000000001", "mode": "argv", "program": "/bin/bash", "args": args, "cwd": "."})
	if err != nil {
		t.Fatal("fixture encode failed")
	}
	return raw
}

func TestToolRunCommandArgumentsPreserveArgvAndBounds(t *testing.T) {
	ctx := context.Background()
	for _, args := range [][]string{{}, {"-c", "printf '%s' 'a b'", "$HOME", "*", "", "中文🙂"}, {strings.Repeat("<", p.MaxRunCommandArgumentBytes-len("/bin/bash"))}, make([]string, p.MaxRunCommandArguments)} {
		raw := commandToolRaw(t, args)
		got, err := DecodeRunCommandToolArguments(ctx, raw)
		if err != nil || !reflect.DeepEqual(got.Arguments.Args, args) || got.Arguments.Program != "/bin/bash" || got.Arguments.Mode != "argv" || got.Arguments.CWD != "." {
			t.Fatal("explicit argv was reparsed or rejected", err)
		}
		payload, err := p.EncodeRunCommandPayload(p.RunCommandPayload{WorkspaceName: "owned", Arguments: got.Arguments})
		if err != nil {
			t.Fatal("real protocol rejected command", err)
		}
		const first p.ID = "01900000-0000-7000-8000-000000000001"
		const second p.ID = "01900000-0000-7000-8000-000000000002"
		const third p.ID = "01900000-0000-7000-8000-000000000003"
		request := p.Request{RequestID: first, OperationID: second, ExecutionID: third, ProjectID: first, AgentID: second, Mount: p.Mount{MountID: first, WorkspaceID: second}, OperationName: p.RunCommandOperation, OperationRevision: p.RunCommandRevision, Payload: payload}
		message, err := p.NewMessage(p.Header{ProtocolVersion: p.CurrentVersion(), MessageID: third, Timestamp: "2026-10-12T01:02:03Z", RequestID: first, OperationID: second}, request)
		if err != nil {
			t.Fatal("actual command envelope invalid", err)
		}
		wire, err := p.Encode(message)
		if err != nil || len(wire) > p.MaxMessageBytes {
			t.Fatal("actual whole message exceeded bound", err)
		}
		decoded, err := p.Decode(wire)
		if err != nil {
			t.Fatal("actual command frame invalid", err)
		}
		round, err := p.DecodeRunCommandPayload(decoded.Payload().(p.Request).Payload)
		if err != nil || !reflect.DeepEqual(round.Arguments, got.Arguments) {
			t.Fatal("actual command frame changed intent", err)
		}
	}
	for _, args := range [][]string{{strings.Repeat("x", p.MaxRunCommandArgumentBytes-len("/bin/bash")+1)}, make([]string, p.MaxRunCommandArguments+1)} {
		if got, err := DecodeRunCommandToolArguments(ctx, commandToolRaw(t, args)); err == nil || !reflect.DeepEqual(got, RunCommandToolArguments{}) {
			t.Fatal("excess argv accepted or retained")
		}
	}
	if got, err := DecodeRunCommandToolArguments(ctx, bytes.Repeat([]byte(" "), p.MaxMessageBytes+1)); err == nil || !reflect.DeepEqual(got, RunCommandToolArguments{}) {
		t.Fatal("raw message budget ignored")
	}
	// Enlarging this independent profile does not change legacy read/schema gates.
	read := `{"mount_id":"01900000-0000-7000-8000-000000000001","path":"x","offset":"0","length":1,"encoding":"utf8"}`
	if _, err := DecodeReadFileToolArguments(ctx, []byte(read+strings.Repeat(" ", 32<<10))); err == nil {
		t.Fatal("old read budget changed")
	}
	if _, err := canonicalSchema(ctx, []byte(`{}`+strings.Repeat(" ", MaxSchemaBytes))); err == nil {
		t.Fatal("schema budget changed")
	}
}

func TestToolRunCommandArgumentsRejectCallerAuthorityAndMalformed(t *testing.T) {
	good := `{"mount_id":"01900000-0000-7000-8000-000000000001","mode":"argv","program":"/bin/bash","args":["private-command-canary"],"cwd":"."}`
	bad := map[string]string{
		"null-argv":        strings.Replace(good, `["private-command-canary"]`, `null`, 1),
		"null-item":        strings.Replace(good, `["private-command-canary"]`, `[null]`, 1),
		"missing-mode":     strings.Replace(good, `"mode":"argv",`, ``, 1),
		"shell-mode":       strings.Replace(good, `"argv"`, `"shell"`, 1),
		"duplicate":        strings.Replace(good, `"cwd":"."`, `"cwd":".","c\u0077d":"."`, 1),
		"case-alias":       strings.Replace(good, `"cwd"`, `"CWD"`, 1),
		"surrogate":        strings.Replace(good, `"private-command-canary"`, `"\ud800"`, 1),
		"nul":              strings.Replace(good, `"private-command-canary"`, `"a\u0000b"`, 1),
		"relative-program": strings.Replace(good, `"/bin/bash"`, `"bash"`, 1),
		"escape-cwd":       strings.Replace(good, `"cwd":"."`, `"cwd":"../other"`, 1),
		"trailing":         good + `{}`,
		"read":             `{"mount_id":"01900000-0000-7000-8000-000000000001","path":"x","offset":"0","length":1,"encoding":"utf8"}`,
		"write":            `{"mount_id":"01900000-0000-7000-8000-000000000001","path":"x","data_base64":"","overwrite":false}`,
	}
	for _, field := range []string{`"workspace_name":"forged"`, `"runner_id":"forged"`, `"env":{}`, `"approval_policy":"allow"`, `"timeout":1`, `"command":"echo"`} {
		bad[field] = strings.TrimSuffix(good, "}") + "," + field + "}"
	}
	for name, raw := range bad {
		t.Run(name, func(t *testing.T) {
			got, err := DecodeRunCommandToolArguments(context.Background(), []byte(raw))
			if err == nil || !reflect.DeepEqual(got, RunCommandToolArguments{}) {
				t.Fatal("invalid or caller-authorized intent retained")
			}
		})
	}
	got, err := DecodeRunCommandToolArguments(context.Background(), []byte(good))
	if err != nil {
		t.Fatal("valid command rejected")
	}
	encoded, err := json.Marshal(got)
	if err != nil || strings.Contains(string(encoded)+fmt.Sprintf("%+v %#v", got, got)+got.LogValue().String(), "private-command-canary") {
		t.Fatal("default formatting disclosed argv")
	}
	for _, decode := range []func(context.Context, []byte) error{
		func(ctx context.Context, b []byte) error { _, e := DecodeReadFileToolArguments(ctx, b); return e },
		func(ctx context.Context, b []byte) error { _, e := DecodeWriteFileToolArguments(ctx, b); return e },
	} {
		if decode(context.Background(), []byte(good)) == nil {
			t.Fatal("file decoder accepted command")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := DecodeRunCommandToolArguments(ctx, []byte(good)); !errors.Is(err, context.Canceled) || !reflect.DeepEqual(got, RunCommandToolArguments{}) {
		t.Fatal("cancelled parse published argv")
	}
	if got, err := DecodeRunCommandToolArguments(nil, []byte(good)); err == nil || !reflect.DeepEqual(got, RunCommandToolArguments{}) {
		t.Fatal("nil context accepted")
	}
	if _, err := DecodeRunCommandToolArguments(context.Background(), []byte(`{"cwd":".","args":[],"program":"/bin/bash","mode":"argv","mount_id":"01900000-0000-7000-8000-000000000001"}`)); err != nil {
		t.Fatal("legal member order rejected")
	}
}
