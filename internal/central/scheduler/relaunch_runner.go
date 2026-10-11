package scheduler

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func (s *ProjectRunner) unknownRelaunch(task wc.TaskID) (wc.TaskRelaunchRequest, bool) {
	r := s.options.Relaunch
	if r == nil {
		return wc.TaskRelaunchRequest{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, call := range r.unknown {
		if call.request.ProjectID == s.options.ProjectID && call.request.TaskID == task {
			return call.request.Clone(), true
		}
	}
	return wc.TaskRelaunchRequest{}, false
}
func (s *ProjectRunner) resumeRelaunch(ctx context.Context, out ProjectTaskVisit, req wc.TaskRelaunchRequest) ProjectTaskVisit {
	out.Action, out.RelaunchRequest = ProjectVisitRelaunch, &req
	id, _ := f.ParseID[DispatchIdentity](req.DispatchID)
	out.DispatchID = &id
	out.Relaunch, out.Err = s.options.Relaunch.ResolveRelaunch(ctx, req)
	return s.finishRelaunchVisit(ctx, out)
}
func (s *ProjectRunner) visitRelaunchTask(ctx context.Context, out ProjectTaskVisit, facts wc.SchedulerTaskFacts) ProjectTaskVisit {
	id, err := f.NewID[DispatchIdentity]()
	if err != nil {
		out.Err = portError(err)
		return out
	}
	request, err := f.NewID[f.Request]()
	if err != nil {
		out.Err = portError(err)
		return out
	}
	purpose := "task/work"
	if facts.State == wc.TaskStateInReview {
		purpose = "task/review"
	} else if facts.State != wc.TaskStateInProgress {
		out.Err = fault(f.InvalidState)
		return out
	}
	req := wc.TaskRelaunchRequest{ProjectID: s.options.ProjectID, TaskID: facts.TaskID, AgentID: *facts.AssigneeAgentID, CurrentSprintID: facts.SprintID, ExpectedTaskVersion: facts.Version, DispatchID: id.String(), RequestID: request, Purpose: purpose}
	out.Action, out.RelaunchRequest, out.DispatchID = ProjectVisitRelaunch, &req, &id
	out.Relaunch, out.Err = s.options.Relaunch.VisitRelaunch(ctx, req, s.options.LaunchPolicy)
	return s.finishRelaunchVisit(ctx, out)
}
func (s *ProjectRunner) finishRelaunchVisit(ctx context.Context, out ProjectTaskVisit) ProjectTaskVisit {
	out.Dispatch = out.Relaunch.Dispatch
	if out.Err != nil || out.Relaunch.CooldownSkipped {
		return out
	}
	id := out.Dispatch.Summary().ID
	if id.Validate() != nil {
		out.Err = unavailable(nil)
		return out
	}
	result, err := s.visitor.Visit(ctx, s.options.ProjectID, id)
	if result.Found {
		out.Dispatch = result.Dispatch
	}
	out.Err = err
	return out
}
