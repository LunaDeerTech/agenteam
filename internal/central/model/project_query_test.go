package model

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func TestModelProjectCursorBindsProjectUserKindButNotLimitOrSession(t *testing.T) {
	actor := testActor(t)
	project := mustID[id.Project](t)
	scope, _ := id.InProject(project)
	store := &noIOStore{}
	s, err := New(store, pureAuthority(t, store), testDependencies(t))
	if err != nil {
		t.Fatal(err)
	}
	binding, _ := projectListBinding(actor, scope, "available-chat")
	now, _ := f.NewInstant(time.Now())
	older, _ := f.NewInstant(now.Time().Add(-time.Second))
	token, err := s.signPage(binding, pagePosition{}, now, mustID[mc.Model](t).String(), older, mustID[mc.Model](t).String())
	if err != nil {
		t.Fatal(err)
	}
	user, _ := f.ParseID[id.User](actor.Details().UserID)
	renewed, _ := id.NewHuman(user, mustID[id.Session](t))
	same, _ := projectListBinding(renewed, scope, "available-chat")
	for _, limit := range []int{1, 100} {
		if _, err = s.pagePosition(SystemQuery{token, limit}, same); err != nil {
			t.Fatal("limit/Session changed cursor", err)
		}
	}
	other, _ := id.InProject(mustID[id.Project](t))
	for _, tc := range []struct {
		actor id.Actor
		scope id.Scope
		kind  string
	}{{actor, other, "available-chat"}, {testActor(t), scope, "available-chat"}, {actor, scope, "models"}, {actor, scope, "providers"}} {
		b, _ := projectListBinding(tc.actor, tc.scope, tc.kind)
		if _, err = s.pagePosition(SystemQuery{token, 1}, b); err == nil {
			t.Fatal("cross boundary cursor accepted")
		}
	}
}
func TestModelProjectAvailableDTOContainsOnlySelectionFields(t *testing.T) {
	scope, _ := id.InProject(mustID[id.Project](t))
	count := mc.TokenCount(123)
	caps := mc.Capabilities{InputModalities: []string{"text"}, OutputModalities: []string{"text"}, StructuredOutputModes: []string{"text"}, Reasoning: true, ReasoningEfforts: []string{"medium"}, ContextLength: &count, MaxOutput: &count}
	v := AvailableChatModel{ID: mustID[mc.Model](t), ProviderID: mustID[mc.Provider](t), Scope: scope, Name: "public model", ProviderName: "public provider", Version: 1, Capabilities: caps.Clone()}
	caps.InputModalities[0] = "image"
	caps.OutputModalities[0] = "image"
	caps.StructuredOutputModes[0] = "json_schema"
	caps.ReasoningEfforts[0] = "high"
	count = 999
	if v.Capabilities.InputModalities[0] != "text" || v.Capabilities.OutputModalities[0] != "text" || v.Capabilities.StructuredOutputModes[0] != "text" || v.Capabilities.ReasoningEfforts[0] != "medium" || *v.Capabilities.ContextLength != 123 || *v.Capabilities.MaxOutput != 123 {
		t.Fatal("capabilities aliases caller storage")
	}
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	want := []string{"id", "provider_id", "scope", "name", "provider_name", "version", "capabilities"}
	if len(fields) != len(want) || reflect.TypeOf(v).NumField() != len(want) {
		t.Fatal("configuration fields escaped safe DTO")
	}
	for _, key := range want {
		if _, ok := fields[key]; !ok {
			t.Fatal("missing public selection field", key)
		}
	}
}
