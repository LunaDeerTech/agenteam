package scheduler

import (
	"context"
	"sync"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type relaunchContextKey struct{}
type relaunchCall struct {
	owner          *RelaunchCoordinator
	request        wc.TaskRelaunchRequest
	actor          i.Actor
	launch         ec.LaunchRequest
	raw            []byte
	cancel         context.CancelCauseFunc
	mu             sync.Mutex
	live, applying bool
	tx             f.Tx
	plan           wc.TaskRelaunchPlan
	locks          []f.LockRequest
	unknown        error
	resolving      bool // owner.mu
}

func relaunchCommand(r wc.TaskRelaunchRequest) f.CommandIdentity {
	c, _ := f.NewCommandIdentity("scheduler", []string{r.ProjectID.String()}, "relaunch", f.IdempotencyKey("scheduler_relaunch:"+r.DispatchID))
	return c
}
func relaunchLocks(r wc.TaskRelaunchRequest, launch ec.LaunchRequest) ([]f.LockRequest, error) {
	ck, _ := f.CommandLock(relaunchCommand(r))
	lc, _ := launch.Command()
	lk, _ := f.CommandLock(lc)
	ak, _ := f.AgentLock(r.AgentID.String())
	tk, _ := f.AggregateLock(f.TaskAggregate, r.TaskID.String())
	dk, _ := f.AggregateLock(f.DispatchAggregate, r.DispatchID)
	return oc.NormalizeLocks(append(pendingLocks(r.ProjectID), f.LockRequest{Key: ck, Mode: f.Exclusive}, f.LockRequest{Key: lk, Mode: f.Exclusive}, f.LockRequest{Key: ak, Mode: f.Shared}, f.LockRequest{Key: tk, Mode: f.Shared}, f.LockRequest{Key: dk, Mode: f.Exclusive}))
}
func (s *RelaunchCoordinator) beginRelaunch(ctx context.Context, r wc.TaskRelaunchRequest, policy ec.Policy) (context.Context, *relaunchCall, error) {
	if ctx == nil || r.Validate() != nil || policy.Validate() != nil {
		return nil, nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if s == nil || s.coordinator == nil {
		return nil, nil, fault(f.DependencyUnbound)
	}
	actor, err := schedulerActor(r.ProjectID, r.DispatchID)
	if err != nil {
		return nil, nil, err
	}
	id, _ := f.ParseID[DispatchIdentity](r.DispatchID)
	launch := ec.LaunchRequest{ProjectID: r.ProjectID, AgentID: r.AgentID, Trigger: ec.Trigger{Kind: "task", TaskID: r.TaskID.String()}, Purpose: r.Purpose, Policy: policy.Clone(), Lineage: ec.Lineage{DispatchID: r.DispatchID}, Meta: f.CommandMeta{RequestID: r.RequestID, IdempotencyKey: launchKey(id)}}
	if launch.Validate() != nil {
		return nil, nil, invalid()
	}
	raw, err := encodeRelaunchIntent(r, policy, s.coordinator.retryPolicy)
	if err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return nil, nil, fault(f.ShuttingDown)
	}
	if prior := s.unknown[id]; prior != nil {
		return nil, nil, prior.unknown
	}
	for c := range s.calls {
		if c.request.ProjectID == r.ProjectID && c.request.TaskID == r.TaskID {
			return nil, nil, fault(f.ResourceBusy)
		}
	}
	owned, cancel := context.WithCancelCause(ctx)
	c := &relaunchCall{owner: s, request: r.Clone(), actor: actor, launch: launch, raw: raw, cancel: cancel, live: true}
	s.calls[c] = struct{}{}
	return context.WithValue(owned, relaunchContextKey{}, c), c, nil
}
func (s *RelaunchCoordinator) releaseRelaunch(c *relaunchCall) {
	c.mu.Lock()
	c.live = false
	c.tx = f.Tx{}
	c.mu.Unlock()
	c.cancel(nil)
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.calls, c)
	if s.stopped && len(s.calls) == 0 {
		select {
		case <-s.drained:
		default:
			close(s.drained)
		}
	}
}
func (s *RelaunchCoordinator) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	cancels := make([]context.CancelCauseFunc, 0, len(s.calls))
	for c := range s.calls {
		cancels = append(cancels, c.cancel)
	}
	if len(cancels) == 0 {
		close(s.drained)
	}
	s.mu.Unlock()
	for _, cancel := range cancels {
		cancel(fault(f.ShuttingDown))
	}
}
func (s *RelaunchCoordinator) Drain(ctx context.Context) error {
	if ctx == nil {
		return invalid()
	}
	if s == nil {
		return nil
	}
	select {
	case <-s.drained:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *RelaunchCoordinator) Joined() bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped && len(s.calls) == 0
}

