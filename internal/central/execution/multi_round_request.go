package execution

import (
	"context"

	"github.com/LunaDeerTech/agenteam/internal/central/agentloop"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

func (s *directTextState) multiProcess() oc.ProcessID {
	id, _ := f.ParseID[oc.Process](s.process.String())
	return id
}
func isLoopRound(r executionRound) bool { _, ok := r.(loopExecutionRound); return ok }
func roundStorageSchema(r executionRound) f.Version {
	if isLoopRound(r) {
		return c.LoopRoundSchemaVersion
	}
	return r.Fields().Input.SchemaVersion
}
func roundInputThrough(r executionRound) f.Sequence {
	if v, ok := r.(loopExecutionRound); ok {
		x := v.LoopRound.Fields()
		if x.Ordinal > 1 {
			return x.TranscriptStart - 1
		}
	}
	return 1
}
func roundAssistantSequence(r executionRound) f.Sequence { return roundInputThrough(r) + 1 }

func capturedToolByName(input c.PreparationInput, name string) (tc.ExecutionTool, error) {
	for _, tool := range input.Fields().Tools {
		if tool.ModelVisibleName == name {
			return tool, nil
		}
	}
	return tc.ExecutionTool{}, fault(f.Forbidden)
}

func loopModelRequest(ctx context.Context, snapshot c.DirectTextSnapshot, r executionRound) (mc.ModelRequest, error) {
	v, ok := r.(loopExecutionRound)
	if !ok {
		return mc.ModelRequest{}, invalid()
	}
	fields := v.LoopRound.Fields()
	base := fields.Round
	p := snapshot.Fields().Context.Input().Fields()
	actor, err := i.NewAgentRun(p.Request.Launch.ProjectID, p.Request.Launch.AgentID, p.Request.ExecutionID)
	if err != nil {
		return mc.ModelRequest{}, err
	}
	request := mc.ModelRequest{Actor: actor, CallID: base.CallID, Consumer: p.Model.Consumer.Clone(), Model: p.Model.Clone(), Input: base.Input.Clone(), Messages: base.Messages, Tools: fields.Tools, ToolChoice: fields.Choice, ResponseFormat: mc.ResponseFormat{Kind: "text"}, RetryClass: mc.AgentRetry}
	validated, err := agentloop.NewLoopRequest(ctx, base.ContextDigest, request)
	if err != nil {
		return mc.ModelRequest{}, err
	}
	return validated.Request(), nil
}

func (s *directTextState) acceptsRunnerBinding(b tc.RunnerBinding) bool {
	if s.multi != nil {
		return b == tc.ReadFileRegistration().Binding || b == tc.WriteFileRegistration().Binding || b == tc.RunCommandRegistration().Binding
	}
	return s.tools != nil && s.tools.profile() == b
}

func (s *directTextState) acceptsToolBinding(b tc.ToolCallBinding) bool {
	if b.RunnerBinding == (tc.RunnerBinding{}) {
		return s.multi != nil && tc.WorkArtifactBuiltinBinding(b.Binding)
	}
	return b.Binding == (tc.BuiltinBinding{}) && s.acceptsRunnerBinding(b.RunnerBinding)
}
