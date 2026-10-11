package scheduler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"sync"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// Options are explicit deployment inputs. LaunchPolicy is copied and applies
// only to newly claimed Dispatches; replay keeps each original stored policy.
type ProjectRunnerOptions struct {
	ProjectID    i.ProjectID
	TickInterval time.Duration
	LaunchPolicy ec.Policy
	Tasks        wc.SchedulerTaskReader
}

type ProjectVisitAction string

const (
	ProjectVisitDeferred ProjectVisitAction = "deferred"
	ProjectVisitPending  ProjectVisitAction = "pending"
	ProjectVisitClaim    ProjectVisitAction = "claim"
)

// Visits are observations, not successful-mutation receipts. Err preserves the
// original owner's error, including unresolved physical CommitUnknown. A
// pending entry has no expected Work group and therefore ExpectedState="".
type ProjectTaskVisit struct {
	TaskID        wc.TaskID
	DispatchID    *DispatchID
	ClaimRequest  *wc.TaskClaimRequest
	ExpectedState wc.TaskState
	Action        ProjectVisitAction
	Dispatch      Dispatch
	Err           error
}
type ProjectRunResult struct {
	ProjectID       i.ProjectID
	CurrentSprintID *wc.SprintID
	Visits          []ProjectTaskVisit
	Executions      []ProjectExecutionVisit
}

// ProjectRunError carries the final bounded observation when continuous Run
// stops. Recovery uses these original identities with the borrowed owners;
// this projection cannot mint a claim/launch permit. Default logging is safe.
type ProjectRunError struct {
	result ProjectRunResult
	cause  error
}

func (e *ProjectRunError) Error() string                 { return "scheduler_project_run" }
func (e *ProjectRunError) Unwrap() error                 { return e.cause }
func (e *ProjectRunError) Observation() ProjectRunResult { return cloneProjectRunResult(e.result) }
func (*ProjectRunError) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "scheduler_project_run")
}
func (*ProjectRunError) LogValue() slog.Value { return slog.StringValue("scheduler_project_run") }
func (ProjectTaskVisit) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "scheduler_project_visit")
}
func (ProjectTaskVisit) LogValue() slog.Value { return slog.StringValue("scheduler_project_visit") }
func (ProjectRunResult) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "scheduler_project_run_result")
}
func (ProjectRunResult) LogValue() slog.Value {
	return slog.StringValue("scheduler_project_run_result")
}
func cloneProjectRunResult(v ProjectRunResult) ProjectRunResult {
	v.CurrentSprintID = cloneSprintID(v.CurrentSprintID)
	v.Visits = slices.Clone(v.Visits)
	v.Executions = slices.Clone(v.Executions)
	for n := range v.Executions {
		v.Executions[n].Execution = v.Executions[n].Execution.Clone()
	}
	for n := range v.Visits {
		entry := &v.Visits[n]
		if entry.DispatchID != nil {
			id := *entry.DispatchID
			entry.DispatchID = &id
		}
		if entry.ClaimRequest != nil {
			r := entry.ClaimRequest.Clone()
			entry.ClaimRequest = &r
		}
	}
	return v
}
func uncertainProjectVisit(err error) bool {
	var known *f.Fault
	return errors.As(err, &known) && (known.CommitState == f.Unknown || known.Code == f.CommitUnknown)
}

// ProjectRunner owns one serial traversal at a time. It borrows the original
// Coordinator and PendingVisitor, including their retained Unknown calls.
// Stop/Drain cover this runner's synchronous calls and timer; their return
// does not claim that any borrowed owner's unresolved attempt has retired.
// There is no goroutine, timer or I/O at construction and no application bind.
type ProjectRunner struct {
	coordinator *Coordinator
	visitor     *PendingVisitor
	options     ProjectRunnerOptions
	// Private read seam; construction always fixes the real bounded SQL reader.
	readPending       func(context.Context, postgres.SQLExecutor, i.ProjectID) ([]traversalPending, error)
	readLaunched      func(context.Context, postgres.SQLExecutor, i.ProjectID, string, string, int) ([]*dispatchRecord, error)
	executions        ec.AssociatedExecutor
	executionPageSize int
	executionAfter    *DispatchID
	executionThrough  *DispatchID
	mu                sync.Mutex
	stopped           bool
	call              *projectRunCall
	drained           chan struct{}
}
type projectRunCall struct {
	owner  *ProjectRunner
	cancel context.CancelCauseFunc
}

