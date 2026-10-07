package model

import (
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func TestModelReplacementPreservesRequiredTypeAndMemoryCapability(t *testing.T) {
	chat := mc.ModelInput{Name: "chat", ProviderModelID: "chat", Type: mc.ChatModel, Enabled: true}
	if e := selectionCompatible("meeting_summary", chat, ""); e != nil {
		t.Fatal("plain text summary rejected", e)
	}
	requireCode(t, selectionCompatible("meeting_summary", chat, "high"), f.InvalidArgument)
	requireCode(t, selectionCompatible("memory", chat, ""), f.CapabilityUnsupported)
	chat.Capabilities.StructuredOutputModes = []string{"json_schema"}
	if e := selectionCompatible("memory", chat, ""); e != nil {
		t.Fatal(e)
	}
	requireCode(t, selectionCompatible("embedding", chat, ""), f.InvalidArgument)
	requireCode(t, selectionCompatible("memory", chat, "high"), f.CapabilityUnsupported)
	chat.Enabled = false
	requireCode(t, selectionCompatible("memory", chat, ""), f.InvalidArgument)
	selection := &selectionRecord{ID: mustID[struct{}](t).String(), Version: 4, Configured: true, Embedding: mustID[mc.Model](t).String(), Memory: mustID[mc.Model](t).String()}
	refs := selectionReferences(selection)
	if len(refs) != 2 || refs[0].Role != "embedding" || refs[1].Role != "memory" || refs[0].Version != 4 || refs[1].Owner != selection.ID {
		t.Fatal("canonical reference shape changed")
	}
}
