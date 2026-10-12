package contract

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func TestToolWriteFileRegistrationClosedAndReadCompatible(t *testing.T) {
	ctx := context.Background()
	write, read := WriteFileRegistration(), ReadFileRegistration()
	if write.Validate(ctx) != nil || read.Validate(ctx) != nil || write.Definition.Annotations.ReadOnly || !write.Definition.Annotations.Destructive || write.Definition.Annotations.Idempotency != None {
		t.Fatal("write profile is not an ordinary explicit mutation without retry promise")
	}
	tool := runnerContractID[id.Tool](t, 19)
	metadata := ExecutionTool{ToolID: tool, SpecRevision: 1, ModelVisibleName: "tool_" + strings.ReplaceAll(tool.String(), "-", ""), BindingSnapshot: BuiltinExecutionBinding{RunnerBinding: write.Binding, ScopeResolverID: write.ScopeResolverID, RiskClassifierID: write.RiskClassifierID, Class: write.Class}}
	if metadata.Validate() != nil {
		t.Fatal("write capture metadata rejected")
	}
	for name, mutate := range map[string]func(*RunnerRegistration){
		"read-binding":          func(v *RunnerRegistration) { v.Binding = read.Binding },
		"read-definition":       func(v *RunnerRegistration) { v.Definition = read.Definition },
		"read-scope":            func(v *RunnerRegistration) { v.ScopeResolverID = read.ScopeResolverID },
		"read-risk":             func(v *RunnerRegistration) { v.RiskClassifierID = read.RiskClassifierID },
		"core":                  func(v *RunnerRegistration) { v.Class = CoreTool },
		"unsupported-operation": func(v *RunnerRegistration) { v.Binding.OperationName = "delete-file" },
		"revision":              func(v *RunnerRegistration) { v.Binding.OperationRevision = "2" },
		"protocol":              func(v *RunnerRegistration) { v.Binding.ProtocolMajor = 2 },
		"weakened-schema":       func(v *RunnerRegistration) { v.Definition.InputSchema = []byte(`{"type":"object"}`) },
		"read-only": func(v *RunnerRegistration) {
			v.Definition.Annotations = &Annotations{ReadOnly: true, Idempotency: Inherent}
		},
	} {
		t.Run(name, func(t *testing.T) {
			bad := WriteFileRegistration()
			mutate(&bad)
			if bad.Validate(ctx) == nil {
				t.Fatal("mixed or weakened registration accepted")
			}
		})
	}
	for _, binding := range []RunnerBinding{read.Binding, write.Binding} {
		bad := metadata
		bad.BindingSnapshot.RunnerBinding = binding
		if binding == read.Binding {
			if bad.Validate() == nil {
				t.Fatal("read binding accepted write scope")
			}
		} else {
			bad.BindingSnapshot.ScopeResolverID = read.ScopeResolverID
			if bad.Validate() == nil {
				t.Fatal("write binding accepted read scope")
			}
		}
	}
	if (BuiltinRegistration{Definition: write.Definition, Binding: BuiltinBinding{"skill.install", 1}, ScopeResolverID: write.ScopeResolverID, RiskClassifierID: write.RiskClassifierID, Class: write.Class}).Validate(ctx) == nil {
		t.Fatal("write disguised as Builtin")
	}
}

func writeToolRaw(t *testing.T, data []byte) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"mount_id": "01900000-0000-7000-8000-000000000001", "path": "private-write-canary.bin", "data_base64": base64.StdEncoding.EncodeToString(data), "overwrite": false})
	if err != nil {
		t.Fatal("fixture could not encode")
	}
	return raw
}