func (s *RelaunchCoordinator) projectInTx(ctx context.Context, tx f.Tx, r wc.TaskRelaunchRequest, enabled bool) (pc.SchedulerProject, error) {
	p, err := s.coordinator.deps.Projects.RequireSchedulerProjectInTx(ctx, tx, r.ProjectID)
	if err != nil {
		return pc.SchedulerProject{}, portError(err)
	}
	if p.Project.ID != r.ProjectID || p.Project.Validate() != nil || p.Config.Validate() != nil {
		return pc.SchedulerProject{}, unavailable(nil)
	}
	if enabled && (!p.Config.Enabled || p.Project.CurrentSprintID == nil || *p.Project.CurrentSprintID != r.CurrentSprintID) {
		return pc.SchedulerProject{}, fault(f.InvalidState)
	}
	return p, ctx.Err()
}

// VisitRelaunch performs at most one durable cooldown visit or a new immutable
// Work-origin/pending pair. It never calls Launch and never waits for a Model.
func (s *RelaunchCoordinator) VisitRelaunch(ctx context.Context, r wc.TaskRelaunchRequest, policy ec.Policy) (RelaunchVisit, error) {
	ctx, call, err := s.beginRelaunch(ctx, r, policy)
	if err != nil {
		return RelaunchVisit{}, err
	}
	retained := false
	defer func() {
		if !retained {
			s.releaseRelaunch(call)
		}
	}()
	a := s.coordinator.authority
	base, err := relaunchLocks(r, call.launch)
	if err != nil {
		return RelaunchVisit{}, err
	}
	cause, _ := f.NewCommandsCause(relaunchCommand(r))
	var runtime *relaunchRuntime
	var latest *dispatchRecord
	var replay *relaunchReceipt
	var out RelaunchVisit
	result := a.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := a.store.AcquireAll(ctx, tx, base); err != nil {
			return portError(err)
		}
		x, err := a.inTx(ctx, tx, r.ProjectID)
		if err != nil {
			return err
		}
		replay, err = loadRelaunchReceipt(ctx, x, r)
		if err != nil {
			return err
		}
		if replay != nil {
			out, err = relaunchReceiptResult(ctx, x, call, replay, false)
			return err
		}
		if _, err = s.projectInTx(ctx, tx, r, true); err != nil {
			return err
		}
		runtime, err = loadRelaunchRuntime(ctx, x, r.ProjectID, r.TaskID)
		if err != nil {
			return err
		}
		latest, err = loadRelaunchLatest(ctx, x, r.ProjectID, r.TaskID, runtime)
		return err
	})
	if err = commitError(result); err != nil {
		return RelaunchVisit{}, err
	}
	if replay != nil {
		return out, nil
	}
	plan, err := s.tasks.DiscoverTaskRelaunch(ctx, call.actor, r)
	if err != nil {
		return RelaunchVisit{}, portError(err)
	}
	if nilPort(plan) {
		return RelaunchVisit{}, fault(f.DependencyUnbound)
	}
	locks := append(base, plan.RequiredLocks()...)
	if latest != nil {
		locks = append(locks, executionIntentLocks(latest)...)
	}
	locks, err = oc.NormalizeLocks(locks)
	if err != nil {
		return RelaunchVisit{}, portError(err)
	}
	result = a.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := a.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := a.inTx(ctx, tx, r.ProjectID)
		if err != nil {
			return err
		}
		receipt, err := loadRelaunchReceipt(ctx, x, r)
		if err != nil {
			return err
		}
		if receipt != nil {
			out, err = relaunchReceiptResult(ctx, x, call, receipt, false)
			if err == nil {
				call.raw = append([]byte(nil), receipt.raw...)
			}
			return err
		}
		project, err := s.projectInTx(ctx, tx, r, true)
		if err != nil {
			return err
		}
		current, err := s.current.CurrentTaskInTx(ctx, tx, r.ProjectID, r.TaskID)
		if err != nil {
			return portError(err)
		}
		if current.ProjectID != r.ProjectID || current.TaskID != r.TaskID || current.SprintID != r.CurrentSprintID || current.Version != r.ExpectedTaskVersion || current.MilestoneID.Validate() != nil || current.Priority.Validate() != nil || current.State != wc.TaskStateInProgress || current.AssigneeAgentID == nil || *current.AssigneeAgentID != r.AgentID || current.HasUnresolvedBlockers {
			return fault(f.ConfirmationStale)
		}
		var pending bool
		if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_scheduler.dispatches WHERE project_id=$1 AND task_id=$2 AND status='pending')`, r.ProjectID.String(), r.TaskID.String()).Scan(&pending); err != nil {
			return portError(err)
		}
		if pending {
			return fault(f.ResourceBusy)
		}
		occupied, err := s.occupancy.ReadInTx(ctx, tx, r.ProjectID, []string{r.TaskID.String()})
		if err != nil {
			return portError(err)
		}
		if occupied.Active == nil || occupied.HistoryTaskIDs == nil {
			return unavailable(nil)
		}
		for _, v := range occupied.Active {
			if v.TaskID != r.TaskID.String() || v.ExecutionID.Validate() != nil || v.AgentID.Validate() != nil || !v.Status.Valid() || v.Status.Terminal() {
				return unavailable(nil)
			}
		}
		if len(occupied.Active) != 0 {
			return fault(f.ResourceBusy)
		}
		actual, err := loadRelaunchRuntime(ctx, x, r.ProjectID, r.TaskID)
		if err != nil {
			return err
		}
		if !sameRelaunchRuntime(runtime, actual) {
			return fault(f.ConfirmationStale)
		}
		latestNow, err := loadRelaunchLatest(ctx, x, r.ProjectID, r.TaskID, actual)
		if err != nil {
			return err
		}
		if (latest == nil) != (latestNow == nil) || latest != nil && (!sameDispatch(latest, latestNow) || latest.execution == nil || latestNow.execution == nil || *latest.execution != *latestNow.execution) {
			return fault(f.ConfirmationStale)
		}
		if latestNow != nil {
			observation, err := s.coordinator.deps.Executions.LookupLaunchInTx(ctx, tx, lookupKey(latestNow), latestNow.digest, latestNow.id.String())
			if err != nil {
				return portError(err)
			}
			if !observation.Found || observation.RequestDigest != latestNow.digest || observation.Execution == nil || observation.Execution.ID != *latestNow.execution || !matchesExecution(latestNow, *observation.Execution) || !observation.Execution.Status.Terminal() || observation.Execution.CompletedAt == nil || observation.Execution.CompletedAt.Validate() != nil {
				return fault(f.ConfirmationStale)
			}
			remaining := s.skipCount
			if actual != nil && actual.cooldownExecution != nil {
				remaining = actual.remaining
			}
			if remaining > 0 {
				next, err := nextRelaunchRuntime(actual, r.ProjectID, r.TaskID, latestNow)
				if err != nil {
					return err
				}
				id := *latestNow.execution
				next.cooldownExecution = &id
				next.remaining = remaining - 1
				var previous f.Version
				if actual != nil {
					previous = actual.version
				}
				if err = writeRelaunchRuntime(ctx, x, next, previous); err != nil {
					return err
				}
				out = RelaunchVisit{CooldownSkipped: true, Remaining: next.remaining}
				return insertRelaunchReceipt(ctx, x, call, out)
			}
		}
		capacity, err := readCapacityRecords(ctx, x, r.ProjectID)
		if err != nil {
			return err
		}
		used, err := s.coordinator.capacityInTx(ctx, tx, r.ProjectID, capacity)
		if err != nil {
			return err
		}
		if project.Config.MaxConcurrency != nil && used >= *project.Config.MaxConcurrency {
			return fault(f.ResourceBusy)
		}
		slot, err := s.coordinator.deps.Executions.AgentSlotInTx(ctx, tx, r.ProjectID, r.AgentID)
		if err != nil {
			return portError(err)
		}
		if slot.Occupied {
			if slot.ExecutionID == nil || slot.ExecutionID.Validate() != nil || !slot.Status.Valid() || slot.Status.Terminal() {
				return unavailable(nil)
			}
			return fault(f.AgentBusy)
		}
		if slot.ExecutionID != nil || slot.Status != "" || slot.CancelRequestedAt != nil {
			return unavailable(nil)
		}
		call.mu.Lock()
		call.applying = true
		call.tx = tx
		call.plan = plan
		call.locks = append([]f.LockRequest(nil), locks...)
		call.mu.Unlock()
		defer func() {
			call.mu.Lock()
			call.applying = false
			call.tx = f.Tx{}
			call.plan = nil
			call.locks = nil
			call.mu.Unlock()
		}()
		applied, err := s.tasks.RecordTaskRelaunchInTx(ctx, tx, call.actor, r, plan)
		if err != nil {
			return portError(err)
		}
		if nilPort(applied) {
			return fault(f.DependencyUnbound)
		}
		if err = s.tasks.CheckTaskRelaunchAppliedInTx(ctx, tx, call.actor, r, plan, applied); err != nil {
			return portError(err)
		}
		source := applied.Source().Clone()
		if source.Validate() != nil || source.Request != r || source.MilestoneID != current.MilestoneID {
			return unavailable(nil)
		}
		id, _ := f.ParseID[DispatchIdentity](r.DispatchID)
		now, _ := f.NewInstant(time.Now().UTC().Truncate(time.Microsecond))
		digest, _ := call.launch.Digest()
		dispatch := &dispatchRecord{id: id, project: r.ProjectID, task: r.TaskID.String(), sprint: r.CurrentSprintID.String(), agent: r.AgentID, launch: call.launch.Clone(), digest: digest, retryPolicy: s.coordinator.retryPolicy, status: Pending, outcome: NotSent, version: 1, relaunch: &source, createdAt: now, updatedAt: now}
		if err = insertDispatch(ctx, x, dispatch); err != nil {
			return err
		}
		out = RelaunchVisit{Dispatch: snapshot(dispatch)}
		return insertRelaunchReceipt(ctx, x, call, out)
	})
	err = commitError(result)
	if result.State() == f.Unknown {
		call.mu.Lock()
		call.live = false
		call.unknown = err
		call.mu.Unlock()
		id, _ := f.ParseID[DispatchIdentity](r.DispatchID)
		s.mu.Lock()
		s.unknown[id] = call
		s.mu.Unlock()
		retained = true
		return RelaunchVisit{}, err
	}
	if err != nil {
		return RelaunchVisit{}, err
	}
	return out, nil
}
func sameRelaunchRuntime(a, b *relaunchRuntime) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.project == b.project && a.task == b.task && a.latestDispatch == b.latestDispatch && a.latestExecution == b.latestExecution && a.remaining == b.remaining && a.version == b.version && a.updatedAt == b.updatedAt && ((a.cooldownExecution == nil && b.cooldownExecution == nil) || (a.cooldownExecution != nil && b.cooldownExecution != nil && *a.cooldownExecution == *b.cooldownExecution))
}

// ResolveRelaunch only observes the original immutable visit. Absence, changed
// bytes or failed reads retain the original physical error and owner.
func (s *RelaunchCoordinator) ResolveRelaunch(ctx context.Context, r wc.TaskRelaunchRequest) (RelaunchVisit, error) {
	if ctx == nil || r.Validate() != nil {
		return RelaunchVisit{}, invalid()
	}
	if err := ctx.Err(); err != nil {
		return RelaunchVisit{}, err
	}
	if s == nil {
		return RelaunchVisit{}, fault(f.DependencyUnbound)
	}
	id, _ := f.ParseID[DispatchIdentity](r.DispatchID)
	s.mu.Lock()
	call := s.unknown[id]
	if call == nil || call.request != r {
		s.mu.Unlock()
		return RelaunchVisit{}, fault(f.NotFound)
	}
	if call.resolving {
		s.mu.Unlock()
		return RelaunchVisit{}, fault(f.ResourceBusy)
	}
	call.resolving = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); call.resolving = false; s.mu.Unlock() }()
	locks, err := relaunchLocks(r, call.launch)
	if err != nil {
		return RelaunchVisit{}, err
	}
	cause, _ := f.NewCommandsCause(relaunchCommand(r))
	a := s.coordinator.authority
	var found *relaunchReceipt
	var out RelaunchVisit
	result := a.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := a.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := a.inTx(ctx, tx, r.ProjectID)
		if err != nil {
			return err
		}
		found, err = loadRelaunchReceipt(ctx, x, r)
		if err != nil || found == nil {
			return err
		}
		out, err = relaunchReceiptResult(ctx, x, call, found, true)
		return err
	})
	if result.State() != f.Committed || found == nil || ctx.Err() != nil {
		return RelaunchVisit{}, call.unknown
	}
	s.mu.Lock()
	delete(s.unknown, id)
	s.mu.Unlock()
	s.releaseRelaunch(call)
	return out, nil
}
