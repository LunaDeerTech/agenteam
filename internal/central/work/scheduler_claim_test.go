package work

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type claimControlAuthority struct {
	discovery func(context.Context) error
	calls     int
}

func (p *claimControlAuthority) RequireTaskClaimDiscoveryInTx(ctx context.Context, _ f.Tx, _ i.Actor, _ c.TaskClaimRequest) error {
	p.calls++
	if p.discovery != nil {
		return p.discovery(ctx)
	}
	return fault(f.Forbidden)
}
func (p *claimControlAuthority) RequireTaskClaimInTx(context.Context, f.Tx, i.Actor, c.TaskClaimRequest, c.TaskClaimPlan) error {
	p.calls++
	return fault(f.Forbidden)
}

type claimControlAgent struct{}

func (*claimControlAgent) RequireSchedulerCurrentInTx(context.Context, f.Tx, i.Actor, i.ProjectID, i.AgentID) (ac.AgentRef, error) {
	return ac.AgentRef{}, fault(f.Forbidden)
}

func claimControlPorts(t *testing.T) (*denialStore, SchedulerClaimDependencies) {
	t.Helper()
	store, _, authority, _ := purePorts(t)
	events, err := c.RegisterSchedulerClaimEvents(event.NewCatalog())
	if err != nil {
		t.Fatal(err)
	}
	return store, SchedulerClaimDependencies{Authority: authority, Scheduler: &claimControlAuthority{}, Agents: &claimControlAgent{}, Pending: &denialTransitionPending{}, Occupancy: &denialTransitionOccupancy{}, Events: &denialEvents{}, ClaimEvents: events}
}
func claimControlRecord(t *testing.T) (schedulerClaimRecord, i.Actor) {
	t.Helper()
	human, _, _ := pureTransitionRecord(t)
	before := human.Plan.After.Task.Clone()
	r := c.TaskClaimRequest{ProjectID: before.ProjectID, TaskID: before.ID, AgentID: *before.AssigneeAgentID, DispatchID: pureID[c.SchedulerClaim](t, 81).String(), ExpectedTaskVersion: before.Version, CurrentSprintID: before.SprintID, Purpose: "task/work", RequestID: pureID[f.Request](t, 82)}
	reg, err := i.RegisterService(i.Scheduler)
	if err != nil {
		t.Fatal(err)
	}
	scope, _ := i.InProject(r.ProjectID)
	actor, err := reg.Actor(r.DispatchID, scope)
	if err != nil {
		t.Fatal(err)
	}
	record, err := buildSchedulerClaimRecord(r, before, []rankItem{{before.ID.String(), before.ManualRank}}, 7, []rankItem{}, 1, 3, pureID[c.TaskEvent](t, 83), pureID[event.EventIdentity](t, 84), before.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	return record, actor
}

func TestSchedulerClaimConstructorAndOriginalCallJoin(t *testing.T) {
	store, deps := claimControlPorts(t)
	service, err := NewSchedulerClaim(store, deps)
	if err != nil {
		t.Fatal(err)
	}
	var typed *claimControlAuthority
	for _, change := range []func(*SchedulerClaimDependencies){func(d *SchedulerClaimDependencies) { d.Scheduler = nil }, func(d *SchedulerClaimDependencies) { d.Scheduler = typed }, func(d *SchedulerClaimDependencies) { d.Agents = nil }, func(d *SchedulerClaimDependencies) { d.Pending = nil }, func(d *SchedulerClaimDependencies) { d.Occupancy = nil }, func(d *SchedulerClaimDependencies) { d.Events = nil }, func(d *SchedulerClaimDependencies) { d.ClaimEvents = c.SchedulerClaimEvents{} }} {
		bad := deps
		change(&bad)
		_, err = NewSchedulerClaim(store, bad)
		pureCode(t, err, f.DependencyUnbound)
	}
	_, err = NewSchedulerClaim(&denialStore{}, deps)
	pureCode(t, err, f.DependencyUnbound)
	ctx, done, err := service.beginClaim(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	service.Stop()
	if ctx.Err() != context.Canceled || service.Joined() {
		t.Fatal("stop forgot original call")
	}
	wait, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(service.Drain(wait), context.Canceled) {
		t.Fatal("timeout became join")
	}
	done()
	if err = service.Drain(context.Background()); err != nil || !service.Joined() {
		t.Fatal("original call did not retire", err)
	}
	if store.touches.Load() != 0 {
		t.Fatal("constructor/lifetime performed SQL")
	}
}

func TestSchedulerClaimPlanRanksAndStrictHistory(t *testing.T) {
	r, _ := claimControlRecord(t)
	if err := validateSchedulerClaimRecord(&r); err != nil {
		t.Fatal(err)
	}
	if r.After.Version != r.Before.Version+1 || r.After.State != c.TaskStateInProgress || r.After.AssigneeAgentID == nil || *r.After.AssigneeAgentID != *r.Before.AssigneeAgentID || r.Guard.SourceOrderGeneration != 8 || r.Event.TargetPosition.OrderGeneration != 2 || len(r.Groups[0].After) != 0 {
		t.Fatal("claim mutation or source slot differs")
	}
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var decoded schedulerClaimRecord
	if err = json.Unmarshal(raw, &decoded); err != nil || !sameValue(r, decoded) {
		t.Fatal("immutable record roundtrip", err)
	}
	for _, mutate := range []func(*schedulerClaimRecord){func(r *schedulerClaimRecord) { r.After.Title = "changed" }, func(r *schedulerClaimRecord) { r.After.Version++ }, func(r *schedulerClaimRecord) { r.Guard.SourceOrderGeneration-- }, func(r *schedulerClaimRecord) { r.History.Actor.CauseID = pureID[c.SchedulerClaim](t, 85).String() }, func(r *schedulerClaimRecord) { r.Groups[0].Generation = math.MaxInt64 }} {
		var next schedulerClaimRecord
		if json.Unmarshal(raw, &next) != nil {
			t.Fatal("restore")
		}
		mutate(&next)
		if validateSchedulerClaimRecord(&next) == nil {
			t.Fatal("changed canonical claim accepted")
		}
	}
	history, err := json.Marshal(r.History)
	if err != nil {
		t.Fatal(err)
	}
	var human c.TaskTransitionEvent
	if json.Unmarshal(history, &human) == nil {
		t.Fatal("Scheduler history entered Human decoder")
	}
	identity, err := decodeTaskTriggerEvent(history)
	if err != nil || identity.task != r.Request.TaskID {
		t.Fatal("typed scheduler history unreadable", err)
	}
	actor, _ := json.Marshal(r.History.Actor)
	payload, _ := json.Marshal(r.History.Payload)
	op := r.Request.DispatchID
	row := taskTriggerRow{r.History.ID.String(), r.Request.ProjectID.String(), r.Request.TaskID.String(), r.After.Version, "state_changed", actor, (*string)(nil), (*string)(nil), (*string)(nil), &op, op, payload, r.After.UpdatedAt.Time()}
	if _, err = scanTaskTriggerEvent(row); err != nil {
		t.Fatal("claim storage arm", err)
	}
	row[8] = &op
	if _, err = scanTaskTriggerEvent(row); err == nil {
		t.Fatal("dual claim/transition arm accepted")
	}
	row[8] = (*string)(nil)
	old, _, _ := pureTransitionRecord(t)
	row[5], _ = json.Marshal(old.Plan.History[0].Actor)
	if _, err = scanTaskTriggerEvent(row); err == nil {
		t.Fatal("Human actor in claim storage accepted")
	}
}

type forgedClaimPlan struct{}

func (forgedClaimPlan) RequiredLocks() []f.LockRequest { return []f.LockRequest{} }

type forgedAppliedClaim struct{ guard c.TaskClaimGuard }

func (v forgedAppliedClaim) Guard() c.TaskClaimGuard { return v.guard.Clone() }

func TestSchedulerClaimRejectsForeignProofBeforeSQL(t *testing.T) {
	store, deps := claimControlPorts(t)
	service, err := NewSchedulerClaim(store, deps)
	if err != nil {
		t.Fatal(err)
	}
	record, actor := claimControlRecord(t)
	r := record.Request
	_, err = service.ApplyTaskClaimInTx(context.Background(), f.NewTx(), actor, r, forgedClaimPlan{})
	pureCode(t, err, f.Forbidden)
	base, err := claimMutationLocks(r, record.Before)
	if err != nil {
		t.Fatal(err)
	}
	p := &schedulerClaimPlan{owner: service, actor: actor, request: r, record: record, baseLocks: base, locks: base}
	alias := p.RequiredLocks()
	alias[0].Mode = f.Shared
	if sameTaskLocks(alias, p.RequiredLocks()) {
		t.Fatal("caller mutated plan locks")
	}
	changed := r
	changed.RequestID = pureID[f.Request](t, 90)
	_, err = service.ApplyTaskClaimInTx(context.Background(), f.NewTx(), actor, changed, p)
	pureCode(t, err, f.Forbidden)
	other, _ := NewSchedulerClaim(store, deps)
	_, err = other.ApplyTaskClaimInTx(context.Background(), f.NewTx(), actor, r, p)
	pureCode(t, err, f.Forbidden)
	err = service.CheckTaskClaimAppliedInTx(context.Background(), f.NewTx(), actor, r, p, forgedAppliedClaim{record.Guard})
	pureCode(t, err, f.Forbidden)
	if store.touches.Load() != 0 {
		t.Fatal("forged proof reached Store")
	}
	// Even a valid private plan may not borrow a different physical Tx.
	original := f.NewTx()
	applied := &schedulerAppliedClaim{owner: service, plan: p, tx: original}
	err = service.CheckTaskClaimAppliedInTx(context.Background(), f.NewTx(), actor, r, p, applied)
	pureCode(t, err, f.Forbidden)
	service.Stop()
	_, err = service.DiscoverTaskClaim(context.Background(), actor, r)
	pureCode(t, err, f.ShuttingDown)
}

func TestSchedulerClaimEventRequiresPrivateAppliedWitness(t *testing.T) {
	store, deps := claimControlPorts(t)
	service, err := NewSchedulerClaim(store, deps)
	if err != nil {
		t.Fatal(err)
	}
	r, actor := claimControlRecord(t)
	base, _ := claimMutationLocks(r.Request, r.Before)
	p := &schedulerClaimPlan{owner: service, actor: actor, request: r.Request, record: r, baseLocks: base, locks: base}
	ev, err := deps.ClaimEvents.NewTaskClaimed(r.Header, r.Event)
	if err != nil {
		t.Fatal(err)
	}
	_, err = deps.Authority.DiscoverAppend(context.Background(), actor, ev.Summary())
	pureCode(t, err, f.Forbidden)
	ctx := context.WithValue(context.Background(), schedulerClaimContextKey{}, schedulerClaimContext{plan: p})
	prepared, err := deps.Authority.DiscoverAppend(ctx, actor, ev.Summary())
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range []oc.Stage{oc.CurrentAccess, oc.NewFact} {
		err = deps.Authority.ValidateAppendInTx(ctx, f.NewTx(), actor, ev.Summary(), prepared, stage)
		pureCode(t, err, f.Forbidden)
	}
	if store.touches.Load() != 0 {
		t.Fatal("dependency DTO became actual writer")
	}
	catalog := event.NewCatalog()
	human, err := c.RegisterTaskTransitionEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := c.RegisterSchedulerClaimEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = human.Restore(r.Header, ev.PayloadBytes()); err == nil {
		t.Fatal("schema2 entered Human schema1")
	}
	if _, err = claim.Restore(r.Header, ev.PayloadBytes()); err != nil {
		t.Fatal(err)
	}
	header := r.Header
	header.SchemaVersion = 3
	if _, err = claim.Restore(header, ev.PayloadBytes()); err == nil {
		t.Fatal("unknown claim schema accepted")
	}
}
