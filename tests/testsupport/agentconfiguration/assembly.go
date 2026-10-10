// Package agentconfiguration supplies a fixed real-authority composition for
// integration fixtures. It neither bootstraps identities nor publishes an
// Agent, enables the default application, or supplies missing runtime tools.
package agentconfiguration

import (
	"reflect"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	agentc "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/knowledge"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	"github.com/LunaDeerTech/agenteam/internal/central/mount"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	"github.com/LunaDeerTech/agenteam/internal/central/projectvariable"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/registry"
)

// The single Store supplies all domain methods. No adapter creates a second
// Store or converts the caller's opaque transaction to another owner.
type Store interface {
	account.Store
	audit.Store
}

type Options struct {
	// Accounts must be the fixture's real authority created against this Store.
	// Its live Session checks retain the original Store/Tx ownership checks.
	Accounts *account.Authority
	// Sources are actual domain backends fixed at construction. A test source,
	// handler label, registration row or nonnil port is not backend readiness.
	BuiltinSources   map[string]registry.BuiltinSource
	InstallSourceKey string
}

// Providers is a copy of the constructed pointers. Mutating this projection
// does not change the graph later used by Assembly.NewService.
type Providers struct {
	Accounts         *account.Authority
	Projects         *project.Authority
	Agents           *agent.Authority
	AgentEvents      *agent.EventAuthority
	Models           *model.Authority
	ModelSelections  *model.AgentConfiguration
	ModelReferences  *model.AgentReferenceService
	Variables        *projectvariable.Authority
	SecretDirectory  *projectvariable.SecretDirectory
	SecretReferences *projectvariable.SecretReferenceService
	SkillAuthority   *skill.Authority
	Skills           *skill.AgentInitializer
	Mounts           *mount.Configuration
	Registry         *registry.Registry
	AuditProjects    ac.ProjectAuthority
}

type Assembly struct {
	store         Store
	providers     Providers
	catalog       *event.Catalog
	installSource string
}

func unbound() error { return f.NewFault(f.DependencyUnbound, f.NotStarted) }
func nilPort(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	}
	return false
}

