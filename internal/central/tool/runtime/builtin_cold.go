package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"sync/atomic"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// This proof belongs to one synchronous, registered Lookup. It is issued only
// after the exact prior process claim is confirmed stopped. It is neither a
// replacement local Execute nor permission to change the persisted process.
type builtinColdProof struct {
	owner   *BuiltinExecutor
	call    *builtinCall
	binding tc.ToolCallBinding
	record  builtinRecord
	process oc.ProcessID
	active  atomic.Bool
}

func coldBuiltinSupported(call *builtinCall) bool {
	return call != nil && call.lookup && call.binding.Validate() == nil &&
		(call.binding.Binding == (tc.BuiltinBinding{HandlerID: "memory.retain", ContractRevision: 1}) ||
			call.binding.Binding == (tc.BuiltinBinding{HandlerID: "skill.prepare", ContractRevision: 1})) &&
		call.binding.RunnerBinding == (tc.RunnerBinding{}) && call.binding.MCPBinding == (tc.MCPBinding{})
}

func sameColdRecord(a, b builtinRecord) bool {
	if a.validate() != nil || b.validate() != nil || a.ID != b.ID || a.Attempt != b.Attempt || a.Request != b.Request || a.InputDigest != b.InputDigest || a.Fingerprint != b.Fingerprint || a.Phase != b.Phase || a.Version != b.Version {
		return false
	}
	left, e1 := a.Input.request()
	right, e2 := b.Input.request()
	return e1 == nil && e2 == nil && sameRunnerCallBinding(left, right)
}

func (p *builtinColdProof) permits(current oc.ProcessID, record builtinRecord) bool {
	if p == nil || !p.active.Load() || p.owner == nil || p.owner.authority == nil || p.owner.authority.process != current || current.Validate() != nil || p.process.Validate() != nil || p.process == current ||
		!coldBuiltinSupported(p.call) || p.call.cold != p || !sameRunnerCallBinding(p.binding, p.call.binding) || !sameColdRecord(p.record, record) || !sameColdRecord(p.record, p.call.record) {
		return false
	}
	p.owner.mu.Lock()
	defer p.owner.mu.Unlock()
	_, admitted := p.owner.lookups[p.call]
	return admitted && !p.owner.stopped && p.owner.calls[p.binding.Identity()] == nil
}

func (e *BuiltinExecutor) resolveCold(ctx context.Context, call *builtinCall) error {
	// Other backends require their own remote-effect/recovery contracts. In
	// particular MCP and install do not inherit a Memory or prepare receipt.
	if !coldBuiltinSupported(call) {
		return nil
	}
	if e == nil || e.authority == nil || e.authority.guard == nil {
		return fail(f.DependencyUnbound)
	}
	e.mu.Lock()
	_, admitted := e.lookups[call]
	available := admitted && !e.stopped && e.calls[call.binding.Identity()] == nil
	e.mu.Unlock()
	if !available {
		return fail(f.ResourceBusy)
	}
	var prior oc.ProcessID
	// Only immutable original input and the exact running attempt may be read
	// before the physical stopped-process proof. No domain call happens here.
	err := e.transaction(ctx, call, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		return e.input(ctx, tx, call, func(current context.Context, raw []byte) error {
			stored, err := loadBuiltinRecord(current, x, call.binding)
			if err != nil {
				return err
			}
			if stored == nil || !sameColdRecord(*stored, call.record) {
				return fail(f.ConfirmationStale)
			}
			prior, err = readBuiltinAttemptProcess(current, x, *stored)
			if err != nil {
				return err
			}
			call.arguments = bytes.Clone(raw)
			return nil
		})
	})
	if err != nil {
		return err
	}
	if prior == e.authority.process {
		return fail(f.ResourceBusy)
	}
	// ConfirmStopped reads the exact claim and verifies its flock. Keep this
	// outside the Store transaction, then recheck all immutable facts under
	// the complete original lock union before lending the lookup witness.
	if err = e.authority.guard.ConfirmStopped(ctx, prior); err != nil {
		return portError(err)
	}
	proof := &builtinColdProof{owner: e, call: call, binding: call.binding, record: call.record, process: prior}
	proof.active.Store(true)
	call.cold = proof
	defer func() { proof.active.Store(false); call.cold = nil }()
	err = e.transaction(ctx, call, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		return e.input(ctx, tx, call, func(current context.Context, raw []byte) error {
			stored, err := loadBuiltinRecord(current, x, call.binding)
			if err != nil {
				return err
			}
			if stored == nil || !sameColdRecord(*stored, proof.record) || !bytes.Equal(raw, call.arguments) {
				return fail(f.ConfirmationStale)
			}
			return requireBuiltinAttempt(current, x, e.authority.process, *stored, proof)
		})
	})
	if err != nil {
		return err
	}
	outcome, err := e.invokeCanonicalLookup(ctx, call, e.backendFor(call))
	if err != nil {
		return err
	}
	if !outcome.Found {
		return fail(f.ResourceBusy)
	}
	body := outcome.Body
	defer func() { clear(body) }()
	terminalOutcome, code := "success", ""
	if outcome.Failure != "" {
		terminalOutcome, code = "failure", string(outcome.Failure)
		body, _ = json.Marshal(map[string]any{"error": map[string]string{"code": code}})
	} else {
		terminalOutcome, code, err = e.backendTerminal(call, body)
		if err != nil {
			return err
		}
	}
	if len(body) < 2 || len(body) > 1<<20 || !json.Valid(body) {
		return fail(f.DependencyUnavailable)
	}
	call.terminal = &builtinTerminal{Format: 1, Operation: call.record.ID, Attempt: call.record.Attempt, Request: call.record.Request, Outcome: terminalOutcome, Code: code, ResultDigest: digest(body), ResultBytes: f.Progress(len(body))}
	call.body = bytes.Clone(body)
	// Unknown remains Unknown. A later Lookup observes this same original
	// target; it cannot assume publication or repeat the Memory mutation.
	return e.publish(ctx, call)
}
