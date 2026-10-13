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
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

// Controlled plans and ports below do not create a real claim, Session grant
// or physical commit. The separate HTTP/browser fixture proves those boundaries.
func cancelTransitionRecord(t *testing.T, from c.TaskState, assigned, comment bool) (*transitionRecord, i.Actor) {
	t.Helper()
	old, actor, _ := pureTaskRecord(t)
	before := old.Plan.After.Task.Clone()
	before.State = from
	if assigned {
		agent := pureID[i.Agent](t, 710)
		before.AssigneeAgentID = &agent
	}
	request := c.TaskTransfer{TargetState: c.TaskStateCancelled, AddBlockers: []c.TaskBlockerCreate{}, ResolveBlockerIDs: []c.TaskBlockerID{}}
	if comment {
		body := "  Cancel this task.\nKeep exact bytes.\t"
		request.Comment = &body
	}
	in := transitionInput{Project: before.ProjectID, Task: before.ID, User: old.User, Expected: before.Version, Request: request}
	r := &transitionRecord{ID: pureID[c.TaskTransitionCommand](t, 711), Key: "cancel-transition", Input: in, Revision: 1, State: "planned", Created: before.UpdatedAt}
	var err error
	r.Semantic, err = in.semantic(actor, r.Key)
	if err != nil {
		t.Fatal(err)
	}
	after := before.Clone()
	after.State, after.Version = c.TaskStateCancelled, before.Version+1
	ranks, err := rankFor([]rankItem{}, after.ID.String(), "", true)
	if err != nil {
		t.Fatal(err)
	}
	after.ManualRank = ranks.Rank
	source, target := groupForTask(before), groupForTask(after)
	sp, err := transitionPosition(source, 3, "", "")
	if err != nil {
		t.Fatal(err)
	}
	tp, err := transitionPosition(target, 2, "", "")
	if err != nil {
		t.Fatal(err)
	}
	history := []c.TaskTransitionEvent{{Type: c.TaskTransitionStateChanged, Payload: c.TaskTransitionFactPayload{StateChanged: &c.TaskStateChangedPayload{FromState: from, ToState: c.TaskStateCancelled}}}}
	if comment {
		history = append(history, c.TaskTransitionEvent{Type: c.TaskTransitionComment, Payload: c.TaskTransitionFactPayload{Comment: &c.TaskCommentPayload{Body: *request.Comment}}})
	}
	for n := range history {
		history[n].ID = pureID[c.TaskEvent](t, 720+n)
		history[n].ProjectID, history[n].TaskID, history[n].TaskVersion = in.Project, in.Task, after.Version
		history[n].Actor = c.TaskTransitionActor{UserID: in.User}
		history[n].OperationID, history[n].CorrelationID, history[n].CreatedAt = r.ID, r.ID, after.UpdatedAt
	}
	header, err := taskHeader(pureID[event.EventIdentity](t, 730), after, after.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	header.EventType, header.SchemaVersion = c.TaskTransitionedName, c.TaskTransitionSchemaVersion
	factory, err := c.RegisterTaskTransitionEvents(event.NewCatalog())
	if err != nil {
		t.Fatal(err)
	}
	mutation, ev, err := factory.NewTaskTransitionData(header, before, after, request, history, sp, tp)
	if err != nil {
		t.Fatal(err)
	}
	r.Plan = transitionPlan{Before: before, After: mutation, Placement: old.Plan.Placement,
		Groups:          []taskGroupPlan{{source, []rankItem{{before.ID.String(), before.ManualRank}}, []rankItem{}, 2}, {target, []rankItem{}, ranks.Items, 1}},
		QueryGeneration: 2, History: history, Source: sp, Target: tp, Header: header, Payload: ev.PayloadBytes()}
	if err = validateTransitionRecord(r, actor); err != nil {
		t.Fatal("controlled cancellation plan", err)
	}
	return r, actor
}

func TestTaskHumanCancelSupportedEdgesAndIntent(t *testing.T) {
	for _, from := range []c.TaskState{c.TaskStateBacklog, c.TaskStateTodo, c.TaskStateInProgress, c.TaskStateInReview, c.TaskStateBlocked} {
		t.Run(string(from), func(t *testing.T) {
			r, _ := cancelTransitionRecord(t, from, from.RequiresAssignee(), false)
			before, request := r.Plan.Before, r.Input.Request
			if transitionEdge(before, request) != nil || !transitionRetainsAssignee(before, request) {
				t.Fatal("ordinary Human cancel edge is not implemented")
			}
			if err := c.CheckTaskTransitionRule(c.TaskTransitionRuleInput{FromState: from, ToState: c.TaskStateCancelled, Role: c.TaskTransitionHumanOwner, CurrentAssigneeAgentID: before.AssigneeAgentID}); err != nil {
				t.Fatal(err)
			}
			agent := pureID[i.Agent](t, 731)
			for _, change := range []func(*c.TaskTransfer){
				func(v *c.TaskTransfer) { v.AssigneeAgentID = &agent },
				func(v *c.TaskTransfer) {
					v.ResolveBlockerIDs = []c.TaskBlockerID{pureID[c.TaskBlockerIdentity](t, 732)}
				},
				func(v *c.TaskTransfer) { v.AddBlockers = []c.TaskBlockerCreate{{}} },
			} {
				bad := request.Clone()
				change(&bad)
				pureCode(t, transitionEdge(before, bad), f.DependencyUnbound)
			}
			locks, err := r.Input.locks(r.Key, before)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []f.LockRequest{projectLock(r.Input.Project, f.Shared), taskScheduleLock(r.Input.Project, f.Exclusive), taskLock(before.ID.String(), f.Exclusive), taskRankLock(r.Input.Project, groupForTask(before)), taskRankLock(r.Input.Project, groupForTask(r.Plan.After.Task))} {
				if !slices.ContainsFunc(locks, func(held f.LockRequest) bool {
					return f.CompareLockKeys(held.Key, want.Key) == 0 && held.Mode == want.Mode
				}) {
					t.Fatal("cancel lost an original mutation/order gate")
				}
			}
		})
	}
	for _, terminal := range []c.TaskState{c.TaskStateDone, c.TaskStateCancelled} {
		pureCode(t, c.CheckTaskTransitionRule(c.TaskTransitionRuleInput{FromState: terminal, ToState: c.TaskStateCancelled, Role: c.TaskTransitionHumanOwner}), f.TaskTerminalImmutable)
		if transitionCancelEdge(c.Task{State: terminal}, c.TaskTransfer{TargetState: c.TaskStateCancelled}) {
			t.Fatal("terminal task admitted for another cancellation")
		}
	}
}

func TestTaskHumanCancelCanonicalHistoryAndRetainedAssignee(t *testing.T) {
	for _, tc := range []struct {
		name              string
		from              c.TaskState
		assigned, comment bool
	}{{"unassigned", c.TaskStateBacklog, false, false}, {"unassigned-comment", c.TaskStateBacklog, false, true}, {"assigned", c.TaskStateTodo, true, true}, {"blocked-unassigned", c.TaskStateBlocked, false, true}, {"review", c.TaskStateInReview, true, false}} {
		t.Run(tc.name, func(t *testing.T) {
			r, actor := cancelTransitionRecord(t, tc.from, tc.assigned, tc.comment)
			raw, err := json.Marshal(r.Plan)
			if err != nil {
				t.Fatal(err)
			}
			var plan transitionPlan
			if err = json.Unmarshal(raw, &plan); err != nil || !sameValue(plan, r.Plan) || plan.Agent != nil || !sameValue(plan.Before.AssigneeAgentID, plan.After.Task.AssigneeAgentID) {
				t.Fatal("cancel plan fabricated assignment or current-Agent fact", err)
			}
			payload, err := c.DecodeTaskTransitioned(plan.Payload)
			wantHistory := 1
			if tc.comment {
				wantHistory++
			}
			if err != nil || payload.AssigneeChange != nil || len(plan.History) != wantHistory {
				t.Fatal("cancel fabricated assignee/Blocker event", err)
			}
			r.Plan, r.State, r.Receipt = plan, "completed", &plan.After
			if err = validateTransitionRecord(r, actor); err != nil {
				t.Fatal("persisted cancellation receipt rejected", err)
			}
			if tc.comment && plan.History[1].Payload.Comment.Body != *r.Input.Request.Comment {
				t.Fatal("comment bytes changed")
			}
			for _, mutate := range []func(*transitionRecord){
				func(v *transitionRecord) {
					agent := pureID[i.Agent](t, 733)
					v.Plan.After.Task.AssigneeAgentID = &agent
				},
				func(v *transitionRecord) {
					v.Plan.Agent = &ac.AgentRef{ProjectID: v.Input.Project, AgentID: pureID[i.Agent](t, 733), ConfigVersion: 1}
				},
				func(v *transitionRecord) { v.Plan.History[0].Payload.StateChanged.ToState = c.TaskStateDone },
			} {
				bad, who := cancelTransitionRecord(t, tc.from, tc.assigned, tc.comment)
				mutate(bad)
				if validateTransitionRecord(bad, who) == nil {
					t.Fatal("forged cancellation postimage accepted")
				}
			}
		})
	}
}

type cancelWrite struct {
	query string
	args  []any
}
type cancelApplySQL struct {
	postgres.SQLExecutor
	writes []cancelWrite
}

func (s *cancelApplySQL) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	s.writes = append(s.writes, cancelWrite{query, append([]any(nil), args...)})
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func TestTaskHumanCancelPreservesBlockersAndSQLAssignment(t *testing.T) {
	for _, assigned := range []bool{false, true} {
		r, _ := cancelTransitionRecord(t, c.TaskStateBlocked, assigned, true)
		x := &unblockSQL{t: t, count: 2}
		resolved, _, err := prepareTransitionResolutions(context.Background(), x, r.Input, r.Created)
		if err != nil || len(resolved) != 0 || x.writes != 0 || x.queries != 1 {
			t.Fatal("cancel changed or required resolution of existing Blockers", err)
		}
		writes := &cancelApplySQL{}
		if err = applyTransition(context.Background(), writes, r); err != nil {
			t.Fatal("cancel could not persist preserved nullable assignment", err)
		}
		if len(writes.writes) != 4+len(r.Plan.History) || !strings.HasPrefix(writes.writes[0].query, "UPDATE agenteam_work.tasks SET state=") {
			t.Fatal("unexpected cancellation writes")
		}
		args := writes.writes[0].args
		var expectedAgent any
		if assigned {
			expectedAgent = r.Plan.Before.AssigneeAgentID.String()
		}
		if args[3] != "cancelled" || args[4] != expectedAgent || args[2] != int64(r.Input.Expected) || args[6] != int64(r.Plan.After.Task.Version) {
			t.Fatal("wrong nullable assignee/state/version SQL binding")
		}
		for _, write := range writes.writes {
			if strings.Contains(write.query, "task_blockers") {
				t.Fatal("cancel mutated retained Blocker")
			}
		}
	}
}

// Exercise the production evaluator through its original placement/occupancy
// ports, stopping at the first rank read. A controlled rank error is not a
// successful plan/commit; full persistence is checked by the real HTTP fixture.
type cancelEvalSQL struct {
	*titleUpdateStore
	reachedRanks bool
	stop         error
}

func (s *cancelEvalSQL) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	if strings.Contains(query, "count(*) FROM agenteam_work.task_blockers") {
		return taskTriggerRow{int64(2)}
	}
	return s.titleUpdateStore.QueryRow(ctx, query, args...)
}
func (s *cancelEvalSQL) Query(context.Context, string, ...any) (*postgres.Rows, error) {
	s.reachedRanks = true
	return nil, s.stop
}

