package memory

import (
	"context"
	"errors"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/memory/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

func TestMemoryJoinedModelFailureIsKnownWithoutHidingPendingRetirement(t *testing.T) {
	modelErr := &mc.ModelError{Category: "unsupported_feature", Code: "wire_unsupported_feature"}
	err := modelCallError(modelErr, nil)
	requireFault(t, err, f.DependencyUnavailable)
	if !errors.Is(err, modelErr) || isUnknown(err) {
		t.Fatal("joined pre-dispatch rejection lost its known outcome")
	}
	unknown := f.NewFault(f.CommitUnknown, f.Unknown)
	for _, result := range []error{modelErr, fault(f.DependencyUnavailable), nil} {
		err := modelCallError(result, unknown)
		if !isUnknown(err) || !errors.Is(err, unknown) {
			t.Fatal("original retirement Unknown was hidden by the model result")
		}
	}
	pending := modelCallError(modelErr, context.DeadlineExceeded)
	var known *f.Fault
	if errors.As(pending, &known) || !errors.Is(pending, context.DeadlineExceeded) {
		t.Fatal("unjoined Close was converted to a completed domain failure")
	}
	if modelCallError(unknown, nil) != unknown || modelCallError(nil, nil) != nil {
		t.Fatal("existing commit state or success was changed")
	}
}

type memoryOwnedHandle struct {
	joined bool
	closes int
}

func (h *memoryOwnedHandle) Close(ctx context.Context) error {
	h.closes++
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if !h.joined {
		return fault(f.ResourceBusy)
	}
	return nil
}
func (h *memoryOwnedHandle) Joined() bool { return h.joined }

func TestMemoryModelCloseRetainsOriginalOwnerUntilActualJoin(t *testing.T) {
	s := &Service{calls: map[*call]struct{}{}, changed: make(chan struct{})}
	_, cancel := context.WithCancel(context.Background())
	entry := &call{service: s, cancel: cancel}
	s.calls[entry] = struct{}{}
	a := &ModelAuthority{domain: s, calls: map[*modelInvocation]struct{}{}}
	inv := &modelInvocation{authority: a, entry: entry}
	a.calls[inv] = struct{}{}
	handle := &memoryOwnedHandle{}
	p := &modelPending{inv: inv, handle: handle}
	m := &Models{domain: s, authority: a, pending: map[*modelPending]struct{}{p: {}}, finished: map[*call]struct{}{}}
	m.finish(entry)
	if m.Joined() {
		t.Fatal("pending owner reported Joined")
	}
	ctx, stop := context.WithCancel(context.Background())
	stop()
	if err := m.close(ctx, p); err == nil {
		t.Fatal("cancelled close cannot imply join")
	}
	if p.done || len(m.pending) != 1 || len(s.calls) != 1 || len(a.calls) != 1 {
		t.Fatal("original owner lost before join")
	}
	if err := m.close(context.Background(), p); err == nil {
		t.Fatal("acknowledgement cannot imply join")
	}
	handle.joined = true
	if err := m.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !p.done || len(m.pending) != 0 || len(s.calls) != 0 || len(a.calls) != 0 || handle.closes != 3 {
		t.Fatal("original handle was not joined exactly once")
	}
	if !m.Joined() {
		t.Fatal("joined owner remains registered")
	}
}

func TestMemoryDrainCannotRetireCallDuringAdmission(t *testing.T) {
	p := &modelPending{admitting: true}
	m := &Models{}
	requireFault(t, m.close(context.Background(), p), f.ResourceBusy)
	if p.done {
		t.Fatal("admitting owner was discarded")
	}
}

type memoryJoinGate struct{ allowed bool }

func (g *memoryJoinGate) DiscoverMemoryTool(context.Context, i.Actor, i.ProjectID, i.AgentID, tc.OperationID, string) ([]f.LockRequest, error) {
	if !g.allowed {
		return nil, fault(f.Forbidden)
	}
	return nil, nil
}
func (g *memoryJoinGate) RequireMemoryToolInTx(context.Context, f.Tx, i.Actor, i.ProjectID, i.AgentID, tc.OperationID, string) error {
	return fault(f.Forbidden)
}
func (g *memoryJoinGate) RequireAgentRunBindingInTx(context.Context, f.Tx, any) error {
	return fault(f.Forbidden)
}
func TestMemoryOperationJoinRequiresOriginalToolWitnessAndExactOperation(t *testing.T) {
	gate := &memoryJoinGate{}
	s := &Service{deps: Dependencies{Tools: gate}, calls: map[*call]struct{}{}, changed: make(chan struct{})}
	ns := c.Namespace{ProjectID: testID[i.Project](t), AgentID: testID[i.Agent](t)}
	actor, err := i.NewAgentRun(ns.ProjectID, ns.AgentID, testID[i.Execution](t))
	if err != nil {
		t.Fatal(err)
	}
	_, cancel := context.WithCancel(context.Background())
	entry := &call{service: s, actor: actor, namespace: ns, operation: testID[tc.Operation](t), intent: "retain", cancel: cancel}
	s.calls[entry] = struct{}{}
	a := &ModelAuthority{domain: s, calls: map[*modelInvocation]struct{}{}}
	inv := &modelInvocation{authority: a, entry: entry}
	a.calls[inv] = struct{}{}
	handle := &memoryOwnedHandle{joined: true}
	p := &modelPending{inv: inv, handle: handle}
	m := &Models{domain: s, authority: a, pending: map[*modelPending]struct{}{p: {}}, finished: map[*call]struct{}{entry: {}}}
	r := &Runtime{domain: s, models: m}
	if err := r.JoinOperation(context.Background(), actor, ns, testID[tc.Operation](t)); err != nil {
		t.Fatal(err)
	}
	if handle.closes != 0 || m.Joined() {
		t.Fatal("foreign operation controlled original owner")
	}
	requireFault(t, r.JoinOperation(context.Background(), actor, ns, entry.operation), f.Forbidden)
	if handle.closes != 0 {
		t.Fatal("DTO without original Tool witness closed owner")
	}
	gate.allowed = true
	if err := r.JoinOperation(context.Background(), actor, ns, entry.operation); err != nil {
		t.Fatal(err)
	}
	if handle.closes != 1 || !m.Joined() {
		t.Fatal("original witnessed operation did not join")
	}
}
