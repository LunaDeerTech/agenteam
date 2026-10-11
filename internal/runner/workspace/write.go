package workspace

import (
	"context"
	"errors"

	"github.com/LunaDeerTech/agenteam/internal/runner/filesystem"
)

type writerRoot interface {
	Write(context.Context, filesystem.WriteRequest) (filesystem.WriteResult, error)
}

// Write uses the same managed Root and actual-call accounting as Read. A
// publication result survives cancellation and must be inspected on error.
func (h *Handle) Write(ctx context.Context, request filesystem.WriteRequest) (filesystem.WriteResult, error) {
	result := filesystem.WriteResult{State: filesystem.WriteNotCommitted}
	if h == nil || h.manager == nil {
		return result, ErrClosed
	}
	m := h.manager
	op, err := m.begin(ctx, h)
	if err != nil {
		return result, err
	}
	if writer, ok := h.root.(writerRoot); ok {
		result, err = writer.Write(op.ctx, request)
	} else {
		err = ErrUnsupported
	}
	m.mu.Lock()
	if cancelled := contextError(op.ctx); cancelled != nil {
		err = errors.Join(err, cancelled)
	}
	m.retireLocked(op)
	m.mu.Unlock()
	return result, err
}
