package scheduler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"sync"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// ProjectExecutions is borrowed. Ready only confirms that its original Run
// lifetime is installed; this manager never starts or stops the executor.
type ProjectExecutions interface {
	ec.AssociatedExecutor
	Ready() <-chan struct{}
}

type ProjectRunnersDependencies struct {
	Projects    pc.SchedulerProjects
	Coordinator *Coordinator
	Visitor     *PendingVisitor
	Tasks       wc.SchedulerTaskReader
	Relaunch    *RelaunchCoordinator
	Executions  ProjectExecutions
}

type ProjectRunnersOptions struct {
	MaxProjects, ProjectPageSize, ExecutionPageSize int
	DiscoveryInterval, TickInterval                 time.Duration
	RelaunchSkipCount                               int64
	LaunchPolicy                                    ec.Policy
}

type ManagedProject struct {
	ProjectID i.ProjectID
	Running   bool
	Retained  bool
	Stopping  bool
	Err       error `json:"-"`
}

// Deferred counts identities excluded by capacity in the latest discovery
// cycle (or its completed prefix). It is not a persistent scheduling receipt.
// Errors retain the original safe carriers and never grant recovery authority.
type ProjectRunnersObservation struct {
	Started, Stopped bool
	Projects         []ManagedProject
	Deferred         uint64
	DiscoveryError   error `json:"-"`
}

func (ProjectRunnersObservation) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "scheduler_project_runners")
}
func (ProjectRunnersObservation) LogValue() slog.Value {
	return slog.StringValue("scheduler_project_runners")
}
func (ProjectRunnersObservation) MarshalJSON() ([]byte, error) {
	return []byte(`"scheduler_project_runners"`), nil
}
func (ManagedProject) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "scheduler_managed_project")
}
func (ManagedProject) LogValue() slog.Value { return slog.StringValue("scheduler_managed_project") }
func (ManagedProject) MarshalJSON() ([]byte, error) {
	return []byte(`"scheduler_managed_project"`), nil
}

// ProjectRunners owns at most MaxProjects concrete ProjectRunners, including
// stopped runners whose original Unknown has not retired. Discovery is a
// bounded, paced keyset cycle, not a grant or a cross-process leader election.
// Construction has no I/O. All borrowed domain owners keep their own lifetime.
type ProjectRunners struct {
	deps                      ProjectRunnersDependencies
	options                   ProjectRunnersOptions
	ops                       managedRunnerOps
	mu                        sync.Mutex
	started, stopped, running bool
	cancel                    context.CancelFunc
	changed                   chan struct{}
	projects                  map[i.ProjectID]*managedRunner
	position                  pc.SchedulerProjectPageRequest
	cycleOpen                 bool
	deferred                  uint64
	discoveryError            error
}

type managedRunner struct {
	runner                 *ProjectRunner
	seen, active, stopping bool
	err                    error
	next                   time.Time
}

// The constructor fixes these functions to the original concrete runners.
// Tests may control call returns without fabricating domain or SQL success.
type managedRunnerOps struct {
	create  func(i.ProjectID) (*ProjectRunner, error)
	run     func(context.Context, *ProjectRunner) error
	recover func(context.Context, *ProjectRunner, error) error
	stop    func(*ProjectRunner)
	joined  func(*ProjectRunner) bool
}

