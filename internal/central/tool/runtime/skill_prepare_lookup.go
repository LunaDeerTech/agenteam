package runtime

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// Only the executor already resolving this exact retained, physically returned
// call can lend the original cleanup witness to a local result lookup. Neither
// a caller DTO, a cold row nor a new invocation can mint this context.
func skillPrepareReturnedLookup(ctx context.Context, v BuiltinInvocation) (context.Context, func(), error) {
	w, _ := ctx.Value(builtinWitnessKey{}).(*builtinWitness)
	if w == nil || !w.active.Load() || w.owner == nil || w.call == nil || !w.call.lookup || !sameRunnerCallBinding(w.call.binding, v.Call) || w.call.record.ID != v.OperationID || w.call.record.Attempt != v.AttemptID || w.call.record.Request != v.RequestID {
		return nil, nil, fail(f.Forbidden)
	}
	e := w.owner
	e.mu.Lock()
	old := e.calls[v.Call.Identity()]
	valid := old != nil && !old.lookup && old.resolving && old.domainReturned && !old.backendPending && sameRunnerCallBinding(old.binding, v.Call) && old.record.ID == w.call.record.ID && old.record.Attempt == w.call.record.Attempt && old.record.Request == w.call.record.Request && old.record.InputDigest == w.call.record.InputDigest && old.record.Fingerprint == w.call.record.Fingerprint && old.record.Phase == "running" && old.record.Version == 2
	if valid {
		select {
		case <-old.done:
		default:
			valid = false
		}
	}
	e.mu.Unlock()
	if !valid {
		return nil, nil, fail(f.ResourceBusy)
	}
	proof := &builtinWitness{owner: e, call: old}
	proof.active.Store(true)
	proof.cleanupOnly.Store(true)
	return context.WithValue(ctx, builtinWitnessKey{}, proof), func() { proof.active.Store(false) }, nil
}
