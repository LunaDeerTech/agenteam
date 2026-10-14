package approval

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/governance/approval/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"slices"
)

type rejectedPlan struct {
	owner  *Service
	record *record
	locks  []f.LockRequest
}

func (p *rejectedPlan) RequiredLocks() []f.LockRequest {
	if p == nil {
		return nil
	}
	return slices.Clone(p.locks)
}
func (p *rejectedPlan) Waiting() c.Waiting {
	if p == nil {
		return c.Waiting{}
	}
	return p.record.value.Waiting()
}
func (p *rejectedPlan) Binding() tc.ToolCallBinding {
	if p == nil {
		return tc.ToolCallBinding{}
	}
	return p.record.operation.Binding
}
func (s *Service) DiscoverRejectedResult(ctx context.Context, b tc.ToolCallBinding, reference string) (c.RejectedResultPlan, error) {
	id, err := f.ParseID[c.RequestIdentity](reference)
	if s == nil || ctx == nil || b.Validate() != nil || err != nil {
		return nil, fault(f.InvalidArgument)
	}
	r, err := loadByID(ctx, s.store, id)
	if err != nil {
		return nil, err
	}
	if r == nil || r.value.Status != c.Rejected || !sameBinding(r.operation.Binding, b) {
		return nil, fault(f.ConfirmationStale)
	}
	locks, err := s.operationLocks(ctx, r.operation)
	if err != nil {
		return nil, err
	}
	return &rejectedPlan{s, r, locks}, nil
}
func (s *Service) WithRejectedResultInTx(ctx context.Context, tx f.Tx, plan c.RejectedResultPlan, consume func(context.Context, c.Waiting, mc.ToolResultPart) error) error {
	p, ok := plan.(*rejectedPlan)
	if s == nil || ctx == nil || consume == nil || !ok || p == nil || p.owner != s {
		return fault(f.Forbidden)
	}
	if err := s.store.RequireHeldLocks(ctx, tx, p.locks); err != nil {
		return portError(err)
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	r, err := loadByID(ctx, x, p.record.value.ID)
	if err != nil {
		return err
	}
	if r == nil || r.value.Status != c.Rejected || !sameBinding(r.operation.Binding, p.Binding()) || r.operation.OperationID != p.record.operation.OperationID {
		return fault(f.ConfirmationStale)
	}
	if err = s.deps.Executions.RequireApprovalExecutionInTx(ctx, tx, p.Binding(), r.value.ID.String()); err != nil {
		return portError(err)
	}
	if err = s.deps.Operations.RequireRejectedOperationInTx(ctx, tx, r.operation, r.value.ID.String()); err != nil {
		return portError(err)
	}
	result := mc.ToolResultPart{CallID: p.Binding().CallID, IsError: true, Parts: []mc.ResultPart{{Text: &mc.TextPart{Text: `{"code":"approval_rejected","message":"The user rejected this tool operation."}`}}}}
	if err = consume(ctx, r.value.Waiting(), result); err != nil {
		return err
	}
	return ctx.Err()
}

var _ c.RejectedResults = (*Service)(nil)