func TestToolWriteFileArgumentsBoundsAndProtectedBytes(t *testing.T) {
	for _, size := range []int{0, 1, 32 << 10, p.MaxWriteFileBytes} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			data := bytes.Repeat([]byte{255}, size)
			raw := writeToolRaw(t, data)
			got, err := DecodeWriteFileToolArguments(context.Background(), raw)
			if err != nil || !bytes.Equal(got.Arguments.Data, data) || got.Arguments.Overwrite || got.Arguments.Path != "private-write-canary.bin" {
				t.Fatal("exact write data or explicit false changed")
			}
			payload, err := p.EncodeWriteFilePayload(p.WriteFilePayload{WorkspaceName: "actual-workspace", Arguments: got.Arguments})
			if err != nil {
				t.Fatal("validated intent did not encode with real protocol")
			}
			roundtrip, err := p.DecodeWriteFilePayload(payload)
			if err != nil || !bytes.Equal(roundtrip.Arguments.Data, data) {
				t.Fatal("protocol handoff changed data")
			}
			redacted, err := json.Marshal(got)
			if err != nil || strings.Contains(string(redacted)+fmt.Sprintf("%+v %#v", got, got)+got.LogValue().String(), "private-write-canary") {
				t.Fatal("default output exposed protected intent")
			}
		})
	}
	// The same padded encoded size can contain one extra decoded byte.
	for _, size := range []int{p.MaxWriteFileBytes + 1, p.MaxWriteFileBytes + 3} {
		got, err := DecodeWriteFileToolArguments(context.Background(), writeToolRaw(t, make([]byte, size)))
		if err == nil || !reflect.DeepEqual(got, WriteFileToolArguments{}) {
			t.Fatal("decoded/encoded bound retained an invalid intent")
		}
	}
	if got, err := DecodeWriteFileToolArguments(context.Background(), bytes.Repeat([]byte(" "), p.MaxMessageBytes+1)); err == nil || !reflect.DeepEqual(got, WriteFileToolArguments{}) {
		t.Fatal("raw payload budget not enforced")
	}
	// Large *write* data does not increase either legacy parser's own budget.
	read := `{"mount_id":"01900000-0000-7000-8000-000000000001","path":"x","offset":"0","length":1,"encoding":"utf8"}`
	if _, err := DecodeReadFileToolArguments(context.Background(), []byte(read)); err != nil {
		t.Fatal("legacy read rejected")
	}
	if _, err := DecodeReadFileToolArguments(context.Background(), []byte(read+strings.Repeat(" ", 32<<10))); err == nil {
		t.Fatal("read budget broadened")
	}
	if _, err := canonicalSchema(context.Background(), []byte(`{}`+strings.Repeat(" ", MaxSchemaBytes))); err == nil {
		t.Fatal("definition schema budget broadened")
	}
}

func TestToolWriteFileArgumentsRejectMalformedAndReadProfile(t *testing.T) {
	good := `{"mount_id":"01900000-0000-7000-8000-000000000001","path":"f","data_base64":"/w==","overwrite":false}`
	for name, raw := range map[string]string{
		"missing-overwrite":    strings.Replace(good, `,"overwrite":false`, ``, 1),
		"null-overwrite":       strings.Replace(good, `false`, `null`, 1),
		"number-overwrite":     strings.Replace(good, `false`, `0`, 1),
		"duplicate":            strings.Replace(good, `"overwrite":false`, `"overwrite":false,"overwrite":true`, 1),
		"case-alias":           strings.Replace(good, `"overwrite"`, `"Overwrite"`, 1),
		"null-data":            strings.Replace(good, `"/w=="`, `null`, 1),
		"unknown-workspace":    strings.Replace(good, `"overwrite":false`, `"overwrite":false,"workspace_name":"forged"`, 1),
		"invalid-base64":       strings.Replace(good, `/w==`, `####`, 1),
		"unpadded":             strings.Replace(good, `/w==`, `/w`, 1),
		"url-base64":           strings.Replace(good, `/w==`, `_w==`, 1),
		"nonzero-padding-bits": strings.Replace(good, `/w==`, `/x==`, 1),
		"newline":              strings.Replace(good, `/w==`, `/w==\n`, 1),
		"parent-path":          strings.Replace(good, `"f"`, `"../f"`, 1),
		"surrogate":            strings.Replace(good, `"f"`, `"\ud800"`, 1),
		"trailing":             good + ` {}`,
		"read":                 `{"mount_id":"01900000-0000-7000-8000-000000000001","path":"f","offset":"0","length":1,"encoding":"utf8"}`,
	} {
		t.Run(name, func(t *testing.T) {
			got, err := DecodeWriteFileToolArguments(context.Background(), []byte(raw))
			if err == nil || !reflect.DeepEqual(got, WriteFileToolArguments{}) {
				t.Fatal("invalid write intent accepted or retained")
			}
		})
	}
	if _, err := DecodeReadFileToolArguments(context.Background(), []byte(good)); err == nil {
		t.Fatal("write accepted by read decoder")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := DecodeWriteFileToolArguments(ctx, []byte(good)); !errors.Is(err, context.Canceled) || !reflect.DeepEqual(got, WriteFileToolArguments{}) {
		t.Fatal("cancelled decode published intent")
	}
	if got, err := DecodeWriteFileToolArguments(nil, []byte(good)); err == nil || !reflect.DeepEqual(got, WriteFileToolArguments{}) {
		t.Fatal("nil context accepted")
	}
	// JSON object order is not a canonical-wire requirement.
	if _, err := DecodeWriteFileToolArguments(context.Background(), []byte(`{"overwrite":true,"data_base64":"","path":"empty","mount_id":"01900000-0000-7000-8000-000000000001"}`)); err != nil {
		t.Fatal("legal key order or empty file rejected")
	}
}
