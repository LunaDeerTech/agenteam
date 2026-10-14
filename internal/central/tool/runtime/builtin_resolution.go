package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// A private context is valid only during this original synchronous invocation.
// Even a panic retires the borrow, while retaining the uncertain domain effect.
func (e *BuiltinExecutor) invokeBackend(ctx context.Context, call *builtinCall, backend BuiltinBackend, lookup bool) (body []byte, found bool, err error) {
	w := &builtinWitness{owner: e, call: call}
	w.active.Store(true)
	borrowed := bytes.Clone(call.arguments)
	defer func() {
		w.active.Store(false)
		clear(borrowed)
		if recover() != nil {
			clear(body)
			body = nil
			found = false
			err = f.NewFault(f.CommitUnknown, f.Unknown)
		}
	}()
	domain := context.WithValue(ctx, builtinWitnessKey{}, w)
	invocation := BuiltinInvocation{call.binding, call.record.ID, call.record.Attempt, call.record.Request}
	if lookup {
		return backend.Lookup(domain, invocation, borrowed)
	}
	body, err = backend.Execute(domain, invocation, borrowed)
	return body, err == nil, err
}

// resolveReturned cannot repeat Execute. It may republish an original complete
// result, or borrow a canonical mutation receipt after that exact domain call
// actually returned. A cold running row alone cannot prove physical return.
func (e *BuiltinExecutor) resolveReturned(ctx context.Context, observed *builtinCall) error {
	e.mu.Lock()
	old := e.calls[observed.binding.Identity()]
	if old == nil || !sameRunnerCallBinding(old.binding, observed.binding) {
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
	if observed.terminal == nil {
		backend := e.backends[observed.binding.Binding]
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
