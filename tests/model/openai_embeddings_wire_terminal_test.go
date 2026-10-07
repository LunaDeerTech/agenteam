//go:build integration

package model_test

import (
	"context"
	"encoding/json"
	"net/http/httptrace"
	"reflect"
	"sync"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	wire "github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func TestModelOpenAIEmbeddingsWireTerminal(t *testing.T) {
	v, a, ctx := embeddingWireFixture(t)
	v.allow(t, true)
	t.Run("valid_json_prefix_is_not_success_before_actual_eof", func(t *testing.T) {
		cfg := wireJSON(embeddingWireReply(t, []int{0}, [][]float64{{1, 2}}, json.RawMessage(`{"prompt_tokens":0}`)))
		hold := 1
		cfg.HoldAfter = &hold
		key := embeddingWireScenario(t, v, cfg)
		r, o := embeddingWireInput(t, v, key)
		x := embeddingWireStart(t, ctx, a, r, o)
		out := embeddingWireConsume(t, ctx, x)
		wireWait(t, "held embedding response not observed", func() bool {
			return x.Observe().Decision != nil && v.state(t, key).ActiveHandlers == 1
		})
		window := time.NewTimer(40 * time.Millisecond)
		defer window.Stop()
		select {
		case <-out:
			t.Fatal("parseable bytes escaped before HTTP EOF")
		case <-window.C:
		}
		if x.Joined() || !reflect.DeepEqual(x.Observe().Usage, mc.Usage{Source: mc.UnknownUsage}) {
			t.Fatal("held EOF manufactured terminal or reliable usage")
		}
		if err := v.net.Release(ctx, key); err != nil {
			t.Fatal("release controlled EOF", err)
		}
		select {
		case got := <-out:
			if got.err != nil || len(got.result.Items) != 1 || !x.Joined() || got.result.Usage.InputTokens == nil || *got.result.Usage.InputTokens != 0 {
				t.Fatal("actual EOF did not publish the full result", got.err)
			}
		case <-ctx.Done():
			t.Fatal("released result did not return")
		}
		v.settled(t, key)
	})
	t.Run("truncation_and_tighter_cap_publish_zero_and_no_usage", func(t *testing.T) {
		for _, kind := range []string{"truncated_json", "valid_json_without_chunked_eof", "tighter_cap"} {
			t.Run(kind, func(t *testing.T) {
				body := embeddingWireReply(t, []int{0}, [][]float64{{1, 2}}, json.RawMessage(`{"prompt_tokens":4}`))
				cfg := wireJSON(body)
				disconnect := 1
				switch kind {
				case "truncated_json":
					cfg.Chunks[0] = body[:len(body)-1]
				case "valid_json_without_chunked_eof":
					cfg.DisconnectAfter = &disconnect
				}
				key := embeddingWireScenario(t, v, cfg)
				r, o := embeddingWireInput(t, v, key)
				if kind == "tighter_cap" {
					o.Limits.ResponseBodyBytes = 64
				}
				x := embeddingWireStart(t, ctx, a, r, o)
				result, err := x.Result(ctx)
				embeddingWireZero(t, result)
				category, code := mc.ErrorCategory("provider_error"), "wire_protocol_invalid"
				if kind == "valid_json_without_chunked_eof" {
					category, code = "network", "wire_transport_error"
				} else if kind == "tighter_cap" {
					code = "wire_limit_exceeded"
				}
				wireModelError(t, err, category, code, true, false)
				if !reflect.DeepEqual(x.Observe().Usage, mc.Usage{Source: mc.UnknownUsage}) {
					t.Fatal("incomplete bounded response invented usage")
				}
				embeddingWireClose(t, x)
				v.settled(t, key)
			})
		}
	})
	t.Run("complete_usage_survives_bad_dimension_and_late_caller_cancel", func(t *testing.T) {
		for _, cancelled := range []bool{false, true} {
			key := embeddingWireScenario(t, v, wireJSON(embeddingWireReply(t, []int{0}, [][]float64{{1, 2}}, json.RawMessage(`{"prompt_tokens":9007199254740993,"total_tokens":null}`))))
			r, o := embeddingWireInput(t, v, key)
			if !cancelled {
				r.ExpectedDimensions = 3
			}
			x := embeddingWireStart(t, ctx, a, r, o)
			call := ctx
			if cancelled {
				// The formal writer precedes response reads; this separate
				// checkpoint proves cancellation after complete JSON/EOF.
				wireWait(t, "complete usage checkpoint not reached", func() bool { return x.Observe().Usage.InputTokens != nil })
				c, cancel := context.WithCancel(ctx)
				cancel()
				call = c
			}
			result, err := x.Result(call)
			embeddingWireZero(t, result)
			if cancelled {
				wireModelError(t, err, "cancelled", "wire_cancelled", true, false)
			} else {
				wireModelError(t, err, "provider_error", "wire_protocol_invalid", true, false)
			}
			u := x.Observe().Usage
			if u.Source != mc.ProviderUsage || u.InputTokens == nil || *u.InputTokens != 9007199254740993 || u.TotalTokens != nil {
				t.Fatal("reliable EOF usage lost on result failure")
			}
			*u.InputTokens = 0
			if *x.Observe().Usage.InputTokens != 9007199254740993 {
				t.Fatal("usage observation not isolated")
			}
			embeddingWireClose(t, x)
			if late, err := x.Result(ctx); err == nil {
				t.Fatal("failed result recovered a late vector")
			} else {
				embeddingWireZero(t, late)
				wireFault(t, err, f.InvalidState)
			}
			v.settled(t, key)
		}
	})
	t.Run("cancelled_real_writer_owns_material_and_mixed_project_slot_until_join", func(t *testing.T) {
		b := wire.NewBudget()
		ea := embeddingWireAdapter(t, v, b)
		chat := v.newAdapter(t, b)
		cfg := wireJSON(embeddingWireReply(t, []int{0}, [][]float64{{1, 2}}, nil))
		hold := 0
		cfg.HoldAfter = &hold
		key := embeddingWireScenario(t, v, cfg)
		r, o := embeddingWireInput(t, v, key)
		entered, released, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var once sync.Once
		release := func() { once.Do(func() { close(released) }) }
		t.Cleanup(release)
		attempt, cancel := context.WithCancel(ctx)
		t.Cleanup(cancel)
		trace := &httptrace.ClientTrace{WroteRequest: func(httptrace.WroteRequestInfo) { close(entered); <-released; close(returned) }}
		x := embeddingWireStart(t, httptrace.WithClientTrace(attempt, trace), ea, r, o)
		waitSignal(t, entered)
		wireWait(t, "held writer not sent", func() bool { return len(v.state(t, key).Requests) == 1 })
		cancel()
		wireWait(t, "cancelled writer lacks actual Decision", func() bool { return x.Observe().Decision != nil })
		result, err := x.Result(ctx)
		embeddingWireZero(t, result)
		wireModelError(t, err, "cancelled", "wire_cancelled", true, false)
		short, stop := context.WithTimeout(ctx, 30*time.Millisecond)
		err = x.Close(short)
		stop()
		wireModelError(t, err, "timeout", "wire_transport_error", false, false)
		if d := x.Observe().Decision; d == nil || d.Reason != ac.Cancelled || !d.Sent || x.Joined() {
			t.Fatal("visible cancellation released a live writer")
		}
		if err := o.Credential.Use(func([]byte) error { return nil }); err != nil {
			t.Fatal("borrowed material not live while writer remains")
		}
		chatCfg := wireJSON(wireReply("unused", "stop", nil, nil))
		chatCfg.HoldAfter = &hold
		chatKey := v.scenario(t, chatCfg)
		for i := 0; i < 7; i++ {
			cr, co := v.input(t, chatKey, wire.JSONResponse)
			co.ProjectID = o.ProjectID
			v.start(t, ctx, chat, cr, co)
		}
		cr, co := v.input(t, chatKey, wire.JSONResponse)
		co.ProjectID = o.ProjectID
		if extra, err := chat.Start(ctx, cr, co); extra != nil {
			t.Fatal("cancelled embedding lost its mixed project slot")
		} else {
			wireFault(t, err, f.ResourceBusy)
		}
		wireWait(t, "seven mixed chat slots not dispatched", func() bool { return len(v.state(t, chatKey).Requests) == 7 })
		release()
		waitSignal(t, returned)
		embeddingWireClose(t, x)
		replacement := v.start(t, ctx, chat, cr, co)
		wireWait(t, "actual writer join did not release one slot", func() bool { return len(v.state(t, chatKey).Requests) == 8 })
		if replacement.Joined() {
			t.Fatal("held replacement response prematurely terminal")
		}
		cleanup, finish := context.WithTimeout(ctx, 5*time.Second)
		defer finish()
		if err := b.Force(cleanup); err != nil || !b.Joined() {
			t.Fatal("mixed actual cleanup failed", err)
		}
		if len(v.state(t, key).Requests) != 1 || len(v.state(t, chatKey).Requests) != 8 {
			t.Fatal("rejected mixed work was queued or retried")
		}
		v.settled(t, key)
		v.settled(t, chatKey)
	})
}
