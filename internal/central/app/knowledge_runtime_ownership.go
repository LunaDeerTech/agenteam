package app

import (
	"context"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/knowledge"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// One original pair owns the HTTP service, resolver and Object source handles.
// Construction fixes the expected guard but does not initialize or probe it;
// Object.Initialize remains the original process owner's responsibility.
type knowledgeCallAssembly struct {
	pair    *knowledge.ProjectCallOwnership
	index   *knowledge.IndexService
	cleanup pc.ProjectLifecycleParticipant
}

func createOwnedKnowledge(cfg config.Config, db database, authority *knowledge.Authority, projects *projectUsageAssembly, accounts *account.Authority, objects *objectAssembly, auditor *audit.Service, journal *outbox.Service, events kc.KnowledgeEvents, invocation ...kc.KnowledgeInvocationAuthority) (*knowledgeCallAssembly, error) {
	store, ok := db.(knowledge.Store)
	if !ok || runtimeInformationNil(store) || authority == nil || projects == nil || projects.lifecycle == nil || projects.projects == nil || accounts == nil || objects == nil || objects.service == nil || objects.guard == nil || objects.process.Validate() != nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	if len(invocation) > 1 {
		return nil, f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	var tools kc.KnowledgeInvocationAuthority
	if len(invocation) == 1 && !runtimeInformationNil(invocation[0]) {
		tools = invocation[0]
	}
	stop, err := project.NewKnowledgeStopAuthority(projects.lifecycle, store)
	if err != nil {
		return nil, err
	}
	sourceStop, err := project.NewKnowledgeProjectSourceStopAuthority(projects.lifecycle, store)
	if err != nil {
		return nil, err
	}
	cleanup, err := project.NewKnowledgeProjectCleanupAuthority(projects.lifecycle, store)
	if err != nil {
		return nil, err
	}
	pair, err := knowledge.NewProjectCallOwnership(store, authority, knowledge.Dependencies{
		InvocationAuthority: tools, Projects: projects.projects, Activity: accounts, Objects: objects.service, Uploads: objects.service,
		ReferenceCleanup: objects.service, ObjectCleanup: objects.service, Audit: auditor, Outbox: journal, Events: events,
		Processes: outboxProcessAuthority{process: objects.process, guard: objects.guard},
		Cursors:   cfg.CursorKeyring(), Confirmations: cfg.KnowledgeConfirmationKeys(),
	}, objects.service, objects.guard, objects.process, stop, sourceStop)
	if err != nil {
		return nil, err
	}
	bound := &knowledgeCallAssembly{pair: pair}
	bound.cleanup, err = knowledge.NewProjectCleanupParticipant(pair, cleanup)
	// Publish even a partial pair to the original Account owner before handling
	// the error. Neither a late Stop nor a failed final binding can lose its tail.
	return bound, err
}

func (a *knowledgeCallAssembly) StopAdmission() {
	if a != nil && a.index != nil {
		a.index.StopAdmission()
	}
	if a != nil && a.pair != nil {
		a.pair.Stop()
	}
}
func (a *knowledgeCallAssembly) Drain(ctx context.Context) error {
	if a == nil || a.pair == nil {
		return nil
	}
	a.StopAdmission()
	if a.index != nil {
		if err := a.index.Drain(ctx); err != nil {
			return err
		}
	}
	if err := a.pair.Drain(ctx); err != nil {
		return err
	}
	if !a.pair.Joined() {
		return f.NewFault(f.DependencyUnavailable, f.NotStarted)
	}
	return nil
}
func (a *knowledgeCallAssembly) Force(ctx context.Context) error { return a.Drain(ctx) }
func (a *knowledgeCallAssembly) Joined() bool {
	return a == nil || (a.index == nil || a.index.Joined()) && (a.pair == nil || a.pair.Joined())
}

var _ accountWork = (*knowledgeCallAssembly)(nil)
