package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
)

func chunk(delta map[string]any, finish any, usage any) string {
	choices := []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}
	if delta == nil {
		choices = []any{}
	}
	b, _ := json.Marshal(map[string]any{"id": "chat-1", "object": "chat.completion.chunk", "created": 1, "model": "native-model", "choices": choices, "usage": usage, "obfuscation": "default-padding"})
	return "data: " + string(b) + "\n\n"
}

type splitReader struct{ io.Reader }

func (r splitReader) Read(p []byte) (int, error) { return r.Reader.Read(p[:min(1, len(p))]) }
func parseStream(t *testing.T, r io.Reader) ([]Event, error, *Exchange) {
	t.Helper()
	x := localExchange(t, SSEResponse)
	x.observation.Decision = &outbound.Decision{Sent: true}
	go func() { end, err := x.stream(r); finishLocal(x, Result{End: end}, err) }()
	var events []Event
	for {
		e, err := x.Next(context.Background())
		if err != nil {
			if err == io.EOF {
				return events, nil, x
			}
			return events, err, x
		}
		events = append(events, e)
	}
}
func TestOpenAIChatSSETextUsageAndRequiredTermination(t *testing.T) {
	stream := ": comment\r\n\r\n" + chunk(map[string]any{"role": "assistant", "content": "世"}, nil, nil) + chunk(map[string]any{"content": "界"}, "length", nil) + chunk(nil, nil, map[string]any{"prompt_tokens": json.Number("9007199254740993"), "completion_tokens": 0}) + "data: [DONE]\n\n"
	events, err, x := parseStream(t, splitReader{strings.NewReader(stream)})
	if err != nil {
		t.Fatal(err)
	}
	var text string
	var final *End
	var usage *mc.Usage
	for _, e := range events {
		switch e.Kind {
		case TextDelta:
			text += e.Text
		case UsageUpdate:
			usage = e.Usage
		case StreamEnd:
			if final != nil {
				t.Fatal("duplicate end")
			}
			final = e.End
		}
	}
	if text != "世界" || final == nil || final.FinishReason != "length" || usage == nil || *usage.InputTokens != 9007199254740993 || *usage.OutputTokens != 0 || *x.Observe().Usage.InputTokens != 9007199254740993 {
		t.Fatal("wrong text/usage/end")
	}
	if _, err := x.Next(context.Background()); err != io.EOF || !x.Joined() {
		t.Fatal("terminal/join", err)
	}
	for _, tc := range []struct {
		name, stream string
		category     mc.ErrorCategory
	}{
		{"missing_done", chunk(map[string]any{"content": "prefix"}, "stop", nil), "provider_error"},
		{"premature_done", "data: [DONE]\n\n", "provider_error"},
		{"duplicate_finish", chunk(map[string]any{}, "stop", nil) + chunk(map[string]any{}, "stop", nil) + "data: [DONE]\n\n", "provider_error"},
		{"usage_before_finish", chunk(nil, nil, map[string]any{"total_tokens": 1}), "provider_error"},
		{"unknown_finish", chunk(map[string]any{}, "future_reason", nil), "provider_error"},
		{"empty_finish", chunk(map[string]any{}, "", nil), "provider_error"},
		{"empty_role", chunk(map[string]any{"role": ""}, nil, nil), "provider_error"},
		{"refusal", chunk(map[string]any{"content": "prefix"}, nil, nil) + chunk(map[string]any{"refusal": "private-refusal-canary"}, nil, nil), "content_filter"},
		{"tool", chunk(map[string]any{"tool_calls": []any{map[string]any{"index": 0}}}, nil, nil), "unsupported_feature"},
		{"bad_json", "data: {\"id\":\"x\",\"id\":\"y\"}\n\n", "provider_error"},
		{"error_event", "event: error\ndata: {\"error\":{\"message\":\"private-canary\"}}\n\n", "provider_error"},
		{"wrong_obfuscation", strings.Replace(chunk(map[string]any{}, "stop", nil), `"obfuscation":"default-padding"`, `"obfuscation":2`, 1), "provider_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			events, err, x := parseStream(t, strings.NewReader(tc.stream))
			var model *mc.ModelError
			if !errors.As(err, &model) || model.Category != tc.category || strings.Contains(err.Error(), "canary") {
				t.Fatal("wrong failure", err)
			}
			for _, e := range events {
				if e.Kind == StreamEnd {
					t.Fatal("false successful terminal")
				}
			}
			if _, err := x.Next(context.Background()); err != io.EOF {
				t.Fatal("more than one error")
			}
		})
	}
}
func TestOpenAIChatSSEUnknownUsageAndBoundedEvents(t *testing.T) {
	for _, obfuscation := range []string{`null`, `"padding"`} {
		input := strings.Replace(chunk(map[string]any{"content": ""}, "content_filter", nil), `"obfuscation":"default-padding"`, `"obfuscation":`+obfuscation, 1) + "data: [DONE]\n\n"
		events, err, _ := parseStream(t, strings.NewReader(input))
		if err != nil || len(events) != 1 || events[0].End.Usage.Source != mc.UnknownUsage || events[0].End.FinishReason != "content_filter" {
			t.Fatal(events, err)
		}
	}
	input := chunk(map[string]any{}, "stop", nil) + chunk(nil, nil, map[string]any{"total_tokens": 7}) + "data: not-json\n\n"
	_, err, x := parseStream(t, strings.NewReader(input))
	if err == nil || *x.Observe().Usage.TotalTokens != 7 {
		t.Fatal("observed reliable usage lost", err)
	}
	input = "data: " + strings.Repeat(" ", maxEventBytes) + "\n\n"
	_, err, _ = parseStream(t, strings.NewReader(input))
	requireModel(t, err, "provider_error", "wire_limit_exceeded")
	input = strings.Replace(chunk(map[string]any{}, "stop", nil), "default-padding", strings.Repeat("x", maxEventBytes), 1)
	_, err, _ = parseStream(t, strings.NewReader(input))
	requireModel(t, err, "provider_error", "wire_limit_exceeded")
	big := strings.Repeat("世界", 40000)
	input = chunk(map[string]any{"content": big}, "stop", nil) + "data: [DONE]\n\n"
	events, err, _ := parseStream(t, strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	var parts []string
	for _, e := range events {
		if e.Kind == TextDelta {
			if len(e.Text) > 64<<10 {
				t.Fatal("oversized delta")
			}
			parts = append(parts, e.Text)
		}
	}
	if strings.Join(parts, "") != big {
		t.Fatal("UTF8 splitting changed text")
	}
}

func TestOpenAIChatSSECumulativeTextBound(t *testing.T) {
	part := chunk(map[string]any{"content": strings.Repeat("x", 512<<10)}, nil, nil)
	readers := make([]io.Reader, 33)
	for i := range readers {
		readers[i] = strings.NewReader(part)
	}
	events, err, _ := parseStream(t, io.MultiReader(readers...))
	requireModel(t, err, "provider_error", "wire_limit_exceeded")
	n := 0
	for _, event := range events {
		if event.Kind != TextDelta {
			t.Fatal("oversized stream produced terminal")
		}
		n += len(event.Text)
	}
	if n != maxTextBytes {
		t.Fatal("cumulative text limit did not preserve exact accepted prefix", n)
	}
}
