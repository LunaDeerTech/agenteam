package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

func TestExecutionToolCallRoundKeepsV1ClosedAndV2Exact(t *testing.T) {
	facts, _ := runtimeModelCandidate(t)
	old := facts.Round.CanonicalBytes()
	fields := facts.Round.Fields()
	definition := tc.ReadFileDefinition()
	tool := mc.Tool{Name: "tool_01900000000070008000000000000901", Description: definition.Description, InputSchema: definition.InputSchema, OutputSchema: definition.OutputSchema}
	digest, err := ec.ToolCallInputDigest(context.Background(), fields.Messages, tool)
	if err != nil {
		t.Fatal(err)
	}
	fields.Input.SchemaVersion = 2
	fields.Input.Digest = digest
	v, err := ec.NewToolCallRound(ec.ToolCallRoundFields{Round: fields, Tool: tool})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ec.DecodeDirectTextRound(v.CanonicalBytes()); err == nil {
		t.Fatal("v2 entered old no-tools codec")
	}
	if _, err = ec.DecodeToolCallRound(old); err == nil {
		t.Fatal("v1 admitted as tool input")
	}
	decoded, err := ec.DecodeToolCallRound(v.CanonicalBytes())
	if err != nil || decoded.Digest() != v.Digest() {
		t.Fatal("v2 roundtrip", err)
	}
	changed := v.Fields()
	changed.Tool.Description = "changed"
	if _, err = ec.NewToolCallRound(changed); err == nil {
		t.Fatal("schema/description not bound to input")
	}
	changed = v.Fields()
	changed.Round.Messages[1].Parts[0].Text.Text = "changed"
	if _, err = ec.NewToolCallRound(changed); err == nil {
		t.Fatal("message not bound")
	}
	raw := v.CanonicalBytes()
	raw[0] = 'x'
	if !bytes.Equal(decoded.CanonicalBytes(), v.CanonicalBytes()) {
		t.Fatal("returned byte mutation changed owner")
	}
	for _, public := range []string{fmt.Sprintf("%+v", v), string(mustToolJSON(t, v))} {
		if strings.Contains(public, "captured") || strings.Contains(public, "properties") {
			t.Fatal("round implicitly disclosed")
		}
	}
}
func mustToolJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func TestExecutionToolCallCurrentWitnessIsOriginalAndScoped(t *testing.T) {
	x := newLaunchControl(t)
	q := x.request
	execution := newTestID[i.Execution](t)
	actor, err := i.NewAgentRun(q.ProjectID, q.AgentID, execution)
	if err != nil {
		t.Fatal(err)
	}
	request := ac.ExecutionConfigurationRequest{Actor: actor, ProjectID: q.ProjectID, AgentID: q.AgentID, ExecutionID: execution, Stage: ac.ExecutionConfigurationCurrent}
	d := &ToolCallDriver{driver: &DirectTextDriver{state: &directTextState{store: x.store, authority: x.authority}}}
	locks := []f.LockRequest{projectLock(q.ProjectID), agentLock(q.AgentID, f.Shared), executionLock(execution)}
	cause, _ := f.NewRecoveryCause("tool-call-witness", execution.String(), q.Meta.RequestID.String())
	result := x.store.WithinTx(context.Background(), cause, func(ctx context.Context, tx f.Tx) error {
		if err := x.store.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		w := &toolCurrentWitness{driver: d, tx: tx, request: request, locks: locks}
		w.active.Store(true)
		owned := context.WithValue(ctx, toolCurrentKey{}, w)
		if err := x.authority.requireModelCurrentConfiguration(owned, tx, request); err != nil {
			t.Fatal(err)
		}
		if err := x.authority.requireToolCurrentConfiguration(ctx, tx, request); err == nil {
			t.Fatal("ordinary request minted proof")
		}
		if err := x.authority.requireToolCurrentConfiguration(owned, f.NewTx(), request); err == nil {
			t.Fatal("foreign Tx admitted")
		}
		wrong := request
		wrong.AgentID = newTestID[i.Agent](t)
		if x.authority.requireToolCurrentConfiguration(owned, tx, wrong) == nil {
			t.Fatal("different Agent admitted")
		}
		x.store.locks = nil
		if x.authority.requireToolCurrentConfiguration(owned, tx, request) == nil {
			t.Fatal("missing union admitted")
		}
		x.store.locks = locks
		w.active.Store(false)
		if x.authority.requireToolCurrentConfiguration(context.WithoutCancel(owned), tx, request) == nil {
			t.Fatal("expired witness replayed")
		}
		return nil
	})
	if err := commitError(result); err != nil {
		t.Fatal(err)
	}
}

