package model

import (
	"encoding/json"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"testing"
	"time"
)

func summaryPlanFixture(t *testing.T) mutationPlan {
	at, _ := f.NewInstant(time.Now())
	key := mustID[struct{}](t).String()
	modelID, providerID := mustID[mc.Model](t).String(), mustID[mc.Provider](t).String()
	m := modelRecord{ID: modelID, ProviderID: providerID, Version: 1, CreatedAt: at, UpdatedAt: at, Input: mc.ModelInput{Notes: "plain text", ProviderModelID: "plain", Type: mc.ChatModel, Enabled: true, Capabilities: mc.Capabilities{StructuredOutputModes: []string{"json_schema"}}, Parameters: json.RawMessage(`{}`), RequestOverwrite: json.RawMessage(`{}`), HeaderOverwrite: map[string]string{}}}
	provider := providerRecord{ID: providerID, Input: providerInput{Name: "provider", Protocol: mc.OpenAIChat, BaseURL: "https://provider.example/v1", Enabled: true, Options: json.RawMessage(`{}`)}, Version: 1, CreatedAt: at, UpdatedAt: at}
	actor := testActor(t)
	ci, e := commandIdentity(mc.CommandMeta{Actor: actor, Scope: id.SystemScope(), Key: "summary-plan"}, "model.selection.update")
	if e != nil {
		t.Fatal(e)
	}
	return mutationPlan{CommandID: mustID[struct{}](t).String(), User: actor.Details().UserID, Identity: ci.Canonical(), Semantic: hash([]byte("summary-plan")), Kind: "model.selection.update", Resource: key, BeforeSelection: &selectionRecord{SummaryReasoning: mc.ReasoningChoice{Mode: "default"}, ID: key, Version: 1}, AfterSelection: &selectionRecord{SummaryReasoning: mc.ReasoningChoice{Mode: "default"}, ID: key, Version: 2, Configured: true, Embedding: mustID[mc.Model](t).String(), Summary: modelID}, At: at, Models: []modelRecord{m}, Providers: []providerRecord{provider}, References: []referenceRecord{}, Receipt: mc.CommandReceipt{Kind: "model.selection.update", ResourceID: key, Version: 2}, Metadata: ac.ModelMetadataFields{SelectionID: key, SelectorKind: "platform", Version: 2, ChangedFields: []string{"selection"}}, Changed: []string{"selection"}}
}
