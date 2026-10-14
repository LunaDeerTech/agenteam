package execution

import (
	"context"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

type loopToolFactsControl struct {
	definition tc.Definition
	checks     int
	err        error
}

func (s *loopToolFactsControl) ReadDefinitionInTx(context.Context, f.Tx, tc.SpecRef) (tc.Definition, error) {
	return s.definition, nil
}
func (s *loopToolFactsControl) RequireExecutionToolFactsInTx(_ context.Context, _ f.Tx, r tc.ExecutionToolFactsRequest) error {
	s.checks++
	if err := r.Validate(); err != nil {
		return err
	}
	return s.err
}

// This exercises the complete captured ToolSet/current metadata gate used by
// startup, after its separate current Agent/Project/Process proofs. The
// controlled metadata port is not evidence of a real Registry transaction.
func TestMultiRoundCurrentCoreAndOrdinaryTools(t *testing.T) {
	for _, test := range []struct {
		name            string
		class           tc.ToolClass
		allowed, denied bool
		metadataErr     error
		want            f.Code
		checks          int
	}{
		{name: "core outside ordinary allow list", class: tc.CoreTool, checks: 1},
		{name: "ordinary allowed", class: tc.OrdinaryTool, allowed: true, checks: 1},
		{name: "ordinary revoked", class: tc.OrdinaryTool, want: f.Forbidden},
		{name: "core still requires current registry", class: tc.CoreTool, metadataErr: fault(f.VersionConflict), want: f.VersionConflict, checks: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			fields := contextBuilderInput(t).Fields()
			id := newTestID[i.Tool](t)
			fields.Tools = []tc.ExecutionTool{{ToolID: id, SpecRevision: 1, ModelVisibleName: "tool_" + strings.ReplaceAll(id.String(), "-", ""), BindingSnapshot: tc.BuiltinExecutionBinding{Binding: tc.BuiltinBinding{HandlerID: "task.read", ContractRevision: 1}, ScopeResolverID: "task.read", RiskClassifierID: "task.read", Class: test.class}}}
			captured := fields.Agent.Fields()
			captured.AllowedToolIDs = []i.ToolID{id}
			var err error
			fields.Agent, err = ac.NewAgentConfig(captured)
			if err != nil {
				t.Fatal(err)
			}
			input, err := c.NewPreparationInput(fields)
			if err != nil {
				t.Fatal(err)
			}
			current := fields.Agent.Fields()
			current.Core.Version++
			current.AllowedToolIDs = []i.ToolID{}
			if test.allowed {
				current.AllowedToolIDs = []i.ToolID{id}
			}
			agent, err := ac.NewAgentConfig(current)
			if err != nil {
				t.Fatal(err)
			}
			metadata := &loopToolFactsControl{definition: tc.ReadFileDefinition(), err: test.metadataErr}
			def := metadata.definition
			tools := []mc.Tool{{Name: fields.Tools[0].ModelVisibleName, Description: def.Description, InputSchema: def.InputSchema, OutputSchema: def.OutputSchema}}
			facts, _ := runtimeModelCandidate(t)
			roundFields := facts.Round.Fields()
			roundFields.Input.SchemaVersion = mc.ToolInputSchemaVersion
			roundFields.Input.Digest, err = mc.ToolInputDigest(context.Background(), roundFields.Messages, tools, mc.ToolChoice{Kind: "required"})
			if err != nil {
				t.Fatal(err)
			}
			round, err := c.NewLoopRound(c.LoopRoundFields{Round: roundFields, Tools: tools, Choice: mc.ToolChoice{Kind: "required"}, Ordinal: 1, TranscriptStart: 1})
			if err != nil {
				t.Fatal(err)
			}
			run := &directTextCall{input: &preparationInputRecord{input: input}, round: loopExecutionRound{round}}
			state := &directTextState{tools: &toolCallDependencies{metadata: metadata}}
			err = state.requireCurrentLoopToolFacts(context.Background(), f.NewTx(), run, agent)
			if test.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				requireCode(t, err, test.want)
			}
			if metadata.checks != test.checks {
				t.Fatalf("current metadata checks=%d, want %d", metadata.checks, test.checks)
			}
		})
	}
}

func TestMultiRoundCurrentPolicyDeniesCapturedCoreAndOrdinary(t *testing.T) {
	fields := contextBuilderInput(t).Fields()
	id := newTestID[i.Tool](t)
	current := fields.Agent.Fields()
	current.AllowedToolIDs = []i.ToolID{id}
	agent, err := ac.NewAgentConfig(current)
	if err != nil {
		t.Fatal(err)
	}
	policy := fields.Request.Launch.Policy
	policy.DeniedToolIDs = []i.ToolID{id}
	for _, class := range []tc.ToolClass{tc.CoreTool, tc.OrdinaryTool} {
		requireCode(t, requireCurrentCapturedTool(agent, policy, tc.ExecutionTool{ToolID: id, BindingSnapshot: tc.BuiltinExecutionBinding{Class: class}}), f.Forbidden)
	}
}
