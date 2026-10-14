package work

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// Reuse the existing controlled retained-assignee plan. This is not a real
// transition or a persisted Blocker; the separate HTTP case proves those facts.
func blockTransitionRecord(t *testing.T) (*transitionRecord, i.Actor) {
	t.Helper()
	r, actor := cancelTransitionRecord(t, c.TaskStateTodo, true, false)
	r.Key = "block-transition"
	r.Input.Request.TargetState = c.TaskStateBlocked
	var err error
	r.Semantic, err = r.Input.semantic(actor, r.Key)
	if err != nil {
		t.Fatal(err)
	}
	p := &r.Plan
	p.After.Task.State = c.TaskStateBlocked
	p.Groups[1].Group.State = c.TaskStateBlocked
	p.Target.State = c.TaskStateBlocked
	p.History[0].Payload.StateChanged.ToState = c.TaskStateBlocked
	factory, err := c.RegisterTaskTransitionEvents(event.NewCatalog())
	if err != nil {
		t.Fatal(err)
	}
	mutation, ev, err := factory.NewTaskTransitionData(p.Header, p.Before, p.After.Task, r.Input.Request, p.History, p.Source, p.Target)
	if err != nil {
		t.Fatal(err)
	}
	p.After, p.Header, p.Payload = mutation, ev.Header(), ev.PayloadBytes()
	if err = validateTransitionRecord(r, actor); err != nil {
		t.Fatal("controlled block plan rejected", err)
	}
	return r, actor
}

func TestTaskHumanBlockRetainsAssignmentAndHistory(t *testing.T) {
	r, actor := blockTransitionRecord(t)
	if transitionEdge(r.Plan.Before, r.Input.Request) != nil || !transitionRetainsAssignee(r.Plan.Before, r.Input.Request) {
		t.Fatal("mark blocked did not retain the historical assignment")
	}
	for _, change := range []func(*c.TaskTransfer){
		func(v *c.TaskTransfer) { v.AssigneeAgentID = r.Plan.Before.AssigneeAgentID },
		func(v *c.TaskTransfer) { v.AddBlockers = []c.TaskBlockerCreate{{}} },
		func(v *c.TaskTransfer) {
			v.ResolveBlockerIDs = []c.TaskBlockerID{pureID[c.TaskBlockerIdentity](t, 750)}
		},
	} {
		bad := r.Input.Request.Clone()
		change(&bad)
		pureCode(t, transitionEdge(r.Plan.Before, bad), f.DependencyUnbound)
	}
	for _, state := range []c.TaskState{c.TaskStateBacklog, c.TaskStateBlocked, c.TaskStateDone, c.TaskStateCancelled} {
		before := r.Plan.Before.Clone()
		before.State = state
		pureCode(t, transitionEdge(before, r.Input.Request), f.DependencyUnbound)
	}
	for _, state := range []c.TaskState{c.TaskStateInProgress, c.TaskStateInReview} {
		before := r.Plan.Before.Clone()
		before.State = state
		request := r.Input.Request.Clone()
		if state == c.TaskStateInReview {
			pureCode(t, transitionEdge(before, request), f.CommentRequired)
			comment := "Review requires another result"
			request.Comment = &comment
		}
		if err := transitionEdge(before, request); err != nil || !transitionRetainsAssignee(before, request) {
			t.Fatal("supported block edge lost original assignee", state, err)
		}
	}
	raw, err := json.Marshal(r.Plan)
	if err != nil {
		t.Fatal(err)
	}
	var restored transitionPlan
	if err = json.Unmarshal(raw, &restored); err != nil || !sameValue(restored, r.Plan) || restored.Agent != nil {
		t.Fatal("retained-assignee plan could not be recovered", err)
	}
	r.Plan, r.State, r.Receipt = restored, "completed", &restored.After
	if err = validateTransitionRecord(r, actor); err != nil || len(restored.History) != 1 || !sameValue(restored.Before.AssigneeAgentID, restored.After.Task.AssigneeAgentID) {
		t.Fatal("historical receipt lost original intent", err)
	}
	for _, mutate := range []func(*transitionRecord){
		func(v *transitionRecord) { v.Plan.After.Task.AssigneeAgentID = nil },
		func(v *transitionRecord) {
			v.Plan.Agent = &ac.AgentRef{ProjectID: v.Input.Project, AgentID: *v.Plan.Before.AssigneeAgentID, ConfigVersion: 1}
		},
		func(v *transitionRecord) { v.Plan.History[0].Payload.StateChanged.ToState = c.TaskStateCancelled },
	} {
		bad, who := blockTransitionRecord(t)
		mutate(bad)
		if validateTransitionRecord(bad, who) == nil {
			t.Fatal("forged block receipt accepted")
		}
	}
}

