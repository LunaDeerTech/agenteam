package agentloop

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func toolResultTestDigest(raw []byte) f.Digest {
	sum := sha256.Sum256(raw)
	return f.Digest("sha256:" + hex.EncodeToString(sum[:]))
}

// This is a controlled codec value, not a substitute for the real Tool reader
// or evidence that a result has been committed and remains authorized.
func toolResultTestBinding(t *testing.T, raw []byte) tc.ReadFileResultBinding {
	t.Helper()
	project, agent, execution := requestID[i.Project](t, 1001), requestID[i.Agent](t, 1002), requestID[i.Execution](t, 1003)
	actor, err := i.NewAgentRun(project, agent, execution)
	requestOK(t, err)
	tool := requestID[i.Tool](t, 1004)
	digest := toolResultTestDigest([]byte("protected-codec-reference"))
	v := tc.ReadFileResultBinding{
		Call: tc.ToolCallBinding{
			Actor: actor, ProjectID: project, AgentID: agent, ExecutionID: execution,
			RoundID: requestID[struct{}](t, 1005).String(), SnapshotID: requestID[struct{}](t, 1006).String(), InputBindingID: requestID[struct{}](t, 1007).String(),
			LogicalCallID: requestID[mc.Call](t, 1008), InvocationID: requestID[mc.Invocation](t, 1009), CallID: "original-call-中文",
			ModelVisibleName: "tool_" + strings.ReplaceAll(tool.String(), "-", ""), Spec: tc.SpecRef{ToolID: tool, SpecRevision: 1},
			RunnerBinding: tc.ReadFileRegistration().Binding,
			Input:         tc.ToolInputReference{PayloadID: requestID[struct{}](t, 1010).String(), SHA256: digest, Bytes: 64}, CanonicalArguments: digest,
		},
		OperationID: requestID[tc.Operation](t, 1011), AttemptID: requestID[tc.Attempt](t, 1012), RequestID: p.ID(requestID[struct{}](t, 1013).String()),
		InputDigest: digest, Fingerprint: digest, TerminalDigest: digest, Outcome: "success", Attempted: true,
		ResultDigest: toolResultTestDigest(raw), ResultBytes: f.Progress(len(raw)),
	}
	requestOK(t, v.Validate())
	return v
}

func TestReadFileToolResultCompleteProjection(t *testing.T) {
	for name, result := range map[string]p.ReadFileResult{
		"utf8":           {Path: "文档/README.md", Offset: 9007199254740993, BytesRead: len("完整🙂\n<%_!>"), EOF: true, Encoding: "utf8", Data: "完整🙂\n<%_!>"},
		"maximum base64": {Path: "data.bin", BytesRead: p.MaxReadFileBytes, Encoding: "base64", Data: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0xff}, p.MaxReadFileBytes))},
		"empty eof":      {Path: "empty", EOF: true, Encoding: "utf8"},
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := p.EncodeReadFileResult(result)
			requestOK(t, err)
			binding := toolResultTestBinding(t, raw)
			projection, err := ProjectReadFileToolResult(context.Background(), binding, raw)
			requestOK(t, err)
			requestOK(t, projection.Validate())
			message := projection.Message()
			requestOK(t, message.Validate())
			if message.Role != "tool" || len(message.Parts) != 1 || message.Parts[0].ToolResult == nil {
				t.Fatal("missing single tool result")
			}
			part := message.Parts[0].ToolResult
			if part.CallID != binding.Call.CallID || part.IsError || len(part.Parts) != 1 || part.Parts[0].Text == nil || part.Parts[0].Text.Text != string(raw) {
				t.Fatal("original result or call identity changed")
			}
			encoded, err := json.Marshal(message)
			requestOK(t, err)
			canonical, err := cursor.CanonicalJSON(encoded)
			requestOK(t, err)
			if !bytes.Equal(canonical, projection.CanonicalBytes()) || projection.Digest() != toolResultTestDigest(canonical) {
				t.Fatal("canonical message binding mismatch")
			}
			for _, backend := range []string{binding.OperationID.String(), binding.AttemptID.String(), string(binding.RequestID), string(binding.TerminalDigest)} {
				if bytes.Contains(canonical, []byte(backend)) {
					t.Fatal("backend identity leaked into model body")
				}
			}
			original := string(raw)
			clear(raw)
			message.Parts[0].ToolResult.CallID = "changed"
			message.Parts[0].ToolResult.Parts[0].Text.Text = "changed"
			copyBytes := projection.CanonicalBytes()
			clear(copyBytes)
			if projection.Message().Parts[0].ToolResult.Parts[0].Text.Text != original || projection.Message().Parts[0].ToolResult.CallID != binding.Call.CallID || !bytes.Equal(canonical, projection.CanonicalBytes()) {
				t.Fatal("borrowed bytes or accessor mutation changed projection")
			}
		})
	}
}

