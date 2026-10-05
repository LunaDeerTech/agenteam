package contract

import (
	"math"
	"testing"

	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func TestEmbeddingShapeAndFiniteValues(t *testing.T) {
	x := setup(t)
	r := Embeddings{InvocationID: x.inv, Items: []Embedding{{Index: 1, Values: []float64{1, 2}}, {Index: 0, Values: []float64{3, 4}}}, Usage: Usage{Source: UnknownUsage}}
	must(t, r.ValidateFor(2, 2))
	reject(t, r.ValidateFor(3, 2))
	reject(t, r.ValidateFor(2, 3))
	for _, v := range []float64{math.Inf(1), math.Inf(-1), math.NaN()} {
		c := r.Clone()
		c.Items[0].Values[0] = v
		reject(t, c.Validate())
	}
	c := r.Clone()
	c.Items[1].Index = 1
	reject(t, c.Validate())
	c = r.Clone()
	c.Items[0].Values = c.Items[0].Values[:1]
	reject(t, c.Validate())
	c = r.Clone()
	c.Items[0].Values[0] = 42
	if r.Items[0].Values[0] != 1 {
		t.Fatal("vector aliases")
	}
}
func TestRerankerDoesNotInventProviderScoreBounds(t *testing.T) {
	x := setup(t)
	r := RankedItems{InvocationID: x.inv, Items: []RankedItem{{Index: 2, Score: 12.5}, {Index: 0, Score: -7}}, Usage: Usage{Source: UnknownUsage}}
	must(t, r.ValidateFor(3, 2))
	reject(t, r.ValidateFor(2, 2))
	reject(t, r.ValidateFor(3, 1))
	c := r.Clone()
	c.Items[0].Index = 0
	reject(t, c.Validate())
	c = r.Clone()
	c.Items[0].Score = math.NaN()
	reject(t, c.Validate())
	if r.Items[0].Score != 12.5 {
		t.Fatal("rank aliases")
	}
}
func TestGeneratedFilesCarryBusinessReferenceOnly(t *testing.T) {
	x := setup(t)
	ref, e := oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.ArtifactFile, ProjectID: x.c.ProjectID, ArtifactID: fresh[struct{}](t).String(), FileID: fresh[struct{}](t).String()})
	must(t, e)
	r := GeneratedFiles{InvocationID: x.inv, Items: []GeneratedFile{{Ref: ref, MediaType: "image/png", Size: 123}}, Usage: Usage{Source: UnknownUsage}}
	must(t, r.Validate())
	c := r.Clone()
	c.Items[0].Size = 0
	reject(t, c.Validate())
	c = r.Clone()
	c.Items[0].Size = TokenCount(oc.MaxObjectSize) + 1
	reject(t, c.Validate())
	c = r.Clone()
	c.Items[0].MediaType = "text/html; charset=utf-8"
	reject(t, c.Validate())
	c = r.Clone()
	c.Items = nil
	reject(t, c.Validate())
}
