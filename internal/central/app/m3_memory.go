package app

import (
	"context"
	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/memory"
	memoryhttp "github.com/LunaDeerTech/agenteam/internal/central/memory/http"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	rc "github.com/LunaDeerTech/agenteam/internal/central/retrieval/contract"
	rr "github.com/LunaDeerTech/agenteam/internal/central/retrieval/runtime"
	tr "github.com/LunaDeerTech/agenteam/internal/central/tool/runtime"
	"net/http"
)

type memoryAudit struct{ appender ac.Appender }

func (a memoryAudit) AppendMemoryInTx(ctx context.Context, tx f.Tx, event memory.AuditEvent) error {
	action := ac.Action("memory." + event.Action)
	metadata, err := ac.MemoryMetadata(action, ac.MemoryMetadataFields{AgentID: event.Namespace.AgentID.String(), MemoryID: event.MemoryID, OperationID: event.OperationID, Gate: event.Gate, Added: event.Added, Updated: event.Updated, Deleted: event.Deleted, Noop: event.Noop})
	if err != nil {
		return err
	}
	kind, key := ac.MemoryResource, event.MemoryID
	if action == ac.MemoryRetain {
		kind, key = ac.MemoryNamespaceResource, event.Namespace.AgentID.String()
	}
	resource, err := ac.NewResource(kind, key)
	if err != nil {
		return err
	}
	scope, err := i.InProject(event.Namespace.ProjectID)
	if err != nil {
		return err
	}
	associations := ac.Associations{}
	if event.Actor.Details().Kind == i.AgentRun {
		associations.ExecutionID = event.Actor.Details().ExecutionID
		associations.OperationID = event.OperationID
	}
	entry, err := ac.NewEntry(ac.EntryFields{Actor: event.Actor, Scope: scope, Action: action, Resource: resource, Outcome: ac.Success, Metadata: metadata, Associations: associations})
	if err != nil {
		return err
	}
	appendKey, err := ac.NewAppendKey(ac.MemoryProducer, event.CauseRef, 0)
	if err != nil {
		return err
	}
	_, err = a.appender.AppendInTx(ctx, tx, entry, appendKey)
	return err
}

func constructMemory(cfg config.Config, db database, b *executionRuntimeAssembly, a *executionRuntimeAuthorities, projects *projectUsageAssembly, agents *agent.Authority, tools *tr.BuiltinAuthority, auditor *audit.Service, models *model.Service, observer rc.Observer) error {
	options, enabled := cfg.RetrievalRuntime()
	if !enabled {
		return nil
	}
	store, ok := db.(memory.Store)
	if !ok || b == nil || a == nil || a.m3 == nil || b.modelRuntime == nil || b.modelCalls == nil || b.initialize == nil {
		return f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	domain, err := memory.New(store, memory.Dependencies{Projects: projects.projects, Agents: agents, Tools: tools, Audit: memoryAudit{auditor}, Gate: memory.SensitiveDetector{}})
	if err != nil {
		return err
	}
	b.memories = domain
	// The domain owns no calls before publication. Once Models exists its
	// combined owner is immediately registered, including failed later wiring.
	modelCalls, err := memory.NewModels(domain, a.m3.memory, memory.ModelDependencies{Resolver: models, Chat: b.modelRuntime, Embedding: b.modelRuntime, Retirement: b.modelCalls, Stopped: b.modelRuntime, ServingRetention: b.servingRetention, Profiles: b.embeddingProfiles})
	if err != nil {
		return err
	}
	b.add(executionRuntimeWork{stop: domain.StopAdmission, drain: modelCalls.Drain, joined: modelCalls.Joined})
	provider, err := memory.NewRetrievalProvider(domain, options.Backend())
	if err != nil {
		return err
	}
	var observers []rc.Observer
	if observer != nil {
		observers = append(observers, observer)
	}
	search, err := rr.New(provider, modelCalls, m3RetrievalTimeouts(), observers...)
	if err != nil {
		return err
	}
	b.memory, err = memory.NewRuntime(domain, modelCalls, search, memory.RuntimeConfig{Limits: m3RetrievalLimits()})
	if err != nil {
		return err
	}
	previousInitialize, memoryRuntime := b.initialize, b.memory
	b.initialize = func(ctx context.Context) error {
		if err := previousInitialize(ctx); err != nil {
			return err
		}
		return memoryRuntime.Initialize(ctx)
	}
	return nil
}
func memoryToolBackends(runtime *memory.Runtime) ([]tr.BuiltinBackend, error) {
	return memory.NewBuiltinBackends(runtime)
}
func createMemoryHTTP(domain *memory.Service, core *account.Service, origin string) (http.Handler, error) {
	boundary, err := account.NewHTTPBoundary(core, origin)
	if err != nil {
		return nil, err
	}
	return memoryhttp.NewHTTPHandler(domain, boundary)
}
func memoryRoutes(previous, memories http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if memoryhttp.HandlesPath(r.URL.Path) {
			memories.ServeHTTP(w, r)
			return
		}
		previous.ServeHTTP(w, r)
	})
}
