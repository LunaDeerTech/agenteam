//go:build integration

package model_test

import (
	"crypto/sha256"
	"encoding/json"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

type meetingSummaryFixture struct{ *systemHTTPFixture }

func newMeetingSummaryFixture(t *testing.T, initialize bool) *meetingSummaryFixture {
	t.Helper()
	v := &meetingSummaryFixture{newSystemHTTPFixture(t)}
	if initialize {
		if e := v.service.InitializeMeetingSummarySelection(testContext(t)); e != nil {
			t.Fatal(e)
		}
	}
	return v
}
func (v *meetingSummaryFixture) summary(t *testing.T) mc.MeetingSummarySelection {
	t.Helper()
	s, e := v.service.GetMeetingSummarySelection(testContext(t), v.admin)
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func (v *meetingSummaryFixture) choice(t *testing.T, m mc.ModelID) mc.UpdateMeetingSummarySelectionRequest {
	t.Helper()
	s := v.summary(t)
	return mc.UpdateMeetingSummarySelectionRequest{CommandMeta: mc.CommandMeta{Actor: v.admin, Scope: id.SystemScope(), Key: f.IdempotencyKey(newID[struct{}](t).String())}, SelectionID: s.ID, ExpectedVersion: s.Version, Model: m}
}
func (v *meetingSummaryFixture) selectModel(t *testing.T, m mc.ModelID) mc.CommandReceipt {
	t.Helper()
	r, e := v.service.UpdateMeetingSummarySelection(testContext(t), v.choice(t, m))
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func (v *meetingSummaryFixture) chat(t *testing.T, memory bool) mc.ModelID {
	t.Helper()
	provider := v.provider(t, mc.OpenAIChat)
	body := httpModelBody(provider, mc.ChatModel)
	if memory {
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
	out := v.systemHTTPFixture.facts(t)
	var raw []byte
	if e := v.raw.QueryRow(testContext(t), `SELECT coalesce(jsonb_agg(to_jsonb(s) ORDER BY id),'[]'::jsonb) FROM agenteam_model.meeting_summary_selection s`).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	out["agenteam_model.meeting_summary_selection"] = sha256.Sum256(raw)
	return out
}
func (v *meetingSummaryFixture) setMemory(t *testing.T, m mc.ModelID) mc.UpdatePlatformSelectionRequest {
	t.Helper()
	s, e := v.service.GetPlatformSelection(testContext(t), v.admin)
	if e != nil {
		t.Fatal(e)
	}
	embedRaw := v.model(t, v.provider(t, mc.OpenAIEmbeddings), mc.EmbeddingModel)
	embed, e := f.ParseID[mc.Model](embedRaw)
	if e != nil {
		t.Fatal(e)
	}
	r := mc.UpdatePlatformSelectionRequest{CommandMeta: mc.CommandMeta{Actor: v.admin, Scope: id.SystemScope(), Key: f.IdempotencyKey(newID[struct{}](t).String())}, Selection: mc.PlatformSelection{ID: s.ID, Version: s.Version, Embedding: embed, Memory: m}, ExpectedVersion: s.Version}
	if _, e = v.service.UpdatePlatformSelection(testContext(t), r); e != nil {
		t.Fatal(e)
	}
	return r
}
func summaryJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
