package model

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	wire "github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

// A handle is created only from the original admitted runtimeCall. There is no
// public constructor or CallID lookup that can cancel another owner's work.
type runtimeJSONCall struct {
	call              *runtimeCall
	mu                sync.Mutex
	started, returned bool
	err               error
}

func (r *Runtime) BeginChat(ctx context.Context, request mc.ModelRequest) (mc.JSONCall, error) {
	call, err := r.begin(ctx, request, wire.JSONResponse)
	if call == nil {
		return nil, err
	}
	handle := &runtimeJSONCall{call: call, started: err != nil, returned: err != nil, err: err}
	return handle, err
}

func (h *runtimeJSONCall) Result(ctx context.Context) (mc.ModelResponse, error) {
	if err := runtimeContextError(ctx); err != nil {
		return mc.ModelResponse{}, err
	}
	if h == nil || h.call == nil {
		return mc.ModelResponse{}, fault(f.DependencyUnbound)
	}
	h.mu.Lock()
	started, returned, original := h.started, h.returned, h.err
	if !started {
		h.started = true
	}
	h.mu.Unlock()
	if started {
		if !returned {
			return mc.ModelResponse{}, fault(f.ResourceBusy)
		}
		// Repeated Result is observation only. Close owns any original Unknown
		// confirmation and never opens a replacement wire call.
		if err := h.call.enter(ctx, false); err != nil {
			return mc.ModelResponse{}, err
		}
		defer h.call.leave()
		if h.Joined() && h.call.result != nil && h.call.copyRecord().value.Final != nil && h.call.copyRecord().value.Final.Status == uc.Succeeded {
			return h.call.result.Clone(), nil
		}
		if original != nil {
			return mc.ModelResponse{}, original
		}
		return mc.ModelResponse{}, fault(f.InvalidState)
	}
	// Result may have a shorter cancellation budget than BeginChat. Preserve
	// the original context values and make cancellation stop that same call.
	stop := context.AfterFunc(ctx, h.call.cancel)
	response, err := h.call.resultJSON(ctx)
	stop()
	h.mu.Lock()
	h.returned, h.err = true, err
	h.mu.Unlock()
	return response, err
}

func (h *runtimeJSONCall) Close(ctx context.Context) error {
	if h == nil || h.call == nil {
		return fault(f.DependencyUnbound)
	}
	return h.call.close(ctx)
}

func (h *runtimeJSONCall) Joined() bool {
	if h == nil || h.call == nil {
		return false
	}
	h.call.mu.Lock()
	defer h.call.mu.Unlock()
	return h.call.joined
}

func (*runtimeJSONCall) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "model_json_call") }
func (*runtimeJSONCall) MarshalJSON() ([]byte, error) { return []byte(`"model_json_call"`), nil }
func (*runtimeJSONCall) LogValue() slog.Value         { return slog.StringValue("model_json_call") }

var _ mc.JSONCaller = (*Runtime)(nil)
var _ mc.JSONCall = (*runtimeJSONCall)(nil)
