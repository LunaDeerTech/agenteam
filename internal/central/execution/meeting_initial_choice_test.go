package execution

import (
	"context"
	"testing"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

func TestLoopRecoveryRejectsForgedTaskInitialAuto(t *testing.T) {
	facts, _ := runtimeModelCandidate(t)
	fields := facts.Round.Fields()
	definition := tc.ReadFileDefinition()
	tools := []mc.Tool{{Name: "tool_01900000000070008000000000000901", Description: definition.Description, InputSchema: definition.InputSchema, OutputSchema: definition.OutputSchema}}
	choice := mc.ToolChoice{Kind: "auto"}
	digest, err := mc.ToolInputDigest(context.Background(), fields.Messages, tools, choice)
	if err != nil {
		t.Fatal(err)
	}
	fields.Input.SchemaVersion, fields.Input.Digest = mc.ToolInputSchemaVersion, digest
	// A canonical data value is not a source grant. Even correctly recomputed
	// bytes and digest cannot change the original Task capture into a Meeting.
	round, err := c.NewLoopRound(c.LoopRoundFields{Round: fields, Tools: tools, Choice: choice, Ordinal: 1, TranscriptStart: 1})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := c.DecodeLoopRound(round.CanonicalBytes())
	if err != nil {
		t.Fatal(err)
	}
	_, err = loopModelRequest(context.Background(), facts.Snapshot, loopExecutionRound{decoded})
	requireCode(t, err, f.Forbidden)
}
