package runtime

import (
	"context"
	"errors"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"testing"
	"time"
)

type ownedBuiltinControl struct {
	BuiltinBackend
	t                      *testing.T
	execute, lookup, joins int
	executed               bool
	cancel                 context.CancelFunc
	joinErr                error
	witness                *builtinWitness
}

func (b *ownedBuiltinControl) Execute(ctx context.Context, _ BuiltinInvocation, _ []byte) ([]byte, error) {
	b.execute++
	b.executed = true
	b.witness, _ = ctx.Value(builtinWitnessKey{}).(*builtinWitness)
	if b.cancel != nil {
		b.cancel()
	}
	return []byte(`{"canonical":true}`), nil
}
func (b *ownedBuiltinControl) Lookup(context.Context, BuiltinInvocation, []byte) ([]byte, bool, error) {
	b.lookup++
	return nil, false, nil
}
func (b *ownedBuiltinControl) JoinBuiltinCall(ctx context.Context, inv BuiltinInvocation) error {
	b.joins++
	w, _ := ctx.Value(builtinWitnessKey{}).(*builtinWitness)
	if !b.executed || w == nil || !w.active.Load() || !w.cleanupOnly.Load() || w.call.record.ID != inv.OperationID {
		b.t.Fatal("cleanup lacked original owner")
	}
	if ctx.Err() != nil {
		b.t.Fatal("cleanup inherited cancelled dispatch budget")
	}
	if w.owner.authority.withCurrent(ctx, f.Tx{}, w, func(context.Context, []byte) error { return nil }) == nil {
		b.t.Fatal("cleanup gained input/mutation permission")
	}
	b.witness = w
	return b.joinErr
}
func TestBuiltinOriginalOwnerJoinsBeforeWitnessRetires(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := &ownedBuiltinControl{t: t, cancel: cancel}
	e := &BuiltinExecutor{authority: &BuiltinAuthority{}}
	call := &builtinCall{arguments: []byte(`{}`)}
	body, found, err := e.invokeBackend(ctx, call, b, false)
	if err != nil || !found || string(body) != `{"canonical":true}` || b.execute != 1 || b.joins != 1 || b.lookup != 0 || call.backendPending || b.witness.active.Load() {
		t.Fatal("original return or Join proof lost", err)
	}
	if ctx.Err() == nil {
		t.Fatal("fixture did not cancel original call")
	}
}
func TestBuiltinFailedJoinRetainsResultAndCannotRetireOwner(t *testing.T) {
	b := &ownedBuiltinControl{t: t, joinErr: context.DeadlineExceeded}
	h := readFileHandoffFixture(t)
	binding := h.binding
	binding.RunnerBinding = tc.RunnerBinding{}
	binding.Binding = tc.BuiltinBinding{HandlerID: "memory.retain", ContractRevision: 1}
	run, cancel := context.WithCancel(context.Background())
	defer cancel()
	e := &BuiltinExecutor{authority: &BuiltinAuthority{}, calls: map[tc.ToolCallIdentity]*builtinCall{}, lookups: map[*builtinCall]struct{}{}, backends: map[tc.BuiltinBinding]BuiltinBackend{binding.Binding: b}, changed: make(chan struct{})}
	call := &builtinCall{binding: binding, ctx: run, cancel: cancel, done: make(chan struct{}), record: builtinRecord{ID: h.record.ID, Attempt: h.attempt, InputDigest: h.record.InputDigest}}
	e.calls[binding.Identity()] = call
	body, _, err := e.invokeBackend(run, call, b, false)
	if err != nil || !call.backendPending || !errors.Is(call.backendJoinErr, context.DeadlineExceeded) || len(body) == 0 {
		t.Fatal("cleanup failure lost known body", err)
	}
	term := builtinTerminal{Format: 1, Operation: call.record.ID, Attempt: call.record.Attempt}
	call.terminal = &term
	call.retirement = call.backendJoinErr
	call.domainReturned = true
	e.returned(call)
	e.Stop()
	observed := &builtinCall{binding: binding, record: call.record, terminal: &term}
	e.retireObserved(observed)
	if e.Joined() {
		t.Fatal("terminal receipt retired unjoined resources")
	}
	wait, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := e.JoinCall(wait, binding); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("failed original resource Join hidden", err)
	}
	if b.execute != 1 || b.lookup != 0 || b.joins != 2 || b.witness.active.Load() {
		t.Fatal("Join replayed Execute/Lookup or leaked witness")
	}
	b.joinErr = nil
	// Exercise only physical owner Join. Publication is separately verified by
	// the original Tool ledger fixture; no SQL success is invented here.
	if err := e.joinReturnedBackend(wait, call); err != nil {
		t.Fatal(err)
	}
	if call.backendPending || b.execute != 1 || b.lookup != 0 || b.joins != 3 {
		t.Fatal("fresh Join replaced original operation")
	}
}
