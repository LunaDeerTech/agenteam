package memory

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"testing"
)

func stoppedResolutionControl(t *testing.T) (*ModelAuthority, *stoppedResolutionWitness, context.Context) {
	t.Helper()
	reg, _ := i.RegisterService(i.MemoryRuntime)
	a, _ := NewModelAuthority(&Service{}, reg)
	a.process = testID[oc.Process](t)
	process := testID[oc.Process](t)
	row := stoppedModelRow{Call: testID[mc.Call](t), Operation: testID[tc.Operation](t), Project: testID[i.Project](t), Agent: testID[i.Agent](t), Execution: testID[i.Execution](t), Purpose: mc.MemoryExtraction, Input: digest([]byte("original-input")), Process: &process}
	scope, _ := i.InProject(row.Project)
	actor, _ := reg.Actor(row.Operation.String(), scope)
	owner, _ := sc.NewCredentialLeaseOwner(sc.ModelCallOwner, row.Call.String())
	consumer := mc.Consumer{Kind: mc.MemoryConsumer, ProjectID: row.Project, AgentID: &row.Agent, OperationID: row.Operation.String(), Purpose: row.Purpose}
	original := mc.ResolveRequest{Actor: actor, Consumer: consumer, Purpose: row.Purpose, Source: mc.CurrentSelectionSource, Selection: &mc.SelectionRef{Kind: "platform", Selector: mc.SummarySelector}, LeaseOwner: owner}
	raw, binding, err := captureModelResolution(original)
	if err != nil {
		t.Fatal(err)
	}
	row.ResolveSource, row.ResolveBinding = raw, &binding
	r, err := row.resolutionRequest(a)
	if err != nil {
		t.Fatal(err)
	}
	if r.Resolve.Selection.Version != nil {
		t.Fatal("nil original selection version replaced")
	}
	mapping, _ := row.mapping()
	w := &stoppedResolutionWitness{authority: a, row: row, request: r, mapping: mapping, current: a.process}
	w.active.Store(true)
	return a, w, context.WithValue(context.Background(), stoppedResolutionKey{}, w)
}

func TestMemoryStoppedResolutionBindsOriginalSourceAndNeverGrantsResolve(t *testing.T) {
	a, w, ctx := stoppedResolutionControl(t)
	if _, err := a.DiscoverStoppedResolution(context.Background(), w.request); err == nil {
		t.Fatal("bare original DTO granted cleanup")
	}
	plan, err := a.DiscoverStoppedResolution(ctx, w.request)
	if err != nil || plan.Details().RetryPolicy != nil || len(plan.RequiredLocks()) != 3 {
		t.Fatal("private resolution owner rejected", err)
	}
	for _, mutate := range []func(*mc.StoppedResolutionRequest){
		func(r *mc.StoppedResolutionRequest) { r.PreparedProcessID = testID[oc.Process](t) },
		func(r *mc.StoppedResolutionRequest) { v := f.Version(1); r.Resolve.Selection.Version = &v },
		func(r *mc.StoppedResolutionRequest) { r.Input.Digest = digest([]byte("other")) },
		func(r *mc.StoppedResolutionRequest) { r.CallID = testID[mc.Call](t) },
	} {
		r := w.request.Clone()
		mutate(&r)
		if _, err = a.DiscoverStoppedResolution(ctx, r); err == nil {
			t.Fatal("resolution identity changed")
		}
	}
	if _, err = a.Discover(ctx, mc.ConsumerRequest{Action: mc.ResolveConsumer, Actor: w.request.Resolve.Actor, Consumer: w.request.Resolve.Consumer, LeaseOwner: w.request.Resolve.LeaseOwner}); err == nil {
		t.Fatal("stopped observation granted Resolve")
	}
	row := w.row
	row.ResolveSource = nil
	row.ResolveBinding = nil
	if _, err = row.resolutionRequest(a); err == nil {
		t.Fatal("legacy missing source reconstructed current selection")
	}
	w.active.Store(false)
	if _, err = a.DiscoverStoppedResolution(ctx, w.request); err == nil {
		t.Fatal("expired resolution owner accepted")
	}
}

type unsettledResolutionObserver struct{ calls int }

func (*unsettledResolutionObserver) LookupStoppedCall(context.Context, mc.StoppedCallObservationRequest) (mc.StoppedCallObservation, error) {
	return mc.StoppedCallObservation{}, nil
}
func (o *unsettledResolutionObserver) LookupStoppedResolution(context.Context, mc.StoppedResolutionRequest) (mc.StoppedResolutionObservation, error) {
	o.calls++
	return mc.StoppedResolutionObservation{}, nil
}
func TestMemoryStoppedResolutionZeroValueIsNotSettledAbsence(t *testing.T) {
	a, w, _ := stoppedResolutionControl(t)
	observer := &unsettledResolutionObserver{}
	m := &Models{authority: a, deps: ModelDependencies{Stopped: observer}}
	// No Store/retirement double is installed: the unresolved observation
	// must reject before any domain mutation or Secret retirement can occur.
	requireFault(t, m.recoverStoppedResolution(context.Background(), w.row), f.ResourceBusy)
	if observer.calls != 1 {
		t.Fatal("original observation was repeated")
	}
}