func NewProjectRunner(coordinator *Coordinator, visitor *PendingVisitor, options ProjectRunnerOptions) (*ProjectRunner, error) {
	if options.ProjectID.Validate() != nil || options.TickInterval <= 0 || options.LaunchPolicy.Validate() != nil {
		return nil, invalid()
	}
	if coordinator == nil || coordinator.authority == nil || visitor == nil || visitor.authority != coordinator.authority || nilPort(options.Tasks) || nilPort(coordinator.deps.Projects) {
		return nil, fault(f.DependencyUnbound)
	}
	options.LaunchPolicy = options.LaunchPolicy.Clone()
	return &ProjectRunner{coordinator: coordinator, visitor: visitor, options: options, readPending: loadTraversalPending, drained: make(chan struct{})}, nil
}
func (s *ProjectRunner) begin(ctx context.Context) (context.Context, *projectRunCall, error) {
	if ctx == nil {
		return nil, nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if s == nil || s.coordinator == nil {
		return nil, nil, fault(f.DependencyUnbound)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return nil, nil, fault(f.ShuttingDown)
	}
	a := s.coordinator.authority
	a.runnerMu.Lock()
	defer a.runnerMu.Unlock()
	if a.runners[s.options.ProjectID] != nil {
		return nil, nil, fault(f.ResourceBusy)
	}
	owned, cancel := context.WithCancelCause(ctx)
	call := &projectRunCall{owner: s, cancel: cancel}
	if a.runners == nil {
		a.runners = make(map[i.ProjectID]*projectRunCall)
	}
	a.runners[s.options.ProjectID] = call
	s.call = call
	return owned, call, nil
}
func (s *ProjectRunner) finish(call *projectRunCall) {
	s.mu.Lock()
	a := s.coordinator.authority
	a.runnerMu.Lock()
	if a.runners[s.options.ProjectID] == call {
		delete(a.runners, s.options.ProjectID)
	}
	a.runnerMu.Unlock()
	s.call = nil
	if s.stopped {
		close(s.drained)
	}
	s.mu.Unlock()
	call.cancel(nil)
}
func (s *ProjectRunner) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	call := s.call
	if call == nil {
		close(s.drained)
	}
	s.mu.Unlock()
	if call != nil {
		call.cancel(fault(f.ShuttingDown))
	}
}
func (s *ProjectRunner) Drain(ctx context.Context) error {
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
func (s *ProjectRunner) Joined() bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped && s.call == nil
}

// Run keeps the Project registration for its entire lifetime, rebuilding the
// bounded snapshot every round. Every visit, including the final one, already
// waits one tick, so the next round adds no second delay. A scan failure stops
// this call; definite per-Task domain refusals are observations and do not
// starve later entries. An uncertain mutation stops with its original handle. Context cancellation stops the original in-flight call and timer.
func (s *ProjectRunner) Run(ctx context.Context) error {
	ctx, call, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer s.finish(call)
	for {
		result, e := s.traverse(ctx)
		if e != nil {
			return &ProjectRunError{result: cloneProjectRunResult(result), cause: e}
		}
	}
}

