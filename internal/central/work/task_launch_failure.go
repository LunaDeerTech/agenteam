package work

import (
	"context"
	"slices"
	"sync"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type TaskLaunchFailureDependencies struct {
	Authority     *Authority
	Scheduler     c.SchedulerTaskLaunchFailureAuthority
	Pending       ec.PendingClaimGroupGuard
	Events        oc.Appender
	FailureEvents c.TaskLaunchFailureEvents
}

// Scheduler owns the outer commit, final marker and failed CAS. Work owns
// the canonical Task mutation or exact preservation and its private proof.
type TaskLaunchFailureService struct {
	store    Store
	deps     TaskLaunchFailureDependencies
	projects schedulerClaimProject
	mu       sync.Mutex
	stopped  bool
	calls    map[*schedulerClaimCall]struct{}
	changed  chan struct{}
}

func NewTaskLaunchFailure(store Store, deps TaskLaunchFailureDependencies) (*TaskLaunchFailureService, error) {
	if nilPort(store) || deps.Authority.state() == nil || !sameStore(store, deps.Authority.state().store) || nilPort(deps.Scheduler) || nilPort(deps.Pending) || nilPort(deps.Events) || !deps.FailureEvents.Valid() {
		return nil, fault(f.DependencyUnbound)
	}
	projects, ok := deps.Authority.state().projects.(schedulerClaimProject)
	if !ok || nilPort(projects) {
		return nil, fault(f.DependencyUnbound)
	}
	return &TaskLaunchFailureService{store: store, deps: deps, projects: projects, calls: map[*schedulerClaimCall]struct{}{}, changed: make(chan struct{})}, nil
}

type taskFailurePlan struct {
	owner      *TaskLaunchFailureService
	actor      i.Actor
	request    c.TaskLaunchFailureRequest
	record     taskFailureRecord
	baseLocks  []f.LockRequest
	locks      []f.LockRequest
	appendPlan oc.AppendPlan
}

func (p *taskFailurePlan) RequiredLocks() []f.LockRequest {
	if p == nil {
		return nil
	}
	return slices.Clone(p.locks)
}

type taskFailureApplied struct {
	owner *TaskLaunchFailureService
	plan  *taskFailurePlan
	tx    f.Tx
}

func (v *taskFailureApplied) Changed() bool {
	return v != nil && v.plan != nil && v.plan.record.Changed
}

type taskFailureContextKey struct{}
type taskFailureContext struct {
	plan    *taskFailurePlan
	applied *taskFailureApplied
}

func failureInput(ctx context.Context, actor i.Actor, r c.TaskLaunchFailureRequest) error {
	if ctx == nil || r.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if r.Relaunch != nil {
		return relaunchInput(ctx, actor, *r.Relaunch)
	}
	return claimActor(actor, r.Claim)
}
func failureMutationLocks(r c.TaskLaunchFailureRequest, record taskFailureRecord) ([]f.LockRequest, error) {
	locks, err := failureDiscoveryLocks(r)
	if err != nil {
		return nil, err
	}
	command, err := f.NewCommandIdentity("scheduler", []string{r.ProjectID().String()}, "task_launch_failure", f.IdempotencyKey("scheduler_failure:"+r.DispatchID()))
	if err != nil {
		return nil, err
	}
	for n := range locks {
		if locks[n].Key.Canonical() == taskLock(r.TaskID().String(), f.Shared).Key.Canonical() {
			locks[n].Mode = f.Exclusive
		}
	}
	locks = append(locks, commandLock(command))
	for _, g := range record.Groups {
		locks = append(locks, taskRankLock(r.ProjectID(), g.Group))
	}
	return taskNormalize(locks)
}
func (s *TaskLaunchFailureService) currentProject(ctx context.Context, tx f.Tx, r c.TaskLaunchFailureRequest) (pc.ProjectRef, error) {
	p, err := s.projects.RequireSchedulerProjectInTx(ctx, tx, r.ProjectID())
	if ctx.Err() != nil {
		return pc.ProjectRef{}, ctx.Err()
	}
	if err != nil {
		return pc.ProjectRef{}, portError(err)
	}
	if p.Project.Validate() != nil || p.Project.ID != r.ProjectID() || p.Config.Validate() != nil {
		return pc.ProjectRef{}, internal(nil)
	}
	// Paused is not a preservation result: keep the confirmed-final reservation
	// pending until resume. Current Sprint identity is evaluated separately.
	if p.Project.Lifecycle != pc.Active || !p.Config.Enabled {
		return pc.ProjectRef{}, fault(f.InvalidState)
	}
	return p.Project, nil
}
func (s *TaskLaunchFailureService) readFailureSource(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, r c.TaskLaunchFailureRequest, facts c.TaskLaunchFailureFacts) (schedulerClaimRecord, *taskRelaunchRecord, c.Task, c.Sprint, *c.SprintID, error) {
	var claim schedulerClaimRecord
	var relaunch *taskRelaunchRecord
	var task c.Task
	var sprint c.Sprint
	if facts.ValidateFor(r) != nil {
		return claim, relaunch, task, sprint, nil, fault(f.Forbidden)
	}
	project, err := s.currentProject(ctx, tx, r)
	if err != nil {
		return claim, relaunch, task, sprint, nil, err
	}
	if r.Relaunch != nil {
		relaunch, err = loadTaskRelaunch(ctx, x, r.ProjectID(), r.DispatchID())
		if err != nil {
			return claim, relaunch, task, sprint, nil, err
		}
		if relaunch == nil || relaunch.Request != *r.Relaunch || facts.Relaunch == nil || relaunch.source() != *facts.Relaunch {
			return claim, relaunch, task, sprint, nil, fault(f.Forbidden)
		}
	} else {
		original, e := loadSchedulerClaim(ctx, x, r.ProjectID(), r.DispatchID())
		if e != nil {
			return claim, relaunch, task, sprint, nil, e
		}
		if original == nil || original.Request != r.Claim || !sameValue(original.Guard, facts.Guard) {
			return claim, relaunch, task, sprint, nil, fault(f.Forbidden)
		}
		claim = *original
	}
	task, err = loadTask(ctx, x, r.ProjectID(), r.TaskID())
	if err != nil {
		return claim, relaunch, task, sprint, nil, err
	}
	sprint, err = loadSprint(ctx, x, r.ProjectID(), r.SprintID(), project.CurrentSprintID)
	if err != nil {
		return claim, relaunch, task, sprint, nil, err
	}
	if _, err = loadMilestone(ctx, x, r.ProjectID(), sprint.MilestoneID); err != nil {
		return claim, relaunch, task, sprint, nil, err
	}
	return claim, relaunch, task, sprint, project.CurrentSprintID, nil
}
func (s *TaskLaunchFailureService) DiscoverTaskLaunchFailure(ctx context.Context, actor i.Actor, r c.TaskLaunchFailureRequest) (c.TaskLaunchFailurePlan, error) {
	ctx, done, err := s.beginFailure(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	if err = failureInput(ctx, actor, r); err != nil {
		return nil, err
	}
	locks, err := failureDiscoveryLocks(r)
	if err != nil {
		return nil, err
	}
	cause, err := readCause("task-launch-failure")
	if err != nil {
		return nil, err
	}
	var record taskFailureRecord
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if e := s.store.AcquireAll(ctx, tx, locks); e != nil {
			return portError(e)
		}
		x, e := s.store.InTx(tx)
		if e != nil {
			return portError(e)
		}
		facts, e := s.deps.Scheduler.RequireTaskLaunchFailureDiscoveryInTx(ctx, tx, actor, r)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if e != nil {
			return portError(e)
		}
		claim, relaunch, before, sprint, current, e := s.readFailureSource(ctx, tx, x, r, facts)
		if e != nil {
			return e
		}
		existing, e := loadTaskFailureRecord(ctx, x, r.ProjectID(), r.DispatchID())
		if e != nil {
			return e
		}
		if existing != nil {
			return fault(f.ConfirmationStale)
		}
		record, e = planTaskFailureRecord(ctx, x, r, facts, claim, before, sprint, current, relaunch)
		return e
	})
	if err = taskTxError(ctx, result); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = validateTaskFailureRecord(&record); err != nil {
		return nil, err
	}
	base, err := failureMutationLocks(r, record)
	if err != nil {
		return nil, err
	}
	p := &taskFailurePlan{owner: s, actor: actor, request: r.Clone(), record: record, baseLocks: base, locks: slices.Clone(base)}
	if record.Changed {
		ev, e := record.event(s.deps.FailureEvents)
		if e != nil {
			return nil, internal(e)
		}
		proof := context.WithValue(ctx, taskFailureContextKey{}, taskFailureContext{plan: p})
		p.appendPlan, err = s.deps.Events.PrepareAppend(proof, actor, ev)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err != nil {
			return nil, portError(err)
		}
		p.locks, err = taskNormalize(append(slices.Clone(base), p.appendPlan.Locks()...))
		if err != nil {
			return nil, err
		}
	}
	return p, nil
}
func (s *TaskLaunchFailureService) originalPlan(ctx context.Context, actor i.Actor, r c.TaskLaunchFailureRequest, raw c.TaskLaunchFailurePlan) (*taskFailurePlan, error) {
	if err := failureInput(ctx, actor, r); err != nil {
		return nil, err
	}
	p, ok := raw.(*taskFailurePlan)
	if s == nil || !ok || p == nil || p.owner != s || !p.actor.Equal(actor) || !p.request.Equal(r) || len(p.locks) == 0 {
		return nil, fault(f.Forbidden)
	}
	return p, nil
}
func (s *TaskLaunchFailureService) requireCurrent(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, actor i.Actor, p *taskFailurePlan) error {
	facts, err := s.deps.Scheduler.RequireTaskLaunchFailureInTx(ctx, tx, actor, p.request, p)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return portError(err)
	}
	claim, relaunch, before, sprint, current, err := s.readFailureSource(ctx, tx, x, p.request, facts)
	if err != nil {
		return err
	}
	if !sameValue(facts, p.record.Facts) || !sameTaskFailureOrigin(claim, relaunch, p.record) || !sameValue(before, p.record.Before) || !sameValue(sprint, p.record.Sprint) || !sameValue(current, p.record.CurrentSprintID) {
		return fault(f.ConfirmationStale)
	}
	existing, err := loadTaskFailureRecord(ctx, x, p.request.ProjectID(), p.request.DispatchID())
	if err != nil {
		return err
	}
	if existing != nil {
		return fault(f.ConfirmationStale)
	}
	if p.record.Changed {
		// No general exemption: all mutated groups remain protected. Only the
		// Scheduler's private proof exempts its own original todo reservation.
		groups := make([]taskGroup, 0, len(p.record.Groups))
		for _, g := range p.record.Groups {
			groups = append(groups, g.Group)
		}
		if err = s.deps.Pending.RequireNoPendingGroupsInTx(ctx, tx, p.request.ProjectID(), pendingGroups(groups...)); err != nil {
			return portError(err)
		}
		if err = requireTaskFailureCapacity(ctx, x, p.record.Before, len(p.record.History)); err != nil {
			return err
		}
		for _, g := range p.record.Groups {
			rows, gen, e := loadTaskRanks(ctx, x, p.request.ProjectID(), g.Group)
			if e != nil {
				return e
			}
			if gen != g.Generation || !sameValue(rows, g.Before) {
				return fault(f.ConfirmationStale)
			}
		}
		q, e := loadTaskQueryGeneration(ctx, x, p.request.ProjectID())
		if e != nil {
			return e
		}
		if q != p.record.QueryGeneration {
			return fault(f.ConfirmationStale)
		}
	}
	return ctx.Err()
}
func (s *TaskLaunchFailureService) ApplyTaskLaunchFailureInTx(ctx context.Context, tx f.Tx, actor i.Actor, r c.TaskLaunchFailureRequest, raw c.TaskLaunchFailurePlan) (c.AppliedTaskLaunchFailure, error) {
	ctx, done, err := s.beginFailure(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	p, err := s.originalPlan(ctx, actor, r, raw)
	if err != nil {
		return nil, err
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return nil, portError(err)
	}
	if err = s.store.RequireHeldLocks(ctx, tx, p.locks); err != nil {
		return nil, portError(err)
	}
	if err = s.requireCurrent(ctx, tx, x, actor, p); err != nil {
		return nil, err
	}
	if err = applyTaskFailureRecord(ctx, x, &p.record); err != nil {
		return nil, err
	}
	applied := &taskFailureApplied{owner: s, plan: p, tx: tx}
	if p.record.Changed {
		ev, e := p.record.event(s.deps.FailureEvents)
		if e != nil {
			return nil, internal(e)
		}
		proof := context.WithValue(ctx, taskFailureContextKey{}, taskFailureContext{plan: p, applied: applied})
		receipt, e := s.deps.Events.AppendEventInTx(proof, tx, actor, ev, p.appendPlan)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if e != nil {
			return nil, portError(e)
		}
		if receipt.EventID != p.record.Header.EventID || receipt.Sequence.Validate() != nil {
			return nil, internal(nil)
		}
	}
	if err = verifyTaskFailurePostimage(ctx, x, &p.record); err != nil {
		return nil, err
	}
	return applied, nil
}
func (s *TaskLaunchFailureService) CheckTaskLaunchFailureAppliedInTx(ctx context.Context, tx f.Tx, actor i.Actor, r c.TaskLaunchFailureRequest, raw c.TaskLaunchFailurePlan, value c.AppliedTaskLaunchFailure) error {
	ctx, done, err := s.beginFailure(ctx)
	if err != nil {
		return err
	}
	defer done()
	p, err := s.originalPlan(ctx, actor, r, raw)
	if err != nil {
		return err
	}
	v, ok := value.(*taskFailureApplied)
	if !ok || v == nil || v.owner != s || v.plan != p || v.tx != tx {
		return fault(f.Forbidden)
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if err = s.store.RequireHeldLocks(ctx, tx, p.locks); err != nil {
		return portError(err)
	}
	facts, err := s.deps.Scheduler.RequireTaskLaunchFailureInTx(ctx, tx, actor, r, p)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return portError(err)
	}
	if !sameValue(facts, p.record.Facts) {
		return fault(f.Forbidden)
	}
	if _, err = s.currentProject(ctx, tx, r); err != nil {
		return err
	}
	return verifyTaskFailurePostimage(ctx, x, &p.record)
}

var _ c.SchedulerTaskLaunchFailures = (*TaskLaunchFailureService)(nil)

func (s *TaskLaunchFailureService) beginFailure(ctx context.Context) (context.Context, func(), error) {
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
func (s *TaskLaunchFailureService) Stop() {
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
func (s *TaskLaunchFailureService) Drain(ctx context.Context) error {
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
func (s *TaskLaunchFailureService) Joined() bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped && len(s.calls) == 0
}
