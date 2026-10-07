//go:build integration

package model_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	wire "github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
)

// The accepted fixture owns real Account/Audit/Policy services and controlled
// TLS resources. Its synthetic Snapshot/material is not a Nonchat binding.
func embeddingWireFixture(t *testing.T) (*wireFixture, *wire.OpenAIEmbeddings, context.Context) {
	t.Helper()
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(func() {
		cancel()
		if time.Since(started) > 2*time.Minute {
			t.Error("embedding scenario exceeded its two-minute budget")
		}
	})
	v := newWireFixture(t)
	return v, embeddingWireAdapter(t, v, v.budget), ctx
}

func embeddingWireAdapter(t *testing.T, v *wireFixture, b *wire.Budget) *wire.OpenAIEmbeddings {
	t.Helper()
	a, err := wire.NewOpenAIEmbeddings(v.transport, b)
	if err != nil {
		t.Fatal("embedding constructor", err)
	}
	known := false
	for _, existing := range v.budgets {
		known = known || existing == b
	}
	if !known {
		v.budgets = append(v.budgets, b)
	}
	return a
}

func embeddingWireScenario(t *testing.T, v *wireFixture, cfg netfixture.WireScenario) string {
	t.Helper()
	key, err := v.net.Create(testContext(t), netfixture.ScenarioConfig{Mode: "openai_embeddings_wire", Wire: &cfg})
	if err != nil {
		t.Fatal("embedding scenario", err)
	}
	v.cases = append(v.cases, key)
	return key
}

func embeddingWireInput(t *testing.T, v *wireFixture, key string) (wire.EmbeddingRequest, wire.CallOptions) {
	t.Helper()
	chat, options := v.input(t, key, wire.JSONResponse)
	snapshot := chat.Snapshot
	snapshot.Identity.Protocol = mc.OpenAIEmbeddings
	snapshot.Identity.Profile = mc.OpenAIEmbeddingsV1
	snapshot.Identity.ModelType = mc.EmbeddingModel
	snapshot.Identity.AdapterRevision = wire.OpenAIEmbeddingsFloatRevision
	snapshot.Capabilities = mc.Capabilities{InputModalities: []string{"text"}, OutputModalities: []string{"vector"}}
	return wire.EmbeddingRequest{Snapshot: snapshot, Texts: []string{"  exact 世界\n"}, ExpectedDimensions: 2}, options
}

func embeddingWireStart(t *testing.T, ctx context.Context, a *wire.OpenAIEmbeddings, r wire.EmbeddingRequest, o wire.CallOptions) *wire.EmbeddingExchange {
	t.Helper()
	x, err := a.Start(ctx, r, o)
	if err != nil || x == nil {
		t.Fatal("embedding admission", err)
	}
	return x
}

func embeddingWireClose(t *testing.T, x *wire.EmbeddingExchange) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := x.Close(ctx); err != nil || !x.Joined() {
		t.Fatal("embedding actual close/join", err)
	}
}

func embeddingWireZero(t *testing.T, result wire.EmbeddingResult) {
	t.Helper()
	if !reflect.DeepEqual(result, wire.EmbeddingResult{}) {
		t.Fatal("failed embedding result published a candidate")
	}
}

type embeddingWireOutcome struct {
	result wire.EmbeddingResult
	err    error
}

// Every asynchronous Result has a cancellation path and an actual return
// checkpoint, including failures before the main test receives its result.
func embeddingWireConsume(t *testing.T, parent context.Context, x *wire.EmbeddingExchange) <-chan embeddingWireOutcome {
	t.Helper()
	ctx, cancel := context.WithCancel(parent)
	out := make(chan embeddingWireOutcome, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		result, err := x.Result(ctx)
		out <- embeddingWireOutcome{result, err}
	}()
	t.Cleanup(func() {
		cancel()
		wait, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := x.Close(wait); err != nil || !x.Joined() {
			t.Error("asynchronous embedding cleanup did not join", err)
		}
		select {
		case <-done:
		case <-wait.Done():
			t.Error("asynchronous Result did not actually return")
		}
	})
	return out
}

func embeddingWireReply(t *testing.T, indices []int, values [][]float64, usage json.RawMessage) []byte {
	t.Helper()
	type row struct {
		Object string    `json:"object"`
		Index  int       `json:"index"`
		Values []float64 `json:"embedding"`
	}
	items := make([]row, len(indices))
	for i, index := range indices {
		items[i] = row{"embedding", index, values[i]}
	}
	response := map[string]any{"object": "list", "model": "provider-response-alias", "data": items}
	if usage != nil {
		response["usage"] = usage
	}
	body, err := json.Marshal(response)
	if err != nil {
		t.Fatal("synthetic embedding response shape")
	}
	return body
}