func NewProjectRunners(deps ProjectRunnersDependencies, options ProjectRunnersOptions) (*ProjectRunners, error) {
	if options.MaxProjects < 1 || options.ProjectPageSize < 1 || options.ProjectPageSize > pc.MaxSchedulerProjectPageSize || options.ExecutionPageSize < 1 || options.ExecutionPageSize > MaxExecutionHandoffPage || options.DiscoveryInterval <= 0 || options.TickInterval <= 0 || options.RelaunchSkipCount < 0 || options.LaunchPolicy.Validate() != nil {
		return nil, invalid()
	}
	if nilPort(deps.Projects) || deps.Coordinator == nil || deps.Coordinator.authority == nil || deps.Visitor == nil || deps.Visitor.authority != deps.Coordinator.authority || nilPort(deps.Coordinator.deps.Projects) || nilPort(deps.Tasks) || deps.Relaunch == nil || deps.Relaunch.coordinator != deps.Coordinator || deps.Relaunch.skipCount != options.RelaunchSkipCount || nilPort(deps.Executions) || deps.Executions.Ready() == nil {
		return nil, fault(f.DependencyUnbound)
	}
	options.LaunchPolicy = options.LaunchPolicy.Clone()
	s := &ProjectRunners{deps: deps, options: options, projects: make(map[i.ProjectID]*managedRunner), changed: make(chan struct{}), position: pc.SchedulerProjectPageRequest{Limit: options.ProjectPageSize}}
	s.ops = managedRunnerOps{
		create: func(p i.ProjectID) (*ProjectRunner, error) {
			return NewProjectRunnerWithExecutions(deps.Coordinator, deps.Visitor, ProjectRunnerOptions{ProjectID: p, TickInterval: options.TickInterval, LaunchPolicy: options.LaunchPolicy, Tasks: deps.Tasks, Relaunch: deps.Relaunch}, deps.Executions, options.ExecutionPageSize)
		},
		stop:   (*ProjectRunner).Stop,
		joined: (*ProjectRunner).Joined,
	}
	s.ops.run = func(ctx context.Context, r *ProjectRunner) error { return r.Run(ctx) }
	s.ops.recover = s.recoverOriginal
	return s, nil
}

func (s *ProjectRunners) notifyLocked() { close(s.changed); s.changed = make(chan struct{}) }

