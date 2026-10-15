package memory

import (
	"context"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"testing"
	"time"
)

func TestMemoryStoppedCallRequiresPrivateOriginalOwner(t *testing.T) {
	reg, _ := i.RegisterService(i.MemoryRuntime)
	a, _ := NewModelAuthority(&Service{}, reg)
	a.process = testID[oc.Process](t)
	prior, snapshot := testID[oc.Process](t), testID[mc.Snapshot](t)
	row := stoppedModelRow{Call: testID[mc.Call](t), Operation: testID[tc.Operation](t), Project: testID[i.Project](t), Agent: testID[i.Agent](t), Execution: testID[i.Execution](t), Purpose: mc.MemoryExtraction, Input: digest([]byte("input")), Snapshot: &snapshot, Process: &prior}
	r, err := row.request(a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.DiscoverStoppedCall(context.Background(), r); err == nil {
		t.Fatal("DTO granted stopped observation")
	}
	mapping, _ := row.mapping()
	w := &stoppedModelWitness{authority: a, row: row, current: a.process, mapping: mapping, request: r}
	w.active.Store(true)
	ctx := context.WithValue(context.Background(), stoppedModelKey{}, w)
	plan, err := a.DiscoverStoppedCall(ctx, r)
	if err != nil || len(plan.RequiredLocks()) != 3 || plan.Details().RetryPolicy != nil {
		t.Fatal("original owner not bound", err)
	}
	for _, mutate := range []func(*mc.StoppedCallObservationRequest){
		func(r *mc.StoppedCallObservationRequest) { r.CallID = testID[mc.Call](t) },
		func(r *mc.StoppedCallObservationRequest) { r.SnapshotID = testID[mc.Snapshot](t) },
		func(r *mc.StoppedCallObservationRequest) { r.Input.Digest = digest([]byte("other")) },
		func(r *mc.StoppedCallObservationRequest) { agent := testID[i.Agent](t); r.Consumer.AgentID = &agent },
	} {
		changed := r.Clone()
		mutate(&changed)
		if _, err = a.DiscoverStoppedCall(ctx, changed); err == nil {
			t.Fatal("original tuple changed")
		}
	}
	w.active.Store(false)
	if _, err = a.DiscoverStoppedCall(ctx, r); err == nil {
		t.Fatal("expired owner accepted")
	}
	w.active.Store(true)
	w.row.Process = &a.process
	if _, err = a.DiscoverStoppedCall(ctx, r); err == nil {
		t.Fatal("current process treated as stopped")
	}
}

func TestMemoryDrainWaitsForActualInitializationReturn(t *testing.T) {
	s := &Service{calls: map[*call]struct{}{}, changed: make(chan struct{})}
	m := &Models{domain: s, initializing: true, initializeDone: make(chan struct{})}
	if m.Joined() {
		t.Fatal("live initializer reported Joined")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := m.Drain(ctx); err == nil {
		t.Fatal("Drain discarded live initializer")
	}
	m.mu.Lock()
	m.initializing = false
	close(m.initializeDone)
	m.mu.Unlock()
	if err := m.Drain(context.Background()); err != nil || !m.Joined() {
		t.Fatal("returned initializer not joined", err)
	}
}
