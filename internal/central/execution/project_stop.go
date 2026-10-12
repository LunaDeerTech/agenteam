package execution

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// ProjectStopper controls only this executor's original jobs. It does not stop
// the borrowed Preparation/DirectText drivers or claim all Project executions.
type ProjectStopper struct {
	executor  *AssociatedExecutor
	authority *project.ExecutionStopAuthority
}

func NewProjectStopper(executor *AssociatedExecutor, driver *project.LifecycleStopDriver) (*ProjectStopper, error) {
	if executor == nil || executor.state == nil || executor.state.preparation == nil || executor.state.preparation.state == nil || executor.state.direct == nil || executor.state.direct.state == nil {
		return nil, fault(f.DependencyUnbound)
	}
	s := executor.state
	// Admission after stop must use the real Project gate, not an arbitrary
	// provider which could accept a deleting Project from a public DTO.
	projects, ok := s.preparation.state.projects.(*project.Authority)
	if !ok || projects == nil || s.preparation.state.store != s.direct.state.store || s.ops.admit == nil {
		return nil, fault(f.DependencyUnbound)
	}
	authority, err := project.NewExecutionStopAuthority(driver, s.preparation.state.store)
	if err != nil {
		return nil, err
	}
	return &ProjectStopper{executor: executor, authority: authority}, nil
}

// LocalJoined is an observation of this executor, not a durable Execution
// terminal, foreign-process result, reference-retirement grant or participant.
type ProjectStopDetails struct {
	ProjectID         i.ProjectID
	OperationID       pc.OperationID
	Action            pc.LifecycleAction
	ProjectVersion    f.Version
	PendingJobs       int
	PendingAdmissions int
	Retained          bool
	LocalJoined       bool
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
	_, _ = io.WriteString(w, "execution_project_stop_report")
}
func (ProjectStopReport) LogValue() slog.Value {
	return slog.StringValue("execution_project_stop_report")
}
func (ProjectStopReport) MarshalJSON() ([]byte, error) {
	return []byte(`"execution_project_stop_report"`), nil
}
func (*ProjectStopReport) UnmarshalJSON([]byte) error { return invalid() }

func (s *ProjectStopper) RequestStop(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) (ProjectStopReport, error) {
	return s.control(ctx, actor, cause, scope, false)
}
func (s *ProjectStopper) DrainProject(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) (ProjectStopReport, error) {
	return s.control(ctx, actor, cause, scope, true)
}
func (s *ProjectStopper) control(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef, wait bool) (ProjectStopReport, error) {
	if s == nil || s.executor == nil || s.executor.state == nil || s.authority == nil {
		return ProjectStopReport{}, fault(f.DependencyUnbound)
	}
	run, done, err := s.executor.state.beginProjectStop(ctx)
	if err != nil {
		return ProjectStopReport{}, err
	}
	defer done()
	var report ProjectStopReport
	err = s.authority.WithConfirmedExecutionStop(run, actor, cause, scope, func(ctx context.Context) error {
		var err error
		report, err = s.executor.state.stopProject(ctx, cause, scope, wait)
		return err
	})
	if err != nil {
		return ProjectStopReport{}, err
	}
	return report, nil
}

type projectStopControl struct{ cancel context.CancelFunc }
type associatedAdmission struct {
	project  i.ProjectID
	dispatch c.AssociatedDispatch
	cancel   context.CancelFunc
	stopped  bool
}

