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

type TaskBusyCompensationDependencies struct {
	Authority          *Authority
	Scheduler          c.SchedulerTaskBusyAuthority
	Pending            ec.PendingClaimGroupGuard
	Events             oc.Appender
	CompensationEvents c.TaskBusyCompensationEvents
}

// Scheduler owns the outer commit, busy marker and skipped CAS. Work owns
// the canonical Task mutation or exact preservation and its private proof.
type TaskBusyCompensationService struct {
	store    Store
	deps     TaskBusyCompensationDependencies
	projects schedulerClaimProject
	mu       sync.Mutex
	stopped  bool
	calls    map[*schedulerClaimCall]struct{}
	changed  chan struct{}
}

func NewTaskBusyCompensation(store Store, deps TaskBusyCompensationDependencies) (*TaskBusyCompensationService, error) {
	if nilPort(store) || deps.Authority.state() == nil || !sameStore(store, deps.Authority.state().store) || nilPort(deps.Scheduler) || nilPort(deps.Pending) || nilPort(deps.Events) || !deps.CompensationEvents.Valid() {
		return nil, fault(f.DependencyUnbound)
	}
	projects, ok := deps.Authority.state().projects.(schedulerClaimProject)
	if !ok || nilPort(projects) {
		return nil, fault(f.DependencyUnbound)
	}
	return &TaskBusyCompensationService{store: store, deps: deps, projects: projects, calls: map[*schedulerClaimCall]struct{}{}, changed: make(chan struct{})}, nil
}

type taskBusyPlan struct {
	owner      *TaskBusyCompensationService
	actor      i.Actor
	request    c.TaskBusyCompensationRequest
	record     taskBusyRecord
	baseLocks  []f.LockRequest
	locks      []f.LockRequest
	appendPlan oc.AppendPlan
}

func (p *taskBusyPlan) RequiredLocks() []f.LockRequest {
	if p == nil {
		return nil
	}
	return slices.Clone(p.locks)
}

type taskBusyApplied struct {
	owner *TaskBusyCompensationService
	plan  *taskBusyPlan
	tx    f.Tx
}

func (v *taskBusyApplied) Restored() bool { return v != nil && v.plan != nil && v.plan.record.Restored }

type taskBusyContextKey struct{}
type taskBusyContext struct {
	plan    *taskBusyPlan
	applied *taskBusyApplied
}

