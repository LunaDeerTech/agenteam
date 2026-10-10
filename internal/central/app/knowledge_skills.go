package app

import (
	"context"
	"net/http"
	"sync"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/knowledge"
	knowledgecommands "github.com/LunaDeerTech/agenteam/internal/central/knowledge/commandhttp"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	knowledgehttp "github.com/LunaDeerTech/agenteam/internal/central/knowledge/http"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	skillhttp "github.com/LunaDeerTech/agenteam/internal/central/skill/http"
)

type knowledgeSkillAuthorities struct {
	knowledge *knowledge.Authority
	skills    *skill.Authority
	audit     ac.ProjectAuthority
}

func createKnowledgeSkillAuthorities(db database, projects *projectUsageAssembly) (*knowledgeSkillAuthorities, error) {
	knowledgeStore, ok := db.(knowledge.Store)
	if !ok || runtimeInformationNil(knowledgeStore) || projects == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	skillStore, ok := db.(skill.Store)
	if !ok || runtimeInformationNil(skillStore) {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	documents, err := knowledge.NewAuthority(knowledgeStore, projects.projects)
	if err != nil {
		return nil, err
	}
	skills, err := skill.NewAuthority(skillStore, projects.projects)
	if err != nil {
		return nil, err
	}
	facts, err := skill.NewInitializationAuditFacts(skills, projects.objectFacts)
	if err != nil {
		return nil, err
	}
	auditing, err := project.NewInitializationAuditAuthority(projects.projects, facts)
	if err != nil {
		return nil, err
	}
	return &knowledgeSkillAuthorities{knowledge: documents, skills: skills, audit: auditing}, nil
}

func createKnowledge(cfg config.Config, db database, authority *knowledge.Authority, projects *project.Authority, accounts *account.Authority, objects *objectAssembly, auditor *audit.Service, journal *outbox.Service, events kc.KnowledgeEvents) (*knowledge.Service, error) {
	store, ok := db.(knowledge.Store)
	if !ok || runtimeInformationNil(store) || objects == nil || objects.service == nil || objects.guard == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	sources, err := knowledge.NewSourceResolver(store, authority, objects.service)
	if err != nil {
		return nil, err
	}
	reads, err := object.NewSourceReads(objects.service, sources)
	if err != nil {
		return nil, err
	}
	return knowledge.New(store, knowledge.Dependencies{
		Projects: projects, Activity: accounts, Objects: objects.service, Uploads: objects.service,
		Sources: sources, SourceReads: reads, ReferenceCleanup: objects.service, ObjectCleanup: objects.service,
		Audit: auditor, Outbox: journal, Events: events,
		Processes: outboxProcessAuthority{process: objects.process, guard: objects.guard},
		Cursors:   cfg.CursorKeyring(), Confirmations: cfg.KnowledgeConfirmationKeys(),
	})
}

func createSkills(ctx context.Context, authority *skill.Authority, objects *objectAssembly) (*skill.Service, error) {
	if objects == nil || objects.service == nil || objects.guard == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	bundle, err := skill.AddSkills(ctx)
	if err != nil {
		return nil, err
	}
	return skill.New(skill.Dependencies{Authority: authority, Objects: objects.service, Processes: objects.guard, ProcessID: objects.process, Bundle: bundle})
}

type skillWork struct{ service *skill.Service }

func (w *skillWork) StopAdmission() { w.service.Stop() }
func (w *skillWork) Drain(ctx context.Context) error {
	w.StopAdmission()
	return w.service.Drain(ctx)
}
func (w *skillWork) Force(ctx context.Context) error { return w.Drain(ctx) }
func (w *skillWork) Joined() bool                    { return w.service.Joined() }

// Knowledge has no Joined port. A successful original Drain after Stop is the
// only proof retained here; cancellation and attempted draining cannot set it.
type knowledgeWork struct {
	service         *knowledge.Service
	mu              sync.Mutex
	stopped, joined bool
}

func (w *knowledgeWork) StopAdmission() {
	w.service.Stop()
	w.mu.Lock()
	w.stopped = true
	w.mu.Unlock()
}
func (w *knowledgeWork) Drain(ctx context.Context) error {
	w.StopAdmission()
	err := w.service.Drain(ctx)
	if err == nil {
		w.mu.Lock()
		w.joined = true
		w.mu.Unlock()
	}
	return err
}
func (w *knowledgeWork) Force(ctx context.Context) error { return w.Drain(ctx) }
func (w *knowledgeWork) Joined() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stopped && w.joined
}

func knowledgeSkillHandlers(documents *knowledge.Service, skills *skill.Service, core *account.Service, origin string) (http.Handler, http.Handler, http.Handler, error) {
	boundary, err := account.NewHTTPBoundary(core, origin)
	if err != nil {
		return nil, nil, nil, err
	}
	reads, err := knowledgehttp.NewHTTPHandler(documents, boundary)
	if err != nil {
		return nil, nil, nil, err
	}
	commands, err := knowledgecommands.NewHTTPHandler(documents, boundary)
	if err != nil {
		return nil, nil, nil, err
	}
	packages, err := skillhttp.NewHTTPHandler(skills, boundary)
	if err != nil {
		return nil, nil, nil, err
	}
	return reads, commands, packages, nil
}

func knowledgeSkillRoutes(existing, reads, commands, packages http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case knowledgehttp.HandlesPath(r.URL.Path):
			reads.ServeHTTP(w, r)
		case knowledgecommands.HandlesPath(r.URL.Path):
			commands.ServeHTTP(w, r)
		case skillhttp.HandlesPath(r.URL.Path):
			packages.ServeHTTP(w, r)
		default:
			existing.ServeHTTP(w, r)
		}
	})
}

var _ accountWork = (*skillWork)(nil)
var _ accountWork = (*knowledgeWork)(nil)
