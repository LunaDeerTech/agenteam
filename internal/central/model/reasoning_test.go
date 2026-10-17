package model

import (
	"encoding/json"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func TestReasoningModelDefinitionAndSummaryDefault(t *testing.T) {
	input := mc.ModelInput{Name: "reasoning", ProviderModelID: "custom-model", Type: mc.ChatModel, Enabled: true, Parameters: json.RawMessage(`{}`), RequestOverwrite: json.RawMessage(`{}`), Capabilities: mc.Capabilities{Reasoning: true, InputModalities: []string{"text", "image", "file"}, OutputModalities: []string{"text"}, StructuredOutputModes: []string{"text", "json_schema"}, ReasoningEfforts: []mc.ReasoningEffort{{Name: "Deep", Value: "custom:deep"}}}}
	p := providerRecord{Input: providerInput{Protocol: mc.OpenAIChat, Enabled: true, Options: json.RawMessage(`{}`)}}
	for _, efforts := range [][]mc.ReasoningEffort{input.Capabilities.ReasoningEfforts, nil} {
		m := input.Clone()
		m.Capabilities.ReasoningEfforts = efforts
		if err := modelPolicy(m, mc.OpenAIChat); err != nil {
			t.Fatal(err)
		}
		rev, err := resolutionProfile(&p, &modelRecord{Input: m}, mc.SelectionRef{Kind: "platform", Selector: mc.SummarySelector})
		if err != nil || rev != adapter.OpenAIChatStructuredRevision {
			t.Fatal("declaration blocked default Summary use", err)
		}
	}
	for _, mutate := range []func(*mc.ModelInput){
		func(m *mc.ModelInput) { m.Capabilities.InputModalities = []string{"image"} },
		func(m *mc.ModelInput) { m.Capabilities.InputModalities = []string{"text", "vector"} },
		func(m *mc.ModelInput) { m.Capabilities.OutputModalities = []string{"vector"} },
	} {
		m := input.Clone()
		mutate(&m)
		requireCode(t, modelPolicy(m, mc.OpenAIChat), f.CapabilityUnsupported)
	}
	embedding := input.Clone()
	embedding.Type = mc.EmbeddingModel
	embedding.Capabilities = mc.Capabilities{InputModalities: []string{"text"}, OutputModalities: []string{"vector"}}
	if err := modelPolicy(embedding, mc.OpenAIEmbeddings); err != nil {
		t.Fatal(err)
	}
	n := mc.TokenCount(100)
	embedding.Capabilities.MaxOutput = &n
	requireCode(t, modelPolicy(embedding, mc.OpenAIEmbeddings), f.CapabilityUnsupported)
}

func TestReasoningResolutionSnapshotPersistsChoice(t *testing.T) {
	r := resolutionTestRequest(t)
	r.ReasoningEffort = "custom:deep"
	d := resolutionTestDraft(t, r)
	d.Snapshot.Capabilities.Reasoning = true
	d.Snapshot.Capabilities.ReasoningEfforts = []mc.ReasoningEffort{{Name: "Deep", Value: r.ReasoningEffort}}
	d.Snapshot.ReasoningEffort = r.ReasoningEffort
	if err := d.validate(); err != nil {
		t.Fatal(err)
	}
	raw, err := encoded(d)
	if err != nil {
		t.Fatal(err)
	}
	var recovered resolutionDraft
	if err = resolutionDecode(raw, 256<<10, &recovered); err != nil {
		t.Fatal(err)
	}
	snapshot, err := recovered.Snapshot.snapshot()
	if err != nil || snapshot.ReasoningEffort != r.ReasoningEffort {
		t.Fatal("persisted choice lost", err)
	}
	if !resolutionEqual(recovered, d) {
		t.Fatal("resolution changed on recovery")
	}
}
