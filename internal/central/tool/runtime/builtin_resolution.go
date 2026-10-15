package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// A private context is valid only during this original synchronous invocation.
// Even a panic retires the borrow, while retaining the uncertain domain effect.
func (e *BuiltinExecutor) invokeBackend(ctx context.Context, call *builtinCall, backend BuiltinBackend, lookup bool) (body []byte, found bool, err error) {
	return e.borrowBackend(ctx, call, backend, lookup, func(domain context.Context, inv BuiltinInvocation, raw []byte) ([]byte, bool, error) {
		if lookup {
			return backend.Lookup(domain, inv, raw)
		}
		body, err := backend.Execute(domain, inv, raw)
		return body, err == nil, err
	})
}

func (e *BuiltinExecutor) borrowBackend(ctx context.Context, call *builtinCall, backend BuiltinBackend, lookup bool, invoke func(context.Context, BuiltinInvocation, []byte) ([]byte, bool, error)) (body []byte, found bool, err error) {
	w := &builtinWitness{owner: e, call: call}
	w.active.Store(true)
	borrowed := bytes.Clone(call.arguments)
	domain := context.WithValue(ctx, builtinWitnessKey{}, w)
	invocation := BuiltinInvocation{call.binding, call.record.ID, call.record.Attempt, call.record.Request}
	defer func() {
		if recover() != nil {
			clear(body)
			body = nil
			found = false
			err = f.NewFault(f.CommitUnknown, f.Unknown)
		}
		if !lookup {
			if owner, ok := backend.(BuiltinCallOwner); ok {
				// Execute has returned (including its deferred cleanup). Keep the same
				// borrowed witness alive, with a fresh bounded budget, for its actual Join.
				w.cleanupOnly.Store(true)
				wait, cancel := context.WithTimeout(context.WithoutCancel(domain), 5*time.Second)
				call.backendJoinErr = joinBuiltinOwner(wait, owner, invocation)
				cancel()
				call.backendPending = call.backendJoinErr != nil
			}
		}
		w.active.Store(false)
		clear(borrowed)
	}()
	return invoke(domain, invocation, borrowed)
}

func (e *BuiltinExecutor) invokeCanonicalLookup(ctx context.Context, call *builtinCall, backend BuiltinBackend) (BuiltinCanonicalOutcome, error) {
	if !coldBuiltinSupported(call) || call.cold == nil || !call.cold.permits(e.authority.process, call.record) {
		return BuiltinCanonicalOutcome{}, fail(f.Forbidden)
	}
	var result BuiltinCanonicalOutcome
	body, found, err := e.borrowBackend(ctx, call, backend, true, func(domain context.Context, inv BuiltinInvocation, raw []byte) ([]byte, bool, error) {
		if canonical, ok := backend.(BuiltinCanonicalLookup); ok {
			var err error
			result, err = canonical.LookupCanonical(domain, inv, raw)
			if err != nil {
				return nil, false, err
			}
			if err = result.validate(); err != nil {
				return nil, false, err
			}
			return result.Body, result.Found, nil
		}
		return backend.Lookup(domain, inv, raw)
	})
	if err != nil {
		clear(result.Body)
		return BuiltinCanonicalOutcome{}, err
	}
	result.Body, result.Found = body, found
	if err = result.validate(); err != nil {
		clear(body)
		return BuiltinCanonicalOutcome{}, err
	}
	return result, nil
}

