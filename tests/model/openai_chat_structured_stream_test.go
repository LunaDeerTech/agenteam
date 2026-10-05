//go:build integration

package model_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	wire "github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func structuredEvents(t *testing.T, events []wire.Event) (string, int, int) {
	t.Helper()
	var text strings.Builder
	usages, ends := 0, 0
	for _, e := range events {
		switch e.Kind {
		case wire.TextDelta:
			text.WriteString(e.Text)
		case wire.UsageUpdate:
			usages++
		case wire.StreamEnd:
			ends++
		default:
			t.Fatal("unknown event")
		}
		wireNoLeak(t, e)
	}
	return text.String(), usages, ends
}

func TestModelOpenAIChatStructuredStream(t *testing.T) {
	v := newWireFixture(t)
	v.allow(t, true)
	t.Run("fragmented_strict_json_and_done_before_http_eof", func(t *testing.T) {
		parts := []string{` {"facts":[{"text":"世`, `界\nquote:\"","kind":"fact","ordinal":9007199`, `254740993,"active":true}],"note":null} `}
		var raw []byte
		raw = append(raw, []byte(": ping\n\n")...)
		for _, part := range parts {
			raw = append(raw, wireChunk(map[string]any{"content": part}, nil, nil, wireBodyCanary, false)...)
		}
		raw = append(raw, wireChunk(map[string]any{}, "stop", nil, nil, false)...)
		raw = append(raw, wireChunk(nil, nil, json.RawMessage(`{"prompt_tokens":7,"completion_tokens":0,"total_tokens":9}`), nil, true)...)
		raw = append(raw, []byte("data: [DONE]\n\n")...)
		raw = bytes.ReplaceAll(raw, []byte("\n"), []byte("\r\n"))
		var chunks [][]byte
		for len(raw) > 0 {
			n := min(11, len(raw))
			chunks = append(chunks, bytes.Clone(raw[:n]))
			raw = raw[n:]
		}
		cfg := wireSSE(chunks...)
		hold := len(chunks)
		cfg.HoldAfter = &hold
		key := v.scenario(t, cfg)
		r, o := structuredWireInput(t, v, key, wire.SSEResponse)
		x := v.start(t, testContext(t), v.adapter, r, o)
		events, err := wireDrain(t, x)
		text, usages, ends := structuredEvents(t, events)
		if err != nil || text != structuredMemoryResult || usages != 1 || ends != 1 || !x.Joined() || x.Observe().Usage.TotalTokens == nil || *x.Observe().Usage.TotalTokens != 9 {
			t.Fatal("strict fragmented completion", err)
		}
		last := events[len(events)-1]
		if last.End == nil || last.End.FinishReason != "stop" || last.End.ProviderRequestID != "req_stream-1" {
			t.Fatal("strict terminal identity")
		}
		var native map[string]json.RawMessage
		state := v.state(t, key)
		if len(state.Requests) != 1 || json.Unmarshal([]byte(state.Requests[0].Body), &native) != nil || string(native["stream"]) != "true" || string(native["stream_options"]) != `{"include_usage":true}` {
			t.Fatal("stream request mapping")
		}
		wireClose(t, x)
		v.settled(t, key) // No Release: DONE must itself close the held body.
	})
	t.Run("schema_failure_preserves_delivered_prefix_and_final_usage", func(t *testing.T) {
		for _, tc := range []struct {
			name, body, finish, tail, category string
			refusal                            bool
		}{
			{"mismatch", `{"v":"wrong"}`, "stop", "data: [DONE]\n\n", "provider_error", false},
			{"trailing", `{"v":1}{}`, "stop", "data: [DONE]\n\n", "provider_error", false},
			{"malformed", `{"v":`, "stop", "data: [DONE]\n\n", "provider_error", false},
			{"length", `{"v":1}`, "length", "data: [DONE]\n\n", "provider_error", false},
			{"filtered", `{"v":1}`, "content_filter", "data: [DONE]\n\n", "content_filter", false},
			{"missing_done", `{"v":1}`, "stop", "", "provider_error", false},
			{"refusal_after_prefix", `{"v":`, "stop", "", "content_filter", true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				chunks := [][]byte{wireChunk(map[string]any{"content": tc.body}, nil, nil, nil, false)}
				if tc.refusal {
					chunks = append(chunks, wireChunk(map[string]any{"refusal": wireBodyCanary}, nil, nil, nil, false))
				} else {
					chunks = append(chunks, wireChunk(map[string]any{}, tc.finish, nil, nil, false), wireChunk(nil, nil, json.RawMessage(`{"completion_tokens":0,"total_tokens":11}`), nil, true))
					if tc.tail != "" {
						chunks = append(chunks, []byte(tc.tail))
					}
				}
				key := v.scenario(t, wireSSE(chunks...))
				r, o := structuredWireInput(t, v, key, wire.SSEResponse)
				r.ResponseFormat.Schema = json.RawMessage(structuredScalarSchema)
				x := v.start(t, testContext(t), v.adapter, r, o)
				events, err := wireDrain(t, x)
				text, usages, ends := structuredEvents(t, events)
				code := "wire_protocol_invalid"
				if tc.category == "content_filter" {
					code = ""
				}
				m := wireModelError(t, err, mc.ErrorCategory(tc.category), code, true, true)
				if text != tc.body || ends != 0 || m.Retryable {
					t.Fatal("schema error emitted success or lost prefix")
				}
				if !tc.refusal && (usages != 1 || x.Observe().Usage.TotalTokens == nil || *x.Observe().Usage.TotalTokens != 11 || x.Observe().Usage.OutputTokens == nil || *x.Observe().Usage.OutputTokens != 0) {
					t.Fatal("final usage was lost")
				}
				wireClose(t, x)
				if len(v.state(t, key).Requests) != 1 {
					t.Fatal("stream failure retried")
				}
			})
		}
	})
	t.Run("no_usage_is_unknown_and_output_node_budget_is_real", func(t *testing.T) {
		for _, over := range []bool{false, true} {
			body := `{"v":1}`
			schema := structuredScalarSchema
			if over {
				body = `{"v":[` + strings.Repeat("0,", 65534) + `0]}`
				schema = `{"type":"object","properties":{"v":{"type":"array","items":{"type":"integer"}}},"required":["v"],"additionalProperties":false}`
			}
			var chunks [][]byte
			for remaining := body; remaining != ""; {
				n := min(8192, len(remaining))
				chunks = append(chunks, wireChunk(map[string]any{"content": remaining[:n]}, nil, nil, nil, false))
				remaining = remaining[n:]
			}
			chunks = append(chunks, wireChunk(map[string]any{}, "stop", nil, nil, false), []byte("data: [DONE]\n\n"))
			key := v.scenario(t, wireSSE(chunks...))
			r, o := structuredWireInput(t, v, key, wire.SSEResponse)
			r.ResponseFormat.Schema = json.RawMessage(schema)
			x := v.start(t, testContext(t), v.adapter, r, o)
			events, err := wireDrain(t, x)
			text, usages, ends := structuredEvents(t, events)
			if text != body || usages != 0 || x.Observe().Usage.Source != mc.UnknownUsage {
				t.Fatal("unknown usage/prefix changed")
			}
			if over {
				wireModelError(t, err, "provider_error", "wire_limit_exceeded", true, true)
				if ends != 0 {
					t.Fatal("node overflow completed")
				}
			} else if err != nil || ends != 1 {
				t.Fatal("unknown usage success", err)
			}
			wireClose(t, x)
		}
	})
}