func busyInput(ctx context.Context, actor i.Actor, r c.TaskBusyCompensationRequest) error {
	if ctx == nil || r.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return claimActor(actor, r.Claim)
}
func busyMutationLocks(r c.TaskBusyCompensationRequest, record taskBusyRecord) ([]f.LockRequest, error) {
	locks, err := claimDiscoveryLocks(r.Claim)
	if err != nil {
		return nil, err
	}
	command, err := f.NewCommandIdentity("scheduler", []string{r.Claim.ProjectID.String()}, "task_busy_compensation", f.IdempotencyKey("scheduler_busy:"+r.Claim.DispatchID))
	if err != nil {
		return nil, err
	}
	for n := range locks {
		if locks[n].Key.Canonical() == taskLock(r.Claim.TaskID.String(), f.Shared).Key.Canonical() {
			locks[n].Mode = f.Exclusive
		}
	}
	locks = append(locks, commandLock(command))
	for _, g := range record.Groups {
		locks = append(locks, taskRankLock(r.Claim.ProjectID, g.Group))
	}
	return taskNormalize(locks)
}
func (s *TaskBusyCompensationService) currentProject(ctx context.Context, tx f.Tx, r c.TaskBusyCompensationRequest) (pc.ProjectRef, error) {
	p, err := s.projects.RequireSchedulerProjectInTx(ctx, tx, r.Claim.ProjectID)
	if ctx.Err() != nil {
		return pc.ProjectRef{}, ctx.Err()
	}
	if err != nil {
		return pc.ProjectRef{}, portError(err)
	}
	if p.Project.Validate() != nil || p.Project.ID != r.Claim.ProjectID || p.Config.Validate() != nil {
		return pc.ProjectRef{}, internal(nil)
	}
	// Paused is not a preservation result: keep the confirmed-busy reservation
	// pending until resume. Current Sprint identity is evaluated separately.
	if p.Project.Lifecycle != pc.Active || !p.Config.Enabled {
		return pc.ProjectRef{}, fault(f.InvalidState)
	}
	return p.Project, nil
}
func (s *TaskBusyCompensationService) readBusySource(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, r c.TaskBusyCompensationRequest, guard c.TaskClaimGuard) (schedulerClaimRecord, c.Task, c.Sprint, *c.SprintID, error) {
	var claim schedulerClaimRecord
	var task c.Task
	var sprint c.Sprint
	project, err := s.currentProject(ctx, tx, r)
	if err != nil {
		return claim, task, sprint, nil, err
	}
	original, err := loadSchedulerClaim(ctx, x, r.Claim.ProjectID, r.Claim.DispatchID)
	if err != nil {
		return claim, task, sprint, nil, err
	}
	if original == nil || original.Request != r.Claim || !sameValue(original.Guard, guard) {
		return claim, task, sprint, nil, fault(f.Forbidden)
	}
	task, err = loadTask(ctx, x, r.Claim.ProjectID, r.Claim.TaskID)
	if err != nil {
		return claim, task, sprint, nil, err
	}
	sprint, err = loadSprint(ctx, x, r.Claim.ProjectID, original.Guard.SourceSprintID, project.CurrentSprintID)
	if err != nil {
		return claim, task, sprint, nil, err
	}
	if _, err = loadMilestone(ctx, x, r.Claim.ProjectID, sprint.MilestoneID); err != nil {
		return claim, task, sprint, nil, err
	}
	return *original, task, sprint, project.CurrentSprintID, nil
}
func (s *TaskBusyCompensationService) DiscoverTaskBusyCompensation(ctx context.Context, actor i.Actor, r c.TaskBusyCompensationRequest) (c.TaskBusyCompensationPlan, error) {
	ctx, done, err := s.beginBusy(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	if err = busyInput(ctx, actor, r); err != nil {
		return nil, err
	}
	locks, err := claimDiscoveryLocks(r.Claim)
	if err != nil {
		return nil, err
	}
	cause, err := readCause("task-busy-compensation")
	if err != nil {
		return nil, err
	}
	var record taskBusyRecord
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if e := s.store.AcquireAll(ctx, tx, locks); e != nil {
			return portError(e)
		}
		x, e := s.store.InTx(tx)
		if e != nil {
			return portError(e)
		}
		guard, e := s.deps.Scheduler.RequireTaskBusyCompensationDiscoveryInTx(ctx, tx, actor, r)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if e != nil {
			return portError(e)
		}
		claim, before, sprint, current, e := s.readBusySource(ctx, tx, x, r, guard)
		if e != nil {
			return e
		}
		existing, e := loadTaskBusyRecord(ctx, x, r.Claim.ProjectID, r.Claim.DispatchID)
		if e != nil {
			return e
		}
		if existing != nil {
			return fault(f.ConfirmationStale)
		}
		record, e = planTaskBusyRecord(ctx, x, r, claim, before, sprint, current)
		return e
	})
	if err = taskTxError(ctx, result); err != nil {
		return nil, err
	}
	if err = validateTaskBusyRecord(&record); err != nil {
		return nil, err
	}
	base, err := busyMutationLocks(r, record)
	if err != nil {
		return nil, err
	}
	p := &taskBusyPlan{owner: s, actor: actor, request: r.Clone(), record: record, baseLocks: base, locks: slices.Clone(base)}
	if record.Restored {
		ev, e := s.deps.CompensationEvents.NewTaskCompensated(*record.Header, *record.Event)
		if e != nil {
			return nil, internal(e)
		}
		proof := context.WithValue(ctx, taskBusyContextKey{}, taskBusyContext{plan: p})
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
func (s *TaskBusyCompensationService) originalPlan(ctx context.Context, actor i.Actor, r c.TaskBusyCompensationRequest, raw c.TaskBusyCompensationPlan) (*taskBusyPlan, error) {
	if err := busyInput(ctx, actor, r); err != nil {
		return nil, err
	}
	p, ok := raw.(*taskBusyPlan)
	if s == nil || !ok || p == nil || p.owner != s || !p.actor.Equal(actor) || p.request != r || len(p.locks) == 0 {
		return nil, fault(f.Forbidden)
	}
	return p, nil
}
func (s *TaskBusyCompensationService) requireCurrent(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, actor i.Actor, p *taskBusyPlan) error {
	guard, err := s.deps.Scheduler.RequireTaskBusyCompensationInTx(ctx, tx, actor, p.request, p)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return portError(err)
	}
	claim, before, sprint, current, err := s.readBusySource(ctx, tx, x, p.request, guard)
	if err != nil {
		return err
	}
	if !sameValue(claim, p.record.Claim) || !sameValue(before, p.record.Before) || !sameValue(sprint, p.record.Sprint) || !sameValue(current, p.record.CurrentSprintID) {
		return fault(f.ConfirmationStale)
	}
	existing, err := loadTaskBusyRecord(ctx, x, p.request.Claim.ProjectID, p.request.Claim.DispatchID)
	if err != nil {
		return err
	}
	if existing != nil {
		return fault(f.ConfirmationStale)
	}
	if p.record.Restored {
		// Scheduler's private proof protects the original todo group with only
		// its own pending Dispatch exempt. The in_progress group has no exemption.
		if err = s.deps.Pending.RequireNoPendingGroupsInTx(ctx, tx, p.request.Claim.ProjectID, pendingGroups(p.record.Groups[0].Group)); err != nil {
			return portError(err)
		}
		for _, g := range p.record.Groups {
			rows, gen, e := loadTaskRanks(ctx, x, p.request.Claim.ProjectID, g.Group)
			if e != nil {
				return e
			}
			if gen != g.Generation || !sameValue(rows, g.Before) {
				return fault(f.ConfirmationStale)
			}
		}
		q, e := loadTaskQueryGeneration(ctx, x, p.request.Claim.ProjectID)
		if e != nil {
			return e
		}
		if q != p.record.QueryGeneration {
			return fault(f.ConfirmationStale)
		}
	}
	return ctx.Err()
}
func (s *TaskBusyCompensationService) ApplyTaskBusyCompensationInTx(ctx context.Context, tx f.Tx, actor i.Actor, r c.TaskBusyCompensationRequest, raw c.TaskBusyCompensationPlan) (c.AppliedTaskBusyCompensation, error) {
	ctx, done, err := s.beginBusy(ctx)
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
	if err = applyTaskBusyRecord(ctx, x, &p.record); err != nil {
		return nil, err
	}
	applied := &taskBusyApplied{owner: s, plan: p, tx: tx}
	if p.record.Restored {
		ev, e := s.deps.CompensationEvents.NewTaskCompensated(*p.record.Header, *p.record.Event)
		if e != nil {
			return nil, internal(e)
		}
		proof := context.WithValue(ctx, taskBusyContextKey{}, taskBusyContext{plan: p, applied: applied})
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
	if err = verifyTaskBusyPostimage(ctx, x, &p.record); err != nil {
		return nil, err
	}
	return applied, nil
}
func (s *TaskBusyCompensationService) CheckTaskBusyCompensationAppliedInTx(ctx context.Context, tx f.Tx, actor i.Actor, r c.TaskBusyCompensationRequest, raw c.TaskBusyCompensationPlan, value c.AppliedTaskBusyCompensation) error {
	ctx, done, err := s.beginBusy(ctx)
	if err != nil {
		return err
	}
	defer done()
	p, err := s.originalPlan(ctx, actor, r, raw)
	if err != nil {
		return err
	}
	v, ok := value.(*taskBusyApplied)
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
	guard, err := s.deps.Scheduler.RequireTaskBusyCompensationInTx(ctx, tx, actor, r, p)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return portError(err)
	}
	if !sameValue(guard, p.record.Claim.Guard) {
		return fault(f.Forbidden)
	}
	if _, err = s.currentProject(ctx, tx, r); err != nil {
		return err
	}
	return verifyTaskBusyPostimage(ctx, x, &p.record)
}

var _ c.SchedulerTaskBusyCompensations = (*TaskBusyCompensationService)(nil)

func (s *TaskBusyCompensationService) beginBusy(ctx context.Context) (context.Context, func(), error) {
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
func (s *TaskBusyCompensationService) Stop() {
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
func (s *TaskBusyCompensationService) Drain(ctx context.Context) error {
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
func (s *TaskBusyCompensationService) Joined() bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped && len(s.calls) == 0
}
