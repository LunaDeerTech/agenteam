package contract

import (
	"encoding/json"
	"sync"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func TestExactInvocationAndSharedLeaseRetirement(t *testing.T) {
	x := setup(t)
	r := x.read(t)
	b, e := ConsumerBinding(r)
	must(t, e)
	for _, mutate := range []func(*ConsumerRequest){func(c *ConsumerRequest) { c.Attempt.InvocationID = fresh[Invocation](t) }, func(c *ConsumerRequest) { c.Attempt.Fence++ }, func(c *ConsumerRequest) { c.Attempt.ProcessID = fresh[oc.Process](t) }, func(c *ConsumerRequest) { c.Input.Digest = digest("b") }} {
		c := r.Clone()
		mutate(&c)
		v, e := ConsumerBinding(c)
		must(t, e)
		if v == b {
			t.Fatal("use identity omitted")
		}
	}
	c := r.Clone()
	c.Attempt.CallID = fresh[Call](t)
	reject(t, c.Validate())
	c = r.Clone()
	c.Action = RetireConsumer
	c.Input = nil
	c.Attempt = nil
	reject(t, c.Validate())
	c.CallID = nil
	must(t, c.Validate())
	c = r.Clone()
	c.LeaseOwner, _ = sc.NewCredentialLeaseOwner(sc.ModelCallOwner, x.call.String())
	c.Action = RetireConsumer
	c.Input = nil
	c.Attempt = nil
	must(t, c.Validate())
	c.CallID = nil
	reject(t, c.Validate())
}
func TestOpaquePlanCopiesAndIssuerCannotBeForged(t *testing.T) {
	i := NewPlanIssuer()
	d := ConsumerDependencyDetails{Binding: digest("a"), Mapping: digest("b"), Locks: locks(t), RetryPolicy: &RetryPolicy{Class: AgentRetry, Categories: []ErrorCategory{"timeout"}}}
	p, e := NewConsumerDependencies(i, d)
	must(t, e)
	d.Locks[0].Mode = f.Exclusive
	d.RetryPolicy.Categories[0] = "cancelled"
	if p.Details().Locks[0].Mode != f.Shared || p.Details().RetryPolicy.Categories[0] != "timeout" {
		t.Fatal("input alias")
	}
	q := p.Details()
	q.RetryPolicy.Categories[0] = "network"
	q.Locks[0].Mode = f.Exclusive
	if p.Details().RetryPolicy.Categories[0] != "timeout" || p.RequiredLocks()[0].Mode != f.Shared {
		t.Fatal("output alias")
	}
	if !p.Matches(i, digest("a"), digest("b")) || p.Matches(NewPlanIssuer(), digest("a"), digest("b")) || p.Matches(i, digest("c"), digest("b")) {
		t.Fatal("binding")
	}
	var zero ConsumerDependencies
	reject(t, zero.Validate())
	reject(t, json.Unmarshal([]byte(`{"binding":"x"}`), &zero))
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				v := p.Details()
				v.Locks[0].Mode = f.Exclusive
				v.RetryPolicy.Categories[0] = "network"
			}
		}()
	}
	wg.Wait()
	if p.Details().RetryPolicy.Categories[0] != "timeout" {
		t.Fatal("concurrent alias")
	}
}
func TestPlanUnionMustCoverChildAndNormalizeStrongest(t *testing.T) {
	x := setup(t)
	childLocks := locks(t)
	childLocks[0].Mode = f.Exclusive
	child, e := NewConsumerDependencies(NewPlanIssuer(), ConsumerDependencyDetails{Binding: digest("a"), Mapping: digest("b"), Locks: childLocks})
	must(t, e)
	d := ResolutionPlanDetails{Binding: digest("c"), Mapping: digest("d"), Locks: locks(t), SnapshotID: x.snapshot.ID, ProviderID: x.snapshot.Identity.ProviderID, ModelID: x.snapshot.Identity.ModelID, Consumer: child}
	_, e = NewResolutionPlan(NewPlanIssuer(), d)
	reject(t, e)
	d.Locks = append(d.Locks, childLocks...)
	p, e := NewResolutionPlan(NewPlanIssuer(), d)
	must(t, e)
	if len(p.RequiredLocks()) != 1 || p.RequiredLocks()[0].Mode != f.Exclusive {
		t.Fatal("lock merge")
	}
	d.LeaseID = &x.lease
	_, e = NewResolutionPlan(NewPlanIssuer(), d)
	reject(t, e)
}
func TestFinitePolicyCannotBecomeAgentBudget(t *testing.T) {
	x := setup(t)
	must(t, (RetryPolicy{Class: AgentRetry, Categories: []ErrorCategory{"timeout"}}).ValidateFor(x.c))
	reject(t, (RetryPolicy{Class: AgentRetry, MaxAttempts: ptr(f.Sequence(5))}).Validate())
	c := Consumer{Kind: ToolConsumer, ProjectID: x.c.ProjectID, Purpose: ApprovalAuto, OperationID: fresh[struct{}](t).String()}
	p := RetryPolicy{Class: BoundedRetry, Deadline: &x.now, MaxAttempts: ptr(f.Sequence(1))}
	must(t, p.ValidateFor(c))
	p.Categories = []ErrorCategory{"timeout"}
	reject(t, p.ValidateFor(c))
	p.Categories = nil
	*p.MaxAttempts = 2
	reject(t, p.ValidateFor(c))
}

func TestConsumerActionBindingsRejectForeignFields(t *testing.T) {
	x := setup(t)
	invoke := x.read(t)
	invoke.Action = InvokeConsumer
	invoke.Attempt = nil
	resolve := x.read(t)
	resolve.Action = ResolveConsumer
	resolve.CallID = nil
	resolve.Attempt = nil
	resolve.Input = nil
	resolve.Resolve = ptr(x.resolve())
	finalize := x.read(t)
	finalize.Action = FinalizeConsumer
	finalize.LeaseID = nil
	finalize.TerminalVersion = ptr(f.Version(1))
	retire := x.read(t)
	retire.Action = RetireConsumer
	retire.CallID = nil
	retire.Attempt = nil
	retire.Input = nil
	for _, r := range []ConsumerRequest{invoke, resolve, finalize, retire, x.read(t)} {
		must(t, r.Validate())
		b, e := ConsumerBinding(r)
		must(t, e)
		c := r.Clone()
		c.SnapshotID = fresh[Snapshot](t)
		different, e := ConsumerBinding(c)
		must(t, e)
		if b == different {
			t.Fatal("snapshot omitted")
		}
		c = r.Clone()
		c.Action = "unknown"
		reject(t, c.Validate())
	}
	c := invoke.Clone()
	c.Attempt = x.read(t).Attempt
	reject(t, c.Validate())
	c = resolve.Clone()
	c.Input = ptr(x.input(t))
	reject(t, c.Validate())
	c = finalize.Clone()
	c.Attempt.CallID = fresh[Call](t)
	reject(t, c.Validate())
	c = x.read(t)
	c.LeaseID = nil
	reject(t, c.Validate())
	c = x.read(t)
	c.TerminalVersion = ptr(f.Version(1))
	reject(t, c.Validate())
	c = retire.Clone()
	c.Input = ptr(x.input(t))
	reject(t, c.Validate())
}
