package execution

import (
	"context"
	"slices"
	"sync"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

type skillToolInputKey struct{}

// This loan contains immutable round facts, not a current-authority cache. It
// exists only inside the original, fully validated Tool input callback. Each
// Object operation still enters that full callback independently.
type skillToolInputLoan struct {
	mu       sync.Mutex
	driver   *ToolCallDriver
	store    Store
	tx       f.Tx
	plan     *toolCallPlan
	binding  tc.ToolCallBinding
	current  *toolCurrentWitness
	ctx      context.Context
	bindings []sc.SkillBinding
	active   bool
	used     bool
}

func (d *ToolCallDriver) withSkillToolInputInTx(ctx context.Context, tx f.Tx, b tc.ToolCallBinding, plan tc.ToolCallPlan, consume func(context.Context, tc.ToolCallBinding, []byte) error) error {
	// The original path verifies the guard, current Project/Agent/Execution,
	// complete lock union, captured tool and protected argument bytes first.
	return d.withToolCallInTx(ctx, tx, b, plan, func(current context.Context, actual tc.ToolCallBinding, raw []byte) error {
		p, ok := plan.(*toolCallPlan)
		if !ok || p == nil || p.owner != d || !sameToolCallBinding(p.binding, actual) {
			return fault(f.Forbidden)
		}
		x, err := d.driver.state.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		stored, err := loadDirectTextRound(current, x, actual.ExecutionID, actual.RoundID)
		if err != nil {
			return err
		}
		if stored == nil || stored.snapshot.Digest() != p.snapshot || stored.round.Digest() != p.round {
			return fault(f.ConfirmationStale)
		}
		bindings, _, err := loadSkillBindingsForRound(current, x, stored)
		if err != nil {
			return err
		}
		return d.withSkillToolInputLoan(current, tx, p, actual, bindings, func(loan context.Context) error {
			return consume(loan, actual, raw)
		})
	})
}

func (d *ToolCallDriver) withSkillToolInputLoan(ctx context.Context, tx f.Tx, p *toolCallPlan, b tc.ToolCallBinding, bindings []sc.SkillBinding, consume func(context.Context) error) error {
	if d == nil || d.driver == nil || ctx == nil || consume == nil || b.Validate() != nil || b.Binding != (tc.BuiltinBinding{HandlerID: "skill.prepare", ContractRevision: 1}) || p == nil || p.owner != d || !sameToolCallBinding(p.binding, b) || bindings == nil {
		return fault(f.Forbidden)
	}
	w, _ := ctx.Value(toolCurrentKey{}).(*toolCurrentWitness)
	request := ac.ExecutionConfigurationRequest{Actor: b.Actor, ProjectID: b.ProjectID, AgentID: b.AgentID, ExecutionID: b.ExecutionID, Stage: ac.ExecutionConfigurationCurrent}
	if w == nil || w.driver != d || w.tx != tx || !w.active.Load() || !sameToolCurrentRequest(w.request, request) {
		return fault(f.Forbidden)
	}
	for n, binding := range bindings {
		if binding.Validate() != nil || n > 0 && bindings[n-1].SkillID.String() >= binding.SkillID.String() {
			return fault(f.InvalidState)
		}
	}
	s := d.driver.state.store
	if _, err := s.InTx(tx); err != nil {
		return portError(err)
	}
	if err := s.RequireHeldLocks(ctx, tx, p.locks); err != nil {
		return portError(err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	loan := &skillToolInputLoan{driver: d, store: s, tx: tx, plan: p, binding: b, current: w, ctx: ctx, bindings: slices.Clone(bindings), active: true}
	defer func() {
		loan.mu.Lock()
		loan.active = false
		clear(loan.bindings)
		loan.bindings = nil
		loan.mu.Unlock()
	}()
	if err := consume(context.WithValue(ctx, skillToolInputKey{}, loan)); err != nil {
		return err
	}
	return ctx.Err()
}

func (d *ToolCallDriver) readSkillToolInputLoan(ctx context.Context, tx f.Tx, b tc.ToolCallBinding, plan tc.ToolCallPlan) ([]sc.SkillBinding, bool, error) {
	if ctx == nil {
		return nil, false, nil
	}
	value := ctx.Value(skillToolInputKey{})
	if value == nil {
		return nil, false, nil
	}
	loan, ok := value.(*skillToolInputLoan)
	p, planOK := plan.(*toolCallPlan)
	if !ok || loan == nil || !planOK || p == nil || d == nil || d.driver == nil || loan.driver != d || loan.store != d.driver.state.store || loan.tx != tx || loan.plan != p || p.owner != d || !sameToolCallBinding(loan.binding, b) || !sameToolCallBinding(p.binding, b) {
		return nil, true, fault(f.Forbidden)
	}
	if _, err := loan.store.InTx(tx); err != nil {
		return nil, true, portError(err)
	}
	if err := loan.store.RequireHeldLocks(ctx, tx, p.locks); err != nil {
		return nil, true, portError(err)
	}
	loan.mu.Lock()
	defer loan.mu.Unlock()
	current, _ := ctx.Value(toolCurrentKey{}).(*toolCurrentWitness)
	if !loan.active || loan.used || current != loan.current || current == nil || !current.active.Load() {
		return nil, true, fault(f.Forbidden)
	}
	if err := loan.ctx.Err(); err != nil {
		return nil, true, err
	}
	if err := ctx.Err(); err != nil {
		return nil, true, err
	}
	loan.used = true
	return slices.Clone(loan.bindings), true, nil
}
