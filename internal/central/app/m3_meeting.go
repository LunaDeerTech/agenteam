package app

import (
	"context"
	"github.com/LunaDeerTech/agenteam/internal/central/account"
	au "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/meeting"
	meetinghttp "github.com/LunaDeerTech/agenteam/internal/central/meeting/http"
	meetingtools "github.com/LunaDeerTech/agenteam/internal/central/meeting/tools"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	"github.com/LunaDeerTech/agenteam/internal/central/realtime"
	tr "github.com/LunaDeerTech/agenteam/internal/central/tool/runtime"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	"net/http"
)

// Both context builders consume only the already committed source input.
// The trigger chooses a fixed implementation, never a reload or another run.
type executionTriggerContexts struct{}

func (executionTriggerContexts) BuildTriggerContext(ctx context.Context, r ec.PreparationRequest, input ec.CapturedTriggerInput) (ec.TriggerContext, error) {
	switch r.Launch.Trigger.Kind {
	case "task":
		return (work.TaskContextBuilder{}).BuildTriggerContext(ctx, r, input)
	case "meeting":
		return (meeting.ContextBuilder{}).BuildTriggerContext(ctx, r, input)
	default:
		return ec.TriggerContext{}, f.NewFault(f.CapabilityUnsupported, f.NotStarted)
	}
}