func (s *ProjectRunners) Observation() ProjectRunnersObservation {
	if s == nil {
		return ProjectRunnersObservation{Projects: []ManagedProject{}}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := ProjectRunnersObservation{Started: s.started, Stopped: s.stopped, Deferred: s.deferred, DiscoveryError: s.discoveryError, Projects: make([]ManagedProject, 0, len(s.projects))}
	for p, v := range s.projects {
		out.Projects = append(out.Projects, ManagedProject{ProjectID: p, Running: v.active, Retained: uncertainProjectVisit(v.err), Stopping: v.stopping, Err: v.err})
	}
	sort.Slice(out.Projects, func(a, b int) bool { return out.Projects[a].ProjectID.String() < out.Projects[b].ProjectID.String() })
	return out
}

// Run performs one lifetime. Each discovery page, including an empty cycle or
// failed read, consumes a complete interval. Runner failures are paced locally;
// a bad Project cannot cancel another Project or the shared executor.
func (s *ProjectRunners) Run(ctx context.Context) error {
	if ctx == nil {
		return invalid()
	}
	if s == nil {
		return fault(f.DependencyUnbound)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	if s.started || s.stopped {
		s.mu.Unlock()
		return fault(f.InvalidState)
	}
	owned, cancel := context.WithCancel(ctx)
	s.started, s.running, s.cancel = true, true, cancel
	s.notifyLocked()
	s.mu.Unlock()
	defer func() { cancel(); s.mu.Lock(); s.running = false; s.notifyLocked(); s.mu.Unlock() }()
	select {
	case <-owned.Done():
	case <-s.deps.Executions.Ready():
		for owned.Err() == nil {
			s.discover(owned)
			s.startDue(owned)
			if waitProjectTick(owned, s.options.DiscoveryInterval) != nil {
				break
			}
		}
	}
	s.Stop()
	// A cancelled Run is still the physical owner of its runner calls. The
	// caller's separate Drain deadline cannot make those goroutines disappear.
	s.waitReturned(context.Background(), false)
	s.mu.Lock()
	err := s.retainedLocked()
	s.mu.Unlock()
	return errors.Join(ctx.Err(), err)
}

func (s *ProjectRunners) discover(ctx context.Context) {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	request := s.position.Clone()
	if !s.cycleOpen {
		s.cycleOpen, s.deferred = true, 0
		for _, v := range s.projects {
			v.seen = false
		}
	}
	s.mu.Unlock()
	page, err := s.deps.Projects.ListSchedulerProjects(ctx, request)
	if err == nil {
		err = page.ValidateFor(request)
	}
	if err == nil {
		err = ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	s.discoveryError = err
	if err != nil {
		s.notifyLocked()
		return
	}
	var deferred uint64
	for _, p := range page.ProjectIDs {
		if v := s.projects[p]; v != nil {
			v.seen = true
			continue
		}
		if len(s.projects) == s.options.MaxProjects {
			deferred++
			continue
		}
		runner, e := s.ops.create(p)
		if e != nil {
			s.discoveryError = e
			s.notifyLocked()
			return
		}
		s.projects[p] = &managedRunner{runner: runner, seen: true}
	}
	if ^uint64(0)-s.deferred < deferred {
		s.deferred = ^uint64(0)
	} else {
		s.deferred += deferred
	}
	if page.Complete {
		for p, v := range s.projects {
			if !v.seen && !v.stopping {
				v.stopping = true
				s.ops.stop(v.runner)
			}
			if v.stopping && !v.active && !uncertainProjectVisit(v.err) && s.ops.joined(v.runner) {
				delete(s.projects, p)
			}
		}
		s.position = pc.SchedulerProjectPageRequest{Limit: s.options.ProjectPageSize}
		s.cycleOpen = false
	} else {
		last := page.ProjectIDs[len(page.ProjectIDs)-1]
		s.position = pc.SchedulerProjectPageRequest{After: &last, Through: page.Through, Limit: s.options.ProjectPageSize}.Clone()
	}
	s.notifyLocked()
}

func (s *ProjectRunners) startDue(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || ctx.Err() != nil {
		return
	}
	now := time.Now()
	for _, v := range s.projects {
		if v.active || v.stopping || now.Before(v.next) {
			continue
		}
		v.active = true
		prior := v.err
		go s.runOne(ctx, v, prior)
	}
	s.notifyLocked()
}

func (s *ProjectRunners) runOne(ctx context.Context, v *managedRunner, prior error) {
	var err error
	defer func() {
		if recover() != nil {
			err = unavailable(nil)
		}
		s.mu.Lock()
		if uncertainProjectVisit(prior) && err != nil {
			var original *ProjectRunError
			if errors.As(prior, &original) {
				if errors.Is(err, original) {
					err = original
				} else {
					err = errors.Join(original, err)
				}
			} else {
				err = errors.Join(prior, err)
			}
		}
		v.active, v.err, v.next = false, err, time.Now().Add(s.options.DiscoveryInterval)
		s.notifyLocked()
		s.mu.Unlock()
	}()
	if uncertainProjectVisit(prior) {
		err = s.ops.recover(ctx, v.runner, prior)
		if err != nil {
			return
		}
		prior = nil
		s.mu.Lock()
		v.err = nil
		s.notifyLocked()
		s.mu.Unlock()
	}
	err = s.ops.run(ctx, v.runner)
}

// Recovery consumes only identities in the original returned observation.
// It precedes a new traversal, even if its Task left the current snapshot.
// The same providers retain their private calls; no replacement owner/key is
// created, and a paused pending lookup remains deferred with its original error.
func (s *ProjectRunners) recoverOriginal(ctx context.Context, r *ProjectRunner, prior error) error {
	var original *ProjectRunError
	if !errors.As(prior, &original) {
		return prior
	}
	observation := original.Observation()
	for _, visit := range observation.Visits {
		if !uncertainProjectVisit(visit.Err) {
			continue
		}
		if visit.ClaimRequest != nil {
			if request, ok := r.unknownClaim(visit.TaskID); ok {
				if request != *visit.ClaimRequest {
					return fault(f.ConfirmationStale)
				}
				if _, err := r.coordinator.ResolveClaim(ctx, request); err != nil {
					return err
				}
			}
		}
		if visit.RelaunchRequest != nil {
			if request, ok := r.unknownRelaunch(visit.TaskID); ok {
				if request != *visit.RelaunchRequest {
					return fault(f.ConfirmationStale)
				}
				if _, err := r.options.Relaunch.ResolveRelaunch(ctx, request); err != nil {
					return err
				}
			}
		}
		if visit.DispatchID != nil {
			if err := s.recoverDispatch(ctx, r, *visit.DispatchID, prior); err != nil {
				return err
			}
		}
	}
	for _, visit := range observation.Executions {
		if !uncertainProjectVisit(visit.Err) {
			continue
		}
		dispatch, err := r.visitor.handoff.Lookup(ctx, observation.ProjectID, visit.DispatchID)
		if err != nil {
			return err
		}
		if dispatch.Summary().Status != Launched || dispatch.Summary().ExecutionID == nil || *dispatch.Summary().ExecutionID != visit.ExecutionID {
			return fault(f.ConfirmationStale)
		}
		out := ProjectRunResult{ProjectID: observation.ProjectID}
		if err := r.advanceExecution(ctx, dispatch, &out, make(map[i.ExecutionID]DispatchID)); err != nil {
			return err
		}
		found := false
		for _, actual := range out.Executions {
			if actual.DispatchID == visit.DispatchID && actual.ExecutionID == visit.ExecutionID && actual.Err == nil && !actual.Advance.Retained {
				found = true
			}
		}
		if !found {
			return prior
		}
	}
	return ctx.Err()
}

func (s *ProjectRunners) recoverDispatch(ctx context.Context, r *ProjectRunner, id DispatchID, prior error) error {
	v := r.visitor
	handoff, busy, failure := false, false, false
	v.handoff.mu.Lock()
	if call := v.handoff.calls[id]; call != nil && call.project == r.options.ProjectID {
		handoff = call.unknown != nil
	}
	v.handoff.mu.Unlock()
	v.busy.mu.Lock()
	if call := v.busy.calls[id]; call != nil && call.project == r.options.ProjectID {
		busy = call.unknown != nil
	}
	v.busy.mu.Unlock()
	if v.failure != nil {
		v.failure.mu.Lock()
		if call := v.failure.calls[id]; call != nil && call.project == r.options.ProjectID {
			failure = call.unknown != nil
		}
		v.failure.mu.Unlock()
	}
	if !handoff && !busy && !failure {
		return ctx.Err()
	}
	ctx, call, err := v.begin(ctx, r.options.ProjectID)
	if err != nil {
		return err
	}
	defer v.finish(call)
	_, project, err := v.readIdentity(ctx, r.options.ProjectID, id)
	if err != nil {
		return err
	}
	if !project.Config.Enabled {
		return prior
	}
	v.mu.Lock()
	call.id = id
	v.mu.Unlock()
	if handoff {
		if _, err = v.handoff.Lookup(ctx, r.options.ProjectID, id); err != nil {
			return err
		}
	}
	if busy {
		if _, err = v.busy.Lookup(ctx, r.options.ProjectID, id); err != nil {
			return err
		}
	}
	if failure {
		if _, err = v.failure.Lookup(ctx, r.options.ProjectID, id); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (s *ProjectRunners) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	s.stopped = true
	if s.cancel != nil {
		s.cancel()
	}
	for _, v := range s.projects {
		v.stopping = true
		s.ops.stop(v.runner)
	}
	s.notifyLocked()
}

func (s *ProjectRunners) retainedLocked() error {
	for _, v := range s.projects {
		if uncertainProjectVisit(v.err) {
			return v.err
		}
	}
	return nil
}

func (s *ProjectRunners) waitReturned(ctx context.Context, includeRun bool) error {
	for {
		s.mu.Lock()
		active := includeRun && s.running
		for _, v := range s.projects {
			active = active || v.active
		}
		changed := s.changed
		s.mu.Unlock()
		if !active {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

// Drain waits only this manager's actual runners. A retained Unknown remains
// observable and occupies its slot; it is not discarded to claim a clean join.
// No cleanup call stops any borrowed Coordinator, Visitor, or executor.
func (s *ProjectRunners) Drain(ctx context.Context) error {
	if ctx == nil {
		return invalid()
	}
	if s == nil {
		return fault(f.DependencyUnbound)
	}
	s.Stop()
	if err := s.waitReturned(ctx, true); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.retainedLocked(); err != nil {
		return err
	}
	return ctx.Err()
}

func (s *ProjectRunners) Joined() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.stopped || s.running || s.retainedLocked() != nil {
		return false
	}
	for _, v := range s.projects {
		if v.active || !s.ops.joined(v.runner) {
			return false
		}
	}
	return true
}
