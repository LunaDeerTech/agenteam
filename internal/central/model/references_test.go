package model

import (
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func TestModelReplacementPreservesRequiredTypeAndSummaryCapability(t *testing.T) {
	chat := mc.ModelInput{Notes: "chat", ProviderModelID: "chat", Type: mc.ChatModel, Enabled: true}
	requireCode(t, selectionCompatible("summary", chat, ""), f.CapabilityUnsupported)
	chat.Capabilities.StructuredOutputModes = []string{"json_schema"}
	if e := selectionCompatible("summary", chat, ""); e != nil {
		t.Fatal(e)
	}
	requireCode(t, selectionCompatible("embedding", chat, ""), f.InvalidArgument)
	requireCode(t, selectionCompatible("summary", chat, "high"), f.CapabilityUnsupported)
	chat.Enabled = false
	requireCode(t, selectionCompatible("summary", chat, ""), f.InvalidArgument)
	selection := &selectionRecord{SummaryReasoning: mc.ReasoningChoice{Mode: "default"}, ID: mustID[struct{}](t).String(), Version: 4, Configured: true, Embedding: mustID[mc.Model](t).String(), Summary: mustID[mc.Model](t).String()}
	refs := selectionReferences(selection)
	if len(refs) != 2 || refs[0].Role != "embedding" || refs[1].Role != "summary" || refs[0].Version != 4 || refs[1].Owner != selection.ID {
		t.Fatal("canonical reference shape changed")
	}
}
