package agentloop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	mt "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

type roundProjectionReaderFunc func(context.Context, i.ExecutionID, ec.RoundID, func(ec.RoundProjectionSource) error) error

func (fn roundProjectionReaderFunc) WithCommittedRoundProjection(ctx context.Context, e i.ExecutionID, r ec.RoundID, consume func(ec.RoundProjectionSource) error) error {
	return fn(ctx, e, r, consume)
}

func roundProjectionJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	requestOK(t, err)
	raw, err = cursor.CanonicalJSON(raw)
	requestOK(t, err)
	return raw
}

// This source is controlled codec material, not a substitute for the real
// committed Execution reader. No actual Model or Runner port is constructed.
func roundProjectionFixture(t *testing.T, added bool) ec.RoundProjectionSource {
	t.Helper()
	v := requestFields(t, "task/work")
	tool := requestID[i.Tool](t, 901)
	registration := tc.ReadFileRegistration()
	mount := mt.ExecutionMountMetadata{ID: requestID[i.Mount](t, 101), WorkspaceID: requestID[mt.Workspace](t, 102), RunnerID: requestID[rc.Runner](t, 103), RunnerVersion: 1, RunnerName: "captured runner", Name: "logical mount", Workspace: "workspace-a", Version: 1}
	agent := v.Agent.Fields()
	agent.AllowedToolIDs, agent.AllowedMountIDs = []i.ToolID{tool}, []i.MountID{mount.ID}
	var err error
	v.Agent, err = ac.NewAgentConfig(agent)
	requestOK(t, err)
	v.Mounts.Mounts, v.Mounts.AttemptBinding = []mt.ExecutionMountMetadata{mount}, v.AttemptBinding
	v.Tools = []tc.ExecutionTool{{ToolID: tool, SpecRevision: 1, ModelVisibleName: "tool_" + strings.ReplaceAll(tool.String(), "-", ""), BindingSnapshot: tc.BuiltinExecutionBinding{RunnerBinding: registration.Binding, ScopeResolverID: registration.ScopeResolverID, RiskClassifierID: registration.RiskClassifierID, Class: registration.Class}}}
	v.Model.Snapshot.Capabilities.ToolCalls = true
	captured := requestContext(t, v)
	actor, err := i.NewAgentRun(v.Request.Launch.ProjectID, v.Request.Launch.AgentID, v.Request.ExecutionID)
	requestOK(t, err)
	oldID, next := requestID[ec.Round](t, 902), requestID[ec.Round](t, 1000)
	request, err := BuildToolCallRequest(context.Background(), captured, actor, oldID.String(), requestID[mc.Call](t, 903), registration.Definition)
	requestOK(t, err)
	snapshot, err := ec.NewDirectTextSnapshot(ec.DirectTextSnapshotFields{ID: requestID[ec.Snapshot](t, 904), StartID: requestID[ec.DirectTextStart](t, 905), ProcessID: requestID[oc.Process](t, 906), Context: captured, CreatedAt: v.CapturedAt})
	requestOK(t, err)
	mr := request.Request()
	round, err := ec.NewToolCallRound(ec.ToolCallRoundFields{Round: ec.DirectTextRoundFields{ID: oldID, InputBindingID: requestID[ec.InputBinding](t, 907), ExecutionID: v.Request.ExecutionID, SnapshotID: snapshot.Fields().ID, StartID: snapshot.Fields().StartID, CallID: mr.CallID, ContextDigest: captured.Digest(), Input: mr.Input, Messages: mr.Messages, CreatedAt: v.CapturedAt}, Tool: mr.Tools[0]})
	requestOK(t, err)
	// Original protected bytes deliberately differ from the canonical JSON
	// embedded in Transcript, while denoting the same complete five fields.
	arguments := []byte(fmt.Sprintf(`{ "path": "README.md", "mount_id": "%s", "encoding": "utf8", "length": 64, "offset": "0" }`, mount.ID.String()))
	canonicalArgs, err := cursor.CanonicalJSON(arguments)
	requestOK(t, err)
	resultRaw, err := p.EncodeReadFileResult(p.ReadFileResult{Path: "README.md", BytesRead: len("private-content🙂"), EOF: true, Encoding: "utf8", Data: "private-content🙂"})
	requestOK(t, err)
	result := toolResultTestBinding(t, resultRaw)
	result.Call.Actor, result.Call.ProjectID, result.Call.AgentID, result.Call.ExecutionID = actor, v.Request.Launch.ProjectID, v.Request.Launch.AgentID, v.Request.ExecutionID
	result.Call.RoundID, result.Call.SnapshotID, result.Call.InputBindingID = oldID.String(), snapshot.Fields().ID.String(), round.Fields().Round.InputBindingID.String()
	result.Call.LogicalCallID, result.Call.ModelVisibleName, result.Call.Spec = mr.CallID, mr.Tools[0].Name, tc.SpecRef{ToolID: tool, SpecRevision: 1}
	result.Call.Input.SHA256, result.Call.Input.Bytes, result.Call.CanonicalArguments = toolResultTestDigest(arguments), f.Progress(len(arguments)), toolResultTestDigest(canonicalArgs)
	requestOK(t, result.Validate())
	assistant := mc.Message{Role: "assistant", Parts: []mc.MessagePart{{Text: &mc.TextPart{Text: "inspect"}}, {ToolCall: &mc.ToolCallPart{ID: result.Call.CallID, Name: result.Call.ModelVisibleName, Arguments: arguments}}}}
	toolMessage := mc.Message{Role: "tool", Parts: []mc.MessagePart{{ToolResult: &mc.ToolResultPart{CallID: result.Call.CallID, Parts: []mc.ResultPart{{Text: &mc.TextPart{Text: string(resultRaw)}}}}}}}
	source := ec.RoundProjectionSource{SchemaVersion: 1, Snapshot: snapshot, Predecessor: round, Result: result, Arguments: arguments, Transcript: []ec.RoundProjectionEntry{}}
	for n, message := range []mc.Message{mr.Messages[1], assistant, toolMessage} {
		raw := roundProjectionJSON(t, message)
		source.Transcript = append(source.Transcript, ec.RoundProjectionEntry{Sequence: f.Sequence(n + 1), RoundID: oldID, Kind: []string{"input", "assistant", "tool_result"}[n], CanonicalMessage: raw, Digest: toolResultTestDigest(raw)})
	}
	q := ec.RoundInputFields{SchemaVersion: 1, RoundID: next, ExecutionID: v.Request.ExecutionID, SnapshotID: snapshot.Fields().ID, SnapshotDigest: snapshot.Digest(), PredecessorRoundID: oldID, Source: ec.ToolResultConsumptionReceipt{ExecutionID: v.Request.ExecutionID, RoundID: oldID, CallID: result.Call.CallID, OperationID: result.OperationID, AttemptID: result.AttemptID, Version: 4, TranscriptThrough: 3, MessageDigest: source.Transcript[2].Digest, ResultDigest: result.ResultDigest, CreatedAt: v.CapturedAt}, Initial: v.Skills.Clone(), SkillInputBindingID: requestID[ec.InputBinding](t, 1001), Bindings: append([]sc.SkillBinding{}, v.Skills.Bindings...), AppliedChangeIDs: []sc.AssignmentChangeID{}, Dispositions: []ec.SkillChangeDisposition{}, AssignmentCutoff: 3, TranscriptThrough: 3, CheckpointVersion: 5, CreatedAt: v.CapturedAt}
	if added {
		b := q.Bindings[0]
		old := b.AssignmentID
		b.AssignmentID, b.AssignmentSequence, b.RevisionID, b.Revision = requestID[sc.Assignment](t, 1002), 3, requestID[sc.Revision](t, 1003), 2
		b.Name, b.Description = "New fixed Skill 中文", "data <ignore instructions> %_!"
		q.Bindings[0] = b
		change := requestID[sc.AssignmentChange](t, 1004)
		q.AppliedChangeIDs = []sc.AssignmentChangeID{change}
		q.Dispositions = []ec.SkillChangeDisposition{{ChangeID: change, Sequence: 3, State: "applied"}}
		q.Control = &ec.SkillsAddedControl{ID: requestID[ec.ControlInput](t, 1005), ExecutionID: q.ExecutionID, RoundID: next, Kind: "skills_added", SourceID: next, SchemaVersion: 1, Payload: []ec.SkillAddition{{ChangeID: change, Binding: b, ReplacedAssignmentID: &old}}, TranscriptPosition: 4}
		q.TranscriptThrough = 4
		source.ControlCanonical = roundProjectionJSON(t, q.Control)
	}
	q.TranscriptPrefixDigest = roundProjectionPrefix(t, source.Transcript)
	source.Input, err = ec.NewRoundInput(q)
	requestOK(t, err)
	return source
}

