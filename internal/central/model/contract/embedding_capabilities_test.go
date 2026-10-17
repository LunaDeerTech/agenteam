package contract

import (
	"encoding/json"
	"testing"
)

func TestEmbeddingDimensionsExactJSONAndTypeBinding(t *testing.T) {
	for _, raw := range []string{`"1"`, `"1536"`, `"4096"`} {
		var d EmbeddingDimensions
		must(t, json.Unmarshal([]byte(raw), &d))
		out, err := json.Marshal(d)
		must(t, err)
		if string(out) != raw {
			t.Fatal("dimension was normalized or truncated")
		}
	}
	for _, raw := range []string{`null`, `1536`, `"0"`, `"4097"`, `"01536"`, `"+2"`, `" 2"`, `"2.0"`, `"1e3"`, `"-1"`} {
		var d EmbeddingDimensions
		if json.Unmarshal([]byte(raw), &d) == nil {
			t.Fatal("invalid dimension accepted", raw)
		}
	}
	c := Capabilities{Embedding: &EmbeddingCapabilities{Dimensions: 1536, DimensionsMode: EmbeddingFixedDimensions}}
	must(t, c.ValidateForModelType(EmbeddingModel))
	for _, typ := range []ModelType{ChatModel, RerankerModel, ImageModel} {
		reject(t, c.ValidateForModelType(typ))
	}
	copy := c.Clone()
	copy.Embedding.Dimensions = 2048
	copy.Embedding.DimensionsMode = EmbeddingRequestDimensions
	if c.Embedding.Dimensions != 1536 || c.Embedding.DimensionsMode != EmbeddingFixedDimensions {
		t.Fatal("snapshot capability clone aliases live dimensions")
	}
	copy.Embedding.DimensionsMode = "auto"
	reject(t, copy.Validate())
	c.Embedding = nil
	reject(t, c.ValidateForModelType(EmbeddingModel))
	must(t, c.ValidateForModelType(ChatModel))
}

func TestEmbeddingSnapshotRequiresExplicitVectorSpace(t *testing.T) {
	s := setup(t).snapshot
	s.Identity.ModelType = EmbeddingModel
	s.Identity.Protocol, s.Identity.Profile = OpenAIEmbeddings, OpenAIEmbeddingsV1
	s.Capabilities = Capabilities{InputModalities: []string{"text"}, OutputModalities: []string{"vector"}}
	reject(t, s.Validate())
	s.Capabilities.Embedding = &EmbeddingCapabilities{Dimensions: 2, DimensionsMode: EmbeddingFixedDimensions}
	must(t, s.Validate())
	s.Identity.ModelType, s.Identity.Protocol, s.Identity.Profile = ChatModel, OpenAIChat, OpenAIChatV1
	reject(t, s.Validate())
}
