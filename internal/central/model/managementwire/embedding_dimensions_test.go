package managementwire

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func fixedEmbedding(dimensions mc.EmbeddingDimensions) *mc.EmbeddingCapabilities {
	return &mc.EmbeddingCapabilities{Dimensions: dimensions, DimensionsMode: mc.EmbeddingFixedDimensions}
}

func TestManagementEmbeddingDimensionsWireAndResponse(t *testing.T) {
	var hits atomic.Int32
	var expectRequest atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		var body map[string]json.RawMessage
		if json.NewDecoder(r.Body).Decode(&body) != nil || r.URL.Path != "/v1%2Fowned/embeddings" && r.URL.EscapedPath() != "/v1%2Fowned/embeddings" {
			t.Error("unexpected management embedding request")
		}
		if expectRequest.Load() {
			if string(body["dimensions"]) != "3" || len(body) != 4 {
				t.Error("explicit dimensions missing or wrong type")
			}
		} else if _, present := body["dimensions"]; present || len(body) != 3 {
			t.Error("fixed dimensions unexpectedly sent")
		}
		_, _ = io.WriteString(w, validEmbedding)
	}))
	defer server.Close()
	transport, call, endpoint := wireFixture(t, server, true)
	input := Request{Action: "test", Protocol: embedProtocol, Endpoint: endpoint, Model: "owned", Embedding: fixedEmbedding(3)}
	for _, mode := range []string{mc.EmbeddingFixedDimensions, mc.EmbeddingRequestDimensions} {
		input.Embedding.DimensionsMode = mode
		expectRequest.Store(mode == mc.EmbeddingRequestDimensions)
		result, err := transport.Run(context.Background(), input, nil, call)
		if err != nil || result.Reason != "" {
			t.Fatal("valid configured probe rejected", err, result.Reason)
		}
	}
	input.Embedding = fixedEmbedding(2)
	expectRequest.Store(false)
	result, err := transport.Run(context.Background(), input, nil, call)
	if err != nil || result.Reason != "invalid_response" || hits.Load() != 3 {
		t.Fatal("wrong vector size was treated as success", err, result.Reason)
	}
	input.Embedding = nil
	if _, err := transport.Run(context.Background(), input, nil, call); err == nil || hits.Load() != 3 {
		t.Fatal("missing configuration produced an outbound probe")
	}
}
