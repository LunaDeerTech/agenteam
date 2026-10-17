package model

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func TestSystemModelSearchQueryAndCursor(t *testing.T) {
	provider := mustID[mc.Provider](t)
	r := httptest.NewRequest("GET", "https://example.test/models?provider_id="+provider.String()+"&q=%20OpenAI%20_25%25%20&limit=2", nil)
	q, filter, err := httpListQuery(r, true)
	if err != nil || q.Search != "openai _25%" || filter != provider || q.Limit != 2 {
		t.Fatal("canonical search", err)
	}
	for _, query := range []string{"?provider_id=", "?q=", "?q=%20", "?q=x&q=y", "?q=%00", "?q=%0a", "?q=%ff", "?q=" + strings.Repeat("a", 129), "?q=x&purpose=summary"} {
		if _, _, err := httpListQuery(httptest.NewRequest("GET", "https://example.test/models"+query, nil), true); err == nil {
			t.Fatalf("accepted invalid query %q", query)
		}
	}
	if _, _, err := httpListQuery(r, false); err == nil {
		t.Fatal("provider listing accepted model query")
	}
	actor := testActor(t)
	store := &noIOStore{}
	s, err := New(store, pureAuthority(t, store), testDependencies(t))
	if err != nil {
		t.Fatal(err)
	}
	binding, err := modelListBinding(actor, "", q.Search)
	if err != nil {
		t.Fatal(err)
	}
	at, _ := f.NewInstant(time.Now())
	older, _ := f.NewInstant(at.Time().Add(-time.Second))
	top, last := mustID[mc.Model](t).String(), mustID[mc.Model](t).String()
	token, err := s.signPage(binding, pagePosition{}, at, top, older, last)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct{ provider, search string }{{provider.String(), q.Search}, {"", "other"}, {"", ""}} {
		other, _ := modelListBinding(actor, change.provider, change.search)
		if _, err := s.pagePosition(SystemQuery{Cursor: token, Limit: 2}, other); err == nil {
			t.Fatal("cursor crossed query boundary")
		}
	}
	if _, err := s.pagePosition(SystemQuery{Cursor: token, Limit: 1}, binding); err != nil {
		t.Fatal("same query cursor rejected", err)
	}
}

func TestSystemModelListProviderProjectionIsClosed(t *testing.T) {
	display := ModelProviderSummary{ID: mustID[mc.Provider](t), Name: "供应商", Protocol: mc.OpenAICompatible, Enabled: false, Version: 9007199254740993}
	raw, err := json.Marshal(display)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil || len(fields) != 5 {
		t.Fatal("unexpected display fields", err)
	}
	for _, key := range []string{"id", "name", "protocol", "enabled", "version"} {
		if _, ok := fields[key]; !ok {
			t.Fatal("missing display field", key)
		}
	}
	if string(fields["version"]) != `"9007199254740993"` {
		t.Fatal("version lost precision")
	}
}
