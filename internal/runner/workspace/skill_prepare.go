package workspace

import (
	"context"
	"errors"
	"github.com/LunaDeerTech/agenteam/internal/runner/filesystem"
)

type skillPreparerRoot interface {
	PrepareSkill(context.Context, filesystem.SkillPrepareRequest) (filesystem.SkillPrepareResult, error)
}

func (h *Handle) PrepareSkill(ctx context.Context, request filesystem.SkillPrepareRequest) (filesystem.SkillPrepareResult, error) {
	result := filesystem.SkillPrepareResult{State: filesystem.WriteNotCommitted}
	if h == nil || h.manager == nil {
		return result, ErrClosed
	}
	m := h.manager
	op, err := m.begin(ctx, h)
	if err != nil {
		return result, err
	}
	if root, ok := h.root.(skillPreparerRoot); ok {
		result, err = root.PrepareSkill(op.ctx, request)
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
