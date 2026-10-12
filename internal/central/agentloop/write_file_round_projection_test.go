package agentloop

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func writeRoundProjectionFixture(t *testing.T, data []byte, code *p.Code) ec.RoundProjectionSource {
	t.Helper()
	source := roundProjectionFixture(t, true)
	q, snapshot, predecessor := source.Input.Fields(), source.Snapshot.Fields(), source.Predecessor.Fields()
	fields := snapshot.Context.Input().Fields()
	reg := tc.WriteFileRegistration()
	fields.Tools[0].BindingSnapshot = tc.BuiltinExecutionBinding{RunnerBinding: reg.Binding, ScopeResolverID: reg.ScopeResolverID, RiskClassifierID: reg.RiskClassifierID, Class: reg.Class}
	captured := requestContext(t, fields)
	request, err := BuildWriteFileToolCallRequest(context.Background(), captured, source.Result.Call.Actor, predecessor.Round.ID.String(), predecessor.Round.CallID, reg.Definition)
	requestOK(t, err)
	mr := request.Request()
	snapshot.Context = captured
	source.Snapshot, err = ec.NewDirectTextSnapshot(snapshot)
	requestOK(t, err)
	predecessor.Round.ContextDigest, predecessor.Round.Input, predecessor.Round.Messages = captured.Digest(), mr.Input, mr.Messages
	predecessor.Tool = mr.Tools[0]
	source.Predecessor, err = ec.NewToolCallRound(predecessor)
	requestOK(t, err)
	arguments := []byte(fmt.Sprintf(`{ "overwrite": true, "data_base64": "%s", "path": "written/文档.bin", "mount_id": "%s" }`, base64.StdEncoding.EncodeToString(data), fields.Mounts.Mounts[0].ID.String()))
	canonicalArguments, err := cursor.CanonicalJSON(arguments)
	requestOK(t, err)
	resultRaw, err := p.EncodeWriteFileResult(p.WriteFileResult{Path: "written/文档.bin", State: "committed", BytesWritten: len(data), Durable: code == nil, CompletionCode: code})
	requestOK(t, err)
	binding := writeToolResultTestBinding(t, resultRaw)
	binding.Call = source.Result.Call
	binding.Call.RunnerBinding = reg.Binding
	binding.Call.Input.Bytes, binding.Call.Input.SHA256, binding.Call.CanonicalArguments = f.Progress(len(arguments)), toolResultTestDigest(arguments), toolResultTestDigest(canonicalArguments)
	requestOK(t, binding.Validate())
	source.Result, source.WriteResult, source.Arguments = tc.ReadFileResultBinding{}, &binding, arguments
	assistant := mc.Message{Role: "assistant", Parts: []mc.MessagePart{{Text: &mc.TextPart{Text: "write"}}, {ToolCall: &mc.ToolCallPart{ID: binding.Call.CallID, Name: binding.Call.ModelVisibleName, Arguments: arguments}}}}
	toolMessage := mc.Message{Role: "tool", Parts: []mc.MessagePart{{ToolResult: &mc.ToolResultPart{CallID: binding.Call.CallID, Parts: []mc.ResultPart{{Text: &mc.TextPart{Text: string(resultRaw)}}}}}}}
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

func TestWriteFileRoundProjectionPreservesCommittedHistory(t *testing.T) {
	internal, cancelled := p.InternalError, p.CancelledCode
	for _, row := range []struct {
		name string
		data []byte
		code *p.Code
	}{
		{"maximum", bytes.Repeat([]byte{0, 255, 13, 10}, p.MaxWriteFileBytes/4), nil},
		{"empty", nil, nil},
		{"late-io", []byte("private file bytes"), &internal},
		{"late-cancel", []byte("private file bytes"), &cancelled},
	} {
		t.Run(row.name, func(t *testing.T) {
			source := writeRoundProjectionFixture(t, row.data, row.code)
			if row.name == "maximum" && len(source.Arguments) <= 32<<10 {
				t.Fatal("fixture did not cross the unchanged read argument budget")
			}
			got, err := roundProjectionCall(t, source)
			requestOK(t, err)
			again, err := roundProjectionCall(t, source)
			requestOK(t, err)
			if !bytes.Equal(got.CanonicalBytes(), again.CanonicalBytes()) || got.Digest() != toolResultTestDigest(got.CanonicalBytes()) || got.SourceDigest() != source.Input.Digest() {
				t.Fatal("committed Write source did not reproduce exact projection")
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
			if call == nil || !bytes.Equal(call.Arguments, canonical) || !bytes.Contains(call.Arguments, []byte(`"data_base64"`)) {
				t.Fatal("original complete Write call was replaced with a summary")
			}
			part := messages[3].Parts[0].ToolResult
			if part == nil || part.IsError || part.CallID != source.WriteResult.Call.CallID || len(part.Parts) != 1 || part.Parts[0].Text == nil || strings.Contains(part.Parts[0].Text.Text, "data_base64") {
				t.Fatal("receipt classified incorrectly or duplicated protected file data")
			}
			result, err := p.DecodeWriteFileResult([]byte(part.Parts[0].Text.Text))
			requestOK(t, err)
			if result.BytesWritten != len(row.data) || result.Durable != (row.code == nil) || (result.CompletionCode == nil) != (row.code == nil) || row.code != nil && *result.CompletionCode != *row.code {
				t.Fatal("known publication completion facts changed")
			}
			if !bytes.Equal(roundProjectionJSON(t, got.Tools()[0]), roundProjectionJSON(t, source.Predecessor.Fields().Tool)) {
				t.Fatal("captured complete Tool schema changed")
			}
			original := got.CanonicalBytes()
			clear(source.Arguments)
			for n := range source.Transcript {
				clear(source.Transcript[n].CanonicalMessage)
			}
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
				if strings.Contains(value, "data_base64") || strings.Contains(value, "written/") || !strings.Contains(value, "agent_round_projection") {
					t.Fatal("default output disclosed historical Write content")
				}
			}
		})
	}
}

func TestWriteFileRoundProjectionRejectsMixedOrChangedReceipt(t *testing.T) {
	for _, name := range []string{"missing", "dual", "read-binding", "raw-digest", "canonical-digest", "operation", "attempt", "path", "written-length", "error"} {
		t.Run(name, func(t *testing.T) {
			source := writeRoundProjectionFixture(t, []byte("file bytes"), nil)
			q := source.Input.Fields()
			switch name {
			case "missing":
				source.WriteResult = nil
			case "dual":
				source.Result = roundProjectionFixture(t, true).Result
			case "read-binding":
				source.WriteResult.Call.RunnerBinding = tc.ReadFileRegistration().Binding
			case "raw-digest":
				source.Arguments = append(source.Arguments, ' ')
			case "canonical-digest":
				source.WriteResult.Call.CanonicalArguments = toolResultTestDigest([]byte("other"))
			case "operation":
				source.WriteResult.OperationID = requestID[tc.Operation](t, 1191)
			case "attempt":
				source.WriteResult.AttemptID = requestID[tc.Attempt](t, 1192)
			default:
				var message mc.Message
				requestOK(t, json.Unmarshal(source.Transcript[2].CanonicalMessage, &message))
				part := message.Parts[0].ToolResult
				if name == "error" {
					part.IsError = true
				} else {
					result, err := p.DecodeWriteFileResult([]byte(part.Parts[0].Text.Text))
					requestOK(t, err)
					if name == "path" {
						result.Path = "different"
					} else {
						result.BytesWritten++
					}
					raw, err := p.EncodeWriteFileResult(result)
					requestOK(t, err)
					part.Parts[0].Text.Text = string(raw)
					source.WriteResult.ResultDigest, source.WriteResult.ResultBytes = toolResultTestDigest(raw), f.Progress(len(raw))
					q.Source.ResultDigest = source.WriteResult.ResultDigest
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
				t.Fatal("changed Write result became a usable projection")
			}
		})
	}
}
