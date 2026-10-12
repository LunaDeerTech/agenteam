package agentloop

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// Controlled codec material only; it is not a historical authority or a
// substitute for the real transactional result reader.
func writeToolResultTestBinding(t *testing.T, raw []byte) tc.WriteFileResultBinding {
	t.Helper()
	old := toolResultTestBinding(t, raw)
	old.Call.RunnerBinding = tc.WriteFileRegistration().Binding
	v := tc.WriteFileResultBinding{
		Call: old.Call, OperationID: old.OperationID, AttemptID: old.AttemptID, RequestID: old.RequestID,
		InputDigest: old.InputDigest, Fingerprint: old.Fingerprint, TerminalDigest: old.TerminalDigest,
		Outcome: "success", Attempted: true, ResultDigest: toolResultTestDigest(raw), ResultBytes: f.Progress(len(raw)),
	}
	requestOK(t, v.Validate())
	return v
}

func TestWriteFileToolResultPreservesCompleteReceipt(t *testing.T) {
	internal, cancelled, timeout := p.InternalError, p.CancelledCode, p.Timeout
	for name, result := range map[string]p.WriteFileResult{
		"durable":      {Path: "文档/file.bin", State: "committed", BytesWritten: p.MaxWriteFileBytes, Durable: true},
		"empty":        {Path: "empty", State: "committed", Durable: true},
		"late-io":      {Path: "file", State: "committed", BytesWritten: 9, CompletionCode: &internal},
		"late-cancel":  {Path: "file", State: "committed", BytesWritten: 9, Durable: true, CompletionCode: &cancelled},
		"late-timeout": {Path: "file", State: "committed", BytesWritten: 9, CompletionCode: &timeout},
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := p.EncodeWriteFileResult(result)
			requestOK(t, err)
			binding := writeToolResultTestBinding(t, raw)
			got, err := ProjectWriteFileToolResult(context.Background(), binding, raw)
			requestOK(t, err)
			requestOK(t, got.Validate())
			message := got.Message()
			if message.Role != "tool" || len(message.Parts) != 1 || message.Parts[0].ToolResult == nil {
				t.Fatal("receipt did not form a unique ToolResult")
			}
			part := message.Parts[0].ToolResult
			if part.CallID != binding.Call.CallID || part.IsError || len(part.Parts) != 1 || part.Parts[0].Text == nil || part.Parts[0].Text.Text != string(raw) {
				t.Fatal("publication receipt was changed or classified as an unpublished failure")
			}
			want := roundProjectionJSON(t, message)
			if !bytes.Equal(want, got.CanonicalBytes()) || got.Digest() != toolResultTestDigest(want) {
				t.Fatal("complete canonical message mismatch")
			}
			for _, forbidden := range []string{binding.OperationID.String(), binding.AttemptID.String(), string(binding.RequestID), binding.Call.Input.PayloadID, "data_base64"} {
				if bytes.Contains(want, []byte(forbidden)) {
					t.Fatal("nonreceipt material projected")
				}
			}
			clear(raw)
			message.Parts[0].ToolResult.Parts[0].Text.Text = "changed"
			clear(got.CanonicalBytes())
			if !bytes.Equal(want, got.CanonicalBytes()) {
				t.Fatal("borrowed or accessor bytes changed the projection")
			}
		})
	}
}

func TestWriteFileToolResultRejectsUnboundOrDamagedReceipt(t *testing.T) {
	raw, err := p.EncodeWriteFileResult(p.WriteFileResult{Path: "protected-path", State: "committed", BytesWritten: 4, Durable: true})
	requestOK(t, err)
	for name, mutate := range map[string]func(*tc.WriteFileResultBinding){
		"read-profile":  func(v *tc.WriteFileResultBinding) { v.Call.RunnerBinding = tc.ReadFileRegistration().Binding },
		"digest":        func(v *tc.WriteFileResultBinding) { v.ResultDigest = toolResultTestDigest([]byte("other")) },
		"length":        func(v *tc.WriteFileResultBinding) { v.ResultBytes++ },
		"call":          func(v *tc.WriteFileResultBinding) { v.Call.CallID = "" },
		"failure":       func(v *tc.WriteFileResultBinding) { v.Outcome = "failure" },
		"unknown":       func(v *tc.WriteFileResultBinding) { v.Outcome = "unknown" },
		"outer-code":    func(v *tc.WriteFileResultBinding) { v.Code = "failure" },
		"not-attempted": func(v *tc.WriteFileResultBinding) { v.Attempted = false },
	} {
		t.Run(name, func(t *testing.T) {
			binding := writeToolResultTestBinding(t, raw)
			mutate(&binding)
			got, err := ProjectWriteFileToolResult(context.Background(), binding, raw)
			if err == nil || got.Validate() == nil {
				t.Fatal("unbound receipt accepted")
			}
		})
	}
	for name, damaged := range map[string][]byte{
		"noncanonical":       append([]byte(" "), raw...),
		"unknown-field":      bytes.Replace(raw, []byte(`{"path"`), []byte(`{"hidden":true,"path"`), 1),
		"not-committed":      bytes.Replace(raw, []byte(`"committed"`), []byte(`"unknown"`), 1),
		"no-durability-fact": bytes.Replace(raw, []byte(`"durable":true`), []byte(`"durable":false`), 1),
		"incomplete":         raw[:len(raw)-1],
	} {
		t.Run(name, func(t *testing.T) {
			got, err := ProjectWriteFileToolResult(context.Background(), writeToolResultTestBinding(t, damaged), damaged)
			if err == nil || got.Validate() == nil || strings.Contains(err.Error(), "protected-path") {
				t.Fatal("damaged receipt published or error disclosed content")
			}
		})
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, ctx := range []context.Context{nil, cancelled} {
		got, err := ProjectWriteFileToolResult(ctx, writeToolResultTestBinding(t, raw), raw)
		if err == nil || got.Validate() == nil {
			t.Fatal("inactive context published receipt")
		}
	}
}

func TestWriteFileToolResultDefaultsRedactReceipt(t *testing.T) {
	raw, err := p.EncodeWriteFileResult(p.WriteFileResult{Path: "protected-path", State: "committed", Durable: true})
	requestOK(t, err)
	got, err := ProjectWriteFileToolResult(context.Background(), writeToolResultTestBinding(t, raw), raw)
	requestOK(t, err)
	encoded, err := json.Marshal(got)
	requestOK(t, err)
	var log bytes.Buffer
	slog.New(slog.NewJSONHandler(&log, nil)).Info("projection", "value", got)
	for _, value := range []string{string(encoded), fmt.Sprintf("%+v %#v", got, got), log.String()} {
		if strings.Contains(value, "protected-path") || !strings.Contains(value, "agent_tool_result_projection") {
			t.Fatal("default output disclosed receipt")
		}
	}
}
