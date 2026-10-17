package adapter

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"

	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func TestEmbeddingDimensionsFixedRequestAndFrozenWire(t *testing.T) {
	for _, mode := range []string{mc.EmbeddingFixedDimensions, mc.EmbeddingRequestDimensions} {
		t.Run(mode, func(t *testing.T) {
			r, options := embeddingUnitInput(t)
			r.Snapshot.Identity.AdapterRevision = OpenAIEmbeddingsDimensionsRevision
			r.Snapshot.Capabilities.Embedding.DimensionsMode = mode
			prepared, err := prepareEmbeddings(context.Background(), r, options)
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.request.Body.Close()
			r.Snapshot.Capabilities.Embedding.Dimensions = 3
			r.Snapshot.Capabilities.Embedding.DimensionsMode = "invalid"
			raw, err := io.ReadAll(prepared.request.Body)
			if err != nil {
				t.Fatal(err)
			}
			var body map[string]json.RawMessage
			if json.Unmarshal(raw, &body) != nil || prepared.dimensions != 2 {
				t.Fatal("prepared vector space changed")
			}
			if mode == mc.EmbeddingFixedDimensions {
				if _, present := body["dimensions"]; present || len(body) != 3 {
					t.Fatal("fixed mode sent an adjustment")
				}
			} else if string(body["dimensions"]) != "2" || len(body) != 4 {
				t.Fatal("request mode lost exact integer dimensions")
			}
		})
	}
	r, options := embeddingUnitInput(t)
	r.Snapshot.Identity.AdapterRevision = OpenAIEmbeddingsDimensionsRevision
	r.ExpectedDimensions = 3
	if _, err := prepareEmbeddings(context.Background(), r, options); err == nil {
		t.Fatal("caller overrode immutable snapshot dimension")
	}
	r.ExpectedDimensions = 2
	r.Snapshot.Identity.AdapterRevision = OpenAIEmbeddingsFloatUsageRevision
	r.Snapshot.Capabilities.Embedding.DimensionsMode = mc.EmbeddingRequestDimensions
	if _, err := prepareEmbeddings(context.Background(), r, options); err == nil {
		t.Fatal("old profile silently acquired new wire parameters")
	}
}

func TestEmbeddingDimensionsProfileAndResponse(t *testing.T) {
	raw, err := os.ReadFile("testdata/openai-embeddings-dimensions-v3.json")
	if err != nil {
		t.Fatal(err)
	}
	var profile struct{ Revision string }
	if json.Unmarshal(raw, &profile) != nil || profile.Revision != OpenAIEmbeddingsDimensionsRevision {
		t.Fatal("explicit-dimensions profile missing")
	}
	body := []byte(`{"object":"list","model":"owned","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}]}`)
	if _, _, err := parseEmbeddingJSONProfile(context.Background(), body, 1, 2, true); err != nil {
		t.Fatal("exact vector rejected", err)
	}
	if _, _, err := parseEmbeddingJSONProfile(context.Background(), body, 1, 3, true); err == nil {
		t.Fatal("provider returned a different vector space")
	}
}