type cancelOccupancy struct {
	store     *titleUpdateStore
	mode      string
	calls     int
	agent     i.AgentID
	execution i.ExecutionID
}

func (p *cancelOccupancy) ReadInTx(ctx context.Context, tx f.Tx, project i.ProjectID, tasks []string) (ec.ExecutionOccupancy, error) {
	p.calls++
	if project != p.store.task.ProjectID || !slices.Equal(tasks, []string{p.store.task.ID.String()}) {
		return ec.ExecutionOccupancy{}, fault(f.Forbidden)
	}
	if err := p.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(project, f.Shared), taskScheduleLock(project, f.Exclusive)}); err != nil {
		return ec.ExecutionOccupancy{}, err
	}
	if p.mode == "active-error" {
		return ec.ExecutionOccupancy{}, fault(f.DependencyUnavailable)
	}
	if p.mode == "active-missing" {
		return ec.ExecutionOccupancy{}, nil
	}
	out := ec.ExecutionOccupancy{Active: []ec.ActiveTaskExecution{}, HistoryTaskIDs: []string{p.store.task.ID.String()}}
	if p.mode == "active" {
		out.Active = []ec.ActiveTaskExecution{{TaskID: tasks[0], AgentID: p.agent, ExecutionID: p.execution, Status: ec.Running}}
	}
	return out, nil
}

