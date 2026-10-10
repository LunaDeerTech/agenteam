package work

import (
	"context"
	"slices"
	"sync"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type SchedulerClaimDependencies struct {
	Authority   *Authority
	Scheduler   c.SchedulerClaimAuthority
	Agents      ac.SchedulerCurrentReferences
	Pending     TaskTransitionPending
	Occupancy   ec.WorkOccupancyReader
	Events      oc.Appender
	ClaimEvents c.SchedulerClaimEvents
}
type schedulerClaimProject interface {
	RequireSchedulerProjectInTx(context.Context, f.Tx, i.ProjectID) (pc.SchedulerProject, error)
}

// Scheduler owns admission, cancellation and the outer commit. This provider
// owns only protected discovery and synchronous work inside the original Tx.
type SchedulerClaimService struct {
	store    Store
	deps     SchedulerClaimDependencies
	projects schedulerClaimProject
	mu       sync.Mutex
	stopped  bool
	calls    map[*schedulerClaimCall]struct{}
	changed  chan struct{}
}
type schedulerClaimCall struct{ cancel context.CancelFunc }

func NewSchedulerClaim(store Store, deps SchedulerClaimDependencies) (*SchedulerClaimService, error) {
	if nilPort(store) || deps.Authority.state() == nil || !sameStore(store, deps.Authority.state().store) || nilPort(deps.Scheduler) || nilPort(deps.Agents) || nilPort(deps.Pending) || nilPort(deps.Occupancy) || nilPort(deps.Events) || !deps.ClaimEvents.Valid() {
		return nil, fault(f.DependencyUnbound)
	}
	projects, ok := deps.Authority.state().projects.(schedulerClaimProject)
	if !ok || nilPort(projects) {
		return nil, fault(f.DependencyUnbound)
	}
	return &SchedulerClaimService{store: store, deps: deps, projects: projects, calls: map[*schedulerClaimCall]struct{}{}, changed: make(chan struct{})}, nil
}

type schedulerClaimPlan struct {
	owner      *SchedulerClaimService
	actor      i.Actor
	request    c.TaskClaimRequest
	record     schedulerClaimRecord
	baseLocks  []f.LockRequest
	locks      []f.LockRequest
	appendPlan oc.AppendPlan
}

func (p *schedulerClaimPlan) RequiredLocks() []f.LockRequest {
	if p == nil {
		return nil
	}
	return slices.Clone(p.locks)
}

type schedulerAppliedClaim struct {
	owner *SchedulerClaimService
	plan  *schedulerClaimPlan
	tx    f.Tx
}

func (v *schedulerAppliedClaim) Guard() c.TaskClaimGuard {
	if v == nil || v.plan == nil {
		return c.TaskClaimGuard{}
	}
	return v.plan.record.Guard.Clone()
}

type schedulerClaimContextKey struct{}
type schedulerClaimContext struct {
	plan    *schedulerClaimPlan
	applied *schedulerAppliedClaim
}

func claimActor(actor i.Actor, r c.TaskClaimRequest) error {
	if actor.Validate() != nil {
		return fault(f.Unauthenticated)
	}
	d := actor.Details()
	if d.Kind != i.Service || d.ServiceName != i.Scheduler || d.ProjectID != r.ProjectID.String() || d.CauseRef != r.DispatchID {
		return fault(f.Forbidden)
	}
	return nil
}
func claimInput(ctx context.Context, actor i.Actor, r c.TaskClaimRequest) error {
	if ctx == nil || r.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return claimActor(actor, r)
}
func claimDiscoveryLocks(r c.TaskClaimRequest) ([]f.LockRequest, error) {
	ak, _ := f.AgentLock(r.AgentID.String())
	return taskNormalize([]f.LockRequest{projectLock(r.ProjectID, f.Shared), taskScheduleLock(r.ProjectID, f.Exclusive), {Key: ak, Mode: f.Shared}, sprintLock(r.CurrentSprintID.String(), f.Shared), taskLock(r.TaskID.String(), f.Shared)})
}
func claimMutationLocks(r c.TaskClaimRequest, b c.Task) ([]f.LockRequest, error) {
	command, err := f.NewCommandIdentity("scheduler", []string{r.ProjectID.String()}, "claim", f.IdempotencyKey("scheduler_claim:"+r.DispatchID))
	if err != nil {
		return nil, err
	}
	ak, _ := f.AgentLock(r.AgentID.String())
	source := groupForTask(b)
	target := source
	target.State = c.TaskStateInProgress
	return taskNormalize([]f.LockRequest{commandLock(command), projectLock(r.ProjectID, f.Shared), taskScheduleLock(r.ProjectID, f.Exclusive), taskRankLock(r.ProjectID, source), taskRankLock(r.ProjectID, target), {Key: ak, Mode: f.Shared}, sprintLock(r.CurrentSprintID.String(), f.Shared), taskLock(r.TaskID.String(), f.Exclusive)})
}
func (s *SchedulerClaimService) currentProject(ctx context.Context, tx f.Tx, r c.TaskClaimRequest) (pc.ProjectRef, error) {
	v, err := s.projects.RequireSchedulerProjectInTx(ctx, tx, r.ProjectID)
	if ctx.Err() != nil {
		return pc.ProjectRef{}, ctx.Err()
	}
	if err != nil {
		return pc.ProjectRef{}, portError(err)
	}
	if v.Project.Validate() != nil || v.Project.ID != r.ProjectID || v.Config.Validate() != nil {
		return pc.ProjectRef{}, internal(nil)
	}
	if v.Project.Lifecycle != pc.Active || !v.Config.Enabled || v.Project.CurrentSprintID == nil || *v.Project.CurrentSprintID != r.CurrentSprintID {
		return pc.ProjectRef{}, fault(f.InvalidState)
	}
	return v.Project, nil
}
func (s *SchedulerClaimService) DiscoverTaskClaim(ctx context.Context, actor i.Actor, r c.TaskClaimRequest) (c.TaskClaimPlan, error) {
	ctx, done, beginErr := s.beginClaim(ctx)
	if beginErr != nil {
		return nil, beginErr
	}
	defer done()
	if err := claimInput(ctx, actor, r); err != nil {
		return nil, err
	}
	if s == nil || nilPort(s.store) {
		return nil, fault(f.DependencyUnbound)
	}
	locks, err := claimDiscoveryLocks(r)
	if err != nil {
		return nil, err
	}
	cause, err := readCause("task-scheduler-claim")
	if err != nil {
		return nil, err
	}
	var record schedulerClaimRecord
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if err = s.deps.Scheduler.RequireTaskClaimDiscoveryInTx(ctx, tx, actor, r); err != nil {
			return portError(err)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		project, err := s.currentProject(ctx, tx, r)
		if err != nil {
			return err
		}
		before, err := loadTask(ctx, x, r.ProjectID, r.TaskID)
		if err != nil {
			return err
		}
		if err = validateClaimTask(before, r); err != nil {
			return err
		}
		sprint, err := loadSprint(ctx, x, r.ProjectID, before.SprintID, project.CurrentSprintID)
		if err != nil {
			return err
		}
		if sprint.State != c.Current || sprint.MilestoneID != before.MilestoneID {
			return fault(f.TaskSprintInvalid)
		}
		if _, err = loadMilestone(ctx, x, r.ProjectID, before.MilestoneID); err != nil {
			return err
		}
		record, err = planSchedulerClaim(ctx, x, r, before)
		return err
	})
	if err = taskTxError(ctx, result); err != nil {
		return nil, err
	}
	base, err := claimMutationLocks(r, record.Before)
	if err != nil {
		return nil, err
	}
	plan := &schedulerClaimPlan{owner: s, actor: actor, request: r.Clone(), record: record, baseLocks: base}
	if err = validateSchedulerClaimRecord(&record); err != nil {
		return nil, err
	}
	ev, err := s.deps.ClaimEvents.NewTaskClaimed(record.Header, record.Event)
	if err != nil {
		return nil, internal(err)
	}
	proof := context.WithValue(ctx, schedulerClaimContextKey{}, schedulerClaimContext{plan: plan})
	plan.appendPlan, err = s.deps.Events.PrepareAppend(proof, actor, ev)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, portError(err)
	}
	plan.locks, err = taskNormalize(append(slices.Clone(base), plan.appendPlan.Locks()...))
	if err != nil {
		return nil, err
	}
	return plan, nil
}
func (s *SchedulerClaimService) originalPlan(ctx context.Context, actor i.Actor, r c.TaskClaimRequest, raw c.TaskClaimPlan) (*schedulerClaimPlan, error) {
	if err := claimInput(ctx, actor, r); err != nil {
		return nil, err
	}
	p, ok := raw.(*schedulerClaimPlan)
	if s == nil || !ok || p == nil || p.owner != s || !p.actor.Equal(actor) || p.request != r || len(p.locks) == 0 {
		return nil, fault(f.Forbidden)
	}
	return p, nil
}
func (s *SchedulerClaimService) ApplyTaskClaimInTx(ctx context.Context, tx f.Tx, actor i.Actor, r c.TaskClaimRequest, raw c.TaskClaimPlan) (c.AppliedTaskClaim, error) {
	ctx, done, beginErr := s.beginClaim(ctx)
	if beginErr != nil {
		return nil, beginErr
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
	if err = s.deps.Scheduler.RequireTaskClaimInTx(ctx, tx, actor, r, p); err != nil {
		return nil, portError(err)
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if _, err = s.currentProject(ctx, tx, r); err != nil {
		return nil, err
	}
	if err = s.requireClaimCurrent(ctx, tx, x, actor, p); err != nil {
		return nil, err
	}
	if err = applySchedulerClaim(ctx, x, &p.record); err != nil {
		return nil, err
	}
	applied := &schedulerAppliedClaim{owner: s, plan: p, tx: tx}
	proof := context.WithValue(ctx, schedulerClaimContextKey{}, schedulerClaimContext{plan: p, applied: applied})
	ev, err := s.deps.ClaimEvents.NewTaskClaimed(p.record.Header, p.record.Event)
	if err != nil {
		return nil, internal(err)
	}
	receipt, err := s.deps.Events.AppendEventInTx(proof, tx, actor, ev, p.appendPlan)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, portError(err)
	}
	if receipt.EventID != p.record.Header.EventID || receipt.Sequence.Validate() != nil {
		return nil, internal(nil)
	}
	if err = verifySchedulerClaimPostimage(ctx, x, &p.record); err != nil {
		return nil, err
	}
	return applied, nil
}
func (s *SchedulerClaimService) CheckTaskClaimAppliedInTx(ctx context.Context, tx f.Tx, actor i.Actor, r c.TaskClaimRequest, raw c.TaskClaimPlan, value c.AppliedTaskClaim) error {
	ctx, done, beginErr := s.beginClaim(ctx)
	if beginErr != nil {
		return beginErr
	}
	defer done()
	p, err := s.originalPlan(ctx, actor, r, raw)
	if err != nil {
		return err
	}
	applied, ok := value.(*schedulerAppliedClaim)
	if !ok || applied == nil || applied.owner != s || applied.plan != p || applied.tx != tx {
		return fault(f.Forbidden)
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if err = s.store.RequireHeldLocks(ctx, tx, p.locks); err != nil {
		return portError(err)
	}
	if err = s.deps.Scheduler.RequireTaskClaimInTx(ctx, tx, actor, r, p); err != nil {
		return portError(err)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err = verifySchedulerClaimPostimage(ctx, x, &p.record); err != nil {
		return err
	}
	return ctx.Err()
}
func validateClaimTask(t c.Task, r c.TaskClaimRequest) error {
	if t.Validate() != nil || t.ProjectID != r.ProjectID || t.ID != r.TaskID {
		return internal(nil)
	}
	if t.Version != r.ExpectedTaskVersion {
		return fault(f.TaskVersionConflict)
	}
	if t.State != c.TaskStateTodo || t.SprintID != r.CurrentSprintID {
		return fault(f.InvalidState)
	}
	if t.AssigneeAgentID == nil || *t.AssigneeAgentID != r.AgentID {
		return field(f.InvalidState, "/agent_id", "TASK_ASSIGNEE_INVALID")
	}
	return nil
}
func (s *SchedulerClaimService) requireClaimCurrent(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, actor i.Actor, p *schedulerClaimPlan) error {
	r := p.request
	b, err := loadTask(ctx, x, r.ProjectID, r.TaskID)
	if err != nil {
		return err
	}
	if err = validateClaimTask(b, r); err != nil {
		return err
	}
	if !sameValue(b, p.record.Before) {
		return fault(f.ConfirmationStale)
	}
	sprint, err := loadSprint(ctx, x, r.ProjectID, b.SprintID, &r.CurrentSprintID)
	if err != nil {
		return err
	}
	if sprint.State != c.Current || sprint.MilestoneID != b.MilestoneID {
		return fault(f.TaskSprintInvalid)
	}
	if _, err = loadMilestone(ctx, x, r.ProjectID, b.MilestoneID); err != nil {
		return err
	}
	a, err := s.deps.Agents.RequireSchedulerCurrentInTx(ctx, tx, actor, r.ProjectID, r.AgentID)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return portError(err)
	}
	if a.Validate() != nil || a.ProjectID != r.ProjectID || a.AgentID != r.AgentID {
		return internal(nil)
	}
	o, err := s.deps.Occupancy.ReadInTx(ctx, tx, r.ProjectID, []string{r.TaskID.String()})
	if err != nil {
		return portError(err)
	}
	if o.Active == nil || o.HistoryTaskIDs == nil {
		return internal(nil)
	}
	if len(o.Active) > 0 {
		return fault(f.ResourceBusy)
	}
	d, err := s.deps.Pending.ReadInTx(ctx, tx, r.ProjectID, []string{r.TaskID.String()})
	if err != nil {
		return portError(err)
	}
	if d.Pending == nil || d.HistoryTaskIDs == nil {
		return internal(nil)
	}
	if len(d.Pending) > 0 {
		return fault(f.ResourceBusy)
	}
	if err = s.deps.Pending.RequireNoPendingGroupsInTx(ctx, tx, r.ProjectID, pendingGroups(p.record.Groups[0].Group, p.record.Groups[1].Group)); err != nil {
		return portError(err)
	}
	var blockers int64
	if err = x.QueryRow(ctx, `SELECT count(*) FROM agenteam_work.task_blockers WHERE project_id=$1 AND task_id=$2 AND resolved_at IS NULL`, r.ProjectID.String(), r.TaskID.String()).Scan(&blockers); err != nil {
		return taskSQL(err)
	}
	if blockers < 0 {
		return internal(nil)
	}
	if blockers > 0 {
		return field(f.InvalidState, "/task_id", "UNRESOLVED_BLOCKERS")
	}
	for _, g := range p.record.Groups {
		rows, gen, e := loadTaskRanks(ctx, x, r.ProjectID, g.Group)
		if e != nil {
			return e
		}
		if gen != g.Generation || !sameValue(rows, g.Before) {
			return fault(f.ConfirmationStale)
		}
	}
	q, err := loadTaskQueryGeneration(ctx, x, r.ProjectID)
	if err != nil {
		return err
	}
	if q != p.record.QueryGeneration {
		return fault(f.ConfirmationStale)
	}
	return ctx.Err()
}

var _ c.SchedulerTaskClaims = (*SchedulerClaimService)(nil)

func (s *SchedulerClaimService) beginClaim(ctx context.Context) (context.Context, func(), error) {
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
func (s *SchedulerClaimService) Stop() {
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
func (s *SchedulerClaimService) Drain(ctx context.Context) error {
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
func (s *SchedulerClaimService) Joined() bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped && len(s.calls) == 0
}