// RunTraversal executes one finite round using the same admission and pacing
// as Run. It cannot overlap another Run/RunTraversal for the same Project and
// PendingAuthority instance, even through a different runner instance. This
// is in-process serialization, not a cross-process Scheduler leader lease.
func (s *ProjectRunner) RunTraversal(ctx context.Context) (ProjectRunResult, error) {
	ctx, call, err := s.begin(ctx)
	if err != nil {
		return ProjectRunResult{}, err
	}
	defer s.finish(call)
	return s.traverse(ctx)
}
func waitProjectTick(ctx context.Context, tick time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	timer := time.NewTimer(tick)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ctx.Err()
	}
}
func (s *ProjectRunner) traverse(ctx context.Context) (ProjectRunResult, error) {
	out := ProjectRunResult{ProjectID: s.options.ProjectID}
	seen := make(map[i.ExecutionID]DispatchID)
	if err := s.visitExecutionPage(ctx, &out, seen); err != nil {
		return out, err
	}
	plan, err := s.captureTraversal(ctx)
	if err != nil {
		return out, err
	}
	out.CurrentSprintID = cloneSprintID(plan.sprint)
	out.Visits = make([]ProjectTaskVisit, 0, len(plan.entries))
	paused := !plan.enabled
	for _, entry := range plan.entries {
		if err = ctx.Err(); err != nil {
			return out, err
		}
		visit, endRound, readErr := s.visitTask(ctx, entry, plan.sprint, paused)
		out.Visits = append(out.Visits, visit)
		if readErr == nil && visit.Err == nil && s.executions != nil && visit.Dispatch.Summary().Status == Launched {
			readErr = s.advanceExecution(ctx, visit.Dispatch, &out, seen)
		}
		if err = waitProjectTick(ctx, s.options.TickInterval); err != nil {
			if uncertainProjectVisit(visit.Err) || uncertainProjectVisit(readErr) {
				return out, errors.Join(visit.Err, readErr, err)
			}
			return out, err
		}
		if readErr != nil {
			return out, readErr
		}
		if uncertainProjectVisit(visit.Err) {
			return out, visit.Err
		}
		if endRound {
			return out, nil
		}
	}
	if len(plan.entries) == 0 && len(out.Executions) == 0 {
		err = waitProjectTick(ctx, s.options.TickInterval)
	}
	return out, err
}
func (s *ProjectRunner) visitTask(ctx context.Context, entry projectTaskEntry, sprint *wc.SprintID, paused bool) (ProjectTaskVisit, bool, error) {
	out := ProjectTaskVisit{TaskID: entry.task, ExpectedState: entry.state, Action: ProjectVisitDeferred}
	current, pending, facts, err := s.readTraversalTask(ctx, entry.task, entry.state != "" && !paused, sprint)
	if err != nil {
		out.Err = err
		return out, paused, err
	}
	if paused {
		return out, false, nil
	}
	if !current.Config.Enabled || !sameTraversalSprint(sprint, current.Project.CurrentSprintID) {
		return out, true, nil
	}
	// An original unknown claim must first be observed by its original owner.
	// Even a missing Dispatch is not permission to mint another identity.
	if original, ok := s.unknownClaim(entry.task); ok {
		out.Action = ProjectVisitClaim
		original = original.Clone()
		out.ClaimRequest = &original
		id, _ := f.ParseID[DispatchIdentity](original.DispatchID)
		out.DispatchID = &id
		out.Dispatch, out.Err = s.coordinator.ResolveClaim(ctx, original)
		if out.Err != nil {
			return out, false, nil
		}
		id = out.Dispatch.Summary().ID
		result, e := s.visitor.Visit(ctx, s.options.ProjectID, id)
		if result.Found {
			out.Dispatch = result.Dispatch
		}
		out.Err = e
		return out, false, nil
	}
	// Current pending is checked before any current Work state/Sprint filter.
	// Thus a new pending appearing after the snapshot also takes this path.
	if pending != nil {
		out.Action = ProjectVisitPending
		id := pending.id
		out.DispatchID = &id
		result, e := s.visitor.Visit(ctx, s.options.ProjectID, pending.id)
		if result.Found {
			out.Dispatch = result.Dispatch
		}
		out.Err = e
		return out, false, nil
	}
	if entry.state == "" || sprint == nil || current.Project.CurrentSprintID == nil || *current.Project.CurrentSprintID != *sprint || facts == nil || facts.SprintID != *sprint || facts.State != entry.state {
		return out, false, nil
	}
	// Relaunch/cooldown and automatic blocked reconciliation have no bound
	// producer in this slice. They remain visited and paced as Deferred.
	if facts.State != wc.TaskStateTodo || facts.AssigneeAgentID == nil || facts.HasUnresolvedBlockers {
		return out, false, nil
	}
	dispatch, e := f.NewID[DispatchIdentity]()
	if e != nil {
		out.Err = portError(e)
		return out, false, nil
	}
	request, e := f.NewID[f.Request]()
	if e != nil {
		out.Err = portError(e)
		return out, false, nil
	}
	claim := wc.TaskClaimRequest{ProjectID: s.options.ProjectID, TaskID: entry.task, AgentID: *facts.AssigneeAgentID, DispatchID: dispatch.String(), ExpectedTaskVersion: facts.Version, CurrentSprintID: *sprint, Purpose: "task/work", RequestID: request}
	out.Action = ProjectVisitClaim
	out.ClaimRequest = &claim
	out.DispatchID = &dispatch
	out.Dispatch, out.Err = s.coordinator.ClaimTask(ctx, claim, s.options.LaunchPolicy)
	if out.Err != nil {
		return out, false, nil
	}
	result, e := s.visitor.Visit(ctx, s.options.ProjectID, dispatch)
	if result.Found {
		out.Dispatch = result.Dispatch
	}
	out.Err = e
	return out, false, nil
}
func (s *ProjectRunner) unknownClaim(task wc.TaskID) (wc.TaskClaimRequest, bool) {
	s.coordinator.mu.Lock()
	defer s.coordinator.mu.Unlock()
	for _, call := range s.coordinator.unknown {
		if call.request.ProjectID == s.options.ProjectID && call.request.TaskID == task {
			return call.request.Clone(), true
		}
	}
	return wc.TaskClaimRequest{}, false
}
func cloneSprintID(v *wc.SprintID) *wc.SprintID {
	if v == nil {
		return nil
	}
	copy := *v
	return &copy
}
func isMissingTraversalTask(err error) bool {
	var known *f.Fault
	return errors.As(err, &known) && known.Code == f.TaskNotFound
}