func TestModelOpenAIChatStructuredCloseAndBudget(t *testing.T) {
	v := newWireFixture(t)
	v.allow(t, true)
	t.Run("backpressured_parser_owns_slot_and_borrowed_material", func(t *testing.T) {
		body := `{"v":"` + strings.Repeat("x", 384<<10) + `"}`
		var chunks [][]byte
		for body != "" {
			n := min(8192, len(body))
			chunks = append(chunks, wireChunk(map[string]any{"content": body[:n]}, nil, nil, nil, false))
			body = body[n:]
		}
		chunks = append(chunks, wireChunk(map[string]any{}, "stop", nil, nil, false), []byte("data: [DONE]\n\n"))
		key := v.scenario(t, wireSSE(chunks...))
		r, o := structuredWireInput(t, v, key, wire.SSEResponse)
		r.ResponseFormat.Schema = json.RawMessage(`{"type":"object","properties":{"v":{"type":"string"}},"required":["v"],"additionalProperties":false}`)
		b := wire.NewBudget()
		a := v.newAdapter(t, b)
		x := v.start(t, testContext(t), a, r, o)
		wireWait(t, "server did not send bounded payload", func() bool { return v.state(t, key).CompletedHandlers == 1 })
		if x.Joined() {
			t.Fatal("unconsumed structured parser escaped backpressure")
		}
		short, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		err := b.Drain(short)
		cancel()
		wireModelError(t, err, "timeout", "wire_transport_error", false, false)
		if x.Joined() {
			t.Fatal("timed out waiter joined parser")
		}
		if err := o.Credential.Use(func([]byte) error { return nil }); err != nil {
			t.Fatal("borrowed material retired before parser", err)
		}
		wireClose(t, x)
		_, err = x.Next(testContext(t))
		wireModelError(t, err, "cancelled", "wire_cancelled", true, false)
		if _, err = x.Next(testContext(t)); !errors.Is(err, io.EOF) {
			t.Fatal("late structured end")
		}
		if err := o.Credential.Use(func([]byte) error { return nil }); err != nil {
			t.Fatal("wire destroyed caller material", err)
		}
		b.StopAdmission()
		if !b.Joined() {
			t.Fatal("terminal parser slot remained")
		}
		v.settled(t, key)
	})
	t.Run("text_and_structured_share_exact_global_and_project_budget", func(t *testing.T) {
		b := wire.NewBudget()
		a := v.newAdapter(t, b)
		other := v.newAdapter(t, b)
		cfg := wireJSON(wireReply(`{"v":1}`, "stop", nil, nil))
		hold := 0
		cfg.HoldAfter = &hold
		key := v.scenario(t, cfg)
		var handles []*wire.Exchange
		for p := 0; p < 8; p++ {
			project := newID[id.Project](t)
			for i := 0; i < 8; i++ {
				r, o := structuredWireInput(t, v, key, wire.JSONResponse)
				r.ResponseFormat.Schema = json.RawMessage(structuredScalarSchema)
				adapter := a
				if i%2 == 0 {
					r.Snapshot.Identity.AdapterRevision = wire.OpenAIChatTextRevision
					r.Snapshot.Capabilities.StructuredOutputModes = nil
					r.ResponseFormat = mc.ResponseFormat{Kind: "text"}
					adapter = other
				}
				o.ProjectID = project
				o.Limits.Overall = 120 * time.Second
				handles = append(handles, v.start(t, testContext(t), adapter, r, o))
			}
			r, o := structuredWireInput(t, v, key, wire.JSONResponse)
			o.ProjectID = project
			if x, err := a.Start(testContext(t), r, o); x != nil {
				t.Fatal("mixed project ninth admitted")
			} else {
				wireFault(t, err, f.ResourceBusy)
			}
		}
		r, o := structuredWireInput(t, v, key, wire.JSONResponse)
		if x, err := a.Start(testContext(t), r, o); x != nil {
			t.Fatal("mixed global 65th admitted")
		} else {
			wireFault(t, err, f.ResourceBusy)
		}
		wireWait(t, "mixed accepted requests not observed", func() bool { return len(v.state(t, key).Requests) == 64 })
		for _, x := range handles {
			if x.Joined() {
				t.Fatal("held body released slot")
			}
		}
		b.StopAdmission()
		if x, err := other.Start(testContext(t), r, o); x != nil {
			t.Fatal("mixed stopped admission accepted")
		} else {
			wireFault(t, err, f.ShuttingDown)
		}
		cancelled, stop := context.WithCancel(context.Background())
		stop()
		if err := b.Drain(cancelled); err == nil || b.Joined() {
			t.Fatal("cancelled Drain declared held clients terminal")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := b.Force(ctx); err != nil || !b.Joined() {
			t.Fatal("mixed Force did not join actual clients", err)
		}
		for _, x := range handles {
			if !x.Joined() {
				t.Fatal("mixed handle escaped owner")
			}
		}
		if len(v.state(t, key).Requests) != 64 {
			t.Fatal("excess request sent/queued")
		}
		v.settled(t, key)
	})
}
