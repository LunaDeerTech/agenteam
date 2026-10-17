//go:build integration

package model_test

import (
	"encoding/json"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

type meetingSummaryFixture struct{ *systemHTTPFixture }

func newMeetingSummaryFixture(t *testing.T) *meetingSummaryFixture {
	t.Helper()
	return &meetingSummaryFixture{newSystemHTTPFixture(t)}
}
func (v *meetingSummaryFixture) summary(t *testing.T) model.PlatformSelectionState {
	t.Helper()
	s, e := v.service.GetPlatformSelection(testContext(t), v.admin)
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func (v *meetingSummaryFixture) choice(t *testing.T, m mc.ModelID) mc.UpdatePlatformSelectionRequest {
	t.Helper()
	s := v.summary(t)
	selection := mc.PlatformSelection{ID: s.ID, Version: s.Version, Summary: mc.DefaultModelChoice(m)}
	if s.Configured != nil {
		selection = s.Configured.Clone()
		selection.Summary = mc.DefaultModelChoice(m)
	} else {
		embed := v.model(t, v.provider(t, mc.OpenAIEmbeddings), mc.EmbeddingModel)
		embedding, e := f.ParseID[mc.Model](embed)
		if e != nil {
			t.Fatal(e)
		}
		selection.Embedding = mc.DefaultModelChoice(embedding)
	}
	return mc.UpdatePlatformSelectionRequest{CommandMeta: mc.CommandMeta{Actor: v.admin, Scope: id.SystemScope(), Key: f.IdempotencyKey(newID[struct{}](t).String())}, Selection: selection, ExpectedVersion: s.Version}
}
func (v *meetingSummaryFixture) selectModel(t *testing.T, m mc.ModelID) mc.CommandReceipt {
	t.Helper()
	r, e := v.service.UpdatePlatformSelection(testContext(t), v.choice(t, m))
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func (v *meetingSummaryFixture) chat(t *testing.T, structured bool) mc.ModelID {
	t.Helper()
	provider := v.provider(t, mc.OpenAIChat)
	body := httpModelBody(provider, mc.ChatModel)
	if structured {
		body["input"].(map[string]any)["capabilities"].(map[string]any)["structured_output_modes"] = []string{"json_schema"}
	}
	r := v.request(t, v.adminBrowser, "POST", "/api/v1/system/models", newID[struct{}](t).String(), body).want(t, 200).object(t)
	key, e := f.ParseID[mc.Model](r["resource_id"].(string))
	if e != nil {
		t.Fatal(e)
	}
	return key
}
func (v *meetingSummaryFixture) summaryFacts(t *testing.T) map[string][32]byte {
	t.Helper()
	return v.systemHTTPFixture.facts(t)
}
func summaryJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
