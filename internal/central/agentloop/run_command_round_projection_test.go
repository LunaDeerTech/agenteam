package agentloop

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func commandRoundProjectionFixture(t *testing.T, bundle tc.RunCommandResultBundle, outcome, code string) ec.RoundProjectionSource {
	t.Helper()
	source := roundProjectionFixture(t, true)
	q, snapshot, predecessor := source.Input.Fields(), source.Snapshot.Fields(), source.Predecessor.Fields()
	fields := snapshot.Context.Input().Fields()
	reg := tc.RunCommandRegistration()
	fields.Tools[0].BindingSnapshot = tc.BuiltinExecutionBinding{RunnerBinding: reg.Binding, ScopeResolverID: reg.ScopeResolverID, RiskClassifierID: reg.RiskClassifierID, Class: reg.Class}
	captured := requestContext(t, fields)
	request, err := BuildRunCommandToolCallRequest(context.Background(), captured, source.Result.Call.Actor, predecessor.Round.ID.String(), predecessor.Round.CallID, reg.Definition)
	requestOK(t, err)
	mr := request.Request()
	snapshot.Context = captured
	source.Snapshot, err = ec.NewDirectTextSnapshot(snapshot)
	requestOK(t, err)
	predecessor.Round.ContextDigest, predecessor.Round.Input, predecessor.Round.Messages = captured.Digest(), mr.Input, mr.Messages
	predecessor.Tool = mr.Tools[0]
	source.Predecessor, err = ec.NewToolCallRound(predecessor)
	requestOK(t, err)
	arguments, err := json.MarshalIndent(struct {
		Mount   string   `json:"mount_id"`
		Mode    string   `json:"mode"`
		Program string   `json:"program"`
		Args    []string `json:"args"`
		CWD     string   `json:"cwd"`
	}{fields.Mounts.Mounts[0].ID.String(), "argv", "/bin/test-command", []string{"original-private-argv", strings.Repeat("<", 40000)}, "."}, "", "  ")
	requestOK(t, err)
	canonicalArguments, err := cursor.CanonicalJSON(arguments)
	requestOK(t, err)
	resultRaw, err := tc.EncodeRunCommandResultBundle(bundle)
	requestOK(t, err)
	binding := commandToolResultTestBinding(t, resultRaw, outcome, code)
	binding.Call = source.Result.Call
	binding.Call.RunnerBinding = reg.Binding
	binding.Call.Input.Bytes, binding.Call.Input.SHA256, binding.Call.CanonicalArguments = f.Progress(len(arguments)), toolResultTestDigest(arguments), toolResultTestDigest(canonicalArguments)
	requestOK(t, binding.Validate())
	source.Result, source.CommandResult, source.Arguments = tc.ReadFileResultBinding{}, &binding, arguments
	assistant := mc.Message{Role: "assistant", Parts: []mc.MessagePart{{Text: &mc.TextPart{Text: "run"}}, {ToolCall: &mc.ToolCallPart{ID: binding.Call.CallID, Name: binding.Call.ModelVisibleName, Arguments: arguments}}}}
	toolMessage := mc.Message{Role: "tool", Parts: []mc.MessagePart{{ToolResult: &mc.ToolResultPart{CallID: binding.Call.CallID, IsError: outcome != "success", Parts: []mc.ResultPart{{Text: &mc.TextPart{Text: string(resultRaw)}}}}}}}
	for n, message := range []mc.Message{mr.Messages[1], assistant, toolMessage} {
		raw := roundProjectionJSON(t, message)
		source.Transcript[n].CanonicalMessage, source.Transcript[n].Digest = raw, toolResultTestDigest(raw)
	}
	q.SnapshotDigest, q.Source.MessageDigest, q.Source.ResultDigest = source.Snapshot.Digest(), source.Transcript[2].Digest, binding.ResultDigest
	q.TranscriptPrefixDigest = roundProjectionPrefix(t, source.Transcript)
	source.Input, err = ec.NewRoundInput(q)
	requestOK(t, err)
	return source
}

