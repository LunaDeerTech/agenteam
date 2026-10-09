package app

import (
	"context"
	"errors"
	"sync"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type workPlanningEvents struct {
	structure wc.WorkEvents
	tasks     wc.TaskEvents
	blockers  wc.TaskBlockerEvents
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
	structure       *work.Service
	structureReader *work.Reader
	tasks           *work.TaskService
	taskReader      *work.TaskReader
	blockers        *work.BlockerService
	blockerReader   *work.BlockerReader
	commands        []workCommandCalls
	mu              sync.Mutex
	stopped, joined bool
}

// These are pure constructors. Until the complete bundle is installed, none of
// its services has been exposed to HTTP or another caller. On a partial failure
// the local owner still retires every service it has constructed.
func createWorkPlanning(cfg config.Config, db database, authority *work.Authority, accounts *account.Authority, journal *outbox.Service, events workPlanningEvents) (result *workPlanningAssembly, err error) {
	store, ok := db.(work.Store)
	if !ok || runtimeInformationNil(store) || authority == nil || accounts == nil || journal == nil || !events.structure.Valid() || !events.tasks.Valid() || !events.blockers.Valid() {
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
	if b.tasks, err = work.NewTask(store, work.TaskDependencies{Authority: authority, Structure: b.structureReader, Events: journal, TaskEvents: events.tasks, Activity: accounts}); err != nil {
		return nil, err
	}
	b.commands = append(b.commands, b.tasks)
	if b.blockers, err = work.NewBlocker(store, work.BlockerDependencies{Authority: authority, Structure: b.structureReader, Events: journal, BlockerEvents: events.blockers, Activity: accounts}); err != nil {
		return nil, err
	}
	b.commands = append(b.commands, b.blockers)
	return b, nil
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
	defer b.mu.Unlock()
	return b.stopped && b.joined
}

var _ accountWork = (*workPlanningAssembly)(nil)
