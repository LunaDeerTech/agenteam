//go:build integration

package model_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptrace"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	wire "github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func wireDrain(t *testing.T, x *wire.Exchange) ([]wire.Event, error) {
	t.Helper()
	var events []wire.Event
	for i := 0; i < 1024; i++ {
		event, e := x.Next(testContext(t))
		if errors.Is(e, io.EOF) {
			return events, nil
		}
		if e != nil {
			if _, again := x.Next(testContext(t)); !errors.Is(again, io.EOF) {
				t.Fatal("wire error did not terminate once")
			}
			return events, e
		}
		events = append(events, event)
	}
	t.Fatal("unbounded event count")
	return nil, nil
}
func wireStreamStart(t *testing.T, v *wireFixture, chunks ...[]byte) (*wire.Exchange, string) {
	t.Helper()
	key := v.scenario(t, wireSSE(chunks...))
	r, o := v.input(t, key, wire.SSEResponse)
	return v.start(t, testContext(t), v.adapter, r, o), key
}

func TestModelOpenAIChatWireStream(t *testing.T) {
	v := newWireFixture(t)
	v.allow(t, true)
	t.Run("fragmented_utf8_crlf_default_obfuscation_and_final_usage", func(t *testing.T) {
		first := wireChunk(map[string]any{"role": "assistant", "content": "世界"}, nil, nil, wireBodyCanary, false)
		first = bytes.Replace(first, []byte(`,"choices"`), []byte(",\ndata: \"choices\""), 1)
		// JSON produced by the helper has deterministic map-key order. Add a second
		// data line at an existing object comma, then split arbitrary network bytes.
		if !bytes.Contains(first, []byte("\ndata: ")) {
			first = bytes.Replace(first, []byte(`,"created"`), []byte(",\ndata: \"created\""), 1)
		}
		raw := append([]byte(": keepalive\n\n"), first...)
		raw = append(raw, wireChunk(map[string]any{"content": " text"}, nil, nil, json.RawMessage(`null`), false)...)
		raw = append(raw, wireChunk(map[string]any{}, "length", nil, nil, false)...)
		raw = append(raw, wireChunk(nil, nil, json.RawMessage(`{"prompt_tokens":7,"completion_tokens":0,"total_tokens":9,"prompt_tokens_details":{"cached_tokens":2,"cache_write_tokens":3},"completion_tokens_details":{"reasoning_tokens":4}}`), wireBodyCanary, true)...)
		raw = append(raw, []byte("data: [DONE]\n\n")...)
		raw = bytes.ReplaceAll(raw, []byte("\n"), []byte("\r\n"))
		var chunks [][]byte
		for len(raw) > 0 {
			n := min(11, len(raw))
			chunks = append(chunks, bytes.Clone(raw[:n]))
			raw = raw[n:]
		}
		x, key := wireStreamStart(t, v, chunks...)
		events, e := wireDrain(t, x)
		if e != nil {
			t.Fatal(e)
		}
		var text string
		updates, ends := 0, 0
		for _, event := range events {
			switch event.Kind {
			case wire.TextDelta:
				if !utf8.ValidString(event.Text) || len(event.Text) > 64<<10 {
					t.Fatal("invalid text delta")
				}
				text += event.Text
			case wire.UsageUpdate:
				updates++
				if event.Usage == nil || event.Usage.TotalTokens == nil || *event.Usage.TotalTokens != 9 {
					t.Fatal("usage update")
				}
				*event.Usage.TotalTokens = 1
			case wire.StreamEnd:
				ends++
				if event.End == nil || event.End.FinishReason != "length" || event.End.Usage.TotalTokens == nil || *event.End.Usage.TotalTokens != 9 || event.End.ProviderRequestID != "req_stream-1" {
					t.Fatal("stream end")
				}
			default:
				t.Fatal("unknown event")
			}
			wireNoLeak(t, event)
		}
		if text != "世界 text" || updates != 1 || ends != 1 || !x.Joined() || *x.Observe().Usage.TotalTokens != 9 {
			t.Fatal("stream projection/copies/join")
		}
		state := v.state(t, key)
		if len(state.Requests) != 1 {
			t.Fatal("SSE sent more than once")
		}
		var body map[string]json.RawMessage
		if json.Unmarshal([]byte(state.Requests[0].Body), &body) != nil || string(body["stream"]) != "true" || string(body["stream_options"]) != `{"include_usage":true}` || strings.Contains(state.Requests[0].Body, "include_obfuscation") {
			t.Fatal("SSE native request")
		}
		wireClose(t, x)
	})
	t.Run("no_usage_and_content_filter_finish_are_not_refusal", func(t *testing.T) {
		for _, finish := range []string{"stop", "content_filter"} {
			x, _ := wireStreamStart(t, v, wireChunk(map[string]any{"content": "ok"}, finish, nil, nil, false), []byte("data: [DONE]\n\n"))
			events, e := wireDrain(t, x)
			if e != nil || len(events) != 2 || events[1].Kind != wire.StreamEnd || events[1].End.FinishReason != mc.FinishReason(finish) || events[1].End.Usage.Source != mc.UnknownUsage || !x.Joined() {
				t.Fatal("unknown usage/finish", e)
			}
			wireClose(t, x)
		}
	})
	t.Run("protocol_errors_and_interruption_never_emit_end", func(t *testing.T) {
		for _, tc := range []struct {
			name     string
			chunks   [][]byte
			category mc.ErrorCategory
			code     string
		}{
			{"missing_done", [][]byte{wireChunk(map[string]any{}, "stop", nil, nil, false)}, "provider_error", "wire_protocol_invalid"},
			{"premature_done", [][]byte{[]byte("data: [DONE]\n\n")}, "provider_error", "wire_protocol_invalid"},
			{"bad_event", [][]byte{[]byte("event: error\ndata: {}\n\n")}, "provider_error", "wire_protocol_invalid"},
			{"unknown_finish", [][]byte{wireChunk(map[string]any{}, "unexpected", nil, nil, false)}, "provider_error", "wire_protocol_invalid"},
			{"tools", [][]byte{wireChunk(map[string]any{"tool_calls": []any{map[string]any{"index": 0}}}, nil, nil, nil, false)}, "unsupported_feature", "wire_unsupported_feature"},
			{"obfuscation_type", [][]byte{wireChunk(map[string]any{}, nil, nil, 123, false)}, "provider_error", "wire_protocol_invalid"},
			{"duplicate_key", [][]byte{[]byte("data: {\"id\":\"a\",\"id\":\"b\"}\n\n")}, "provider_error", "wire_protocol_invalid"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				x, _ := wireStreamStart(t, v, tc.chunks...)
				events, e := wireDrain(t, x)
				wireModelError(t, e, tc.category, tc.code, true, false)
				for _, event := range events {
					if event.Kind == wire.StreamEnd {
						t.Fatal("error stream ended successfully")
					}
				}
				wireClose(t, x)
			})
		}
		cfg := wireSSE(wireChunk(map[string]any{"content": "prefix"}, nil, nil, nil, false))
		n := 1
		cfg.DisconnectAfter = &n
		key := v.scenario(t, cfg)
		r, o := v.input(t, key, wire.SSEResponse)
		x := v.start(t, testContext(t), v.adapter, r, o)
		events, e := wireDrain(t, x)
		wireModelError(t, e, "network", "wire_transport_error", true, true)
		if len(events) != 1 || events[0].Text != "prefix" {
			t.Fatal("disconnect prefix")
		}
		wireClose(t, x)
	})
	t.Run("refusal_is_one_safe_error_with_actual_prefix", func(t *testing.T) {
		for _, prefix := range []bool{false, true} {
			var chunks [][]byte
			if prefix {
				chunks = append(chunks, wireChunk(map[string]any{"content": "delivered"}, nil, nil, nil, false))
			}
			chunks = append(chunks, wireChunk(map[string]any{"refusal": wireBodyCanary}, nil, nil, nil, false))
			x, _ := wireStreamStart(t, v, chunks...)
			events, e := wireDrain(t, x)
			m := wireModelError(t, e, "content_filter", "", true, prefix)
			if m.Retryable {
				t.Fatal("retry refusal")
			}
			for _, event := range events {
				if event.Kind != wire.TextDelta || event.Text != "delivered" {
					t.Fatal("refusal produced extra event")
				}
			}
			wireClose(t, x)
		}
	})
	t.Run("observed_usage_survives_later_bad_event", func(t *testing.T) {
		x, _ := wireStreamStart(t, v, wireChunk(map[string]any{}, "stop", nil, nil, false), wireChunk(nil, nil, json.RawMessage(`{"completion_tokens":0}`), nil, true), []byte("event: error\ndata: {}\n\n"))
		events, e := wireDrain(t, x)
		wireModelError(t, e, "provider_error", "wire_protocol_invalid", true, false)
		if len(events) != 1 || events[0].Kind != wire.UsageUpdate || x.Observe().Usage.OutputTokens == nil || *x.Observe().Usage.OutputTokens != 0 {
			t.Fatal("lost known usage")
		}
		wireClose(t, x)
	})
	t.Run("real_d04_body_cap_is_not_ignored_obfuscation", func(t *testing.T) {
		cfg := wireSSE(wireChunk(map[string]any{}, "stop", nil, strings.Repeat("x", 2048), false), []byte("data: [DONE]\n\n"))
		key := v.scenario(t, cfg)
		r, o := v.input(t, key, wire.SSEResponse)
		o.Limits.ResponseBodyBytes = 512
		x := v.start(t, testContext(t), v.adapter, r, o)
		events, e := wireDrain(t, x)
		wireModelError(t, e, "provider_error", "wire_limit_exceeded", true, false)
		if len(events) != 0 || x.Observe().Decision == nil || x.Observe().Decision.Reason != ac.ResponseLimit {
			t.Fatal("D04 response cap bypassed")
		}
		wireClose(t, x)
	})
	t.Run("slow_consumer_backpressure_cancel_has_no_late_end", func(t *testing.T) {
		var chunks [][]byte
		for i := 0; i < 40; i++ {
			chunks = append(chunks, wireChunk(map[string]any{"content": strings.Repeat("x", 8000)}, nil, nil, nil, false))
		}
		chunks = append(chunks, wireChunk(map[string]any{}, "stop", nil, nil, false), []byte("data: [DONE]\n\n"))
		x, key := wireStreamStart(t, v, chunks...)
		wireWait(t, "server did not finish bounded stream", func() bool { return v.state(t, key).CompletedHandlers == 1 })
		// More than both queue limits has been sent. Without a consumer the parser
		// cannot reach terminal; server completion alone is not client join.
		if x.Joined() {
			t.Fatal("parser bypassed bounded backpressure")
		}
		event, e := x.Next(testContext(t))
		if e != nil || event.Kind != wire.TextDelta {
			t.Fatal("prefix", e)
		}
		cancelCtx, cancel := context.WithCancel(context.Background())
		cancel()
		_, e = x.Next(cancelCtx)
		wireModelError(t, e, "cancelled", "wire_cancelled", true, true)
		wireClose(t, x)
		if _, e = x.Next(testContext(t)); !errors.Is(e, io.EOF) {
			t.Fatal("late event after cancellation")
		}
	})
}

