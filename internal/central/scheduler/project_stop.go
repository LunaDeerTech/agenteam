package scheduler

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type ProjectStopper struct {
	manager   *ProjectRunners
	authority *project.SchedulerStopAuthority
}

func NewProjectStopper(manager *ProjectRunners, driver *project.LifecycleStopDriver) (*ProjectStopper, error) {
	if manager == nil || manager.deps.Coordinator == nil || manager.deps.Coordinator.authority == nil || manager.changed == nil {
		return nil, fault(f.DependencyUnbound)
	}
	directory, ok := manager.deps.Projects.(*project.SchedulerProjects)
	if !ok || directory == nil {
		return nil, fault(f.DependencyUnbound)
	}
	authority, err := project.NewSchedulerStopAuthority(driver, directory, manager.deps.Coordinator.authority.store)
	if err != nil {
		return nil, err
	}
	return &ProjectStopper{manager: manager, authority: authority}, nil
}

// This is a snapshot of one manager's local runners, never a participant,
// foreign-process, shared executor or reference-retirement result.
type ProjectStopDetails struct {
	ProjectID      i.ProjectID
	OperationID    pc.OperationID
	Action         pc.LifecycleAction
	ProjectVersion f.Version
	PendingRunners int
	Retained       bool
	LocalJoined    bool
}
type ProjectStopReport struct{ data func() ProjectStopDetails }

func (r ProjectStopReport) Valid() bool { return r.data != nil }
func (r ProjectStopReport) Details() ProjectStopDetails {
	if r.data == nil {
		return ProjectStopDetails{}
	}
	return r.data()
}
func (r ProjectStopReport) Matches(cause pc.LifecycleCause, scope pc.ScopeRef) bool {
	if !r.Valid() || cause.Validate() != nil || pc.RequireProjectScope(scope) != nil {
		return false
	}
	d := r.Details()
	return d.ProjectID == scope.ProjectID && d.OperationID == cause.OperationID && d.Action == cause.Action && d.ProjectVersion == cause.ProjectVersion
}
func (ProjectStopReport) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "scheduler_project_stop_report")
}
func (ProjectStopReport) LogValue() slog.Value {
	return slog.StringValue("scheduler_project_stop_report")
}
func (ProjectStopReport) MarshalJSON() ([]byte, error) {
	return []byte(`"scheduler_project_stop_report"`), nil
}
func (*ProjectStopReport) UnmarshalJSON([]byte) error { return invalid() }

func (s *ProjectStopper) RequestStop(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) (ProjectStopReport, error) {
	return s.control(ctx, actor, cause, scope, false)
}

// DrainProject waits only the selected Project's original runOne. It cannot
// wait on the manager-wide control set, which includes this very call.
func (s *ProjectStopper) DrainProject(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) (ProjectStopReport, error) {
	return s.control(ctx, actor, cause, scope, true)
}
func (s *ProjectStopper) control(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef, wait bool) (ProjectStopReport, error) {
	if s == nil || s.manager == nil || s.authority == nil {
		return ProjectStopReport{}, fault(f.DependencyUnbound)
	}
	run, done, err := s.manager.beginProjectStop(ctx)
	if err != nil {
		return ProjectStopReport{}, err
	}
	defer done()
	var report ProjectStopReport
	err = s.authority.WithConfirmedSchedulerStop(run, actor, cause, scope, func(ctx context.Context) error {
		var err error
		report, err = s.manager.stopProject(ctx, cause, scope, wait)
		return err
	})
	if err != nil {
		return ProjectStopReport{}, err
	}
	return report, nil
}

type projectStopControl struct{ cancel context.CancelFunc }

func (s *ProjectRunners) beginProjectStop(ctx context.Context) (context.Context, func(), error) {
	if ctx == nil {
		return nil, nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return nil, nil, fault(f.ShuttingDown)
	}
	if len(s.stopControls) >= s.options.MaxProjects {
		return nil, nil, fault(f.ResourceBusy)
	}
	run, cancel := context.WithCancel(ctx)
	call := &projectStopControl{cancel: cancel}
	if s.stopControls == nil {
		s.stopControls = make(map[*projectStopControl]struct{})
	}
	s.stopControls[call] = struct{}{}
	s.notifyLocked()
	return run, func() {
		cancel()
		s.mu.Lock()
		delete(s.stopControls, call)
		s.notifyLocked()
		s.mu.Unlock()
	}, nil
}

func (s *ProjectRunners) stopManagedLocked(v *managedRunner) {
	v.stopping = true
	if v.cancel != nil {
		v.cancel()
	}
	s.ops.stop(v.runner)
}

func (s *ProjectRunners) projectStopReportLocked(cause pc.LifecycleCause, scope pc.ScopeRef) ProjectStopReport {
	d := ProjectStopDetails{ProjectID: scope.ProjectID, OperationID: cause.OperationID, Action: cause.Action, ProjectVersion: cause.ProjectVersion, LocalJoined: true}
	if v := s.projects[scope.ProjectID]; v != nil {
		if v.active || !s.ops.joined(v.runner) {
			d.PendingRunners = 1
		}
		d.Retained = uncertainProjectVisit(v.err)
		d.LocalJoined = d.PendingRunners == 0 && !d.Retained
	}
	return ProjectStopReport{data: func() ProjectStopDetails { return d }}
}

// Only the private confirmed-authority callback reaches this mutation.
func (s *ProjectRunners) stopProject(ctx context.Context, cause pc.LifecycleCause, scope pc.ScopeRef, wait bool) (ProjectStopReport, error) {
	s.mu.Lock()
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return ProjectStopReport{}, err
	}
	if s.stopped {
		s.mu.Unlock()
		return ProjectStopReport{}, fault(f.ShuttingDown)
	}
	if s.generation == math.MaxUint64 {
		s.mu.Unlock()
		return ProjectStopReport{}, fault(f.ResourceBusy)
	}
	s.generation++
	s.position = pc.SchedulerProjectPageRequest{Limit: s.options.ProjectPageSize}
	s.cycleOpen, s.deferred = false, 0
	if v := s.projects[scope.ProjectID]; v != nil {
		s.stopManagedLocked(v)
	}
	s.notifyLocked()
	s.mu.Unlock()
	for {
		s.mu.Lock()
		if err := ctx.Err(); err != nil {
			s.mu.Unlock()
			return ProjectStopReport{}, err
		}
		report := s.projectStopReportLocked(cause, scope)
		v := s.projects[scope.ProjectID]
		if report.Details().LocalJoined {
			delete(s.projects, scope.ProjectID)
			s.notifyLocked()
			s.mu.Unlock()
			return report, nil
		}
		if !wait {
			s.mu.Unlock()
			return report, nil
		}
		if v != nil && !v.active && uncertainProjectVisit(v.err) {
			err := v.err
			s.mu.Unlock()
			return ProjectStopReport{}, err
		}
		changed := s.changed
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return ProjectStopReport{}, ctx.Err()
		case <-changed:
		}
	}
}