// New builds the metadata/owner graph without I/O, workers, mutable setters or
// synthetic facts. It is deliberately not an all-providers-ready result: a
// Registry with no real install source is useful for testing the refusal, but
// cannot be passed through NewService as a successful Agent configuration.
func New(store Store, options Options) (*Assembly, error) {
	if nilPort(store) || !reflect.TypeOf(store).Comparable() || options.Accounts == nil {
		return nil, unbound()
	}
	p := Providers{Accounts: options.Accounts}
	// Preserve the existing Project fact routes, adding Agent through its
	// Store-only private-witness checker before the immutable map is copied.
	agentFacts, err := agent.NewProjectAuditAuthority(store)
	if err != nil {
		return nil, err
	}
	secretFacts, err := secret.NewProjectAuditAuthority(store)
	if err != nil {
		return nil, err
	}
	objectFacts, err := object.NewProjectAuditAuthority(store)
	if err != nil {
		return nil, err
	}
	knowledgeFacts, err := knowledge.NewProjectAuditAuthority(store)
	if err != nil {
		return nil, err
	}
	p.Variables, err = projectvariable.NewAuthority(store)
	if err != nil {
		return nil, err
	}
	p.Projects, err = project.NewAuthority(store, project.AuthorityDependencies{
		Sessions: p.Accounts, Routes: p.Accounts,
		AuditFacts: map[ac.Producer]ac.ProjectFactAuthority{
			ac.AgentProducer: agentFacts, ac.SecretProducer: secretFacts,
			ac.ObjectProducer: objectFacts, ac.KnowledgeProducer: knowledgeFacts,
			ac.ProjectVariableProducer: p.Variables,
		},
	})
	if err != nil {
		return nil, err
	}
	p.Agents, err = agent.NewAuthority(store, p.Projects)
	if err != nil {
		return nil, err
	}
	p.Models, err = model.NewAuthority(store, model.Authorizations{Sessions: p.Accounts, System: p.Accounts, Projects: p.Projects})
	if err != nil {
		return nil, err
	}
	p.ModelSelections, err = model.NewAgentConfiguration(store, p.Models)
	if err != nil {
		return nil, err
	}
	modelOwner, err := agent.NewModelOwnerAuthority(p.Agents)
	if err != nil {
		return nil, err
	}
	p.ModelReferences, err = model.NewAgentReferences(store, p.ModelSelections, modelOwner)
	if err != nil {
		return nil, err
	}
	p.SecretDirectory, err = projectvariable.NewSecretDirectory(store, p.Projects)
	if err != nil {
		return nil, err
	}
	p.SecretReferences, err = projectvariable.NewSecretReferences(store, p.SecretDirectory, p.Agents)
	if err != nil {
		return nil, err
	}
	p.SkillAuthority, err = skill.NewAuthority(store, p.Projects)
	if err != nil {
		return nil, err
	}
	p.Skills, err = skill.NewAgentInitializer(p.SkillAuthority, p.Agents)
	if err != nil {
		return nil, err
	}
	p.Mounts, err = mount.NewConfiguration(store, p.Projects, p.Agents)
	if err != nil {
		return nil, err
	}
	p.Registry, err = registry.New(store, registry.Options{
		Sources:        options.BuiltinSources,
		Authorizations: registry.Authorizations{Sessions: p.Accounts, Projects: p.Projects},
	})
	if err != nil {
		return nil, err
	}
	// This wrapper only extends the Audit port. All configuration providers
	// keep the original concrete Project authority above.
	skillFacts, err := skill.NewInitializationAuditFacts(p.SkillAuthority, objectFacts)
	if err != nil {
		return nil, err
	}
	p.AuditProjects, err = project.NewInitializationAuditAuthority(p.Projects, skillFacts)
	if err != nil {
		return nil, err
	}
	catalog := event.NewCatalog()
	agentEvents, err := agentc.RegisterAgentEvents(catalog)
	if err != nil {
		return nil, err
	}
	p.AgentEvents, err = agent.NewEventAuthority(p.Agents, agentEvents)
	if err != nil {
		return nil, err
	}
	return &Assembly{store: store, providers: p, catalog: catalog, installSource: options.InstallSourceKey}, nil
}

func (a *Assembly) Providers() Providers {
	if a == nil {
		return Providers{}
	}
	return a.providers
}

// NewService never supplies an install-skill stub or a false/empty workaround.
// The caller must bind its real install backend/source and original process
// authority. The caller owns Stop/Drain/Joined for the returned Agent service;
// this helper creates no Outbox Runtime or background process of its own.
func (a *Assembly) NewService(keys cursor.Keyring, processes oc.ProcessAuthority) (*agent.Service, error) {
	if a == nil || nilPort(a.store) {
		return nil, unbound()
	}
	p := a.providers
	tools, err := registry.NewConfiguration(p.Registry, p.Agents, a.installSource)
	if err != nil {
		return nil, err
	}
	if a.installSource == "" || nilPort(processes) {
		return nil, unbound()
	}
	auditor, err := audit.New(a.store, keys, audit.Authorizations{
		Accounts: p.Accounts, Sessions: p.Accounts, System: p.Accounts, Projects: p.AuditProjects, Models: p.Models,
	})
	if err != nil {
		return nil, err
	}
	journal, err := outbox.New(a.store, a.catalog, outbox.Authorizations{
		Producers: map[event.StableName]oc.ProducerAuthority{agentc.AgentProducer: p.AgentEvents},
		Projects:  p.Projects, Processes: processes, Sessions: p.Accounts, System: p.Accounts, Audit: auditor, Cursors: keys,
	})
	if err != nil {
		return nil, err
	}
	return agent.New(a.store, agent.Dependencies{
		Authority: p.Agents, EventAuthority: p.AgentEvents, Events: journal, Audit: auditor, Activity: p.Accounts,
		Models: p.ModelReferences, Tools: tools, ToolReferences: tools, Mounts: p.Mounts,
		Secrets: p.SecretDirectory, SecretReferences: p.SecretReferences, Skills: p.Skills,
	})
}
