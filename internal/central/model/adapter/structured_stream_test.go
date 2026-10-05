package adapter

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
)

func structuredStream(t *testing.T, stream string) ([]Event, error, *Exchange) {
	t.Helper()
	x := localExchange(t, SSEResponse)
	x.schema = singleSchema(t, map[string]any{"type": "integer"})
	x.observation.Decision = &outbound.Decision{Sent: true}
	go func() {
		end, err := x.stream(splitReader{strings.NewReader(stream)})
		finishLocal(x, Result{End: end}, err)
	}()
	var events []Event
	for {
		e, err := x.Next(context.Background())
		if err != nil {
			if errors.Is(err, io.EOF) {
				return events, nil, x
			}
			return events, err, x
		}
		events = append(events, e)
	}
}
func TestStructuredStreamCompletion(t *testing.T) {
	for _, tc := range []struct {
		name, content, finish, tail, category string
		success                               bool
	}{
		{"success", `{"v":9007199254740993}`, "stop", "data: [DONE]\n\n", "", true},
		{"wrong_type", `{"v":"x"}`, "stop", "data: [DONE]\n\n", "provider_error", false},
		{"extra_key", `{"v":1,"x":2}`, "stop", "data: [DONE]\n\n", "provider_error", false},
		{"trailing_json", `{"v":1}{}`, "stop", "data: [DONE]\n\n", "provider_error", false},
		{"length_even_valid", `{"v":1}`, "length", "data: [DONE]\n\n", "provider_error", false},
		{"filtered", `{"v":1}`, "content_filter", "data: [DONE]\n\n", "content_filter", false},
		{"missing_done", `{"v":1}`, "stop", "", "provider_error", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stream := chunk(map[string]any{"role": "assistant", "content": tc.content[:3]}, nil, nil) + chunk(map[string]any{"content": tc.content[3:]}, tc.finish, nil) + chunk(nil, nil, map[string]any{"completion_tokens": 0, "total_tokens": 9}) + tc.tail
			events, err, x := structuredStream(t, stream)
			var text string
			ends, usages := 0, 0
			for _, e := range events {
				switch e.Kind {
				case TextDelta:
					text += e.Text
				case UsageUpdate:
					usages++
				case StreamEnd:
					ends++
				}
			}
			if text != tc.content || usages != 1 || x.Observe().Usage.TotalTokens == nil || *x.Observe().Usage.TotalTokens != 9 || x.Observe().Usage.OutputTokens == nil || *x.Observe().Usage.OutputTokens != 0 {
				t.Fatal("prefix/usage lost")
			}
			if tc.success {
				if err != nil || ends != 1 || !x.Joined() {
					t.Fatal("valid strict stream", err)
				}
			} else {
				code := "wire_protocol_invalid"
				if tc.category == "content_filter" {
					code = ""
				}
				m := requireModel(t, err, mc.ErrorCategory(tc.category), code)
				if ends != 0 || !m.PartialOutput || !m.Dispatched || m.Retryable {
					t.Fatal("false structured terminal")
				}
				if _, e := x.Next(context.Background()); e != io.EOF {
					t.Fatal("error repeated")
				}
			}
		})
	}
	s := singleSchema(t, map[string]any{"type": "integer"})
	for _, finish := range []mc.FinishReason{"length", "content_filter"} {
		err := s.complete(context.Background(), `{"v":1}`, finish)
		if finish == "length" {
			requireModel(t, err, "provider_error", "wire_protocol_invalid")
		} else {
			requireModel(t, err, "content_filter", "")
		}
	}
	stream := chunk(map[string]any{"content": `{"v":1}`}, "stop", nil) + "data: [DONE]\n\n"
	events, err, x := structuredStream(t, stream)
	if err != nil || len(events) != 2 || x.Observe().Usage.Source != mc.UnknownUsage {
		t.Fatal("unknown usage success", err)
	}
}

type cancelAtStructuredCheck struct {
	context.Context
	cancel context.CancelFunc
	checks atomic.Int32
}

