package model

import (
	"encoding/json"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"strings"
	"testing"
)

func TestPickerReasoningSelectionReferencesAndCompatibility(t *testing.T) {
	selected := &selectionRecord{ID: mustID[struct{}](t).String(), Version: 7, Configured: true, Embedding: mustID[mc.Model](t).String(), Summary: mustID[mc.Model](t).String(), SummaryReasoning: mc.ReasoningChoice{Mode: "off"}}
	refs := selectionReferences(selected)
	if len(refs) != 2 || refs[1].ReasoningMode != "off" || refs[1].Effort != "" {
		t.Fatal("choice lost from reference")
	}
	state, err := selectionView(selected)
	if err != nil || state.Configured.Summary.Reasoning.Mode != "off" {
		t.Fatal("choice lost on read", err)
	}
	raw, err := json.Marshal(httpSelectionDTO(state))
	if err != nil || !strings.Contains(string(raw), `"reasoning":{"mode":"off"}`) {
		t.Fatal("wire choice", err)
	}
	a, b := "enable_thinking", "thinking_type"
	before := mc.ModelInput{Type: mc.ChatModel, Enabled: true, Capabilities: mc.Capabilities{Reasoning: true, ReasoningToggleMode: &a}}
	after := before.Clone()
	after.Capabilities.ReasoningToggleMode = &b
	if compatibleReferencedReasoning(refs[1], before, after) != nil {
		t.Fatal("valid target dialect")
	}
	after.Enabled = false
	if compatibleReferencedReasoning(refs[1], before, after) != nil {
		t.Fatal("disable semantics changed")
	}
	after.Capabilities.ReasoningToggleMode = nil
	requireCode(t, compatibleReferencedReasoning(refs[1], before, after), f.CapabilityUnsupported)
	refs[1].ReasoningMode = "effort"
	refs[1].Effort = "custom:depth"
	after.Capabilities.ReasoningEfforts = []mc.ReasoningEffort{{Name: "深度", Value: "custom:depth"}}
	if compatibleReferencedReasoning(refs[1], before, after) != nil {
		t.Fatal("declared effort")
	}
	after.Capabilities.ReasoningEfforts = nil
	requireCode(t, compatibleReferencedReasoning(refs[1], before, after), f.CapabilityUnsupported)
}
func TestPickerReasoningTrustedSnapshotAndClosedHTTP(t *testing.T) {
	off := false
	snap := resolutionSnapshotDTO{ReasoningEnabled: &off}
	req := resolutionRequestDTO{Source: mc.CurrentSelectionSource, Selection: &mc.SelectionRef{Kind: "platform", Selector: mc.SummarySelector}}
	if !resolutionReasoningMatches(req, snap) {
		t.Fatal("trusted frozen choice")
	}
	req.ReasoningEffort = "injected"
	if resolutionReasoningMatches(req, snap) {
		t.Fatal("platform injection")
	}
	req.ReasoningEffort = ""
	req.Selection.Kind = "direct"
	if resolutionReasoningMatches(req, snap) {
		t.Fatal("direct bool injection")
	}
	req.Selection = &mc.SelectionRef{Kind: "platform", Selector: mc.EmbeddingSelector}
	if resolutionReasoningMatches(req, snap) {
		t.Fatal("embedding toggle")
	}
	for _, mutate := range []func(string) string{
		func(s string) string { return strings.Replace(s, `"notes":"test",`, ``, 1) },
		func(s string) string { return strings.Replace(s, `"notes":"test"`, `"notes":null`, 1) },
		func(s string) string { return strings.Replace(s, `"notes":"test"`, `"name":"test"`, 1) },
		func(s string) string { return strings.Replace(s, `"reasoning_toggle_mode":null,`, ``, 1) },
	} {
		var d httpModelInput
		err := decodeHTTP(t, mutate(httpValidModel), &d)
		if err == nil {
			_, err = d.input()
		}
		if err == nil {
			t.Fatal("missing/null/legacy field accepted")
		}
	}
}
