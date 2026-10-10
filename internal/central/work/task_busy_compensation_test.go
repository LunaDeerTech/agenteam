package work

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type busyControlAuthority struct{}

func (*busyControlAuthority) RequireTaskBusyCompensationDiscoveryInTx(context.Context, f.Tx, i.Actor, c.TaskBusyCompensationRequest) (c.TaskClaimGuard, error) {
	return c.TaskClaimGuard{}, fault(f.Forbidden)
}
func (*busyControlAuthority) RequireTaskBusyCompensationInTx(context.Context, f.Tx, i.Actor, c.TaskBusyCompensationRequest, c.TaskBusyCompensationPlan) (c.TaskClaimGuard, error) {
	return c.TaskClaimGuard{}, fault(f.Forbidden)
}
func busyControlPorts(t *testing.T) (*denialStore, TaskBusyCompensationDependencies) {
	t.Helper()
	store, _, authority, _ := purePorts(t)
	events, err := c.RegisterTaskBusyCompensationEvents(event.NewCatalog())
	if err != nil {
		t.Fatal(err)
	}
	return store, TaskBusyCompensationDependencies{Authority: authority, Scheduler: &busyControlAuthority{}, Pending: &denialTransitionPending{}, Events: &denialEvents{}, CompensationEvents: events}
}
func busyControlRecord(t *testing.T) (taskBusyRecord, i.Actor) {
	t.Helper()
	claim, actor := claimControlRecord(t)
	// Give the original todo claim two logical anchors; these are real typed
	// plan inputs, not a substitute for the PG writer exercised separately.
	left, right := pureID[c.Task](t, 101), pureID[c.Task](t, 102)
	source := []rankItem{{left.String(), "1fffffffffffffffffffffffffffffff"}, {claim.Before.ID.String(), claim.Before.ManualRank}, {right.String(), "efffffffffffffffffffffffffffffff"}}
	var err error
	claim, err = buildSchedulerClaimRecord(claim.Request, claim.Before, source, 7, []rankItem{}, 1, 3, claim.History.ID, claim.Header.EventID, claim.Before.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	before := claim.After.Clone()
	// A later rank-only maintenance of in_progress may change this numeric
	// rank without changing the claimed Task's business version.
	before.ManualRank = "3fffffffffffffffffffffffffffffff"
	sprint := taskTriggerFixtureInput(t).Sprint.Clone()
	sprint.ID = before.SprintID
	sprint.ProjectID = before.ProjectID
	sprint.MilestoneID = before.MilestoneID
	sprint.State = c.Current
	at := before.UpdatedAt
	started := c.ActorHistory{Kind: i.Human, UserID: pureID[i.User](t, 1).String()}
	sprint.StartedAt = &at
	sprint.StartedBy = &started
	r := c.TaskBusyCompensationRequest{Claim: claim.Request, DispatchVersion: 3, LaunchAttempt: 1}
	groups := []taskGroupPlan{{Group: groupForTask(before), Before: []rankItem{{before.ID.String(), before.ManualRank}}, Generation: 4}, {Group: claim.Groups[0].Group, Before: slices.Clone(claim.Groups[0].After), Generation: claim.Guard.SourceOrderGeneration}}
	out, err := buildTaskBusyRecord(r, claim, before, sprint, &sprint.ID, groups, 6, pureID[c.TaskEvent](t, 103), pureID[event.EventIdentity](t, 104), before.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	return out, actor
}
func TestTaskBusyCompensationRestoresLogicalSlot(t *testing.T) {
	r, _ := busyControlRecord(t)
	if err := validateTaskBusyRecord(&r); err != nil {
		t.Fatal(err)
	}
	if !r.Restored || r.After.Version != r.Before.Version+1 || r.After.State != c.TaskStateTodo || r.After.Title != r.Before.Title || r.After.AssigneeAgentID == nil || *r.After.AssigneeAgentID != *r.Before.AssigneeAgentID || r.Event.TargetPosition.OrderGeneration != r.Claim.Guard.SourceOrderGeneration+1 {
		t.Fatal("compensation changed business facts or generations")
	}
	expected := []string{r.Claim.Guard.PredecessorID.String(), r.Before.ID.String(), r.Claim.Guard.SuccessorID.String()}
	for n, row := range r.Groups[1].After {
		if row.ID != expected[n] {
			t.Fatal("logical slot not restored")
		}
	}
	if len(r.Groups[0].After) != 0 || r.After.ManualRank == r.Before.ManualRank {
		t.Fatal("source not removed or stale in_progress rank reused")
	}
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var decoded taskBusyRecord
	if err = json.Unmarshal(raw, &decoded); err != nil || !sameValue(r, decoded) {
		t.Fatal("immutable compensation roundtrip", err)
	}
	// Force dense current ranks: rebalance maintains the current ID order and
	// returns a new rank; it does not consult the original claim's rank bytes.
	dense := []rankItem{{expected[0], strings.Repeat("0", 31) + "1"}, {expected[2], strings.Repeat("0", 31) + "2"}}
	before, err := busyRestoreBefore(dense, r.Claim.Guard.SourceOrderGeneration, r.Claim.Guard)
	if err != nil {
		t.Fatal(err)
	}
	ranked, err := rankFor(dense, r.Before.ID.String(), before, true)
	if err != nil || !ranked.Rebalanced {
		t.Fatal("dense logical slot", err)
	}
	for n, row := range ranked.Items {
		if row.ID != expected[n] {
			t.Fatal("rebalance changed logical order")
		}
	}
	for _, bad := range []struct {
		rows []rankItem
		gen  int64
	}{{dense[:1], r.Claim.Guard.SourceOrderGeneration}, {dense, r.Claim.Guard.SourceOrderGeneration + 1}, {[]rankItem{}, r.Claim.Guard.SourceOrderGeneration}} {
		if _, err = busyRestoreBefore(bad.rows, bad.gen, r.Claim.Guard); err == nil {
			t.Fatal("missing slot proof became guessed position")
		}
	}
}
func TestTaskBusyCompensationPreservesChangedTasks(t *testing.T) {
	r, _ := busyControlRecord(t)
	changed := r.Before.Clone()
	changed.Title = "User's later title"
	changed.Version++
	preserved, err := buildTaskBusyRecord(r.Request, r.Claim, changed, r.Sprint, r.CurrentSprintID, []taskGroupPlan{}, 0, c.TaskEventID{}, event.EventID{}, r.CreatedAt)
	if err != nil || preserved.Restored || !sameValue(preserved.Before, preserved.After) || preserved.History != nil || preserved.Event != nil || preserved.Header != nil || len(preserved.Groups) != 0 {
		t.Fatal("later user facts overwritten", err)
	}
	raw, err := json.Marshal(preserved)
	if err != nil {
		t.Fatal(err)
	}
	var next taskBusyRecord
	if err = json.Unmarshal(raw, &next); err != nil || !sameValue(preserved, next) {
		t.Fatal("preserved result not durable", err)
	}
	// Moving Project's current Sprint is current business state, not permission
	// to restore Task into an obsolete active scheduling context.
	preserved, err = buildTaskBusyRecord(r.Request, r.Claim, r.Before, r.Sprint, nil, []taskGroupPlan{}, 0, c.TaskEventID{}, event.EventID{}, r.CreatedAt)
	if err != nil || preserved.Restored || !sameValue(preserved.Before, preserved.After) {
		t.Fatal("changed current Sprint overwritten", err)
	}
	corrupt := r.Before.Clone()
	corrupt.Title = "same-version corruption"
	if _, err = buildTaskBusyRecord(r.Request, r.Claim, corrupt, r.Sprint, r.CurrentSprintID, r.Groups, r.QueryGeneration, r.History.ID, r.Header.EventID, r.CreatedAt); err == nil {
		t.Fatal("same version business rewrite accepted")
	}
	next = r
	next.Restored = false
	if validateTaskBusyRecord(&next) == nil {
		t.Fatal("public preserved bit replaced canonical result")
	}
}

type forgedBusyPlan struct{}

func (forgedBusyPlan) RequiredLocks() []f.LockRequest { return nil }

type forgedBusyApplied struct{}

func (forgedBusyApplied) Restored() bool { return true }
func TestTaskBusyCompensationRejectsForeignWitnessAndJoins(t *testing.T) {
	store, deps := busyControlPorts(t)
	s, err := NewTaskBusyCompensation(store, deps)
	if err != nil {
		t.Fatal(err)
	}
	var typed *busyControlAuthority
	for _, mutate := range []func(*TaskBusyCompensationDependencies){func(d *TaskBusyCompensationDependencies) { d.Scheduler = nil }, func(d *TaskBusyCompensationDependencies) { d.Scheduler = typed }, func(d *TaskBusyCompensationDependencies) { d.Pending = nil }, func(d *TaskBusyCompensationDependencies) { d.Events = nil }, func(d *TaskBusyCompensationDependencies) { d.CompensationEvents = c.TaskBusyCompensationEvents{} }} {
		bad := deps
		mutate(&bad)
		_, err = NewTaskBusyCompensation(store, bad)
		pureCode(t, err, f.DependencyUnbound)
	}
	_, err = NewTaskBusyCompensation(&denialStore{}, deps)
	pureCode(t, err, f.DependencyUnbound)
	r, actor := busyControlRecord(t)
	_, err = s.ApplyTaskBusyCompensationInTx(context.Background(), f.NewTx(), actor, r.Request, forgedBusyPlan{})
	pureCode(t, err, f.Forbidden)
	locks, _ := busyMutationLocks(r.Request, r)
	p := &taskBusyPlan{owner: s, actor: actor, request: r.Request, record: r, baseLocks: locks, locks: locks}
	alias := p.RequiredLocks()
	alias[0].Mode = f.Shared
	if sameTaskLocks(alias, p.RequiredLocks()) {
		t.Fatal("mutable lock plan")
	}
	changed := r.Request
	changed.LaunchAttempt++
	_, err = s.ApplyTaskBusyCompensationInTx(context.Background(), f.NewTx(), actor, changed, p)
	pureCode(t, err, f.Forbidden)
	err = s.CheckTaskBusyCompensationAppliedInTx(context.Background(), f.NewTx(), actor, r.Request, p, forgedBusyApplied{})
	pureCode(t, err, f.Forbidden)
	applied := &taskBusyApplied{owner: s, plan: p, tx: f.NewTx()}
	err = s.CheckTaskBusyCompensationAppliedInTx(context.Background(), f.NewTx(), actor, r.Request, p, applied)
	pureCode(t, err, f.Forbidden)
	owned, done, err := s.beginBusy(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s.Stop()
	if owned.Err() != context.Canceled || s.Joined() {
		t.Fatal("Stop released active caller")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(s.Drain(ctx), context.Canceled) {
		t.Fatal("timeout became joined")
	}
	done()
	if err = s.Drain(context.Background()); err != nil || !s.Joined() {
		t.Fatal("original call tail not joined", err)
	}
	_, err = s.DiscoverTaskBusyCompensation(context.Background(), actor, r.Request)
	pureCode(t, err, f.ShuttingDown)
	if store.touches.Load() != 0 {
		t.Fatal("forged proof reached Store")
	}
}
func TestTaskBusyCompensationTypedHistoryAndEventProof(t *testing.T) {
	r, actor := busyControlRecord(t)
	raw, err := json.Marshal(r.History)
	if err != nil {
		t.Fatal(err)
	}
	var claim c.SchedulerTaskEvent
	var human c.TaskTransitionEvent
	if json.Unmarshal(raw, &claim) == nil || json.Unmarshal(raw, &human) == nil {
		t.Fatal("busy history entered old decoder")
	}
	if _, err = decodeTaskTriggerEvent(raw); err != nil {
		t.Fatal("new typed history unreadable", err)
	}
	actorRaw, _ := json.Marshal(r.History.Actor)
	payload, _ := json.Marshal(r.History.Payload)
	op := r.Request.Claim.DispatchID
	row := taskTriggerRow{r.History.ID.String(), r.Before.ProjectID.String(), r.Before.ID.String(), r.After.Version, "state_changed", actorRaw, (*string)(nil), (*string)(nil), (*string)(nil), (*string)(nil), &op, op, payload, r.CreatedAt.Time()}
	if _, err = scanTaskTriggerEvent(row); err != nil {
		t.Fatal("compensation storage arm", err)
	}
	row[9] = &op
	if _, err = scanTaskTriggerEvent(row); err == nil {
		t.Fatal("dual claim/compensation accepted")
	}
	row[10] = (*string)(nil)
	if _, err = scanTaskTriggerEvent(row); err == nil {
		t.Fatal("compensation payload in claim arm accepted")
	}
	store, deps := busyControlPorts(t)
	s, err := NewTaskBusyCompensation(store, deps)
	if err != nil {
		t.Fatal(err)
	}
	locks, _ := busyMutationLocks(r.Request, r)
	p := &taskBusyPlan{owner: s, actor: actor, request: r.Request, record: r, baseLocks: locks, locks: locks}
	ev, err := deps.CompensationEvents.NewTaskCompensated(*r.Header, *r.Event)
	if err != nil {
		t.Fatal(err)
	}
	_, err = deps.Authority.DiscoverAppend(context.Background(), actor, ev.Summary())
	pureCode(t, err, f.Forbidden)
	ctx := context.WithValue(context.Background(), taskBusyContextKey{}, taskBusyContext{plan: p})
	prepared, err := deps.Authority.DiscoverAppend(ctx, actor, ev.Summary())
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range []oc.Stage{oc.CurrentAccess, oc.NewFact} {
		err = deps.Authority.ValidateAppendInTx(ctx, f.NewTx(), actor, ev.Summary(), prepared, stage)
		pureCode(t, err, f.Forbidden)
	}
	if store.touches.Load() != 0 {
		t.Fatal("public dependency plan replaced actual write")
	}
	catalog := event.NewCatalog()
	old1, err := c.RegisterTaskTransitionEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	old2, err := c.RegisterSchedulerClaimEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	new3, err := c.RegisterTaskBusyCompensationEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = old1.Restore(*r.Header, ev.PayloadBytes()); err == nil {
		t.Fatal("schema3 entered Human1")
	}
	if _, err = old2.Restore(*r.Header, ev.PayloadBytes()); err == nil {
		t.Fatal("schema3 entered Claim2")
	}
	if _, err = new3.Restore(*r.Header, ev.PayloadBytes()); err != nil {
		t.Fatal(err)
	}
}
