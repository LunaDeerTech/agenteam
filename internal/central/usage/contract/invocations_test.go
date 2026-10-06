package contract

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func invocationTestID[T any](t *testing.T) f.ID[T] {
	t.Helper()
	v, e := f.NewID[T]()
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func invocationRequest(t *testing.T) InvocationRequest {
	a, _ := id.NewHuman(invocationTestID[id.User](t), invocationTestID[id.Session](t))
	agent, execution := invocationTestID[id.Agent](t), invocationTestID[id.Execution](t)
	return InvocationRequest{Actor: a, Access: ApplyInvocation, Action: ReserveAction, Sequence: 1, Identity: InvocationIdentity{Attempt: mc.AttemptIdentity{CallID: invocationTestID[mc.Call](t), InvocationID: invocationTestID[mc.Invocation](t), AttemptIndex: 1, ProcessID: invocationTestID[oc.Process](t), Fence: 1}, Consumer: mc.Consumer{Kind: mc.AgentConsumer, ProjectID: invocationTestID[id.Project](t), AgentID: &agent, ExecutionID: &execution, Purpose: mc.AgentGeneration}, SnapshotID: invocationTestID[mc.Snapshot](t), Input: mc.InputIdentity{ExecutionID: &execution, RoundID: invocationTestID[struct{}](t).String(), Digest: digestBytes([]byte("canonical-input-canary")), SchemaVersion: 1}}}
}
func TestInvocationBindingAndOpaquePlans(t *testing.T) {
	r := invocationRequest(t)
	b, e := InvocationBinding(r)
	if e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*InvocationRequest){func(q *InvocationRequest) { q.Access = ConfirmInvocation }, func(q *InvocationRequest) { q.Action = ObserveAction }, func(q *InvocationRequest) { q.Identity.Attempt.Fence++ }, func(q *InvocationRequest) { q.Identity.Input.SchemaVersion++ }, func(q *InvocationRequest) {
		u, _ := f.ParseID[id.User](q.Actor.Details().UserID)
		q.Actor, _ = id.NewHuman(u, invocationTestID[id.Session](t))
	}} {
		q := r.Clone()
		change(&q)
		other, e := InvocationBinding(q)
		if e != nil || b == other {
			t.Fatal("complete binding missing", e)
		}
	}
	i := NewPlanIssuer()
	mapping := digestBytes([]byte("provider-version"))
	k, _ := f.ProjectLock(r.Identity.Consumer.ProjectID.String())
	dependencies, e := NewInvocationDependencies(i, InvocationDependencyDetails{Binding: b, Mapping: mapping, Locks: []f.LockRequest{{Key: k, Mode: f.Shared}, {Key: k, Mode: f.Exclusive}}})
	if e != nil || len(dependencies.RequiredLocks()) != 1 || dependencies.RequiredLocks()[0].Mode != f.Exclusive {
		t.Fatal("lock normalization", e)
	}
	locks := dependencies.RequiredLocks()
	locks[0].Mode = f.Shared
	if dependencies.RequiredLocks()[0].Mode != f.Exclusive {
		t.Fatal("mutable dependency")
	}
	command, _ := f.NewCommandIdentity("model.usage", []string{r.Identity.Consumer.ProjectID.String()}, "call-ledger", f.IdempotencyKey(r.Identity.Attempt.CallID.String()))
	cause, _ := f.NewCommandsCause(command)
	p, e := NewInvocationPlan(i, InvocationPlanDetails{Binding: b, Mapping: mapping, Locks: dependencies.RequiredLocks(), Facts: dependencies, Cause: cause})
	if e != nil || !p.Matches(i, b, mapping) || p.Matches(NewPlanIssuer(), b, mapping) {
		t.Fatal("issuer isolation", e)
	}
	if _, e = NewInvocationPlan(i, InvocationPlanDetails{Binding: b, Mapping: mapping, Locks: []f.LockRequest{{Key: k, Mode: f.Shared}}, Facts: dependencies, Cause: cause}); e == nil {
		t.Fatal("EX lock omitted")
	}
	for _, target := range []any{new(PlanIssuer), new(InvocationPlan), new(InvocationDependencies), new(InvocationFact), new(InvocationRequest)} {
		if json.Unmarshal([]byte(`{}`), target) == nil {
			t.Fatal("deserialized authority")
		}
	}
	for _, v := range []any{r, dependencies, p, struct{ p InvocationPlan }{p}} {
		for _, format := range []string{"%v", "%+v", "%#v"} {
			text := fmt.Sprintf(format, v)
			if strings.Contains(text, string(r.Identity.Input.Digest)) || strings.Contains(text, r.Actor.Details().SessionID) {
				t.Fatal("private identity leaked")
			}
		}
	}
	cloned := r.Clone()
	*cloned.Identity.Input.ExecutionID = invocationTestID[id.Execution](t)
	if *r.Identity.Input.ExecutionID == *cloned.Identity.Input.ExecutionID {
		t.Fatal("input aliased")
	}
	_ = context.Background()
}
