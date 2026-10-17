//go:build integration

package model_test

import (
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

// This independent check uses the real management HTTP handler, account/session
// authority and PostgreSQL. It does not start a full App or call any provider.
func TestSystemModelTypeFilterHTTPIndependentReview(t *testing.T) {
	// Full-schema preparation has its own budget; model HTTP requests retain the
	// shared fixture's 20-second operation contexts. No production timeout changes.
	db := pgfixture.NewDatabase(t)
	cfg := db.Config(t, map[string]string{"STARTUP_TIMEOUT": "120s"})
	migrator, err := postgres.NewMigrator(cfg)
	if err != nil {
		t.Fatal("prepare migration", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), cfg.StartupTimeout())
	started := time.Now()
	migrated := migrator.Migrate(ctx)
	cancel()
	if !migrated.Migrated {
		t.Fatalf("owned schema preparation failed code=%s sql_state=%s elapsed=%s", migrated.Fault.Code(), migrated.Fault.SQLState(), time.Since(started))
	}
	t.Logf("owned schema preparation completed in %s", time.Since(started))
	v := newSystemHTTPFixture(t, db)
	providers := make([]string, 2)
	for i, name := range []string{"North supplier", "South Needle supplier"} {
		body := httpProviderBody(mc.OpenAICompatible)
		body["input"].(map[string]any)["name"] = name
		providers[i] = v.request(t, v.adminBrowser, "POST", "/api/v1/system/model-providers", newID[struct{}](t).String(), body).want(t, 200).object(t)["resource_id"].(string)
	}
	kinds := []mc.ModelType{mc.ChatModel, mc.EmbeddingModel, mc.RerankerModel, mc.ImageModel}
	expected := map[mc.ModelType][]string{}
	for _, kind := range kinds {
		for i, provider := range providers {
			body := httpModelBody(provider, kind)
			input := body["input"].(map[string]any)
			input["provider_model_id"] = "remote-" + string(kind)
			input["notes"] = "Needle " + string(kind)
			caps := input["capabilities"].(map[string]any)
			// The shared historical helper predates mandatory embedding metadata.
			// Supply the current contract locally without changing that helper.
			caps["embedding"] = nil
			if kind == mc.EmbeddingModel {
				caps["embedding"] = map[string]any{"dimensions": "1536", "dimensions_mode": "fixed"}
			}
			if kind == mc.ChatModel {
				caps["structured_output_modes"] = []string{"text", "json_schema"}
			}
			if kind == mc.RerankerModel && i == 1 {
				input["enabled"] = false // Catalog filtering must preserve same-type disabled rows.
			}
			created := v.request(t, v.adminBrowser, "POST", "/api/v1/system/models", newID[struct{}](t).String(), body).want(t, 200).object(t)["resource_id"].(string)
			expected[kind] = append(expected[kind], created)
		}
	}
	read := func(query string) model.SystemModelPage {
		t.Helper()
		response := v.request(t, v.adminBrowser, "GET", "/api/v1/system/models?"+query, "", nil).want(t, 200)
		var page model.SystemModelPage
		if err := json.Unmarshal(response.body, &page); err != nil {
			t.Fatal("decode complete model page", err)
		}
		return page
	}
	wantIDs := func(page model.SystemModelPage, wanted []string) {
		t.Helper()
		actual := make([]string, 0, len(page.Items))
		for _, item := range page.Items {
			actual = append(actual, item.ID.String())
			if item.Provider.ID != item.ProviderID || item.Provider.Protocol != mc.OpenAICompatible || item.Provider.Name == "" {
				t.Fatal("joined provider metadata lost")
			}
		}
		wanted = slices.Clone(wanted)
		slices.Sort(actual)
		slices.Sort(wanted)
		if !slices.Equal(actual, wanted) {
			t.Fatalf("model identities differ: got %v want %v", actual, wanted)
		}
	}

	// Creation order places both image rows ahead of every embedding row.
	// A LIMIT-then-type implementation would therefore return an empty first page.
	unfiltered := read("limit=2")
	wantIDs(unfiltered, expected[mc.ImageModel])
	if unfiltered.NextCursor == "" {
		t.Fatal("mixed management catalog lacks continuation")
	}
	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			query := "limit=1&type=" + string(kind) + "&q=needle"
			first := read(query)
			if len(first.Items) != 1 || first.NextCursor == "" || first.Items[0].Input.Type != kind {
				t.Fatal("typed first page did not filter before pagination")
			}
			second := read(query + "&cursor=" + url.QueryEscape(first.NextCursor))
			if len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].Input.Type != kind {
				t.Fatal("typed continuation did not terminate at the matching rows")
			}
			wantIDs(model.SystemModelPage{Items: append(first.Items, second.Items...)}, expected[kind])
			canonical := read("limit=1&type=" + string(kind) + "&q=%20NEEDLE%20&cursor=" + url.QueryEscape(first.NextCursor))
			wantIDs(canonical, []string{second.Items[0].ID.String()})
			combined := read("limit=10&type=" + string(kind) + "&q=needle&provider_id=" + providers[1])
			wantIDs(combined, expected[kind][1:])
			if kind == mc.RerankerModel && combined.Items[0].Input.Enabled {
				t.Fatal("same-type disabled candidate changed")
			}
			for _, changed := range []string{
				"limit=1&q=needle", // A typed cursor cannot become an untyped listing.
				"limit=1&type=" + string(kinds[(slices.Index(kinds, kind)+1)%len(kinds)]) + "&q=needle",
				"limit=1&type=" + string(kind) + "&q=other",
				"limit=1&type=" + string(kind) + "&q=needle&provider_id=" + providers[1],
			} {
				v.request(t, v.adminBrowser, "GET", "/api/v1/system/models?"+changed+"&cursor="+url.QueryEscape(first.NextCursor), "", nil).problem(t, 400, f.CursorInvalid)
			}
		})
	}
	var all []string
	for _, kind := range kinds {
		all = append(all, expected[kind]...)
	}
	wantIDs(read("limit=100"), all)
	wantIDs(read("limit=100&q=south"), []string{expected[mc.ChatModel][1], expected[mc.EmbeddingModel][1], expected[mc.RerankerModel][1], expected[mc.ImageModel][1]})
	wantIDs(read("limit=100&type=embedding&q=remote-embedding&provider_id="+providers[0]), expected[mc.EmbeddingModel][:1])
	empty := read("limit=1&type=embedding&q=remote-chat")
	if len(empty.Items) != 0 || empty.NextCursor != "" {
		t.Fatal("empty typed search fabricated rows or a continuation")
	}
	v.request(t, v.adminBrowser, "GET", "/api/v1/system/models?limit=2&type=chat&cursor="+url.QueryEscape(unfiltered.NextCursor), "", nil).problem(t, 400, f.CursorInvalid)
	for _, query := range []string{"type=", "type=CHAT", "type=unknown", "type=chat&type=chat", "type=chat&type=embedding"} {
		v.request(t, v.adminBrowser, "GET", "/api/v1/system/models?"+query, "", nil).problem(t, 400, f.InvalidArgument)
	}
	t.Log("real HTTP/account/PostgreSQL: four types across providers, filter-before-limit, search/provider composition, cursor boundaries and untyped management catalog passed")
}
