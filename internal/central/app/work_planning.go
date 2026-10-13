package app

import (
	"context"
	"errors"
	"net/http"
	"sync"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	"github.com/LunaDeerTech/agenteam/internal/central/scheduler"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	workhttp "github.com/LunaDeerTech/agenteam/internal/central/work/http"
)

type workPlanningEvents struct {
	structure   wc.WorkEvents
	tasks       wc.TaskEvents
	blockers    wc.TaskBlockerEvents
	transitions wc.TaskTransitionEvents
	sprints     wc.SprintLifecycleEvents
}

func defineWorkPlanningEvents(catalog *event.Catalog) (workPlanningEvents, error) {
	var v workPlanningEvents
	var err error
	if v.structure, err = wc.RegisterWorkEvents(catalog); err != nil {
		return workPlanningEvents{}, err
	}
	if v.tasks, err = wc.RegisterTaskEvents(catalog); err != nil {
		return workPlanningEvents{}, err
	}
	if v.blockers, err = wc.RegisterTaskBlockerEvents(catalog); err != nil {
		return workPlanningEvents{}, err
	}
	if v.transitions, err = wc.RegisterTaskTransitionEvents(catalog); err != nil {
		return workPlanningEvents{}, err
	}
	if v.sprints, err = wc.RegisterSprintLifecycleEvents(catalog); err != nil {
		return workPlanningEvents{}, err
	}
	return v, nil
}

// The producer is built before the Outbox that captures it. Every command and
// reader below receives this same capability, never a late-filled locator.
func createWorkPlanningAuthority(db database, projects *project.Authority) (*work.Authority, error) {
	store, ok := db.(work.Store)
	if !ok || runtimeInformationNil(store) || projects == nil {
		return nil, foundation.NewFault(foundation.DependencyUnbound, foundation.NotStarted)
	}
	return work.NewAuthority(store, projects)
}

type workCommandCalls interface {
	Stop()
	Drain(context.Context) error
}

type workPlanningAssembly struct {
	ownership       *work.ProjectCallOwnership
	structure       *work.Service
	structureReader *work.Reader
	tasks           *work.TaskService
	taskReader      *work.TaskReader
	blockers        *work.BlockerService
	blockerReader   *work.BlockerReader
	transitions     *work.TaskTransitionService
	sprints         *work.SprintLifecycleService
	commands        []workCommandCalls
	mu              sync.Mutex
	stopped, joined bool
}

// These are pure constructors. Until the complete bundle is installed, none of
// its services has been exposed to HTTP or another caller. On a partial failure
// the local owner still retires every service it has constructed.
func createWorkPlanning(cfg config.Config, db database, authority *work.Authority, accounts *account.Authority, journal *outbox.Service, events workPlanningEvents, transitionProjects ...*project.Authority) (*workPlanningAssembly, error) {
	store, ok := db.(work.Store)
	if !ok || runtimeInformationNil(store) {
		return nil, foundation.NewFault(foundation.DependencyUnbound, foundation.NotStarted)
	}
	pending, err := scheduler.NewPendingAuthority(store)
	if err != nil {
		return nil, err
	}
	return createWorkPlanningWithPending(cfg, db, authority, accounts, journal, events, pending, transitionProjects...)
}

func createWorkPlanningWithPending(cfg config.Config, db database, authority *work.Authority, accounts *account.Authority, journal *outbox.Service, events workPlanningEvents, pending *scheduler.PendingAuthority, transitionProjects ...*project.Authority) (result *workPlanningAssembly, err error) {
	return createWorkPlanningWithProviders(cfg, db, authority, accounts, journal, events, pending, nil, transitionProjects...)
}

func createWorkPlanningWithAgents(cfg config.Config, db database, authority *work.Authority, accounts *account.Authority, journal *outbox.Service, events workPlanningEvents, agents *agent.Authority, projects *project.Authority) (*workPlanningAssembly, error) {
	store, ok := db.(work.Store)
	if !ok || runtimeInformationNil(store) || agents == nil {
		return nil, foundation.NewFault(foundation.DependencyUnbound, foundation.NotStarted)
	}
	pending, err := scheduler.NewPendingAuthority(store)
	if err != nil {
		return nil, err
	}
	return createWorkPlanningWithProviders(cfg, db, authority, accounts, journal, events, pending, agents, projects)
}

