package contract

import (
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"testing"
)

func embeddingProfileRequest(t *testing.T) EmbeddingProfileRequest {
	t.Helper()
	project, agent := fresh[id.Project](t), fresh[id.Agent](t)
	scope, err := id.InProject(project)
	must(t, err)
	reg, err := id.RegisterService(id.MemoryRuntime)
	must(t, err)
	operation := fresh[struct{}](t).String()
	actor, err := reg.Actor(operation, scope)
	must(t, err)
	return EmbeddingProfileRequest{Actor: actor, Consumer: Consumer{Kind: MemoryConsumer, ProjectID: project, AgentID: &agent, Purpose: MemoryEmbedding, OperationID: operation}}
}
func TestEmbeddingProfileBindsOriginalRuntimeOperation(t *testing.T) {
	r := embeddingProfileRequest(t)
	original, err := EmbeddingProfileBinding(r)
	must(t, err)
	for name, edit := range map[string]func(*EmbeddingProfileRequest){
		"wrong purpose":   func(r *EmbeddingProfileRequest) { r.Consumer.Purpose = MemoryExtraction },
		"wrong operation": func(r *EmbeddingProfileRequest) { r.Consumer.OperationID = fresh[struct{}](t).String() },
		"wrong project":   func(r *EmbeddingProfileRequest) { r.Consumer.ProjectID = fresh[id.Project](t) },
		"missing agent":   func(r *EmbeddingProfileRequest) { r.Consumer.AgentID = nil },
		"wrong runtime": func(r *EmbeddingProfileRequest) {
			scope, _ := id.InProject(r.Consumer.ProjectID)
			reg, _ := id.RegisterService(id.KnowledgeRuntime)
			r.Actor, _ = reg.Actor(r.Consumer.OperationID, scope)
		},
	} {
		t.Run(name, func(t *testing.T) { copy := r.Clone(); edit(&copy); reject(t, copy.Validate()) })
	}
	copy := r.Clone()
	*copy.Consumer.AgentID = fresh[id.Agent](t)
	changed, err := EmbeddingProfileBinding(copy)
	must(t, err)
	if changed == original || *copy.Consumer.AgentID == *r.Consumer.AgentID {
		t.Fatal("namespace binding or copy was lost")
	}
	scope, _ := id.InProject(r.Consumer.ProjectID)
	reg, _ := id.RegisterService(id.KnowledgeRuntime)
	r.Actor, _ = reg.Actor(r.Consumer.OperationID, scope)
	r.Consumer.Kind, r.Consumer.Purpose, r.Consumer.AgentID = KnowledgeConsumer, KnowledgeEmbedding, nil
	must(t, r.Validate())
}
func TestEmbeddingProfileDigestTracksEveryStableRevision(t *testing.T) {
	p := EmbeddingProfile{SelectionVersion: 1, ModelID: fresh[Model](t), ModelVersion: 2, ProviderID: fresh[Provider](t), ProviderVersion: 3, AdapterRevision: "openai-embeddings-float-usage-v2"}
	old, err := p.Digest()
	must(t, err)
	for _, edit := range []func(*EmbeddingProfile){
		func(p *EmbeddingProfile) { p.SelectionVersion++ }, func(p *EmbeddingProfile) { p.ModelID = fresh[Model](t) }, func(p *EmbeddingProfile) { p.ModelVersion++ }, func(p *EmbeddingProfile) { p.ProviderID = fresh[Provider](t) }, func(p *EmbeddingProfile) { p.ProviderVersion++ }, func(p *EmbeddingProfile) { p.AdapterRevision = "future-v3" },
	} {
		copy := p
		edit(&copy)
		next, err := copy.Digest()
		must(t, err)
		if next == old {
			t.Fatal("profile change did not change identity")
		}
	}
	p.ProviderVersion = 0
	reject(t, p.Validate())
}
