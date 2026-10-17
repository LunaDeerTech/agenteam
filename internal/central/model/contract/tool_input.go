package contract

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

const ToolCallInputSchemaVersion f.Version = 2

// ToolInputSchemaVersion identifies a full frozen ToolSet on the first round.
const ToolInputSchemaVersion f.Version = 4

// ToolCallInputDigest is the original schema-2 first-round encoding. Moving the
// sole algorithm to Model's contract allows both Execution and Runtime to bind
// exactly the same frozen input without a reverse dependency.
func ToolCallInputDigest(ctx context.Context, messages []Message, tool Tool) (f.Digest, error) {
	if ctx == nil {
		return "", bad()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if tool.Validate() != nil || len(messages) != 2 {
		return "", bad()
	}
	for n, message := range messages {
		role := "system"
		if n == 1 {
			role = "user"
		}
		if !validFirstToolMessage(message, role) {
			return "", bad()
		}
	}
	return toolInputDigest(ctx, ToolCallInputSchemaVersion, messages, []Tool{tool}, ToolChoice{Kind: "required"})
}

// ToolInputDigest binds every first-round tool without cropping the configured
// set and its required/auto choice. The owning Execution validates that choice
// against the captured source. Schema 2 retains its required single-tool bytes.
func ToolInputDigest(ctx context.Context, messages []Message, tools []Tool, choice ToolChoice) (f.Digest, error) {
	if ctx == nil {
		return "", bad()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if len(messages) != 2 || len(tools) == 0 || len(tools) > 128 || choice != (ToolChoice{Kind: "required"}) && choice != (ToolChoice{Kind: "auto"}) {
		return "", bad()
	}
	for n, message := range messages {
		role := "system"
		if n == 1 {
			role = "user"
		}
		if !validFirstToolMessage(message, role) {
			return "", bad()
		}
	}
	names := map[string]bool{}
	for _, tool := range tools {
		if tool.Validate() != nil || names[tool.Name] {
			return "", bad()
		}
		names[tool.Name] = true
	}
	return toolInputDigest(ctx, ToolInputSchemaVersion, messages, tools, choice)
}

func ValidateToolInputRequest(ctx context.Context, request ModelRequest) error {
	if ctx == nil {
		return bad()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if request.Validate() != nil || request.Input.SchemaVersion != ToolInputSchemaVersion || request.ResponseFormat.Kind != "text" || request.RetryClass != AgentRetry {
		return bad()
	}
	digest, err := ToolInputDigest(ctx, request.Messages, request.Tools, request.ToolChoice)
	if err != nil {
		return err
	}
	if digest != request.Input.Digest {
		return bad()
	}
	return nil
}

func validFirstToolMessage(message Message, role string) bool {
	if message.Validate() != nil || message.Role != role {
		return false
	}
	if role == "system" {
		return len(message.Parts) == 1 && message.Parts[0].Text != nil
	}
	mediaCount := 0
	for _, p := range message.Parts {
		if p.ToolCall != nil || p.ToolResult != nil || p.Metadata != nil {
			return false
		}
		if p.Image != nil || p.File != nil {
			mediaCount++
		}
	}
	if mediaCount == 0 {
		return len(message.Parts) == 1 && message.Parts[0].Text != nil
	}
	return mediaCount <= 4
}
func toolInputDigest(ctx context.Context, schema f.Version, messages []Message, tools []Tool, choice ToolChoice) (f.Digest, error) {
	messagesRaw, err := EncodeCanonicalMessages(messages)
	if err != nil {
		return "", err
	}
	defer clear(messagesRaw)

	value := struct {
		Schema         f.Version       `json:"schema_version"`
		Messages       json.RawMessage `json:"messages"`
		Tools          []Tool          `json:"tools"`
		ToolChoice     ToolChoice      `json:"tool_choice"`
		ResponseFormat ResponseFormat  `json:"response_format"`
	}{schema, messagesRaw, tools, choice, ResponseFormat{Kind: "text"}}
	remaining := 16 << 20
	if err := successorSemanticJSONSize(ctx, reflect.ValueOf(value), &remaining); err != nil {
		return "", err
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 16<<20 {
		return "", bad()
	}
	defer clear(raw)
	canonical, err := cursor.CanonicalJSON(raw)
	if err != nil || len(canonical) > 16<<20 {
		return "", bad()
	}
	defer clear(canonical)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return successorSemanticDigest(canonical), nil
}

// ValidateToolHistory requires every assistant tool group to be completely
// paired before another ordinary message. IDs are scoped to their group.
// It grants no schema or dispatch authority and does not mutate the history.
func ValidateToolHistory(messages []Message) error {
	pending := map[string]bool{}
	for _, m := range messages {
		if m.Validate() != nil {
			return bad()
		}
		if m.Role != "tool" && len(pending) > 0 {
			return bad()
		}
		for _, part := range m.Parts {
			if m.Role == "tool" && part.ToolResult == nil {
				return bad()
			}
			if part.ToolCall != nil {
				if m.Role != "assistant" || pending[part.ToolCall.ID] {
					return bad()
				}
				pending[part.ToolCall.ID] = true
			}
			if part.ToolResult != nil {
				if m.Role != "tool" || !pending[part.ToolResult.CallID] {
					return bad()
				}
				delete(pending, part.ToolResult.CallID)
			}
		}
	}
	if len(pending) > 0 {
		return bad()
	}
	return nil
}
