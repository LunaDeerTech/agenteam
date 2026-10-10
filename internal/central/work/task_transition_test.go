package work

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type denialTransitionAgents struct{}

func (*denialTransitionAgents) RequireCurrentInTx(context.Context, f.Tx, i.Actor, i.ProjectID, i.AgentID) (ac.AgentRef, error) {
	return ac.AgentRef{}, fault(f.Forbidden)
}

type denialTransitionOccupancy struct{}

func (*denialTransitionOccupancy) ReadInTx(context.Context, f.Tx, i.ProjectID, []string) (ec.ExecutionOccupancy, error) {
	return ec.ExecutionOccupancy{}, fault(f.Forbidden)
}

type denialTransitionPending struct{}

func (*denialTransitionPending) ReadInTx(context.Context, f.Tx, i.ProjectID, []string) (ec.DispatchOccupancy, error) {
	return ec.DispatchOccupancy{}, fault(f.Forbidden)
}
func (*denialTransitionPending) RequireNoPendingGroupsInTx(context.Context, f.Tx, i.ProjectID, []ec.PendingClaimGroup) error {
	return fault(f.Forbidden)
}
func pureTransitionPorts(t *testing.T) (*denialStore, TaskTransitionDependencies) {
	t.Helper()
	store, _, _, old := pureTaskPorts(t)
	events, err := c.RegisterTaskTransitionEvents(event.NewCatalog())
	if err != nil {
		t.Fatal(err)
	}
	return store, TaskTransitionDependencies{Structure: old.Structure, Authority: old.Authority, TaskEvents: events, Events: old.Events, Activity: old.Activity, Agents: &denialTransitionAgents{}, Occupancy: &denialTransitionOccupancy{}, Pending: &denialTransitionPending{}}
}
func TestTaskTransitionConstructorAndOwnedRetirement(t *testing.T) {
	store, deps := pureTransitionPorts(t)
	service, err := NewTaskTransition(store, deps)
	if err != nil {
		t.Fatal(err)
	}
	var typed *denialTransitionPending
	for _, change := range []func(*TaskTransitionDependencies){func(d *TaskTransitionDependencies) { d.Agents = nil }, func(d *TaskTransitionDependencies) { d.Occupancy = nil }, func(d *TaskTransitionDependencies) { d.Pending = nil }, func(d *TaskTransitionDependencies) { d.Pending = typed }, func(d *TaskTransitionDependencies) { d.TaskEvents = c.TaskTransitionEvents{} }} {
		bad := deps
		change(&bad)
		_, err = NewTaskTransition(store, bad)
		pureCode(t, err, f.DependencyUnbound)
	}
	run, entry, done, err := service.begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	confirm, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.state().mu.Lock()
	entry.confirmations[&confirmation{cancel: cancel}] = struct{}{}
	service.state().mu.Unlock()
	service.Stop()
	if run.Err() != context.Canceled || confirm.Err() != context.Canceled {
		t.Fatal("original calls not canceled")
	}
	ctx, stop := context.WithCancel(context.Background())
	stop()
	if !errors.Is(service.Drain(ctx), context.Canceled) {
		t.Fatal("timeout substituted for join")
	}
	done()
	if err = service.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, _, _, err = service.begin(context.Background())
	pureCode(t, err, f.ShuttingDown)
	if store.touches.Load() != 0 {
		t.Fatal("constructor or retirement used shared Store")
	}
}
func pureTransitionRecord(t *testing.T) (*transitionRecord, i.Actor, event.Summary) {
	t.Helper()
	old, actor, _ := pureTaskRecord(t)
	before := old.Plan.After.Task.Clone()
	agent := pureID[i.Agent](t, 20)
	comment := "private transition comment"
	in := transitionInput{Project: before.ProjectID, Task: before.ID, User: old.User, Expected: before.Version, Request: c.TaskTransfer{TargetState: c.TaskStateTodo, AssigneeAgentID: &agent, Comment: &comment, AddBlockers: []c.TaskBlockerCreate{}, ResolveBlockerIDs: []c.TaskBlockerID{}}}
	r := &transitionRecord{ID: pureID[c.TaskTransitionCommand](t, 21), Key: "transition-key", Input: in, Revision: 1, State: "planned", Created: before.UpdatedAt}
	var err error
	r.Semantic, err = in.semantic(actor, r.Key)
	if err != nil {
		t.Fatal(err)
	}
	after := before.Clone()
	after.State = c.TaskStateTodo
	after.AssigneeAgentID = &agent
	after.Version++
	source := groupForTask(before)
	target := groupForTask(after)
	sp, _ := transitionPosition(source, 3, "", "")
	tp, _ := transitionPosition(target, 2, "", "")
	ids := []c.TaskEventID{pureID[c.TaskEvent](t, 22), pureID[c.TaskEvent](t, 23), pureID[c.TaskEvent](t, 24)}
	history := make([]c.TaskTransitionEvent, 3)
	kinds := []c.TaskTransitionEventType{c.TaskTransitionStateChanged, c.TaskTransitionAssigneeChanged, c.TaskTransitionComment}
	payloads := []c.TaskTransitionFactPayload{{StateChanged: &c.TaskStateChangedPayload{FromState: before.State, ToState: after.State}}, {AssigneeChanged: &c.TaskAssigneeChangedPayload{ToAgentID: agent}}, {Comment: &c.TaskCommentPayload{Body: comment}}}
	for n := range history {
		history[n] = c.TaskTransitionEvent{ID: ids[n], ProjectID: before.ProjectID, TaskID: before.ID, TaskVersion: after.Version, Type: kinds[n], Actor: c.TaskTransitionActor{UserID: in.User}, OperationID: r.ID, CorrelationID: r.ID, Payload: payloads[n], CreatedAt: after.UpdatedAt}
	}
	header, err := taskHeader(pureID[event.EventIdentity](t, 25), after, after.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	header.EventType = c.TaskTransitionedName
	header.SchemaVersion = c.TaskTransitionSchemaVersion
	factory, err := c.RegisterTaskTransitionEvents(event.NewCatalog())
	if err != nil {
		t.Fatal(err)
	}
	mutation, ev, err := factory.NewTaskTransitionData(header, before, after, in.Request, history, sp, tp)
	if err != nil {
		t.Fatal(err)
	}
	r.Plan = transitionPlan{Before: before, After: mutation, Placement: old.Plan.Placement, Agent: ac.AgentRef{ProjectID: before.ProjectID, AgentID: agent, ConfigVersion: 1}, Groups: []taskGroupPlan{{source, []rankItem{{before.ID.String(), before.ManualRank}}, []rankItem{}, 2}, {target, []rankItem{}, []rankItem{{after.ID.String(), after.ManualRank}}, 1}}, QueryGeneration: 2, History: history, Source: sp, Target: tp, Header: header, Payload: ev.PayloadBytes()}
	if err = validateTransitionRecord(r, actor); err != nil {
		t.Fatal("coherent transition plan", err)
	}
	return r, actor, ev.Summary()
}
func TestTaskTransitionPlanBindsOriginalIntentAndPostimage(t *testing.T) {
	r, actor, summary := pureTransitionRecord(t)
	binding, locks, opaque, err := transitionEventBinding(r, actor, summary)
	if err != nil {
		t.Fatal(err)
	}
	issuer := oc.NewPlanIssuer()
	deps, err := oc.NewDependencies(issuer, binding, locks, opaque)
	if err != nil || !deps.Matches(issuer, binding) {
		t.Fatal(err)
	}
	other, _, _, err := transitionEventBinding(r, pureActor(t, 3), summary)
	if err != nil || other == binding {
		t.Fatal("Session not bound", err)
	}
	raw, err := canonical(r.Plan)
	if err != nil {
		t.Fatal(err)
	}
	var decoded transitionPlan
	if err = json.Unmarshal(raw, &decoded); err != nil || !sameValue(decoded, r.Plan) {
		t.Fatal("persisted roundtrip", err)
	}
	for name, mutate := range map[string]func(*transitionRecord){"body": func(r *transitionRecord) { r.Plan.After.Task.Title = "forged" }, "agent": func(r *transitionRecord) { r.Plan.Agent.AgentID = pureID[i.Agent](t, 99) }, "generation": func(r *transitionRecord) { r.Plan.Source.OrderGeneration++ }, "history": func(r *transitionRecord) { r.Plan.History[2].Payload.Comment.Body = "forged" }, "rank": func(r *transitionRecord) { r.Plan.Groups[1].After[0].Rank = "3fffffffffffffffffffffffffffffff" }} {
		r, actor, _ := pureTransitionRecord(t)
		mutate(r)
		if validateTransitionRecord(r, actor) == nil {
			t.Fatal(name, "plan forgery accepted")
		}
	}
	if strings.Contains(fmt.Sprintf("%+v %#+v", r, r.Plan), "private") {
		t.Fatal("private input exposed")
	}
}

type foreignTransitionPlan struct{}

func (foreignTransitionPlan) RequiredLocks() []f.LockRequest { return nil }
func TestTaskTransitionOpaquePlanAndCancellationRejectBeforeSQL(t *testing.T) {
	store, deps := pureTransitionPorts(t)
	s, err := NewTaskTransition(store, deps)
	if err != nil {
		t.Fatal(err)
	}
	_, actor, _ := pureTransitionRecord(t)
	if _, err = s.TransferTaskInTx(context.Background(), f.NewTx(), actor, foreignTransitionPlan{}); err == nil {
		t.Fatal("foreign plan accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = s.PrepareTaskTransition(ctx, actor, f.CommandMeta{}, c.ProjectID{}, c.TaskID{}, c.TaskTransfer{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if store.touches.Load() != 0 {
		t.Fatal("invalid request touched SQL")
	}
	if _, err = transitionNext(math.MaxInt64); err == nil {
		t.Fatal("overflow accepted")
	}
	g := taskGroup{Sprint: pureID[pc.Sprint](t, 6), State: c.TaskStateBacklog, Priority: c.TaskPriorityHigh}
	other := g
	other.State = c.TaskStateTodo
	groups := pendingGroups(other, g, g)
	if len(groups) != 2 || groups[0].State != "backlog" || groups[1].State != "todo" {
		t.Fatal("full deduplicated source groups")
	}
}
func TestTaskTransitionHistoryRemainsReadableWithoutGrant(t *testing.T) {
	r, _, _ := pureTransitionRecord(t)
	for _, h := range r.Plan.History {
		raw, err := json.Marshal(h)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil {
			t.Fatal("wire")
		}
		op := h.OperationID.String()
		row := taskTriggerRow{h.ID.String(), h.ProjectID.String(), h.TaskID.String(), h.TaskVersion, string(h.Type), []byte(fields["actor"]), (*string)(nil), (*string)(nil), &op, (*string)(nil), (*string)(nil), h.CorrelationID.String(), []byte(fields["payload"]), h.CreatedAt.Time()}
		out, err := scanTaskTriggerEvent(row)
		if err != nil {
			t.Fatal("real storage arm", err)
		}
		fact, err := decodeTaskTriggerEvent(out)
		if err != nil || fact.version != h.TaskVersion || fact.task != h.TaskID {
			t.Fatal(err)
		}
		bad := slices.Clone(row)
		bad[6] = &op
		if _, err = scanTaskTriggerEvent(bad); err == nil {
			t.Fatal("mixed original operation arms")
		}
	}
}
