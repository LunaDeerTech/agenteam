package work

import (
	"context"
	"slices"
	"sync"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type TaskRelaunchDependencies struct {
	Authority *Authority
	Scheduler c.SchedulerRelaunchAuthority
	Agents    ac.SchedulerCurrentReferences
	Pending   ec.PendingDispatchReader
	Occupancy ec.WorkOccupancyReader
}

// TaskRelaunchService records only Work's immutable source. Scheduler owns the
// pending Dispatch, cooldown, transaction result and any subsequent Launch.
type TaskRelaunchService struct {
	store    Store
	deps     TaskRelaunchDependencies
	projects schedulerClaimProject
	mu       sync.Mutex
	stopped  bool
	calls    map[*schedulerClaimCall]struct{}
	changed  chan struct{}
}

func NewTaskRelaunch(store Store, deps TaskRelaunchDependencies) (*TaskRelaunchService, error) {
	if nilPort(store) || deps.Authority.state() == nil || !sameStore(store, deps.Authority.state().store) || nilPort(deps.Scheduler) || nilPort(deps.Agents) || nilPort(deps.Pending) || nilPort(deps.Occupancy) {
		return nil, fault(f.DependencyUnbound)
	}
	projects, ok := deps.Authority.state().projects.(schedulerClaimProject)
	if !ok || nilPort(projects) {
		return nil, fault(f.DependencyUnbound)
	}
	return &TaskRelaunchService{store: store, deps: deps, projects: projects, calls: map[*schedulerClaimCall]struct{}{}, changed: make(chan struct{})}, nil
}

type taskRelaunchPlan struct {
	owner   *TaskRelaunchService
	actor   i.Actor
	request c.TaskRelaunchRequest
	record  taskRelaunchRecord
	locks   []f.LockRequest
}

func (p *taskRelaunchPlan) RequiredLocks() []f.LockRequest {
	if p == nil {
		return nil
	}
	return slices.Clone(p.locks)
}

type appliedTaskRelaunch struct {
	owner *TaskRelaunchService
	plan  *taskRelaunchPlan
	tx    f.Tx
}

func (v *appliedTaskRelaunch) Source() c.TaskRelaunchSource {
	if v == nil || v.plan == nil {
		return c.TaskRelaunchSource{}
	}
	return v.plan.record.source()
}
func relaunchInput(ctx context.Context, actor i.Actor, r c.TaskRelaunchRequest) error {
	if ctx == nil || r.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if actor.Validate() != nil {
		return fault(f.Unauthenticated)
	}
	d := actor.Details()
	if d.Kind != i.Service || d.ServiceName != i.Scheduler || d.ProjectID != r.ProjectID.String() || d.CauseRef != r.DispatchID {
		return fault(f.Forbidden)
	}
	return nil
}
func taskRelaunchLocks(r c.TaskRelaunchRequest) ([]f.LockRequest, error) {
	ak, err := f.AgentLock(r.AgentID.String())
	if err != nil {
		return nil, err
	}
	command, err := f.NewCommandIdentity("scheduler", []string{r.ProjectID.String()}, "task_relaunch", f.IdempotencyKey("scheduler_relaunch:"+r.DispatchID))
	if err != nil {
		return nil, err
	}
	return taskNormalize([]f.LockRequest{commandLock(command), projectLock(r.ProjectID, f.Shared), taskScheduleLock(r.ProjectID, f.Exclusive), {Key: ak, Mode: f.Shared}, sprintLock(r.CurrentSprintID.String(), f.Shared), taskLock(r.TaskID.String(), f.Shared)})
}
func validateRelaunchTask(t c.Task, r c.TaskRelaunchRequest) error {
	if t.Validate() != nil || t.ProjectID != r.ProjectID || t.ID != r.TaskID {
		return internal(nil)
	}
	if t.Version != r.ExpectedTaskVersion {
		return fault(f.TaskVersionConflict)
	}
	if t.State != c.TaskStateInProgress || t.SprintID != r.CurrentSprintID {
		return fault(f.InvalidState)
	}
	if t.AssigneeAgentID == nil || *t.AssigneeAgentID != r.AgentID {
		return field(f.InvalidState, "/agent_id", "TASK_ASSIGNEE_INVALID")
	}
	return nil
}
func (s *TaskRelaunchService) currentSource(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, r c.TaskRelaunchRequest) (taskRelaunchRecord, error) {
	var zero taskRelaunchRecord
	project, err := s.projects.RequireSchedulerProjectInTx(ctx, tx, r.ProjectID)
	if ctx.Err() != nil {
		return zero, ctx.Err()
	}
	if err != nil {
		return zero, portError(err)
	}
	if project.Project.Validate() != nil || project.Project.ID != r.ProjectID || project.Config.Validate() != nil {
		return zero, internal(nil)
	}
	if project.Project.Lifecycle != pc.Active || !project.Config.Enabled || project.Project.CurrentSprintID == nil || *project.Project.CurrentSprintID != r.CurrentSprintID {
		return zero, fault(f.InvalidState)
	}
	task, err := loadTask(ctx, x, r.ProjectID, r.TaskID)
	if err != nil {
		return zero, err
	}
	if err = validateRelaunchTask(task, r); err != nil {
		return zero, err
	}
	sprint, err := loadSprint(ctx, x, r.ProjectID, r.CurrentSprintID, project.Project.CurrentSprintID)
	if err != nil {
		return zero, err
	}
	if sprint.ProjectID != r.ProjectID || sprint.ID != r.CurrentSprintID || sprint.State != c.Current || sprint.MilestoneID != task.MilestoneID {
		return zero, fault(f.TaskSprintInvalid)
	}
	milestone, err := loadMilestone(ctx, x, r.ProjectID, task.MilestoneID)
	if err != nil {
		return zero, err
	}
	if milestone.ProjectID != r.ProjectID || milestone.ID != task.MilestoneID {
		return zero, internal(nil)
	}
	var unresolved int64
	if err = x.QueryRow(ctx, `SELECT count(*) FROM agenteam_work.task_blockers WHERE project_id=$1 AND task_id=$2 AND resolved_at IS NULL`, r.ProjectID.String(), r.TaskID.String()).Scan(&unresolved); err != nil {
		return zero, taskSQL(err)
	}
	if unresolved < 0 {
		return zero, internal(nil)
	}
	if unresolved != 0 {
		return zero, field(f.InvalidState, "/task_id", "UNRESOLVED_BLOCKERS")
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	return taskRelaunchRecord{Request: r, Task: task.Clone(), Sprint: sprint.Clone(), Milestone: milestone}, nil
}
func (s *TaskRelaunchService) DiscoverTaskRelaunch(ctx context.Context, actor i.Actor, r c.TaskRelaunchRequest) (c.TaskRelaunchPlan, error) {
	ctx, done, err := s.beginRelaunch(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	if err = relaunchInput(ctx, actor, r); err != nil {
		return nil, err
	}
	locks, err := taskRelaunchLocks(r)
	if err != nil {
		return nil, err
	}
	cause, err := readCause("task-relaunch")
	if err != nil {
		return nil, err
	}
	var record taskRelaunchRecord
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if err = s.deps.Scheduler.RequireTaskRelaunchDiscoveryInTx(ctx, tx, actor, r); err != nil {
			return portError(err)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		record, err = s.currentSource(ctx, tx, x, r)
		return err
	})
	if err = taskTxError(ctx, result); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	record.CreatedAt, _ = f.NewInstant(time.Now())
	if record.CreatedAt.Time().Before(record.Task.UpdatedAt.Time()) {
		record.CreatedAt = record.Task.UpdatedAt
	}
	if err = validateTaskRelaunchRecord(&record); err != nil {
		return nil, err
	}
	return &taskRelaunchPlan{s, actor, r, record, locks}, nil
}
func (s *TaskRelaunchService) originalRelaunchPlan(ctx context.Context, actor i.Actor, r c.TaskRelaunchRequest, raw c.TaskRelaunchPlan) (*taskRelaunchPlan, error) {
	if err := relaunchInput(ctx, actor, r); err != nil {
		return nil, err
	}
	p, ok := raw.(*taskRelaunchPlan)
	if s == nil || !ok || p == nil || p.owner != s || !p.actor.Equal(actor) || p.request != r || len(p.locks) == 0 {
		return nil, fault(f.Forbidden)
	}
	return p, nil
}
func (s *TaskRelaunchService) requireRelaunchCurrent(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, actor i.Actor, p *taskRelaunchPlan) error {
	if err := s.store.RequireHeldLocks(ctx, tx, p.locks); err != nil {
		return portError(err)
	}
	if err := s.deps.Scheduler.RequireTaskRelaunchInTx(ctx, tx, actor, p.request, p); err != nil {
		return portError(err)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	current, err := s.currentSource(ctx, tx, x, p.request)
	if err != nil {
		return err
	}
	current.CreatedAt = p.record.CreatedAt
	if !sameValue(current, p.record) {
		return fault(f.ConfirmationStale)
	}
	r := p.request
	agent, err := s.deps.Agents.RequireSchedulerCurrentInTx(ctx, tx, actor, r.ProjectID, r.AgentID)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return portError(err)
	}
	if agent.Validate() != nil || agent.ProjectID != r.ProjectID || agent.AgentID != r.AgentID {
		return internal(nil)
	}
	occupancy, err := s.deps.Occupancy.ReadInTx(ctx, tx, r.ProjectID, []string{r.TaskID.String()})
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return portError(err)
	}
	if occupancy.Active == nil || occupancy.HistoryTaskIDs == nil {
		return internal(nil)
	}
	if len(occupancy.Active) != 0 {
		return fault(f.ResourceBusy)
	}
	for _, id := range occupancy.HistoryTaskIDs {
		if id != r.TaskID.String() {
			return internal(nil)
		}
	}
	if len(occupancy.HistoryTaskIDs) > 1 {
		return internal(nil)
	}
	pending, err := s.deps.Pending.ReadInTx(ctx, tx, r.ProjectID, []string{r.TaskID.String()})
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return portError(err)
	}
	if pending.Pending == nil || pending.HistoryTaskIDs == nil {
		return internal(nil)
	}
	if len(pending.Pending) != 0 {
		return fault(f.ResourceBusy)
	}
	for _, id := range pending.HistoryTaskIDs {
		if id != r.TaskID.String() {
			return internal(nil)
		}
	}
	if len(pending.HistoryTaskIDs) > 1 {
		return internal(nil)
	}
	return ctx.Err()
}
func (s *TaskRelaunchService) RecordTaskRelaunchInTx(ctx context.Context, tx f.Tx, actor i.Actor, r c.TaskRelaunchRequest, raw c.TaskRelaunchPlan) (c.AppliedTaskRelaunch, error) {
	ctx, done, err := s.beginRelaunch(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	p, err := s.originalRelaunchPlan(ctx, actor, r, raw)
	if err != nil {
		return nil, err
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return nil, portError(err)
	}
	if err = s.requireRelaunchCurrent(ctx, tx, x, actor, p); err != nil {
		return nil, err
	}
	if err = insertTaskRelaunch(ctx, x, &p.record); err != nil {
		return nil, err
	}
	if err = verifyTaskRelaunch(ctx, x, &p.record); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return &appliedTaskRelaunch{s, p, tx}, nil
}
func (s *TaskRelaunchService) CheckTaskRelaunchAppliedInTx(ctx context.Context, tx f.Tx, actor i.Actor, r c.TaskRelaunchRequest, raw c.TaskRelaunchPlan, value c.AppliedTaskRelaunch) error {
	ctx, done, err := s.beginRelaunch(ctx)
	if err != nil {
		return err
	}
	defer done()
	p, err := s.originalRelaunchPlan(ctx, actor, r, raw)
	if err != nil {
		return err
	}
	applied, ok := value.(*appliedTaskRelaunch)
	if !ok || applied == nil || applied.owner != s || applied.plan != p || applied.tx != tx {
		return fault(f.Forbidden)
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if err = s.requireRelaunchCurrent(ctx, tx, x, actor, p); err != nil {
		return err
	}
	return verifyTaskRelaunch(ctx, x, &p.record)
}

func (s *TaskRelaunchService) beginRelaunch(ctx context.Context) (context.Context, func(), error) {
	if ctx == nil {
		return nil, nil, fault(f.InvalidArgument)
	}
	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}
	if s == nil || nilPort(s.store) || s.calls == nil {
		return nil, nil, fault(f.DependencyUnbound)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return nil, nil, fault(f.ShuttingDown)
	}
	owned, cancel := context.WithCancel(ctx)
	call := &schedulerClaimCall{cancel}
	s.calls[call] = struct{}{}
	var once sync.Once
	return owned, func() {
		once.Do(func() {
			cancel()
			s.mu.Lock()
			delete(s.calls, call)
			close(s.changed)
			s.changed = make(chan struct{})
			s.mu.Unlock()
		})
	}, nil
}
func (s *TaskRelaunchService) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
	for call := range s.calls {
		call.cancel()
	}
}
func (s *TaskRelaunchService) Drain(ctx context.Context) error {
	if ctx == nil {
		return fault(f.InvalidArgument)
	}
	if s == nil {
		return nil
	}
	for {
		s.mu.Lock()
		if len(s.calls) == 0 {
			s.mu.Unlock()
			return nil
		}
		changed := s.changed
		s.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
func (s *TaskRelaunchService) Joined() bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped && len(s.calls) == 0
}

var _ c.SchedulerTaskRelaunches = (*TaskRelaunchService)(nil)
