package work

import (
	"context"
	"encoding/json"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"slices"
)

func transitionNullableID(v string) any {
	if v == "00000000-0000-0000-0000-000000000000" || v == "" {
		return nil
	}
	return v
}
func (in transitionInput) historyActor() c.TaskTransitionActor {
	return c.TaskTransitionActor{UserID: in.User, AgentID: in.Agent, ExecutionID: in.Execution}
}
func (in transitionInput) matches(actor i.Actor) bool {
	d := actor.Details()
	return actor.Validate() == nil && (d.Kind == i.Human && in.User.Validate() == nil && d.UserID == in.User.String() || d.Kind == i.AgentRun && in.Agent.Validate() == nil && d.AgentID == in.Agent.String() && d.ExecutionID == in.Execution.String() && d.ProjectID == in.Project.String())
}
func (in transitionInput) MarshalJSON() ([]byte, error) {
	if in.historyActor().Validate() != nil {
		return nil, internal(nil)
	}
	w := map[string]any{"project_id": in.Project, "task_id": in.Task, "expected_version": in.Expected, "request": in.Request}
	if in.User.Validate() == nil {
		w["actor_user_id"] = in.User
	} else {
		w["actor_agent_id"] = in.Agent
		w["actor_execution_id"] = in.Execution
	}
	if in.Cancellation != nil {
		w["execution_cancellation"] = in.Cancellation
	}
	return json.Marshal(w)
}
func transitionReadInput(ctx context.Context, actor i.Actor, project c.ProjectID) error {
	if ctx == nil || project.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return canceled(err)
	}
	if actor.Validate() != nil {
		return fault(f.Unauthenticated)
	}
	d := actor.Details()
	if d.Kind == i.Human || d.Kind == i.AgentRun && d.ProjectID == project.String() {
		return nil
	}
	return fault(f.Forbidden)
}

type transitionAccessKey struct{}
type transitionAccess struct {
	owner   *taskTransitionServiceState
	actor   i.ActorDetails
	project c.ProjectID
	task    c.TaskID
	intent  string
	locks   []f.LockRequest
}

