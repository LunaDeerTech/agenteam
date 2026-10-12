package agentloop

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/schema"
)

type SuccessorResponseKind string

const (
	SuccessorText        SuccessorResponseKind = "text"
	SuccessorToolCall    SuccessorResponseKind = "tool_call"
	SuccessorUnsupported SuccessorResponseKind = "unsupported"
	SuccessorInterrupted SuccessorResponseKind = "interrupted"
	SuccessorCancelled   SuccessorResponseKind = "cancelled"
	SuccessorError       SuccessorResponseKind = "error"
	SuccessorUnknown     SuccessorResponseKind = "unknown"
)

// SuccessorResponse is a pure classification, not proof of an actual Invoke,
// committed response, current permission, or authority to dispatch a Tool.
// Only complete text/single-call classifications retain a response candidate.
type SuccessorResponse struct{ data *successorResponseData }
type successorResponseData struct {
	kind       SuccessorResponseKind
	finish     mc.FinishReason
	call       mc.CallID
	invocation mc.InvocationID
	usage      mc.Usage
	response   *mc.ModelResponse
}

// ValidateSuccessorResponse accepts the explicit auto/text/AgentRetry profile.
// The shared Model contract owns the original schema-3 semantic digest. This
// function grants no Runtime capability and does not change required sessions.
// InvocationID is checked for C0 shape only; its actual owner remains a later
// invocation boundary. All schema validation is synchronous and in-memory.
func ValidateSuccessorResponse(ctx context.Context, request mc.ModelRequest, response mc.ModelResponse) (SuccessorResponse, error) {
	var zero SuccessorResponse
	if _, err := mc.ValidateSuccessorRequest(ctx, request); err != nil {
		return zero, err
	}
	capabilities := request.Model.Snapshot.Capabilities
	if len(request.Tools) != 1 || !capabilities.ToolCalls || capabilities.ParallelToolCalls {
		return zero, unsupported()
	}
	if response.Validate() != nil || response.CallID != request.CallID {
		return zero, invalid()
	}
	var kind SuccessorResponseKind
	switch response.FinishReason {
	case "stop":
		for _, part := range response.Message.Parts {
			if part.Text == nil {
				return zero, invalid()
			}
		}
		kind = SuccessorText
	case "tool_calls":
		count := 0
		var call *mc.ToolCallPart
		for _, part := range response.Message.Parts {
			if part.ToolCall != nil {
				count++
				call = part.ToolCall
			} else if part.Text == nil {
				return zero, invalid()
			}
		}
		switch {
		case count == 0:
			return zero, invalid()
		case count > 1:
			// No first-call fallback, even when every call is well formed.
			kind = SuccessorUnsupported
		default:
			if call.Name != request.Tools[0].Name {
				return zero, invalid()
			}
			compiled, err := schema.Compile(ctx, request.Tools[0].InputSchema)
			if err != nil {
				return zero, err
			}
			if err = compiled.Validate(ctx, call.Arguments); err != nil {
				return zero, err
			}
			kind = SuccessorToolCall
		}
	case "length", "content_filter":
		kind = SuccessorInterrupted
	case "cancelled":
		kind = SuccessorCancelled
	case "error":
		kind = SuccessorError
	case "unknown":
		kind = SuccessorUnknown
	default:
		return zero, invalid()
	}
	data := &successorResponseData{kind: kind, finish: response.FinishReason, call: response.CallID, invocation: response.InvocationID, usage: response.Usage.Clone()}
	if kind == SuccessorText || kind == SuccessorToolCall {
		owned := response.Clone()
		data.response = &owned
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return SuccessorResponse{data}, nil
}

func (v SuccessorResponse) Validate() error {
	if v.data == nil {
		return invalid()
	}
	return nil
}
func (v SuccessorResponse) Kind() SuccessorResponseKind {
	if v.data == nil {
		return ""
	}
	return v.data.kind
}
func (v SuccessorResponse) FinishReason() mc.FinishReason {
	if v.data == nil {
		return ""
	}
	return v.data.finish
}
func (v SuccessorResponse) CallID() mc.CallID {
	if v.data == nil {
		return mc.CallID{}
	}
	return v.data.call
}
func (v SuccessorResponse) InvocationID() mc.InvocationID {
	if v.data == nil {
		return mc.InvocationID{}
	}
	return v.data.invocation
}
func (v SuccessorResponse) Usage() mc.Usage {
	if v.data == nil {
		return mc.Usage{}
	}
	return v.data.usage.Clone()
}

// Response returns an owned copy only for a complete text or single-call
// candidate. It does not turn the candidate into an execution capability.
func (v SuccessorResponse) Response() (mc.ModelResponse, bool) {
	if v.data == nil || v.data.response == nil {
		return mc.ModelResponse{}, false
	}
	return v.data.response.Clone(), true
}
func (SuccessorResponse) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "agent_successor_response")
}
func (SuccessorResponse) LogValue() slog.Value { return slog.StringValue("agent_successor_response") }
func (SuccessorResponse) MarshalJSON() ([]byte, error) {
	return []byte(`"agent_successor_response"`), nil
}
func (*SuccessorResponse) UnmarshalJSON([]byte) error { return invalid() }
