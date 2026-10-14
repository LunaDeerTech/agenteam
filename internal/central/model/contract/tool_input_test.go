package contract

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

func TestToolInputKeepsOriginalDigestAndBindsFullSet(t *testing.T) {
	messages := []Message{{Role: "system", Parts: []MessagePart{{Text: &TextPart{Text: "system"}}}}, {Role: "user", Parts: []MessagePart{{Text: &TextPart{Text: "user"}}}}}
	tool := Tool{Name: "read_file", InputSchema: json.RawMessage(`{"type":"object"}`)}
	digest, err := ToolCallInputDigest(context.Background(), messages, tool)
	if err != nil || digest != "sha256:0ce5bb113270f4d976905715babb72cbd0884cec081f9f2059f3883b2fd5897b" {
		t.Fatal("historical schema-2 bytes changed", err, digest)
	}
	tools := make([]Tool, 128)
	for n := range tools {
		tools[n] = Tool{Name: fmt.Sprintf("frozen_tool_%d", n), InputSchema: json.RawMessage(`{"type":"object"}`)}
	}
	complete, err := ToolInputDigest(context.Background(), messages, tools, ToolChoice{Kind: "required"})
	if err != nil {
		t.Fatal(err)
	}
	tools[127].Description = "the last configured core tool"
	changed, err := ToolInputDigest(context.Background(), messages, tools, ToolChoice{Kind: "required"})
	if err != nil || changed == complete {
		t.Fatal("last configured tool omitted", err)
	}
	if _, err := ToolInputDigest(context.Background(), messages, append(tools, tool), ToolChoice{Kind: "required"}); err == nil {
		t.Fatal("oversize ToolSet cropped")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ToolInputDigest(ctx, messages, tools, ToolChoice{Kind: "required"}); !errors.Is(err, context.Canceled) {
		t.Fatal("lost cancellation", err)
	}
}

func TestSuccessorSemanticKeepsEveryCompletedToolRound(t *testing.T) {
	_, parts := successorPartsFixture(t)
	parts.Messages = parts.Messages[:2]
	for n := 0; n < 127; n++ {
		parts.Messages = append(parts.Messages,
			Message{Role: "assistant", Parts: []MessagePart{{ToolCall: &ToolCallPart{ID: "provider-local-id", Name: "original_tool", Arguments: json.RawMessage(`{}`)}}}},
			Message{Role: "tool", Parts: []MessagePart{{ToolResult: &ToolResultPart{CallID: "provider-local-id", Parts: []ResultPart{{Text: &TextPart{Text: fmt.Sprintf("result-%d", n)}}}}}}})
	}
	for n := 1; n < 128; n++ {
		parts.Tools = append(parts.Tools, Tool{Name: fmt.Sprintf("frozen_tool_%d", n), InputSchema: json.RawMessage(`{}`)})
	}
	complete, err := NewSuccessorSemantic(context.Background(), parts)
	if err != nil {
		t.Fatal(err)
	}
	if len(complete.Parts().Messages) != 256 || len(complete.Parts().Tools) != 128 {
		t.Fatal("complete history or tools cropped")
	}
	decoded, err := DecodeSuccessorSemantic(context.Background(), complete.CanonicalBytes())
	if err != nil || decoded.Digest() != complete.Digest() {
		t.Fatal("full history did not roundtrip", err)
	}
	parts.Messages = append(parts.Messages, Message{Role: "user", Parts: []MessagePart{{Text: &TextPart{Text: "one too many"}}}})
	if _, err := NewSuccessorSemantic(context.Background(), parts); err == nil {
		t.Fatal("oversize history cropped")
	}
}

func TestSuccessorSemanticRejectsUnpairedResults(t *testing.T) {
	for _, mode := range []string{"missing", "foreign", "duplicate", "crossed", "tool-text"} {
		t.Run(mode, func(t *testing.T) {
			_, parts := successorPartsFixture(t)
			switch mode {
			case "missing":
				parts.Messages = append(parts.Messages[:3], parts.Messages[4:]...)
			case "foreign":
				parts.Messages[3].Parts[0].ToolResult.CallID = "foreign"
			case "duplicate":
				parts.Messages[3].Parts = append(parts.Messages[3].Parts, parts.Messages[3].Parts[0].Clone())
			case "crossed":
				parts.Messages[3], parts.Messages[4] = parts.Messages[4], parts.Messages[3]
			case "tool-text":
				parts.Messages[3].Parts[0] = MessagePart{Text: &TextPart{Text: "unbound result"}}
			}
			if _, err := NewSuccessorSemantic(context.Background(), parts); err == nil {
				t.Fatal("history accepted without exact call/result pairing")
			}
		})
	}
}
