package memory

import (
	"context"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/memory/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

func TestMemoryEmbeddingProfileRequiresOriginalLiveToolOwner(t *testing.T) {
	s := &Service{calls: map[*call]struct{}{}}
	registration, _ := i.RegisterService(i.MemoryRuntime)
	a, err := NewModelAuthority(s, registration)
	if err != nil {
		t.Fatal(err)
	}
	a.process = testID[oc.Process](t)
	ns := c.Namespace{ProjectID: testID[i.Project](t), AgentID: testID[i.Agent](t)}
	actor, _ := i.NewAgentRun(ns.ProjectID, ns.AgentID, testID[i.Execution](t))
	entry := &call{service: s, namespace: ns, actor: actor, operation: testID[tc.Operation](t), intent: "recall"}
	s.calls[entry] = struct{}{}
	ctx := context.WithValue(context.Background(), callKey{}, entry)
	request, err := a.profileRequest(entry)
	if err != nil || request.Validate() != nil {
		t.Fatal("original profile request", err)
	}
	plan, err := a.DiscoverEmbeddingProfile(ctx, request)
	if err != nil || plan.Validate() != nil || plan.Details().RetryPolicy != nil {
		t.Fatal("original profile discovery", err)
	}
	if _, err := a.DiscoverEmbeddingProfile(context.Background(), request); err == nil {
		t.Fatal("bare request granted configuration access")
	}
	for name, change := range map[string]func(*mc.EmbeddingProfileRequest){
		"namespace": func(r *mc.EmbeddingProfileRequest) { v := testID[i.Agent](t); r.Consumer.AgentID = &v },
		"operation": func(r *mc.EmbeddingProfileRequest) { r.Consumer.OperationID = testID[tc.Operation](t).String() },
		"purpose":   func(r *mc.EmbeddingProfileRequest) { r.Consumer.Purpose = mc.MemoryExtraction },
	} {
		t.Run(name, func(t *testing.T) {
			r := request.Clone()
			change(&r)
			_, err := a.DiscoverEmbeddingProfile(ctx, r)
			requireFault(t, err, f.Forbidden)
		})
	}
	entry.intent = "lookup-retain"
	_, err = a.DiscoverEmbeddingProfile(ctx, request)
	requireFault(t, err, f.Forbidden)
	entry.intent = "reflect"
	if _, err := a.DiscoverEmbeddingProfile(ctx, request); err != nil {
		t.Fatal("original reflection profile read", err)
	}
	delete(s.calls, entry)
	_, err = a.DiscoverEmbeddingProfile(ctx, request)
	requireFault(t, err, f.Forbidden)
}

func TestMemoryEmbeddingProfileRequiresStableCaptureIdentity(t *testing.T) {
	profile := mc.EmbeddingProfile{SelectionVersion: 3, ModelID: testID[mc.Model](t), ModelVersion: 2, ProviderID: testID[mc.Provider](t), ProviderVersion: 4, AdapterRevision: "openai-embeddings-float-usage-v2"}
	version := profile.SelectionVersion
	snapshot := mc.ConfigSnapshot{ID: testID[mc.Snapshot](t), SelectionVersion: &version, Identity: mc.ModelIdentity{ModelID: profile.ModelID, ProviderID: profile.ProviderID, AdapterRevision: profile.AdapterRevision}}
	if !profileMatchesSnapshot(profile, snapshot) {
		t.Fatal("matching captured configuration rejected")
	}
	// Capture IDs are unique per Resolve and never identify profile changes.
	snapshot.ID = testID[mc.Snapshot](t)
	if !profileMatchesSnapshot(profile, snapshot) {
		t.Fatal("random capture identity treated as profile change")
	}
	for name, change := range map[string]func(*mc.ConfigSnapshot){
		"selection":   func(s *mc.ConfigSnapshot) { *s.SelectionVersion++ },
		"unversioned": func(s *mc.ConfigSnapshot) { s.SelectionVersion = nil },
		"model":       func(s *mc.ConfigSnapshot) { s.Identity.ModelID = testID[mc.Model](t) },
		"provider":    func(s *mc.ConfigSnapshot) { s.Identity.ProviderID = testID[mc.Provider](t) },
		"adapter":     func(s *mc.ConfigSnapshot) { s.Identity.AdapterRevision = "future-v3" },
	} {
		t.Run(name, func(t *testing.T) {
			s := snapshot.Clone()
			change(&s)
			if profileMatchesSnapshot(profile, s) {
				t.Fatal("configuration drift accepted before dispatch")
			}
		})
	}
}
