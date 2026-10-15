package knowledge

import (
	"context"
	"encoding/json"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func TestEmbeddingProfileRequiresOriginalBuildWithoutDocumentAuthority(t *testing.T) {
	s, owner, p := retentionFixture(t)
	for _, human := range []bool{true, false} {
		run, done, err := s.admit(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		caller := owner
		if !human {
			caller = id.Actor{}
		}
		ctx, b, err := s.newBuild(run, caller, p.Project)
		if err != nil {
			t.Fatal(err)
		}
		r := b.request()
		plan, err := s.authority.DiscoverEmbeddingProfile(ctx, r)
		if err != nil || plan.Validate() != nil || plan.Details().RetryPolicy != nil {
			t.Fatal("original profile owner rejected", err)
		}
		if _, err := s.authority.DiscoverEmbeddingProfile(context.Background(), r); err == nil {
			t.Fatal("DTO alone granted profile access")
		}
		for _, mutate := range []func(*mc.EmbeddingProfileRequest){
			func(r *mc.EmbeddingProfileRequest) { r.Consumer.ProjectID = newID[id.Project](t) },
			func(r *mc.EmbeddingProfileRequest) { r.Consumer.OperationID = newID[struct{}](t).String() },
			func(r *mc.EmbeddingProfileRequest) { r.Actor = owner },
		} {
			forged := r.Clone()
			mutate(&forged)
			if _, err := s.authority.DiscoverEmbeddingProfile(ctx, forged); err == nil {
				t.Fatal("profile request replaced original operation")
			}
		}
		if !human && readInput(ctx, b.actor, p.Project) == nil {
			t.Fatal("background profile authority granted arbitrary document read")
		}
		done()
		if _, err := s.authority.DiscoverEmbeddingProfile(ctx, r); err == nil {
			t.Fatal("returned owner retained profile access")
		}
	}
}

func TestIndexBuildLedgerDoesNotAcceptMixedAuthority(t *testing.T) {
	v := projectCallIdentity{Format: 1, Method: "index-build", IndexBuild: newID[struct{}](t).String()}
	raw, _ := json.Marshal(v)
	if _, err := canonicalProjectIdentity(raw); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*projectCallIdentity){
		func(v *projectCallIdentity) { v.IndexJob = v.IndexBuild },
		func(v *projectCallIdentity) { v.User = newID[id.User](t).String() },
		func(v *projectCallIdentity) { v.Operation = v.IndexBuild },
		func(v *projectCallIdentity) { v.Document = v.IndexBuild },
		func(v *projectCallIdentity) { v.Method = "read" },
		func(v *projectCallIdentity) { v.IndexBuild = "" },
	} {
		forged := v
		mutate(&forged)
		raw, _ = json.Marshal(forged)
		if _, err := canonicalProjectIdentity(raw); err == nil {
			t.Fatal("mixed background build identity accepted")
		}
	}
}

func TestEmbeddingProfileSnapshotRequiresExactSelectionAndIdentity(t *testing.T) {
	p := mc.EmbeddingProfile{SelectionVersion: 1, ModelID: newID[mc.Model](t), ModelVersion: 2, ProviderID: newID[mc.Provider](t), ProviderVersion: 3, AdapterRevision: "openai.embeddings.float.v2"}
	v := p.SelectionVersion
	snapshot := mc.ConfigSnapshot{SelectionVersion: &v, Identity: mc.ModelIdentity{ModelID: p.ModelID, ProviderID: p.ProviderID, AdapterRevision: p.AdapterRevision}}
	if !embeddingProfileMatchesSnapshot(p, snapshot) {
		t.Fatal("matching snapshot rejected")
	}
	for _, mutate := range []func(*mc.ConfigSnapshot){
		func(s *mc.ConfigSnapshot) { s.SelectionVersion = nil },
		func(s *mc.ConfigSnapshot) { n := f.Version(2); s.SelectionVersion = &n },
		func(s *mc.ConfigSnapshot) { s.Identity.ModelID = newID[mc.Model](t) },
		func(s *mc.ConfigSnapshot) { s.Identity.ProviderID = newID[mc.Provider](t) },
		func(s *mc.ConfigSnapshot) { s.Identity.AdapterRevision = "other" },
	} {
		other := snapshot
		mutate(&other)
		if embeddingProfileMatchesSnapshot(p, other) {
			t.Fatal("snapshot substituted embedding space")
		}
	}
	// Model/provider revisions are deliberately observed by the Reader before
	// and after Resolve; snapshot identity alone cannot prove those revisions.
	before, _ := p.Digest()
	for _, mutate := range []func(*mc.EmbeddingProfile){func(p *mc.EmbeddingProfile) { p.ModelVersion++ }, func(p *mc.EmbeddingProfile) { p.ProviderVersion++ }} {
		changed := p
		mutate(&changed)
		after, _ := changed.Digest()
		if before == after || p == changed {
			t.Fatal("configuration version lost from build identity")
		}
	}
}
