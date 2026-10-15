package runtime

import (
	"context"
	"testing"

	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

func TestSkillPrepareReturnedLookupRequiresOriginalPhysicalOwner(t *testing.T) {
	h := readFileHandoffFixture(t)
	b := h.binding
	b.RunnerBinding = tc.RunnerBinding{}
	b.Binding = tc.BuiltinBinding{HandlerID: "skill.prepare", ContractRevision: 1}
	v := BuiltinInvocation{Call: b, OperationID: h.record.ID, AttemptID: h.attempt, RequestID: h.request}
	old := &builtinCall{binding: b, done: make(chan struct{}), resolving: true, domainReturned: true, record: builtinRecord{ID: h.record.ID, Attempt: h.attempt, Request: h.request, InputDigest: h.record.InputDigest, Phase: "running", Version: 2}}
	e := &BuiltinExecutor{calls: map[tc.ToolCallIdentity]*builtinCall{b.Identity(): old}}
	observed := &builtinCall{binding: b, lookup: true, record: old.record}
	w := &builtinWitness{owner: e, call: observed}
	w.active.Store(true)
	ctx := context.WithValue(context.Background(), builtinWitnessKey{}, w)
	if _, _, err := skillPrepareReturnedLookup(context.Background(), v); err == nil {
		t.Fatal("DTO replaced original private lookup")
	}
	if _, _, err := skillPrepareReturnedLookup(ctx, v); err == nil {
		t.Fatal("unreturned original owner admitted lookup")
	}
	close(old.done)
	old.backendPending = true
	if _, _, err := skillPrepareReturnedLookup(ctx, v); err == nil {
		t.Fatal("unjoined backend admitted lookup")
	}
	old.backendPending = false
	owned, release, err := skillPrepareReturnedLookup(ctx, v)
	if err != nil {
		t.Fatal(err)
	}
	proof := owned.Value(builtinWitnessKey{}).(*builtinWitness)
	if proof.call != old || !proof.active.Load() || !proof.cleanupOnly.Load() {
		t.Fatal("lookup failed to bind the actual original cleanup witness")
	}
	release()
	if proof.active.Load() {
		t.Fatal("lookup witness escaped return")
	}
	observed.record.InputDigest = "changed"
	if _, _, err = skillPrepareReturnedLookup(ctx, v); err == nil {
		t.Fatal("changed original input rebound")
	}
}
