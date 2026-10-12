package agentloop

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"strconv"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// These are controlled codec facts, not a historical authority or proof that
// a supplied report came from an actual Runner/Operation transaction.
func commandToolResultTestBinding(t *testing.T, raw []byte, outcome, code string) tc.RunCommandResultBinding {
	t.Helper()
	old := toolResultTestBinding(t, []byte("{}"))
	old.Call.RunnerBinding = tc.RunCommandRegistration().Binding
	v := tc.RunCommandResultBinding{
		Call: old.Call, OperationID: old.OperationID, AttemptID: old.AttemptID, RequestID: old.RequestID,
		InputDigest: old.InputDigest, Fingerprint: old.Fingerprint, TerminalDigest: old.TerminalDigest,
		Outcome: outcome, Code: code, Attempted: true, ResultDigest: toolResultTestDigest(raw), ResultBytes: f.Progress(len(raw)),
	}
	requestOK(t, v.Validate())
	return v
}

func commandToolResultTestReport(stdout, stderr []byte) p.RunCommandResult {
	return p.RunCommandResult{
		State: "completed", DurationMS: "123", StdoutBytes: p.Decimal(strconv.Itoa(len(stdout))), StderrBytes: p.Decimal(strconv.Itoa(len(stderr))),
		OutputComplete: true, ProcessGroupTerminationConfirmed: true,
	}
}

func TestRunCommandToolResultPreservesKnownReports(t *testing.T) {
	internal, cancelled, timeout := p.InternalError, p.CancelledCode, p.Timeout
	for _, row := range []struct {
		name, outcome, code string
		mutate              func(*p.RunCommandResult)
	}{
		{"success", "success", "", func(*p.RunCommandResult) {}},
		{"truncated", "success", "", func(r *p.RunCommandResult) { r.StdoutTruncated, r.StderrTruncated = true, true }},
		{"exit", "failure", "command_exit_nonzero", func(r *p.RunCommandResult) { r.ExitCode = 23 }},
		{"signal", "failure", "command_signaled", func(r *p.RunCommandResult) { r.ExitCode, r.Signal = -1, 15 }},
		{"cancel", "cancelled", string(cancelled), func(r *p.RunCommandResult) {
			r.ExitCode, r.Signal, r.Cancelled, r.OutputComplete, r.CompletionCode = -1, 15, true, false, &cancelled
		}},
		{"timeout", "cancelled", string(timeout), func(r *p.RunCommandResult) {
			r.ExitCode, r.Signal, r.Cancelled, r.TimedOut, r.OutputComplete, r.CompletionCode = -1, 9, true, true, false, &timeout
		}},
		{"io", "failure", string(internal), func(r *p.RunCommandResult) {
			r.OutputComplete, r.ProcessGroupTerminationConfirmed, r.CompletionCode = false, false, &internal
		}},
		{"io-before-cancel", "failure", string(internal), func(r *p.RunCommandResult) {
			r.Cancelled, r.OutputComplete, r.CompletionCode = true, false, &internal
		}},
		{"maximum-escaped", "success", "", func(*p.RunCommandResult) {}},
	} {
		t.Run(row.name, func(t *testing.T) {
			stdout, stderr := []byte("stdout-private\x00中🙂\n"), []byte("stderr-private\t\"\\")
			if row.name == "maximum-escaped" {
				stdout, stderr = bytes.Repeat([]byte{0}, p.MaxRunCommandStreamBytes), bytes.Repeat([]byte{1}, p.MaxRunCommandStreamBytes)
			}
			report := commandToolResultTestReport(stdout, stderr)
			row.mutate(&report)
			raw, err := tc.EncodeRunCommandResultBundle(tc.RunCommandResultBundle{Report: report, Stdout: stdout, Stderr: stderr})
			requestOK(t, err)
			if row.name == "maximum-escaped" && (len(raw) <= 512<<10 || len(raw) > tc.MaxRunCommandResultBundleBytes) {
				t.Fatal("fixture missed the Command-only escaped result budget")
			}
			binding := commandToolResultTestBinding(t, raw, row.outcome, row.code)
			got, err := ProjectRunCommandToolResult(context.Background(), binding, raw)
			requestOK(t, err)
			requestOK(t, got.Validate())
			message := got.Message()
			if message.Role != "tool" || len(message.Parts) != 1 || message.Parts[0].ToolResult == nil {
				t.Fatal("report did not form exactly one ToolResult")
			}
			part := message.Parts[0].ToolResult
			if part.CallID != binding.Call.CallID || part.IsError != (row.outcome != "success") || len(part.Parts) != 1 || part.Parts[0].Text == nil || part.Parts[0].Text.Text != string(raw) {
				t.Fatal("original report or normalized error classification changed")
			}
			decoded, err := tc.DecodeRunCommandResultBundle(context.Background(), []byte(part.Parts[0].Text.Text))
			requestOK(t, err)
			if !reflect.DeepEqual(decoded.Report, report) || !bytes.Equal(decoded.Stdout, stdout) || !bytes.Equal(decoded.Stderr, stderr) {
				t.Fatal("complete status or exact UTF-8/NUL streams changed")
			}
			want := roundProjectionJSON(t, message)
			if !bytes.Equal(want, got.CanonicalBytes()) || got.Digest() != toolResultTestDigest(want) {
				t.Fatal("complete canonical message mismatch")
			}
			for _, forbidden := range []string{binding.OperationID.String(), binding.AttemptID.String(), string(binding.RequestID), binding.Call.Input.PayloadID} {
				if bytes.Contains(want, []byte(forbidden)) {
					t.Fatal("backend identity leaked into result content")
				}
			}
			clear(raw)
			clear(stdout)
			clear(stderr)
			clear(decoded.Stdout)
			clear(decoded.Stderr)
			part.Parts[0].Text.Text = "changed"
			clear(got.CanonicalBytes())
			if !bytes.Equal(want, got.CanonicalBytes()) {
				t.Fatal("borrowed or accessor mutation changed the projection")
			}
		})
	}
}

