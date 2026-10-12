package scheduler

import (
	"context"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// The fixed entry wrappers encompass the original owners' complete return,
// including their retained outcome handling and synchronous provider tails.
// A nil ownership binding preserves the original constructors' behavior.
func finishProjectCall(state *projectCallState, entry *projectCallEntry, err *error) {
	if failure := recover(); failure != nil {
		_ = state.finish(entry, pendingUnknown())
		state.returned(entry)
		panic(failure)
	}
	*err = state.finish(entry, *err)
	state.returned(entry)
}

func projectEntryIdentity(project i.ProjectID, method string, key f.IdempotencyKey) (f.CommandIdentity, error) {
	if project.Validate() != nil {
		return f.CommandIdentity{}, invalid()
	}
	return f.NewCommandIdentity("scheduler", []string{project.String()}, method, key)
}

func (s *Coordinator) ClaimTask(ctx context.Context, r wc.TaskClaimRequest, policy ec.Policy) (out Dispatch, err error) {
	if ctx == nil || r.Validate() != nil || policy.Validate() != nil {
		return Dispatch{}, invalid()
	}
	if s == nil {
		return Dispatch{}, fault(f.DependencyUnbound)
	}
	r = r.Clone()
	policy = policy.Clone()
	identity, err := projectEntryIdentity(r.ProjectID, "claim", f.IdempotencyKey("scheduler_claim:"+r.DispatchID))
	if err != nil {
		return Dispatch{}, err
	}
	ctx, entry, err := s.projectCalls.admit(ctx, r.ProjectID, projectLaneCoordinator, identity)
	var bodyErr error
	if entry != nil {
		defer func() {
			if bodyErr == nil && err != nil {
				out = Dispatch{}
			}
		}()
		defer finishProjectCall(s.projectCalls, entry, &err)
	}
	if err != nil {
		return Dispatch{}, err
	}
	out, err = s.claimTask(ctx, r, policy)
	bodyErr = err
	return
}

func (s *Coordinator) ResolveClaim(ctx context.Context, r wc.TaskClaimRequest) (out Dispatch, err error) {
	if ctx == nil || r.Validate() != nil {
		return Dispatch{}, invalid()
	}
	if s == nil {
		return Dispatch{}, fault(f.DependencyUnbound)
	}
	r = r.Clone()
	identity, err := projectEntryIdentity(r.ProjectID, "resolve_claim", f.IdempotencyKey("scheduler_claim:"+r.DispatchID))
	if err != nil {
		return Dispatch{}, err
	}
	ctx, entry, err := s.projectCalls.admit(ctx, r.ProjectID, projectLaneCoordinator, identity)
	var bodyErr error
	if entry != nil {
		defer func() {
			if bodyErr == nil && err != nil {
				out = Dispatch{}
			}
		}()
		defer finishProjectCall(s.projectCalls, entry, &err)
	}
	if err != nil {
		return Dispatch{}, err
	}
	out, err = s.resolveClaim(ctx, r)
	bodyErr = err
	return
}

func (s *PendingVisitor) VisitNext(ctx context.Context, p i.ProjectID, after *DispatchID) (out PendingVisitResult, err error) {
	if ctx == nil || p.Validate() != nil || after != nil && after.Validate() != nil {
		return PendingVisitResult{}, invalid()
	}
	if s == nil {
		return PendingVisitResult{}, fault(f.DependencyUnbound)
	}
	if after != nil {
		copied := *after
		after = &copied
	}
	identity, err := projectEntryIdentity(p, "visit_next", "project_visit")
	if err != nil {
		return PendingVisitResult{}, err
	}
	ctx, entry, err := s.projectCalls.admit(ctx, p, projectLaneVisitor, identity)
	var bodyErr error
	if entry != nil {
		defer func() {
			if bodyErr == nil && err != nil {
				out = PendingVisitResult{}
			}
		}()
		defer finishProjectCall(s.projectCalls, entry, &err)
	}
	if err != nil {
		return PendingVisitResult{}, err
	}
	out, err = s.visitNext(ctx, p, after)
	bodyErr = err
	return
}

func (s *PendingVisitor) Visit(ctx context.Context, p i.ProjectID, id DispatchID) (out PendingVisitResult, err error) {
	if ctx == nil || p.Validate() != nil || id.Validate() != nil {
		return PendingVisitResult{}, invalid()
	}
	if s == nil {
		return PendingVisitResult{}, fault(f.DependencyUnbound)
	}

	identity, err := projectEntryIdentity(p, "visit", launchKey(id))
	if err != nil {
		return PendingVisitResult{}, err
	}
	ctx, entry, err := s.projectCalls.admit(ctx, p, projectLaneVisitor, identity)
	var bodyErr error
	if entry != nil {
		defer func() {
			if bodyErr == nil && err != nil {
				out = PendingVisitResult{}
			}
		}()
		defer finishProjectCall(s.projectCalls, entry, &err)
	}
	if err != nil {
		return PendingVisitResult{}, err
	}
	out, err = s.visitIdentity(ctx, p, id)
	bodyErr = err
	return
}

func (s *LaunchHandoff) LaunchOnce(ctx context.Context, p i.ProjectID, id DispatchID) (out Dispatch, err error) {
	if ctx == nil || p.Validate() != nil || id.Validate() != nil {
		return Dispatch{}, invalid()
	}
	if s == nil {
		return Dispatch{}, fault(f.DependencyUnbound)
	}

	identity, err := projectEntryIdentity(p, "launch_once", launchKey(id))
	if err != nil {
		return Dispatch{}, err
	}
	ctx, entry, err := s.projectCalls.admit(ctx, p, projectLaneHandoff, identity)
	var bodyErr error
	if entry != nil {
		defer func() {
			if bodyErr == nil && err != nil {
				out = Dispatch{}
			}
		}()
		defer finishProjectCall(s.projectCalls, entry, &err)
	}
	if err != nil {
		return Dispatch{}, err
	}
	out, err = s.launchOnce(ctx, p, id)
	bodyErr = err
	return
}

func (s *LaunchHandoff) Lookup(ctx context.Context, p i.ProjectID, id DispatchID) (out Dispatch, err error) {
	if ctx == nil || p.Validate() != nil || id.Validate() != nil {
		return Dispatch{}, invalid()
	}
	if s == nil {
		return Dispatch{}, fault(f.DependencyUnbound)
	}

	identity, err := projectEntryIdentity(p, "lookup_launch", launchKey(id))
	if err != nil {
		return Dispatch{}, err
	}
	ctx, entry, err := s.projectCalls.admit(ctx, p, projectLaneHandoff, identity)
	var bodyErr error
	if entry != nil {
		defer func() {
			if bodyErr == nil && err != nil {
				out = Dispatch{}
			}
		}()
		defer finishProjectCall(s.projectCalls, entry, &err)
	}
	if err != nil {
		return Dispatch{}, err
	}
	out, err = s.lookup(ctx, p, id)
	bodyErr = err
	return
}

func (s *LaunchHandoff) RetryDue(ctx context.Context, p i.ProjectID, id DispatchID) (out Dispatch, err error) {
	if ctx == nil || p.Validate() != nil || id.Validate() != nil {
		return Dispatch{}, invalid()
	}
	if s == nil {
		return Dispatch{}, fault(f.DependencyUnbound)
	}

	identity, err := projectEntryIdentity(p, "retry_due", launchKey(id))
	if err != nil {
		return Dispatch{}, err
	}
	ctx, entry, err := s.projectCalls.admit(ctx, p, projectLaneHandoff, identity)
	var bodyErr error
	if entry != nil {
		defer func() {
			if bodyErr == nil && err != nil {
				out = Dispatch{}
			}
		}()
		defer finishProjectCall(s.projectCalls, entry, &err)
	}
	if err != nil {
		return Dispatch{}, err
	}
	out, err = s.retryDue(ctx, p, id)
	bodyErr = err
	return
}

func (s *BusyCompensator) CompensateAgentBusy(ctx context.Context, p i.ProjectID, id DispatchID) (out Dispatch, err error) {
	if ctx == nil || p.Validate() != nil || id.Validate() != nil {
		return Dispatch{}, invalid()
	}
	if s == nil {
		return Dispatch{}, fault(f.DependencyUnbound)
	}

	identity, err := projectEntryIdentity(p, "compensate_agent_busy", launchKey(id))
	if err != nil {
		return Dispatch{}, err
	}
	ctx, entry, err := s.projectCalls.admit(ctx, p, projectLaneBusy, identity)
	var bodyErr error
	if entry != nil {
		defer func() {
			if bodyErr == nil && err != nil {
				out = Dispatch{}
			}
		}()
		defer finishProjectCall(s.projectCalls, entry, &err)
	}
	if err != nil {
		return Dispatch{}, err
	}
	out, err = s.compensateAgentBusy(ctx, p, id)
	bodyErr = err
	return
}

func (s *BusyCompensator) Lookup(ctx context.Context, p i.ProjectID, id DispatchID) (out Dispatch, err error) {
	if ctx == nil || p.Validate() != nil || id.Validate() != nil {
		return Dispatch{}, invalid()
	}
	if s == nil {
		return Dispatch{}, fault(f.DependencyUnbound)
	}

	identity, err := projectEntryIdentity(p, "lookup_busy_compensation", launchKey(id))
	if err != nil {
		return Dispatch{}, err
	}
	ctx, entry, err := s.projectCalls.admit(ctx, p, projectLaneBusy, identity)
	var bodyErr error
	if entry != nil {
		defer func() {
			if bodyErr == nil && err != nil {
				out = Dispatch{}
			}
		}()
		defer finishProjectCall(s.projectCalls, entry, &err)
	}
	if err != nil {
		return Dispatch{}, err
	}
	out, err = s.lookup(ctx, p, id)
	bodyErr = err
	return
}

func (s *LaunchFailureFinalizer) FinalizeLaunchFailure(ctx context.Context, p i.ProjectID, id DispatchID) (out Dispatch, err error) {
	if ctx == nil || p.Validate() != nil || id.Validate() != nil {
		return Dispatch{}, invalid()
	}
	if s == nil {
		return Dispatch{}, fault(f.DependencyUnbound)
	}

	identity, err := projectEntryIdentity(p, "finalize_launch_failure", launchKey(id))
	if err != nil {
		return Dispatch{}, err
	}
	ctx, entry, err := s.projectCalls.admit(ctx, p, projectLaneFailure, identity)
	var bodyErr error
	if entry != nil {
		defer func() {
			if bodyErr == nil && err != nil {
				out = Dispatch{}
			}
		}()
		defer finishProjectCall(s.projectCalls, entry, &err)
	}
	if err != nil {
		return Dispatch{}, err
	}
	out, err = s.finalizeLaunchFailure(ctx, p, id)
	bodyErr = err
	return
}

func (s *LaunchFailureFinalizer) Lookup(ctx context.Context, p i.ProjectID, id DispatchID) (out Dispatch, err error) {
	if ctx == nil || p.Validate() != nil || id.Validate() != nil {
		return Dispatch{}, invalid()
	}
	if s == nil {
		return Dispatch{}, fault(f.DependencyUnbound)
	}

	identity, err := projectEntryIdentity(p, "lookup_launch_failure", launchKey(id))
	if err != nil {
		return Dispatch{}, err
	}
	ctx, entry, err := s.projectCalls.admit(ctx, p, projectLaneFailure, identity)
	var bodyErr error
	if entry != nil {
		defer func() {
			if bodyErr == nil && err != nil {
				out = Dispatch{}
			}
		}()
		defer finishProjectCall(s.projectCalls, entry, &err)
	}
	if err != nil {
		return Dispatch{}, err
	}
	out, err = s.lookup(ctx, p, id)
	bodyErr = err
	return
}

func (s *RelaunchCoordinator) VisitRelaunch(ctx context.Context, r wc.TaskRelaunchRequest, policy ec.Policy) (out RelaunchVisit, err error) {
	if ctx == nil || r.Validate() != nil || policy.Validate() != nil {
		return RelaunchVisit{}, invalid()
	}
	if s == nil {
		return RelaunchVisit{}, fault(f.DependencyUnbound)
	}
	r = r.Clone()
	policy = policy.Clone()
	identity, err := projectEntryIdentity(r.ProjectID, "relaunch", f.IdempotencyKey("scheduler_relaunch:"+r.DispatchID))
	if err != nil {
		return RelaunchVisit{}, err
	}
	ctx, entry, err := s.projectCalls.admit(ctx, r.ProjectID, projectLaneRelaunch, identity)
	var bodyErr error
	if entry != nil {
		defer func() {
			if bodyErr == nil && err != nil {
				out = RelaunchVisit{}
			}
		}()
		defer finishProjectCall(s.projectCalls, entry, &err)
	}
	if err != nil {
		return RelaunchVisit{}, err
	}
	out, err = s.visitRelaunch(ctx, r, policy)
	bodyErr = err
	return
}

func (s *RelaunchCoordinator) ResolveRelaunch(ctx context.Context, r wc.TaskRelaunchRequest) (out RelaunchVisit, err error) {
	if ctx == nil || r.Validate() != nil {
		return RelaunchVisit{}, invalid()
	}
	if s == nil {
		return RelaunchVisit{}, fault(f.DependencyUnbound)
	}
	r = r.Clone()
	identity, err := projectEntryIdentity(r.ProjectID, "resolve_relaunch", f.IdempotencyKey("scheduler_relaunch:"+r.DispatchID))
	if err != nil {
		return RelaunchVisit{}, err
	}
	ctx, entry, err := s.projectCalls.admit(ctx, r.ProjectID, projectLaneRelaunch, identity)
	var bodyErr error
	if entry != nil {
		defer func() {
			if bodyErr == nil && err != nil {
				out = RelaunchVisit{}
			}
		}()
		defer finishProjectCall(s.projectCalls, entry, &err)
	}
	if err != nil {
		return RelaunchVisit{}, err
	}
	out, err = s.resolveRelaunch(ctx, r)
	bodyErr = err
	return
}

func (s *ProjectRunner) Run(ctx context.Context) (err error) {
	if ctx == nil {
		return invalid()
	}
	if s == nil {
		return fault(f.DependencyUnbound)
	}
	identity, err := projectEntryIdentity(s.options.ProjectID, "project_run", "project_runner")
	if err != nil {
		return err
	}
	ctx, entry, err := s.projectCalls.admit(ctx, s.options.ProjectID, projectLaneRunner, identity)
	if entry != nil {
		defer finishProjectCall(s.projectCalls, entry, &err)
	}
	if err != nil {
		return err
	}
	return s.run(ctx)
}

func (s *ProjectRunner) RunTraversal(ctx context.Context) (out ProjectRunResult, err error) {
	if ctx == nil {
		return ProjectRunResult{}, invalid()
	}
	if s == nil {
		return ProjectRunResult{}, fault(f.DependencyUnbound)
	}
	identity, err := projectEntryIdentity(s.options.ProjectID, "project_traversal", "project_runner")
	if err != nil {
		return ProjectRunResult{}, err
	}
	ctx, entry, err := s.projectCalls.admit(ctx, s.options.ProjectID, projectLaneRunner, identity)
	if entry != nil {
		defer finishProjectCall(s.projectCalls, entry, &err)
	}
	if err != nil {
		return ProjectRunResult{}, err
	}
	return s.runTraversal(ctx)
}
