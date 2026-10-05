package contract

import (
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func ptrVersion(v f.Version) *f.Version { return &v }
func TestResolveBindingIncludesFullActorAndSource(t *testing.T) {
	x := setup(t)
	r := x.resolve()
	a, e := id.NewHuman(fresh[id.User](t), fresh[id.Session](t))
	must(t, e)
	r.Actor = a
	b, e := ResolveBinding(r)
	must(t, e)
	u, e := f.ParseID[id.User](a.Details().UserID)
	must(t, e)
	r.Actor, e = id.NewHuman(u, fresh[id.Session](t))
	must(t, e)
	other, e := ResolveBinding(r)
	must(t, e)
	if b == other {
		t.Fatal("session omitted from plan binding")
	}
	r.Source = ""
	reject(t, r.Validate())
}
func TestServingResolutionKeepsOldConfigAndNewCallOwner(t *testing.T) {
	x := setup(t)
	r := x.resolve()
	r.Consumer = Consumer{Kind: KnowledgeConsumer, ProjectID: x.c.ProjectID, Purpose: KnowledgeEmbedding, OperationID: fresh[struct{}](t).String()}
	r.Purpose = KnowledgeEmbedding
	r.Actor, _ = id.NewHuman(fresh[id.User](t), fresh[id.Session](t))
	r.Source = ServingSnapshotSource
	r.ModelRef = nil
	r.Selection = nil
	r.ServingSnapshotID = &x.snapshot.ID
	r.ServingGenerationID = fresh[struct{}](t).String()
	r.LeaseOwner, _ = sc.NewCredentialLeaseOwner(sc.ModelCallOwner, x.call.String())
	must(t, r.Validate())
	b, e := ResolveBinding(r)
	must(t, e)
	c := r.Clone()
	c.ServingGenerationID = fresh[struct{}](t).String()
	b2, e := ResolveBinding(c)
	must(t, e)
	if b == b2 {
		t.Fatal("generation omitted")
	}
	c = r.Clone()
	c.Selection = &SelectionRef{Kind: "platform", Selector: EmbeddingSelector}
	reject(t, c.Validate())
	c = r.Clone()
	c.LeaseOwner = x.owner
	reject(t, c.Validate())
	c = r.Clone()
	c.ReasoningEffort = "high"
	reject(t, c.Validate())
}
