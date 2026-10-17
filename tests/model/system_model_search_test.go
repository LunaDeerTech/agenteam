//go:build integration

package model_test

import (
	"encoding/json"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func TestSystemModelSearchAcrossProviders(t *testing.T) {
	v := newFixture(t)
	createProvider := func(name string) mc.ProviderView {
		t.Helper()
		r, err := v.service.CreateProvider(testContext(t), mc.CreateProviderRequest{CommandMeta: v.meta(t, newID[struct{}](t).String()), Input: mc.ProviderInput{Name: name, Protocol: mc.OpenAICompatible, BaseURL: "https://owned.invalid/api", Enabled: true, Options: json.RawMessage(`{}`)}})
		if err != nil {
			t.Fatal("provider setup", err)
		}
		key, _ := f.ParseID[mc.Provider](r.ResourceID)
		p, err := v.service.GetProvider(testContext(t), v.admin, key)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	createModel := func(provider mc.ProviderView, name, remote string) mc.ModelID {
		t.Helper()
		r, err := v.service.CreateModel(testContext(t), mc.CreateModelRequest{CommandMeta: v.meta(t, newID[struct{}](t).String()), ProviderID: provider.ID, Input: mc.ModelInput{Name: name, ProviderModelID: remote, Type: mc.ChatModel, Enabled: true, Parameters: json.RawMessage(`{}`), RequestOverwrite: json.RawMessage(`{}`)}})
		if err != nil {
			t.Fatal("model setup", err)
		}
		key, _ := f.ParseID[mc.Model](r.ResourceID)
		return key
	}
	a := createProvider("Supplier Needle")
	b := createProvider(strings.Repeat("供应商", 30))
	m1 := createModel(a, "Alpha", "remote-a")
	m2 := createModel(b, "Model Needle", "literal_%")
	m3 := createModel(b, "Gamma", "RemoteNeedle")
	createModel(b, "Ordinary", "plain")
	input := b.Input.Clone()
	input.Enabled = false
	if _, err := v.service.UpdateProvider(testContext(t), mc.UpdateProviderRequest{CommandMeta: v.meta(t, newID[struct{}](t).String()), ID: b.ID, ExpectedVersion: b.Version, Input: input}); err != nil {
		t.Fatal(err)
	}
	all := mc.ProviderID{}
	first, err := v.service.ListModels(testContext(t), v.admin, all, model.SystemQuery{Limit: 1, Search: " NeEdLe "})
	if err != nil || len(first.Items) != 1 || first.NextCursor == "" {
		t.Fatal("first global page", err)
	}
	// A new matching row above the original watermark must not enter page two.
	later := createModel(a, "Later", "remote-later")
	seen := map[mc.ModelID]bool{first.Items[0].ID: true}
	cursor := first.NextCursor
	for cursor != "" {
		page, err := v.service.ListModels(testContext(t), v.admin, all, model.SystemQuery{Limit: 1, Search: "needle", Cursor: cursor})
		if err != nil || len(page.Items) != 1 {
			t.Fatal("continuation", err)
		}
		item := page.Items[0]
		if seen[item.ID] || item.ID == later || len(seen) >= 3 {
			t.Fatal("duplicate/new row entered bounded page")
		}
		seen[item.ID] = true
		if item.Provider.ID != item.ProviderID || item.Provider.Version.Validate() != nil || item.Provider.Protocol != mc.OpenAICompatible {
			t.Fatal("invalid joined provider display")
		}
		cursor = page.NextCursor
	}
	if len(seen) != 3 || !seen[m1] || !seen[m2] || !seen[m3] {
		t.Fatal("search missed one of its three fields")
	}
	filtered, err := v.service.ListModels(testContext(t), v.admin, b.ID, model.SystemQuery{Limit: 100, Search: "needle"})
	if err != nil || len(filtered.Items) != 2 {
		t.Fatal("provider filter", err)
	}
	for _, item := range filtered.Items {
		if item.Provider.Name != b.Input.Name || item.Provider.Enabled || item.Provider.Version != b.Version+1 {
			t.Fatal("disabled/current provider display lost")
		}
	}
	for _, literal := range []string{"%", "_"} {
		page, err := v.service.ListModels(testContext(t), v.admin, all, model.SystemQuery{Limit: 100, Search: literal})
		if err != nil || len(page.Items) != 1 || page.Items[0].ID != m2 {
			t.Fatal("wildcard interpreted instead of literal", err)
		}
	}
	for _, query := range []struct {
		provider mc.ProviderID
		search   string
	}{{b.ID, "needle"}, {all, "different"}, {all, ""}} {
		_, err := v.service.ListModels(testContext(t), v.admin, query.provider, model.SystemQuery{Limit: 1, Cursor: first.NextCursor, Search: query.search})
		requireCode(t, err, f.CursorInvalid)
	}
	_, err = v.service.ListModels(testContext(t), v.regular, all, model.SystemQuery{Limit: 10})
	requireCode(t, err, f.Forbidden)
	otherAdmin := v.human(t, "admin")
	_, err = v.service.ListModels(testContext(t), otherAdmin, all, model.SystemQuery{Limit: 1, Cursor: first.NextCursor, Search: "needle"})
	requireCode(t, err, f.CursorInvalid)
	foreign, foreignModel := newID[mc.Provider](t), newID[mc.Model](t)
	_, err = v.raw.Exec(testContext(t), `INSERT INTO agenteam_model.providers(id,scope,project_id,name,protocol,base_url,provider_options,enabled,version,created_at,updated_at) VALUES($1,'project',$2,'Project Needle','openai-chat-completions','https://owned.invalid','{}',true,1,clock_timestamp(),clock_timestamp())`, foreign.String(), newID[id.Project](t).String())
	if err != nil {
		t.Fatal("foreign provider fixture", err)
	}
	_, err = v.raw.Exec(testContext(t), `INSERT INTO agenteam_model.models(id,provider_id,type,name,provider_model_id,parameters,request_overwrite,header_overwrite,capabilities,enabled,version,created_at,updated_at) VALUES($1,$2,'chat','Foreign Needle','foreign','{}','{}','{}','{}',true,1,clock_timestamp(),clock_timestamp())`, foreignModel.String(), foreign.String())
	if err != nil {
		t.Fatal("foreign model fixture", err)
	}
	page, err := v.service.ListModels(testContext(t), v.admin, all, model.SystemQuery{Limit: 100})
	if err != nil || len(page.Items) != 5 {
		t.Fatal("global query leaked or lost scope", err)
	}
	_, err = v.service.ListModels(testContext(t), v.admin, foreign, model.SystemQuery{Limit: 100})
	requireCode(t, err, f.NotFound)
}
