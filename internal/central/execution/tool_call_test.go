package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/agentloop"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

type checkpointModelCall struct {
	response mc.ModelResponse
	joined   bool
}

func (m *checkpointModelCall) BeginChat(context.Context, mc.ModelRequest) (mc.JSONCall, error) {
	return m, nil
}
func (m *checkpointModelCall) Result(context.Context) (mc.ModelResponse, error) {
	return m.response.Clone(), nil
}
func (m *checkpointModelCall) Close(context.Context) error { m.joined = true; return nil }
func (m *checkpointModelCall) Joined() bool                { return m.joined }

// A controlled Model response must reach the original checkpoint with its
// captured install binding. This is candidate/codec evidence; the App test
// separately proves the actual Registry, operation and installation ledger.
func TestExecutionSkillInstallCheckpoint(t *testing.T) {
	ctx := context.Background()
	facts, consumer := runtimeModelCandidate(t)
	fields := facts.Snapshot.Fields().Context.Input().Fields()
	id := newTestID[i.Tool](t)
	tool := tc.ExecutionTool{ToolID: id, SpecRevision: 1, ModelVisibleName: "tool_" + strings.ReplaceAll(id.String(), "-", ""), BindingSnapshot: tc.BuiltinExecutionBinding{Binding: tc.BuiltinBinding{HandlerID: "skill.install", ContractRevision: 1}, ScopeResolverID: "skill.install", RiskClassifierID: "skill.install", Class: tc.OrdinaryTool}}
	fields.Tools = []tc.ExecutionTool{tool}
	agent := fields.Agent.Fields()
	agent.AllowedToolIDs = []i.ToolID{id}
	var err error
	fields.Agent, err = ac.NewAgentConfig(agent)
	if err != nil {
		t.Fatal(err)
	}
	input, err := ec.NewPreparationInput(fields)
	if err != nil {
		t.Fatal(err)
	}
	builder, _ := NewContextBuilder(contextBuilderProvider(t))
	captured, err := builder.BuildContext(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	sf := facts.Snapshot.Fields()
	sf.Context = captured
	snapshot, err := ec.NewDirectTextSnapshot(sf)
	if err != nil {
		t.Fatal(err)
	}
	rf := facts.Round.Fields()
	rf.ContextDigest = captured.Digest()
	tools := []mc.Tool{{Name: tool.ModelVisibleName, Description: "Install a skill", InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)}}
	choice := mc.ToolChoice{Kind: "required"}
	rf.Input.SchemaVersion = mc.ToolInputSchemaVersion
	rf.Input.Digest, err = mc.ToolInputDigest(ctx, rf.Messages, tools, choice)
	if err != nil {
		t.Fatal(err)
	}
	round, err := ec.NewLoopRound(ec.LoopRoundFields{Round: rf, Tools: tools, Choice: choice, Ordinal: 1, TranscriptStart: 1})
	if err != nil {
		t.Fatal(err)
	}
	request, err := agentloop.NewLoopRequest(ctx, captured.Digest(), mc.ModelRequest{Actor: consumer.Actor, CallID: rf.CallID, Consumer: fields.Model.Consumer.Clone(), Model: fields.Model, Input: rf.Input, Messages: rf.Messages, Tools: tools, ToolChoice: choice, ResponseFormat: mc.ResponseFormat{Kind: "text"}, RetryClass: mc.AgentRetry})
	if err != nil {
		t.Fatal(err)
	}
	model := &checkpointModelCall{response: mc.ModelResponse{CallID: rf.CallID, InvocationID: newTestID[mc.Invocation](t), Message: mc.Message{Role: "assistant", Parts: []mc.MessagePart{{ToolCall: &mc.ToolCallPart{ID: "install-call", Name: tool.ModelVisibleName, Arguments: json.RawMessage(`{ }`)}}}}, FinishReason: "tool_calls", Usage: mc.Usage{Source: mc.UnknownUsage}}}
	controller, err := agentloop.NewDirectTextController(model)
	if err != nil {
		t.Fatal(err)
	}
	session, err := controller.Accept(request)
	if err != nil {
		t.Fatal(err)
	}
	response, err := session.Result(ctx)
	if err != nil || !session.Joined() {
		t.Fatal("original Model session", err)
	}
	run := &directTextCall{ctx: ctx, owner: &directTextState{multi: &multiRoundState{}}, input: &preparationInputRecord{input: input}, snapshot: snapshot, round: loopExecutionRound{round}, allJoined: true, invocation: &directTextInvocation{session: session}}
	candidate, err := newToolCallCandidate(run, response)
	if err != nil || candidate == nil || string(candidate.canonical) != `{}` || string(candidate.original) != `{ }` {
		t.Fatal("captured install could not checkpoint", err)
	}
	stored := &directTextRecord{snapshot: snapshot, round: run.round, toolPending: candidate}
	binding, err := toolBinding(stored)
	if err != nil || binding.Binding != tool.BindingSnapshot.Binding || binding.RunnerBinding != (tc.RunnerBinding{}) || !run.owner.acceptsToolBinding(binding) {
		t.Fatal("checkpoint changed independent install binding", err)
	}
	if tc.DomainBuiltinBinding(binding.Binding) {
		t.Fatal("install entered generic builtin ledger")
	}
	routing := toolCallStorageBinding(tool)
	if routing != (tc.RunnerBinding{OperationName: "skill.install", OperationRevision: "1", ProtocolMajor: 1}) {
		t.Fatal("install routing facts missing")
	}
	foreign := response.Clone()
	foreign.Message.Parts[0].ToolCall.Name = "uncaptured-tool"
	if _, err := newToolCallCandidate(run, foreign); err == nil {
		t.Fatal("uncaptured tool reached checkpoint")
	}
	run.allJoined = false
	if _, err := newToolCallCandidate(run, response); err == nil {
		t.Fatal("unjoined response reached checkpoint")
	}
	controller.Stop()
	if err := controller.Drain(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestExecutionSkillRoutingKeepsClosedBindingKinds(t *testing.T) {
	state := &directTextState{multi: &multiRoundState{}}
	for _, name := range []string{"skill.install", "skill.list", "skill.read", "skill.assign", "skill.prepare"} {
		binding := tc.ToolCallBinding{Binding: tc.BuiltinBinding{HandlerID: name, ContractRevision: 1}}
		if !state.acceptsToolBinding(binding) {
			t.Fatalf("registered binding %s rejected", name)
		}
		binding.Binding.ContractRevision = 2
		if state.acceptsToolBinding(binding) {
			t.Fatalf("unregistered revision for %s admitted", name)
		}
	}
	for _, binding := range []tc.ToolCallBinding{
		{},
		{Binding: tc.BuiltinBinding{HandlerID: "skill.delete", ContractRevision: 1}},
		{Binding: tc.BuiltinBinding{HandlerID: "skill.install", ContractRevision: 1}, RunnerBinding: tc.ReadFileRegistration().Binding},
		{Binding: tc.BuiltinBinding{HandlerID: "skill.install", ContractRevision: 1}, MCPBinding: tc.MCPBinding{RemoteToolName: "mixed"}},
	} {
		if state.acceptsToolBinding(binding) {
			t.Fatal("invalid or mixed binding admitted")
		}
	}
	if !state.acceptsToolBinding(tc.ToolCallBinding{RunnerBinding: tc.ReadFileRegistration().Binding}) {
		t.Fatal("existing Runner binding rejected")
	}
	if (&directTextState{}).acceptsToolBinding(tc.ToolCallBinding{Binding: tc.BuiltinBinding{HandlerID: "skill.install", ContractRevision: 1}}) {
		t.Fatal("install enabled outside explicit Tool loop")
	}
}

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
