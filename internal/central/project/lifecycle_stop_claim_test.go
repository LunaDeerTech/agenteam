package project

import (
	"context"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func TestLifecycleStopClaimExactJoinAndForeignProof(t *testing.T) {
	store := &stopRoundGateStore{}
	d := stopRoundTestDriver(t, store, func(context.Context, identity.Actor, c.LifecycleCause, c.ScopeRef) error { return nil })
	process := d.state.processes.(*testProcess)
	claim := lifecycleStopClaim{project: testID[identity.Project](t), operation: testID[c.Operation](t), process: process.id, attempt: testID[struct{}](t).String(), fence: 1, phase: "running"}
	hasCode(t, d.state.canJoin(context.Background(), &claim), f.ResourceBusy)
	d.state.joined[claim.operation] = claim
	if err := d.state.canJoin(context.Background(), &claim); err != nil {
		t.Fatal(err)
	}
	changed := claim
	changed.fence++
	hasCode(t, d.state.canJoin(context.Background(), &changed), f.ResourceBusy)
	changed = claim
	changed.attempt = testID[struct{}](t).String()
	hasCode(t, d.state.canJoin(context.Background(), &changed), f.ResourceBusy)
	claim.process = testID[oc.Process](t)
	hasCode(t, d.state.canJoin(context.Background(), &claim), f.ResourceBusy)
	if process.requested != claim.process || process.calls != 1 {
		t.Fatal("foreign death identity changed")
	}
	process.stopped = true
	if err := d.state.canJoin(context.Background(), &claim); err != nil {
		t.Fatal(err)
	}
	claim.phase = "terminal"
	process.stopped = false
	if err := d.state.canJoin(context.Background(), &claim); err != nil || process.calls != 2 {
		t.Fatal("terminal work attempt requires fabricated operation completion", err)
	}
}
func TestLifecycleStopClaimCanonicalDecode(t *testing.T) {
	project, operation, process := testID[identity.Project](t), testID[c.Operation](t), testID[oc.Process](t)
	attempt := testID[struct{}](t).String()
	store := &stopRoundGateStore{row: valuesRow(project.String(), process.String(), attempt, int64(7), "running")}
	got, err := loadLifecycleStopClaim(context.Background(), store, operation)
	if err != nil || got == nil || got.project != project || got.operation != operation || got.process != process || got.attempt != attempt || got.fence != 7 || got.phase != "running" {
		t.Fatal("exact persisted claim lost", err)
	}
	for _, bad := range []struct {
		phase string
		fence int64
	}{{"completed", 7}, {"running", 0}} {
		store.row = valuesRow(project.String(), process.String(), attempt, bad.fence, bad.phase)
		_, err = loadLifecycleStopClaim(context.Background(), store, operation)
		hasCode(t, err, f.DependencyUnavailable)
	}
	store.row = valuesRow(project.String(), process.String(), "not-an-attempt", int64(1), "terminal")
	_, err = loadLifecycleStopClaim(context.Background(), store, operation)
	hasCode(t, err, f.DependencyUnavailable)
}