func TestReadFileToolResultRejectsUnboundOrDamagedResult(t *testing.T) {
	raw, err := p.EncodeReadFileResult(p.ReadFileResult{Path: "private-file.txt", BytesRead: 4, EOF: true, Encoding: "utf8", Data: "data"})
	requestOK(t, err)
	for name, mutate := range map[string]func(*tc.ReadFileResultBinding){
		"digest":               func(v *tc.ReadFileResultBinding) { v.ResultDigest = toolResultTestDigest([]byte("other")) },
		"length":               func(v *tc.ReadFileResultBinding) { v.ResultBytes++ },
		"call":                 func(v *tc.ReadFileResultBinding) { v.Call.CallID = "" },
		"non-read-file":        func(v *tc.ReadFileResultBinding) { v.Call.RunnerBinding.OperationName = "other" },
		"failure":              func(v *tc.ReadFileResultBinding) { v.Outcome = "failure"; v.Code = "READ_FAILED" },
		"unknown":              func(v *tc.ReadFileResultBinding) { v.Outcome = "unknown" },
		"cancelled":            func(v *tc.ReadFileResultBinding) { v.Outcome = "cancelled" },
		"phase is not outcome": func(v *tc.ReadFileResultBinding) { v.Outcome = "succeeded" },
		"not attempted":        func(v *tc.ReadFileResultBinding) { v.Attempted = false },
	} {
		t.Run(name, func(t *testing.T) {
			binding := toolResultTestBinding(t, raw)
			mutate(&binding)
			got, err := ProjectReadFileToolResult(context.Background(), binding, raw)
			if err == nil || got.Validate() == nil {
				t.Fatal("invalid or non-success result published")
			}
		})
	}
	for name, damaged := range map[string][]byte{
		"noncanonical whitespace": append([]byte(" "), raw...),
		"unknown field":           bytes.Replace(raw, []byte(`{"path"`), []byte(`{"internal":"hidden","path"`), 1),
		"invalid byte count":      bytes.Replace(raw, []byte(`"bytes_read":4`), []byte(`"bytes_read":3`), 1),
		"invalid utf8":            bytes.Replace(raw, []byte("data"), []byte{0xff}, 1),
		"incomplete":              raw[:len(raw)-1],
	} {
		t.Run(name, func(t *testing.T) {
			binding := toolResultTestBinding(t, damaged)
			got, err := ProjectReadFileToolResult(context.Background(), binding, damaged)
			if err == nil || got.Validate() == nil || strings.Contains(err.Error(), "private-file") {
				t.Fatal("damaged protocol published or disclosed")
			}
		})
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, ctx := range []context.Context{nil, cancelled} {
		got, err := ProjectReadFileToolResult(ctx, toolResultTestBinding(t, raw), raw)
		if err == nil || got.Validate() == nil {
			t.Fatal("inactive context published a projection")
		}
	}
}

func TestReadFileToolResultDefaultsRedactContent(t *testing.T) {
	raw, err := p.EncodeReadFileResult(p.ReadFileResult{Path: "sensitive-path", BytesRead: len("private-content"), EOF: true, Encoding: "utf8", Data: "private-content"})
	requestOK(t, err)
	projection, err := ProjectReadFileToolResult(context.Background(), toolResultTestBinding(t, raw), raw)
	requestOK(t, err)
	encoded, err := json.Marshal(projection)
	requestOK(t, err)
	if string(encoded) != `"agent_tool_result_projection"` {
		t.Fatal("default JSON includes result")
	}
	var log bytes.Buffer
	slog.New(slog.NewJSONHandler(&log, nil)).Info("projection", "value", projection)
	for _, value := range []string{fmt.Sprintf("%+v %#v %s", projection, projection, projection), log.String()} {
		if strings.Contains(value, "private-content") || strings.Contains(value, "sensitive-path") || !strings.Contains(value, "agent_tool_result_projection") {
			t.Fatal("default output disclosed protected result")
		}
	}
	var zero ToolResultProjection
	if zero.Validate() == nil || len(zero.CanonicalBytes()) != 0 || zero.Digest() != "" || zero.Message().Validate() == nil || json.Unmarshal(projection.CanonicalBytes(), &zero) == nil {
		t.Fatal("zero or decoded value became a projection")
	}
}