func TestExecutionToolCallCurrentAndCaptureFactsStaySeparate(t *testing.T) {
	input := contextBuilderInput(t)
	captured := input.Fields().Agent.Fields()
	current := captured
	current.Core.Version++
	current.AllowedToolIDs = append(current.AllowedToolIDs, newTestID[i.Tool](t))
	agent, err := ac.NewAgentConfig(current)
	if err != nil {
		t.Fatal(err)
	}
	r := toolFactsRequest(input, agent)
	if r.CaptureAgentVersion != captured.Core.Version || r.AgentVersion != current.Core.Version || r.AttemptBinding != input.Fields().AttemptBinding || len(r.Selection.AllowedToolIDs) != len(captured.AllowedToolIDs) || len(r.AllowedToolIDs) != len(current.AllowedToolIDs) {
		t.Fatal("current facts rewrote immutable capture")
	}
}

func TestExecutionToolCallObservationBindsCompleteOriginalSuccess(t *testing.T) {
	facts, _ := runtimeModelCandidate(t)
	at := facts.Round.Fields().CreatedAt
	response := mc.ModelResponse{CallID: facts.Round.Fields().CallID, InvocationID: newTestID[mc.Invocation](t), Message: mc.Message{Role: "assistant", Parts: []mc.MessagePart{{Text: &mc.TextPart{Text: "original assistant"}}, {ToolCall: &mc.ToolCallPart{ID: "original-call", Name: "original-name", Arguments: json.RawMessage(`{"path":"README.md"}`)}}}}, FinishReason: "tool_calls", Usage: mc.Usage{Source: mc.UnknownUsage}}
	original := &toolCallCandidate{payload: newTestID[toolArgumentPayload](t), response: response, call: *response.Message.Parts[1].ToolCall, original: []byte(`{ "path":"README.md" }`), canonical: []byte(`{"path":"README.md"}`), at: at, version: 3}
	if !sameToolCallCheckpoint(original, original) {
		t.Fatal("same original rejected")
	}
	for name, change := range map[string]func(*toolCallCandidate){
		"self-consistent other call": func(v *toolCallCandidate) {
			v.call.ID = "other-call"
			v.response.Message.Parts[1].ToolCall.ID = "other-call"
		},
		"assistant":      func(v *toolCallCandidate) { v.response.Message.Parts[0].Text.Text = "other assistant" },
		"invocation":     func(v *toolCallCandidate) { v.response.InvocationID = newTestID[mc.Invocation](t) },
		"usage":          func(v *toolCallCandidate) { v.response.ProviderRequestID = "other provider request" },
		"time":           func(v *toolCallCandidate) { v.at, _ = f.NewInstant(v.at.Time().Add(1000)) },
		"original bytes": func(v *toolCallCandidate) { v.original = append([]byte(" "), v.original...) },
	} {
		t.Run(name, func(t *testing.T) {
			copy := *original
			copy.response = original.response.Clone()
			change(&copy)
			if sameToolCallCheckpoint(&copy, original) {
				t.Fatal("unrelated candidate cleared original Unknown")
			}
		})
	}
}