func (c *cancelAtStructuredCheck) Err() error {
	if c.checks.Add(1) == 64 {
		c.cancel()
	}
	return c.Context.Err()
}
func TestStructuredValidationCancellation(t *testing.T) {
	base, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := &cancelAtStructuredCheck{Context: base, cancel: cancel}
	s := singleSchema(t, map[string]any{"type": "array", "items": map[string]any{"type": "integer"}})
	// The context wrapper triggers a real cancellation at a later parser check,
	// not a fabricated provider error or a pre-cancelled entry-only case.
	err := s.complete(ctx, `{"v":[`+strings.Repeat("0,", 1000)+`0]}`, "stop")
	requireModel(t, err, "cancelled", "wire_cancelled")
	if ctx.checks.Load() < 64 || base.Err() != context.Canceled {
		t.Fatal("validation did not reach cancellation boundary")
	}
	x := localExchange(t, SSEResponse)
	x.schema = s
	x.cancel()
	end, err := x.stream(strings.NewReader("data: [DONE]\n\n"))
	finishLocal(x, Result{End: end}, err)
	_, err = x.Next(context.Background())
	requireModel(t, err, "cancelled", "wire_cancelled")
	if _, err = x.Next(context.Background()); err != io.EOF {
		t.Fatal("late terminal after cancellation")
	}
	if !x.Joined() {
		t.Fatal("cancelled parser not terminal")
	}
	for _, kind := range []string{"string", "number"} {
		base, cancel := context.WithCancel(context.Background())
		ctx := &cancelAtStructuredCheck{Context: base, cancel: cancel}
		schema := singleSchema(t, map[string]any{"type": kind})
		body := `{"v":"` + strings.Repeat("x", 4<<20) + `"}`
		if kind == "number" {
			body = `{"v":` + strings.Repeat("9", 4<<20) + `}`
		}
		err := schema.validate(ctx, body)
		if kind == "string" {
			requireModel(t, err, "cancelled", "wire_cancelled")
			if ctx.checks.Load() < 64 {
				t.Fatal("long string lacked scan cancellation")
			}
		} else {
			requireModel(t, err, "provider_error", "wire_limit_exceeded")
			if ctx.checks.Load() >= 64 || base.Err() != nil {
				t.Fatal("number limit waited for a whole-value scan")
			}
		}
		cancel()
	}
}

func TestStructuredStreamAccumulatedContentBound(t *testing.T) {
	x := localExchange(t, SSEResponse)
	x.schema = singleSchema(t, map[string]any{"type": "string"})
	x.observation.Decision = &outbound.Decision{Sent: true}
	part := chunk(map[string]any{"content": strings.Repeat("x", 64<<10)}, nil, nil)
	input := io.MultiReader(strings.NewReader(chunk(map[string]any{"content": `{"v":"`}, nil, nil)), strings.NewReader(strings.Repeat(part, 256)))
	go func() { end, err := x.stream(input); finishLocal(x, Result{End: end}, err) }()
	var terminal error
	for count := 0; count < 300; count++ {
		event, err := x.Next(context.Background())
		if err != nil {
			terminal = err
			break
		}
		if event.Kind != TextDelta {
			t.Fatal("oversized structured stream completed")
		}
	}
	m := requireModel(t, terminal, "provider_error", "wire_limit_exceeded")
	if !m.PartialOutput || !x.Joined() {
		t.Fatal("bounded collector lost prefix/join")
	}
	if _, err := x.Next(context.Background()); err != io.EOF {
		t.Fatal("collector overflow did not terminate")
	}
	_, err, _ := structuredStream(t, "data: "+strings.Repeat(" ", 1<<20)+"\n\n")
	requireModel(t, err, "provider_error", "wire_limit_exceeded")
}

type structuredJoinBarrier struct {
	context.Context
	checks           atomic.Int32
	entered, release chan struct{}
}

func (c *structuredJoinBarrier) Err() error {
	// First check is at the ordinary successful consumer path; the second is
	// consumerJoin's existing check, after its wait context has been made.
	if c.checks.Add(1) == 2 {
		close(c.entered)
		<-c.release
	}
	return c.Context.Err()
}
func TestStructuredSuccessfulConsumptionRechecksCancellation(t *testing.T) {
	for _, mode := range []ResponseMode{JSONResponse, SSEResponse} {
		for _, target := range []string{"caller", "attempt"} {
			t.Run(string(mode)+"_"+target, func(t *testing.T) {
				x := localExchange(t, mode)
				x.schema = singleSchema(t, map[string]any{"type": "integer"})
				finishLocal(x, Result{Text: `{"v":1}`, End: End{FinishReason: "stop", Usage: mc.Usage{Source: mc.UnknownUsage}}}, nil)
				base, cancel := context.WithCancel(context.Background())
				defer cancel()
				ctx := &structuredJoinBarrier{Context: base, entered: make(chan struct{}), release: make(chan struct{})}
				done := make(chan error, 1)
				go func() {
					if mode == JSONResponse {
						_, err := x.Result(ctx)
						done <- err
					} else {
						_, err := x.Next(ctx)
						done <- err
					}
				}()
				select {
				case <-ctx.entered:
				case <-time.After(time.Second):
					close(ctx.release)
					t.Fatal("consumer join barrier not reached")
				}
				if target == "caller" {
					cancel()
				} else {
					x.cancel()
				}
				close(ctx.release)
				var err error
				select {
				case err = <-done:
				case <-time.After(time.Second):
					t.Fatal("consumer did not return")
				}
				if !x.Joined() {
					t.Fatal("cancellation probe displaced actual join")
				}
				if active, _ := x.budget.snapshot(); len(active) != 0 {
					t.Fatal("already-terminal owner retained slot")
				}
				requireModel(t, err, "cancelled", "wire_cancelled")
			})
		}
	}
}