func constructMeeting(db database, b *executionRuntimeAssembly, a *executionRuntimeAuthorities, projects *projectUsageAssembly, models *model.Service, references meeting.ReferenceAuthority, objects *objectAssembly, auditor au.Appender, lifecycleEvents *meeting.ProposalEvents, decisionEvents *meeting.DecisionEvents, attachments ...ec.ArtifactAttachmentCapture) error {
	store, ok := db.(meeting.Store)
	if !ok || b == nil || a == nil || a.m3 == nil || b.agentCalls == nil || b.executionCalls == nil || b.cancellation == nil || b.modelRuntime == nil || b.modelCalls == nil || b.realtime == nil || projects == nil || runtimeInformationNil(references) || objects == nil || objects.guard == nil || runtimeInformationNil(auditor) || lifecycleEvents == nil || decisionEvents == nil {
		return f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	if b.meetingResults == nil {
		var err error
		b.meetingResults, err = execution.NewMeetingExecutionReader(store, a.m3.meeting)
		if err != nil {
			return err
		}
	}
	deletionAuthority, err := meeting.NewDeletionAuthority(a.m3.meeting, objects.guard, objects.process)
	if err != nil {
		return err
	}
	deletionProjects, err := project.NewMeetingDeletionAccess(projects.projects, deletionAuthority)
	if err != nil {
		return err
	}
	retirement, err := execution.NewMeetingExecutionRetirement(b.executionCalls, deletionAuthority)
	if err != nil {
		return err
	}
	if len(attachments) > 1 || len(attachments) == 1 && runtimeInformationNil(attachments[0]) {
		return f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	var capture ec.ArtifactAttachmentCapture
	if len(attachments) == 1 {
		capture = attachments[0]
	}
	b.meetings, err = meeting.NewService(store, meeting.Dependencies{Attachments: capture, Projects: projects.projects, Agents: a.agents, Directory: b.agentCalls.DirectoryReader(), References: references, DecisionAuthority: a.m3.meeting, DecisionResults: b.meetingResults, DeletionAuthority: deletionAuthority, Audit: auditor, LifecycleEvents: lifecycleEvents, RuntimeViews: b.meetingResults, DecisionEvents: decisionEvents, RuntimeProjections: b.meetingResults, Realtime: b.realtime})
	if err != nil {
		return err
	}
	summary, err := meeting.NewSummaryModel(store, meeting.SummaryModelDependencies{Authority: a.m3.summary, Resolver: models, Calls: b.modelRuntime, Stopped: b.modelRuntime, Retirement: b.modelCalls, Retention: b.servingRetention, Actor: a.m3.meetingActor})
	if err != nil {
		return err
	}
	b.add(runtimeWork(summary))
	control, err := execution.NewMeetingExecutionControl(b.cancellation, a.m3.meeting)
	if err != nil {
		return err
	}
	b.meetingRuntime, err = meeting.NewRuntime(store, meeting.RuntimeDependencies{Projects: projects.projects, Launcher: b.executionCalls.Service(), Executor: b.executionCalls.Associated(), Results: b.meetingResults, Authority: a.m3.meeting, Control: control, Summaries: summary, Actor: a.m3.meetingActor, DeletionAuthority: deletionAuthority, DeletionProjects: deletionProjects, Retirement: retirement, LifecycleEvents: lifecycleEvents, Audit: auditor, DecisionEvents: decisionEvents, Realtime: b.realtime})
	if err != nil {
		return err
	}
	b.add(runtimeWork(b.meetingRuntime))
	return nil
}

// Construct the stateless reader and domain backend before Registry captures
// its fixed implementations. Authorization is still lent by the original
// Builtin and Meeting transaction witnesses at invocation time.
func constructMeetingDecisionBackend(db database, b *executionRuntimeAssembly, a *m3Authorities, tools *tr.BuiltinAuthority, events *meeting.DecisionEvents) (tr.BuiltinBackend, error) {
	store, ok := db.(meeting.Store)
	if !ok || b == nil || a == nil || a.meeting == nil || tools == nil || b.meetingResults != nil || b.realtime == nil || events == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	reader, err := execution.NewMeetingExecutionReader(store, a.meeting)
	if err != nil {
		return nil, err
	}
	domain, err := meeting.NewDecisionService(store, a.meeting, tools, reader, events, b.realtime)
	if err != nil {
		return nil, err
	}
	backend, err := meetingtools.NewDecisionBackend(domain)
	if err != nil {
		return nil, err
	}
	b.meetingResults = reader
	return backend, nil
}
func createMeetingHTTP(domain *meeting.Service, core *account.Service, origin string) (http.Handler, error) {
	boundary, err := account.NewHTTPBoundary(core, origin)
	if err != nil {
		return nil, err
	}
	commands, err := meetinghttp.NewHTTPHandler(domain, boundary)
	if err != nil {
		return nil, err
	}
	deletion, err := meetinghttp.NewDeletionHTTPHandler(domain, boundary)
	if err != nil {
		return nil, err
	}
	return meetingDeletionRoutes(commands, deletion), nil
}
func createMeetingRuntimeHTTP(domain *meeting.Service, core *account.Service, origin string) (http.Handler, error) {
	boundary, err := account.NewHTTPBoundary(core, origin)
	if err != nil {
		return nil, err
	}
	return meetinghttp.NewRuntimeHTTPHandler(domain, boundary)
}

func createMeetingRealtimeHTTP(domain *meeting.Service, hub *realtime.Hub, core *account.Service, origin string) (http.Handler, error) {
	boundary, err := account.NewHTTPBoundary(core, origin)
	if err != nil {
		return nil, err
	}
	authority, err := meeting.NewRealtimeAuthority(domain)
	if err != nil {
		return nil, err
	}
	return realtime.NewHTTPHandler(hub, boundary, authority, realtime.HTTPOptions{PublicOrigin: origin})
}

func meetingRealtimeRoutes(previous, stream http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == realtime.Path {
			stream.ServeHTTP(w, r)
			return
		}
		previous.ServeHTTP(w, r)
	})
}
func meetingRuntimeRoutes(previous, runtime http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if meetinghttp.HandlesRuntimePath(r.URL.Path) {
			runtime.ServeHTTP(w, r)
			return
		}
		previous.ServeHTTP(w, r)
	})
}
func meetingRoutes(previous, meetings http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if meetinghttp.HandlesPath(r.URL.Path) || meetinghttp.HandlesDeletionPath(r.URL.Path) {
			meetings.ServeHTTP(w, r)
			return
		}
		previous.ServeHTTP(w, r)
	})
}

func meetingDeletionRoutes(commands, deletion http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if meetinghttp.HandlesDeletionPath(r.URL.Path) {
			deletion.ServeHTTP(w, r)
			return
		}
		commands.ServeHTTP(w, r)
	})
}
