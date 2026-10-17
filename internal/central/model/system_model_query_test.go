package model

import (
	"context"
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
	r := httptest.NewRequest("GET", "https://example.test/models?provider_id="+provider.String()+"&q=%20OpenAI%20_25%25%20&limit=2&type=embedding", nil)
	q, filter, err := httpListQuery(r, true)
	if err != nil || q.Search != "openai _25%" || filter != provider || q.Limit != 2 || q.Type != mc.EmbeddingModel {
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
	binding, err := modelListBinding(actor, "", q.Search, q.Type)
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
		other, _ := modelListBinding(actor, change.provider, change.search, q.Type)
		if _, err := s.pagePosition(SystemQuery{Cursor: token, Limit: 2}, other); err == nil {
			t.Fatal("cursor crossed query boundary")
		}
	}
	if _, err := s.pagePosition(SystemQuery{Cursor: token, Limit: 1}, binding); err != nil {
		t.Fatal("same query cursor rejected", err)
	}
}

func TestSystemModelTypeQueryAndCursor(t *testing.T) {
	types := []mc.ModelType{"", mc.ChatModel, mc.EmbeddingModel, mc.RerankerModel, mc.ImageModel}
	actor := testActor(t)
	store := &noIOStore{}
	s, err := New(store, pureAuthority(t, store), testDependencies(t))
	if err != nil {
		t.Fatal(err)
	}
	at, _ := f.NewInstant(time.Now())
	top := mustID[mc.Model](t).String()
	for _, typ := range types {
		query := ""
		if typ != "" {
			query = "?type=" + string(typ)
		}
		r := httptest.NewRequest("GET", "https://example.test/models"+query, nil)
		q, provider, err := httpListQuery(r, true)
		if err != nil || q.Type != typ || provider != (mc.ProviderID{}) {
			t.Fatalf("type %q query: %#v, %v", typ, q, err)
		}
		if typ != "" {
			if _, _, err := httpListQuery(r, false); err == nil {
				t.Fatalf("provider listing accepted type %q", typ)
			}
		}
		binding, err := modelListBinding(actor, "", "", typ)
		if err != nil {
			t.Fatal(err)
		}
		token, err := s.signPage(binding, pagePosition{}, at, top, at, top)
		if err != nil {
			t.Fatal(err)
		}
		for _, otherType := range types {
			other, err := modelListBinding(actor, "", "", otherType)
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.pagePosition(SystemQuery{Cursor: token, Limit: 1}, other)
			if otherType == typ {
				if err != nil {
					t.Fatalf("same type %q rejected: %v", typ, err)
				}
			} else {
				requireCode(t, err, f.CursorInvalid)
			}
		}
	}
	for _, query := range []string{"?type=", "?type=chat&type=embedding", "?type=chat&type=chat", "?type=summary", "?type=image", "?type=CHAT", "?type=%20chat", "?type=chat%20", "?type=%00", "?type=%ff"} {
		if _, _, err := httpListQuery(httptest.NewRequest("GET", "https://example.test/models"+query, nil), true); err == nil {
			t.Fatalf("accepted invalid type query %q", query)
		}
	}
	_, err = s.ListModels(context.Background(), actor, mc.ProviderID{}, SystemQuery{Limit: 1, Type: "summary"})
	requireCode(t, err, f.InvalidArgument)
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