func TestRunCommandRoundProjectionPreservesKnownHistory(t *testing.T) {
	for _, name := range []string{"exit", "success-maximum", "cancelled", "io-incomplete"} {
		t.Run(name, func(t *testing.T) {
			stdout, stderr := []byte("private-stdout\x00中\n"), []byte("private-stderr🙂\t")
			if name == "success-maximum" {
				stdout, stderr = bytes.Repeat([]byte{0}, p.MaxRunCommandStreamBytes), bytes.Repeat([]byte{1}, p.MaxRunCommandStreamBytes)
			}
			report := commandToolResultTestReport(stdout, stderr)
			outcome, code := "failure", "command_exit_nonzero"
			switch name {
			case "exit":
				report.ExitCode = 23
			case "success-maximum":
				outcome, code = "success", ""
			case "cancelled":
				c := p.CancelledCode
				report.ExitCode, report.Signal, report.Cancelled, report.OutputComplete, report.CompletionCode = -1, 15, true, false, &c
				outcome, code = "cancelled", string(c)
			case "io-incomplete":
				c := p.InternalError
				report.OutputComplete, report.ProcessGroupTerminationConfirmed, report.CompletionCode = false, false, &c
				outcome, code = "failure", string(c)
			}
			source := commandRoundProjectionFixture(t, tc.RunCommandResultBundle{Report: report, Stdout: stdout, Stderr: stderr}, outcome, code)
			if len(source.Arguments) <= 32<<10 {
				t.Fatal("fixture did not cross the unchanged Read argument budget")
			}
			got, err := roundProjectionCall(t, source)
			requestOK(t, err)
			again, err := roundProjectionCall(t, source)
			requestOK(t, err)
			if !bytes.Equal(got.CanonicalBytes(), again.CanonicalBytes()) || got.Digest() != toolResultTestDigest(got.CanonicalBytes()) || got.SourceDigest() != source.Input.Digest() {
				t.Fatal("committed Command source did not reproduce exact projection")
			}
			messages := got.Messages()
			if len(messages) != 6 || !strings.HasPrefix(messages[4].Parts[0].Text.Text, roundControlLabel) || !strings.HasPrefix(messages[5].Parts[0].Text.Text, roundCatalogLabel) {
				t.Fatal("committed Skill control or catalog order changed")
			}
			for n := range source.Transcript {
				if !bytes.Equal(roundProjectionJSON(t, messages[n+1]), source.Transcript[n].CanonicalMessage) {
					t.Fatal("original canonical Transcript was rewritten")
				}
			}
			call := messages[2].Parts[1].ToolCall
			canonical, err := cursor.CanonicalJSON(source.Arguments)
			requestOK(t, err)
			if call == nil || !bytes.Equal(call.Arguments, canonical) || !bytes.Contains(call.Arguments, []byte("original-private-argv")) {
				t.Fatal("original complete argv or canonical prefix was changed")
			}
			part := messages[3].Parts[0].ToolResult
			if part == nil || part.IsError != (outcome != "success") || part.CallID != source.CommandResult.Call.CallID || len(part.Parts) != 1 || part.Parts[0].Text == nil {
				t.Fatal("known report lost its original call or normalized classification")
			}
			decoded, err := tc.DecodeRunCommandResultBundle(context.Background(), []byte(part.Parts[0].Text.Text))
			requestOK(t, err)
			if !reflect.DeepEqual(decoded.Report, report) || !bytes.Equal(decoded.Stdout, stdout) || !bytes.Equal(decoded.Stderr, stderr) {
				t.Fatal("known status, cancellation or complete streams changed")
			}
			if !bytes.Equal(roundProjectionJSON(t, got.Tools()[0]), roundProjectionJSON(t, source.Predecessor.Fields().Tool)) {
				t.Fatal("captured complete Tool schema changed")
			}
			original := got.CanonicalBytes()
			clear(source.Arguments)
			for n := range source.Transcript {
				clear(source.Transcript[n].CanonicalMessage)
			}
			clear(decoded.Stdout)
			clear(decoded.Stderr)
			clear(call.Arguments)
			part.Parts[0].Text.Text = "changed"
			if !bytes.Equal(original, got.CanonicalBytes()) {
				t.Fatal("borrowed or accessor mutation changed projection")
			}
			encoded, err := json.Marshal(got)
			requestOK(t, err)
			var log bytes.Buffer
			slog.New(slog.NewJSONHandler(&log, nil)).Info("projection", "value", got)
			for _, value := range []string{string(encoded), fmt.Sprintf("%+v %#v", got, got), log.String()} {
				if strings.Contains(value, "private-") || strings.Contains(value, "/bin/test-command") || !strings.Contains(value, "agent_round_projection") {
					t.Fatal("default output disclosed original argv or streams")
				}
			}
		})
	}
}

func TestRunCommandRoundProjectionRejectsMixedOrChangedReport(t *testing.T) {
	for _, name := range []string{"missing", "dual-read", "dual-write", "write-binding", "raw-digest", "canonical-digest", "operation", "attempt", "result-digest", "wrong-code", "false-success", "is-error"} {
		t.Run(name, func(t *testing.T) {
			stdout, stderr := []byte("out"), []byte("err")
			report := commandToolResultTestReport(stdout, stderr)
			report.ExitCode = 23
			source := commandRoundProjectionFixture(t, tc.RunCommandResultBundle{Report: report, Stdout: stdout, Stderr: stderr}, "failure", "command_exit_nonzero")
			q := source.Input.Fields()
			switch name {
			case "missing":
				source.CommandResult = nil
			case "dual-read":
				source.Result = roundProjectionFixture(t, true).Result
			case "dual-write":
				source.WriteResult = &tc.WriteFileResultBinding{}
			case "write-binding":
				source.CommandResult.Call.RunnerBinding = tc.WriteFileRegistration().Binding
			case "raw-digest":
				source.Arguments = append(source.Arguments, ' ')
			case "canonical-digest":
				source.CommandResult.Call.CanonicalArguments = toolResultTestDigest([]byte("other"))
			case "operation":
				source.CommandResult.OperationID = requestID[tc.Operation](t, 1291)
			case "attempt":
				source.CommandResult.AttemptID = requestID[tc.Attempt](t, 1292)
			case "result-digest":
				source.CommandResult.ResultDigest = toolResultTestDigest([]byte("other"))
			case "wrong-code":
				source.CommandResult.Code = string(p.InternalError)
			default:
				var message mc.Message
				requestOK(t, json.Unmarshal(source.Transcript[2].CanonicalMessage, &message))
				message.Parts[0].ToolResult.IsError = false
				if name == "false-success" {
					source.CommandResult.Outcome, source.CommandResult.Code = "success", ""
				}
				source.Transcript[2].CanonicalMessage = roundProjectionJSON(t, message)
				source.Transcript[2].Digest = toolResultTestDigest(source.Transcript[2].CanonicalMessage)
				q.Source.MessageDigest, q.TranscriptPrefixDigest = source.Transcript[2].Digest, roundProjectionPrefix(t, source.Transcript)
			}
			var err error
			source.Input, err = ec.NewRoundInput(q)
			requestOK(t, err)
			got, err := roundProjectionCall(t, source)
			if err == nil || got.Validate() == nil {
				t.Fatal("mixed or changed Command report became usable projection")
			}
		})
	}
}
