package runtime

import (
	"context"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

func (e *BuiltinExecutor) beginLookup(ctx context.Context, b tc.ToolCallBinding) (*builtinCall, func(), error) {
	if err := contextError(ctx); err != nil {
		return nil, nil, err
	}
	if e == nil || b.Validate() != nil || !tc.WorkArtifactBuiltinBinding(b.Binding) {
		return nil, nil, fail(f.InvalidArgument)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopped {
		return nil, nil, fail(f.ShuttingDown)
	}
	run, cancel := context.WithCancel(ctx)
	call := &builtinCall{binding: b, ctx: run, cancel: cancel, done: make(chan struct{}), lookup: true}
	e.lookups[call] = struct{}{}
	return call, func() {
		cancel()
		clear(call.arguments)
		clear(call.body)
		e.mu.Lock()
		delete(e.lookups, call)
		close(call.done)
		close(e.changed)
		e.changed = make(chan struct{})
		e.mu.Unlock()
	}, nil
}

// Lookup never invokes a domain mutation and cannot admit an absent or
// uncertain call. Only a published, validated terminal retires a retained
// original owner; observing a running row leaves that owner outstanding.
func (e *BuiltinExecutor) Lookup(ctx context.Context, b tc.ToolCallBinding) (tc.RunnerOutcome, bool, error) {
	call, done, err := e.beginLookup(ctx, b)
	if err != nil {
		return tc.RunnerOutcome{}, false, err
	}
	defer done()
	ctx = call.ctx
	if err = e.discover(ctx, call); err != nil {
		return tc.RunnerOutcome{}, false, err
	}
	var found *builtinRecord
	err = e.transaction(ctx, call, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		return e.input(ctx, tx, call, func(current context.Context, _ []byte) error {
			var err error
			found, err = loadBuiltinRecord(current, x, b)
			return err
		})
	})
	if err != nil || found == nil {
		return tc.RunnerOutcome{}, false, err
	}
	call.record = *found
	lock, err := builtinOperationLock(found.ID)
	if err != nil {
		return tc.RunnerOutcome{}, true, err
	}
	call.locks, err = unionLocks(append(call.locks, lock))
	if err != nil {
		return tc.RunnerOutcome{}, true, err
	}
	if found.Version == 2 {
		if err = e.resolveReturned(ctx, call); err != nil {
			return tc.RunnerOutcome{}, true, err
		}
	}
	out, err := e.observe(ctx, call)
	if err != nil {
		return tc.RunnerOutcome{}, false, err
	}
	if out.ResultAvailable {
		e.retireObserved(call)
	}
	return out, true, nil
}

func (e *BuiltinExecutor) retireObserved(observed *builtinCall) {
	e.mu.Lock()
	defer e.mu.Unlock()
	old := e.calls[observed.binding.Identity()]
	if old == nil {
		return
	}
	select {
	case <-old.done:
	default:
		return
	}
	if old.record.ID != observed.record.ID || old.record.Attempt != observed.record.Attempt || old.record.InputDigest != observed.record.InputDigest {
		return
	}
	// observe() validated the exact immutable terminal. A retained start
	// without an actual domain return cannot be discharged by another actor.
	if old.terminal == nil || observed.terminal == nil || *old.terminal != *observed.terminal {
		return
	}
	clear(old.arguments)
	clear(old.body)
	old.retirement = nil
	delete(e.calls, observed.binding.Identity())
	close(e.changed)
	e.changed = make(chan struct{})
}

type builtinResultPlan struct {
	owner  *BuiltinExecutor
	call   tc.ToolCallBinding
	input  tc.ToolCallPlan
	locks  []f.LockRequest
	result tc.RunnerResultBinding
}

func (p *builtinResultPlan) RequiredLocks() []f.LockRequest {
	if p == nil {
		return nil
	}
	return slices.Clone(p.locks)
}
func (p *builtinResultPlan) Binding() tc.RunnerResultBinding {
	if p == nil {
		return tc.RunnerResultBinding{}
	}
	return p.result
}

func (e *BuiltinExecutor) DiscoverRunnerResult(ctx context.Context, b tc.ToolCallBinding) (tc.RunnerResultPlan, error) {
	call, done, err := e.beginLookup(ctx, b)
	if err != nil {
		return nil, err
	}
	defer done()
	ctx = call.ctx
	if err = e.discover(ctx, call); err != nil {
		return nil, err
	}
	var found *builtinRecord
	err = e.transaction(ctx, call, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		return e.input(ctx, tx, call, func(current context.Context, _ []byte) error {
			var err error
			found, err = loadBuiltinRecord(current, x, b)
			return err
		})
	})
	if err != nil {
		return nil, err
	}
	if found == nil || found.Version != 3 {
		return nil, fail(f.InvalidState)
	}
	call.record = *found
	lock, err := builtinOperationLock(found.ID)
	if err != nil {
		return nil, err
	}
	call.locks, err = unionLocks(append(call.locks, lock))
	if err != nil {
		return nil, err
	}
	plan := &builtinResultPlan{owner: e, call: b, input: call.plan, locks: slices.Clone(call.locks)}
	err = e.transaction(ctx, call, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		return e.input(ctx, tx, call, func(current context.Context, _ []byte) error {
			r, err := loadBuiltinRecord(current, x, b)
			if err != nil {
				return err
			}
			if r == nil || r.ID != found.ID {
				return fail(f.ConfirmationStale)
			}
			terminal, body, raw, err := loadBuiltinTerminal(current, x, *r)
			defer clear(body)
			if err != nil {
				return err
			}
			plan.result = terminal.binding(b, *r, raw)
			return plan.result.Validate()
		})
	})
	if err != nil {
		return nil, err
	}
	return plan, nil
}

func (e *BuiltinExecutor) WithRunnerResultInTx(ctx context.Context, tx f.Tx, plan tc.RunnerResultPlan, consume func(context.Context, tc.RunnerResultBinding, mc.ToolResultPart) error) error {
	p, ok := plan.(*builtinResultPlan)
	if e == nil || !ok || p == nil || p.owner != e || consume == nil || p.result.Validate() != nil {
		return fail(f.Forbidden)
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	x, err := e.authority.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if err = e.authority.store.RequireHeldLocks(ctx, tx, p.locks); err != nil {
		return portError(err)
	}
	if err = e.authority.guard.RequireCurrentProcessInTx(ctx, tx, e.authority.process); err != nil {
		return portError(err)
	}
	call := &builtinCall{binding: p.call, plan: p.input, locks: p.locks}
	return e.input(ctx, tx, call, func(current context.Context, _ []byte) error {
		r, err := loadBuiltinRecord(current, x, p.call)
		if err != nil {
			return err
		}
		if r == nil {
			return fail(f.ConfirmationStale)
		}
		terminal, body, raw, err := loadBuiltinTerminal(current, x, *r)
		defer clear(body)
		if err != nil {
			return err
		}
		binding := terminal.binding(p.call, *r, raw)
		if !sameRunnerResultBinding(binding, p.result) {
			return fail(f.ConfirmationStale)
		}
		part := mc.ToolResultPart{CallID: p.call.CallID, IsError: terminal.Outcome != "success", Parts: []mc.ResultPart{{Text: &mc.TextPart{Text: string(body)}}}}
		return consume(current, binding, part)
	})
}

var _ tc.RunnerResultAuthority = (*BuiltinExecutor)(nil)
