package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

type builtinResolutionBackend struct {
	BuiltinBackend
	execute, lookup int
	witness         *builtinWitness
	borrow          []byte
	panicCall       bool
}

func (b *builtinResolutionBackend) Execute(ctx context.Context, _ BuiltinInvocation, raw []byte) ([]byte, error) {
	b.execute++
	b.witness, _ = ctx.Value(builtinWitnessKey{}).(*builtinWitness)
	b.borrow = raw
	if b.panicCall {
		panic("private backend failure")
	}
	return []byte(`{"ok":true}`), nil
}
func (b *builtinResolutionBackend) Lookup(ctx context.Context, _ BuiltinInvocation, raw []byte) ([]byte, bool, error) {
	b.lookup++
	b.witness, _ = ctx.Value(builtinWitnessKey{}).(*builtinWitness)
	b.borrow = raw
	return []byte(`{"ok":true}`), true, nil
}
func TestBuiltinBorrowRetiresOnReturnAndPanic(t *testing.T) {
	for _, panicCall := range []bool{false, true} {
		backend := &builtinResolutionBackend{panicCall: panicCall}
		executor := &BuiltinExecutor{}
		call := &builtinCall{arguments: []byte(`{"private":"content"}`)}
		_, _, err := executor.invokeBackend(context.Background(), call, backend, false)
		if backend.witness == nil || backend.witness.active.Load() {
			t.Fatal("domain witness escaped its original invocation")
		}
		for _, v := range backend.borrow {
			if v != 0 {
				t.Fatal("domain argument borrow not cleared")
			}
		}
		if panicCall != builtinUnknown(err) {
			t.Fatal("panic lost its uncertain outcome")
		}
	}
}
func TestBuiltinLookupCannotDispatchOrAuthorizeMutation(t *testing.T) {
	backend := &builtinResolutionBackend{}
	executor := &BuiltinExecutor{}
	task := newOperationID[f.Request](t).String()
	call := &builtinCall{lookup: true, arguments: []byte(`{"task_id":"` + task + `"}`)}
	call.binding.Binding = tc.BuiltinBinding{HandlerID: "task.transfer", ContractRevision: 1}
	_, found, err := executor.invokeBackend(context.Background(), call, backend, true)
	if err != nil || !found || backend.lookup != 1 || backend.execute != 0 {
		t.Fatal("lookup repeated domain Execute")
	}
	if checkBuiltinTask(call, task, "lookup-transfer") != nil || checkBuiltinTask(call, task, "transfer") == nil {
		t.Fatal("historical lookup acquired transfer authority")
	}
}
func TestBuiltinJoinRequiresOriginalReturnAndExactTerminal(t *testing.T) {
	h := readFileHandoffFixture(t)
	b := h.binding
	b.RunnerBinding = tc.RunnerBinding{}
	b.Binding = tc.BuiltinBinding{HandlerID: "task.transfer", ContractRevision: 1}
	run, cancel := context.WithCancel(context.Background())
	defer cancel()
	call := &builtinCall{binding: b, ctx: run, cancel: cancel, done: make(chan struct{}), record: builtinRecord{ID: h.record.ID, Attempt: h.attempt, InputDigest: h.record.InputDigest}, retirement: f.NewFault(f.CommitUnknown, f.Unknown)}
	executor := &BuiltinExecutor{authority: &BuiltinAuthority{}, calls: map[tc.ToolCallIdentity]*builtinCall{b.Identity(): call}, lookups: map[*builtinCall]struct{}{}, changed: make(chan struct{})}
	executor.Stop()
	if run.Err() == nil || executor.Joined() {
		t.Fatal("Stop was treated as physical Join")
	}
	wait, cancelWait := context.WithTimeout(context.Background(), 5*time.Millisecond)
	if !errors.Is(executor.JoinCall(wait, b), context.DeadlineExceeded) {
		t.Fatal("join did not await original return")
	}
	cancelWait()
	terminal := builtinTerminal{Format: 1, Operation: h.record.ID, Attempt: h.attempt, Request: h.request, Outcome: "success"}
	call.terminal = &terminal
	observed := &builtinCall{binding: b, record: call.record, terminal: &terminal}
	executor.retireObserved(observed)
	if executor.Joined() {
		t.Fatal("receipt retired an unreturned domain call")
	}
	close(call.done)
	wrong := terminal
	wrong.Attempt = newOperationID[tc.Attempt](t)
	observed.terminal = &wrong
	executor.retireObserved(observed)
	if executor.Joined() || executor.JoinCall(context.Background(), b) == nil {
		t.Fatal("foreign terminal retired Unknown")
	}
	observed.terminal = &terminal
	executor.retireObserved(observed)
	if !executor.Joined() || executor.Drain(context.Background()) != nil {
		t.Fatal("exact returned owner did not retire")
	}
	zero := &BuiltinExecutor{}
	zero.Stop()
	if zero.Joined() || zero.Drain(context.Background()) == nil {
		t.Fatal("zero executor acquired join proof")
	}
}
