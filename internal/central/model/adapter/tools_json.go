package adapter

import (
	"context"
	"encoding/json"
	"strings"

	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

// toolPolicy retains only the frozen response allow-list, never a caller-owned
// schema or slice. It supplies no Tool execution or current authorization.
type toolPolicy struct {
	names  map[string]bool
	choice mc.ToolChoice
}

func newToolPolicy(r Request) *toolPolicy {
	p := &toolPolicy{names: make(map[string]bool, len(r.Tools)), choice: r.ToolChoice}
	for _, t := range r.Tools {
		p.names[t.Name] = true
	}
	return p
}
func nativeToolName(name string) bool {
	if len(name) == 0 || len(name) > 64 {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

type functionWire struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
	Strict      bool            `json:"strict"`
}
type toolWire struct {
	Type     string       `json:"type"`
	Function functionWire `json:"function"`
}
type callFunctionWire struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}
type callWire struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function callFunctionWire `json:"function"`
}
type toolsMessageWire struct {
	Role       string     `json:"role"`
	Content    any        `json:"content"`
	ToolCalls  []callWire `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}
type resultTextWire struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func prepareToolsBody(ctx context.Context, r Request) ([]byte, error) {
	if ctx == nil {
		return nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, contextFailure(err)
	}
	s, c := r.Snapshot.Identity, r.Snapshot.Capabilities
	if s.Profile != mc.OpenAIChatV1 || s.Protocol != mc.OpenAIChat || s.ModelType != mc.ChatModel || !emptyObject(r.Snapshot.Parameters) || !emptyObject(r.Snapshot.RequestOverwrite) || len(r.Snapshot.HeaderOverwrite) != 0 || !c.ToolCalls || c.ParallelToolCalls || c.Reasoning || len(c.ReasoningEfforts) != 0 || !onlyText(c.InputModalities) || !onlyText(c.OutputModalities) || len(c.StructuredOutputModes) > 0 && !onlyText(c.StructuredOutputModes) || r.ResponseFormat.Kind != "text" || r.Mode != JSONResponse {
		return nil, unsupported()
	}
	// Charge the entire owned encoding before Marshal expands text or schemas.
	// Fixed overhead is a conservative bound; final bytes retain the same 16 MiB cap.
	size := 512 + quotedSize(s.ProviderModelID)
	charge := func(n int) error {
		if n > maxTextBytes-size {
			return limitFailure()
		}
		size += n
		return nil
	}
	tools := make([]toolWire, 0, len(r.Tools))
	names := map[string]bool{}
	for _, t := range r.Tools {
		if !nativeToolName(t.Name) || names[t.Name] || jsonShape(t.InputSchema) != nil {
			return nil, invalid()
		}
		names[t.Name] = true
		if err := charge(128 + quotedSize(t.Name) + quotedSize(t.Description) + quotedSize(string(t.InputSchema))); err != nil {
			return nil, err
		}
		tools = append(tools, toolWire{"function", functionWire{t.Name, t.Description, t.InputSchema, false}})
	}
	var choice any
	var parallel *bool
	if len(tools) == 0 {
		if r.ToolChoice.Kind != "none" && r.ToolChoice.Kind != "auto" {
			return nil, invalid()
		}
	} else {
		parallel = new(bool)
		choice = r.ToolChoice.Kind
		if r.ToolChoice.Kind == "named" {
			if !names[r.ToolChoice.Name] {
				return nil, invalid()
			}
			choice = struct {
				Type     string `json:"type"`
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			}{Type: "function", Function: struct {
				Name string `json:"name"`
			}{r.ToolChoice.Name}}
		}
	}
	messages := make([]toolsMessageWire, 0, len(r.Messages))
	pending := map[string]bool{}
	parts := 0
	for _, m := range r.Messages {
		if err := ctx.Err(); err != nil {
			return nil, contextFailure(err)
		}
		parts += len(m.Parts)
		if parts > 256 {
			return nil, limitFailure()
		}
		if m.Role != "tool" && len(pending) > 0 {
			return nil, invalid()
		}
		switch m.Role {
		case "system", "user":
			if len(m.Parts) != 1 || m.Parts[0].Text == nil {
				return nil, unsupported()
			}
			text := m.Parts[0].Text.Text
			if err := charge(64 + quotedSize(text)); err != nil {
				return nil, err
			}
			messages = append(messages, toolsMessageWire{Role: m.Role, Content: text})
		case "assistant":
			item := toolsMessageWire{Role: m.Role}
			textSeen := false
			for _, part := range m.Parts {
				if part.Text != nil {
					if textSeen {
						return nil, unsupported()
					}
					textSeen = true
					if err := charge(quotedSize(part.Text.Text)); err != nil {
						return nil, err
					}
					item.Content = part.Text.Text
				} else if part.ToolCall != nil {
					call := part.ToolCall
					if !nativeToolName(call.Name) || strings.ContainsRune(call.ID, 0) || pending[call.ID] || jsonShape(call.Arguments) != nil {
						return nil, invalid()
					}
					if err := charge(128 + quotedSize(call.ID) + quotedSize(call.Name) + quotedSize(string(call.Arguments))); err != nil {
						return nil, err
					}
					pending[call.ID] = true
					item.ToolCalls = append(item.ToolCalls, callWire{call.ID, "function", callFunctionWire{call.Name, string(call.Arguments)}})
				} else {
					return nil, unsupported()
				}
			}
			if err := charge(64); err != nil {
				return nil, err
			}
			messages = append(messages, item)
		case "tool":
			for _, part := range m.Parts {
				if part.ToolResult == nil {
					return nil, unsupported()
				}
				result := part.ToolResult
				if !pending[result.CallID] {
					return nil, invalid()
				}
				delete(pending, result.CallID)
				if err := charge(96 + quotedSize(result.CallID)); err != nil {
					return nil, err
				}
				var content any = ""
				if len(result.Parts) > 0 {
					texts := make([]resultTextWire, 0, len(result.Parts))
					for _, p := range result.Parts {
						if p.Text == nil {
							return nil, unsupported()
						}
						if err := charge(32 + quotedSize(p.Text.Text)); err != nil {
							return nil, err
						}
						texts = append(texts, resultTextWire{"text", p.Text.Text})
					}
					content = texts
				}
				messages = append(messages, toolsMessageWire{Role: "tool", Content: content, ToolCallID: result.CallID})
			}
		default:
			return nil, unsupported()
		}
		if len(messages) > 256 {
			return nil, limitFailure()
		}
	}
	if len(pending) > 0 {
		return nil, invalid()
	}
	var maxOutput *int64
	if c.MaxOutput != nil {
		v := int64(*c.MaxOutput)
		maxOutput = &v
	}
	body := struct {
		Model     string             `json:"model"`
		Messages  []toolsMessageWire `json:"messages"`
		Stream    bool               `json:"stream"`
		Tools     []toolWire         `json:"tools,omitempty"`
		Choice    any                `json:"tool_choice,omitempty"`
		Parallel  *bool              `json:"parallel_tool_calls,omitempty"`
		MaxOutput *int64             `json:"max_completion_tokens,omitempty"`
	}{s.ProviderModelID, messages, false, tools, choice, parallel, maxOutput}
	raw, err := json.Marshal(body)
	if err != nil || len(raw) > maxTextBytes {
		return nil, limitFailure()
	}
	if err := ctx.Err(); err != nil {
		clear(raw)
		return nil, contextFailure(err)
	}
	return raw, nil
}

func (p *toolPolicy) decode(raw json.RawMessage, finish mc.FinishReason) ([]mc.ToolCallPart, error) {
	var items []json.RawMessage
	if !null(raw) && json.Unmarshal(raw, &items) != nil {
		return nil, protocolFailure()
	}
	// M1 advertises parallel=false; one completion cannot authorize a batch.
	if len(items) > 1 {
		return nil, protocolFailure()
	}
	if len(items) == 0 {
		if finish == "tool_calls" || p.choice.Kind == "required" || p.choice.Kind == "named" {
			return nil, protocolFailure()
		}
		return nil, nil
	}
	if finish == "content_filter" {
		return nil, failure("content_filter", "")
	}
	if finish != "tool_calls" || p.choice.Kind == "none" {
		return nil, protocolFailure()
	}
	calls := make([]mc.ToolCallPart, 0, len(items))
	seen := map[string]bool{}
	for _, raw := range items {
		item, err := object(raw, "id type function")
		if err != nil {
			return nil, err
		}
		kind, err := nativeString(item["type"], 32, true)
		if err != nil {
			return nil, err
		}
		if kind != "function" {
			return nil, unsupported()
		}
		callID, err := nativeString(item["id"], 256, true)
		if err != nil {
			return nil, err
		}
		fn, err := object(item["function"], "name arguments")
		if err != nil {
			return nil, err
		}
		name, err := nativeString(fn["name"], 64, true)
		if err != nil {
			return nil, err
		}
		arguments, err := nativeString(fn["arguments"], 1<<20, true)
		if err != nil {
			return nil, err
		}
		call := mc.ToolCallPart{ID: callID, Name: name, Arguments: json.RawMessage(arguments)}
		if call.Validate() != nil || jsonShape(call.Arguments) != nil || strings.ContainsRune(callID, 0) || !nativeToolName(name) || !p.names[name] || seen[callID] || p.choice.Kind == "named" && p.choice.Name != name {
			return nil, protocolFailure()
		}
		seen[callID] = true
		calls = append(calls, call)
	}
	return calls, nil
}
func cloneToolCalls(calls []mc.ToolCallPart) []mc.ToolCallPart {
	if calls == nil {
		return nil
	}
	out := make([]mc.ToolCallPart, len(calls))
	for i, c := range calls {
		out[i] = c.Clone()
	}
	return out
}
