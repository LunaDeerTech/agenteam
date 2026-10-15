package usage

import (
	"context"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

func stoppedUsageRequest(t *testing.T, fixture *usageOwnerFixture) uc.StoppedInvocationRequest {
	t.Helper()
	r := usageModelRequest(t, fixture.p)
	at, _ := f.NewInstant(time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC))
	a := mc.AttemptIdentity{CallID: r.CallID, InvocationID: usageID[mc.Invocation](t, 100), AttemptIndex: 1, ProcessID: usageID[oc.Process](t, 101), Fence: 1}
	v := uc.Invocation{ID: a.InvocationID, CallID: a.CallID, AttemptIndex: a.AttemptIndex, Consumer: r.Consumer.Clone(), SnapshotID: r.Model.Snapshot.ID, Identity: r.Model.Snapshot.Identity, ProcessID: a.ProcessID, Fence: a.Fence, Dispatch: uc.Sent, StartedAt: at, DispatchedAt: &at, Usage: mc.Usage{Source: mc.UnknownUsage}}
	request := uc.StoppedInvocationRequest{Identity: uc.InvocationIdentity{Attempt: a, Consumer: r.Consumer.Clone(), SnapshotID: r.Model.Snapshot.ID, Input: r.Input.Clone()}, BeforeSequence: 3, Before: v, AfterSequence: 4, After: v.Clone()}
	request.After.Final = &uc.Final{Status: uc.Unknown, Version: 1, FinishedAt: at}
	if err := request.Validate(); err != nil {
		t.Fatal(err)
	}
	return request
}

// This provider deliberately never installs a Model recovery witness. A
// syntactically valid request cannot get as far as reading or closing a row.
type stoppedDeniedFacts struct {
	usageInvocationFacts
	discoveries int
}

func (v *stoppedDeniedFacts) DiscoverStoppedInvocationFacts(context.Context, uc.StoppedInvocationRequest) (uc.InvocationDependencies, error) {
	v.discoveries++
	return uc.InvocationDependencies{}, fault(f.Forbidden)
}
func (*stoppedDeniedFacts) ValidateStoppedInvocationFactsInTx(context.Context, f.Tx, uc.StoppedInvocationRequest, uc.InvocationDependencies) error {
	return fault(f.Forbidden)
}

func TestStoppedInvocationRejectsUnboundAndBareRequests(t *testing.T) {
	facts := &stoppedDeniedFacts{}
	v := newUsageOwner(t, facts)
	r := stoppedUsageRequest(t, v)
	_, err := v.pair.Service().DiscoverStoppedInvocation(context.Background(), r)
	expectFault(t, err, f.Forbidden)
	if facts.discoveries != 1 {
		t.Fatal("request bypassed original Model facts")
	}
	current := r.Clone()
	current.Identity.Attempt.ProcessID = v.proof.process
	current.Before.ProcessID, current.After.ProcessID = v.proof.process, v.proof.process
	_, err = v.pair.Service().DiscoverStoppedInvocation(context.Background(), current)
	expectFault(t, err, f.ResourceBusy)
	if facts.discoveries != 1 {
		t.Fatal("current process reached foreign recovery")
	}
	if calls, reads, writes := v.store.counts(); calls != 0 || reads != 0 || writes != 0 {
		t.Fatal("bare recovery request accessed storage")
	}
	u := newUsageOwner(t, nil)
	_, err = u.pair.Service().DiscoverStoppedInvocation(context.Background(), r)
	expectFault(t, err, f.DependencyUnbound)
}

func TestStoppedInvocationPlanBindsCompleteBorrowerSet(t *testing.T) {
	v := newUsageOwner(t, nil)
	r := stoppedUsageRequest(t, v)
	binding, _ := uc.StoppedInvocationBinding(r)
	locks := localLocks(stoppedIdentity(r))
	factIssuer := uc.NewPlanIssuer()
	deps, err := uc.NewInvocationDependencies(factIssuer, uc.InvocationDependencyDetails{Binding: binding, Mapping: hash([]byte("private-model-original")), Locks: locks})
	if err != nil {
		t.Fatal(err)
	}
	row := projectCallRecord{ID: usageID[projectCallID](t, 102), Project: r.Identity.Consumer.ProjectID, Process: r.Identity.Attempt.ProcessID, Instance: usageID[projectCallInstance](t, 103), Lane: projectCallInvocation, CallID: r.Identity.Attempt.CallID.String(), Phase: "running", Registered: "2026-10-13T00:00:00Z"}
	original := stoppedMapping(deps, locks, []projectCallRecord{row})
	for _, mutate := range []func(*projectCallRecord){
		func(v *projectCallRecord) { v.ID = usageID[projectCallID](t, 104) },
		func(v *projectCallRecord) { v.Instance = usageID[projectCallInstance](t, 105) },
		func(v *projectCallRecord) { v.Process = usageID[oc.Process](t, 106) },
		func(v *projectCallRecord) { v.Project = usageID[i.Project](t, 107) },
		func(v *projectCallRecord) { v.Lane = projectCallSummary },
		func(v *projectCallRecord) { v.CallID = usageID[mc.Call](t, 108).String() },
		func(v *projectCallRecord) { v.Registered = "2026-10-13T00:00:01Z" },
	} {
		changed := row
		mutate(&changed)
		if original == stoppedMapping(deps, locks, []projectCallRecord{changed}) {
			t.Fatal("borrower identity escaped original plan binding")
		}
	}
	if original == stoppedMapping(deps, locks, nil) || original == stoppedMapping(deps, locks, []projectCallRecord{row, row}) {
		t.Fatal("changed borrower set reused the original plan")
	}
	row.Phase, row.Closed = "business_closed", "2026-10-13T00:00:01Z"
	if original != stoppedMapping(deps, locks, []projectCallRecord{row}) {
		t.Fatal("original plan cannot observe its own monotonic closure")
	}
	plan, err := uc.NewStoppedInvocationPlan(v.pair.Service().state().issuer, uc.InvocationPlanDetails{Binding: binding, Mapping: original, Locks: locks, Facts: deps, Cause: invocationCause(stoppedIdentity(r))})
	if err != nil || !plan.Matches(v.pair.Service().state().issuer, binding, original) || plan.Matches(uc.NewPlanIssuer(), binding, original) {
		t.Fatal("recovery plan lost issuer isolation", err)
	}
}
