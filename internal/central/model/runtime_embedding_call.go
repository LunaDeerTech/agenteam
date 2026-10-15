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
type runtimeEmbeddingHandle struct {
	call              *runtimeCall
	mu                sync.Mutex
	started, returned bool
	err               error
}

func (r *Runtime) BeginEmbed(ctx context.Context, request mc.EmbeddingRequest) (result mc.EmbeddingCall, err error) {
	ctx, usage := r.usageOuter(ctx)
	defer func() { err = usage.leave(err) }()
	call, err := r.beginCall(ctx, mc.ModelRequest{}, wire.JSONResponse, &request)
	if call == nil {
		return nil, err
	}
	handle := &runtimeEmbeddingHandle{call: call, started: err != nil, returned: err != nil, err: err}
	return handle, err
}

func (h *runtimeEmbeddingHandle) Result(ctx context.Context) (response mc.Embeddings, err error) {
	if err := runtimeContextError(ctx); err != nil {
		return mc.Embeddings{}, err
	}
	if h == nil || h.call == nil {
		return mc.Embeddings{}, fault(f.DependencyUnbound)
	}
	ctx, usage, err := h.call.borrowUsage(ctx)
	if err != nil {
		return mc.Embeddings{}, err
	}
	defer func() {
		original := err
		err = usage.leave(err)
		if original == nil && err != nil {
			h.mu.Lock()
			if h.returned && h.err == nil {
				h.err = err
			}
			h.mu.Unlock()
		}
	}()
	h.mu.Lock()
	started, returned, original := h.started, h.returned, h.err
	if !started {
		h.started = true
	}
	h.mu.Unlock()
	if started {
		if !returned {
			return mc.Embeddings{}, fault(f.ResourceBusy)
		}
		// Repeated Result is observation only. Close owns any original Unknown
		// confirmation and never opens a replacement wire call.
		if err := h.call.enter(ctx, false); err != nil {
			return mc.Embeddings{}, err
		}
		defer h.call.leave()
		if h.Joined() && h.call.embeddingResult != nil && h.call.copyRecord().value.Final != nil && h.call.copyRecord().value.Final.Status == uc.Succeeded {
			return h.call.embeddingResult.Clone(), nil
		}
		if original != nil {
			return mc.Embeddings{}, original
		}
		return mc.Embeddings{}, fault(f.InvalidState)
	}
	// Result may have a shorter cancellation budget than BeginEmbed. Preserve
	// the original context values and make cancellation stop that same call.
	stop := context.AfterFunc(ctx, h.call.cancel)
	response, err = h.call.resultEmbedding(ctx)
	stop()
	h.mu.Lock()
	h.returned, h.err = true, err
	h.mu.Unlock()
	return response, err
}

func (h *runtimeEmbeddingHandle) Close(ctx context.Context) error {
	if h == nil || h.call == nil {
		return fault(f.DependencyUnbound)
	}
	return h.call.close(ctx)
}

func (h *runtimeEmbeddingHandle) Joined() bool {
	if h == nil || h.call == nil {
		return false
	}
	h.call.mu.Lock()
	joined := h.call.joined
	h.call.mu.Unlock()
	return joined && h.call.usageJoined()
}

func (*runtimeEmbeddingHandle) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "model_embedding_call")
}
func (*runtimeEmbeddingHandle) MarshalJSON() ([]byte, error) {
	return []byte(`"model_embedding_call"`), nil
}
func (*runtimeEmbeddingHandle) LogValue() slog.Value { return slog.StringValue("model_embedding_call") }

var _ mc.EmbeddingCaller = (*Runtime)(nil)
var _ mc.EmbeddingCall = (*runtimeEmbeddingHandle)(nil)