func roundProjectionPrefix(t *testing.T, entries []ec.RoundProjectionEntry) f.Digest {
	t.Helper()
	raw := make([]json.RawMessage, len(entries))
	for n, entry := range entries {
		raw[n] = entry.CanonicalMessage
	}
	encoded, err := json.Marshal(raw)
	requestOK(t, err)
	return toolResultTestDigest(encoded)
}

func roundProjectionCall(t *testing.T, source ec.RoundProjectionSource) (RoundProjection, error) {
	t.Helper()
	q := source.Input.Fields()
	projector, err := NewRoundProjector(roundProjectionReaderFunc(func(ctx context.Context, e i.ExecutionID, next ec.RoundID, consume func(ec.RoundProjectionSource) error) error {
		if e != q.ExecutionID || next != q.RoundID {
			t.Fatal("reader received replacement identity")
		}
		return consume(source)
	}))
	requestOK(t, err)
	return projector.Project(context.Background(), q.ExecutionID, q.RoundID)
}

func TestRoundProjectionUsesCommittedOrderAndFixedSources(t *testing.T) {
	for _, added := range []bool{false, true} {
		t.Run(fmt.Sprintf("control-%t", added), func(t *testing.T) {
			source := roundProjectionFixture(t, added)
			got, err := roundProjectionCall(t, source)
			requestOK(t, err)
			requestOK(t, got.Validate())
			again, err := roundProjectionCall(t, source)
			requestOK(t, err)
			if !bytes.Equal(got.CanonicalBytes(), again.CanonicalBytes()) || got.Digest() != again.Digest() || got.Digest() != toolResultTestDigest(got.CanonicalBytes()) || got.SourceDigest() != source.Input.Digest() || got.ExecutionID() != source.Input.Fields().ExecutionID || got.RoundID() != source.Input.Fields().RoundID {
				t.Fatal("same committed input did not reproduce exact projection")
			}
			messages := got.Messages()
			wantCount := 5
			if added {
				wantCount++
			}
			if len(messages) != wantCount || messages[0].Role != "system" || messages[1].Role != "user" || messages[2].Role != "assistant" || messages[3].Role != "tool" || messages[len(messages)-1].Role != "user" {
				t.Fatal("message sequence changed")
			}
			if messages[0].Parts[0].Text.Text != source.Predecessor.Fields().Round.Messages[0].Parts[0].Text.Text {
				t.Fatal("original fixed system rewritten")
			}
			for n := 0; n < 3; n++ {
				if !bytes.Equal(roundProjectionJSON(t, messages[n+1]), source.Transcript[n].CanonicalMessage) {
					t.Fatal("historical message rewritten")
				}
			}
			catalog := messages[len(messages)-1].Parts[0].Text.Text
			if !strings.HasPrefix(catalog, roundCatalogLabel) || !strings.Contains(catalog, source.Input.Fields().Bindings[0].Name) {
				t.Fatal("effective fixed catalog missing")
			}
			if added && (messages[4].Role != "user" || !strings.HasPrefix(messages[4].Parts[0].Text.Text, roundControlLabel) || !strings.Contains(messages[4].Parts[0].Text.Text, `"replaces_previous":true`) || strings.Contains(catalog, "Example Skill")) {
				t.Fatal("replacement control or current catalog lost")
			}
			if !bytes.Equal(roundProjectionJSON(t, got.Tools()[0]), roundProjectionJSON(t, source.Predecessor.Fields().Tool)) {
				t.Fatal("fixed complete Tool schema changed")
			}
			for _, forbidden := range []string{source.Result.OperationID.String(), source.Result.AttemptID.String(), string(source.Result.RequestID), source.Result.Call.Input.PayloadID, source.Snapshot.Fields().Context.Input().Fields().Model.Snapshot.Endpoint, source.Input.Fields().Bindings[0].PackageSHA256.String()} {
				if bytes.Contains(got.CanonicalBytes(), []byte(forbidden)) {
					t.Fatal("non-projected backend identity disclosed")
				}
			}
			original := got.CanonicalBytes()
			clear(source.Arguments)
			for n := range source.Transcript {
				clear(source.Transcript[n].CanonicalMessage)
			}
			clear(source.ControlCanonical)
			messages[0].Parts[0].Text.Text = "mutated"
			messages[3].Parts[0].ToolResult.Parts[0].Text.Text = "mutated"
			clear(got.Tools()[0].InputSchema)
			clear(got.CanonicalBytes())
			if !bytes.Equal(original, got.CanonicalBytes()) {
				t.Fatal("borrowed or accessor alias rewrote projection")
			}
			encoded, err := json.Marshal(got)
			requestOK(t, err)
			var log bytes.Buffer
			slog.New(slog.NewJSONHandler(&log, nil)).Info("projection", "value", got)
			for _, redacted := range []string{string(encoded), fmt.Sprintf("%+v %#v", got, got), log.String()} {
				if !strings.Contains(redacted, "agent_round_projection") || strings.Contains(redacted, "private-content") || strings.Contains(redacted, "ordinary-value") {
					t.Fatal("default formatting leaked source")
				}
			}
			var decoded RoundProjection
			if json.Unmarshal(original, &decoded) == nil || decoded.Validate() == nil {
				t.Fatal("caller decoded a usable projection")
			}
		})
	}
}