// resolveReturned cannot repeat Execute. It may republish an original complete
// result, or borrow a canonical mutation receipt after that exact domain call
// actually returned. A cold running row alone cannot prove physical return.
func (e *BuiltinExecutor) resolveReturned(ctx context.Context, observed *builtinCall) error {
	e.mu.Lock()
	old := e.calls[observed.binding.Identity()]
	if old == nil {
		e.mu.Unlock()
		return e.resolveCold(ctx, observed)
	}
	if !sameRunnerCallBinding(old.binding, observed.binding) {
		e.mu.Unlock()
		return nil
	}
	select {
	case <-old.done:
	default:
		e.mu.Unlock()
		return fail(f.ResourceBusy)
	}
	if !old.domainReturned || old.resolving || old.record.ID != observed.record.ID || old.record.Attempt != observed.record.Attempt || old.record.InputDigest != observed.record.InputDigest {
		e.mu.Unlock()
		return fail(f.ResourceBusy)
	}
	old.resolving = true
	observed.arguments = bytes.Clone(old.arguments)
	if old.terminal != nil {
		terminal := *old.terminal
		observed.terminal = &terminal
		observed.body = bytes.Clone(old.body)
	}
	e.mu.Unlock()
	defer func() { e.mu.Lock(); old.resolving = false; e.mu.Unlock() }()
	if err := e.joinReturnedBackend(ctx, old); err != nil {
		return err
	}
	if observed.terminal == nil {
		backend := e.backendFor(observed)
		body, found, err := e.invokeBackend(ctx, observed, backend, true)
		defer func() { clear(body) }()
		if err != nil {
			var fault *f.Fault
			if !errors.As(err, &fault) || fault.CommitState != f.Committed || fault.Code != f.ResourceDeleted {
				return err
			}
			// The canonical domain command is cancelled/deleted. It grants no
			// result or retry; preserve its known failure as the Tool response.
			body, _ = json.Marshal(map[string]any{"error": map[string]string{"code": string(fault.Code.Safe())}})
			terminal := builtinTerminal{Format: 1, Operation: observed.record.ID, Attempt: observed.record.Attempt, Request: observed.record.Request, Outcome: "failure", Code: string(fault.Code.Safe()), ResultDigest: digest(body), ResultBytes: f.Progress(len(body))}
			observed.terminal = &terminal
		} else {
			if !found {
				return fail(f.ResourceBusy)
			}
			if len(body) < 2 || len(body) > 1<<20 || !json.Valid(body) {
				return fail(f.DependencyUnavailable)
			}
			terminal := builtinTerminal{Format: 1, Operation: observed.record.ID, Attempt: observed.record.Attempt, Request: observed.record.Request, Outcome: "success", ResultDigest: digest(body), ResultBytes: f.Progress(len(body))}
			if skillPrepareResultBinding(observed.binding) {
				terminal.Outcome, terminal.Code, err = e.backendTerminal(observed, body)
				if err != nil {
					return err
				}
			}
			observed.terminal = &terminal
		}
		observed.body = bytes.Clone(body)
		// Capture before publication, including when that commit is Unknown.
		e.mu.Lock()
		terminal := *observed.terminal
		old.terminal = &terminal
		old.body = bytes.Clone(observed.body)
		e.mu.Unlock()
	}
	return e.publish(ctx, observed)
}

func joinBuiltinOwner(ctx context.Context, owner BuiltinCallOwner, invocation BuiltinInvocation) (err error) {
	defer func() {
		if recover() != nil {
			err = fail(f.DependencyUnavailable)
		}
	}()
	return owner.JoinBuiltinCall(ctx, invocation)
}

// Called only after the exact retained call returned and while its original
// owner is exclusively resolving. This witness grants cleanup, not dispatch.
func (e *BuiltinExecutor) joinReturnedBackend(ctx context.Context, call *builtinCall) error {
	e.mu.Lock()
	pending := call.backendPending
	e.mu.Unlock()
	if !pending {
		return nil
	}
	owner, ok := e.backendFor(call).(BuiltinCallOwner)
	if !ok {
		return fail(f.DependencyUnbound)
	}
	w := &builtinWitness{owner: e, call: call}
	w.cleanupOnly.Store(true)
	w.active.Store(true)
	defer w.active.Store(false)
	domain := context.WithValue(ctx, builtinWitnessKey{}, w)
	err := joinBuiltinOwner(domain, owner, BuiltinInvocation{call.binding, call.record.ID, call.record.Attempt, call.record.Request})
	e.mu.Lock()
	call.backendJoinErr = err
	call.backendPending = err != nil
	e.mu.Unlock()
	return err
}