func (s *TaskTransitionService) bindTransitionAccess(ctx context.Context, actor i.Actor, project c.ProjectID, task c.TaskID, intent string) (context.Context, error) {
	if intent != c.TaskToolRead && intent != c.TaskToolTransfer && intent != c.TaskToolLookupTransfer {
		return ctx, fault(f.Forbidden)
	}
	if actor.Details().Kind != i.AgentRun {
		return ctx, nil
	}
	st := s.state()
	if st == nil || nilPort(st.deps.AgentTools) || task.Validate() != nil {
		return ctx, fault(f.DependencyUnbound)
	}
	locks, err := st.deps.AgentTools.DiscoverTaskTool(ctx, actor, project, task.String(), intent)
	if err != nil {
		return ctx, portError(err)
	}
	locks, err = taskNormalize(locks)
	if err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, transitionAccessKey{}, &transitionAccess{st, actor.Details(), project, task, intent, locks}), nil
}
func (a *Authority) transitionAccess(ctx context.Context, actor i.Actor, project c.ProjectID) (*transitionAccess, error) {
	w, _ := ctx.Value(transitionAccessKey{}).(*transitionAccess)
	if a.state() == nil || w == nil || w.owner != a.state().transitions || !sameValue(w.actor, actor.Details()) || w.project != project {
		return nil, fault(f.Forbidden)
	}
	return w, nil
}
func (a *Authority) transitionAccessLocks(ctx context.Context, actor i.Actor, project c.ProjectID) ([]f.LockRequest, error) {
	if actor.Details().Kind == i.Human {
		return []f.LockRequest{userLock(actor.Details().UserID, f.Shared), projectLock(project, f.Shared)}, nil
	}
	w, err := a.transitionAccess(ctx, actor, project)
	if err != nil {
		return nil, err
	}
	return slices.Clone(w.locks), nil
}
func (a *Authority) requireTransitionAccess(ctx context.Context, tx f.Tx, actor i.Actor, project c.ProjectID, intent i.AccessIntent) (pc.ProjectRef, error) {
	if actor.Details().Kind == i.Human {
		grant, err := a.state().projects.RequireOwnerInTx(ctx, tx, actor, project, intent)
		if err != nil {
			return pc.ProjectRef{}, portError(err)
		}
		if !grant.Matches(actor, project) {
			return pc.ProjectRef{}, fault(f.Forbidden)
		}
		return grant.Project(), nil
	}
	w, err := a.transitionAccess(ctx, actor, project)
	if err != nil {
		return pc.ProjectRef{}, err
	}
	if intent == i.Mutate && w.intent != c.TaskToolTransfer {
		return pc.ProjectRef{}, fault(f.Forbidden)
	}
	if err = a.state().store.RequireHeldLocks(ctx, tx, w.locks); err != nil {
		return pc.ProjectRef{}, portError(err)
	}
	if err = w.owner.deps.AgentTools.RequireTaskToolInTx(ctx, tx, actor, project, w.task.String(), w.intent); err != nil {
		return pc.ProjectRef{}, portError(err)
	}
	gate, ok := a.state().projects.(ec.PreparationProjectGate)
	if !ok || nilPort(gate) {
		return pc.ProjectRef{}, fault(f.DependencyUnbound)
	}
	ref, err := gate.RequirePreparingProjectInTx(ctx, tx, project)
	if err != nil {
		return pc.ProjectRef{}, portError(err)
	}
	if ref.ID != project {
		return pc.ProjectRef{}, fault(f.Forbidden)
	}
	return ref, nil
}
func (s *TaskTransitionService) transitionLocks(ctx context.Context, actor i.Actor, project c.ProjectID, base []f.LockRequest) ([]f.LockRequest, error) {
	extra, err := s.state().deps.Authority.transitionAccessLocks(ctx, actor, project)
	if err != nil {
		return nil, err
	}
	locks := append(slices.Clone(base), extra...)
	if w, ok := ctx.Value(protectionContextKey{}).(protectionContext); ok {
		if nilPort(s.state().deps.Cancellation) {
			return nil, fault(f.DependencyUnbound)
		}
		cancellation, e := s.state().deps.Cancellation.CancelLocks(actor, w.scope)
		if e != nil {
			return nil, portError(e)
		}
		locks = append(locks, cancellation...)
	}
	return taskNormalize(locks)
}

func (s *TaskTransitionService) transitionPlacement(ctx context.Context, tx f.Tx, actor i.Actor, project c.ProjectID, id c.SprintID) (c.Placement, error) {
	if actor.Details().Kind == i.Human && ctx.Value(protectionContextKey{}) == nil {
		return s.state().deps.Structure.ReadPlacementInTx(ctx, tx, actor, project, id)
	}
	ref, err := s.state().deps.Authority.requireTransitionAccess(ctx, tx, actor, project, i.Read)
	if err != nil {
		return c.Placement{}, err
	}
	x, err := s.state().store.InTx(tx)
	if err != nil {
		return c.Placement{}, portError(err)
	}
	if err = checkPointer(ctx, x, ref); err != nil {
		return c.Placement{}, err
	}
	sprint, err := loadSprint(ctx, x, project, id, ref.CurrentSprintID)
	if err != nil {
		return c.Placement{}, err
	}
	milestone, err := loadMilestone(ctx, x, project, sprint.MilestoneID)
	if err != nil {
		return c.Placement{}, err
	}
	return c.Placement{Milestone: milestone, Sprint: sprint}, nil
}

func (s *TaskTransitionService) checkTransitionOccupancy(ctx context.Context, actor i.Actor, before c.Task, request c.TaskTransfer, occupancy ec.ExecutionOccupancy) error {
	if occupancy.Active == nil || occupancy.HistoryTaskIDs == nil {
		return internal(nil)
	}
	if len(occupancy.Active) == 0 {
		return nil
	}
	if len(occupancy.Active) != 1 || occupancy.Active[0].TaskID != before.ID.String() {
		return internal(nil)
	}
	if transitionReviewEdge(before, request) {
		// The original Execution retains its captured Agent and Task identity.
		// Schedule EX prevents a second launch until that owner has retired.
		if actor.Details().Kind == i.AgentRun && occupancy.Active[0].ExecutionID.String() != actor.Details().ExecutionID {
			return fault(f.Forbidden)
		}
		return nil
	}
	if transitionCancelEdge(before, request) && !nilPort(s.state().deps.Cancellation) {
		return nil
	}
	return fault(f.ResourceBusy)
}