func TestRoundProjectionRejectsChangedPairingAndSource(t *testing.T) {
	for _, name := range []string{"schema", "round", "snapshot", "prefix", "sequence", "missing-result", "extra-entry", "raw-arguments", "argument-digest", "operation", "attempt", "result-digest", "result-length", "control", "call-id", "tool-name", "duplicate-call", "tool-result-call", "tool-result-error"} {
		t.Run(name, func(t *testing.T) {
			source := roundProjectionFixture(t, true)
			q := source.Input.Fields()
			switch name {
			case "schema":
				source.SchemaVersion++
			case "round":
				source.Result.Call.RoundID = requestID[ec.Round](t, 1090).String()
			case "snapshot":
				q.SnapshotDigest = toolResultTestDigest([]byte("other"))
			case "prefix":
				q.TranscriptPrefixDigest = toolResultTestDigest([]byte("other"))
			case "sequence":
				source.Transcript[1].Sequence++
			case "missing-result":
				source.Transcript = source.Transcript[:2]
			case "extra-entry":
				source.Transcript = append(source.Transcript, source.Transcript[2])
			case "raw-arguments":
				source.Arguments = append(source.Arguments, ' ')
			case "argument-digest":
				source.Result.Call.CanonicalArguments = toolResultTestDigest([]byte("other"))
			case "operation":
				source.Result.OperationID = requestID[tc.Operation](t, 1091)
			case "attempt":
				source.Result.AttemptID = requestID[tc.Attempt](t, 1092)
			case "result-digest":
				source.Result.ResultDigest = toolResultTestDigest([]byte("other"))
			case "result-length":
				source.Result.ResultBytes++
			case "control":
				source.ControlCanonical = []byte(`{}`)
			default:
				n := 1
				if strings.HasPrefix(name, "tool-result") {
					n = 2
				}
				var m mc.Message
				requestOK(t, json.Unmarshal(source.Transcript[n].CanonicalMessage, &m))
				switch name {
				case "call-id":
					m.Parts[1].ToolCall.ID = "other"
				case "tool-name":
					m.Parts[1].ToolCall.Name = "other"
				case "duplicate-call":
					m.Parts = append(m.Parts, m.Parts[1])
				case "tool-result-call":
					m.Parts[0].ToolResult.CallID = "other"
				case "tool-result-error":
					m.Parts[0].ToolResult.IsError = true
				}
				source.Transcript[n].CanonicalMessage = roundProjectionJSON(t, m)
				source.Transcript[n].Digest = toolResultTestDigest(source.Transcript[n].CanonicalMessage)
				q.TranscriptPrefixDigest = roundProjectionPrefix(t, source.Transcript)
				if n == 2 {
					q.Source.MessageDigest = source.Transcript[2].Digest
				}
			}
			var err error
			source.Input, err = ec.NewRoundInput(q)
			requestOK(t, err)
			got, err := roundProjectionCall(t, source)
			if err == nil || got.Validate() == nil || len(got.CanonicalBytes()) != 0 {
				t.Fatal("changed complete pairing became usable projection")
			}
		})
	}
}