func (s *associatedExecutorState) beginProjectStop(ctx context.Context) (context.Context, func(), error) {
	if ctx == nil {
		return nil, nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started {
		return nil, nil, fault(f.InvalidState)
	}
	if s.stopped || s.root.Err() != nil {
		return nil, nil, fault(f.ShuttingDown)
	}
	if len(s.stopControls) >= s.options.MaxOwned {
		return nil, nil, fault(f.ResourceBusy)
	}
	run, cancel := context.WithCancel(ctx)
	control := &projectStopControl{cancel: cancel}
	if s.stopControls == nil {
		s.stopControls = make(map[*projectStopControl]struct{})
	}
	s.stopControls[control] = struct{}{}
	s.notifyLocked()
	return run, func() {
		cancel()
		s.mu.Lock()
		delete(s.stopControls, control)
		s.notifyLocked()
		s.mu.Unlock()
	}, nil
}

// Called and returned with s.mu held. The admission is counted BEFORE its
// original authorization read, so stop cannot miss a delayed Store return.
// A constant flag enables this gate after the first confirmed Project stop;
// no unbounded permanent Project tombstone map is needed. Canonical deleting
// (or absent) Projects reject subsequent admissions, while active B can enter.
func (s *associatedExecutorState) admitAssociatedLocked(ctx context.Context, project i.ProjectID, dispatch c.AssociatedDispatch) (c.ExecutionAdvance, error) {
	if s.ops.admit == nil {
		return c.ExecutionAdvance{}, fault(f.DependencyUnbound)
	}
	run, cancel := context.WithCancel(ctx)
	rootReturned := make(chan struct{})
	stopRoot := context.AfterFunc(s.root, func() { cancel(); close(rootReturned) })
	admission := &associatedAdmission{project: project, dispatch: dispatch, cancel: cancel}
	if s.admissions == nil {
		s.admissions = make(map[i.ExecutionID]*associatedAdmission)
	}
	s.admissions[dispatch.ExecutionID] = admission
	s.notifyLocked()
	s.mu.Unlock()
	err := func() (err error) {
		defer func() {
			if recover() != nil {
				err = unavailable(nil)
			}
		}()
		return s.ops.admit(run, project, dispatch)
	}()
	if !stopRoot() {
		<-rootReturned
	}
	s.mu.Lock()
	delete(s.admissions, dispatch.ExecutionID)
	s.notifyLocked()
	if err == nil {
		err = run.Err()
	}
	cancel()
	if err != nil {
		return c.ExecutionAdvance{}, err
	}
	if s.stopped || s.root.Err() != nil {
		return c.ExecutionAdvance{}, fault(f.ShuttingDown)
	}
	if admission.stopped {
		return c.ExecutionAdvance{}, fault(f.InvalidState)
	}
	return s.startAssociatedLocked(project, dispatch), nil
}

func (s *associatedExecutorState) projectReportLocked(cause pc.LifecycleCause, scope pc.ScopeRef) ProjectStopReport {
	d := ProjectStopDetails{ProjectID: scope.ProjectID, OperationID: cause.OperationID, Action: cause.Action, ProjectVersion: cause.ProjectVersion}
	for _, admission := range s.admissions {
		if admission.project == scope.ProjectID {
			d.PendingAdmissions++
		}
	}
	for id, job := range s.jobs {
		if job.project != scope.ProjectID {
			continue
		}
		if job.active || job.retained {
			d.PendingJobs++
			d.Retained = d.Retained || job.retained
		} else if job.projectStopped {
			// The canonical Delete gate prevents another preparation, and this
			// exact original call has returned without a retained driver owner.
			delete(s.jobs, id)
			job.cancel()
		}
	}
	d.LocalJoined = d.PendingJobs == 0 && d.PendingAdmissions == 0 && !d.Retained
	return ProjectStopReport{data: func() ProjectStopDetails { return d }}
}

func (s *associatedExecutorState) stopProject(ctx context.Context, cause pc.LifecycleCause, scope pc.ScopeRef, wait bool) (ProjectStopReport, error) {
	s.mu.Lock()
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return ProjectStopReport{}, err
	}
	if s.stopped {
		s.mu.Unlock()
		return ProjectStopReport{}, fault(f.ShuttingDown)
	}
	s.requiresAdmission = true
	for _, admission := range s.admissions {
		if admission.project == scope.ProjectID {
			admission.stopped = true
			admission.cancel()
		}
	}
	for _, job := range s.jobs {
		if job.project == scope.ProjectID {
			job.projectStopped = true
			job.cancel()
		}
	}
	s.notifyLocked()
	s.mu.Unlock()
	// Each explicit Drain owns at most one existing recovery per retained job;
	// it cannot wait for, or refresh, a recovery interval in the background.
	recovered := make(map[*associatedCall]bool)
	for {
		s.mu.Lock()
		if err := ctx.Err(); err != nil {
			s.mu.Unlock()
			return ProjectStopReport{}, err
		}
		report := s.projectReportLocked(cause, scope)
		if report.Details().LocalJoined || !wait {
			s.mu.Unlock()
			return report, nil
		}
		active := report.Details().PendingAdmissions != 0
		var retained error
		var candidate *associatedCall
		for _, job := range s.jobs {
			if job.project != scope.ProjectID {
				continue
			}
			if job.active {
				active = true
				continue
			}
			if !job.retained {
				continue
			}
			if retained == nil {
				retained = job.err
			}
			// Observing start-commit Unknown can enter the first Model call.
			// Keep it pending after stop rather than invoking that recovery.
			if candidate == nil && !recovered[job] && !time.Now().Before(job.next) && !(job.owners.direct && job.owners.uncertainty == "start") {
				candidate = job
			}
		}
		if candidate != nil && !active {
			recovered[candidate] = true
			candidate.active = true
			candidate.next = time.Now().Add(s.options.RecoveryInterval)
			s.workers++
			s.mu.Unlock()
			s.execute(candidate, true, context.WithValue(ctx, associatedContextKey{}, candidate))
			continue
		}
		if !active {
			s.mu.Unlock()
			if retained == nil {
				retained = fault(f.ResourceBusy)
			}
			return ProjectStopReport{}, retained
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
