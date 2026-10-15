package execution

import (
	"bytes"
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/agentloop"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

func TestMultiRoundSkillControlPreservesResultAndCapturedTools(t *testing.T) {
	ctx := context.Background()
	facts, consumer := runtimeModelCandidate(t)
	fields := facts.Round.Fields()
	def := tc.ReadFileDefinition()
	tools := []mc.Tool{{Name: "tool_01900000000070008000000000000901", Description: def.Description, InputSchema: def.InputSchema, OutputSchema: def.OutputSchema}}
	choice := mc.ToolChoice{Kind: "required"}
	digest, err := mc.ToolInputDigest(ctx, fields.Messages, tools, choice)
	if err != nil {
		t.Fatal(err)
	}
	fields.Input.SchemaVersion, fields.Input.Digest = mc.ToolInputSchemaVersion, digest
	round, err := c.NewLoopRound(c.LoopRoundFields{Round: fields, Tools: tools, Choice: choice, Ordinal: 1, TranscriptStart: 1})
	if err != nil {
		t.Fatal(err)
	}
	request := mc.ModelRequest{Actor: consumer.Actor, CallID: fields.CallID, Consumer: consumer.Consumer, Model: facts.Snapshot.Fields().Context.Input().Fields().Model, Input: fields.Input, Messages: fields.Messages, Tools: tools, ToolChoice: choice, ResponseFormat: mc.ResponseFormat{Kind: "text"}, RetryClass: mc.AgentRetry}
	fixed, err := agentloop.NewLoopRequest(ctx, fields.ContextDigest, request)
	if err != nil {
		t.Fatal(err)
	}
	run := &directTextCall{request: facts.Snapshot.Fields().Context.Input().Fields().Request, snapshot: facts.Snapshot, round: loopExecutionRound{round}, invocation: &directTextInvocation{request: fixed}}
	run.toolFacts = &toolCallCandidate{response: mc.ModelResponse{Message: mc.Message{Role: "assistant", Parts: []mc.MessagePart{{ToolCall: &mc.ToolCallPart{ID: "skill-boundary", Name: tools[0].Name, Arguments: []byte(`{}`)}}}}}}
	message := mc.Message{Role: "tool", Parts: []mc.MessagePart{{ToolResult: &mc.ToolResultPart{CallID: "skill-boundary", Parts: []mc.ResultPart{{Text: &mc.TextPart{Text: `{"result":"original"}`}}}}}}}
	source, changes := roundInputSelectionControl(t)
	p := &skillRoundPlan{run: run, source: fields.ID, snapshot: fields.SnapshotID, previous: 2, cutoff: 7, bindings: source.initial.Initial.Bindings, pending: changes}
	transition, err := buildSkillRoundTransition(p, []sc.AssignmentValidity{"", sc.AssignmentRemoved, sc.AssignmentValid})
	if err != nil {
		t.Fatal(err)
	}
	at, _ := f.NewInstant(fields.CreatedAt.Time().Add(time.Second))
	next, err := newLoopSuccessor(ctx, run, tc.RunnerResultBinding{}, message, at, 4, transition)
	if err != nil {
		t.Fatal(err)
	}
	controlSequence := roundAssistantSequence(run.round) + 2
	if next.skills == nil || next.skills.ControlSequence == nil || *next.skills.ControlSequence != controlSequence || next.round.LoopRound.Fields().TranscriptStart != controlSequence+1 {
		t.Fatal("control did not occupy the position between the original result and next round")
	}
	r := next.request.Request()
	if len(r.Messages) != len(request.Messages)+3 || !reflect.DeepEqual(r.Tools, request.Tools) || r.Model.Snapshot.ID != request.Model.Snapshot.ID || !r.Model.Consumer.Equal(request.Model.Consumer) || !r.Model.LeaseOwner.Equal(request.Model.LeaseOwner) || !reflect.DeepEqual(r.Messages[len(r.Messages)-2], message) {
		t.Fatal("skill addition rewrote the original result or captured model/tool set")
	}
	control, err := next.skills.ControlBytes()
	if err != nil {
		t.Fatal(err)
	}
	if last := r.Messages[len(r.Messages)-1]; last.Role != "user" || len(last.Parts) != 1 || last.Parts[0].Text == nil || !bytes.Equal([]byte(last.Parts[0].Text.Text), control) {
		t.Fatal("model projection differs from the canonical control transcript")
	}
	if err = validateSuccessorSkills(next, transition, controlSequence); err != nil {
		t.Fatal(err)
	}
	changed := *transition
	changedPlan := *p
	changedPlan.cutoff++
	changed.plan = &changedPlan
	if validateSuccessorSkills(next, &changed, controlSequence) == nil || validateSuccessorSkills(next, transition, controlSequence+1) == nil {
		t.Fatal("reserved successor accepted a changed cutoff or transcript position")
	}
	if _, err = loopModelRequest(ctx, facts.Snapshot, next.round); err != nil {
		t.Fatal(err)
	}
	// Removing the control from an otherwise valid request must not preserve
	// its model input identity: it changes the exact next-round model input.
	r.Messages = r.Messages[:len(r.Messages)-1]
	if _, err = agentloop.NewLoopRequest(ctx, fields.ContextDigest, r); err == nil {
		t.Fatal("model input digest did not bind the canonical skills_added control")
	}
	unchanged, err := buildSkillRoundTransition(p, []sc.AssignmentValidity{"", sc.AssignmentRemoved, sc.AssignmentDisabled})
	if err != nil {
		t.Fatal(err)
	}
	plain, err := newLoopSuccessor(ctx, run, tc.RunnerResultBinding{}, message, at, 4, unchanged)
	if err != nil {
		t.Fatal(err)
	}
	if plain.skills == nil || plain.skills.ControlSequence != nil || plain.round.LoopRound.Fields().TranscriptStart != controlSequence || len(plain.request.Request().Messages) != len(request.Messages)+2 || !reflect.DeepEqual(plain.skills.Bindings, p.bindings) {
		t.Fatal("no-addition successor omitted its immutable input or invented a control")
	}
}