func createWorkPlanningWithProviders(cfg config.Config, db database, authority *work.Authority, accounts *account.Authority, journal *outbox.Service, events workPlanningEvents, pending *scheduler.PendingAuthority, originalAgents *agent.Authority, transitionProjects ...*project.Authority) (result *workPlanningAssembly, err error) {
	store, ok := db.(work.Store)
	if !ok || runtimeInformationNil(store) || pending == nil || authority == nil || accounts == nil || journal == nil || !events.structure.Valid() || !events.tasks.Valid() || !events.blockers.Valid() {
		return nil, foundation.NewFault(foundation.DependencyUnbound, foundation.NotStarted)
	}
	// An omitted capability retains the old three-service assembly. An explicit
	// capability must be exactly one original Project authority; no partial or
	// silently ignored transition/lifecycle binding is accepted.
	if len(transitionProjects) > 1 || len(transitionProjects) == 1 && (transitionProjects[0] == nil || !events.transitions.Valid() || !events.sprints.Valid()) {
		return nil, foundation.NewFault(foundation.DependencyUnbound, foundation.NotStarted)
	}
	b := &workPlanningAssembly{}
	defer func() {
		if err != nil {
			// No admission is reachable during pure construction, so these drains
			// are immediate and cannot acquire an independent shutdown budget.
			b.StopAdmission()
			_ = b.Drain(context.Background())
		}
	}()
	if b.structureReader, err = work.NewReader(store, authority, cfg.CursorKeyring()); err != nil {
		return nil, err
	}
	if b.taskReader, err = work.NewTaskReader(store, authority, b.structureReader, cfg.CursorKeyring()); err != nil {
		return nil, err
	}
	if b.blockerReader, err = work.NewBlockerReader(store, authority, cfg.CursorKeyring()); err != nil {
		return nil, err
	}
	if b.structure, err = work.New(store, work.Dependencies{Authority: authority, Events: journal, WorkEvents: events.structure, Activity: accounts}); err != nil {
		return nil, err
	}
	b.commands = append(b.commands, b.structure)
	if b.tasks, err = work.NewTask(store, work.TaskDependencies{Authority: authority, Structure: b.structureReader, Events: journal, TaskEvents: events.tasks, Activity: accounts, Pending: pending}); err != nil {
		return nil, err
	}
	b.commands = append(b.commands, b.tasks)
	if b.blockers, err = work.NewBlocker(store, work.BlockerDependencies{Authority: authority, Structure: b.structureReader, Events: journal, BlockerEvents: events.blockers, Activity: accounts}); err != nil {
		return nil, err
	}
	b.commands = append(b.commands, b.blockers)
	if len(transitionProjects) == 1 {
		// All providers receive the original application Store and Project
		// authority. Their current reads use the caller's actual transaction.
		agents := originalAgents
		if agents == nil {
			agents, err = agent.NewAuthority(store, transitionProjects[0])
			if err != nil {
				return nil, err
			}
		}
		occupancy, e := execution.NewWorkOccupancy(store)
		if e != nil {
			return nil, e
		}
		if b.transitions, err = work.NewTaskTransition(store, work.TaskTransitionDependencies{
			Agents: agents, Occupancy: occupancy, Pending: pending,
			Structure: b.structureReader, Authority: authority, Events: journal,
			TaskEvents: events.transitions, Activity: accounts,
		}); err != nil {
			return nil, err
		}
		b.commands = append(b.commands, b.transitions)
		pointer, e := project.NewSprintLifecycle(transitionProjects[0], authority)
		if e != nil {
			return nil, e
		}
		if b.sprints, err = work.NewSprintLifecycle(store, work.SprintLifecycleDependencies{
			Authority: authority, Projects: pointer, Events: journal,
			SprintEvents: events.sprints, Activity: accounts,
		}); err != nil {
			return nil, err
		}
		b.commands = append(b.commands, b.sprints)
	}
	return b, nil
}

// The default App installs one original five-service/three-reader owner before
// exposing any of its views. Legacy constructors above keep their independent
// services and do not start registering calls.
func createOwnedWorkPlanningWithAgents(cfg config.Config, db database, authority *work.Authority, accounts *account.Authority, journal *outbox.Service, events workPlanningEvents, agents *agent.Authority, projects *project.Authority, lifecycle *project.LifecycleAuthority, objects *objectAssembly) (*workPlanningAssembly, error) {
	store, ok := db.(work.Store)
	if !ok || runtimeInformationNil(store) {
		return nil, foundation.NewFault(foundation.DependencyUnbound, foundation.NotStarted)
	}
	pending, err := scheduler.NewPendingAuthority(store)
	if err != nil {
		return nil, err
	}
	return createOwnedWorkPlanningWithProviders(cfg, db, authority, accounts, journal, events, pending, agents, projects, lifecycle, objects)
}