func TestRunCommandToolResultRejectsUnboundOrDamagedBundle(t *testing.T) {
	stdout, stderr := []byte("private-stdout"), []byte("private-stderr")
	report := commandToolResultTestReport(stdout, stderr)
	report.ExitCode = 23
	raw, err := tc.EncodeRunCommandResultBundle(tc.RunCommandResultBundle{Report: report, Stdout: stdout, Stderr: stderr})
	requestOK(t, err)
	for name, mutate := range map[string]func(*tc.RunCommandResultBinding){
		"read-profile":  func(v *tc.RunCommandResultBinding) { v.Call.RunnerBinding = tc.ReadFileRegistration().Binding },
		"write-profile": func(v *tc.RunCommandResultBinding) { v.Call.RunnerBinding = tc.WriteFileRegistration().Binding },
		"digest":        func(v *tc.RunCommandResultBinding) { v.ResultDigest = toolResultTestDigest([]byte("other")) },
		"length":        func(v *tc.RunCommandResultBinding) { v.ResultBytes++ },
		"call":          func(v *tc.RunCommandResultBinding) { v.Call.CallID = "" },
		"unknown":       func(v *tc.RunCommandResultBinding) { v.Outcome = "unknown" },
		"not-attempted": func(v *tc.RunCommandResultBinding) { v.Attempted = false },
		"false-success": func(v *tc.RunCommandResultBinding) { v.Outcome, v.Code = "success", "" },
		"wrong-code":    func(v *tc.RunCommandResultBinding) { v.Code = string(p.InternalError) },
		"false-cancel":  func(v *tc.RunCommandResultBinding) { v.Outcome, v.Code = "cancelled", string(p.CancelledCode) },
	} {
		t.Run(name, func(t *testing.T) {
			binding := commandToolResultTestBinding(t, raw, "failure", "command_exit_nonzero")
			mutate(&binding)
			got, err := ProjectRunCommandToolResult(context.Background(), binding, raw)
			if err == nil || got.Validate() == nil {
				t.Fatal("unbound or misclassified report accepted")
			}
		})
	}
	for name, damaged := range map[string][]byte{
		"noncanonical":  append([]byte(" "), raw...),
		"unknown-field": append([]byte(`{"extra":true,`), raw[1:]...),
		"missing":       []byte(`{}`),
		"null-stream":   bytes.Replace(raw, []byte(`"stdout":"private-stdout"`), []byte(`"stdout":null`), 1),
		"wrong-count":   bytes.Replace(raw, []byte(`"stdout_bytes":"14"`), []byte(`"stdout_bytes":"0"`), 1),
		"invalid-utf8":  bytes.Replace(raw, stdout, []byte{255}, 1),
		"incomplete":    raw[:len(raw)-1],
	} {
		t.Run(name, func(t *testing.T) {
			if bytes.Equal(raw, damaged) {
				t.Fatal("damaged fixture did not change original input")
			}
			got, err := ProjectRunCommandToolResult(context.Background(), commandToolResultTestBinding(t, damaged, "failure", "command_exit_nonzero"), damaged)
			if err == nil || got.Validate() == nil || strings.Contains(err.Error(), "private-") {
				t.Fatal("damaged bundle published or failure disclosed output")
			}
		})
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, ctx := range []context.Context{nil, cancelled} {
		got, err := ProjectRunCommandToolResult(ctx, commandToolResultTestBinding(t, raw, "failure", "command_exit_nonzero"), raw)
		if err == nil || got.Validate() == nil {
			t.Fatal("inactive context published report")
		}
	}
}

func TestRunCommandToolResultDefaultsRedactStreams(t *testing.T) {
	stdout, stderr := []byte("private-stdout"), []byte("private-stderr")
	raw, err := tc.EncodeRunCommandResultBundle(tc.RunCommandResultBundle{Report: commandToolResultTestReport(stdout, stderr), Stdout: stdout, Stderr: stderr})
	requestOK(t, err)
	got, err := ProjectRunCommandToolResult(context.Background(), commandToolResultTestBinding(t, raw, "success", ""), raw)
	requestOK(t, err)
	encoded, err := json.Marshal(got)
	requestOK(t, err)
	var log bytes.Buffer
	slog.New(slog.NewJSONHandler(&log, nil)).Info("projection", "value", got)
	for _, value := range []string{string(encoded), fmt.Sprintf("%+v %#v", got, got), log.String()} {
		if strings.Contains(value, "private-") || !strings.Contains(value, "agent_tool_result_projection") {
			t.Fatal("default output disclosed command streams")
		}
	}
}