func TestModelOpenAIChatWireJoinAndBudget(t *testing.T) {
	v := newWireFixture(t)
	v.allow(t, true)
	t.Run("real_writer_late_return_keeps_exact_slot", func(t *testing.T) {
		b := wire.NewBudget()
		a := v.newAdapter(t, b)
		other := v.newAdapter(t, b)
		n := 0
		cfg := wireJSON(wireReply("held", "stop", nil, nil))
		cfg.HoldAfter = &n
		key := v.scenario(t, cfg)
		entered, released, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var once sync.Once
		release := func() { once.Do(func() { close(released) }) }
		defer release()
		ctx, cancel := context.WithCancel(testContext(t))
		defer cancel()
		ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{WroteRequest: func(httptrace.WroteRequestInfo) { close(entered); <-released; close(returned) }})
		r, o := v.input(t, key, wire.JSONResponse)
		project := o.ProjectID
		x := v.start(t, ctx, a, r, o)
		waitSignal(t, entered)
		wireWait(t, "late writer has not reached actual server", func() bool { return len(v.state(t, key).Requests) == 1 })
		cancel()
		short, stop := context.WithTimeout(context.Background(), 30*time.Millisecond)
		e := x.Close(short)
		stop()
		wireModelError(t, e, "timeout", "wire_transport_error", false, false)
		// Close's own control error is not a Result and makes no Sent claim. The
		// actual Do has independently returned cancelled with its real Decision.
		wireWait(t, "cancelled Do has not produced Decision", func() bool { return x.Observe().Decision != nil })
		if x.Joined() || x.Observe().Decision.Reason != ac.Cancelled || !x.Observe().Decision.Sent {
			t.Fatal("cancellation falsely joined real writer")
		}
		for i := 0; i < 7; i++ {
			r, o = v.input(t, key, wire.JSONResponse)
			o.ProjectID = project
			v.start(t, testContext(t), other, r, o)
		}
		r, o = v.input(t, key, wire.JSONResponse)
		o.ProjectID = project
		if handle, e := a.Start(testContext(t), r, o); handle != nil {
			t.Fatal("unjoined writer slot replaced")
		} else {
			wireFault(t, e, f.ResourceBusy)
		}
		wireWait(t, "accepted same-project calls not sent", func() bool { return len(v.state(t, key).Requests) == 8 })
		release()
		waitSignal(t, returned)
		wireClose(t, x)
		replacement := v.start(t, testContext(t), other, r, o)
		wireWait(t, "joined slot not reused", func() bool { return len(v.state(t, key).Requests) == 9 })
		if replacement.Joined() {
			t.Fatal("held body considered done")
		}
		b.StopAdmission()
		if handle, e := a.Start(testContext(t), r, o); handle != nil {
			t.Fatal("sealed admitted")
		} else {
			wireFault(t, e, f.ShuttingDown)
		}
		cancelled, cancelledStop := context.WithCancel(context.Background())
		cancelledStop()
		if e := b.Drain(cancelled); e == nil || b.Joined() {
			t.Fatal("Drain falsely joined held bodies")
		}
		force, forceStop := context.WithTimeout(context.Background(), 5*time.Second)
		defer forceStop()
		if e := b.Force(force); e != nil || !b.Joined() {
			t.Fatal("force/join", e)
		}
		v.settled(t, key)
	})
	t.Run("two_adapters_share_global64_project8_without_queue", func(t *testing.T) {
		b := wire.NewBudget()
		a := v.newAdapter(t, b)
		other := v.newAdapter(t, b)
		n := 0
		cfg := wireJSON(wireReply("hold", "stop", nil, nil))
		cfg.HoldAfter = &n
		key := v.scenario(t, cfg)
		var first id.ProjectID
		var handles []*wire.Exchange
		for p := 0; p < 8; p++ {
			project := newID[id.Project](t)
			if p == 0 {
				first = project
			}
			for i := 0; i < 8; i++ {
				r, o := v.input(t, key, wire.JSONResponse)
				o.ProjectID = project
				o.Limits.Overall = 120 * time.Second
				adapter := a
				if i%2 == 1 {
					adapter = other
				}
				handles = append(handles, v.start(t, testContext(t), adapter, r, o))
			}
			if p == 0 {
				r, o := v.input(t, key, wire.JSONResponse)
				o.ProjectID = first
				if x, e := other.Start(testContext(t), r, o); x != nil {
					t.Fatal("project 9 admitted")
				} else {
					wireFault(t, e, f.ResourceBusy)
				}
			}
		}
		r, o := v.input(t, key, wire.JSONResponse)
		if x, e := a.Start(testContext(t), r, o); x != nil {
			t.Fatal("global65 admitted")
		} else {
			wireFault(t, e, f.ResourceBusy)
		}
		wireWait(t, "64 accepted calls not observed", func() bool { return len(v.state(t, key).Requests) == 64 })
		for _, x := range handles {
			if x.Joined() {
				t.Fatal("held response retired")
			}
		}
		b.StopAdmission()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if e := b.Force(ctx); e != nil || !b.Joined() {
			t.Fatal("global force", e)
		}
		for _, x := range handles {
			if !x.Joined() {
				t.Fatal("accepted handle escaped owner")
			}
		}
		if len(v.state(t, key).Requests) != 64 {
			t.Fatal("excess/queued request sent")
		}
		v.settled(t, key)
	})
}
