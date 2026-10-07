//go:build integration

package model_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	wire "github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
)

func TestModelOpenAIEmbeddingsWireHTTP(t *testing.T) {
	v, a, ctx := embeddingWireFixture(t)
	t.Run("real_policy_and_exact_frozen_float_batch", func(t *testing.T) {
		cfg := wireJSON(embeddingWireReply(t, []int{1, 0}, [][]float64{{3.5, -4}, {1, 2}}, json.RawMessage(`{"prompt_tokens":9007199254740993,"total_tokens":0}`)))
		cfg.Suffix = "/base%2Fkeep//./inner/embeddings"
		key := embeddingWireScenario(t, v, cfg)
		r, o := embeddingWireInput(t, v, key)
		r.Texts = append(r.Texts, "second\tinput")
		r.Snapshot.Endpoint += "/base%2Fkeep//./inner/"
		denied := embeddingWireStart(t, ctx, a, r, o)
		result, err := denied.Result(ctx)
		embeddingWireZero(t, result)
		wireModelError(t, err, "permission", "wire_transport_error", false, false)
		embeddingWireClose(t, denied)
		if d := denied.Observe(); !d.DoStarted || d.Decision == nil || d.Decision.Reason != ac.PrivateNotAllowed || d.Decision.Sent || len(v.state(t, key).Requests) != 0 {
			t.Fatal("real policy denial did not prove zero send")
		}
		v.allow(t, true)
		x := embeddingWireStart(t, ctx, a, r, o)
		r.Texts[0] = "mutated after admission"
		r.ExpectedDimensions = 1
		r.Snapshot.Identity.ProviderModelID = "mutated model"
		result, err = x.Result(ctx)
		want := []mc.Embedding{{Index: 0, Values: []float64{1, 2}}, {Index: 1, Values: []float64{3.5, -4}}}
		if err != nil || !x.Joined() || !reflect.DeepEqual(result.Items, want) || result.ProviderRequestID != "req_wire:1" {
			t.Fatal("complete ordered embedding batch", err)
		}
		u := result.Usage
		if u.Source != mc.ProviderUsage || u.InputTokens == nil || *u.InputTokens != 9007199254740993 || u.TotalTokens == nil || *u.TotalTokens != 0 || u.OutputTokens != nil || u.CachedInputTokens != nil || u.CacheWriteTokens != nil || u.ReasoningTokens != nil {
			t.Fatal("exact embedding usage projection")
		}
		*u.InputTokens = 1
		if *x.Observe().Usage.InputTokens != 9007199254740993 {
			t.Fatal("embedding observation aliases result")
		}
		state := v.state(t, key)
		if len(state.Requests) != 1 {
			t.Fatal("embedding request count")
		}
		got := state.Requests[0]
		if got.Method != "POST" || got.Host != "fixture.test:8443" || got.RequestURI != "/case/"+key+cfg.Suffix || got.Query != "" || got.Headers.Get("Authorization") != "Bearer "+wireCredentialCanary || got.Headers.Get("Content-Type") != "application/json" || got.Headers.Get("Accept") != "application/json" {
			t.Fatal("embedding origin/path/header binding")
		}
		var body map[string]json.RawMessage
		var texts []string
		if json.Unmarshal([]byte(got.Body), &body) != nil || len(body) != 3 || string(body["model"]) != `"fixture-native-model"` || string(body["encoding_format"]) != `"float"` || json.Unmarshal(body["input"], &texts) != nil || !reflect.DeepEqual(texts, []string{"  exact 世界\n", "second\tinput"}) {
			t.Fatal("embedding body keys/order/original text")
		}
		if d := x.Observe().Decision; d == nil || !d.Sent || d.Reason != "" {
			t.Fatal("successful embedding lacks actual Decision")
		}
		if again, err := x.Result(ctx); err == nil {
			t.Fatal("embedding Result consumed twice")
		} else {
			embeddingWireZero(t, again)
			wireFault(t, err, f.InvalidState)
		}
		embeddingWireClose(t, x)
		v.settled(t, key)
	})
	t.Run("single_item_stays_array_and_missing_usage_stays_unknown", func(t *testing.T) {
		key := embeddingWireScenario(t, v, wireJSON(embeddingWireReply(t, []int{0}, [][]float64{{0, 0}}, nil)))
		r, o := embeddingWireInput(t, v, key)
		x := embeddingWireStart(t, ctx, a, r, o)
		result, err := x.Result(ctx)
		if err != nil || len(result.Items) != 1 || !reflect.DeepEqual(result.Usage, mc.Usage{Source: mc.UnknownUsage}) {
			t.Fatal("single embedding/unknown usage", err)
		}
		state := v.state(t, key)
		var body struct{ Input []string }
		if len(state.Requests) != 1 || json.Unmarshal([]byte(state.Requests[0].Body), &body) != nil || len(body.Input) != 1 || body.Input[0] != r.Texts[0] {
			t.Fatal("single input was not an unchanged string array")
		}
		embeddingWireClose(t, x)
	})
	t.Run("unsupported_invalid_cancelled_are_zero_handles_and_requests", func(t *testing.T) {
		key := embeddingWireScenario(t, v, wireJSON(nil))
		for _, kind := range []string{"dimensions", "overwrite", "header", "revision", "bad_dimension", "duplicate", "cancelled"} {
			r, o := embeddingWireInput(t, v, key)
			call := ctx
			switch kind {
			case "dimensions":
				r.Snapshot.Parameters = json.RawMessage(`{"dimensions":2}`)
			case "overwrite":
				r.Snapshot.RequestOverwrite = json.RawMessage(`{"encoding_format":"base64"}`)
			case "header":
				r.Snapshot.HeaderOverwrite = map[string]string{"X-Custom": "unsupported"}
			case "revision":
				r.Snapshot.Identity.AdapterRevision = "unverified-revision"
			case "bad_dimension":
				r.ExpectedDimensions = 0
			case "duplicate":
				r.Snapshot.Parameters = json.RawMessage(`{"x":1,"x":2}`)
			case "cancelled":
				c, cancel := context.WithCancel(ctx)
				cancel()
				call = c
			}
			x, err := a.Start(call, r, o)
			if x != nil {
				t.Fatal("rejected embedding acquired handle")
			}
			switch kind {
			case "bad_dimension", "duplicate":
				wireFault(t, err, f.InvalidArgument)
			case "cancelled":
				wireModelError(t, err, "cancelled", "wire_cancelled", false, false)
			default:
				wireModelError(t, err, "unsupported_feature", "wire_unsupported_feature", false, false)
			}
		}
		empty, cancel := context.WithCancel(ctx)
		cancel()
		if len(v.state(t, key).Requests) != 0 || v.budget.Drain(empty) != nil {
			t.Fatal("preflight rejection sent or occupied a slot")
		}
		var nilResolver wireNilResolver
		transport := v.transport
		transport.Resolver = nilResolver
		if _, err := wire.NewOpenAIEmbeddings(transport, wire.NewBudget()); err == nil {
			t.Fatal("typed nil resolver accepted")
		}
	})
	t.Run("safe_status_mapping_without_retry", func(t *testing.T) {
		for _, tc := range []struct {
			status   int
			category mc.ErrorCategory
			retry    bool
		}{{400, "provider_error", false}, {401, "authentication", false}, {403, "permission", false}, {404, "provider_error", false}, {429, "rate_limited", true}, {500, "provider_error", false}, {502, "provider_unavailable", true}, {503, "provider_unavailable", true}, {504, "provider_unavailable", true}} {
			t.Run(strconv.Itoa(tc.status), func(t *testing.T) {
				cfg := wireJSON([]byte(strings.Repeat(wireBodyCanary, 3000)))
				cfg.Status = tc.status
				cfg.Headers["X-Request-Id"] = "unsafe request id"
				key := embeddingWireScenario(t, v, cfg)
				r, o := embeddingWireInput(t, v, key)
				x := embeddingWireStart(t, ctx, a, r, o)
				result, err := x.Result(ctx)
				embeddingWireZero(t, result)
				m := wireModelError(t, err, tc.category, "wire_http_error", true, false)
				if m.Retryable != tc.retry || m.ProviderRequestID != "" || len(v.state(t, key).Requests) != 1 {
					t.Fatal("embedding status retried or leaked request id")
				}
				embeddingWireClose(t, x)
			})
		}
	})
	t.Run("tls_and_post_redirect_preserve_actual_send_boundary", func(t *testing.T) {
		key := embeddingWireScenario(t, v, wireJSON(nil))
		r, o := embeddingWireInput(t, v, key)
		r.Snapshot.Endpoint = strings.Replace(r.Snapshot.Endpoint, "fixture.test", "wrong.fixture.test", 1)
		x := embeddingWireStart(t, ctx, a, r, o)
		result, err := x.Result(ctx)
		embeddingWireZero(t, result)
		wireModelError(t, err, "network", "wire_transport_error", false, false)
		if d := x.Observe().Decision; d == nil || d.Reason != ac.TLSFailed || d.Sent || len(v.state(t, key).Requests) != 0 {
			t.Fatal("TLS failure did not preserve zero send")
		}
		embeddingWireClose(t, x)
		cfg := wireJSON(nil)
		cfg.Status = 307
		cfg.Headers["Location"] = "https://fixture.test:8443/case/" + key + "/embeddings"
		redirect := embeddingWireScenario(t, v, cfg)
		r, o = embeddingWireInput(t, v, redirect)
		x = embeddingWireStart(t, ctx, a, r, o)
		result, err = x.Result(ctx)
		embeddingWireZero(t, result)
		wireModelError(t, err, "permission", "wire_transport_error", true, false)
		if d := x.Observe().Decision; d == nil || d.Reason != ac.RedirectDenied || len(v.state(t, redirect).Requests) != 1 || len(v.state(t, key).Requests) != 0 {
			t.Fatal("POST redirect sent a successor request")
		}
		embeddingWireClose(t, x)
	})
	t.Run("fixture_modes_suffixes_and_existing_limits_stay_closed", func(t *testing.T) {
		for _, cfg := range []netfixture.ScenarioConfig{
			{Mode: "openai_embeddings_wire"},
			{Mode: "stream", Wire: &netfixture.WireScenario{Status: 200}},
			{Mode: "openai_embeddings_wire", Body: "legacy", Wire: &netfixture.WireScenario{Status: 200}},
			{Mode: "openai_embeddings_wire", Wire: &netfixture.WireScenario{Status: 201}},
			{Mode: "openai_embeddings_wire", Wire: &netfixture.WireScenario{Status: 200, Suffix: "/chat/completions"}},
			{Mode: "openai_chat_wire", Wire: &netfixture.WireScenario{Status: 200, Suffix: "/embeddings"}},
			{Mode: "openai_embeddings_wire", Wire: &netfixture.WireScenario{Status: 200, Suffix: "/bad?query/embeddings"}},
			{Mode: "openai_embeddings_wire", Wire: &netfixture.WireScenario{Status: 200, Headers: map[string]string{"Set-Cookie": "forbidden"}}},
			{Mode: "openai_embeddings_wire", Wire: &netfixture.WireScenario{Status: 200, Chunks: make([][]byte, 513)}},
			{Mode: "openai_embeddings_wire", Wire: &netfixture.WireScenario{Status: 200, Chunks: [][]byte{make([]byte, 512<<10+1)}}},
		} {
			if _, err := v.net.Create(ctx, cfg); err == nil {
				t.Fatal("outside closed embedding scenario accepted")
			}
		}
	})
}