type cancelPending struct {
	store         *titleUpdateStore
	mode          string
	calls, groups int
	agent         i.AgentID
	dispatch      string
}

func (p *cancelPending) ReadInTx(ctx context.Context, tx f.Tx, project i.ProjectID, tasks []string) (ec.DispatchOccupancy, error) {
	p.calls++
	if project != p.store.task.ProjectID || !slices.Equal(tasks, []string{p.store.task.ID.String()}) {
		return ec.DispatchOccupancy{}, fault(f.Forbidden)
	}
	if err := p.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(project, f.Shared), taskScheduleLock(project, f.Exclusive)}); err != nil {
		return ec.DispatchOccupancy{}, err
	}
	if p.mode == "pending-error" {
		return ec.DispatchOccupancy{}, fault(f.DependencyUnavailable)
	}
	if p.mode == "pending-missing" {
		return ec.DispatchOccupancy{}, nil
	}
	out := ec.DispatchOccupancy{Pending: []ec.PendingTaskDispatch{}, HistoryTaskIDs: []string{tasks[0]}}
	if p.mode == "pending" {
		out.Pending = []ec.PendingTaskDispatch{{TaskID: tasks[0], AgentID: p.agent, DispatchID: p.dispatch, SprintID: p.store.task.SprintID.String()}}
	}
	return out, nil
}
func (p *cancelPending) RequireNoPendingGroupsInTx(ctx context.Context, tx f.Tx, project i.ProjectID, groups []ec.PendingClaimGroup) error {
	p.groups++
	after := p.store.task.Clone()
	after.State = c.TaskStateCancelled
	if !slices.Equal(groups, pendingGroups(groupForTask(p.store.task), groupForTask(after))) {
		return fault(f.Forbidden)
	}
	if p.mode == "group-pending" {
		return fault(f.ResourceBusy)
	}
	return p.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(project, f.Shared), taskScheduleLock(project, f.Exclusive)})
}