func TestTaskHumanBlockRequiresPersistentCause(t *testing.T) {
	r, _ := blockTransitionRecord(t)
	for _, tc := range []struct {
		count int64
		code  f.Code
	}{{0, f.TaskBlockedWithoutBlocker}, {-1, f.InternalError}, {2, ""}} {
		x := &unblockSQL{t: t, count: tc.count}
		resolutions, _, err := prepareTransitionResolutions(context.Background(), x, r.Input, r.Created)
		if tc.code != "" {
			pureCode(t, err, tc.code)
		} else if err != nil {
			t.Fatal(err)
		}
		if len(resolutions) != 0 || x.queries != 1 || x.writes != 0 {
			t.Fatal("blocking must read actual causes without resolving them")
		}
	}
	cause := errors.New("controlled blocker read failure")
	if _, _, err := prepareTransitionResolutions(context.Background(), &unblockSQL{t: t, err: cause}, r.Input, r.Created); !errors.Is(err, cause) {
		t.Fatal("failed read was treated as an unresolved cause", err)
	}
	// The final Outbox producer must reject an empty canonical postimage even
	// though this state-only command has no resolution rows to verify.
	x := &unblockSQL{t: t, count: 1}
	if err := verifyTransitionResolutions(context.Background(), x, r); err != nil || x.queries != 1 {
		t.Fatal("producer skipped actual unresolved count", err)
	}
	for _, tc := range []struct{ count, linked int64 }{{0, 0}, {-1, 0}, {1, 1}} {
		pureCode(t, verifyTransitionResolutions(context.Background(), &unblockSQL{t: t, count: tc.count, linked: tc.linked}, r), f.Forbidden)
	}
	writes := &cancelApplySQL{}
	if err := applyTransition(context.Background(), writes, r); err != nil {
		t.Fatal(err)
	}
	if len(writes.writes) != 4+len(r.Plan.History) || writes.writes[0].args[3] != "blocked" || writes.writes[0].args[4] != r.Plan.Before.AssigneeAgentID.String() || writes.writes[0].args[6] != int64(r.Plan.After.Task.Version) {
		t.Fatal("state-only write changed original assignment or version")
	}
	for _, write := range writes.writes {
		if strings.Contains(write.query, "task_blockers") {
			t.Fatal("marking blocked rewrote an existing cause")
		}
	}
}

type blockPending struct{ *cancelPending }

func (p *blockPending) RequireNoPendingGroupsInTx(ctx context.Context, tx f.Tx, project i.ProjectID, groups []ec.PendingClaimGroup) error {
	p.groups++
	after := p.store.task.Clone()
	after.State = c.TaskStateBlocked
	if !slices.Equal(groups, pendingGroups(groupForTask(p.store.task), groupForTask(after))) {
		return fault(f.Forbidden)
	}
	if p.mode == "group-pending" {
		return fault(f.ResourceBusy)
	}
	return p.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(project, f.Shared), taskScheduleLock(project, f.Exclusive)})
}

func TestTaskHumanBlockUsesOriginalOccupancyAndPending(t *testing.T) {
	for _, mode := range []string{"idle", "active", "pending", "group-pending"} {
		t.Run(mode, func(t *testing.T) {
			tasks, store, owner, _, actor, _ := titleUpdateFixture(t)
			store.task.State = c.TaskStateTodo
			agent := *store.task.AssigneeAgentID
			in := transitionInput{Project: store.task.ProjectID, Task: store.task.ID, User: owner.ref.OwnerUserID, Expected: store.task.Version, Request: c.TaskTransfer{TargetState: c.TaskStateBlocked}}
			store.tx = f.NewTx()
			var err error
			store.locks, err = in.locks("block-evaluation", store.task)
			if err != nil {
				t.Fatal(err)
			}
			occupancy := &cancelOccupancy{store: store, mode: mode, agent: agent, execution: pureID[i.Execution](t, 751)}
			pending := &blockPending{&cancelPending{store: store, mode: mode, agent: agent, dispatch: pureID[f.Request](t, 752).String()}}
			deps := tasks.state().deps
			factory, err := c.RegisterTaskTransitionEvents(event.NewCatalog())
			if err != nil {
				t.Fatal(err)
			}
			service, err := NewTaskTransition(store, TaskTransitionDependencies{Authority: deps.Authority, Structure: deps.Structure, Agents: &denialTransitionAgents{}, Occupancy: occupancy, Pending: pending, TaskEvents: factory, Events: deps.Events, Activity: deps.Activity})
			if err != nil {
				t.Fatal(err)
			}
			stop := errors.New("controlled rank read stop")
			x := &cancelEvalSQL{titleUpdateStore: store, stop: stop}
			r := &transitionRecord{ID: pureID[c.TaskTransitionCommand](t, 753), Input: in}
			_, err = service.evaluateTransition(context.Background(), store.tx, x, actor, r, store.task, store.task.UpdatedAt, nil, pureID[event.EventIdentity](t, 754))
			if mode == "idle" {
				if !errors.Is(err, stop) || !x.reachedRanks || pending.groups != 1 {
					t.Fatal("block evaluator bypassed gates or required a new Agent selection", err)
				}
			} else {
				pureCode(t, err, f.ResourceBusy)
				if x.reachedRanks {
					t.Fatal("occupied task reached rank mutation planning")
				}
			}
			if occupancy.calls != 1 || mode != "active" && pending.calls != 1 || store.writes != 0 {
				t.Fatal("original occupancy/pending ports bypassed or refusal wrote data")
			}
		})
	}
}