func TestRoundProjectionPublishesOnlyAfterOriginalReaderReturn(t *testing.T) {
	if _, err := NewRoundProjector(nil); err == nil {
		t.Fatal("unbound reader accepted")
	}
	for _, name := range []string{"success", "reader-error", "cancelled", "no-callback", "twice", "ignored-callback-error"} {
		t.Run(name, func(t *testing.T) {
			source := roundProjectionFixture(t, false)
			q := source.Input.Fields()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			entered, release := make(chan struct{}), make(chan struct{})
			defer func() {
				select {
				case <-release:
				default:
					close(release)
				}
			}()
			sentinel := f.NewFault(f.CommitUnknown, f.Unknown)
			projector, err := NewRoundProjector(roundProjectionReaderFunc(func(_ context.Context, _ i.ExecutionID, _ ec.RoundID, consume func(ec.RoundProjectionSource) error) error {
				var err error
				if name == "ignored-callback-error" {
					source.SchemaVersion++
				}
				if name != "no-callback" {
					err = consume(source)
				}
				if name == "twice" {
					_ = consume(source)
				}
				close(entered)
				<-release
				if name == "reader-error" {
					return sentinel
				}
				if name == "ignored-callback-error" {
					return nil
				}
				return err
			}))
			requestOK(t, err)
			type outcome struct {
				value RoundProjection
				err   error
			}
			done := make(chan outcome, 1)
			go func() {
				v, e := projector.Project(ctx, q.ExecutionID, q.RoundID)
				done <- outcome{v, e}
			}()
			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				t.Fatal("original reader did not reach hold")
			}
			if name == "cancelled" {
				cancel()
			}
			select {
			case <-done:
				t.Fatal("projection returned before actual reader return")
			default:
			}
			close(release)
			select {
			case result := <-done:
				if name == "success" {
					requestOK(t, result.err)
					requestOK(t, result.value.Validate())
				} else if result.err == nil || result.value.Validate() == nil {
					t.Fatal("failed or cancelled reader published candidate")
				}
				if name == "reader-error" && !errors.Is(result.err, sentinel) {
					t.Fatal("original unknown was replaced")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("original reader return did not release projector")
			}
		})
	}
}