// ReadTask returns the canonical version needed by transfer-task. Agent access
// is tied to the live read-task operation before any Work-owned SQL.
func (s *TaskTransitionService) ReadTask(ctx context.Context, actor i.Actor, project c.ProjectID, task c.TaskID) (c.Task, error) {
	ctx, entry, done, err := s.begin(ctx)
	if err != nil {
		return c.Task{}, err
	}
	defer done()
	if err = transitionReadInput(ctx, actor, project); err != nil {
		return c.Task{}, err
	}
	if task.Validate() != nil {
		return c.Task{}, fault(f.InvalidArgument)
	}
	ctx, err = s.bindTransitionAccess(ctx, actor, project, task, c.TaskToolRead)
	if err != nil {
		return c.Task{}, err
	}
	entry.project, entry.class = project, projectCallRead
	if own := s.state().ownership; own != nil {
		ctx, err = own.admit(ctx, entry, actor, projectCallTransition)
		if err != nil {
			return c.Task{}, err
		}
	}
	locks, err := s.transitionLocks(ctx, actor, project, []f.LockRequest{projectLock(project, f.Shared), taskLock(task.String(), f.Shared)})
	if err != nil {
		return c.Task{}, err
	}
	cause, err := readCause("task-tool-read")
	if err != nil {
		return c.Task{}, err
	}
	var out c.Task
	result := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if e := s.state().store.AcquireAll(ctx, tx, locks); e != nil {
			return portError(e)
		}
		if _, e := s.state().deps.Authority.requireTransitionAccess(ctx, tx, actor, project, i.Read); e != nil {
			return e
		}
		x, e := s.state().store.InTx(tx)
		if e != nil {
			return portError(e)
		}
		out, e = loadTask(ctx, x, project, task)
		return e
	})
	if err = taskTxError(ctx, result); err != nil {
		return c.Task{}, err
	}
	return out.Clone(), nil
}

func (s *TaskTransitionService) RequireBuiltinBindingInTx(ctx context.Context, tx f.Tx, store any) error {
	if err := s.RequireStore(store); err != nil {
		return err
	}
	st := s.state()
	if ctx == nil || nilPort(st.deps.AgentTools) || nilPort(st.deps.AgentRunAgents) || st.ownership == nil || st.deps.Authority.state().transitions != st {
		return fault(f.DependencyUnbound)
	}
	if gate, ok := st.deps.Authority.state().projects.(ec.PreparationProjectGate); !ok || nilPort(gate) {
		return fault(f.DependencyUnbound)
	}
	_, err := st.store.InTx(tx)
	return portError(err)
}

func (s *TaskTransitionService) LookupTaskToolTransfer(ctx context.Context, actor i.Actor, meta f.CommandMeta, project c.ProjectID, task c.TaskID, request c.TaskTransfer) (c.TaskTransitionLookup, error) {
	ctx, entry, done, err := s.begin(ctx)
	if err != nil {
		return c.TaskTransitionLookup{}, err
	}
	defer done()
	_, q, err := transitionRequest(ctx, actor, meta, project, task, request)
	if err != nil {
		return c.TaskTransitionLookup{}, err
	}
	ctx, err = s.bindTransitionAccess(ctx, actor, project, task, c.TaskToolLookupTransfer)
	if err != nil {
		return c.TaskTransitionLookup{}, err
	}
	entry.project, entry.class = project, projectCallRead
	if own := s.state().ownership; own != nil {
		ctx, err = own.admit(ctx, entry, actor, projectCallTransition)
		if err != nil {
			return c.TaskTransitionLookup{}, err
		}
	}
	return s.lookupTransition(ctx, actor, q)
}