func TestTaskHumanCancelUsesOriginalOccupancyAndPending(t *testing.T) {
	for _, mode := range []string{"unassigned", "assigned", "active", "pending", "group-pending", "active-error", "pending-error", "active-missing", "pending-missing"} {
		t.Run(mode, func(t *testing.T) {
			tasks, store, owner, _, actor, _ := titleUpdateFixture(t)
			agent := *store.task.AssigneeAgentID
			if mode != "assigned" {
				store.task.State, store.task.AssigneeAgentID = c.TaskStateBacklog, nil
			}
			in := transitionInput{Project: store.task.ProjectID, Task: store.task.ID, User: owner.ref.OwnerUserID, Expected: store.task.Version, Request: c.TaskTransfer{TargetState: c.TaskStateCancelled}}
			store.tx = f.NewTx()
			var err error
			store.locks, err = in.locks("cancel-evaluation", store.task)
			if err != nil {
				t.Fatal(err)
			}
			occupancy, pending := &cancelOccupancy{store: store, mode: mode, agent: agent, execution: pureID[i.Execution](t, 736)}, &cancelPending{store: store, mode: mode, agent: agent, dispatch: pureID[f.Request](t, 737).String()}
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
			r := &transitionRecord{ID: pureID[c.TaskTransitionCommand](t, 734), Input: in}
			_, err = service.evaluateTransition(context.Background(), store.tx, x, actor, r, store.task, store.task.UpdatedAt, nil, pureID[event.EventIdentity](t, 735))
			if mode == "unassigned" || mode == "assigned" {
				if !errors.Is(err, stop) || !x.reachedRanks || pending.groups != 1 {
					t.Fatal("cancel failed before original rank read or fabricated current-Agent requirement", err)
				}
			} else {
				code := f.ResourceBusy
				if strings.HasSuffix(mode, "error") {
					code = f.DependencyUnavailable
				}
				if strings.HasSuffix(mode, "missing") {
					code = f.InternalError
				}
				pureCode(t, err, code)
				if x.reachedRanks {
					t.Fatal("rejected occupancy reached rank mutation planning")
				}
			}
			if occupancy.calls != 1 || (!strings.HasPrefix(mode, "active") && pending.calls != 1) || store.writes != 0 {
				t.Fatal("original ports bypassed or refusal wrote data")
			}
		})
	}
}