func createOwnedWorkPlanningWithProviders(cfg config.Config, db database, authority *work.Authority, accounts *account.Authority, journal *outbox.Service, events workPlanningEvents, pending *scheduler.PendingAuthority, agents *agent.Authority, projects *project.Authority, lifecycle *project.LifecycleAuthority, objects *objectAssembly) (*workPlanningAssembly, error) {
	store, ok := db.(work.Store)
	if !ok || runtimeInformationNil(store) || authority == nil || accounts == nil || journal == nil || pending == nil || agents == nil || projects == nil || objects == nil || objects.guard == nil || objects.process.Validate() != nil || !events.structure.Valid() || !events.tasks.Valid() || !events.blockers.Valid() || !events.transitions.Valid() || !events.sprints.Valid() {
		return nil, foundation.NewFault(foundation.DependencyUnbound, foundation.NotStarted)
	}
	stop, err := project.NewWorkStopAuthority(lifecycle, store)
	if err != nil {
		return nil, err
	}
	// The domain factory validates these ordinary placement dependencies, then
	// replaces them with its private owned Reader before returning any service.
	// This construction-only reader is never published or called.
	placement, err := work.NewReader(store, authority, cfg.CursorKeyring())
	if err != nil {
		return nil, err
	}
	occupancy, err := execution.NewWorkOccupancy(store)
	if err != nil {
		return nil, err
	}
	pointer, err := project.NewSprintLifecycle(projects, authority)
	if err != nil {
		return nil, err
	}
	pair, err := work.NewProjectCallOwnershipWithReaders(store, work.ProjectCallDependencies{
		Structure: work.Dependencies{Authority: authority, Events: journal, WorkEvents: events.structure, Activity: accounts},
		Tasks:     work.TaskDependencies{Authority: authority, Structure: placement, Events: journal, TaskEvents: events.tasks, Activity: accounts, Pending: pending},
		Transitions: work.TaskTransitionDependencies{
			Agents: agents, Occupancy: occupancy, Pending: pending, Structure: placement,
			Authority: authority, Events: journal, TaskEvents: events.transitions, Activity: accounts,
		},
		Blockers: work.BlockerDependencies{Authority: authority, Structure: placement, Events: journal, BlockerEvents: events.blockers, Activity: accounts},
		Sprints:  work.SprintLifecycleDependencies{Authority: authority, Projects: pointer, Events: journal, SprintEvents: events.sprints, Activity: accounts},
	}, cfg.CursorKeyring(), objects.guard, objects.process, stop)
	if err != nil {
		return nil, err
	}
	return &workPlanningAssembly{
		ownership: pair,
		structure: pair.Service(), structureReader: pair.Reader(),
		tasks: pair.TaskService(), taskReader: pair.TaskReader(),
		blockers: pair.BlockerService(), blockerReader: pair.BlockerReader(),
		transitions: pair.TaskTransitionService(), sprints: pair.SprintLifecycleService(),
		commands: []workCommandCalls{pair},
	}, nil
}

func (b *workPlanningAssembly) StopAdmission() {
	b.mu.Lock()
	b.stopped = true
	b.mu.Unlock()
	for _, command := range b.commands {
		command.Stop()
	}
}

func (b *workPlanningAssembly) Drain(ctx context.Context) error {
	b.StopAdmission()
	for _, command := range b.commands {
		if err := command.Drain(ctx); err != nil {
			return err
		}
	}
	if b.ownership != nil && !b.ownership.Joined() {
		return foundation.NewFault(foundation.DependencyUnavailable, foundation.NotStarted)
	}
	b.markJoined()
	return nil
}

func (b *workPlanningAssembly) Force(ctx context.Context) error {
	// Work has Stop/Drain, not a separate Force capability. Even with no time
	// left, every admission is cancelled and every concrete Drain is attempted.
	b.StopAdmission()
	var result error
	for _, command := range b.commands {
		result = errors.Join(result, command.Drain(ctx))
	}
	if result == nil && b.ownership != nil && !b.ownership.Joined() {
		return foundation.NewFault(foundation.DependencyUnavailable, foundation.NotStarted)
	}
	if result == nil {
		b.markJoined()
	}
	return result
}

func (b *workPlanningAssembly) markJoined() {
	b.mu.Lock()
	b.joined = true
	b.mu.Unlock()
}

func (b *workPlanningAssembly) Joined() bool {
	b.mu.Lock()
	stopped, joined := b.stopped, b.joined
	b.mu.Unlock()
	if b.ownership != nil {
		return stopped && b.ownership.Joined()
	}
	return stopped && joined
}

func workPlanningHandler(b *workPlanningAssembly, core *account.Service, origin string) (http.Handler, error) {
	if b == nil {
		return nil, foundation.NewFault(foundation.DependencyUnbound, foundation.NotStarted)
	}
	boundary, err := account.NewHTTPBoundary(core, origin)
	if err != nil {
		return nil, err
	}
	return workhttp.NewHTTPHandler(workhttp.Bindings{
		Structure: b.structure, StructureReader: b.structureReader,
		Tasks: b.tasks, TaskReader: b.taskReader,
		Blockers: b.blockers, BlockerReader: b.blockerReader,
		Transitions:     b.transitions,
		SprintLifecycle: b.sprints,
	}, boundary)
}

func workPlanningRoutes(existing, planning http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if workhttp.HandlesPath(r.URL.Path) {
			planning.ServeHTTP(w, r)
			return
		}
		existing.ServeHTTP(w, r)
	})
}

var _ accountWork = (*workPlanningAssembly)(nil)
